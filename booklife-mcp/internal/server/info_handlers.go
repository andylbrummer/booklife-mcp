package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// InfoInput for the info tool (progressive discovery)
type InfoInput struct {
	Category string `json:"category,omitempty"` // Optional category filter
	Tool     string `json:"tool,omitempty"`     // Optional tool detail
	Workflow string `json:"workflow,omitempty"` // Optional workflow guide
}

func (s *Server) handleInfo(ctx context.Context, req *mcp.CallToolRequest, input InfoInput) (*mcp.CallToolResult, any, error) {
	// No arguments: Show overview
	if input.Category == "" && input.Tool == "" && input.Workflow == "" {
		return s.handleInfoOverview()
	}

	// Category: Show tools in category
	if input.Category != "" {
		return s.handleInfoCategory(input.Category)
	}

	// Tool: Show detailed tool help
	if input.Tool != "" {
		return s.handleInfoTool(input.Tool)
	}

	// Workflow: Show workflow guide
	if input.Workflow != "" {
		return s.handleInfoWorkflow(input.Workflow)
	}

	return nil, nil, fmt.Errorf("invalid info request")
}

func (s *Server) handleInfoOverview() (*mcp.CallToolResult, any, error) {
	var sb strings.Builder
	sb.WriteString("BookLife MCP - Your Personal Reading Assistant\n\n")

	sb.WriteString("Categories:\n")
	sb.WriteString("  hardcover (3 tools)    - Reading tracker and library management\n")
	sb.WriteString("  libby (6 tools)        - Library access via OverDrive\n")
	sb.WriteString("  tbr (6 tools)          - Unified to-be-read list management\n")
	sb.WriteString("  booklife (2 tools)     - Unified cross-provider actions\n")
	sb.WriteString("  history (4 tools)      - Reading history and statistics\n")
	sb.WriteString("  enrichment (2 tools)   - Metadata enrichment\n")
	sb.WriteString("  sync (1 tool)          - Universal sync operations\n")
	sb.WriteString("  profile (1 tool)       - Reading profile and preferences\n")
	sb.WriteString("  recommendation (1 tool) - Content-based book recommendations\n\n")

	sb.WriteString("Common Workflows:\n")
	sb.WriteString("  → Use workflow=\"find_and_read\" for discovery flow\n")
	sb.WriteString("  → Use workflow=\"sync_history\" for history management\n")
	sb.WriteString("  → Use workflow=\"tbr_management\" for managing your reading list\n")
	sb.WriteString("  → Use workflow=\"recommendations\" for personalized picks\n\n")

	sb.WriteString("Quick Start:\n")
	sb.WriteString("  1. Check library: libby_get_loans\n")
	sb.WriteString("  2. Find a book: booklife_find_book_everywhere query=\"...\"\n")
	sb.WriteString("  3. Get recommendations: profile_get\n\n")

	sb.WriteString("→ Use info category=\"hardcover\" for tools in category\n")
	sb.WriteString("→ Use info tool=\"libby_place_hold\" for detailed help\n")
	sb.WriteString("→ Use info workflow=\"find_and_read\" for workflow guide\n")

	return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: sb.String()},
			},
		}, map[string]any{
			"categories": []string{"hardcover", "libby", "tbr", "booklife", "history", "enrichment", "sync", "profile", "recommendation"},
			"workflows":  []string{"find_and_read", "sync_history", "tbr_management", "recommendations"},
			"tool_count": 27,
			"_meta": map[string]any{
				"has_results":         true,
				"action_needed":       false,
				"suggested_next":      []string{"info"},
				"automation_friendly": false,
			},
		}, nil
}

