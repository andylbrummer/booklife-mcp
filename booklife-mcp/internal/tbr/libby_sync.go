package tbr

import (
	"context"
	"fmt"
	"strings"

	"github.com/user/booklife-mcp/internal/models"
	"github.com/user/booklife-mcp/internal/providers"
)

// LibbyTagSyncer handles syncing Libby tag metadata
type LibbyTagSyncer struct {
	store  *Store
	libby  providers.LibbyProvider
}

// NewLibbyTagSyncer creates a new Libby tag metadata syncer
func NewLibbyTagSyncer(store *Store, libby providers.LibbyProvider) *LibbyTagSyncer {
	return &LibbyTagSyncer{
		store:  store,
		libby:  libby,
	}
}

// SyncTagMetadata fetches full book information for all Libby tagged books
// Returns counts of processed, successful, and failed syncs
func (s *LibbyTagSyncer) SyncTagMetadata(ctx context.Context) (processed, successful, failed int, err error) {
	// Get all tags from Libby
	tags, err := s.libby.GetTags(ctx)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("getting Libby tags: %w", err)
	}

	// Build map of media_id -> tag names
	mediaIDToTags := make(map[string][]string)
	for tag, mediaIDs := range tags {
		for _, mediaID := range mediaIDs {
			mediaIDToTags[mediaID] = append(mediaIDToTags[mediaID], tag)
		}
	}

	// For each unique media_id, fetch book details
	for mediaID, tagList := range mediaIDToTags {
		processed++

		// Search Libby to get full book metadata
		// We use the media_id in the search - this is a limitation
		// In practice, we need to extract title/author from somewhere
		// For now, we'll mark this as a TODO to enhance
		// The Libby API doesn't have a "get by media_id" endpoint directly

		// WORKAROUND: We skip this for now and will enhance when we have
		// a better way to get book details by media_id
		// In the real implementation, we'd need to:
		// 1. Get loans/holds to find matching media_id
		// 2. Or enhance the Libby provider with a GetByMediaID method

		// For now, save what we have (media_id and tags only)
		meta := &LibbyTagMeta{
			MediaID: mediaID,
			Tags:    tagList,
		}

		if err := s.store.SaveLibbyTagMetadata(meta); err != nil {
			failed++
			continue
		}
		successful++
	}

	return processed, successful, failed, nil
}

// SyncTagMetadataWithSearchFallback fetches full book info by searching for each tagged book
// This is less efficient but works when we only have media_ids
func (s *LibbyTagSyncer) SyncTagMetadataWithSearchFallback(ctx context.Context, loansAndHolds []LibbyBookInfo) (processed, successful, failed int, err error) {
	// Get all tags from Libby
	tags, err := s.libby.GetTags(ctx)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("getting Libby tags: %w", err)
	}

	// Build map of media_id -> tag names
	mediaIDToTags := make(map[string][]string)
	for tag, mediaIDs := range tags {
		for _, mediaID := range mediaIDs {
			mediaIDToTags[mediaID] = append(mediaIDToTags[mediaID], tag)
		}
	}

	// Build map of media_id -> book info from loans/holds
	mediaIDToBook := make(map[string]LibbyBookInfo)
	for _, book := range loansAndHolds {
		mediaIDToBook[book.MediaID] = book
	}

	// Process each tagged media_id
	for mediaID, tagList := range mediaIDToTags {
		processed++

		bookInfo, found := mediaIDToBook[mediaID]
		if !found {
			// Try searching for this book
			// We don't have title/author, so we skip for now
			failed++
			continue
		}

		// Save full metadata
		meta := &LibbyTagMeta{
			MediaID:       mediaID,
			TitleID:       bookInfo.TitleID,
			Title:         bookInfo.Title,
			Subtitle:      bookInfo.Subtitle,
			Author:        bookInfo.Author,
			ISBN:          bookInfo.ISBN,
			Publisher:     bookInfo.Publisher,
			PublishedDate: bookInfo.PublishedDate,
			CoverURL:      bookInfo.CoverURL,
			Format:        bookInfo.Format,
			IsAvailable:   bookInfo.IsAvailable,
			WaitlistSize:  bookInfo.WaitlistSize,
			Tags:          tagList,
		}

		if err := s.store.SaveLibbyTagMetadata(meta); err != nil {
			failed++
			continue
		}
		successful++
	}

	return processed, successful, failed, nil
}

// LibbyBookInfo represents basic book information from Libby
type LibbyBookInfo struct {
	MediaID       string
	TitleID       string
	Title         string
	Subtitle      string
	Author        string
	ISBN          string
	Publisher     string
	PublishedDate string
	CoverURL      string
	Format        string
	IsAvailable   bool
	WaitlistSize  int
}

// ExtractBookInfoFromLoans extracts book info from Libby loans
func ExtractBookInfoFromLoans(loans []models.LibbyLoan) []LibbyBookInfo {
	var infos []LibbyBookInfo
	for _, loan := range loans {
		infos = append(infos, LibbyBookInfo{
			MediaID:     loan.MediaID,
			TitleID:     loan.ID,
			Title:       loan.Title,
			Author:      loan.Author,
			CoverURL:    loan.CoverURL,
			Format:      loan.Format,
			IsAvailable: true, // Currently checked out
		})
	}
	return infos
}

