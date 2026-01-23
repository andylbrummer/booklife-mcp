package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/user/booklife-mcp/internal/tbr"
)

// ===== TBR Input Types =====

// TBRListInput for listing TBR books
type TBRListInput struct {
	Source string `json:"source,omitempty"` // Filter by source: physical, hardcover, libby
	PaginationParams
}

// TBRSearchInput for searching TBR
type TBRSearchInput struct {
	Query  string `json:"query"`
	Source string `json:"source,omitempty"`
	PaginationParams
}

// TBRAddInput for adding a book to TBR
type TBRAddInput struct {
	// Required fields
	Title  string `json:"title"`
	Author string `json:"author"`

	// Optional metadata
	Subtitle      string   `json:"subtitle,omitempty"`
	ISBN10        string   `json:"isbn10,omitempty"`
	ISBN13        string   `json:"isbn13,omitempty"`
	Publisher     string   `json:"publisher,omitempty"`
	PublishedDate string   `json:"published_date,omitempty"`
	PageCount     int      `json:"page_count,omitempty"`
	Description   string   `json:"description,omitempty"`
	CoverURL      string   `json:"cover_url,omitempty"`
	Genres        []string `json:"genres,omitempty"`

	// Series
	SeriesName     string  `json:"series_name,omitempty"`
	SeriesPosition float64 `json:"series_position,omitempty"`

	// TBR metadata
	Notes    string `json:"notes,omitempty"`
	Priority int    `json:"priority,omitempty"`
	Source   string `json:"source,omitempty"` // physical (default), hardcover, libby
}

// TBRRemoveInput for removing from TBR
type TBRRemoveInput struct {
	ID     int64  `json:"id,omitempty"`     // Remove by ID
	Title  string `json:"title,omitempty"`  // Or by title+author
	Author string `json:"author,omitempty"`
}

// TBRSyncInput for syncing TBR from external sources
type TBRSyncInput struct {
	Action string `json:"action"` // sync_hardcover, sync_libby_holds, sync_libby_tags, sync_all
}

// TBRTagMetadataSyncInput for syncing Libby tag metadata
type TBRTagMetadataSyncInput struct {
	// No input needed - syncs all tags
}

// TBRStatsInput for getting TBR statistics
type TBRStatsInput struct {
	// No input needed
}

// ===== TBR Tool Handlers =====

func (s *Server) handleTBRList(ctx context.Context, req *mcp.CallToolRequest, input TBRListInput) (*mcp.CallToolResult, any, error) {
	if s.tbrStore == nil {
		return nil, nil, fmt.Errorf("TBR store is not available")
	}

	page, pageSize := getPagination(input.PaginationParams)
	offset := input.PaginationParams.offset()

	entries, total, err := s.tbrStore.GetAll(input.Source, offset, pageSize)
	if err != nil {
		return nil, nil, fmt.Errorf("getting TBR list: %w", err)
	}

	// Calculate pagination metadata
	pagedResult := calculatePagedResult(page, pageSize, total)

	// Build text output
	var sb strings.Builder
	if total == 0 {
		sb.WriteString("📚 Your TBR list is empty\n")
		sb.WriteString("\nUse tbr_add to add books manually, or tbr_sync to sync from Hardcover/Libby\n")
	} else {
		sourceFilter := input.Source
		if sourceFilter == "" {
			sourceFilter = "all sources"
		}
		sb.WriteString(fmt.Sprintf("📚 Your TBR List (%s) - %d books\n\n", sourceFilter, total))

		for i, entry := range entries {
			sb.WriteString(fmt.Sprintf("[%d] %s\n", i+1, entry.Title))
			if entry.Subtitle != "" {
				sb.WriteString(fmt.Sprintf("    %s\n", entry.Subtitle))
			}
			sb.WriteString(fmt.Sprintf("    by %s\n", entry.Author))
			sb.WriteString(fmt.Sprintf("    Source: %s", entry.Source))
			if entry.Priority > 0 {
				sb.WriteString(fmt.Sprintf(" | Priority: %d", entry.Priority))
			}
			sb.WriteString("\n")

			// Series info
			if entry.SeriesName != "" {
				sb.WriteString(fmt.Sprintf("    Series: %s", entry.SeriesName))
				if entry.SeriesPosition > 0 {
					sb.WriteString(fmt.Sprintf(" #%.0f", entry.SeriesPosition))
				}
				sb.WriteString("\n")
			}

			// Libby-specific info
			if entry.Source == tbr.SourceLibby {
				if len(entry.LibbyTags) > 0 {
					sb.WriteString(fmt.Sprintf("    Tags: %s\n", strings.Join(entry.LibbyTags, ", ")))
				}
				if entry.LibbyAvailable {
					sb.WriteString("    ✅ Available now at library\n")
				} else if entry.LibbyWaitlist > 0 {
					sb.WriteString(fmt.Sprintf("    📚 Library waitlist: %d\n", entry.LibbyWaitlist))
				}
				if entry.LibbyHoldID != "" {
					sb.WriteString("    🔖 On hold\n")
				}
			}

			// Notes
			if entry.Notes != "" {
				sb.WriteString(fmt.Sprintf("    Notes: %s\n", entry.Notes))
			}

			// IDs for reference
			sb.WriteString(fmt.Sprintf("    ID: %d", entry.ID))
			if entry.ISBN13 != "" {
				sb.WriteString(fmt.Sprintf(" | ISBN: %s", entry.ISBN13))
			}
			sb.WriteString("\n\n")
		}

		sb.WriteString(formatPagingFooter(pagedResult, len(entries)))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: sb.String(),
			},
		},
	}, map[string]any{
		"total_books": total,
		"page_info":   pagedResult,
	}, nil
}

