package server

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/user/booklife-mcp/internal/history"
	"github.com/user/booklife-mcp/internal/models"
)

// ===== History Input Types =====

// ImportTimelineInput for the history_import_timeline tool
type ImportTimelineInput struct {
	URL string `json:"url"`
}

// SyncCurrentLoansInput for the history_sync_current_loans tool
type SyncCurrentLoansInput struct {
	// No input needed
}

// GetLocalHistoryInput for the history_get tool
type GetLocalHistoryInput struct {
	Page     int `json:"page,omitempty"`
	PageSize int `json:"page_size,omitempty"`
}

// SearchLocalHistoryInput for the history_search tool
type SearchLocalHistoryInput struct {
	Query    string `json:"query"`
	Page     int    `json:"page,omitempty"`
	PageSize int    `json:"page_size,omitempty"`
}

// GetHistoryStatsInput for the history_stats tool
type GetHistoryStatsInput struct {
	// No input needed
}

// ExportForImportInput for the history_export_for_import tool
type ExportForImportInput struct {
	Activity string `json:"activity,omitempty"` // "Returned" (default), "Borrowed", "Reserved", "CheckedIn"
	Format   string `json:"format,omitempty"`   // "audiobook", "ebook", "book", "magazine"
	Limit    int    `json:"limit,omitempty"`    // Maximum number of books to export
}

// ===== History Tool Handlers =====

func (s *Server) handleImportTimeline(ctx context.Context, req *mcp.CallToolRequest, input ImportTimelineInput) (*mcp.CallToolResult, any, error) {
	if s.historyStore == nil {
		return nil, nil, fmt.Errorf("history store is not available")
	}

	if input.URL == "" {
		return nil, nil, fmt.Errorf("URL is required")
	}
	if len(input.URL) > 2048 {
		return nil, nil, fmt.Errorf("URL too long (max 2048 characters)")
	}

	// Validate URL scheme
	parsedURL, err := url.Parse(input.URL)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid URL: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, nil, fmt.Errorf("URL must use http or https scheme")
	}

	importer := history.NewImporter(s.historyStore)
	count, err := importer.ImportTimeline(input.URL)
	if err != nil {
		return nil, nil, fmt.Errorf("importing timeline: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: fmt.Sprintf("✅ Imported %d timeline entries from Libby\n", count),
			},
		},
	}, map[string]any{"entries_imported": count}, nil
}

func (s *Server) handleSyncCurrentLoans(ctx context.Context, req *mcp.CallToolRequest, input SyncCurrentLoansInput) (*mcp.CallToolResult, any, error) {
	if s.historyStore == nil {
		return nil, nil, fmt.Errorf("history store is not available")
	}
	if s.libby == nil {
		return nil, nil, fmt.Errorf("Libby is not configured")
	}

	loans, err := s.libby.GetLoans(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("getting current loans: %w", err)
	}

	count := 0
	for _, loan := range loans {
		if err := s.historyStore.ImportCurrentLoan(loan); err != nil {
			return nil, nil, fmt.Errorf("importing loan %s: %w", loan.Title, err)
		}
		count++
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: fmt.Sprintf("✅ Synced %d current loans to history\n", count),
			},
		},
	}, map[string]any{"loans_synced": count}, nil
}

func (s *Server) handleGetLocalHistory(ctx context.Context, req *mcp.CallToolRequest, input GetLocalHistoryInput) (*mcp.CallToolResult, any, error) {
	if s.historyStore == nil {
		return nil, nil, fmt.Errorf("history store is not available")
	}

	page := input.Page
	if page < 1 {
		page = 1
	}
	pageSize := input.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		return nil, nil, fmt.Errorf("page_size too large (max 100)")
	}

	offset := (page - 1) * pageSize
	entries, total, err := s.historyStore.GetHistory(offset, pageSize)
	if err != nil {
		return nil, nil, fmt.Errorf("getting history: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📚 Reading History (Page %d, %d total entries)\n\n", page, total))

	for _, entry := range entries {
		date := time.UnixMilli(entry.Timestamp).Format("2006-01-02")
		sb.WriteString(fmt.Sprintf("📖 %s by %s\n", entry.Title, entry.Author))
		sb.WriteString(fmt.Sprintf("   Activity: %s on %s\n", entry.Activity, date))
		if entry.Library != "" {
			sb.WriteString(fmt.Sprintf("   Library: %s\n", entry.Library))
		}
		if entry.Format != "" {
			sb.WriteString(fmt.Sprintf("   Format: %s\n", entry.Format))
		}
		sb.WriteString("\n")
	}

	totalPages := (total + pageSize - 1) / pageSize
	if page < totalPages {
		sb.WriteString(fmt.Sprintf("Page %d of %d - use page=%d for more\n", page, totalPages, page+1))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: sb.String(),
			},
		},
	}, map[string]any{"page": page, "page_size": pageSize, "total": total}, nil
}

