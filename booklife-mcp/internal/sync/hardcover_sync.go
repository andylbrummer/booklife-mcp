package sync

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/user/booklife-mcp/internal/models"
	"golang.org/x/time/rate"
)

// HardcoverProvider defines the interface for Hardcover operations
type HardcoverProvider interface {
	SearchBooks(ctx context.Context, query string, offset, limit int) ([]models.Book, int, error)
	AddBook(ctx context.Context, isbn, title, author, status string) (string, error)
	UpdateBookStatus(ctx context.Context, bookID, status string, progress int, rating float64) error
	GetUserBooks(ctx context.Context, status string, offset, limit int) ([]models.Book, int, error)
}

// HistoryStore defines the interface for history operations
type HistoryStore interface {
	GetUnsyncedReturns(targetSystem string, limit ...int) ([]models.TimelineEntry, error)
	MarkEntrySynced(titleID string, timestamp int64, activity, targetSystem, targetBookID string, status SyncStatus, errorMsg string) error
	GetSyncState(titleID, activity string, timestamp int64, targetSystem string) (*HistorySyncState, error)

	// Book identity cache methods
	GetBookIdentityByLibbyID(libbyTitleID string) (*BookIdentity, error)
	GetBookIdentityByISBN(isbn string) (*BookIdentity, error)
	SaveBookIdentity(bi *BookIdentity) error
}

// LibbySearcher defines the interface for searching Libby catalog
// Used to find alternate format ISBNs when audiobook ISBN doesn't match Hardcover
type LibbySearcher interface {
	Search(ctx context.Context, query string, formats []string, available bool, offset, limit int) ([]models.Book, int, error)
}

// HardcoverSync syncs reading history to Hardcover
type HardcoverSync struct {
	hardcover         HardcoverProvider
	store             HistoryStore
	libby             LibbySearcher // Optional: for cross-format ISBN lookup
	dryRun            bool
	rateLimiter       *rate.Limiter
	readBooksCache    map[string]bool // Cache of book IDs already marked as read
	cacheMu           sync.RWMutex
	backoffUntil      time.Time  // Time until which we should not make requests
	backoffLevel      int        // Current backoff level (for exponential backoff)
	consecutiveErrors int        // Count of consecutive errors
	mu                sync.Mutex // Protects backoff fields
	limit             int        // Max number of entries to sync (0 = all)
}

// BookIdentity represents a cross-platform book identity mapping
type BookIdentity struct {
	LibbyTitleID string
	HardcoverID  string
	ISBN10       string
	ISBN13       string
	Title        string
	Author       string
}

// NewHardcoverSync creates a new HardcoverSync
func NewHardcoverSync(hardcover HardcoverProvider, store HistoryStore) *HardcoverSync {
	// Rate limit to 3 requests per 2 seconds to avoid throttling (increased from 1)
	return &HardcoverSync{
		hardcover:      hardcover,
		store:          store,
		dryRun:         false,
		rateLimiter:    rate.NewLimiter(rate.Every(600*time.Millisecond), 1), // ~1.6 req/sec
		readBooksCache: make(map[string]bool),
	}
}

// SetDryRun enables or disables dry run mode
func (s *HardcoverSync) SetDryRun(dryRun bool) {
	s.dryRun = dryRun
}

// SetLimit sets the maximum number of entries to sync (0 = all)
func (s *HardcoverSync) SetLimit(limit int) {
	s.limit = limit
}

// SetLibbySearcher sets the Libby searcher for cross-format ISBN lookup
func (s *HardcoverSync) SetLibbySearcher(libby LibbySearcher) {
	s.libby = libby
}

