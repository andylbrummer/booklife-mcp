package server

import (
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/user/booklife-mcp/internal/models"
)

// ===== Shared Types =====

// PaginationParams provides common pagination for list requests
// Page is 1-indexed (default: 1)
// PageSize is items per page (default: 20, max: 100)
type PaginationParams struct {
	Page     int `json:"page,omitempty"`
	PageSize int `json:"page_size,omitempty"`
}

// getPagination returns page (default 1) and pageSize (default 20, max 100)
func getPagination(params PaginationParams) (page, pageSize int) {
	page = params.Page
	if page < 1 {
		page = 1
	}

	pageSize = params.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	return page, pageSize
}

// offset returns the calculated offset for pagination
func (p PaginationParams) offset() int {
	page, pageSize := getPagination(p)
	return (page - 1) * pageSize
}

// calculatePagedResult creates a PagedResult from page, pageSize, and totalCount
func calculatePagedResult(page, pageSize, totalCount int) PagedResult {
	totalPages := (totalCount + pageSize - 1) / pageSize
	if totalPages == 0 {
		totalPages = 1
	}

	return PagedResult{
		Page:       page,
		PageSize:   pageSize,
		TotalCount: totalCount,
		TotalPages: totalPages,
		HasNext:    page < totalPages,
		HasPrev:    page > 1,
	}
}

// PagedResult contains pagination metadata for list responses
type PagedResult struct {
	Page       int  `json:"page"`
	PageSize   int  `json:"page_size"`
	TotalCount int  `json:"total_count"`
	TotalPages int  `json:"total_pages"`
	HasNext    bool `json:"has_next"`
	HasPrev    bool `json:"has_prev"`
}

// formatPagingFooter adds pagination info to the output
// itemCount is the number of items on the current page
func formatPagingFooter(paged PagedResult, itemCount int) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n--- Page %d of %d (%d items shown, %d total) ---",
		paged.Page, paged.TotalPages, itemCount, paged.TotalCount))
	if paged.HasPrev {
		sb.WriteString(" (prev page available)")
	}
	if paged.HasNext {
		sb.WriteString(" (next page available)")
	}
	return sb.String()
}

// ===== Format Helpers for Cross-Tool Data Roundtripping =====

// createResponseMeta creates automation metadata for AI agents
// This helps AI agents understand the response and suggest next actions
// Follows MCP best practices with nested automation structure
func createResponseMeta(hasResults bool, actionNeeded bool, suggestedNext []string, automationFriendly bool, confidence float64) map[string]any {
	automation := map[string]any{}

	if len(suggestedNext) > 0 {
		automation["next_actions"] = suggestedNext
	}

	if confidence > 0 {
		automation["confidence"] = confidence
	}

	return map[string]any{
		"automation": automation,
	}
}

// createSearchMeta creates standardized automation metadata for search tools
func createSearchMeta(resultCount int, truncated bool, nextActions []string) map[string]any {
	automation := map[string]any{
		"result_count": resultCount,
		"truncated":    truncated,
	}

	if len(nextActions) > 0 {
		automation["next_actions"] = nextActions
	}

	return map[string]any{
		"automation": automation,
	}
}

// createOperationMeta creates standardized automation metadata for operations (add, remove, update)
func createOperationMeta(success bool, confirmable bool, destructive bool, nextActions []string) map[string]any {
	automation := map[string]any{
		"success":     success,
		"confirmable": confirmable,
	}

	if destructive {
		automation["destructive"] = true
		automation["reversible"] = false
	}

	if len(nextActions) > 0 {
		automation["next_actions"] = nextActions
	}

	return map[string]any{
		"automation": automation,
	}
}

// formatIDsForDisplay creates a unified ID format for cross-tool usage
// Format: "IDs: { book_id: 123, isbn: 9780593135204 }"
func formatIDsForDisplay(idMap map[string]string, indent string) string {
	if len(idMap) == 0 {
		return ""
	}

	var pairs []string
	// Order: book_id, isbn, media_id, then others alphabetically
	preferredOrder := []string{"book_id", "isbn", "media_id"}

	for _, key := range preferredOrder {
		if value, ok := idMap[key]; ok && value != "" {
			pairs = append(pairs, fmt.Sprintf("%s: %s", key, value))
		}
	}

	// Add any remaining IDs
	for key, value := range idMap {
		isPreferred := false
		for _, pk := range preferredOrder {
			if key == pk {
				isPreferred = true
				break
			}
		}
		if !isPreferred && value != "" {
			pairs = append(pairs, fmt.Sprintf("%s: %s", key, value))
		}
	}

	if len(pairs) == 0 {
		return ""
	}

	return fmt.Sprintf("%sIDs: { %s }\n", indent, strings.Join(pairs, ", "))
}

