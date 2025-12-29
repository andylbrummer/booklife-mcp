package sync

import "time"

// OperationType defines the type of sync operation
type OperationType string

const (
	OpUpdateStatus   OperationType = "update_status"
	OpUpdateProgress OperationType = "update_progress"
	OpAddBook        OperationType = "add_book"
	OpUpdateRating   OperationType = "update_rating"
)

// SyncStatus defines the status of a sync operation
type SyncStatus string

const (
	StatusPending    SyncStatus = "pending"
	StatusInProgress SyncStatus = "in_progress"
	StatusCompleted  SyncStatus = "completed"
	StatusFailed     SyncStatus = "failed"
	StatusSkipped    SyncStatus = "skipped"
)

// SyncOperation represents a pending sync operation
type SyncOperation struct {
	ID            int64         `json:"id"`
	Operation     OperationType `json:"operation"`
	SourceSystem  string        `json:"source_system"` // libby, local
	TargetSystem  string        `json:"target_system"` // hardcover
	BookID        string        `json:"book_id"`
	SourceEntryID string        `json:"source_entry_id"` // e.g., libby title_id

	// Operation-specific data
	Status       string     `json:"status,omitempty"`   // reading, read, want-to-read, dnf
	Progress     int        `json:"progress,omitempty"` // 0-100
	Rating       float64    `json:"rating,omitempty"`
	FinishedDate *time.Time `json:"finished_date,omitempty"`

	// Book identification (for matching)
	ISBN   string `json:"isbn,omitempty"`
	Title  string `json:"title,omitempty"`
	Author string `json:"author,omitempty"`

	// State
	SyncStatus    SyncStatus `json:"sync_status"`
	Attempts      int        `json:"attempts"`
	MaxAttempts   int        `json:"max_attempts"`
	LastAttemptAt *time.Time `json:"last_attempt_at,omitempty"`
	ErrorMessage  string     `json:"error_message,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// SyncResult represents the outcome of a sync operation
type SyncResult struct {
	Operation    *SyncOperation `json:"operation"`
	Success      bool           `json:"success"`
	ErrorMessage string         `json:"error_message,omitempty"`
	TargetBookID string         `json:"target_book_id,omitempty"` // Hardcover book ID if found
	TargetTitle  string         `json:"target_title,omitempty"`   // Hardcover book title if found
	Skipped      bool           `json:"skipped"`                  // True if already synced
	SkipReason   string         `json:"skip_reason,omitempty"`
}

// SyncSummary provides an overview of sync operations
type SyncSummary struct {
	TotalProcessed int          `json:"total_processed"`
	Successful     int          `json:"successful"`
	Failed         int          `json:"failed"`
	Skipped        int          `json:"skipped"`
	Results        []SyncResult `json:"results,omitempty"`
	Errors         []string     `json:"errors,omitempty"`
}

// HistorySyncState tracks which history entries have been synced
type HistorySyncState struct {
	TitleID      string     `json:"title_id"`
	Activity     string     `json:"activity"`  // Borrowed, Returned
	Timestamp    int64      `json:"timestamp"` // Original history timestamp
	SyncedAt     time.Time  `json:"synced_at"`
	TargetSystem string     `json:"target_system"`
	TargetBookID string     `json:"target_book_id,omitempty"`
	SyncStatus   SyncStatus `json:"sync_status"`
	ErrorMessage string     `json:"error_message,omitempty"`
}