// SyncReturnedBooks syncs all returned books from history to Hardcover as "read"
func (s *HardcoverSync) SyncReturnedBooks(ctx context.Context) (*SyncSummary, error) {
	if s.hardcover == nil {
		return nil, fmt.Errorf("hardcover provider not configured")
	}

	// Pre-cache all read books to avoid repeated API calls
	log.Printf("Loading existing read books from Hardcover...")
	if err := s.loadReadBooksCache(ctx); err != nil {
		log.Printf("Warning: couldn't load read books cache: %v (will check individually)", err)
	} else {
		log.Printf("Cached %d read books", len(s.readBooksCache))
	}

	// Get all unsynced "Returned" entries
	entries, err := s.store.GetUnsyncedReturns("hardcover", s.limit)
	if err != nil {
		return nil, fmt.Errorf("getting unsynced returns: %w", err)
	}

	summary := &SyncSummary{
		Results: make([]SyncResult, 0, len(entries)),
	}

	total := len(entries)
	s.mu.Lock()
	s.consecutiveErrors = 0
	s.backoffLevel = 0
	s.mu.Unlock()

	for i, entry := range entries {
		// Progress indicator
		if (i+1)%10 == 0 || i == 0 {
			log.Printf("Progress: %d/%d (%.0f%%) [success: %d, skip: %d, fail: %d]",
				i+1, total, float64(i+1)/float64(total)*100,
				summary.Successful, summary.Skipped, summary.Failed)
		}

		result := s.syncEntry(ctx, entry)
		summary.Results = append(summary.Results, result)
		summary.TotalProcessed++

		if result.Success || result.Skipped {
			summary.Successful += countIf(result.Success)
			summary.Skipped += countIf(result.Skipped)
			// Reset consecutive errors on success
			s.mu.Lock()
			s.consecutiveErrors = 0
			s.backoffLevel = 0
			s.mu.Unlock()
		} else {
			summary.Failed++
			if result.ErrorMessage != "" {
				summary.Errors = append(summary.Errors, fmt.Sprintf("%s: %s", entry.Title, result.ErrorMessage))
			}

			// Handle error with backoff
			if s.shouldBackoff(result.ErrorMessage) {
				waitTime := s.incrementBackoff()
				log.Printf("Consecutive errors detected, backing off for %v...", waitTime)
				select {
				case <-ctx.Done():
					return summary, ctx.Err()
				case <-time.After(waitTime):
				}
			}
		}
	}

	return summary, nil
}

func countIf(b bool) int {
	if b {
		return 1
	}
	return 0
}

// loadReadBooksCache pre-loads all read books into memory
func (s *HardcoverSync) loadReadBooksCache(ctx context.Context) error {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	offset := 0
	limit := 100
	for {
		if err := s.waitForRateLimit(ctx); err != nil {
			return err
		}

		books, total, err := s.hardcover.GetUserBooks(ctx, "read", offset, limit)
		if err != nil {
			return err
		}

		for _, book := range books {
			s.readBooksCache[book.HardcoverID] = true
		}

		offset += len(books)
		if offset >= total || len(books) == 0 {
			break
		}
	}

	return nil
}

// waitForRateLimit waits for rate limiter and handles backoff
func (s *HardcoverSync) waitForRateLimit(ctx context.Context) error {
	s.mu.Lock()
	backoffUntil := s.backoffUntil
	s.mu.Unlock()

	// Check if we're in a backoff period
	if time.Now().Before(backoffUntil) {
		waitTime := time.Until(backoffUntil)
		log.Printf("[BACKOFF] In backoff period, waiting %v...", waitTime.Round(time.Second))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
			// Clear backoff after waiting
			s.mu.Lock()
			s.backoffUntil = time.Time{}
			s.mu.Unlock()
		}
	}

	return s.rateLimiter.Wait(ctx)
}

// isRateLimitError checks if an error is a rate limit error
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Check for common rate limit indicators
	return strings.Contains(errStr, "429") ||
		strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "throttle") ||
		strings.Contains(errStr, "too many requests") ||
		strings.Contains(errStr, "quota exceeded")
}

// isRetriableError checks if an error is retriable (rate limits, network errors, timeouts)
// Permanent failures (book not found, no match) should return false
func isRetriableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()

	// Rate limit errors are always retriable
	if isRateLimitError(err) {
		return true
	}

	// Network errors that are retriable
	retriablePatterns := []string{
		"timeout",
		"connection refused",
		"connection reset",
		"temporary failure",
		"network",
		"EOF",
	}

	for _, pattern := range retriablePatterns {
		if strings.Contains(strings.ToLower(errStr), pattern) {
			return true
		}
	}

	// "not found" or "no match" errors are permanent (not retriable)
	if strings.Contains(errStr, "not found") ||
		strings.Contains(errStr, "no match") ||
		strings.Contains(errStr, "no exact title") ||
		strings.Contains(errStr, "book not found") {
		return false
	}

	// Other errors are assumed to be retriable
	return true
}