func (s *Server) handleInfoCategory(category string) (*mcp.CallToolResult, any, error) {
	categories := map[string][]string{
		"hardcover": {
			"hardcover_get_my_library - Get your reading list",
			"hardcover_update_reading_status - Update status/progress/rating",
			"hardcover_add_to_library - Add book to library",
		},
		"libby": {
			"libby_search - Search library catalog (includes availability)",
			"libby_get_loans - Get current loans",
			"libby_get_holds - Get hold queue",
			"libby_place_hold - Place library hold",
			"libby_sync_tag_metadata - Sync tag metadata to cache",
			"libby_tag_metadata_list - List cached tag metadata",
		},
		"tbr": {
			"tbr_list - List unified TBR from all sources",
			"tbr_search - Search TBR books",
			"tbr_add - Add book to TBR",
			"tbr_remove - Remove book from TBR",
			"tbr_sync - Sync TBR from Hardcover/Libby",
			"tbr_stats - Get TBR statistics",
		},
		"booklife": {
			"booklife_find_book_everywhere - Search all sources for a book",
			"booklife_best_way_to_read - Determine best way to access a book",
		},
		"history": {
			"history_import_timeline - Import Libby reading history",
			"history_sync_current_loans - Sync current loans to local store",
			"history_get - Get reading history with pagination and search",
			"history_stats - Get reading statistics",
		},
		"enrichment": {
			"enrichment_enrich_history - Enrich history with metadata (background job)",
			"enrichment_status - Query enrichment job progress",
		},
		"sync": {
			"sync - Universal sync for history, enrichment, and tag metadata",
		},
		"profile": {
			"profile_get - Get your reading profile and preferences",
		},
		"recommendation": {
			"book_find_similar - Find similar books based on content",
		},
	}

	tools, ok := categories[category]
	if !ok {
		validCategories := []string{"hardcover", "libby", "tbr", "booklife", "history", "enrichment", "sync", "profile", "recommendation"}
		return nil, nil, fmt.Errorf("unknown category: %s (valid: %s)", category, strings.Join(validCategories, ", "))
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Category: %s\n\n", category))
	sb.WriteString("Tools:\n")
	for _, tool := range tools {
		sb.WriteString(fmt.Sprintf("  %s\n", tool))
	}
	sb.WriteString("\n→ Use info tool=\"tool_name\" for detailed help\n")

	return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: sb.String()},
			},
		}, map[string]any{
			"category": category,
			"tools":    tools,
			"_meta": map[string]any{
				"has_results":         true,
				"action_needed":       false,
				"suggested_next":      []string{"info"},
				"automation_friendly": false,
			},
		}, nil
}

func (s *Server) handleInfoTool(tool string) (*mcp.CallToolResult, any, error) {
	toolHelp := map[string]string{
		"hardcover_search_books": `
Tool: hardcover_search_books

Search for books in the Hardcover catalog.

Parameters:
  query (required)    - Search query (title, author, ISBN)
  page (optional)     - Page number (default: 1)
  page_size (optional) - Items per page (default: 20, max: 100)
  sort_by (optional)  - Sort: relevance, rating, date, title
  format (optional)   - Filter: ebook, audiobook, physical
  min_rating (optional) - Minimum community rating (0-5)
  genre (optional)    - Filter by genre

Returns:
  - Book details with IDs for cross-tool usage
  - book_id - for hardcover_update_reading_status
  - isbn - for libby_check_availability

Example:
  {"query": "Project Hail Mary"}
  {"query": "Andy Weir", "sort_by": "rating", "page_size": 5}
`,
		"libby_place_hold": `
Tool: libby_place_hold

Place a hold on a library ebook or audiobook.

Parameters:
  media_id (required)   - Media ID from libby_search or libby_check_availability
  format (required)     - "ebook" or "audiobook"
  auto_borrow (optional) - Auto-borrow when available (default: false)

Returns:
  - Hold confirmation
  - Position in queue
  - Estimated wait time

Example:
  {"media_id": "12345", "format": "ebook"}
  {"media_id": "12345", "format": "audiobook", "auto_borrow": true}

Prerequisites:
  - Get media_id from libby_search or libby_check_availability
  - Must have library card configured
`,
		"sync": `
Tool: sync (Progressive Disclosure)

Sync reading history between services (Libby → Hardcover).

Actions (progressive disclosure):
  status (default) - Show pending count and last sync
  preview          - List books that will be synced
  run              - Execute sync, show summary
  details          - Show sync state for specific entry
  unmatched        - Show books that failed to match

Parameters:
  action (optional)        - Action to perform (default: "status")
  target (optional)        - Target system (default: "hardcover")
  entry_id (optional)      - For "details" action
  limit (optional)         - For "run" action: limit entries
  dry_run (optional)       - For "run" action: test without changes
  unmatched_type (optional) - For "unmatched" action: isbn, no_isbn, all

Typical Flow:
  1. sync action="status"    - Check pending count
  2. sync action="preview"   - See what will sync
  3. sync action="run"       - Execute sync
  4. sync action="details"   - Review results

Example:
  {} or {"action": "status"}  - Quick status check
  {"action": "preview"}       - See what will sync
  {"action": "run"}           - Sync returned books to Hardcover
  {"action": "run", "dry_run": true} - Test without changes
`,
	}

	help, ok := toolHelp[tool]
	if !ok {
		return nil, nil, fmt.Errorf("no detailed help available for tool: %s\nTip: Use info category=\"...\" to browse tools by category", tool)
	}

	return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: help},
			},
		}, map[string]any{
			"tool": tool,
			"_meta": map[string]any{
				"has_results":         true,
				"action_needed":       false,
				"automation_friendly": false,
			},
		}, nil
}