func (s *Server) handleTBRSearch(ctx context.Context, req *mcp.CallToolRequest, input TBRSearchInput) (*mcp.CallToolResult, any, error) {
	if s.tbrStore == nil {
		return nil, nil, fmt.Errorf("TBR store is not available")
	}

	if input.Query == "" {
		return nil, nil, fmt.Errorf("query is required")
	}

	page, pageSize := getPagination(input.PaginationParams)
	offset := input.PaginationParams.offset()

	entries, total, err := s.tbrStore.Search(input.Query, input.Source, offset, pageSize)
	if err != nil {
		return nil, nil, fmt.Errorf("searching TBR: %w", err)
	}

	pagedResult := calculatePagedResult(page, pageSize, total)

	var sb strings.Builder
	if total == 0 {
		sb.WriteString(fmt.Sprintf("No TBR books found matching \"%s\"\n", input.Query))
	} else {
		sb.WriteString(fmt.Sprintf("Found %d TBR books matching \"%s\":\n\n", total, input.Query))

		for i, entry := range entries {
			sb.WriteString(fmt.Sprintf("[%d] %s by %s\n", i+1, entry.Title, entry.Author))
			sb.WriteString(fmt.Sprintf("    Source: %s | ID: %d\n", entry.Source, entry.ID))
			if len(entry.LibbyTags) > 0 {
				sb.WriteString(fmt.Sprintf("    Tags: %s\n", strings.Join(entry.LibbyTags, ", ")))
			}
			sb.WriteString("\n")
		}

		sb.WriteString(formatPagingFooter(pagedResult, len(entries)))
	}

	// Determine next actions
	var nextActions []string
	if total > 0 {
		nextActions = append(nextActions, "tbr_list", "tbr_remove")
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: sb.String(),
			},
		},
	}, map[string]any{
		"total_results": total,
		"page_info":     pagedResult,
		"_meta":         createSearchMeta(total, pagedResult.HasNext, nextActions),
	}, nil
}

