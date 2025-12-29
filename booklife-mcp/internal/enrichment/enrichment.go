package enrichment

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/user/booklife-mcp/internal/models"
	"github.com/user/booklife-mcp/internal/providers"
)

// Service handles metadata enrichment for history entries
type Service struct {
	db                *sql.DB
	hardcoverEnricher *HardcoverEnricher
	openLibrary       *OpenLibraryEnricher
	googleBooks       *GoogleBooksEnricher
	hardcover         providers.HardcoverProvider // For ISBN lookup fallback
	jobManager        *JobManager
}

// NewService creates a new enrichment service
func NewService(db *sql.DB, openLibrary *OpenLibraryEnricher, googleBooks *GoogleBooksEnricher, hardcover providers.HardcoverProvider, hardcoverEnricher *HardcoverEnricher) *Service {
	return &Service{
		db:                db,
		hardcoverEnricher: hardcoverEnricher,
		openLibrary:       openLibrary,
		googleBooks:       googleBooks,
		hardcover:         hardcover,
		jobManager:        NewJobManager(),
	}
}

// EnrichHistoryBackground starts a background job to enrich all history entries
// Returns the job for tracking progress
func (s *Service) EnrichHistoryBackground(ctx context.Context, force bool) (*Job, error) {
	// Validate prerequisites
	if err := s.validatePrerequisites(); err != nil {
		return nil, err
	}

	// Get list of books to enrich
	books, err := s.getBooksToEnrich(force)
	if err != nil {
		return nil, fmt.Errorf("getting books to enrich: %w", err)
	}

	if len(books) == 0 {
		return nil, fmt.Errorf("no books to enrich\n\n" +
			"All books in your history are already enriched.\n\n" +
			"Use force=true to re-enrich all books")
	}

	// Create job
	job := s.jobManager.CreateJob()
	job.UpdateProgress(func(p *JobProgress) {
		p.TotalBooks = len(books)
		p.Status = JobStatusRunning
	})

	// Start background processing
	go s.processEnrichmentJob(job, books, force)

	return job, nil
}

// validatePrerequisites checks if enrichment can be started
func (s *Service) validatePrerequisites() error {
	// Check if book_enrichment table exists
	var tableName string
	err := s.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='book_enrichment'").Scan(&tableName)
	if err != nil {
		return fmt.Errorf("database schema not initialized: %w\n\n"+
			"The book_enrichment table does not exist.\n\n"+
			"Fix:\n"+
			"1. Restart the BookLife server to initialize schema\n"+
			"2. Or delete and recreate database:\n"+
			"   rm ~/.local/share/booklife/history.db", err)
	}

	// Check if we have enrichment providers
	if s.openLibrary == nil && s.googleBooks == nil {
		return fmt.Errorf("no enrichment providers configured\n\n" +
			"At least one provider (Open Library or Google Books) must be configured")
	}

	return nil
}

// getBooksToEnrich gets the list of books that need enrichment
func (s *Service) getBooksToEnrich(force bool) ([]bookToEnrich, error) {
	var query string
	if force {
		// Re-enrich all books
		query = `
			SELECT id, title, author, isbn
			FROM history
			WHERE activity = 'Returned'
			  AND (timestamp IS NULL OR timestamp > 0)
			ORDER BY timestamp DESC NULLS LAST
		`
	} else {
		// Only enrich books without enrichment data
		query = `
			SELECT h.id, h.title, h.author, h.isbn
			FROM history h
			LEFT JOIN book_enrichment e ON h.id = e.history_id
			WHERE h.activity = 'Returned'
				AND e.id IS NULL
				AND (h.timestamp IS NULL OR h.timestamp > 0)
			ORDER BY h.timestamp DESC NULLS LAST
		`
	}

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("querying books: %w", err)
	}
	defer rows.Close()

	var books []bookToEnrich
	for rows.Next() {
		var book bookToEnrich
		var isbn sql.NullString
		if err := rows.Scan(&book.HistoryID, &book.Title, &book.Author, &isbn); err != nil {
			return nil, fmt.Errorf("scanning book: %w", err)
		}
		if isbn.Valid {
			book.ISBN = isbn.String
		}
		books = append(books, book)
	}

	return books, nil
}

type bookToEnrich struct {
	HistoryID int
	Title     string
	Author    string
	ISBN      string
}