// shouldBackoff determines if we should backoff based on the error
func (s *HardcoverSync) shouldBackoff(errMsg string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if isRateLimitError(fmt.Errorf("%s", errMsg)) {
		// Definitely backoff for rate limit errors
		return true
	}

	// For other errors, backoff after 2 consecutive errors (reduced from 5)
	s.consecutiveErrors++
	if s.consecutiveErrors >= 2 {
		return true
	}
	return false
}

// incrementBackoff increases the backoff level and returns the wait time
// Exponential backoff: 5s, 10s, 20s, 40s, 80s, max 2min
func (s *HardcoverSync) incrementBackoff() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.backoffLevel++

	// Calculate backoff duration: 5 * 2^level, capped at 2 minutes
	backoffDuration := 5 * time.Second * (1 << uint(s.backoffLevel-1))
	if backoffDuration > 2*time.Minute {
		backoffDuration = 2 * time.Minute
	}

	s.backoffUntil = time.Now().Add(backoffDuration)
	return backoffDuration
}

// resetBackoff clears the backoff state (call on success)
func (s *HardcoverSync) resetBackoff() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.backoffLevel = 0
	s.consecutiveErrors = 0
	s.backoffUntil = time.Time{}
}

// syncEntry processes a single history entry
func (s *HardcoverSync) syncEntry(ctx context.Context, entry models.TimelineEntry) SyncResult {
	result := SyncResult{
		Operation: &SyncOperation{
			Operation:     OpUpdateStatus,
			SourceSystem:  "libby",
			TargetSystem:  "hardcover",
			SourceEntryID: entry.TitleID,
			ISBN:          entry.ISBN,
			Title:         entry.Title,
			Author:        entry.Author,
			Status:        "read",
			CreatedAt:     time.Now(),
		},
	}

	// Check if already synced
	existing, _ := s.store.GetSyncState(entry.TitleID, entry.Activity, entry.Timestamp, "hardcover")
	if existing != nil && existing.SyncStatus == StatusCompleted {
		result.Skipped = true
		result.SkipReason = "already synced"
		return result
	}

	// Find the book in Hardcover
	hardcoverBookID, hardcoverTitle, err := s.findHardcoverBook(ctx, entry)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("finding book: %v", err)
		// Check if this is a retriable error (rate limit, network error)
		if isRetriableError(err) {
			s.markSynced(entry, "", StatusFailed, result.ErrorMessage)
		} else {
			result.Skipped = true
			result.SkipReason = result.ErrorMessage
			s.markSynced(entry, "", StatusSkipped, result.SkipReason)
		}
		return result
	}

	if hardcoverBookID == "" {
		result.Skipped = true
		result.SkipReason = "book not found in Hardcover"
		s.markSynced(entry, "", StatusSkipped, result.SkipReason)
		return result
	}

	result.TargetBookID = hardcoverBookID
	result.TargetTitle = hardcoverTitle

	// Dry run - don't actually update
	if s.dryRun {
		result.Success = true
		result.Skipped = true
		result.SkipReason = "dry run"
		return result
	}

	// Check if book is already marked as read
	isAlreadyRead, err := s.isBookRead(ctx, hardcoverBookID)
	if err != nil {
		log.Printf("warning: couldn't check if book is read: %v", err)
	}
	if isAlreadyRead {
		result.Skipped = true
		result.SkipReason = "already marked as read in Hardcover"
		s.markSynced(entry, hardcoverBookID, StatusCompleted, "")
		return result
	}

	// Update status to "read"
	err = s.hardcover.UpdateBookStatus(ctx, hardcoverBookID, "read", 100, 0)
	if err != nil {
		// Book not in library - try to add it first with strict verification
		_, addErr := s.hardcover.AddBook(ctx, entry.ISBN, entry.Title, entry.Author, "read")
		if addErr != nil {
			result.Skipped = true
			result.SkipReason = fmt.Sprintf("book not in library and couldn't add: %v", addErr)
			// Check if this is a retriable error (rate limit, network error)
			if isRetriableError(addErr) {
				s.markSynced(entry, hardcoverBookID, StatusFailed, result.SkipReason)
			} else {
				s.markSynced(entry, hardcoverBookID, StatusSkipped, result.SkipReason)
			}
			return result
		}
		// Successfully added - now it should be in library
		log.Printf("[ADDED] Successfully added to library: %s by %s", entry.Title, entry.Author)
	}

	result.Success = true
	s.markSynced(entry, hardcoverBookID, StatusCompleted, "")
	return result
}

