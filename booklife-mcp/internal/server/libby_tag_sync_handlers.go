package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/user/booklife-mcp/internal/tbr"
)

// ===== Libby Tag Metadata Sync Input Types =====

// LibbyTagMetadataSyncInput for syncing Libby tag metadata
type LibbyTagMetadataSyncInput struct {
	Tag string `json:"tag,omitempty"` // Optional: sync only books with this tag
}

// LibbyTagMetadataListInput for listing cached tag metadata
type LibbyTagMetadataListInput struct {
	Tag string `json:"tag,omitempty"` // Optional: filter by tag
	PaginationParams
}

// ===== Libby Tag Metadata Tool Handlers =====

func (s *Server) handleLibbyTagMetadataSync(ctx context.Context, req *mcp.CallToolRequest, input LibbyTagMetadataSyncInput) (*mcp.CallToolResult, any, error) {
	if s.libby == nil {
		return nil, nil, fmt.Errorf("Libby is not configured")
	}
	if s.tbrStore == nil {
		return nil, nil, fmt.Errorf("TBR store is not available")
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

	// Sync tag metadata
	tagSyncer := tbr.NewLibbyTagSyncer(s.tbrStore, s.libby)
	processed, successful, failed, err := tagSyncer.SyncTagMetadataWithSearchFallback(ctx, bookInfos)
	if err != nil {
		return nil, nil, fmt.Errorf("syncing tag metadata: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🏷️  Synced Libby tag metadata:\n\n"))
	sb.WriteString(fmt.Sprintf("Processed: %d media items\n", processed))
	sb.WriteString(fmt.Sprintf("Successful: %d\n", successful))
	sb.WriteString(fmt.Sprintf("Failed: %d\n", failed))
	sb.WriteString(fmt.Sprintf("\n✅ Full book information is now cached locally for offline access\n"))
	sb.WriteString(fmt.Sprintf("\nUse libby_tag_metadata_list to browse tagged books\n"))

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: sb.String(),
			},
		},
	}, map[string]any{
		"processed":  processed,
		"successful": successful,
		"failed":     failed,
	}, nil
}

func (s *Server) handleLibbyTagMetadataList(ctx context.Context, req *mcp.CallToolRequest, input LibbyTagMetadataListInput) (*mcp.CallToolResult, any, error) {
	if s.tbrStore == nil {
		return nil, nil, fmt.Errorf("TBR store is not available")
	}

	page, pageSize := getPagination(input.PaginationParams)
	offset := input.PaginationParams.offset()

	metas, total, err := s.tbrStore.GetLibbyTagMetadata(input.Tag, offset, pageSize)
	if err != nil {
		return nil, nil, fmt.Errorf("getting tag metadata: %w", err)
	}

	pagedResult := calculatePagedResult(page, pageSize, total)

	var sb strings.Builder
	if total == 0 {
		sb.WriteString("No tagged book metadata found\n\n")
		sb.WriteString("Run libby_sync_tag_metadata to fetch and cache full book information\n")
	} else {
		filterMsg := "all tags"
		if input.Tag != "" {
			filterMsg = fmt.Sprintf("tag: %s", input.Tag)
		}
		sb.WriteString(fmt.Sprintf("🏷️  Libby Tagged Books (%s) - %d total\n\n", filterMsg, total))

		for i, meta := range metas {
			sb.WriteString(fmt.Sprintf("[%d] %s\n", i+1, meta.Title))
			if meta.Subtitle != "" {
				sb.WriteString(fmt.Sprintf("    %s\n", meta.Subtitle))
			}
			sb.WriteString(fmt.Sprintf("    by %s\n", meta.Author))

			if len(meta.Tags) > 0 {
				sb.WriteString(fmt.Sprintf("    Tags: %s\n", strings.Join(meta.Tags, ", ")))
			}

			if meta.Format != "" {
				sb.WriteString(fmt.Sprintf("    Format: %s", meta.Format))
				if meta.IsAvailable {
					sb.WriteString(" ✅ Available now")
				} else if meta.WaitlistSize > 0 {
					sb.WriteString(fmt.Sprintf(" (waitlist: %d)", meta.WaitlistSize))
				}
				sb.WriteString("\n")
			}

			if meta.ISBN != "" {
				sb.WriteString(fmt.Sprintf("    ISBN: %s\n", meta.ISBN))
			}
			if meta.Publisher != "" {
				sb.WriteString(fmt.Sprintf("    Publisher: %s", meta.Publisher))
				if meta.PublishedDate != "" {
					sb.WriteString(fmt.Sprintf(", %s", meta.PublishedDate))
				}
				sb.WriteString("\n")
			}

			sb.WriteString(fmt.Sprintf("    media_id: %s\n", meta.MediaID))
			sb.WriteString("\n")
		}

		sb.WriteString(formatPagingFooter(pagedResult, len(metas)))
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