// processEnrichmentJob processes books in the background with progress updates
func (s *Service) processEnrichmentJob(job *Job, books []bookToEnrich, force bool) {
	job.SetStatus(JobStatusRunning)

	for i, book := range books {
		// Check for cancellation
		select {
		case <-job.ctx.Done():
			job.SetStatus(JobStatusCancelled)
			return
		default:
		}

		// Update current book
		job.UpdateProgress(func(p *JobProgress) {
			p.ProcessedBooks = i
			p.CurrentBook = book.Title
			p.CurrentAuthor = book.Author
		})

		// Enrich the book
		_, err := s.EnrichBook(job.ctx, book.HistoryID, book.Title, book.Author, book.ISBN)
		if err != nil {
			job.UpdateProgress(func(p *JobProgress) {
				p.FailedBooks++
			})
			job.AddError(book.Title, err.Error())
		} else {
			job.UpdateProgress(func(p *JobProgress) {
				p.SuccessfulBooks++
			})
		}
	}

	// Mark as completed
	job.UpdateProgress(func(p *JobProgress) {
		p.ProcessedBooks = len(books)
		p.CurrentBook = ""
		p.CurrentAuthor = ""
	})
	job.SetStatus(JobStatusCompleted)
}

// GetJobProgress returns the progress of a specific job
func (s *Service) GetJobProgress(jobID string) (*JobProgress, error) {
	job, err := s.jobManager.GetJob(jobID)
	if err != nil {
		return nil, err
	}

	progress := job.GetProgress()
	return &progress, nil
}

// GetCurrentJobProgress returns the progress of the most recent job
func (s *Service) GetCurrentJobProgress() (*JobProgress, error) {
	job := s.jobManager.GetCurrentJob()
	if job == nil {
		return nil, fmt.Errorf("no enrichment jobs found")
	}

	progress := job.GetProgress()
	return &progress, nil
}

// CancelJob cancels a running enrichment job
func (s *Service) CancelJob(jobID string) error {
	return s.jobManager.CancelJob(jobID)
}

// EnrichBook enriches a single history entry with external metadata
func (s *Service) EnrichBook(ctx context.Context, historyID int, title, author, isbn string) (*models.BookEnrichment, error) {
	// Try Hardcover enricher first (primary source with rich metadata)
	var hcData *HardcoverData
	var hcErr error

	if s.hardcoverEnricher != nil {
		// Try direct enrichment from Hardcover
		hcData, hcErr = s.hardcoverEnricher.GetByTitleAuthor(ctx, title, author)
		if hcErr == nil && hcData != nil {
			// Found in Hardcover with full enrichment data
			return s.saveEnrichment(ctx, historyID, title, author, hcData, nil, nil)
		}
	}

	// If no ISBN, try to get it from Hardcover search (for ISBN-based fallback to OL/GB)
	if isbn == "" && s.hardcover != nil {
		books, _, err := s.hardcover.SearchBooks(ctx, title+" "+author, 0, 3)
		if err == nil && len(books) > 0 {
			// Try to find best match by author
			for _, book := range books {
				// Check if author matches
				for _, bookAuthor := range book.Authors {
					if strings.Contains(strings.ToLower(bookAuthor.Name), strings.ToLower(author)) ||
						strings.Contains(strings.ToLower(author), strings.ToLower(bookAuthor.Name)) {
						// Found a match! Use its ISBN
						if book.ISBN13 != "" {
							isbn = book.ISBN13
							break
						} else if book.ISBN10 != "" {
							isbn = book.ISBN10
							break
						}
					}
				}
				if isbn != "" {
					break
				}
			}
			// If no author match, use first result's ISBN if available
			if isbn == "" && len(books) > 0 {
				if books[0].ISBN13 != "" {
					isbn = books[0].ISBN13
				} else if books[0].ISBN10 != "" {
					isbn = books[0].ISBN10
				}
			}
		}
	}

	// Try Open Library as second fallback
	var olData *OpenLibraryData
	var olErr error
	var gbData *GoogleBooksData
	var gbErr error

	if s.openLibrary != nil {
		if isbn != "" {
			olData, olErr = s.openLibrary.GetByISBN(ctx, isbn)
		} else {
			olData, olErr = s.openLibrary.SearchByTitleAuthor(ctx, title, author)
		}
		if olErr == nil && olData != nil {
			// Found in Open Library
			return s.saveEnrichment(ctx, historyID, title, author, nil, olData, nil)
		}
	}

	// Try Google Books as final fallback
	if s.googleBooks != nil {
		if isbn != "" {
			gbData, gbErr = s.googleBooks.GetByISBN(ctx, isbn)
		} else {
			gbData, gbErr = s.googleBooks.SearchByTitleAuthor(ctx, title, author)
		}
		if gbErr == nil && gbData != nil {
			// Found in Google Books
			return s.saveEnrichment(ctx, historyID, title, author, nil, olData, gbData)
		}
	}

	// Build error message with details
	errMsg := "book not found in any enrichment source"
	var errs []string
	if hcErr != nil {
		errs = append(errs, fmt.Sprintf("Hardcover (%s)", hcErr.Error()))
	}
	if olErr != nil {
		errs = append(errs, fmt.Sprintf("Open Library (%s)", olErr.Error()))
	}
	if gbErr != nil {
		errs = append(errs, fmt.Sprintf("Google Books (%s)", gbErr.Error()))
	}

	if len(errs) > 0 {
		errMsg = fmt.Sprintf("all sources failed: %s", strings.Join(errs, ", "))
	}

	return nil, fmt.Errorf("%s", errMsg)
}

