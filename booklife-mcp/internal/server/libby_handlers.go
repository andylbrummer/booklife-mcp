package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/user/booklife-mcp/internal/models"
)

// ===== Libby Input Types =====

// SearchLibraryInput for the search_library tool
type SearchLibraryInput struct {
	Query string `json:"query"`
	// Filtering
	Format    []string `json:"format,omitempty"`    // ebook, audiobook, magazine
	Available bool     `json:"available,omitempty"` // Only show available items
	Language  string   `json:"language,omitempty"`  // Filter by language (e.g., "eng", "spa")
	// Sorting
	SortBy string `json:"sort_by,omitempty"` // relevance, title, author, date
	// Pagination
	PaginationParams `json:",inline"`
}

// PlaceHoldInput for the place_hold tool
type PlaceHoldInput struct {
	MediaID    string `json:"media_id"`
	Format     string `json:"format"`
	AutoBorrow bool   `json:"auto_borrow,omitempty"`
}

// ===== Libby Tool Handlers =====

func (s *Server) handleSearchLibrary(ctx context.Context, req *mcp.CallToolRequest, input SearchLibraryInput) (*mcp.CallToolResult, any, error) {
	if s.libby == nil {
		return nil, nil, fmt.Errorf("Libby is not configured")
	}

	if input.Query == "" {
		return nil, nil, fmt.Errorf("query is required")
	}
	if len(input.Query) > 500 {
		return nil, nil, fmt.Errorf("query too long (max 500 characters)")
	}

	// Get pagination parameters
	page, pageSize := getPagination(input.PaginationParams)
	offset := input.PaginationParams.offset()

	results, totalCount, err := s.libby.Search(ctx, input.Query, input.Format, input.Available, offset, pageSize)
	if err != nil {
		return nil, nil, fmt.Errorf("searching library: %w", err)
	}

	// Calculate pagination metadata
	pagedResult := calculatePagedResult(page, pageSize, totalCount)

	// Build detailed text output with media_id for cross-tool usage
	var sb strings.Builder
	if len(results) == 0 {
		sb.WriteString(fmt.Sprintf("No results found for \"%s\"\n", input.Query))
	} else {
		sb.WriteString(fmt.Sprintf("Found %d results in library catalog for \"%s\":\n", totalCount, input.Query))
		for i, book := range results {
			sb.WriteString(fmt.Sprintf("\n[%d] %s\n", i+1, book.Title))
			if book.Subtitle != "" {
				sb.WriteString(fmt.Sprintf("     %s\n", book.Subtitle))
			}

			// Authors
			if len(book.Authors) > 0 {
				authorNames := make([]string, 0, len(book.Authors))
				for _, a := range book.Authors {
					authorNames = append(authorNames, a.Name)
				}
				sb.WriteString(fmt.Sprintf("     by %s\n", strings.Join(authorNames, ", ")))
			}

			// === CRITICAL: media_id for place_hold ===
			if book.LibraryAvailability != nil {
				sb.WriteString(fmt.Sprintf("     media_id: %s\n", book.LibraryAvailability.MediaID))

				// Formats and availability
				formats := make([]string, 0)
				if book.LibraryAvailability.EbookAvailable {
					formats = append(formats, "ebook")
				}
				if book.LibraryAvailability.AudiobookAvailable {
					formats = append(formats, "audiobook")
				}
				if len(formats) > 0 {
					sb.WriteString(fmt.Sprintf("     Formats: %s\n", strings.Join(formats, ", ")))
				}

				if book.LibraryAvailability.EbookAvailable || book.LibraryAvailability.AudiobookAvailable {
					sb.WriteString("     ✅ Available now\n")
				} else {
					sb.WriteString("     ⏳ Wait list only\n")
					if book.LibraryAvailability.EstimatedWaitDays > 0 {
						sb.WriteString(fmt.Sprintf("     Estimated wait: ~%d days\n", book.LibraryAvailability.EstimatedWaitDays))
					}
				}
			}

			// ISBN if available
			if book.ISBN13 != "" {
				sb.WriteString(fmt.Sprintf("     ISBN: %s\n", book.ISBN13))
			} else if book.ISBN10 != "" {
				sb.WriteString(fmt.Sprintf("     ISBN: %s\n", book.ISBN10))
			}
		}
		sb.WriteString(formatPagingFooter(pagedResult, len(results)))
	}

	// Determine next actions based on results
	var nextActions []string
	if len(results) > 0 {
		// Check if any books are available now
		hasAvailable := false
		for _, book := range results {
			if book.LibraryAvailability != nil && (book.LibraryAvailability.EbookAvailable || book.LibraryAvailability.AudiobookAvailable) {
				hasAvailable = true
				break
			}
		}

		if hasAvailable {
			nextActions = append(nextActions, "place_hold")
		}
		nextActions = append(nextActions, "check_availability", "add_to_tbr")
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: sb.String(),
			},
		},
	}, map[string]any{
		"results":    results,
		"count":      len(results),
		"pagination": pagedResult,
		"_meta":      createSearchMeta(len(results), pagedResult.HasNext, nextActions),
	}, nil
}