func (s *Server) handleInfoWorkflow(workflow string) (*mcp.CallToolResult, any, error) {
	workflows := map[string]string{
		"find_and_read": `
Workflow: Finding and Reading a Book

1. Search for a book:
   booklife_find_book_everywhere query="The Name of the Wind"

2. Check library availability in results:
   Look for "Library Availability" section with media_id

3. If available at library:
   libby_place_hold media_id="12345" format="ebook"

4. If not available, add to TBR:
   booklife_add_to_tbr isbn="9780756404741" place_hold=true

5. Track in Hardcover:
   hardcover_add_to_library isbn="9780756404741" status="want-to-read"

Tips:
  - Use booklife_find_book_everywhere to search all sources at once
  - Library holds are free - always check availability first
  - place_hold=true in add_to_tbr will auto-hold if available
`,
		"sync_history": `
Workflow: Syncing Reading History (Libby → Hardcover)

1. Import your Libby timeline (one-time setup):
   history_import_timeline url="https://share.libbyapp.com/..."

   Get timeline URL:
   - Open Libby app > Settings > Reading History
   - Tap "Export Timeline" > Copy link

2. Check sync status:
   sync action="status"

3. Preview what will sync:
   sync action="preview"

4. Run the sync:
   sync action="run"

5. Check results:
   sync action="details"

What Gets Synced:
  - Returned books marked as "read" in Hardcover
  - Preserves original return date
  - Skips books already in Hardcover

Tips:
  - Use dry_run=true to test before syncing
  - Check "unmatched" to see books that need manual review
  - Sync runs incrementally - only new returns
`,
		"tbr_management": `
Workflow: Managing Your Unified TBR List

1. Initial setup - sync from all sources:
   tbr_sync action="sync_all"

   This pulls in:
   - Hardcover "want-to-read" list
   - Libby holds and tagged books
   - Keeps them in sync locally

2. View your TBR overview:
   tbr_stats

   Shows:
   - Total books by source
   - Available at library count
   - Priority breakdown
   - Format distribution

3. Browse your TBR:
   tbr_list source="all" page_size=20

   Filter options:
   - source: physical, hardcover, libby, all
   - tag: filter by Libby tags
   - available: true (library books ready now)

4. Search within TBR:
   tbr_search query="science fiction" source="all"

5. Add books manually:
   tbr_add title="Project Hail Mary" author="Andy Weir" source="physical" priority=1

   Or add with auto-hold at library:
   booklife_add_to_tbr isbn="9780593135204" place_hold=true

6. Remove finished books:
   tbr_remove tbr_id=123

Tips:
  - Use tbr_sync regularly to keep Hardcover/Libby in sync
  - Filter by available=true to see what you can borrow now
  - Libby tags automatically sync full book metadata
  - Priority levels: 0=normal, 1=high, 2=urgent
`,
		"recommendations": `
Workflow: Getting Personalized Recommendations

1. Enrich your history (one-time setup):
   enrichment_enrich_history

   This background job:
   - Fetches descriptions from Open Library
   - Extracts themes, topics, mood
   - Takes ~1-2 seconds per book
   - Can be cancelled anytime

2. Monitor progress:
   enrichment_status

3. Get your reading profile:
   profile_get

   Shows:
   - Format preferences (ebook vs audiobook)
   - Top genres and authors
   - Reading cadence and streaks
   - Completion rate

4. Find similar books:
   book_find_similar title="Project Hail Mary" author="Andy Weir"

   Uses content-based matching:
   - Themes and topics
   - Mood and complexity
   - Similar writing style

5. Use prompts for interactive recommendations:
   what_should_i_read mood="light sci-fi" format="audiobook"

Tips:
  - Enrichment is required for content-based recommendations
  - Use force=true to re-enrich all books
  - Profile updates automatically as you read
`,
	}

	guide, ok := workflows[workflow]
	if !ok {
		validWorkflows := []string{"find_and_read", "sync_history", "tbr_management", "recommendations"}
		return nil, nil, fmt.Errorf("unknown workflow: %s (valid: %s)", workflow, strings.Join(validWorkflows, ", "))
	}

	return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: guide},
			},
		}, map[string]any{
			"workflow": workflow,
			"_meta": map[string]any{
				"has_results":         true,
				"action_needed":       false,
				"automation_friendly": false,
			},
		}, nil
}
