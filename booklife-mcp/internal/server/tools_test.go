package server

import (
	"context"
	"errors"
	"testing"

	"github.com/user/booklife-mcp/internal/models"
	"github.com/user/booklife-mcp/internal/providers/mocks"
)

func TestHandleSearchBooks(t *testing.T) {
	t.Run("returns books from hardcover", func(t *testing.T) {
		mock := &mocks.MockHardcoverProvider{
			SearchBooksFunc: func(ctx context.Context, query string, offset, limit int) ([]models.Book, int, error) {
				return []models.Book{
					{Title: "Test Book", Authors: []models.Contributor{{Name: "Test Author", Role: "author"}}},
				}, 1, nil
			},
		}

		s := &Server{hardcover: mock}
		input := SearchBooksInput{
			Query:            "test",
			PaginationParams: PaginationParams{Page: 1, PageSize: 10},
		}

		result, data, err := s.handleSearchBooks(context.Background(), nil, input)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected result, got nil")
		}

		dataMap, ok := data.(map[string]any)
		if !ok {
			t.Fatal("expected data to be map[string]any")
		}
		books, ok := dataMap["books"].([]models.Book)
		if !ok {
			t.Fatal("expected 'books' field to be []models.Book")
		}
		if len(books) != 1 {
			t.Errorf("expected 1 book, got %d", len(books))
		}
		if books[0].Title != "Test Book" {
			t.Errorf("expected 'Test Book', got '%s'", books[0].Title)
		}
	})

	t.Run("returns empty when no provider configured", func(t *testing.T) {
		s := &Server{}
		input := SearchBooksInput{
			Query:            "test",
			PaginationParams: PaginationParams{Page: 1, PageSize: 10},
		}

		result, data, err := s.handleSearchBooks(context.Background(), nil, input)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected result, got nil")
		}

		dataMap, ok := data.(map[string]any)
		if !ok {
			t.Fatal("expected data to be map[string]any")
		}
		books, ok := dataMap["books"].([]models.Book)
		if !ok {
			t.Fatal("expected 'books' field to be []models.Book")
		}
		if len(books) != 0 {
			t.Errorf("expected 0 books, got %d", len(books))
		}
	})

	t.Run("requires query", func(t *testing.T) {
		s := &Server{}
		input := SearchBooksInput{
			Query:            "",
			PaginationParams: PaginationParams{Page: 1, PageSize: 10},
		}

		_, _, err := s.handleSearchBooks(context.Background(), nil, input)

		if err == nil {
			t.Fatal("expected error for empty query")
		}
	})

	t.Run("uses default page size when not specified", func(t *testing.T) {
		var capturedOffset, capturedLimit int
		mock := &mocks.MockHardcoverProvider{
			SearchBooksFunc: func(ctx context.Context, query string, offset, limit int) ([]models.Book, int, error) {
				capturedOffset = offset
				capturedLimit = limit
				return nil, 0, nil
			},
		}

		s := &Server{hardcover: mock}
		input := SearchBooksInput{Query: "test"} // No page size specified

		_, _, _ = s.handleSearchBooks(context.Background(), nil, input)

		if capturedOffset != 0 {
			t.Errorf("expected offset of 0, got %d", capturedOffset)
		}
		if capturedLimit != 20 {
			t.Errorf("expected default page size of 20, got %d", capturedLimit)
		}
	})
}

func TestHandleGetMyLibrary(t *testing.T) {
	t.Run("returns books from hardcover", func(t *testing.T) {
		mock := &mocks.MockHardcoverProvider{
			GetUserBooksFunc: func(ctx context.Context, status string, offset, limit int) ([]models.Book, int, error) {
				return []models.Book{
					{Title: "My Book", Authors: []models.Contributor{{Name: "Author", Role: "author"}}},
					{Title: "Another Book", Authors: []models.Contributor{{Name: "Author 2", Role: "author"}}},
				}, 2, nil
			},
		}

		s := &Server{hardcover: mock}
		input := GetMyLibraryInput{
			Status:           "reading",
			PaginationParams: PaginationParams{Page: 1, PageSize: 20},
		}

		result, data, err := s.handleGetMyLibrary(context.Background(), nil, input)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected result, got nil")
		}

		dataMap, ok := data.(map[string]any)
		if !ok {
			t.Fatal("expected data to be map[string]any")
		}
		books, ok := dataMap["books"].([]models.Book)
		if !ok {
			t.Fatal("expected 'books' field to be []models.Book")
		}
		if len(books) != 2 {
			t.Errorf("expected 2 books, got %d", len(books))
		}
	})

	t.Run("errors when hardcover not configured", func(t *testing.T) {
		s := &Server{}
		input := GetMyLibraryInput{Status: "reading"}

		_, _, err := s.handleGetMyLibrary(context.Background(), nil, input)

		if err == nil {
			t.Fatal("expected error when hardcover not configured")
		}
	})

	t.Run("uses default status and page size", func(t *testing.T) {
		var capturedStatus string
		var capturedOffset, capturedLimit int
		mock := &mocks.MockHardcoverProvider{
			GetUserBooksFunc: func(ctx context.Context, status string, offset, limit int) ([]models.Book, int, error) {
				capturedStatus = status
				capturedOffset = offset
				capturedLimit = limit
				return nil, 0, nil
			},
		}

		s := &Server{hardcover: mock}
		input := GetMyLibraryInput{}

		_, _, _ = s.handleGetMyLibrary(context.Background(), nil, input)

		if capturedStatus != "all" {
			t.Errorf("expected default status 'all', got '%s'", capturedStatus)
		}
		if capturedOffset != 0 {
			t.Errorf("expected offset of 0, got %d", capturedOffset)
		}
		if capturedLimit != 20 {
			t.Errorf("expected default page size 20, got %d", capturedLimit)
		}
	})

	t.Run("propagates provider errors", func(t *testing.T) {
		expectedErr := errors.New("provider error")
		mock := &mocks.MockHardcoverProvider{
			GetUserBooksFunc: func(ctx context.Context, status string, offset, limit int) ([]models.Book, int, error) {
				return nil, 0, expectedErr
			},
		}

		s := &Server{hardcover: mock}
		input := GetMyLibraryInput{Status: "reading"}

		_, _, err := s.handleGetMyLibrary(context.Background(), nil, input)

		if err == nil {
			t.Fatal("expected error to be propagated")
		}
	})
}

