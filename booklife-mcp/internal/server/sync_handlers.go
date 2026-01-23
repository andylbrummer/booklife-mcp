package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/user/booklife-mcp/internal/sync"
	"github.com/user/booklife-mcp/internal/tbr"
)

// SyncInput for the unified sync tool
type SyncInput struct {
	// Action: "status" (default), "run", "preview", "details", "unmatched"
	// - status: Show sync stats (pending count, last sync, errors)
	// - preview: Show what would be synced without running
	// - run: Execute sync and show summary
	// - details: Show detailed results from last sync or specific entry
	// - unmatched: Show books that failed to match (by ISBN or title)
	Action string `json:"action,omitempty"`

	// Target system to sync to (default: "hardcover")
	Target string `json:"target,omitempty"`

	// For "details" action: entry ID to get details for
	EntryID string `json:"entry_id,omitempty"`

	// For "run" action: limit number of entries to sync (default: all)
	Limit int `json:"limit,omitempty"`

	// For "run" action: dry run mode (default: false)
	DryRun bool `json:"dry_run,omitempty"`

	// For "unmatched" action: filter by type ("isbn", "no_isbn", or "all")
	UnmatchedType string `json:"unmatched_type,omitempty"`
}

func (s *Server) handleSync(ctx context.Context, req *mcp.CallToolRequest, input SyncInput) (*mcp.CallToolResult, any, error) {
	if s.historyStore == nil {
		return nil, nil, fmt.Errorf("history store is not available")
	}
	if s.hardcover == nil {
		return nil, nil, NewHardcoverNotConfiguredError()
	}

	action := input.Action
	if action == "" {
		action = "status"
	}

	target := input.Target
	if target == "" {
		target = "hardcover"
	}

	if target != "hardcover" {
		return nil, nil, fmt.Errorf("unsupported sync target: %s (only 'hardcover' supported)", target)
	}

	switch action {
	case "status":
		return s.handleSyncStatus(ctx, target)
	case "preview":
		return s.handleSyncPreview(ctx, target, input.Limit)
	case "run":
		return s.handleSyncRun(ctx, target, input.Limit, input.DryRun)
	case "details":
		return s.handleSyncDetails(ctx, target, input.EntryID)
	case "unmatched":
		return s.handleSyncUnmatched(ctx, target, input.UnmatchedType)
	case "sync_all":
		return s.handleSyncAll(ctx, target, input.Limit, input.DryRun)
	default:
		return nil, nil, NewInvalidActionError("sync", action, []string{"status", "preview", "run", "details", "unmatched", "sync_all"})
	}
}

// handleSyncStatus shows sync statistics (progressive disclosure level 1)
func (s *Server) handleSyncStatus(ctx context.Context, target string) (*mcp.CallToolResult, any, error) {
	stats, err := s.historyStore.GetSyncStats(target)
	if err != nil {
		return nil, nil, fmt.Errorf("getting sync stats: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Sync Status → %s\n\n", target))

	// Unsynced count
	if unsynced, ok := stats["unsynced_returns"].(int); ok {
		if unsynced > 0 {
			sb.WriteString(fmt.Sprintf("Pending: %d returned books to sync\n", unsynced))
			sb.WriteString("  → Use action=\"preview\" to see list\n")
			sb.WriteString("  → Use action=\"run\" to sync now\n\n")
		} else {
			sb.WriteString("All returned books are synced\n\n")
		}
	}

	// Status breakdown
	if byStatus, ok := stats["by_status"].(map[string]int); ok && len(byStatus) > 0 {
		sb.WriteString("History:\n")
		for status, count := range byStatus {
			sb.WriteString(fmt.Sprintf("  %s: %d\n", status, count))
		}
		sb.WriteString("\n")
	}

	// Last sync
	if lastSync, ok := stats["last_sync"].(string); ok {
		sb.WriteString(fmt.Sprintf("Last sync: %s\n", lastSync))
	} else {
		sb.WriteString("Last sync: never\n")
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: sb.String()},
		},
	}, stats, nil
}

