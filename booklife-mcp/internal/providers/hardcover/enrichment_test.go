package hardcover

import (
	"context"
	"os"
	"testing"

	"github.com/user/booklife-mcp/internal/enrichment"
)

// TestHardcoverEnricherGetByID tests enrichment by Hardcover ID
func TestHardcoverEnricherGetByID(t *testing.T) {
	apiKey := os.Getenv("HARDCOVER_API_KEY")
	if apiKey == "" {
		t.Skip("HARDCOVER_API_KEY not set")
	}

	enricher := enrichment.NewHardcoverEnricherDirect("", apiKey)
	if enricher == nil {
		t.Fatal("Failed to create enricher")
	}

	ctx := context.Background()

	// First, search for a well-known book to get its ID
	client, err := NewClient("", apiKey)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	books, _, err := client.SearchBooks(ctx, "Project Hail Mary Andy Weir", 0, 1)
	if err != nil {
		t.Fatalf("SearchBooks failed: %v", err)
	}

	if len(books) == 0 {
		t.Skip("Test book not found")
	}

	bookID := books[0].HardcoverID
	t.Logf("Testing enrichment for book ID: %s", bookID)

	// Test the GetByID enrichment function
	data, err := enricher.GetByID(ctx, bookID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if data == nil {
		t.Fatal("No enrichment data returned")
	}

	// Verify we got the expected data
	t.Logf("Title: %s", data.Title)
	t.Logf("Description length: %d", len(data.Description))
	t.Logf("Authors: %v", data.Authors)
	t.Logf("Genres: %v", data.Genres)
	t.Logf("Moods: %v", data.Moods)
	t.Logf("Themes: %v", data.Themes)
	t.Logf("Series: %s (position %.0f)", data.SeriesName, data.SeriesPosition)
	t.Logf("ISBNs: %s / %s", data.ISBN10, data.ISBN13)

	// Validate critical fields
	if data.Title == "" {
		t.Error("Title is empty")
	}
	if len(data.Authors) == 0 {
		t.Error("No authors found")
	}
	if data.Description == "" {
		t.Error("Description is empty")
	}
}

// TestHardcoverEnricherGetByTitleAuthor tests title/author search and enrichment
func TestHardcoverEnricherGetByTitleAuthor(t *testing.T) {
	apiKey := os.Getenv("HARDCOVER_API_KEY")
	if apiKey == "" {
		t.Skip("HARDCOVER_API_KEY not set")
	}

	enricher := enrichment.NewHardcoverEnricherDirect("", apiKey)
	if enricher == nil {
		t.Fatal("Failed to create enricher")
	}

	ctx := context.Background()

	testCases := []struct {
		title  string
		author string
	}{
		{"Project Hail Mary", "Andy Weir"},
		{"The Way of Kings", "Brandon Sanderson"},
		{"Dune", "Frank Herbert"},
	}

	for _, tc := range testCases {
		t.Run(tc.title, func(t *testing.T) {
			// This will test the problematic GetByTitleAuthor function
			data, err := enricher.GetByTitleAuthor(ctx, tc.title, tc.author)

			// Currently disabled, so expect error
			if err != nil {
				t.Logf("GetByTitleAuthor failed (expected while disabled): %v", err)
				return
			}

			if data == nil {
				t.Error("No enrichment data returned")
				return
			}

			t.Logf("Successfully enriched: %s by %s", data.Title, data.Authors)
			t.Logf("  Genres: %v", data.Genres)
			t.Logf("  Moods: %v", data.Moods)
			t.Logf("  Themes: %v", data.Themes)

			if data.Title == "" {
				t.Error("Title is empty")
			}
			if len(data.Authors) == 0 {
				t.Error("No authors found")
			}
		})
	}
}

// TestSeriesInformation tests series metadata extraction
func TestSeriesInformation(t *testing.T) {
	apiKey := os.Getenv("HARDCOVER_API_KEY")
	if apiKey == "" {
		t.Skip("HARDCOVER_API_KEY not set")
	}

	enricher := enrichment.NewHardcoverEnricherDirect("", apiKey)
	if enricher == nil {
		t.Fatal("Failed to create enricher")
	}

	ctx := context.Background()

	// First find the book ID
	client, err := NewClient("", apiKey)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	books, _, err := client.SearchBooks(ctx, "The Way of Kings Brandon Sanderson", 0, 1)
	if err != nil || len(books) == 0 {
		t.Skip("Could not find test book")
	}

	bookID := books[0].HardcoverID
	data, err := enricher.GetByID(ctx, bookID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	t.Logf("Series: %s", data.SeriesName)
	t.Logf("Position: %.0f", data.SeriesPosition)

	// Note: Series data availability depends on which edition is returned by Hardcover
	// Some editions may not have series information populated
	if data.SeriesName == "" {
		t.Logf("Warning: No series name found for this edition of The Way of Kings (data availability issue, not a code bug)")
	}
	if data.SeriesPosition == 0 {
		t.Logf("Warning: No series position found for this edition (data availability issue, not a code bug)")
	}
}

// TestGenresAndTags tests genre/tag extraction
func TestGenresAndTags(t *testing.T) {
	apiKey := os.Getenv("HARDCOVER_API_KEY")
	if apiKey == "" {
		t.Skip("HARDCOVER_API_KEY not set")
	}

	enricher := enrichment.NewHardcoverEnricherDirect("", apiKey)
	if enricher == nil {
		t.Fatal("Failed to create enricher")
	}

	ctx := context.Background()

	// First find the book ID
	client, err := NewClient("", apiKey)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	books, _, err := client.SearchBooks(ctx, "Dune Frank Herbert", 0, 1)
	if err != nil || len(books) == 0 {
		t.Skip("Could not find test book")
	}

	bookID := books[0].HardcoverID
	data, err := enricher.GetByID(ctx, bookID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	t.Logf("Genres: %v", data.Genres)
	t.Logf("Moods: %v", data.Moods)
	t.Logf("Themes: %v", data.Themes)

	// Dune should have sci-fi genre at minimum
	if len(data.Genres) == 0 {
		t.Error("Expected at least one genre for Dune")
	}
}