// ExtractBookInfoFromHolds extracts book info from Libby holds
func ExtractBookInfoFromHolds(holds []models.LibbyHold) []LibbyBookInfo {
	var infos []LibbyBookInfo
	for _, hold := range holds {
		infos = append(infos, LibbyBookInfo{
			MediaID:      hold.MediaID,
			TitleID:      hold.ID,
			Title:        hold.Title,
			Author:       hold.Author,
			CoverURL:     hold.CoverURL,
			Format:       hold.Format,
			IsAvailable:  hold.IsReady,
			WaitlistSize: hold.QueuePosition,
		})
	}
	return infos
}

// TBRSyncer handles syncing TBR from multiple sources
type TBRSyncer struct {
	store *Store
	libby providers.LibbyProvider
}

// NewTBRSyncer creates a new TBR syncer
func NewTBRSyncer(store *Store, libby providers.LibbyProvider) *TBRSyncer {
	return &TBRSyncer{
		store: store,
		libby: libby,
	}
}

// SyncLibbyHolds syncs Libby holds to the unified TBR
func (s *TBRSyncer) SyncLibbyHolds(ctx context.Context, holds []models.LibbyHold) (added, updated int, err error) {
	for _, hold := range holds {
		entry := &TBREntry{
			Title:          hold.Title,
			Author:         hold.Author,
			LibbyMediaID:   hold.MediaID,
			LibbyHoldID:    hold.ID,
			CoverURL:       hold.CoverURL,
			Source:         SourceLibby,
			LibbyAvailable: hold.IsReady,
			LibbyWaitlist:  hold.QueuePosition,
			SourceMetadata: map[string]interface{}{
				"hold_placed_date":    hold.HoldPlacedDate.Format("2006-01-02"),
				"estimated_wait_days": hold.EstimatedWaitDays,
				"auto_borrow":         hold.AutoBorrow,
			},
		}

		id, err := s.store.AddBook(entry)
		if err != nil {
			return added, updated, fmt.Errorf("adding hold %s: %w", hold.Title, err)
		}

		if id > 0 {
			added++
		} else {
			updated++
		}
	}

	return added, updated, nil
}

// SyncLibbyTags syncs Libby tagged books to the unified TBR
func (s *TBRSyncer) SyncLibbyTags(ctx context.Context, taggedBooks []LibbyBookInfo) (added, updated int, err error) {
	for _, book := range taggedBooks {
		entry := &TBREntry{
			Title:        book.Title,
			Subtitle:     book.Subtitle,
			Author:       book.Author,
			ISBN13:       book.ISBN,
			LibbyMediaID: book.MediaID,
			Publisher:    book.Publisher,
			CoverURL:     book.CoverURL,
			Source:       SourceLibby,
			LibbyTags:    []string{}, // Will be populated from libby_tag_metadata
			SourceMetadata: map[string]interface{}{
				"format": book.Format,
			},
		}

		id, err := s.store.AddBook(entry)
		if err != nil {
			return added, updated, fmt.Errorf("adding tagged book %s: %w", book.Title, err)
		}

		if id > 0 {
			added++
		} else {
			updated++
		}
	}

	return added, updated, nil
}

// SyncFromHardcover syncs Hardcover TBR to the unified TBR
func (s *TBRSyncer) SyncFromHardcover(hardcoverBooks []models.Book) (added, updated int, err error) {
	for _, book := range hardcoverBooks {
		// Build genres list
		genres := book.Genres

		// Build author name
		author := ""
		if len(book.Authors) > 0 {
			authorNames := make([]string, len(book.Authors))
			for i, a := range book.Authors {
				authorNames[i] = a.Name
			}
			author = strings.Join(authorNames, ", ")
		}

		entry := &TBREntry{
			Title:         book.Title,
			Subtitle:      book.Subtitle,
			Author:        author,
			ISBN10:        book.ISBN10,
			ISBN13:        book.ISBN13,
			HardcoverID:   book.HardcoverID,
			OpenLibraryID: book.OpenLibID,
			Publisher:     book.Publisher,
			PublishedDate: book.PublishedDate,
			PageCount:     book.PageCount,
			Description:   book.Description,
			CoverURL:      book.CoverURL,
			Genres:        genres,
			Source:        SourceHardcover,
		}

		// Add series info if available
		if book.Series != nil {
			entry.SeriesName = book.Series.Name
			entry.SeriesPosition = book.Series.Position
			entry.SeriesTotal = book.Series.Total
		}

		// Add user status to source metadata
		if book.UserStatus != nil {
			entry.SourceMetadata = map[string]interface{}{
				"user_status": book.UserStatus.Status,
				"date_added":  book.UserStatus.DateAdded.Format("2006-01-02"),
			}
		}

		id, err := s.store.AddBook(entry)
		if err != nil {
			return added, updated, fmt.Errorf("adding Hardcover book %s: %w", book.Title, err)
		}

		if id > 0 {
			added++
		} else {
			updated++
		}
	}

	return added, updated, nil
}