// handleSyncPreview shows what would be synced (progressive disclosure level 2)
func (s *Server) handleSyncPreview(ctx context.Context, target string, limit int) (*mcp.CallToolResult, any, error) {
	entries, err := s.historyStore.GetUnsyncedReturns(target)
	if err != nil {
		return nil, nil, fmt.Errorf("getting unsynced entries: %w", err)
	}

	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Preview: %d books to sync → %s\n\n", len(entries), target))

	if len(entries) == 0 {
		sb.WriteString("No pending books to sync.\n")
	} else {
		for i, e := range entries {
			date := time.UnixMilli(e.Timestamp).Format("2006-01-02")
			sb.WriteString(fmt.Sprintf("%d. [%s] %s by %s\n", i+1, e.TitleID, e.Title, e.Author))
			sb.WriteString(fmt.Sprintf("   Returned: %s • Format: %s\n", date, e.Format))
			if e.ISBN != "" {
				sb.WriteString(fmt.Sprintf("   ISBN: %s\n", e.ISBN))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("→ Use action=\"run\" to sync these books\n")
		sb.WriteString("→ Use action=\"run\" dry_run=true to test without changes\n")
	}

	// Return structured data for programmatic use
	type previewEntry struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Author string `json:"author"`
		Date   string `json:"date"`
		Format string `json:"format"`
		ISBN   string `json:"isbn,omitempty"`
	}

	preview := make([]previewEntry, 0, len(entries))
	for _, e := range entries {
		preview = append(preview, previewEntry{
			ID:     e.TitleID,
			Title:  e.Title,
			Author: e.Author,
			Date:   time.UnixMilli(e.Timestamp).Format("2006-01-02"),
			Format: e.Format,
			ISBN:   e.ISBN,
		})
	}

	return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: sb.String()},
			},
		}, map[string]any{
			"count":   len(entries),
			"target":  target,
			"entries": preview,
		}, nil
}

// handleSyncRun executes the sync (progressive disclosure level 3)
func (s *Server) handleSyncRun(ctx context.Context, target string, limit int, dryRun bool) (*mcp.CallToolResult, any, error) {
	// Create the sync processor
	adapter := sync.NewStoreAdapter(s.historyStore)
	syncer := sync.NewHardcoverSync(s.hardcover, adapter)
	syncer.SetDryRun(dryRun)

	// Enable cross-format ISBN lookup via Libby
	if s.libby != nil {
		syncer.SetLibbySearcher(s.libby)
	}

	// Run sync
	summary, err := syncer.SyncReturnedBooks(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("sync failed: %w", err)
	}

	var sb strings.Builder
	if dryRun {
		sb.WriteString("Sync Preview (dry run - no changes made)\n\n")
	} else {
		sb.WriteString("Sync Complete\n\n")
	}

	sb.WriteString(fmt.Sprintf("Processed: %d books\n", summary.TotalProcessed))
	sb.WriteString(fmt.Sprintf("  Successful: %d\n", summary.Successful))
	sb.WriteString(fmt.Sprintf("  Skipped: %d\n", summary.Skipped))
	sb.WriteString(fmt.Sprintf("  Failed: %d\n", summary.Failed))

	if len(summary.Results) > 0 {
		sb.WriteString("\nResults:\n")
		for _, r := range summary.Results {
			status := "✓"
			detail := ""
			if r.Skipped {
				status = "⊘"
				detail = r.SkipReason
			} else if !r.Success {
				status = "✗"
				detail = r.ErrorMessage
			} else if r.TargetBookID != "" {
				detail = fmt.Sprintf("→ hardcover:%s", r.TargetBookID)
			}

			title := r.Operation.Title
			if len(title) > 40 {
				title = title[:37] + "..."
			}
			sb.WriteString(fmt.Sprintf("  %s %s", status, title))
			if detail != "" {
				sb.WriteString(fmt.Sprintf(" (%s)", detail))
			}
			sb.WriteString("\n")
		}
	}

	if len(summary.Errors) > 0 {
		sb.WriteString("\nErrors:\n")
		for _, e := range summary.Errors {
			sb.WriteString(fmt.Sprintf("  • %s\n", e))
		}
	}

	return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: sb.String()},
			},
		}, map[string]any{
			"dry_run":   dryRun,
			"processed": summary.TotalProcessed,
			"success":   summary.Successful,
			"skipped":   summary.Skipped,
			"failed":    summary.Failed,
			"errors":    summary.Errors,
		}, nil
}