func TestHandleUpdateReadingStatus(t *testing.T) {
	t.Run("updates status successfully", func(t *testing.T) {
		var capturedBookID, capturedStatus string
		var capturedProgress int
		var capturedRating float64

		mock := &mocks.MockHardcoverProvider{
			UpdateBookStatusFunc: func(ctx context.Context, bookID, status string, progress int, rating float64) error {
				capturedBookID = bookID
				capturedStatus = status
				capturedProgress = progress
				capturedRating = rating
				return nil
			},
		}

		s := &Server{hardcover: mock}
		input := UpdateReadingStatusInput{
			BookID:   "book-123",
			Status:   "reading",
			Progress: 50,
			Rating:   4.5,
		}

		result, _, err := s.handleUpdateReadingStatus(context.Background(), nil, input)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected result, got nil")
		}
		if capturedBookID != "book-123" {
			t.Errorf("expected book ID 'book-123', got '%s'", capturedBookID)
		}
		if capturedStatus != "reading" {
			t.Errorf("expected status 'reading', got '%s'", capturedStatus)
		}
		if capturedProgress != 50 {
			t.Errorf("expected progress 50, got %d", capturedProgress)
		}
		if capturedRating != 4.5 {
			t.Errorf("expected rating 4.5, got %f", capturedRating)
		}
	})

	t.Run("errors when hardcover not configured", func(t *testing.T) {
		s := &Server{}
		input := UpdateReadingStatusInput{BookID: "123", Status: "reading"}

		_, _, err := s.handleUpdateReadingStatus(context.Background(), nil, input)

		if err == nil {
			t.Fatal("expected error when hardcover not configured")
		}
	})
}

func TestHandleGetLoans(t *testing.T) {
	t.Run("returns loans from libby", func(t *testing.T) {
		mock := &mocks.MockLibbyProvider{
			GetLoansFunc: func(ctx context.Context) ([]models.LibbyLoan, error) {
				return []models.LibbyLoan{
					{ID: "loan-1", Title: "Borrowed Book"},
					{ID: "loan-2", Title: "Another Loan"},
				}, nil
			},
		}

		s := &Server{libby: mock}

		result, data, err := s.handleGetLoans(context.Background(), nil, nil)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected result, got nil")
		}

		dataMap, ok := data.(map[string]any)
		if !ok {
			t.Fatal("expected data to be map[string]any")
		}
		loans, ok := dataMap["loans"].([]models.LibbyLoan)
		if !ok {
			t.Fatal("expected 'loans' field to be []models.LibbyLoan")
		}
		if len(loans) != 2 {
			t.Errorf("expected 2 loans, got %d", len(loans))
		}
	})

	t.Run("errors when libby not configured", func(t *testing.T) {
		s := &Server{}

		_, _, err := s.handleGetLoans(context.Background(), nil, nil)

		if err == nil {
			t.Fatal("expected error when libby not configured")
		}
	})
}

func TestHandleGetHolds(t *testing.T) {
	t.Run("returns holds from libby", func(t *testing.T) {
		mock := &mocks.MockLibbyProvider{
			GetHoldsFunc: func(ctx context.Context) ([]models.LibbyHold, error) {
				return []models.LibbyHold{
					{ID: "hold-1", Title: "On Hold Book", IsReady: true},
				}, nil
			},
		}

		s := &Server{libby: mock}

		result, data, err := s.handleGetHolds(context.Background(), nil, nil)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected result, got nil")
		}

		dataMap, ok := data.(map[string]any)
		if !ok {
			t.Fatal("expected data to be map[string]any")
		}
		holds, ok := dataMap["holds"].([]models.LibbyHold)
		if !ok {
			t.Fatal("expected 'holds' field to be []models.LibbyHold")
		}
		if len(holds) != 1 {
			t.Errorf("expected 1 hold, got %d", len(holds))
		}
		if !holds[0].IsReady {
			t.Error("expected hold to be ready")
		}
	})
}