// formatBookForDisplay creates a detailed, human-readable representation with actionable IDs
// Returns text that shows all identifiers needed for cross-tool usage:
// - book_id → for update_reading_status, get_my_library
// - isbn → for add_to_library, check_availability
func formatBookForDisplay(book models.Book, index int) string {
	var sb strings.Builder

	// Header with index
	if index > 0 {
		sb.WriteString(fmt.Sprintf("[%d] ", index))
	}

	// Title line with subtitle
	sb.WriteString(book.Title)
	if book.Subtitle != "" {
		sb.WriteString(fmt.Sprintf(": %s", book.Subtitle))
	}
	sb.WriteString("\n")

	// Authors
	if len(book.Authors) > 0 {
		authorNames := make([]string, 0, len(book.Authors))
		for _, a := range book.Authors {
			authorNames = append(authorNames, a.Name)
		}
		sb.WriteString(fmt.Sprintf("    by %s\n", strings.Join(authorNames, ", ")))
	}

	// === CRITICAL: Identifiers for cross-tool usage ===
	idMap := make(map[string]string)
	if book.HardcoverID != "" {
		idMap["book_id"] = book.HardcoverID
	}
	if book.ISBN13 != "" {
		idMap["isbn"] = book.ISBN13
	} else if book.ISBN10 != "" {
		idMap["isbn"] = book.ISBN10
	}
	if idStr := formatIDsForDisplay(idMap, "    "); idStr != "" {
		sb.WriteString(idStr)
	}

	// User status if available
	if book.UserStatus != nil {
		statusInfo := book.UserStatus.Status
		if book.UserStatus.Progress > 0 {
			statusInfo += fmt.Sprintf(" (%d%%)", book.UserStatus.Progress)
		}
		if book.UserStatus.Rating > 0 {
			statusInfo += fmt.Sprintf(" ⭐ %.1f", book.UserStatus.Rating)
		}
		sb.WriteString(fmt.Sprintf("    Status: %s\n", statusInfo))
	}

	// Series info
	if book.Series != nil {
		series := book.Series.Name
		if book.Series.Position > 0 {
			series += fmt.Sprintf(" #%g", book.Series.Position)
		}
		if book.Series.Total > 0 {
			series += fmt.Sprintf(" of %d", book.Series.Total)
		}
		sb.WriteString(fmt.Sprintf("    Series: %s\n", series))
	}

	// Publication info
	if book.Publisher != "" || book.PublishedDate != "" {
		pubInfo := ""
		if book.Publisher != "" {
			pubInfo = book.Publisher
		}
		if book.PublishedDate != "" {
			if pubInfo != "" {
				pubInfo += ", "
			}
			pubInfo += book.PublishedDate
		}
		sb.WriteString(fmt.Sprintf("    Publisher: %s\n", pubInfo))
	}

	// Page count
	if book.PageCount > 0 {
		sb.WriteString(fmt.Sprintf("    Pages: %d\n", book.PageCount))
	}

	// Genres
	if len(book.Genres) > 0 {
		sb.WriteString(fmt.Sprintf("    Genres: %s\n", strings.Join(book.Genres, ", ")))
	}

	// Community rating
	if book.HardcoverRating > 0 {
		rating := fmt.Sprintf("⭐ %.1f", book.HardcoverRating)
		if book.HardcoverCount > 0 {
			rating += fmt.Sprintf(" (%d ratings)", book.HardcoverCount)
		}
		sb.WriteString(fmt.Sprintf("    Community Rating: %s\n", rating))
	}

	// Library availability
	if book.LibraryAvailability != nil {
		sb.WriteString(formatLibraryAvailabilityForDisplay(book.LibraryAvailability, "    "))
	}

	return sb.String()
}