// handleSyncDetails shows details for a specific entry (progressive disclosure level 4)
func (s *Server) handleSyncDetails(ctx context.Context, target, entryID string) (*mcp.CallToolResult, any, error) {
	if entryID == "" {
		// Show recent sync activity
		stats, err := s.historyStore.GetSyncStats(target)
		if err != nil {
			return nil, nil, fmt.Errorf("getting sync stats: %w", err)
		}

		var sb strings.Builder
		sb.WriteString("Sync Details\n\n")
		sb.WriteString("Provide entry_id to see specific entry details.\n\n")
		sb.WriteString("Summary:\n")

		if byStatus, ok := stats["by_status"].(map[string]int); ok {
			for status, count := range byStatus {
				sb.WriteString(fmt.Sprintf("  %s: %d\n", status, count))
			}
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: sb.String()},
			},
		}, stats, nil
	}

	// Get specific entry and its activities
	entry, err := s.historyStore.GetByTitleID(entryID)
	if err != nil {
		return nil, nil, fmt.Errorf("entry not found: %w", err)
	}

	activities, _ := s.historyStore.GetAllActivitiesForTitle(entryID)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Entry Details: %s\n\n", entryID))
	sb.WriteString(fmt.Sprintf("Title: %s\n", entry.Title))
	sb.WriteString(fmt.Sprintf("Author: %s\n", entry.Author))
	if entry.ISBN != "" {
		sb.WriteString(fmt.Sprintf("ISBN: %s\n", entry.ISBN))
	}
	sb.WriteString(fmt.Sprintf("Format: %s\n", entry.Format))
	sb.WriteString(fmt.Sprintf("Library: %s\n\n", entry.Library))

	sb.WriteString("Activity History:\n")
	for _, act := range activities {
		date := time.UnixMilli(act.Timestamp).Format("2006-01-02 15:04")
		sb.WriteString(fmt.Sprintf("  • %s on %s\n", act.Activity, date))

		// Check sync state for this activity
		syncState, _ := s.historyStore.GetSyncState(act.TitleID, act.Activity, act.Timestamp, target)
		if syncState != nil {
			sb.WriteString(fmt.Sprintf("    Sync: %s", syncState.SyncStatus))
			if syncState.TargetBookID != "" {
				sb.WriteString(fmt.Sprintf(" → %s", syncState.TargetBookID))
			}
			if syncState.ErrorMessage != "" {
				sb.WriteString(fmt.Sprintf(" (error: %s)", syncState.ErrorMessage))
			}
			sb.WriteString("\n")
		}
	}

	return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: sb.String()},
			},
		}, map[string]any{
			"entry_id":   entryID,
			"entry":      entry,
			"activities": activities,
		}, nil
}

