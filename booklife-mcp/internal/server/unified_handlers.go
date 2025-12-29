package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/user/booklife-mcp/internal/models"
)

// ===== Unified Action Input Types =====

// FindBookEverywhereInput for the find_book_everywhere tool
type FindBookEverywhereInput struct {
	Query string `json:"query"`
}

// BestWayToReadInput for the best_way_to_read tool
type BestWayToReadInput struct {
	BookID      string   `json:"book_id,omitempty"`
	ISBN        string   `json:"isbn,omitempty"`
	Preferences []string `json:"preferences,omitempty"`
}

// AddToTBRInput for the add_to_tbr tool
type AddToTBRInput struct {
	ISBN          string `json:"isbn,omitempty"`
	Title         string `json:"title,omitempty"`
	Author        string `json:"author,omitempty"`
	PlaceHold     bool   `json:"place_hold,omitempty"`
	NotifyInStock bool   `json:"notify_in_stock,omitempty"`
}

// ===== Unified Action Tool Handlers =====

func (s *Server) handleFindBookEverywhere(ctx context.Context, req *mcp.CallToolRequest, input FindBookEverywhereInput) (*mcp.CallToolResult, any, error) {
	if input.Query == "" {
		return nil, nil, fmt.Errorf("query is required")
	}
	if len(input.Query) > 500 {
		return nil, nil, fmt.Errorf("query too long (max 500 characters)")
	}

	result := models.UnifiedBookResult{
		Query: input.Query,
	}

	// Search Hardcover for metadata
	if s.hardcover != nil {
		books, _, err := s.hardcover.SearchBooks(ctx, input.Query, 0, 1)
		if err == nil && len(books) > 0 {
			result.Book = &books[0]
		}
	}

	// Check library availability
	if s.libby != nil && result.Book != nil {
		avail, err := s.libby.CheckAvailability(ctx, result.Book.ISBN13, result.Book.Title, "")
		if err == nil {
			result.LibraryAvailability = avail
		}
	}

	// Generate recommendation
	result.Recommendation = generateRecommendation(&result)

	// Build detailed output with all cross-tool identifiers
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Results for \"%s\":\n\n", input.Query))

	if result.Book != nil {
		sb.WriteString(formatBookForDisplay(*result.Book, 0))
		sb.WriteString("\n")
	} else {
		sb.WriteString(fmt.Sprintf("No book found matching \"%s\"\n\n", input.Query))
	}

	if result.LibraryAvailability != nil {
		sb.WriteString(formatLibraryAvailabilityForDisplay(result.LibraryAvailability, ""))
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("💡 Recommendation: %s\n", result.Recommendation))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: sb.String()},
		},
	}, result, nil
}

func (s *Server) handleBestWayToRead(ctx context.Context, req *mcp.CallToolRequest, input BestWayToReadInput) (*mcp.CallToolResult, any, error) {
	// Similar to find_book_everywhere but returns prioritized options
	var options []map[string]any

	// Check library first
	if s.libby != nil {
		avail, err := s.libby.CheckAvailability(ctx, input.ISBN, "", "")
		if err == nil && avail != nil {
			if avail.EbookAvailable || avail.AudiobookAvailable {
				options = append(options, map[string]any{
					"priority":    1,
					"source":      "library",
					"action":      "borrow_now",
					"cost":        "FREE",
					"media_id":    avail.MediaID,
					"description": fmt.Sprintf("Available NOW at library via Libby (media_id: %s)", avail.MediaID),
				})
			} else {
				options = append(options, map[string]any{
					"priority":    2,
					"source":      "library",
					"action":      "place_hold",
					"cost":        "FREE",
					"media_id":    avail.MediaID,
					"wait_days":   avail.EstimatedWaitDays,
					"description": fmt.Sprintf("Place hold at library - %d day wait (media_id: %s)", avail.EstimatedWaitDays, avail.MediaID),
				})
			}
		}
	}

	// Local bookstore fallback
	options = append(options, map[string]any{
		"priority":    3,
		"source":      "local_bookstore",
		"action":      "visit_store",
		"cost":        "varies",
		"description": "Check local bookstores like BookPeople",
	})

	// Online fallback
	options = append(options, map[string]any{
		"priority":    4,
		"source":      "online",
		"action":      "purchase",
		"cost":        "varies",
		"description": "Available for purchase online",
	})

	// Build human-readable output
	var sb strings.Builder
	sb.WriteString("Best way to read this book (in priority order):\n\n")
	for i, opt := range options {
		priority, _ := opt["priority"].(int)
		cost, _ := opt["cost"].(string)
		desc, _ := opt["description"].(string)

		sb.WriteString(fmt.Sprintf("%d. %s", i+1, desc))
		if cost != "varies" {
			sb.WriteString(fmt.Sprintf(" (%s)", cost))
		}
		if priority == 1 {
			sb.WriteString(" ⭐ RECOMMENDED")
		}
		sb.WriteString("\n")

		// Show media_id if present for cross-tool usage
		if mediaID, ok := opt["media_id"].(string); ok && mediaID != "" {
			sb.WriteString(fmt.Sprintf("   media_id: %s\n", mediaID))
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: sb.String(),
			},
		},
	}, map[string]any{"options": options}, nil
}

func (s *Server) handleAddToTBR(ctx context.Context, req *mcp.CallToolRequest, input AddToTBRInput) (*mcp.CallToolResult, any, error) {
	// Add to Hardcover
	addInput := AddToLibraryInput{
		ISBN:      input.ISBN,
		Title:     input.Title,
		Author:    input.Author,
		Status:    "want-to-read",
		PlaceHold: input.PlaceHold,
	}

	return s.handleAddToLibrary(ctx, req, addInput)
}

// Helper function to generate reading recommendations
func generateRecommendation(result *models.UnifiedBookResult) string {
	if result.LibraryAvailability != nil {
		if result.LibraryAvailability.EbookAvailable {
			return fmt.Sprintf("🎉 Great news! This book is available RIGHT NOW as an ebook at %s. Borrow it free with Libby! (media_id: %s)",
				result.LibraryAvailability.LibraryName,
				result.LibraryAvailability.MediaID)
		}
		if result.LibraryAvailability.AudiobookAvailable {
			return fmt.Sprintf("🎧 This audiobook is available RIGHT NOW at %s. Listen free with Libby! (media_id: %s)",
				result.LibraryAvailability.LibraryName,
				result.LibraryAvailability.MediaID)
		}
		if result.LibraryAvailability.EstimatedWaitDays < 14 {
			return fmt.Sprintf("📚 This book has a short wait at %s (~%d days). Place a hold for free access! (media_id: %s)",
				result.LibraryAvailability.LibraryName,
				result.LibraryAvailability.EstimatedWaitDays,
				result.LibraryAvailability.MediaID)
		}
	}

	return "📖 Check local bookstores like BookPeople, or place a library hold for free access later."
}