// findHardcoverBook searches for a book in Hardcover, using the cache first
//
// Matching priority:
// 1. Cache by Libby TitleID (exact match - fastest)
// 2. Cache by ISBN (exact match)
// 3. Search by ISBN (exact match - ONLY if no ISBN in cache)
// 4. Search by title + author (ONLY if entry has no ISBN)
//
// IMPORTANT: If an ISBN exists, we ONLY match by ISBN. No fallback to title matching.
// This prevents matching different editions/formats when the specific ISBN isn't in Hardcover.
//
// Returns: (hardcoverID, hardcoverTitle, error)
func (s *HardcoverSync) findHardcoverBook(ctx context.Context, entry models.TimelineEntry) (string, string, error) {
	// Priority 1: Check cache by Libby TitleID (exact match - fastest)
	if s.store != nil && entry.TitleID != "" {
		cached, err := s.store.GetBookIdentityByLibbyID(entry.TitleID)
		if err == nil && cached != nil && cached.HardcoverID != "" {
			log.Printf("[CACHE HIT] Found by Libby TitleID: %s -> Hardcover: %s (%s)", entry.TitleID, cached.HardcoverID, cached.Title)
			return cached.HardcoverID, cached.Title, nil
		}
	}

	// Priority 2: Check cache by ISBN (exact match)
	if s.store != nil && entry.ISBN != "" {
		cached, err := s.store.GetBookIdentityByISBN(entry.ISBN)
		if err == nil && cached != nil && cached.HardcoverID != "" {
			log.Printf("[CACHE HIT] Found by ISBN: %s -> Hardcover: %s (%s)", entry.ISBN, cached.HardcoverID, cached.Title)
			return cached.HardcoverID, cached.Title, nil
		}
	}

	hasISBN := entry.ISBN != ""

	// Priority 3: Search Hardcover API by ISBN (exact external match)
	if hasISBN {
		if err := s.waitForRateLimit(ctx); err != nil {
			return "", "", fmt.Errorf("rate limiter: %w", err)
		}

		books, _, err := s.hardcover.SearchBooks(ctx, entry.ISBN, 0, 10)
		if err == nil && len(books) > 0 {
			// First try: exact ISBN match
			for _, book := range books {
				if book.ISBN10 == entry.ISBN || book.ISBN13 == entry.ISBN {
					log.Printf("[ISBN MATCH] Found exact ISBN match: %s", entry.ISBN)
					s.saveBookIdentity(entry, book.HardcoverID)
					return book.HardcoverID, book.Title, nil
				}
			}

			// Second try: strict title + author match (same book, different edition)
			entryTitleNorm := normalizeTitle(entry.Title)
			for _, book := range books {
				bookTitleNorm := normalizeTitle(book.Title)
				if entryTitleNorm == bookTitleNorm && authorsMatch(entry.Author, book.Authors) {
					log.Printf("[TITLE+AUTHOR MATCH] Same book, different ISBN: \"%s\" by %s (Libby ISBN: %s, HC ISBN: %s/%s)",
						book.Title, entry.Author, entry.ISBN, book.ISBN10, book.ISBN13)
					s.saveBookIdentity(entry, book.HardcoverID)
					return book.HardcoverID, book.Title, nil
				}
			}

			// Search returned results but none matched
			log.Printf("[ISBN MISMATCH] Search for ISBN %s returned %d books but none with matching ISBN or title+author", entry.ISBN, len(books))
		} else {
			// ISBN search returned no results - log but continue to title+author search below
			log.Printf("[ISBN NOT FOUND] Book with ISBN %s not found via ISBN search, will try title+author: %s by %s", entry.ISBN, entry.Title, entry.Author)
		}

		// Priority 3.5: Try cross-format ISBN lookup via Libby
		// If audiobook ISBN failed, search Libby for ebook format ID and try that
		if s.libby != nil && entry.Format == "audiobook" {
			if ebookID, ebookTitle := s.tryEbookISBNFromLibby(ctx, entry); ebookID != "" {
				return ebookID, ebookTitle, nil
			}
		}

		// Continue to title+author search below (not returning empty anymore)
	}

	// Priority 4: Search Hardcover API by title + author (fallback when ISBN not found or no ISBN)
	if err := s.waitForRateLimit(ctx); err != nil {
		return "", "", fmt.Errorf("rate limiter: %w", err)
	}

	// First try: search by title + author
	query := entry.Title
	if entry.Author != "" {
		query += " " + entry.Author
	}

	books, _, err := s.hardcover.SearchBooks(ctx, query, 0, 5)
	if err != nil {
		return "", "", err
	}

	// Try to find a match in the title+author search results
	if matchID, matchTitle := s.findBestMatchWithTitle(entry, books); matchID != "" {
		return matchID, matchTitle, nil
	}

	// If title+author search didn't find anything, try title-only search
	// This handles cases where Libby has different author formatting
	if len(books) == 0 || (len(books) > 0 && !s.anyTitleMatches(entry, books)) {
		log.Printf("[TITLE-ONLY SEARCH] Trying title-only search for: %s", entry.Title)
		if err := s.waitForRateLimit(ctx); err != nil {
			return "", "", fmt.Errorf("rate limiter: %w", err)
		}

		books, _, err = s.hardcover.SearchBooks(ctx, entry.Title, 0, 10)
		if err != nil {
			return "", "", err
		}

		if matchID, matchTitle := s.findBestMatchWithTitle(entry, books); matchID != "" {
			return matchID, matchTitle, nil
		}
	}

	log.Printf("[NOT FOUND] No match for: %s by %s (no ISBN)", entry.Title, entry.Author)
	return "", "", nil
}

