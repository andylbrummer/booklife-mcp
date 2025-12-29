package sync

import (
	"github.com/user/booklife-mcp/internal/history"
	"github.com/user/booklife-mcp/internal/models"
)

// StoreAdapter adapts history.Store to the HistoryStore interface
type StoreAdapter struct {
	store *history.Store
}

// NewStoreAdapter creates a new adapter for the history store
func NewStoreAdapter(store *history.Store) *StoreAdapter {
	return &StoreAdapter{store: store}
}

// GetUnsyncedReturns returns all "Returned" activities that haven't been synced
func (a *StoreAdapter) GetUnsyncedReturns(targetSystem string, limit ...int) ([]models.TimelineEntry, error) {
	return a.store.GetUnsyncedReturns(targetSystem, limit...)
}

// MarkEntrySynced records that a history entry has been synced
func (a *StoreAdapter) MarkEntrySynced(titleID string, timestamp int64, activity, targetSystem, targetBookID string, status SyncStatus, errorMsg string) error {
	return a.store.MarkEntrySynced(titleID, timestamp, activity, targetSystem, targetBookID, string(status), errorMsg)
}

// GetSyncState returns the sync state for a specific history entry
func (a *StoreAdapter) GetSyncState(titleID, activity string, timestamp int64, targetSystem string) (*HistorySyncState, error) {
	state, err := a.store.GetSyncState(titleID, activity, timestamp, targetSystem)
	if err != nil {
		return nil, err
	}

	return &HistorySyncState{
		TitleID:      state.TitleID,
		Activity:     state.Activity,
		Timestamp:    state.Timestamp,
		SyncedAt:     state.SyncedAt,
		TargetSystem: state.TargetSystem,
		TargetBookID: state.TargetBookID,
		SyncStatus:   SyncStatus(state.SyncStatus),
		ErrorMessage: state.ErrorMessage,
	}, nil
}

// GetBookIdentityByLibbyID looks up a book identity by Libby TitleID
func (a *StoreAdapter) GetBookIdentityByLibbyID(libbyTitleID string) (*BookIdentity, error) {
	bi, err := a.store.GetBookIdentityByLibbyID(libbyTitleID)
	if err != nil {
		return nil, err
	}

	return &BookIdentity{
		LibbyTitleID: bi.LibbyTitleID,
		HardcoverID:  bi.HardcoverID,
		ISBN10:       bi.ISBN10,
		ISBN13:       bi.ISBN13,
		Title:        bi.Title,
		Author:       bi.Author,
	}, nil
}

// GetBookIdentityByISBN looks up a book identity by ISBN
func (a *StoreAdapter) GetBookIdentityByISBN(isbn string) (*BookIdentity, error) {
	bi, err := a.store.GetBookIdentityByISBN(isbn)
	if err != nil {
		return nil, err
	}

	return &BookIdentity{
		LibbyTitleID: bi.LibbyTitleID,
		HardcoverID:  bi.HardcoverID,
		ISBN10:       bi.ISBN10,
		ISBN13:       bi.ISBN13,
		Title:        bi.Title,
		Author:       bi.Author,
	}, nil
}

// SaveBookIdentity saves or updates a book identity mapping
func (a *StoreAdapter) SaveBookIdentity(bi *BookIdentity) error {
	histBI := &history.BookIdentity{
		LibbyTitleID: bi.LibbyTitleID,
		HardcoverID:  bi.HardcoverID,
		ISBN10:       bi.ISBN10,
		ISBN13:       bi.ISBN13,
		Title:        bi.Title,
		Author:       bi.Author,
	}
	return a.store.SaveBookIdentity(histBI)
}
