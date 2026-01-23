package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ===== Hardcover Input Types =====

// GetMyLibraryInput for the get_my_library tool
type GetMyLibraryInput struct {
	// Filtering
	Status string `json:"status,omitempty"` // reading, read, want-to-read, dnf, all (default: all)
	// Progressive detail
	Detail string `json:"detail,omitempty"` // summary, list (default), full
	// Sorting
	SortBy string `json:"sort_by,omitempty"` // date_added, title, author, rating, progress
	// Pagination
	PaginationParams `json:",inline"`
}

// UpdateReadingStatusInput for the update_reading_status tool
type UpdateReadingStatusInput struct {
	BookID   string  `json:"book_id"`
	Status   string  `json:"status"`
	Progress int     `json:"progress,omitempty"`
	Rating   float64 `json:"rating,omitempty"`
}

// AddToLibraryInput for the add_to_library tool
type AddToLibraryInput struct {
	ISBN      string `json:"isbn,omitempty"`
	Title     string `json:"title,omitempty"`
	Author    string `json:"author,omitempty"`
	Status    string `json:"status,omitempty"`
	PlaceHold bool   `json:"place_hold,omitempty"`
}

// ===== Hardcover Tool Handlers =====

func (s *Server) handleGetMyLibrary(ctx context.Context, req *mcp.CallToolRequest, input GetMyLibraryInput) (*mcp.CallToolResult, any, error) {
	if s.hardcover == nil {
		return nil, nil, NewHardcoverNotConfiguredError()
	}

	detail := input.Detail
	if detail == "" {
		detail = "list"
	}

	// Summary mode: fetch counts for all statuses without full book data
	if detail == "summary" {
		return s.handleGetMyLibrarySummary(ctx)
	}

	// Get pagination parameters
	page, pageSize := getPagination(input.PaginationParams)
	offset := input.PaginationParams.offset()

	status := input.Status
	if status == "" {
		status = "all"
	}

	books, totalCount, err := s.hardcover.GetUserBooks(ctx, status, offset, pageSize)
	if err != nil {
		return nil, nil, fmt.Errorf("fetching library: %w", err)
	}

	// Calculate pagination metadata
	pagedResult := calculatePagedResult(page, pageSize, totalCount)

	// Build detailed text output with book_id for cross-tool usage
	var sb strings.Builder
	if len(books) == 0 {
		sb.WriteString(fmt.Sprintf("No books found in your library with status \"%s\"\n", status))
	} else {
		sb.WriteString(fmt.Sprintf("Your library (%s) - %d books:\n\n", status, totalCount))
		for i, book := range books {
			sb.WriteString(formatBookForDisplay(book, i+1))
		}
		sb.WriteString(formatPagingFooter(pagedResult, len(books)))
	}

	// Suggest next actions
	var suggestedNext []string
	if len(books) > 0 && status == "reading" {
		suggestedNext = append(suggestedNext, "hardcover_update_reading_status")
	}

	return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: sb.String(),
				},
			},
		}, map[string]any{
			"books":      books,
			"status":     status,
			"pagination": pagedResult,
			"_meta":      createResponseMeta(len(books) > 0, false, suggestedNext, true, 0),
		}, nil
}

// handleGetMyLibrarySummary provides a token-efficient summary of library status
func (s *Server) handleGetMyLibrarySummary(ctx context.Context) (*mcp.CallToolResult, any, error) {
	// Fetch small samples from each status to get counts and stats
	statuses := []string{"reading", "want-to-read", "read"}
	summary := make(map[string]any)

	var sb strings.Builder
	sb.WriteString("Library Summary\n\n")

	for _, status := range statuses {
		books, totalCount, err := s.hardcover.GetUserBooks(ctx, status, 0, 20)
		if err != nil {
			continue
		}

		summary[status] = map[string]any{
			"count": totalCount,
		}

		switch status {
		case "reading":
			if totalCount > 0 {
				// Calculate average progress
				totalProgress := 0
				for _, book := range books {
					if book.UserStatus != nil {
						totalProgress += book.UserStatus.Progress
					}
				}
				avgProgress := totalProgress / len(books)
				summary[status].(map[string]any)["avg_progress"] = avgProgress
				sb.WriteString(fmt.Sprintf("Currently Reading: %d books (avg %d%% progress)\n", totalCount, avgProgress))
			} else {
				sb.WriteString("Currently Reading: 0 books\n")
			}

		case "want-to-read":
			sb.WriteString(fmt.Sprintf("Want to Read (TBR): %d books\n", totalCount))

		case "read":
			if totalCount > 0 {
				// Calculate average rating from sample
				totalRating := 0.0
				ratedCount := 0
				for _, book := range books {
					if book.UserStatus != nil && book.UserStatus.Rating > 0 {
						totalRating += book.UserStatus.Rating
						ratedCount++
					}
				}
				if ratedCount > 0 {
					avgRating := totalRating / float64(ratedCount)
					summary[status].(map[string]any)["avg_rating"] = avgRating
					sb.WriteString(fmt.Sprintf("Read: %d books (avg %.1f★)\n", totalCount, avgRating))
				} else {
					sb.WriteString(fmt.Sprintf("Read: %d books\n", totalCount))
				}
			} else {
				sb.WriteString("Read: 0 books\n")
			}
		}
	}

	sb.WriteString("\n→ Use detail=\"list\" to see book lists\n")
	sb.WriteString("→ Use status=\"reading\" to filter by status\n")

	return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: sb.String()},
			},
		}, map[string]any{
			"summary": summary,
			"_meta":   createResponseMeta(true, false, []string{"hardcover_get_my_library"}, true, 0),
		}, nil
}

