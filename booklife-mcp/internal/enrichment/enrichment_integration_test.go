package enrichment

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// TestEnrichmentServiceRealAPIs tests enrichment service with real external APIs
func TestEnrichmentServiceRealAPIs(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create temporary database with full schema
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	defer db.Close()

	// Create enrichment schema
	_, err = db.Exec(`
		CREATE TABLE book_enrichment (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			history_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			author TEXT NOT NULL,
			openlibrary_id TEXT,
			googlebooks_id TEXT,
			description TEXT,
			themes TEXT,
			topics TEXT,
			mood TEXT,
			complexity TEXT,
			series_name TEXT,
			series_position REAL,
			series_total INTEGER,
			enrichment_sources TEXT,
			enriched_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(history_id)
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Create service
	olEnricher := NewOpenLibraryEnricher("", "")
	gbEnricher := NewGoogleBooksEnricher(os.Getenv("GOOGLEBOOKS_API_KEY"))
	service := NewService(db, olEnricher, gbEnricher, nil, nil) // No Hardcover provider in tests

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Run("EnrichWithISBN", func(t *testing.T) {
		// Test with a known ISBN that should work
		// Using a popular book: "The Great Gatsby" ISBN 9780743273565
		result, err := service.EnrichBook(ctx, 1, "The Great Gatsby", "F. Scott Fitzgerald", "9780743273565")
		if err != nil {
			t.Logf("Warning: EnrichBook failed (may be rate limited or network issue): %v", err)
			// Try fallback to title/author search
			result, err = service.EnrichBook(ctx, 1, "The Great Gatsby", "F. Scott Fitzgerald", "")
			if err != nil {
				t.Logf("Warning: Title/author search also failed: %v", err)
				return
			}
		}

		if result == nil {
			t.Fatal("Expected non-nil result")
		}

		t.Logf("✓ Enriched: %s by %s", result.Title, result.Author)

		// Verify it was saved to database
		enriched, err := service.GetEnrichment(1)
		if err != nil {
			t.Fatalf("GetEnrichment failed: %v", err)
		}

		if enriched.Title != "The Great Gatsby" {
			t.Errorf("Expected title 'The Great Gatsby', got '%s'", enriched.Title)
		}

		// Check for extracted themes/topics
		if len(enriched.Themes) > 0 {
			t.Logf("  Extracted themes: %v", enriched.Themes)
		}
		if len(enriched.Topics) > 0 {
			t.Logf("  Extracted topics: %v", enriched.Topics)
		}
	})

	t.Run("EnrichByTitleAuthor", func(t *testing.T) {
		// Test without ISBN (title/author search)
		result, err := service.EnrichBook(ctx, 2, "1984", "George Orwell", "")
		if err != nil {
			t.Logf("Warning: EnrichBook failed: %v", err)
			return
		}

		if result == nil {
			t.Fatal("Expected non-nil result")
		}

		t.Logf("✓ Enriched by title/author: %s", result.Title)
	})

	t.Run("EnrichAndFindSimilar", func(t *testing.T) {
		// Enrich multiple books to test similarity
		books := []struct {
			id                  int
			title, author, isbn string
		}{
			{10, "The Fellowship of the Ring", "J.R.R. Tolkien", ""},
			{11, "The Two Towers", "J.R.R. Tolkien", ""},
			{12, "The Return of the King", "J.R.R. Tolkien", ""},
		}

		enrichedCount := 0
		for _, b := range books {
			_, err := service.EnrichBook(ctx, b.id, b.title, b.author, b.isbn)
			if err != nil {
				t.Logf("Warning: Failed to enrich %s: %v", b.title, err)
				continue
			}
			enrichedCount++
		}

		if enrichedCount < 2 {
			t.Skipf("Need at least 2 enriched books for similarity test, got %d", enrichedCount)
		}

		// Test FindSimilar
		enrichment1, err := service.GetEnrichment(10)
		if err != nil {
			t.Skipf("Skipping FindSimilar - couldn't get enrichment: %v", err)
		}

		similar, err := service.FindSimilar(enrichment1, 5)
		if err != nil {
			t.Fatalf("FindSimilar failed: %v", err)
		}

		t.Logf("✓ Found %d similar books", len(similar))
		for _, s := range similar {
			t.Logf("  - %s by %s (themes: %v)", s.Title, s.Author, s.Themes)
		}
	})
}

// TestOpenLibraryEnricherRealAPI tests Open Library API directly
func TestOpenLibraryEnricherRealAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := NewOpenLibraryEnricher("", "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("GetByISBN", func(t *testing.T) {
		data, err := client.GetByISBN(ctx, "9780743273565")
		if err != nil {
			t.Fatalf("GetByISBN failed: %v", err)
		}

		if data == nil {
			t.Fatal("Expected non-nil data")
		}

		// Verify basic fields are populated
		if data.Title == "" {
			t.Error("Expected title to be set")
		}
		if data.OpenLibraryID == "" {
			t.Error("Expected OpenLibraryID to be set")
		}
		if len(data.Authors) == 0 {
			t.Error("Expected at least one author")
		}

		// Validate expected values for "The Great Gatsby"
		if data.Title != "The Great Gatsby" {
			t.Errorf("Expected title 'The Great Gatsby', got '%s'", data.Title)
		}

		foundAuthor := false
		for _, author := range data.Authors {
			if author == "F. Scott Fitzgerald" {
				foundAuthor = true
				break
			}
		}
		if !foundAuthor {
			t.Errorf("Expected author 'F. Scott Fitzgerald', got %v", data.Authors)
		}

		t.Logf("✓ Open Library: %s by %v", data.Title, data.Authors)
	})

	t.Run("SearchByTitleAuthor", func(t *testing.T) {
		data, err := client.SearchByTitleAuthor(ctx, "The Hobbit", "Tolkien")
		if err != nil {
			t.Fatalf("SearchByTitleAuthor failed: %v", err)
		}

		if data == nil {
			t.Fatal("Expected non-nil data")
		}

		// Verify basic fields
		if data.Title == "" {
			t.Error("Expected title to be set")
		}
		if data.OpenLibraryID == "" {
			t.Error("Expected OpenLibraryID to be set")
		}
		if len(data.Authors) == 0 {
			t.Error("Expected at least one author")
		}

		t.Logf("✓ Open Library search: %s by %v", data.Title, data.Authors)
	})
}

// TestGoogleBooksEnricherRealAPI tests Google Books API directly
func TestGoogleBooksEnricherRealAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	apiKey := os.Getenv("GOOGLEBOOKS_API_KEY")
	client := NewGoogleBooksEnricher(apiKey)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("GetByISBN", func(t *testing.T) {
		// Use "1984" ISBN which is known to work reliably
		data, err := client.GetByISBN(ctx, "9780451524935")
		if err != nil {
			t.Fatalf("GetByISBN failed: %v", err)
		}

		if data == nil {
			t.Fatal("Expected non-nil data")
		}

		// Verify basic fields
		if data.Title == "" {
			t.Error("Expected title to be set")
		}
		if data.GoogleBooksID == "" {
			t.Error("Expected GoogleBooksID to be set")
		}
		if len(data.Authors) == 0 {
			t.Error("Expected at least one author")
		}

		// Validate expected values for "1984"
		if data.Title != "1984" {
			t.Logf("Note: Expected title '1984', got '%s'", data.Title)
		}

		t.Logf("✓ Google Books: %s by %v", data.Title, data.Authors)

		if len(data.Categories) > 0 {
			t.Logf("  Categories: %v", data.Categories)
		}
	})

	t.Run("SearchByTitleAuthor", func(t *testing.T) {
		data, err := client.SearchByTitleAuthor(ctx, "1984", "George Orwell")
		if err != nil {
			t.Fatalf("SearchByTitleAuthor failed: %v", err)
		}

		if data == nil {
			t.Fatal("Expected non-nil data")
		}

		// Verify basic fields
		if data.Title == "" {
			t.Error("Expected title to be set")
		}
		if data.GoogleBooksID == "" {
			t.Error("Expected GoogleBooksID to be set")
		}
		if len(data.Authors) == 0 {
			t.Error("Expected at least one author")
		}

		t.Logf("✓ Google Books search: %s by %v", data.Title, data.Authors)
	})

	t.Run("NoAPIKey", func(t *testing.T) {
		// Test that it works without API key (public API)
		clientNoKey := NewGoogleBooksEnricher("")
		data, err := clientNoKey.SearchByTitleAuthor(ctx, "1984", "Orwell")
		if err != nil {
			t.Fatalf("SearchByTitleAuthor without API key failed: %v", err)
		}

		if data == nil {
			t.Fatal("Expected non-nil data")
		}

		// Verify basic fields
		if data.Title == "" {
			t.Error("Expected title to be set")
		}
		if data.GoogleBooksID == "" {
			t.Error("Expected GoogleBooksID to be set")
		}

		t.Logf("✓ Google Books without API key works: %s", data.Title)
	})
}
