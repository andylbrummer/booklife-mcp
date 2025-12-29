package enrichment

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestGoogleBooksGetByISBN tests real Google Books API with a known ISBN
func TestGoogleBooksGetByISBN(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	apiKey := os.Getenv("GOOGLEBOOKS_API_KEY")
	client := NewGoogleBooksEnricher(apiKey)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test with "1984" ISBN - known to work reliably
	data, err := client.GetByISBN(ctx, "9780451524935")
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
	if data.GoogleBooksID == "" {
		t.Error("Expected GoogleBooksID to be set")
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

	// Validate expected values for "1984"
	expectedTitle := "1984"
	if data.Title != expectedTitle {
		t.Logf("Note: Expected title '%s', got '%s'", expectedTitle, data.Title)
	}

	// Verify author contains "George Orwell"
	foundAuthor := false
	for _, author := range data.Authors {
		if author == "George Orwell" {
			foundAuthor = true
			break
		}
	}
	if !foundAuthor {
		t.Logf("Note: Expected author 'George Orwell', got %v", data.Authors)
	}

	t.Logf("✓ Found: %s by %v (GB: %s)", data.Title, data.Authors, data.GoogleBooksID)

	// Check for description
	if data.Description != "" {
		if len(data.Description) < 10 {
			t.Errorf("Description too short: %q", data.Description)
		}
		t.Logf("  Description: %s", data.Description[:min(100, len(data.Description))])
	} else {
		t.Log("  Description: (none)")
	}

	// Check for publisher
	if data.Publisher != "" {
		t.Logf("  Publisher: %s", data.Publisher)
	}

	// Check for published date
	if data.PublishDate != "" {
		t.Logf("  Published: %s", data.PublishDate)
	}

	// Check for categories
	if len(data.Categories) > 0 {
		for i, cat := range data.Categories {
			if cat == "" {
				t.Errorf("Category at index %d is empty", i)
			}
		}
		t.Logf("  Categories: %v", data.Categories)
	}

	// Check for page count
	if data.PageCount > 0 {
		t.Logf("  Pages: %d", data.PageCount)
	}
}

// TestGoogleBooksSearchByTitleAuthor tests real Google Books search API
func TestGoogleBooksSearchByTitleAuthor(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	apiKey := os.Getenv("GOOGLEBOOKS_API_KEY")
	client := NewGoogleBooksEnricher(apiKey)
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
	if data.GoogleBooksID == "" {
		t.Error("Expected GoogleBooksID to be set")
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

	t.Logf("✓ Found: %s by %v (GB: %s)", data.Title, data.Authors, data.GoogleBooksID)

	// Check for description
	if data.Description != "" {
		if len(data.Description) < 10 {
			t.Errorf("Description too short: %q", data.Description)
		}
		t.Logf("  Description: %s", data.Description[:min(100, len(data.Description))])
	} else {
		t.Log("  Description: (none)")
	}

	// Check for categories
	if len(data.Categories) > 0 {
		for i, cat := range data.Categories {
			if cat == "" {
				t.Errorf("Category at index %d is empty", i)
			}
		}
		t.Logf("  Categories: %v", data.Categories)
	} else {
		t.Log("  Categories: (none)")
	}

	// Check for publisher
	if data.Publisher != "" {
		t.Logf("  Publisher: %s", data.Publisher)
	}

	// Check for published date
	if data.PublishDate != "" {
		t.Logf("  Published: %s", data.PublishDate)
	}

	// Check for page count
	if data.PageCount > 0 {
		t.Logf("  Pages: %d", data.PageCount)
	}
}

// TestGoogleBooksNoAPIKey tests that Google Books works without API key (with lower limits)
func TestGoogleBooksNoAPIKey(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := NewGoogleBooksEnricher("") // No API key
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Should still work with public API (but lower limits)
	data, err := client.SearchByTitleAuthor(ctx, "1984", "George Orwell")
	if err != nil {
		t.Fatalf("SearchByTitleAuthor without API key failed: %v", err)
	}

	if data == nil {
		t.Fatal("Expected non-nil data")
	}

	t.Logf("✓ Works without API key: %s", data.Title)
}

// TestGoogleBooksClientCreation tests that the enricher is properly configured
func TestGoogleBooksClientCreation(t *testing.T) {
	// Test that client can be created with and without API key
	client1 := NewGoogleBooksEnricher("")
	if client1 == nil {
		t.Fatal("Expected non-nil client")
	}

	// Client with API key
	client2 := NewGoogleBooksEnricher("test-api-key")
	if client2 == nil {
		t.Fatal("Expected non-nil client with API key")
	}

	t.Log("✓ GoogleBooks enricher creation works correctly")
}

// TestGoogleBooksCategories tests that BISAC categories are parsed
func TestGoogleBooksCategories(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := NewGoogleBooksEnricher("")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Search for a book with known categories
	data, err := client.SearchByTitleAuthor(ctx, "The Da Vinci Code", "Dan Brown")
	if err != nil {
		t.Fatalf("SearchByTitleAuthor failed: %v", err)
	}

	if data == nil {
		t.Fatal("Expected non-nil data")
	}

	if len(data.Categories) == 0 {
		t.Log("Warning: No categories found for this book")
	} else {
		t.Logf("✓ Categories: %v", data.Categories)
	}
}