func (s *Server) handleTBRAdd(ctx context.Context, req *mcp.CallToolRequest, input TBRAddInput) (*mcp.CallToolResult, any, error) {
	if s.tbrStore == nil {
		return nil, nil, fmt.Errorf("TBR store is not available")
	}

	if input.Title == "" {
		return nil, nil, fmt.Errorf("title is required")
	}
	if input.Author == "" {
		return nil, nil, fmt.Errorf("author is required")
	}

	// Default source to physical if not specified
	source := input.Source
	if source == "" {
		source = tbr.SourcePhysical
	}

	entry := &tbr.TBREntry{
		Title:          input.Title,
		Subtitle:       input.Subtitle,
		Author:         input.Author,
		ISBN10:         input.ISBN10,
		ISBN13:         input.ISBN13,
		Publisher:      input.Publisher,
		PublishedDate:  input.PublishedDate,
		PageCount:      input.PageCount,
		Description:    input.Description,
		CoverURL:       input.CoverURL,
		Genres:         input.Genres,
		SeriesName:     input.SeriesName,
		SeriesPosition: input.SeriesPosition,
		Notes:          input.Notes,
		Priority:       input.Priority,
		Source:         source,
	}

	id, err := s.tbrStore.AddBook(entry)
	if err != nil {
		return nil, nil, fmt.Errorf("adding book to TBR: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: fmt.Sprintf("✅ Added \"%s\" by %s to your TBR list (ID: %d, Source: %s)\n", input.Title, input.Author, id, source),
			},
		},
	}, map[string]any{
		"id":     id,
		"source": source,
		"_meta":  createOperationMeta(true, true, false, []string{"tbr_list", "tbr_stats"}),
	}, nil
}

func (s *Server) handleTBRRemove(ctx context.Context, req *mcp.CallToolRequest, input TBRRemoveInput) (*mcp.CallToolResult, any, error) {
	if s.tbrStore == nil {
		return nil, nil, fmt.Errorf("TBR store is not available")
	}

	var err error
	var message string

	if input.ID > 0 {
		err = s.tbrStore.RemoveByID(input.ID)
		message = fmt.Sprintf("✅ Removed book ID %d from your TBR list\n", input.ID)
	} else if input.Title != "" && input.Author != "" {
		err = s.tbrStore.RemoveByTitleAuthor(input.Title, input.Author)
		message = fmt.Sprintf("✅ Removed \"%s\" by %s from your TBR list\n", input.Title, input.Author)
	} else {
		return nil, nil, fmt.Errorf("must provide either id or title+author")
	}

	if err != nil {
		return nil, nil, fmt.Errorf("removing from TBR: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: message,
			},
		},
	}, map[string]any{
		"removed": true,
		"_meta":   createOperationMeta(true, true, true, []string{"tbr_list", "tbr_stats"}),
	}, nil
}