// formatLibraryAvailabilityForDisplay shows detailed availability with media_id
func formatLibraryAvailabilityForDisplay(avail *models.LibraryAvailability, indent string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("%sLibrary Availability (%s):\n", indent, avail.LibraryName))

	// === CRITICAL: media_id for place_hold ===
	idMap := map[string]string{"media_id": avail.MediaID}
	sb.WriteString(formatIDsForDisplay(idMap, indent+"  "))

	if avail.EbookAvailable {
		sb.WriteString(fmt.Sprintf("%s  ✅ Ebook: Available now (%d copies)\n", indent, avail.EbookCopies))
	} else if avail.EbookWaitlistSize > 0 {
		sb.WriteString(fmt.Sprintf("%s  📚 Ebook: %d in wait list\n", indent, avail.EbookWaitlistSize))
	}

	if avail.AudiobookAvailable {
		sb.WriteString(fmt.Sprintf("%s  ✅ Audiobook: Available now (%d copies)\n", indent, avail.AudiobookCopies))
	} else if avail.AudiobookWaitlistSize > 0 {
		sb.WriteString(fmt.Sprintf("%s  🎧 Audiobook: %d in wait list\n", indent, avail.AudiobookWaitlistSize))
	}

	if avail.EstimatedWaitDays > 0 {
		sb.WriteString(fmt.Sprintf("%s  Estimated wait: ~%d days\n", indent, avail.EstimatedWaitDays))
	}

	return sb.String()
}

// formatAuthorsList formats authors for display
func formatAuthorsList(authors []models.Contributor) string {
	if len(authors) == 0 {
		return "Unknown"
	}
	names := make([]string, 0, len(authors))
	for _, a := range authors {
		names = append(names, a.Name)
	}
	return strings.Join(names, ", ")
}