// GetEnrichment retrieves enrichment data for a history entry
func (s *Service) GetEnrichment(historyID int) (*models.BookEnrichment, error) {
	query := `
		SELECT id, history_id, title, author, openlibrary_id, googlebooks_id,
		       description, themes, topics, mood, complexity,
		       series_name, series_position, series_total,
		       enrichment_sources, enriched_at, updated_at
		FROM book_enrichment
		WHERE history_id = ?
	`

	var e models.BookEnrichment
	var themesJSON, topicsJSON, moodJSON, sourcesJSON sql.NullString

	err := s.db.QueryRow(query, historyID).Scan(
		&e.ID, &e.HistoryID, &e.Title, &e.Author,
		&e.OpenLibraryID, &e.GoogleBooksID,
		&e.Description,
		&themesJSON, &topicsJSON, &moodJSON,
		&e.Complexity,
		&e.SeriesName, &e.SeriesPosition, &e.SeriesTotal,
		&sourcesJSON, &e.EnrichedAt, &e.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	// Parse JSON arrays
	if themesJSON.Valid {
		json.Unmarshal([]byte(themesJSON.String), &e.Themes)
	}
	if topicsJSON.Valid {
		json.Unmarshal([]byte(topicsJSON.String), &e.Topics)
	}
	if moodJSON.Valid {
		json.Unmarshal([]byte(moodJSON.String), &e.Mood)
	}
	if sourcesJSON.Valid {
		json.Unmarshal([]byte(sourcesJSON.String), &e.EnrichmentSources)
	}

	return &e, nil
}

// GetEnrichmentByTitleAuthor retrieves enrichment by title and author (fuzzy match)
func (s *Service) GetEnrichmentByTitleAuthor(title, author string) (*models.BookEnrichment, error) {
	query := `
		SELECT id, history_id, title, author, openlibrary_id, googlebooks_id,
		       description, themes, topics, mood, complexity,
		       series_name, series_position, series_total,
		       enrichment_sources, enriched_at, updated_at
		FROM book_enrichment
		WHERE title LIKE ? AND author LIKE ?
		ORDER BY updated_at DESC
		LIMIT 1
	`

	var e models.BookEnrichment
	var themesJSON, topicsJSON, moodJSON, sourcesJSON sql.NullString

	err := s.db.QueryRow(query, "%"+title+"%", "%"+author+"%").Scan(
		&e.ID, &e.HistoryID, &e.Title, &e.Author,
		&e.OpenLibraryID, &e.GoogleBooksID,
		&e.Description,
		&themesJSON, &topicsJSON, &moodJSON,
		&e.Complexity,
		&e.SeriesName, &e.SeriesPosition, &e.SeriesTotal,
		&sourcesJSON, &e.EnrichedAt, &e.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	// Parse JSON arrays
	if themesJSON.Valid {
		json.Unmarshal([]byte(themesJSON.String), &e.Themes)
	}
	if topicsJSON.Valid {
		json.Unmarshal([]byte(topicsJSON.String), &e.Topics)
	}
	if moodJSON.Valid {
		json.Unmarshal([]byte(moodJSON.String), &e.Mood)
	}
	if sourcesJSON.Valid {
		json.Unmarshal([]byte(sourcesJSON.String), &e.EnrichmentSources)
	}

	return &e, nil
}

// FindSimilar finds books similar to the given enrichment data
func (s *Service) FindSimilar(enrichment *models.BookEnrichment, limit int) ([]models.BookEnrichment, error) {
	// Build query to find books with similar themes/topics
	query := `
		SELECT id, history_id, title, author, openlibrary_id, googlebooks_id,
		       description, themes, topics, mood, complexity,
		       series_name, series_position, series_total,
		       enrichment_sources, enriched_at, updated_at
		FROM book_enrichment
		WHERE id != ?
		ORDER BY
			-- Prefer same series
			CASE WHEN series_name = ? THEN 0 ELSE 1 END,
			-- Then by shared themes (simple overlap)
			updated_at DESC
		LIMIT ?
	`

	rows, err := s.db.Query(query, enrichment.ID, enrichment.SeriesName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []models.BookEnrichment
	for rows.Next() {
		var e models.BookEnrichment
		var themesJSON, topicsJSON, moodJSON, sourcesJSON sql.NullString

		err := rows.Scan(
			&e.ID, &e.HistoryID, &e.Title, &e.Author,
			&e.OpenLibraryID, &e.GoogleBooksID,
			&e.Description,
			&themesJSON, &topicsJSON, &moodJSON,
			&e.Complexity,
			&e.SeriesName, &e.SeriesPosition, &e.SeriesTotal,
			&sourcesJSON, &e.EnrichedAt, &e.UpdatedAt,
		)
		if err != nil {
			continue
		}

		// Parse JSON arrays
		if themesJSON.Valid {
			json.Unmarshal([]byte(themesJSON.String), &e.Themes)
		}
		if topicsJSON.Valid {
			json.Unmarshal([]byte(topicsJSON.String), &e.Topics)
		}
		if moodJSON.Valid {
			json.Unmarshal([]byte(moodJSON.String), &e.Mood)
		}
		if sourcesJSON.Valid {
			json.Unmarshal([]byte(sourcesJSON.String), &e.EnrichmentSources)
		}

		// Calculate similarity score
		score := calculateSimilarity(enrichment, &e)
		if score > 0.1 { // Minimum threshold
			results = append(results, e)
		}
	}

	return results, nil
}

// saveEnrichment saves enrichment data to the database
func (s *Service) saveEnrichment(ctx context.Context, historyID int, title, author string, hcData *HardcoverData, olData *OpenLibraryData, gbData *GoogleBooksData) (*models.BookEnrichment, error) {
	e := &models.BookEnrichment{
		HistoryID: historyID,
		Title:     title,
		Author:    author,
	}

	sources := []string{}

	// Merge data from Hardcover (primary source)
	if hcData != nil {
		// Hardcover has the richest metadata for reading trackers
		if e.Description == "" {
			e.Description = hcData.Description
		}
		e.Topics = mergeUnique(e.Topics, hcData.Genres) // Map genres to topics
		e.Themes = mergeUnique(e.Themes, hcData.Themes)
		e.Mood = mergeUnique(e.Mood, hcData.Moods)
		if hcData.SeriesName != "" {
			e.SeriesName = hcData.SeriesName
			e.SeriesPosition = hcData.SeriesPosition
		}
		sources = append(sources, models.SourceHardcover)
	}

	// Merge data from Open Library (fallback)
	if olData != nil {
		e.OpenLibraryID = olData.OpenLibraryID
		if e.Description == "" {
			e.Description = olData.Description
		}
		e.Themes = mergeUnique(e.Themes, olData.Themes)
		e.Topics = mergeUnique(e.Topics, olData.Subjects)
		if olData.SeriesName != "" && e.SeriesName == "" {
			e.SeriesName = olData.SeriesName
			e.SeriesPosition = olData.SeriesPosition
		}
		sources = append(sources, models.SourceOpenLibrary)
	}

	// Merge data from Google Books (final fallback)
	if gbData != nil {
		e.GoogleBooksID = gbData.GoogleBooksID
		if e.Description == "" {
			e.Description = gbData.Description
		}
		e.Topics = mergeUnique(e.Topics, gbData.Categories)
		sources = append(sources, models.SourceGoogleBooks)
	}

	// Extract themes and mood from description if missing
	if e.Description != "" && len(e.Themes) == 0 {
		e.Themes = extractThemes(e.Description)
		e.Mood = extractMood(e.Description)
	}

	// Infer complexity if not set
	if e.Complexity == "" {
		e.Complexity = inferComplexity(e.Description, e.Topics)
	}

	e.EnrichmentSources = sources
	e.EnrichedAt = time.Now()
	e.UpdatedAt = time.Now()

	// Save to database
	themesJSON, _ := json.Marshal(e.Themes)
	topicsJSON, _ := json.Marshal(e.Topics)
	moodJSON, _ := json.Marshal(e.Mood)
	sourcesJSON, _ := json.Marshal(e.EnrichmentSources)

	query := `
		INSERT INTO book_enrichment
		(history_id, title, author, openlibrary_id, googlebooks_id, description,
		 themes, topics, mood, complexity, series_name, series_position, series_total,
		 enrichment_sources, enriched_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(history_id) DO UPDATE SET
			openlibrary_id = excluded.openlibrary_id,
			googlebooks_id = excluded.googlebooks_id,
			description = COALESCE(NULLIF(excluded.description, ''), book_enrichment.description),
			themes = excluded.themes,
			topics = excluded.topics,
			mood = excluded.mood,
			complexity = COALESCE(NULLIF(excluded.complexity, ''), book_enrichment.complexity),
			series_name = COALESCE(NULLIF(excluded.series_name, ''), book_enrichment.series_name),
			series_position = COALESCE(NULLIF(excluded.series_position, 0), book_enrichment.series_position),
			enrichment_sources = excluded.enrichment_sources,
			updated_at = CURRENT_TIMESTAMP
	`

	_, err := s.db.ExecContext(ctx, query,
		e.HistoryID, e.Title, e.Author,
		e.OpenLibraryID, e.GoogleBooksID, e.Description,
		string(themesJSON), string(topicsJSON), string(moodJSON),
		e.Complexity,
		e.SeriesName, e.SeriesPosition, e.SeriesTotal,
		string(sourcesJSON), e.EnrichedAt, e.UpdatedAt,
	)

	return e, err
}

// calculateSimilarity calculates similarity between two enrichment records
func calculateSimilarity(a, b *models.BookEnrichment) float64 {
	score := 0.0

	// Same series (high weight)
	if a.SeriesName != "" && a.SeriesName == b.SeriesName {
		score += 0.5
	}

	// Shared themes
	themeOverlap := 0
	if len(a.Themes) > 0 && len(b.Themes) > 0 {
		aThemes := make(map[string]bool)
		for _, t := range a.Themes {
			aThemes[t] = true
		}
		for _, t := range b.Themes {
			if aThemes[t] {
				themeOverlap++
			}
		}
		score += float64(themeOverlap) * 0.15
	}

	// Shared topics
	topicOverlap := 0
	if len(a.Topics) > 0 && len(b.Topics) > 0 {
		aTopics := make(map[string]bool)
		for _, t := range a.Topics {
			aTopics[t] = true
		}
		for _, t := range b.Topics {
			if aTopics[t] {
				topicOverlap++
			}
		}
		score += float64(topicOverlap) * 0.1
	}

	// Shared mood
	if len(a.Mood) > 0 && len(b.Mood) > 0 {
		aMoods := make(map[string]bool)
		for _, m := range a.Mood {
			aMoods[m] = true
		}
		for _, m := range b.Mood {
			if aMoods[m] {
				score += 0.1
			}
		}
	}

	// Same author (medium weight)
	aAuthors := parseAuthorList(a.Author)
	bAuthors := parseAuthorList(b.Author)
	for _, aa := range aAuthors {
		for _, ba := range bAuthors {
			if aa == ba {
				score += 0.2
				break
			}
		}
	}

	if score > 1.0 {
		score = 1.0
	}

	return score
}

// mergeUnique merges two string slices, removing duplicates
func mergeUnique(a, b []string) []string {
	seen := make(map[string]bool)
	result := []string{}

	for _, s := range a {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	for _, s := range b {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}

	return result
}

// parseAuthorList parses author string into individual names
func parseAuthorList(author string) []string {
	// Split by common delimiters
	parts := strings.FieldsFunc(author, func(r rune) bool {
		return r == ',' || r == ';' || r == '&'
	})

	result := []string{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}

	return result
}

// extractThemes extracts themes from description using keyword matching
func extractThemes(description string) []string {
	desc := strings.ToLower(description)
	themes := []string{}

	// Theme keyword mappings
	themeKeywords := map[string][]string{
		models.ThemeComingOfAge: {"coming of age", "growing up", "adolescence", "youth"},
		models.ThemeGoodVsEvil:  {"good vs evil", "battle between", "light and dark"},
		models.ThemeIdentity:    {"identity", "who they are", "self-discovery"},
		models.ThemeLove:        {"love", "romance", "fall in love"},
		models.ThemeFriendship:  {"friendship", "friends", "bond"},
		models.ThemeSurvival:    {"survival", "survive", "struggle to survive"},
		models.ThemeRedemption:  {"redemption", "redeem", "second chance"},
		models.ThemeJustice:     {"justice", "injustice", "legal"},
		models.ThemeCourage:     {"courage", "bravery", "brave"},
		models.ThemeFamily:      {"family", "mother", "father", "sibling", "parent"},
		models.ThemeBetrayal:    {"betrayal", "betray", "treason"},
		models.ThemePower:       {"power", "political", "authority"},
		models.ThemeFreedom:     {"freedom", "liberty", "independence"},
		models.ThemeRevenge:     {"revenge", "avenge", "vengeance"},
		models.ThemeWar:         {"war", "battle", "conflict", "soldier"},
		models.ThemeTechnology:  {"technology", "tech", "digital", "ai", "artificial intelligence"},
		models.ThemeAdventure:   {"adventure", "quest", "journey", "expedition"},
		models.ThemeMystery:     {"mystery", "detective", "investigation"},
		models.ThemeHorror:      {"horror", "terrifying", "scary"},
		models.ThemeSciFi:       {"science fiction", "sci-fi", "space", "futuristic"},
		models.ThemeFantasy:     {"fantasy", "magic", "magical", "dragon", "wizard"},
		models.ThemeHistorical:  {"historical", "history", "period piece"},
	}

	for theme, keywords := range themeKeywords {
		for _, kw := range keywords {
			if strings.Contains(desc, kw) {
				themes = append(themes, theme)
				break
			}
		}
	}

	return themes
}

// extractMood extracts mood from description using keyword matching
func extractMood(description string) []string {
	desc := strings.ToLower(description)
	moods := []string{}

	// Mood keyword mappings
	moodKeywords := map[string][]string{
		models.MoodHopeful:     {"hope", "hopeful", "optimistic", "inspiring"},
		models.MoodDark:        {"dark", "grim", "bleak", "disturbing"},
		models.MoodFunny:       {"funny", "humor", "comic", "witty", "satire"},
		models.MoodEducational: {"learn", "educational", "informative", "guide"},
		models.MoodThrilling:   {"thriller", "suspense", "tense", "exciting"},
		models.MoodRomantic:    {"romance", "love story", "romantic"},
		models.MoodMysterious:  {"mystery", "enigma", "puzzle"},
		models.MoodInspiring:   {"inspire", "inspirational", "uplifting"},
		models.MoodMelancholic: {"sad", "melancholy", "tragic", "sorrow"},
		models.MoodWhimsical:   {"whimsical", "quirky", "playful", "charming"},
	}

	for mood, keywords := range moodKeywords {
		for _, kw := range keywords {
			if strings.Contains(desc, kw) {
				moods = append(moods, mood)
				break
			}
		}
	}

	return moods
}

// inferComplexity infers complexity level from description and topics
func inferComplexity(description string, topics []string) string {
	desc := strings.ToLower(description)

	// Academic/technical topics suggest advanced complexity
	advancedKeywords := []string{
		"academic", "scholarly", "research", "thesis", "philosophy",
		"quantum", "theoretical", "analysis", "technical",
	}
	for _, kw := range advancedKeywords {
		if strings.Contains(desc, kw) {
			return models.ComplexityAdvanced
		}
	}

	// Check topics for complexity indicators
	for _, t := range topics {
		topic := strings.ToLower(t)
		if strings.Contains(topic, "philosophy") ||
			strings.Contains(topic, "science") ||
			strings.Contains(topic, "academic") ||
			strings.Contains(topic, "theory") {
			return models.ComplexityAdvanced
		}
	}

	// Simple/accessible language suggests beginner
	beginnerKeywords := []string{
		"introduction", "beginner", "basics", "easy", "simple",
		"children", "kids", "young adult", "ya",
	}
	for _, kw := range beginnerKeywords {
		if strings.Contains(desc, kw) {
			return models.ComplexityBeginner
		}
	}

	return models.ComplexityIntermediate
}