func (s *Server) handleGetLoans(ctx context.Context, req *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	if s.libby == nil {
		return nil, nil, fmt.Errorf("Libby is not configured")
	}

	loans, err := s.libby.GetLoans(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("getting loans: %w", err)
	}

	// Ensure empty array instead of null
	if loans == nil {
		loans = []models.LibbyLoan{}
	}

	// Build detailed text output with media_id for potential actions
	var sb strings.Builder
	if len(loans) == 0 {
		sb.WriteString("You have no active loans\n")
	} else {
		sb.WriteString(fmt.Sprintf("You have %d active loan(s):\n\n", len(loans)))
		for i, loan := range loans {
			sb.WriteString(fmt.Sprintf("[%d] %s\n", i+1, loan.Title))
			sb.WriteString(fmt.Sprintf("     by %s\n", loan.Author))
			sb.WriteString(fmt.Sprintf("     media_id: %s\n", loan.MediaID))
			sb.WriteString(fmt.Sprintf("     Format: %s\n", loan.Format))

			if !loan.DueDate.IsZero() {
				daysRemaining := int(time.Until(loan.DueDate).Hours() / 24)
				sb.WriteString(fmt.Sprintf("     Due: %s", loan.DueDate.Format("Jan 2, 2006")))
				if daysRemaining < 0 {
					sb.WriteString(" ⚠️ OVERDUE!")
				} else if daysRemaining <= 3 {
					sb.WriteString(" ⚠️ Due soon!")
				}
				if daysRemaining >= 0 {
					sb.WriteString(fmt.Sprintf(" (%d days remaining)", daysRemaining))
				}
				sb.WriteString("\n")
			}

			if loan.Progress > 0 {
				sb.WriteString(fmt.Sprintf("     Progress: %.0f%%\n", loan.Progress*100))
			}
			sb.WriteString("\n")
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: sb.String(),
			},
		},
	}, map[string]any{"loans": loans, "count": len(loans)}, nil
}

func (s *Server) handleGetHolds(ctx context.Context, req *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	if s.libby == nil {
		return nil, nil, fmt.Errorf("Libby is not configured")
	}

	holds, err := s.libby.GetHolds(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("getting holds: %w", err)
	}

	// Ensure empty array instead of null
	if holds == nil {
		holds = []models.LibbyHold{}
	}

	// Build detailed text output with media_id for cross-tool usage
	var sb strings.Builder
	if len(holds) == 0 {
		sb.WriteString("You have no active holds\n")
	} else {
		sb.WriteString(fmt.Sprintf("You have %d active hold(s):\n\n", len(holds)))
		for i, hold := range holds {
			sb.WriteString(fmt.Sprintf("[%d] %s\n", i+1, hold.Title))
			sb.WriteString(fmt.Sprintf("     by %s\n", hold.Author))
			sb.WriteString(fmt.Sprintf("     media_id: %s\n", hold.MediaID))
			sb.WriteString(fmt.Sprintf("     Format: %s\n", hold.Format))
			sb.WriteString(fmt.Sprintf("     Queue Position: #%d\n", hold.QueuePosition))

			if hold.IsReady {
				sb.WriteString("     🎉 Book is ready to borrow!\n")
			} else if hold.EstimatedWaitDays > 0 {
				sb.WriteString(fmt.Sprintf("     Estimated Wait: ~%d days\n", hold.EstimatedWaitDays))
			}

			if hold.AutoBorrow {
				sb.WriteString("     Auto-borrow: enabled\n")
			}
			sb.WriteString("\n")
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: sb.String(),
			},
		},
	}, map[string]any{"holds": holds, "count": len(holds)}, nil
}

func (s *Server) handlePlaceHold(ctx context.Context, req *mcp.CallToolRequest, input PlaceHoldInput) (*mcp.CallToolResult, any, error) {
	if s.libby == nil {
		return nil, nil, fmt.Errorf("Libby is not configured")
	}

	holdID, err := s.libby.PlaceHold(ctx, input.MediaID, input.Format, input.AutoBorrow)
	if err != nil {
		return nil, nil, fmt.Errorf("placing hold: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("✅ Hold placed successfully!\n")
	sb.WriteString(fmt.Sprintf("   Hold ID: %s\n", holdID))
	sb.WriteString(fmt.Sprintf("   Media ID: %s\n", input.MediaID))
	sb.WriteString(fmt.Sprintf("   Format: %s\n", input.Format))
	if input.AutoBorrow {
		sb.WriteString("   Auto-borrow: enabled\n")
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: sb.String(),
			},
		},
	}, map[string]any{
		"hold_id":  holdID,
		"media_id": input.MediaID,
		"format":   input.Format,
		"_meta":    createOperationMeta(true, true, false, []string{"get_holds", "get_loans"}),
	}, nil
}