// handleSyncUnmatched shows books that failed to sync (progressive disclosure level 5)
func (s *Server) handleSyncUnmatched(ctx context.Context, target, filterType string) (*mcp.CallToolResult, any, error) {
	if filterType == "" {
		filterType = "all"
	}

	entries, err := s.historyStore.GetFailedSyncs(target, filterType)
	if err != nil {
		return nil, nil, fmt.Errorf("getting failed syncs: %w", err)
	}

	var sb strings.Builder

	// Header based on filter type
	switch filterType {
	case "isbn":
		sb.WriteString(fmt.Sprintf("Unmatched Books (has ISBN, not in Hardcover) → %s\n\n", target))
		sb.WriteString("These books have an ISBN but weren't found in Hardcover.\n")
		sb.WriteString("This means Hardcover doesn't have this specific edition.\n\n")
	case "no_isbn":
		sb.WriteString(fmt.Sprintf("Unmatched Books (no ISBN, title match failed) → %s\n\n", target))
		sb.WriteString("These books don't have an ISBN and title matching failed.\n")
		sb.WriteString("They may need manual matching or aren't in Hardcover.\n\n")
	default:
		sb.WriteString(fmt.Sprintf("All Unmatched Books → %s\n\n", target))
	}

	if len(entries) == 0 {
		sb.WriteString("No unmatched books found.\n")
	} else {
		sb.WriteString(fmt.Sprintf("Found %d unmatched books:\n\n", len(entries)))

		for i, e := range entries {
			date := time.UnixMilli(e.Timestamp).Format("2006-01-02")
			sb.WriteString(fmt.Sprintf("%d. %s by %s\n", i+1, e.Title, e.Author))
			sb.WriteString(fmt.Sprintf("   Returned: %s • Format: %s\n", date, e.Format))

			if e.ISBN != "" {
				sb.WriteString(fmt.Sprintf("   ISBN: %s\n", e.ISBN))
				sb.WriteString("   Reason: Book with this ISBN not found in Hardcover\n")
			} else {
				sb.WriteString("   ISBN: (none)\n")
				sb.WriteString("   Reason: No ISBN and title matching failed\n")
			}

			if e.Library != "" {
				sb.WriteString(fmt.Sprintf("   Library: %s\n", e.Library))
			}
			sb.WriteString("\n")
		}
	}

	// Return structured data for programmatic use
	type unmatchedEntry struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Author  string `json:"author"`
		Date    string `json:"date"`
		Format  string `json:"format"`
		ISBN    string `json:"isbn,omitempty"`
		Library string `json:"library,omitempty"`
		Reason  string `json:"reason"`
	}

	unmatched := make([]unmatchedEntry, 0, len(entries))
	for _, e := range entries {
		reason := "title match failed"
		if e.ISBN != "" {
			reason = "ISBN not found in Hardcover"
		}

		unmatched = append(unmatched, unmatchedEntry{
			ID:      e.TitleID,
			Title:   e.Title,
			Author:  e.Author,
			Date:    time.UnixMilli(e.Timestamp).Format("2006-01-02"),
			Format:  e.Format,
			ISBN:    e.ISBN,
			Library: e.Library,
			Reason:  reason,
		})
	}

	return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: sb.String()},
			},
		}, map[string]any{
			"target":      target,
			"filter_type": filterType,
			"count":       len(entries),
			"entries":     unmatched,
		}, nil
}