// findBestMatchWithTitle searches through books for the best title+author match, returning both ID and title
func (s *HardcoverSync) findBestMatchWithTitle(entry models.TimelineEntry, books []models.Book) (string, string) {
	// Find best match - exact title match first
	for _, book := range books {
		if normalizeTitle(book.Title) == normalizeTitle(entry.Title) {
			log.Printf("[TITLE MATCH] Exact match (no ISBN): %s", book.Title)
			s.saveBookIdentity(entry, book.HardcoverID)
			return book.HardcoverID, book.Title
		}
	}

	// Try matching main title (before subtitle) for cases like "Outlive" vs "Outlive: The Science..."
	for _, book := range books {
		if titleMainPartMatches(entry.Title, book.Title) && authorsMatch(entry.Author, book.Authors) {
			log.Printf("[TITLE+AUTHOR MATCH] Main title matches: \"%s\" vs \"%s\"", entry.Title, book.Title)
			s.saveBookIdentity(entry, book.HardcoverID)
			return book.HardcoverID, book.Title
		}
	}

	// No exact match, use fuzzy match with caution
	if len(books) > 0 {
		if len(entry.Title) > 5 && len(books[0].Title) > 5 {
			// Check if first 10 chars match (more confident than 5)
			entryNorm := normalizeTitle(entry.Title)
			bookNorm := normalizeTitle(books[0].Title)
			minLen := min(len(entryNorm), 10)
			if minLen > len(bookNorm) {
				minLen = len(bookNorm)
			}
			if minLen > 0 && entryNorm[:minLen] == bookNorm[:minLen] {
				log.Printf("[FUZZY MATCH] Partial match (no ISBN): %s ~= %s", entry.Title, books[0].Title)
				s.saveBookIdentity(entry, books[0].HardcoverID)
				return books[0].HardcoverID, books[0].Title
			}
		}
	}

	return "", ""
}

// anyTitleMatches checks if any book in the list has a matching title
func (s *HardcoverSync) anyTitleMatches(entry models.TimelineEntry, books []models.Book) bool {
	for _, book := range books {
		if normalizeTitle(book.Title) == normalizeTitle(entry.Title) {
			return true
		}
		if titleMainPartMatches(entry.Title, book.Title) {
			return true
		}
	}
	return false
}

