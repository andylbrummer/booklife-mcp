package enrichment

import (
	"context"
	"testing"
	"time"
)

// TestOpenLibraryGetByISBN tests real Open Library API with a known ISBN
func TestOpenLibraryGetByISBN(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := NewOpenLibraryEnricher("", "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test with a well-known book: "The Great Gatsby" ISBN 9780743273565
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
	// Verify each author is non-empty
	for i, author := range data.Authors {
		if author == "" {
			t.Errorf("Author at index %d is empty", i)
		}
	}

	// Validate expected values for "The Great Gatsby"
	expectedTitle := "The Great Gatsby"
	if data.Title != expectedTitle {
		t.Errorf("Expected title '%s', got '%s'", expectedTitle, data.Title)
	}

	// Verify author contains "F. Scott Fitzgerald"
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

	t.Logf("✓ Found: %s by %v (OL: %s)", data.Title, data.Authors, data.OpenLibraryID)
}

// TestOpenLibrarySearchByTitleAuthor tests real Open Library search API
func TestOpenLibrarySearchByTitleAuthor(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := NewOpenLibraryEnricher("", "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test with a well-known book
	data, err := client.SearchByTitleAuthor(ctx, "The Hobbit", "J.R.R. Tolkien")
	if err != nil {
		t.Fatalf("SearchByTitleAuthor failed: %v", err)
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
	// Verify each author is non-empty
	for i, author := range data.Authors {
		if author == "" {
			t.Errorf("Author at index %d is empty", i)
		}
	}

	// Validate expected values for "The Hobbit"
	expectedTitle := "The Hobbit"
	if data.Title != expectedTitle {
		t.Logf("Note: Expected title '%s', got '%s' (search may return related works)", expectedTitle, data.Title)
	}

	t.Logf("✓ Found: %s by %v (OL: %s)", data.Title, data.Authors, data.OpenLibraryID)
}

// TestOpenLibraryGetDescription tests fetching description from works API
func TestOpenLibraryGetDescription(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := NewOpenLibraryEnricher("", "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test with a known work ID: "The Hobbit" has OL works ID /works/OL2745191W
	desc, err := client.GetDescription(ctx, "/works/OL2745191W")
	if err != nil {
		t.Fatalf("GetDescription failed: %v", err)
	}

	if desc == "" {
		t.Log("Warning: No description found (may be expected for some works)")
	} else {
		t.Logf("✓ Description: %s", desc[:min(100, len(desc))])
	}
}

// TestOpenLibraryClientCreation tests that the enricher is properly configured
func TestOpenLibraryClientCreation(t *testing.T) {
	// Test that client can be created with different base URLs
	client1 := NewOpenLibraryEnricher("", "")
	if client1 == nil {
		t.Fatal("Expected non-nil client")
	}

	// Client with custom base URL
	client2 := NewOpenLibraryEnricher("https://openlibrary.org", "")
	if client2 == nil {
		t.Fatal("Expected non-nil client with custom base URL")
	}

	t.Log("✓ OpenLibrary enricher creation works correctly")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