func (s *Server) handleTBRSync(ctx context.Context, req *mcp.CallToolRequest, input TBRSyncInput) (*mcp.CallToolResult, any, error) {
	if s.tbrStore == nil {
		return nil, nil, fmt.Errorf("TBR store is not available")
	}

	syncer := tbr.NewTBRSyncer(s.tbrStore, s.libby)
	var sb strings.Builder
	totalAdded := 0
	totalUpdated := 0

	switch input.Action {
	case "sync_hardcover":
		if s.hardcover == nil {
			return nil, nil, fmt.Errorf("Hardcover is not configured")
		}

		// Fetch Hardcover TBR
		books, _, err := s.hardcover.GetUserBooks(ctx, "want-to-read", 0, 500)
		if err != nil {
			return nil, nil, fmt.Errorf("fetching Hardcover TBR: %w", err)
		}

		added, updated, err := syncer.SyncFromHardcover(books)
		if err != nil {
			return nil, nil, fmt.Errorf("syncing from Hardcover: %w", err)
		}

		totalAdded += added
		totalUpdated += updated
		sb.WriteString(fmt.Sprintf("📖 Synced Hardcover TBR: %d added, %d updated\n", added, updated))

	case "sync_libby_holds":
		if s.libby == nil {
			return nil, nil, fmt.Errorf("Libby is not configured")
		}

		holds, err := s.libby.GetHolds(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("fetching Libby holds: %w", err)
		}

		added, updated, err := syncer.SyncLibbyHolds(ctx, holds)
		if err != nil {
			return nil, nil, fmt.Errorf("syncing Libby holds: %w", err)
		}

		totalAdded += added
		totalUpdated += updated
		sb.WriteString(fmt.Sprintf("📚 Synced Libby holds: %d added, %d updated\n", added, updated))

	case "sync_libby_tags":
		if s.libby == nil {
			return nil, nil, fmt.Errorf("Libby is not configured")
		}

		// Get loans and holds to build book info map
		loans, err := s.libby.GetLoans(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("fetching loans: %w", err)
		}
		holds, err := s.libby.GetHolds(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("fetching holds: %w", err)
		}

		// Extract book info
		bookInfos := append(
			tbr.ExtractBookInfoFromLoans(loans),
			tbr.ExtractBookInfoFromHolds(holds)...,
		)

		// Sync tagged books to TBR
		added, updated, err := syncer.SyncLibbyTags(ctx, bookInfos)
		if err != nil {
			return nil, nil, fmt.Errorf("syncing tagged books: %w", err)
		}

		totalAdded += added
		totalUpdated += updated
		sb.WriteString(fmt.Sprintf("📚 Synced tagged books to TBR: %d added, %d updated\n", added, updated))
		sb.WriteString(fmt.Sprintf("\nNote: Use libby_sync_tag_metadata to fetch full book details for tagged items\n"))

	case "sync_all":
		// Sync everything
		sb.WriteString("🔄 Syncing all sources...\n\n")

		// Hardcover
		if s.hardcover != nil {
			books, _, err := s.hardcover.GetUserBooks(ctx, "want-to-read", 0, 500)
			if err == nil {
				added, updated, _ := syncer.SyncFromHardcover(books)
				totalAdded += added
				totalUpdated += updated
				sb.WriteString(fmt.Sprintf("📖 Hardcover: %d added, %d updated\n", added, updated))
			}
		}

		// Libby holds and tags
		if s.libby != nil {
			holds, err := s.libby.GetHolds(ctx)
			if err == nil {
				added, updated, _ := syncer.SyncLibbyHolds(ctx, holds)
				totalAdded += added
				totalUpdated += updated
				sb.WriteString(fmt.Sprintf("📚 Libby holds: %d added, %d updated\n", added, updated))

				// Sync tagged books to TBR
				loans, _ := s.libby.GetLoans(ctx)
				bookInfos := append(
					tbr.ExtractBookInfoFromLoans(loans),
					tbr.ExtractBookInfoFromHolds(holds)...,
				)

				added, updated, _ = syncer.SyncLibbyTags(ctx, bookInfos)
				totalAdded += added
				totalUpdated += updated
				sb.WriteString(fmt.Sprintf("📚 Libby tagged books: %d added, %d updated\n", added, updated))
			}
		}

	default:
		return nil, nil, fmt.Errorf("unknown action: %s (use sync_hardcover, sync_libby_holds, sync_libby_tags, or sync_all)", input.Action)
	}

	sb.WriteString(fmt.Sprintf("\n✅ Total: %d books added, %d updated\n", totalAdded, totalUpdated))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: sb.String(),
			},
		},
	}, map[string]any{
		"added":   totalAdded,
		"updated": totalUpdated,
	}, nil
}

func (s *Server) handleTBRStats(ctx context.Context, req *mcp.CallToolRequest, input TBRStatsInput) (*mcp.CallToolResult, any, error) {
	if s.tbrStore == nil {
		return nil, nil, fmt.Errorf("TBR store is not available")
	}

	stats, err := s.tbrStore.GetStats()
	if err != nil {
		return nil, nil, fmt.Errorf("getting TBR stats: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("📊 TBR Statistics\n\n")
	sb.WriteString(fmt.Sprintf("Total books: %v\n\n", stats["total_books"]))

	if bySrc, ok := stats["by_source"].(map[string]int); ok {
		sb.WriteString("By source:\n")
		for src, count := range bySrc {
			sb.WriteString(fmt.Sprintf("  %s: %d\n", src, count))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("Libby available now: %v\n", stats["libby_available"]))
	sb.WriteString(fmt.Sprintf("Libby on hold: %v\n", stats["libby_on_hold"]))
	sb.WriteString(fmt.Sprintf("Libby tagged books (cached): %v\n", stats["libby_tagged_books"]))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: sb.String(),
			},
		},
	}, stats, nil
}
