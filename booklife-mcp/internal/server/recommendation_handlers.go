package server

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/user/booklife-mcp/internal/analytics"
	"github.com/user/booklife-mcp/internal/enrichment"
	"github.com/user/booklife-mcp/internal/graph"
	"github.com/user/booklife-mcp/internal/models"

	_ "github.com/mattn/go-sqlite3"
)

// ===== Recommendation Input Types =====

// GetBookWithAnalysisInput for the book_get_with_analysis tool
type GetBookWithAnalysisInput struct {
	Title string `json:"title"`
	Author string `json:"author"`
}

// FindSimilarBooksInput for the book_find_similar tool
type FindSimilarBooksInput struct {
	Title    string `json:"title"`
	Author   string `json:"author"`
	Limit    int    `json:"limit,omitempty"` // default 10
}

// GetReadingProfileInput for the profile_get tool
type GetReadingProfileInput struct {
	// No input needed
}

// EnrichHistoryInput for the enrichment_enrich_history tool
type EnrichHistoryInput struct {
	Force bool `json:"force,omitempty"` // Re-enrich even if already enriched
}

// ===== Recommendation Tool Handlers =====

// Ensure these services are initialized in the server
func (s *Server) initRecommendationServices() error {
	if s.historyStore == nil {
		return fmt.Errorf("history store not available")
	}

	// Get the database from the history store
	db, err := s.getHistoryDB()
	if err != nil {
		return err
	}

	// Initialize enrichment service
	if s.enrichmentService == nil {
		olEnricher := enrichment.NewOpenLibraryEnricher("", "")
		gbEnricher := enrichment.NewGoogleBooksEnricher("") // API key optional

		// Create Hardcover enricher from provider
		var hcEnricher *enrichment.HardcoverEnricher
		if s.hardcover != nil {
			// Extract API key and endpoint from config for enricher
			hcEnricher = enrichment.NewHardcoverEnricherDirect(
				s.cfg.Providers.Hardcover.Endpoint,
				s.cfg.Providers.Hardcover.APIKey,
			)
		}

		s.enrichmentService = enrichment.NewService(db, olEnricher, gbEnricher, s.hardcover, hcEnricher)
	}

	// Initialize graph builder
	if s.graphBuilder == nil {
		s.graphBuilder = graph.NewBuilder(db)
	}

	// Initialize profile service
	if s.profileService == nil {
		s.profileService = analytics.NewComputeService(db)
	}

	return nil
}

// getHistoryDB extracts the database from the history store
func (s *Server) getHistoryDB() (*sql.DB, error) {
	dbPath := filepath.Join(s.DataDir, "history.db")
	return sql.Open("sqlite3", dbPath)
}

func (s *Server) handleGetBookWithAnalysis(ctx context.Context, req *mcp.CallToolRequest, input GetBookWithAnalysisInput) (*mcp.CallToolResult, any, error) {
	if err := s.initRecommendationServices(); err != nil {
		return nil, nil, fmt.Errorf("initializing services: %w", err)
	}

	// Find the history entry by title and author
	historyID, title, author, err := s.findHistoryEntry(input.Title, input.Author)
	if err != nil {
		return nil, nil, fmt.Errorf("finding book: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📖 %s by %s\n\n", title, author))

	// Get enrichment data
	enrichment, err := s.enrichmentService.GetEnrichment(historyID)
	if err == nil && enrichment != nil {
		sb.WriteString("📚 Enriched Details:\n")

		if enrichment.Description != "" {
			// Truncate long descriptions
			desc := enrichment.Description
			if len(desc) > 500 {
				desc = desc[:497] + "..."
			}
			sb.WriteString(fmt.Sprintf("   Description: %s\n\n", desc))
		}

		if len(enrichment.Themes) > 0 {
			sb.WriteString(fmt.Sprintf("   Themes: %s\n", strings.Join(enrichment.Themes, ", ")))
		}

		if len(enrichment.Topics) > 0 {
			sb.WriteString(fmt.Sprintf("   Topics: %s\n", strings.Join(enrichment.Topics, ", ")))
		}

		if len(enrichment.Mood) > 0 {
			sb.WriteString(fmt.Sprintf("   Mood: %s\n", strings.Join(enrichment.Mood, ", ")))
		}

		if enrichment.Complexity != "" {
			sb.WriteString(fmt.Sprintf("   Complexity: %s\n", enrichment.Complexity))
		}

		if enrichment.SeriesName != "" {
			sb.WriteString(fmt.Sprintf("   Series: %s", enrichment.SeriesName))
			if enrichment.SeriesPosition > 0 {
				sb.WriteString(fmt.Sprintf(" (#%.0f", enrichment.SeriesPosition))
				if enrichment.SeriesTotal > 0 {
					sb.WriteString(fmt.Sprintf(" of %d", enrichment.SeriesTotal))
				}
				sb.WriteString(")")
			}
			sb.WriteString("\n")
		}

		sb.WriteString("\n")
	}

	// Get relationships
	relatedBooks, err := s.graphBuilder.GetRelatedBooks(ctx, historyID, "", 5)
	if err == nil && len(relatedBooks) > 0 {
		sb.WriteString("🔗 Related Books:\n")
		for _, rb := range relatedBooks {
			relDesc := ""
			switch rb.RelationshipType {
			case models.RelSameAuthor:
				relDesc = "Same author"
			case models.RelSameSeries:
				relDesc = "Same series"
			case models.RelAlsoRead:
				relDesc = "Also read around this time"
			default:
				relDesc = rb.RelationshipType
			}
			sb.WriteString(fmt.Sprintf("   • %s by %s (%s, %.0f%% match)\n", rb.Title, rb.Author, relDesc, rb.Strength*100))
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: sb.String()},
		},
	}, map[string]any{
		"title":  title,
		"author": author,
	}, nil
}