// registerTools registers all BookLife tools with the MCP server
func (s *Server) registerTools() {
	// === Info/Help (Progressive Discovery) ===

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name: "info",
		Description: `Get help and discover BookLife capabilities.
Progressive discovery system for tools, workflows, and categories.
Example: {} - Show overview
Example: {"category": "hardcover"} - List Hardcover tools
Example: {"tool": "libby_place_hold"} - Detailed tool help
Example: {"workflow": "find_and_read"} - Step-by-step workflow guide`,
	}, s.handleInfo)

	// === Hardcover (Reading Tracker) ===

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name: "hardcover_get_my_library",
		Description: `Get your reading list from Hardcover.
Progressive detail levels for token efficiency.
Example: {"detail": "summary"} - quick stats (200 tokens)
Example: {"status": "reading"} - currently reading (list mode)
Example: {"status": "want-to-read"} - TBR list
Example: {"status": "read", "page_size": 10} - recently finished`,
	}, s.handleGetMyLibrary)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name: "hardcover_update_reading_status",
		Description: `Update a book's status, progress, or rating in Hardcover.
Requires book_id from search_books or get_my_library.
Example: {"book_id": "123", "status": "reading", "progress": 50}
Example: {"book_id": "123", "status": "read", "rating": 4.5}`,
	}, s.handleUpdateReadingStatus)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name: "hardcover_add_to_library",
		Description: `Add a book to your Hardcover library.
Can use ISBN from search_books or title/author.
Example: {"isbn": "9780593135204", "status": "want-to-read"}
Example: {"title": "Project Hail Mary", "author": "Andy Weir"}`,
	}, s.handleAddToLibrary)

	// === Libby (Library Access) ===

	if s.libby != nil {
		mcp.AddTool(s.mcpServer, &mcp.Tool{
			Name: "libby_search",
			Description: `Search library catalog via Libby for ebooks and audiobooks.
Returns detailed results with media_id for placing holds.
Example: {"query": "Project Hail Mary"}
Example: {"query": "Brandon Sanderson", "available": true}`,
		}, s.handleSearchLibrary)

		mcp.AddTool(s.mcpServer, &mcp.Tool{
			Name: "libby_get_loans",
			Description: `Get your current Libby loans with due dates.
Returns detailed loan information.
Example: {} - returns all current checkouts`,
		}, s.handleGetLoans)

		mcp.AddTool(s.mcpServer, &mcp.Tool{
			Name: "libby_get_holds",
			Description: `Get your current library holds and queue position.
Returns detailed hold information with media_id.
Example: {} - returns all active holds`,
		}, s.handleGetHolds)

		mcp.AddTool(s.mcpServer, &mcp.Tool{
			Name: "libby_place_hold",
			Description: `Place a hold on a library ebook or audiobook.
Requires media_id from libby_search or libby_check_availability.
Example: {"media_id": "12345", "format": "ebook"}
Example: {"media_id": "12345", "format": "audiobook", "auto_borrow": true}`,
		}, s.handlePlaceHold)

		mcp.AddTool(s.mcpServer, &mcp.Tool{
			Name: "libby_sync_tag_metadata",
			Description: `Sync full book information for all Libby tagged books to local cache.

Purpose: Fetch complete metadata (title, author, ISBN, cover, format, availability)
for all books you've tagged in Libby and store it locally for offline access.

This is useful for:
  - Browsing tagged books offline
  - Building reading lists and recommendations
  - Organizing your library collection
  - Cross-referencing with other services

The sync fetches data from current loans and holds, so books you've returned
may have limited metadata unless they're still in your holds.

Example: {} - Sync all tagged books

After syncing, use libby_tag_metadata_list to browse the cached data.`,
		}, s.handleLibbyTagMetadataSync)

		mcp.AddTool(s.mcpServer, &mcp.Tool{
			Name: "libby_tag_metadata_list",
			Description: `List cached Libby tag metadata with full book information.

Shows locally cached book details for all tagged items.
This data is populated by libby_sync_tag_metadata.

Filters:
  tag (optional): Show only books with this tag

Pagination:
  page (default: 1)
  page_size (default: 20, max: 100)

Examples:
  {} - List all cached tagged books
  {"tag": "favorites"} - Only books tagged as favorites
  {"tag": "sci-fi", "page_size": 50} - First 50 sci-fi tagged books`,
		}, s.handleLibbyTagMetadataList)
	}

	// === Unified Actions ===

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name: "booklife_find_book_everywhere",
		Description: `Search all sources for a book and show availability.
Returns comprehensive availability with all actionable IDs.
Example: {"query": "The Name of the Wind"}`,
	}, s.handleFindBookEverywhere)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name: "booklife_best_way_to_read",
		Description: `Determine the best way to access a book (library, bookstore, etc.).
Returns prioritized options with identifiers.
Example: {"isbn": "9780756404741"}`,
	}, s.handleBestWayToRead)

	// === Local History Store ===

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name: "history_import_timeline",
		Description: `Import Libby reading history from a timeline export URL.
This imports your complete Libby reading history into the local store.
Example: {"url": "https://share.libbyapp.com/data/{uuid}/libbytimeline-all-loans.json"}`,
	}, s.handleImportTimeline)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name: "history_sync_current_loans",
		Description: `Sync current Libby loans to local history store.
This captures your current checkouts for historical tracking.
Example: {}`,
	}, s.handleSyncCurrentLoans)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name: "history_get",
		Description: `Get reading history from local store with pagination.
Returns all imported timeline entries and synced loans.
Supports optional query parameter for searching.
Example: {"page": 1, "page_size": 20}
Example: {"query": "Sanderson", "page": 1}`,
	}, s.handleGetLocalHistory)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name: "history_stats",
		Description: `Get reading statistics from local history store.
Returns breakdowns by format, library, year, and totals.
Example: {}`,
	}, s.handleGetHistoryStats)

	// === Sync (Progressive Disclosure) ===

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name: "sync",
		Description: `Universal sync for reading history, enrichment, and Libby tag metadata.

Flow: Libby → Local ↔ Hardcover
  1. Sync Libby history to local database
  2. Mark returned books as "read" in Hardcover
  3. Enrich books with metadata from Open Library/Google Books
  4. Cache full book information for Libby tagged items

Actions (progressive disclosure):
  status   - Show pending count and last sync (default)
  preview  - List books that will be synced
  run      - Execute history sync only
  sync_all - Comprehensive sync (history + enrichment + tag metadata)
  details  - Show sync state for specific entry
  unmatched - Show books that failed to match

Examples:
  {} or {"action": "status"} - quick status check
  {"action": "preview"} - see what will sync
  {"action": "run"} - sync returned books to Hardcover
  {"action": "sync_all"} - full comprehensive sync (recommended)
  {"action": "sync_all", "dry_run": true} - test without changes
  {"action": "details", "entry_id": "abc123"} - entry details`,
	}, s.handleSync)

	// === Content-Based Recommendations (LLM-Powered) ===

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name: "book_find_similar",
		Description: `Find books similar to a given book based on themes, topics, and mood.
Content-based recommendation using enriched metadata.

Example: {"title": "Project Hail Mary", "author": "Andy Weir", "limit": 10}`,
	}, s.handleFindSimilarBooks)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name: "profile_get",
		Description: `Get your reading profile with preferences and patterns.
Shows format preferences, top genres, most-read authors, completion rate, reading cadence, and streaks.

Example: {}`,
	}, s.handleGetReadingProfile)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name: "enrichment_enrich_history",
		Description: `Enriches reading history with metadata from Open Library and Google Books API.

Purpose: Fetches book descriptions, themes, topics, mood, and series data. Required before using content-based recommendation tools (find_similar_books, get_book_with_analysis).

Behavior: Processes entire library asynchronously in background job. Sends progress notifications via MCP. Already-enriched books are skipped unless force=true.

Parameters:
  force (boolean, optional): Re-enrich all books, even if already enriched. Default: false

Returns: Job ID, total books count, initial status

Use enrichment_status to monitor progress after starting.`,
	}, s.handleEnrichHistory)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name: "enrichment_status",
		Description: `Queries progress and status of an enrichment job.

Purpose: Monitor background enrichment operations with detailed metrics.

Parameters:
  job_id (string, optional): Specific job to query. Defaults to most recent job if omitted.

Returns: Job status (pending/running/completed/failed/cancelled), processed/successful/failed book counts, current book being processed, elapsed time, estimated time remaining, average time per book, recent errors (last 10).

Use this to check progress if MCP notifications are not visible or for historical job data.`,
	}, s.handleEnrichmentStatus)

	// === Unified TBR (To Be Read) Management ===

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name: "tbr_list",
		Description: `List your unified TBR (to-be-read) list.
Combines books from Hardcover, Libby holds/tags, and manually added physical books.

Filters:
  source (optional): Filter by source - "physical", "hardcover", "libby"

Pagination:
  page (default: 1)
  page_size (default: 20, max: 100)

Examples:
  {} - List all TBR books
  {"source": "libby"} - Only Libby books
  {"source": "hardcover", "page_size": 50} - First 50 Hardcover TBR books`,
	}, s.handleTBRList)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name: "tbr_search",
		Description: `Search your TBR list by title or author.

Parameters:
  query (required): Search term
  source (optional): Filter by source

Examples:
  {"query": "Sanderson"} - Find all Sanderson books in TBR
  {"query": "Mistborn", "source": "libby"} - Search only Libby TBR`,
	}, s.handleTBRSearch)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name: "tbr_add",
		Description: `Add a book to your TBR list manually (for physical books or custom entries).

Required:
  title, author

Optional:
  subtitle, isbn10, isbn13, publisher, published_date, page_count
  description, cover_url, genres (array)
  series_name, series_position
  notes, priority (0-10, higher = more important)
  source (default: "physical")

Examples:
  {"title": "The Name of the Wind", "author": "Patrick Rothfuss"}
  {"title": "Mistborn", "author": "Sanderson", "priority": 5, "notes": "Recommended by friend"}
  {"title": "Project Hail Mary", "author": "Andy Weir", "isbn13": "9780593135204", "source": "physical"}`,
	}, s.handleTBRAdd)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name: "tbr_remove",
		Description: `Remove a book from your TBR list.

Use either:
  id (from tbr_list or tbr_search)
  OR title + author

Examples:
  {"id": 42}
  {"title": "The Name of the Wind", "author": "Patrick Rothfuss"}`,
	}, s.handleTBRRemove)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name: "tbr_sync",
		Description: `Sync TBR from external sources (Hardcover, Libby).

Actions:
  sync_hardcover - Sync Hardcover TBR (want-to-read list)
  sync_libby_holds - Sync Libby holds to TBR
  sync_libby_tags - Sync Libby tagged books to TBR (with full metadata)
  sync_all - Sync everything

Examples:
  {"action": "sync_hardcover"}
  {"action": "sync_libby_tags"}
  {"action": "sync_all"}

Note: sync_libby_tags fetches full book information for all tagged books
and stores it locally for offline access.`,
	}, s.handleTBRSync)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name: "tbr_stats",
		Description: `Get statistics about your TBR list.

Shows:
  - Total books
  - Breakdown by source (physical, hardcover, libby)
  - Libby availability stats
  - Tagged books count

Example:
  {}`,
	}, s.handleTBRStats)
}