// saveBookIdentity saves a book identity mapping to the cache
func (s *HardcoverSync) saveBookIdentity(entry models.TimelineEntry, hardcoverID string) {
	if s.store == nil {
		return
	}

	bi := &BookIdentity{
		LibbyTitleID: entry.TitleID,
		HardcoverID:  hardcoverID,
		Title:        entry.Title,
		Author:       entry.Author,
	}

	// Parse ISBN - determine if it's ISBN10 or ISBN13
	if entry.ISBN != "" {
		if len(entry.ISBN) == 10 {
			bi.ISBN10 = entry.ISBN
		} else if len(entry.ISBN) == 13 {
			bi.ISBN13 = entry.ISBN
		}
	}

	if err := s.store.SaveBookIdentity(bi); err != nil {
		log.Printf("warning: failed to save book identity: %v", err)
	} else {
		log.Printf("[CACHE SAVE] Saved mapping: %s -> %s", entry.TitleID, hardcoverID)
	}
}

// isBookRead checks if a book is already marked as read in Hardcover
func (s *HardcoverSync) isBookRead(ctx context.Context, bookID string) (bool, error) {
	// Get user's read books and check if this one is there
	// This is inefficient for large libraries - could be optimized with a cache
	books, _, err := s.hardcover.GetUserBooks(ctx, "read", 0, 1000)
	if err != nil {
		return false, err
	}

	for _, book := range books {
		if book.HardcoverID == bookID {
			return true, nil
		}
	}

	return false, nil
}

// markSynced records the sync state in the database
func (s *HardcoverSync) markSynced(entry models.TimelineEntry, targetBookID string, status SyncStatus, errorMsg string) {
	if s.store == nil {
		return
	}
	err := s.store.MarkEntrySynced(entry.TitleID, entry.Timestamp, entry.Activity, "hardcover", targetBookID, status, errorMsg)
	if err != nil {
		log.Printf("warning: failed to mark entry as synced: %v", err)
	}
}

// normalizeTitle normalizes a title for comparison
func normalizeTitle(title string) string {
	// Simple normalization - could be enhanced
	result := ""
	for _, c := range title {
		if c >= 'a' && c <= 'z' {
			result += string(c)
		} else if c >= 'A' && c <= 'Z' {
			result += string(c + 32) // lowercase
		} else if c >= '0' && c <= '9' {
			result += string(c)
		}
	}
	return result
}

// authorsMatch checks if the Libby author matches any of the Hardcover authors
// Handles cases like "Author Name" vs "Author Name, Co-Author"
// Also handles combined authors like "Peter Attia, MD, Bill Gifford" vs "Peter Attia"
// Matches by: exact name, first author extraction, last name, or substring
func authorsMatch(libbyAuthor string, hardcoreAuthors []models.Contributor) bool {
	if len(hardcoreAuthors) == 0 {
		return false
	}

	// Extract first author from Libby (before comma) for combined author strings
	firstLibbyAuthor := libbyAuthor
	if idx := strings.Index(libbyAuthor, ","); idx > 0 {
		firstLibbyAuthor = strings.TrimSpace(libbyAuthor[:idx])
	}

	// Extract last name (last word) from first Libby author for loose matching
	firstLibbyLastName := getLastWord(firstLibbyAuthor)

	// Normalize for comparison
	libbyNorm := normalizeString(libbyAuthor)
	firstLibbyNorm := normalizeString(firstLibbyAuthor)
	firstLibbyLastNorm := normalizeString(firstLibbyLastName)

	for _, ha := range hardcoreAuthors {
		authorNorm := normalizeString(ha.Name)
		authorLastName := getLastWord(ha.Name)
		authorLastNorm := normalizeString(authorLastName)

		// Check if Libby author matches Hardcover author:
		// 1. Exact match
		// 2. First Libby author matches Hardcover author (handles "Peter Attia, MD, Bill Gifford" vs "Peter Attia")
		// 3. Last name match (handles "P. Attia" vs "Peter Attia" or "Attia" vs "Peter Attia")
		// 4. Substring match (handles "Robert Greene" in "Robert Greene, X")
		if libbyNorm == authorNorm || firstLibbyNorm == authorNorm ||
			firstLibbyLastNorm == authorLastNorm ||
			strings.Contains(libbyNorm, authorNorm) || strings.Contains(authorNorm, libbyNorm) ||
			strings.Contains(firstLibbyNorm, authorNorm) || strings.Contains(authorNorm, firstLibbyNorm) {
			return true
		}
	}

	return false
}

// getLastWord extracts the last word from a name (for last name matching)
func getLastWord(name string) string {
	name = strings.TrimSpace(name)
	words := strings.Fields(name)
	if len(words) == 0 {
		return ""
	}
	return words[len(words)-1]
}