func (s *Server) handleFindSimilarBooks(ctx context.Context, req *mcp.CallToolRequest, input FindSimilarBooksInput) (*mcp.CallToolResult, any, error) {
	if err := s.initRecommendationServices(); err != nil {
		return nil, nil, fmt.Errorf("initializing services: %w", err)
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}

	var enrichment *models.BookEnrichment
	var title, author string
	var fromExternalSource bool

	// Try to find the book in local history first
	historyID, t, a, err := s.findHistoryEntry(input.Title, input.Author)
	if err == nil {
		// Found in history, get its enrichment data
		title, author = t, a
		enrichment, err = s.enrichmentService.GetEnrichment(historyID)
		if err != nil {
			return nil, nil, fmt.Errorf("getting enrichment: %w", err)
		}
	} else {
		// Not in history - query external sources for metadata
		title, author = input.Title, input.Author
		fromExternalSource = true

		// Enrich from external sources (Hardcover, Open Library, Google Books)
		// Use historyID=0 as a temporary marker (won't be saved to DB)
		enrichment, err = s.enrichmentService.EnrichBook(ctx, 0, title, author, "")
		if err != nil {
			return nil, nil, fmt.Errorf("enriching book from external sources: %w\n\n"+
				"The book '%s' by %s was not found in your reading history,\n"+
				"and we couldn't find enrichment data from external sources.\n\n"+
				"Try:\n"+
				"1. Import your Libby reading history if you've read this book\n"+
				"2. Search for a different book that's in your history\n"+
				"3. Check the title and author spelling", err, title, author)
		}
	}

	// Find similar books
	similar, err := s.enrichmentService.FindSimilar(enrichment, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("finding similar books: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📚 Books Similar to \"%s\" by %s\n", title, author))
	if fromExternalSource {
		sb.WriteString("   (enriched from external sources)\n")
	}
	sb.WriteString("\n")

	if len(similar) == 0 {
		sb.WriteString("No similar books found. Try enriching more history entries first.\n")
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: sb.String()},
			},
		}, map[string]any{"found": 0}, nil
	}

	for i, book := range similar {
		sb.WriteString(fmt.Sprintf("%d. %s by %s\n", i+1, book.Title, book.Author))

		// Show why it's similar
		reasons := []string{}
		if book.SeriesName != "" && enrichment.SeriesName == book.SeriesName {
			reasons = append(reasons, "same series")
		}

		// Count shared themes
		sharedThemes := countShared(enrichment.Themes, book.Themes)
		if sharedThemes > 0 {
			reasons = append(reasons, fmt.Sprintf("%d shared themes", sharedThemes))
		}

		// Count shared topics
		sharedTopics := countShared(enrichment.Topics, book.Topics)
		if sharedTopics > 0 {
			reasons = append(reasons, fmt.Sprintf("%d shared topics", sharedTopics))
		}

		// Count shared mood
		sharedMood := countShared(enrichment.Mood, book.Mood)
		if sharedMood > 0 {
			reasons = append(reasons, "similar mood")
		}

		if len(reasons) > 0 {
			sb.WriteString(fmt.Sprintf("   (%s)\n", strings.Join(reasons, ", ")))
		}

		// Show key themes
		if len(book.Themes) > 0 {
			sb.WriteString(fmt.Sprintf("   Themes: %s\n", strings.Join(book.Themes[:min(3, len(book.Themes))], ", ")))
		}

		sb.WriteString("\n")
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: sb.String()},
		},
	}, map[string]any{
		"source_title":  title,
		"source_author": author,
		"found":         len(similar),
	}, nil
}