func (s *Server) handleUpdateReadingStatus(ctx context.Context, req *mcp.CallToolRequest, input UpdateReadingStatusInput) (*mcp.CallToolResult, any, error) {
	if s.hardcover == nil {
		return nil, nil, fmt.Errorf("Hardcover is not configured")
	}

	if input.BookID == "" {
		return nil, nil, fmt.Errorf("book_id is required")
	}

	if input.Status == "" {
		return nil, nil, fmt.Errorf("status is required")
	}

	// Validate progress is in range 0-100
	if input.Progress < 0 || input.Progress > 100 {
		return nil, nil, fmt.Errorf("progress must be between 0 and 100")
	}

	// Validate rating is in range 0-5
	if input.Rating < 0 || input.Rating > 5 {
		return nil, nil, fmt.Errorf("rating must be between 0 and 5")
	}

	err := s.hardcover.UpdateBookStatus(ctx, input.BookID, input.Status, input.Progress, input.Rating)
	if err != nil {
		return nil, nil, fmt.Errorf("updating status: %w", err)
	}

	// Get updated book details directly
	updatedBook, err := s.hardcover.GetBook(ctx, input.BookID)
	if err != nil || updatedBook == nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("✅ Updated reading status for book_id: %s\n", input.BookID),
				},
			},
		}, map[string]any{"book_id": input.BookID, "status": input.Status}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: fmt.Sprintf("✅ Updated reading status for \"%s\"\n\n%s", updatedBook.Title, formatBookForDisplay(*updatedBook, 0)),
			},
		},
	}, map[string]any{"book_id": input.BookID, "status": input.Status}, nil
}

func (s *Server) handleAddToLibrary(ctx context.Context, req *mcp.CallToolRequest, input AddToLibraryInput) (*mcp.CallToolResult, any, error) {
	if s.hardcover == nil {
		return nil, nil, fmt.Errorf("Hardcover is not configured")
	}

	bookID, err := s.hardcover.AddBook(ctx, input.ISBN, input.Title, input.Author, input.Status)
	if err != nil {
		return nil, nil, fmt.Errorf("adding to library: %w", err)
	}

	// If place_hold is true and we have a Libby connection, try to place a hold
	if input.PlaceHold && s.libby != nil && input.ISBN != "" {
		// Search for the book in Libby
		searchResult, _, err := s.libby.Search(ctx, input.ISBN, nil, false, 0, 1)
		if err == nil && len(searchResult) > 0 {
			mediaID := searchResult[0].LibraryAvailability.MediaID
			_, holdErr := s.libby.PlaceHold(ctx, mediaID, "ebook", false)
			if holdErr != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{
							Text: fmt.Sprintf("✅ Added \"%s\" to your library (note: could not place library hold: %v)\n", input.Title, holdErr),
						},
					},
				}, map[string]any{
					"book_id": bookID,
					"_meta":   createOperationMeta(true, true, false, []string{"get_my_library", "update_reading_status"}),
				}, nil
			}
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: fmt.Sprintf("✅ Added \"%s\" to your library\n", input.Title),
			},
		},
	}, map[string]any{
		"book_id": bookID,
		"_meta":   createOperationMeta(true, true, false, []string{"get_my_library", "update_reading_status"}),
	}, nil
}