func (s *Server) handleSearchLocalHistory(ctx context.Context, req *mcp.CallToolRequest, input SearchLocalHistoryInput) (*mcp.CallToolResult, any, error) {
	if s.historyStore == nil {
		return nil, nil, fmt.Errorf("history store is not available")
	}

	// Validate query length
	if len(input.Query) > 500 {
		return nil, nil, fmt.Errorf("query too long (max 500 characters)")
	}

	page := input.Page
	if page < 1 {
		page = 1
	}
	pageSize := input.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		return nil, nil, fmt.Errorf("page_size too large (max 100)")
	}

	offset := (page - 1) * pageSize

	var entries []models.TimelineEntry
	var total int
	var err error

	// If query is empty, return all history (same as history_get)
	if input.Query == "" {
		entries, total, err = s.historyStore.GetHistory(offset, pageSize)
	} else {
		entries, total, err = s.historyStore.SearchHistory(input.Query, offset, pageSize)
	}

	if err != nil {
		return nil, nil, fmt.Errorf("searching history: %w", err)
	}

	var sb strings.Builder
	if input.Query == "" {
		sb.WriteString(fmt.Sprintf("📚 Reading History (Page %d, %d total entries)\n\n", page, total))
	} else {
		sb.WriteString(fmt.Sprintf("🔍 Search Results for \"%s\" (%d total matches)\n\n", input.Query, total))
	}

	for _, entry := range entries {
		date := time.UnixMilli(entry.Timestamp).Format("2006-01-02")
		sb.WriteString(fmt.Sprintf("📖 %s by %s\n", entry.Title, entry.Author))
		sb.WriteString(fmt.Sprintf("   Activity: %s on %s\n", entry.Activity, date))
		if entry.Library != "" {
			sb.WriteString(fmt.Sprintf("   Library: %s\n", entry.Library))
		}
		if entry.Format != "" {
			sb.WriteString(fmt.Sprintf("   Format: %s\n", entry.Format))
		}
		sb.WriteString("\n")
	}

	if total == 0 && input.Query != "" {
		sb.WriteString("No matches found in your reading history.\n")
	} else if total > pageSize {
		totalPages := (total + pageSize - 1) / pageSize
		if page < totalPages {
			sb.WriteString(fmt.Sprintf("Page %d of %d - use page=%d for more\n", page, totalPages, page+1))
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: sb.String(),
			},
		},
	}, map[string]any{"query": input.Query, "page": page, "page_size": pageSize, "total": total}, nil
}

func (s *Server) handleGetHistoryStats(ctx context.Context, req *mcp.CallToolRequest, input GetHistoryStatsInput) (*mcp.CallToolResult, any, error) {
	if s.historyStore == nil {
		return nil, nil, fmt.Errorf("history store is not available")
	}

	stats, err := s.historyStore.GetStats()
	if err != nil {
		return nil, nil, fmt.Errorf("getting stats: %w", err)
	}

	yearlyStats, err := s.historyStore.GetYearlyStats()
	if err != nil {
		return nil, nil, fmt.Errorf("getting yearly stats: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("📊 Reading Statistics\n\n")

	if total, ok := stats["total_entries"].(int); ok {
		sb.WriteString(fmt.Sprintf("Total Entries: %d\n", total))
	}
	if unique, ok := stats["unique_borrows"].(int); ok {
		sb.WriteString(fmt.Sprintf("Unique Books Borrowed: %d\n", unique))
	}

	if first, ok := stats["first_activity"].(string); ok {
		if last, ok := stats["last_activity"].(string); ok {
			sb.WriteString(fmt.Sprintf("Date Range: %s to %s\n", first, last))
		}
	}

	sb.WriteString("\n📚 By Format:\n")
	if formats, ok := stats["by_format"].(map[string]int); ok {
		for format, count := range formats {
			sb.WriteString(fmt.Sprintf("  %s: %d\n", format, count))
		}
	}

	sb.WriteString("\n🏛️ By Library:\n")
	if libraries, ok := stats["by_library"].(map[string]int); ok {
		for lib, count := range libraries {
			sb.WriteString(fmt.Sprintf("  %s: %d\n", lib, count))
		}
	}

	sb.WriteString("\n📅 Yearly Breakdown:\n")
	for _, year := range yearlyStats {
		if y, ok := year["year"].(string); ok {
			if total, ok := year["total"].(int); ok {
				if borrowed, ok := year["borrowed"].(int); ok {
					sb.WriteString(fmt.Sprintf("  %s: %d entries (%d borrowed)\n", y, total, borrowed))
				}
			}
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: sb.String(),
			},
		},
	}, stats, nil
}

func (s *Server) handleExportForImport(ctx context.Context, req *mcp.CallToolRequest, input ExportForImportInput) (*mcp.CallToolResult, any, error) {
	if s.historyStore == nil {
		return nil, nil, fmt.Errorf("history store is not available")
	}

	// Set defaults
	activity := input.Activity
	if activity == "" {
		activity = "Returned"
	}

	// Build query with filters
	query := fmt.Sprintf("activity:%s", activity)
	if input.Format != "" {
		query += fmt.Sprintf(" format:%s", input.Format)
	}

	// Fetch entries
	offset := 0
	limit := input.Limit
	if limit <= 0 {
		limit = 10000 // Large default to get all books
	}

	entries, _, err := s.historyStore.SearchHistory(query, offset, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("searching history: %w", err)
	}

	if len(entries) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: "No entries found matching the filters",
				},
			},
		}, map[string]any{"exported_count": 0, "file_path": ""}, nil
	}

	// Generate CSV file
	importer := history.NewImporter(s.historyStore)
	filePath, count, err := importer.ExportForImport(entries, activity)
	if err != nil {
		return nil, nil, fmt.Errorf("exporting to CSV: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("✅ Exported %d books to CSV file\n\n", count))
	sb.WriteString(fmt.Sprintf("📁 File: %s\n\n", filePath))
	sb.WriteString("Import to Hardcover:\n")
	sb.WriteString("1. Go to https://hardcover.app\n")
	sb.WriteString("2. Settings → Import Books\n")
	sb.WriteString("3. Choose 'Goodreads' format\n")
	sb.WriteString("4. Upload the CSV file\n")

	return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: sb.String(),
				},
			},
		}, map[string]any{
			"exported_count": count,
			"file_path":      filePath,
			"activity":       activity,
			"format":         input.Format,
		}, nil
}