func (s *Server) handleGetReadingProfile(ctx context.Context, req *mcp.CallToolRequest, input GetReadingProfileInput) (*mcp.CallToolResult, any, error) {
	if err := s.initRecommendationServices(); err != nil {
		return nil, nil, fmt.Errorf("initializing services: %w", err)
	}

	// Check if profile exists, compute if not
	profile, err := s.profileService.GetProfile(ctx)
	if err != nil {
		// No profile exists, compute one
		profile, err = s.profileService.ComputeProfile(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("computing profile: %w", err)
		}
	}

	summary, err := s.profileService.GetProfileSummary(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("generating profile summary: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: summary},
		},
	}, map[string]any{
		"computed_at": profile.ComputedAt.Format(time.RFC3339),
	}, nil
}

// EnrichmentEntry represents a book to be enriched
type EnrichmentEntry struct {
	ID     int
	Title  string
	Author string
	ISBN   string
}

func (s *Server) handleEnrichHistory(ctx context.Context, req *mcp.CallToolRequest, input EnrichHistoryInput) (*mcp.CallToolResult, any, error) {
	if err := s.initRecommendationServices(); err != nil {
		return nil, nil, fmt.Errorf("initializing services: %w", err)
	}

	// Start enrichment job in background (processes full library, no limit)
	job, err := s.enrichmentService.EnrichHistoryBackground(ctx, input.Force)
	if err != nil {
		return nil, nil, fmt.Errorf("starting enrichment: %w", err)
	}

	// Start MCP notification sender in background
	go s.sendEnrichmentNotifications(context.Background(), req.Session, job)

	progress := job.GetProgress()
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("🔄 Enrichment started in background\nJob ID: %s\nBooks to enrich: %d\n\nYou'll receive progress notifications.\nUse enrichment_status tool to check detailed progress.", job.ID, progress.TotalBooks)},
		},
	}, map[string]any{
		"job_id":      job.ID,
		"total_books": progress.TotalBooks,
		"status":      string(progress.Status),
	}, nil
}

// EnrichmentStatusInput defines input for enrichment_status tool
type EnrichmentStatusInput struct {
	JobID string `json:"job_id,omitempty"`
}

func (s *Server) handleEnrichmentStatus(ctx context.Context, req *mcp.CallToolRequest, input EnrichmentStatusInput) (*mcp.CallToolResult, any, error) {
	if err := s.initRecommendationServices(); err != nil {
		return nil, nil, fmt.Errorf("initializing services: %w", err)
	}

	var progress *enrichment.JobProgress
	var err error

	if input.JobID != "" {
		progress, err = s.enrichmentService.GetJobProgress(input.JobID)
	} else {
		progress, err = s.enrichmentService.GetCurrentJobProgress()
	}

	if err != nil {
		return nil, nil, fmt.Errorf("getting job progress: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📊 Job %s\n\n", progress.JobID))
	sb.WriteString(fmt.Sprintf("Status: %s\n", progress.Status))
	sb.WriteString(fmt.Sprintf("Progress: %d/%d books (%.1f%%)\n",
		progress.ProcessedBooks, progress.TotalBooks,
		float64(progress.ProcessedBooks)/float64(progress.TotalBooks)*100))
	sb.WriteString(fmt.Sprintf("✅ Successful: %d\n", progress.SuccessfulBooks))
	sb.WriteString(fmt.Sprintf("❌ Failed: %d\n", progress.FailedBooks))

	if progress.CurrentBook != "" {
		sb.WriteString(fmt.Sprintf("\nCurrently processing: %s by %s\n", progress.CurrentBook, progress.CurrentAuthor))
	}

	sb.WriteString(fmt.Sprintf("\nElapsed: %dm %ds\n", progress.ElapsedSeconds/60, progress.ElapsedSeconds%60))
	if progress.EstimatedSeconds > 0 {
		sb.WriteString(fmt.Sprintf("Estimated remaining: %dm %ds\n",
			progress.EstimatedSeconds/60, progress.EstimatedSeconds%60))
		sb.WriteString(fmt.Sprintf("Average: %.1fs per book\n", progress.AvgSecondsPerBook))
	}

	if len(progress.RecentErrors) > 0 {
		sb.WriteString("\nRecent errors:\n")
		for _, err := range progress.RecentErrors {
			sb.WriteString(fmt.Sprintf("  • %s\n", err))
		}
	}

	if progress.Error != "" {
		sb.WriteString(fmt.Sprintf("\n❌ Error: %s\n", progress.Error))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: sb.String()},
		},
	}, map[string]any{
		"job_id":           progress.JobID,
		"status":           string(progress.Status),
		"total_books":      progress.TotalBooks,
		"processed_books":  progress.ProcessedBooks,
		"successful_books": progress.SuccessfulBooks,
		"failed_books":     progress.FailedBooks,
		"elapsed_seconds":  progress.ElapsedSeconds,
	}, nil
}