// handleSyncAll performs a comprehensive sync: history + enrichment + Libby tag metadata
func (s *Server) handleSyncAll(ctx context.Context, target string, limit int, dryRun bool) (*mcp.CallToolResult, any, error) {
	var sb strings.Builder
	sb.WriteString("🔄 Comprehensive Sync\n")
	sb.WriteString("Running: Import new books → Sync history → Enrich metadata → Cache tags\n\n")

	stats := make(map[string]any)

	// Step 0: Import current Libby loans to local history
	sb.WriteString("━━━ Step 0: Importing Current Libby Loans ━━━\n\n")

	if s.libby != nil && s.historyStore != nil {
		loans, err := s.libby.GetLoans(ctx)
		if err == nil {
			count := 0
			for _, loan := range loans {
				if err := s.historyStore.ImportCurrentLoan(loan); err != nil {
					// Log error but continue
				} else {
					count++
				}
			}
			sb.WriteString(fmt.Sprintf("✅ Imported %d current loans to local history\n", count))
			stats["loans_imported"] = count
		} else {
			sb.WriteString(fmt.Sprintf("⚠️  Could not fetch loans: %v\n", err))
		}
	} else {
		sb.WriteString("⚠️  Libby loan import skipped - not configured\n")
	}
	sb.WriteString("\n")

	// 1. History Sync (Libby → Hardcover)
	sb.WriteString("━━━ Step 1: Syncing Reading History ━━━\n\n")

	adapter := sync.NewStoreAdapter(s.historyStore)
	syncer := sync.NewHardcoverSync(s.hardcover, adapter)
	syncer.SetDryRun(dryRun)

	// Enable cross-format ISBN lookup via Libby
	if s.libby != nil {
		syncer.SetLibbySearcher(s.libby)
	}

	// Run sync
	summary, err := syncer.SyncReturnedBooks(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("sync failed: %w", err)
	}

	sb.WriteString(fmt.Sprintf("✅ Synced %d books (failed: %d, skipped: %d)\n",
		summary.Successful, summary.Failed, summary.Skipped))
	if dryRun {
		sb.WriteString("   (DRY RUN - no changes made)\n")
	}
	sb.WriteString("\n")
	stats["history_synced"] = summary.Successful
	stats["history_failed"] = summary.Failed
	stats["history_skipped"] = summary.Skipped

	// 2. Enrichment
	sb.WriteString("━━━ Step 2: Enriching Book Metadata ━━━\n\n")

	if s.historyStore != nil {
		unenriched, _ := s.historyStore.GetUnenrichedCount()
		if unenriched > 0 {
			sb.WriteString(fmt.Sprintf("Found %d books needing enrichment\n", unenriched))
			sb.WriteString("Starting background enrichment job...\n")

			// Initialize enrichment service if needed
			if err := s.initRecommendationServices(); err != nil {
				sb.WriteString(fmt.Sprintf("⚠️  Enrichment failed to initialize: %v\n", err))
			} else {
				// Start enrichment (this runs in background)
				job, err := s.enrichmentService.EnrichHistoryBackground(ctx, false)
				if err != nil {
					sb.WriteString(fmt.Sprintf("⚠️  Enrichment failed to start: %v\n", err))
				} else {
					progress := job.GetProgress()
					sb.WriteString(fmt.Sprintf("✅ Started enrichment job %s for %d books\n", job.ID, progress.TotalBooks))
					sb.WriteString("   Use enrichment_status to monitor progress\n")
					stats["enrichment_job_id"] = job.ID
					stats["enrichment_total"] = progress.TotalBooks
				}
			}
		} else {
			sb.WriteString("✅ All books already enriched\n")
			stats["enrichment_needed"] = 0
		}
	} else {
		sb.WriteString("⚠️  Enrichment skipped - history store not available\n")
	}
	sb.WriteString("\n")

	// 3. Libby Tag Metadata Sync
	sb.WriteString("━━━ Step 3: Syncing Libby Tag Metadata ━━━\n\n")

	if s.libby != nil && s.tbrStore != nil {
		loans, err := s.libby.GetLoans(ctx)
		if err == nil {
			holds, err := s.libby.GetHolds(ctx)
			if err == nil {
				// Extract book info from loans and holds
				bookInfos := append(
					tbr.ExtractBookInfoFromLoans(loans),
					tbr.ExtractBookInfoFromHolds(holds)...,
				)

				tagSyncer := tbr.NewLibbyTagSyncer(s.tbrStore, s.libby)
				processed, successful, failed, err := tagSyncer.SyncTagMetadataWithSearchFallback(ctx, bookInfos)
				if err != nil {
					sb.WriteString(fmt.Sprintf("⚠️  Tag metadata sync failed: %v\n", err))
				} else {
					sb.WriteString(fmt.Sprintf("✅ Tag metadata: %d processed, %d successful, %d failed\n",
						processed, successful, failed))
					stats["tag_metadata_processed"] = processed
					stats["tag_metadata_successful"] = successful
					stats["tag_metadata_failed"] = failed
				}
			} else {
				sb.WriteString(fmt.Sprintf("⚠️  Could not fetch holds: %v\n", err))
			}
		} else {
			sb.WriteString(fmt.Sprintf("⚠️  Could not fetch loans: %v\n", err))
		}
	} else {
		if s.libby == nil {
			sb.WriteString("⚠️  Libby tag sync skipped - Libby not configured\n")
		} else {
			sb.WriteString("⚠️  Libby tag sync skipped - TBR store not available\n")
		}
	}
	sb.WriteString("\n")

	// Summary
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━\n")
	sb.WriteString("✅ Comprehensive sync complete!\n")

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: sb.String()},
		},
	}, stats, nil
}