// normalizeString normalizes a string for comparison (lowercase, alphanumeric only)
func normalizeString(s string) string {
	result := ""
	for _, c := range s {
		if c >= 'a' && c <= 'z' {
			result += string(c)
		} else if c >= 'A' && c <= 'Z' {
			result += string(c + 32)
		} else if c >= '0' && c <= '9' {
			result += string(c)
		}
	}
	return result
}

// titleMainPartMatches checks if the main title (before subtitle delimiters) matches
// Handles cases like "Outlive" matching "Outlive: The Science and Art of Longevity"
func titleMainPartMatches(libbyTitle, hardcoverTitle string) bool {
	// Get the main part of the Hardcover title (before subtitle delimiters)
	hardcoverMain := hardcoverTitle
	for _, delimiter := range []string{": ", " - ", " — ", ":  "} {
		if idx := strings.Index(hardcoverTitle, delimiter); idx > 0 {
			hardcoverMain = hardcoverTitle[:idx]
			break
		}
	}

	// Normalize both titles for comparison
	libbyNorm := normalizeTitle(libbyTitle)
	hardcoverNorm := normalizeTitle(hardcoverMain)

	// Check if they match
	return libbyNorm == hardcoverNorm
}

// tryEbookISBNFromLibby searches Libby by title/author to find the ebook format ID
// and tries to match it in Hardcover. This handles the case where audiobook ISBNs
// don't exist in Hardcover but ebook ISBNs do.
func (s *HardcoverSync) tryEbookISBNFromLibby(ctx context.Context, entry models.TimelineEntry) (string, string) {
	if s.libby == nil {
		return "", ""
	}

	// Search Libby by title and author
	query := entry.Title
	if entry.Author != "" {
		query += " " + entry.Author
	}

	if err := s.waitForRateLimit(ctx); err != nil {
		log.Printf("[LIBBY SEARCH] Rate limit error: %v", err)
		return "", ""
	}

	libbyBooks, _, err := s.libby.Search(ctx, query, nil, false, 0, 5)
	if err != nil {
		log.Printf("[LIBBY SEARCH] Error searching Libby: %v", err)
		return "", ""
	}

	// Find matching book in Libby results
	for _, libbyBook := range libbyBooks {
		// Verify title match to avoid false positives
		if normalizeTitle(libbyBook.Title) != normalizeTitle(entry.Title) {
			continue
		}

		// Check if this book has an ebook format with a different ID
		if libbyBook.LibraryAvailability == nil {
			continue
		}

		ebookID := libbyBook.LibraryAvailability.EbookID
		if ebookID == "" || ebookID == entry.ISBN {
			// No ebook ID or same as audiobook ISBN
			continue
		}

		log.Printf("[CROSS-FORMAT] Found ebook ID %s for audiobook %s (%s)", ebookID, entry.ISBN, entry.Title)

		// Try searching Hardcover with the ebook ID
		if err := s.waitForRateLimit(ctx); err != nil {
			log.Printf("[CROSS-FORMAT] Rate limit error: %v", err)
			return "", ""
		}

		hcBooks, _, err := s.hardcover.SearchBooks(ctx, ebookID, 0, 10)
		if err != nil {
			log.Printf("[CROSS-FORMAT] Error searching Hardcover with ebook ID: %v", err)
			continue
		}

		// Look for exact ISBN match or title+author match
		for _, hcBook := range hcBooks {
			if hcBook.ISBN10 == ebookID || hcBook.ISBN13 == ebookID {
				log.Printf("[CROSS-FORMAT MATCH] Found Hardcover book via ebook ISBN: %s -> %s", ebookID, hcBook.HardcoverID)
				s.saveBookIdentity(entry, hcBook.HardcoverID)
				return hcBook.HardcoverID, hcBook.Title
			}

			// Also accept strict title+author match from ebook search
			if normalizeTitle(hcBook.Title) == normalizeTitle(entry.Title) && authorsMatch(entry.Author, hcBook.Authors) {
				log.Printf("[CROSS-FORMAT MATCH] Found Hardcover book via ebook search (title+author): %s", hcBook.HardcoverID)
				s.saveBookIdentity(entry, hcBook.HardcoverID)
				return hcBook.HardcoverID, hcBook.Title
			}
		}
	}

	return "", ""
}