// EnrichmentCancelInput defines input for enrichment_cancel tool
type EnrichmentCancelInput struct {
	JobID string `json:"job_id"`
}

func (s *Server) handleEnrichmentCancel(ctx context.Context, req *mcp.CallToolRequest, input EnrichmentCancelInput) (*mcp.CallToolResult, any, error) {
	if err := s.initRecommendationServices(); err != nil {
		return nil, nil, fmt.Errorf("initializing services: %w", err)
	}

	if err := s.enrichmentService.CancelJob(input.JobID); err != nil {
		return nil, nil, fmt.Errorf("cancelling job: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("🚫 Job %s cancelled", input.JobID)},
		},
	}, map[string]any{"job_id": input.JobID, "status": "cancelled"}, nil
}

// sendEnrichmentNotifications monitors a job and sends MCP progress notifications
func (s *Server) sendEnrichmentNotifications(ctx context.Context, session *mcp.ServerSession, job *enrichment.Job) {
	ticker := time.NewTicker(5 * time.Second) // Check every 5 seconds
	defer ticker.Stop()

	lastProgress := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			progress := job.GetProgress()

			// Send progress notification if changed
			if progress.ProcessedBooks > lastProgress || progress.Status != enrichment.JobStatusRunning {
				percentComplete := 0.0
				if progress.TotalBooks > 0 {
					percentComplete = float64(progress.ProcessedBooks) / float64(progress.TotalBooks) * 100
				}

				message := fmt.Sprintf("Enriching: %s by %s (%d/%d) - %.1f%%",
					progress.CurrentBook, progress.CurrentAuthor,
					progress.ProcessedBooks, progress.TotalBooks, percentComplete)

				if progress.EstimatedSeconds > 0 {
					etaMins := progress.EstimatedSeconds / 60
					message += fmt.Sprintf(" - ETA: %dm", etaMins)
				}

				session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
					ProgressToken: progress.JobID,
					Progress:      percentComplete,
					Total:         float64(progress.TotalBooks),
					Message:       message,
				})

				lastProgress = progress.ProcessedBooks
			}

			// Check if job is complete
			if progress.Status == enrichment.JobStatusCompleted ||
				progress.Status == enrichment.JobStatusFailed ||
				progress.Status == enrichment.JobStatusCancelled {

				// Send final notification
				var finalMsg string
				switch progress.Status {
				case enrichment.JobStatusCompleted:
					finalMsg = fmt.Sprintf("✅ Enrichment complete: %d succeeded, %d failed",
						progress.SuccessfulBooks, progress.FailedBooks)
				case enrichment.JobStatusFailed:
					finalMsg = fmt.Sprintf("❌ Enrichment failed: %s", progress.Error)
				case enrichment.JobStatusCancelled:
					finalMsg = "🚫 Enrichment cancelled"
				}

				session.Log(ctx, &mcp.LoggingMessageParams{
					Level: "info",
					Data:  finalMsg,
				})

				// Log recent errors if any
				if len(progress.RecentErrors) > 0 {
					var sb strings.Builder
					sb.WriteString("Recent errors:\n")
					for _, err := range progress.RecentErrors {
						sb.WriteString(fmt.Sprintf("  • %s\n", err))
					}
					session.Log(ctx, &mcp.LoggingMessageParams{
						Level: "warn",
						Data:  sb.String(),
					})
				}

				return
			}
		}
	}
}

// Helper functions

func (s *Server) findHistoryEntry(title, author string) (int, string, string, error) {
	db, err := s.getHistoryDB()
	if err != nil {
		return 0, "", "", err
	}

	query := `
		SELECT id, title, author
		FROM history
		WHERE title LIKE ? AND author LIKE ?
		ORDER BY timestamp DESC
		LIMIT 1
	`

	var id int
	var t, a string
	err = db.QueryRow(query, "%"+title+"%", "%"+author+"%").Scan(&id, &t, &a)
	if err != nil {
		return 0, "", "", fmt.Errorf("book not found: %w", err)
	}

	return id, t, a, nil
}

func countShared(a, b []string) int {
	set := make(map[string]bool)
	for _, s := range a {
		set[s] = true
	}

	count := 0
	for _, s := range b {
		if set[s] {
			count++
		}
	}
	return count
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