func TestHandlePlaceHold(t *testing.T) {
	t.Run("places hold successfully", func(t *testing.T) {
		var capturedMediaID, capturedFormat string
		var capturedAutoBorrow bool

		mock := &mocks.MockLibbyProvider{
			PlaceHoldFunc: func(ctx context.Context, mediaID, format string, autoBorrow bool) (string, error) {
				capturedMediaID = mediaID
				capturedFormat = format
				capturedAutoBorrow = autoBorrow
				return "new-hold-id", nil
			},
		}

		s := &Server{libby: mock}
		input := PlaceHoldInput{
			MediaID:    "media-123",
			Format:     "ebook",
			AutoBorrow: true,
		}

		result, data, err := s.handlePlaceHold(context.Background(), nil, input)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected result, got nil")
		}
		if capturedMediaID != "media-123" {
			t.Errorf("expected media ID 'media-123', got '%s'", capturedMediaID)
		}
		if capturedFormat != "ebook" {
			t.Errorf("expected format 'ebook', got '%s'", capturedFormat)
		}
		if !capturedAutoBorrow {
			t.Error("expected auto_borrow to be true")
		}

		holdData, ok := data.(map[string]any)
		if !ok {
			t.Fatal("expected data to be map[string]any")
		}
		if holdData["hold_id"] != "new-hold-id" {
			t.Errorf("expected hold_id 'new-hold-id', got '%v'", holdData["hold_id"])
		}
	})
}

func TestHandleCheckAvailability(t *testing.T) {
	t.Run("returns availability info", func(t *testing.T) {
		mock := &mocks.MockLibbyProvider{
			CheckAvailabilityFunc: func(ctx context.Context, isbn, title, author string) (*models.LibraryAvailability, error) {
				return &models.LibraryAvailability{
					LibraryName:    "Austin Public Library",
					EbookAvailable: true,
					EbookCopies:    3,
				}, nil
			},
		}

		s := &Server{libby: mock}
		input := CheckAvailabilityInput{ISBN: "978-1234567890"}

		result, data, err := s.handleCheckAvailability(context.Background(), nil, input)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected result, got nil")
		}

		dataMap, ok := data.(map[string]any)
		if !ok {
			t.Fatal("expected data to be map[string]any")
		}
		avail, ok := dataMap["availability"].(*models.LibraryAvailability)
		if !ok {
			t.Fatal("expected 'availability' field to be *models.LibraryAvailability")
		}
		if !avail.EbookAvailable {
			t.Error("expected ebook to be available")
		}
	})

	t.Run("handles book not found", func(t *testing.T) {
		mock := &mocks.MockLibbyProvider{
			CheckAvailabilityFunc: func(ctx context.Context, isbn, title, author string) (*models.LibraryAvailability, error) {
				return nil, nil
			},
		}

		s := &Server{libby: mock}
		input := CheckAvailabilityInput{ISBN: "unknown"}

		result, data, err := s.handleCheckAvailability(context.Background(), nil, input)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected result, got nil")
		}
		dataMap, ok := data.(map[string]any)
		if !ok {
			t.Fatal("expected data to be map[string]any")
		}
		found, ok := dataMap["found"].(bool)
		if !ok {
			t.Fatal("expected 'found' field to be bool")
		}
		if found {
			t.Error("expected 'found' to be false for not found book")
		}
	})
}

func TestGenerateRecommendation(t *testing.T) {
	t.Run("recommends ebook when available", func(t *testing.T) {
		result := &models.UnifiedBookResult{
			LibraryAvailability: &models.LibraryAvailability{
				EbookAvailable: true,
			},
		}

		rec := generateRecommendation(result)

		if rec == "" {
			t.Fatal("expected recommendation, got empty string")
		}
		if !contains(rec, "ebook") && !contains(rec, "Libby") {
			t.Error("expected recommendation to mention ebook or Libby")
		}
	})

	t.Run("recommends audiobook when available", func(t *testing.T) {
		result := &models.UnifiedBookResult{
			LibraryAvailability: &models.LibraryAvailability{
				AudiobookAvailable: true,
			},
		}

		rec := generateRecommendation(result)

		if !contains(rec, "audiobook") || !contains(rec, "Libby") {
			t.Error("expected recommendation to mention audiobook and Libby")
		}
	})

	t.Run("recommends short wait when applicable", func(t *testing.T) {
		result := &models.UnifiedBookResult{
			LibraryAvailability: &models.LibraryAvailability{
				EstimatedWaitDays: 7,
			},
		}

		rec := generateRecommendation(result)

		if !contains(rec, "short wait") && !contains(rec, "7 days") {
			t.Error("expected recommendation to mention short wait")
		}
	})

	t.Run("falls back to local bookstores", func(t *testing.T) {
		result := &models.UnifiedBookResult{}

		rec := generateRecommendation(result)

		if !contains(rec, "bookstore") && !contains(rec, "library") {
			t.Error("expected fallback recommendation")
		}
	})
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
