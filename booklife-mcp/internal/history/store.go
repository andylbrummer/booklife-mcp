package history

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/user/booklife-mcp/internal/models"

	_ "github.com/mattn/go-sqlite3"
)

// Store manages local reading history using SQLite
type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewStore creates or opens a local history database
func NewStore(dataDir string) (*Store, error) {
	// Ensure data directory exists
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("creating data directory %s: %w\n\n"+
			"This usually means:\n"+
			"1. Permission denied (check directory permissions)\n"+
			"2. Disk full\n"+
			"3. Invalid path\n\n"+
			"Fix:\n"+
			"1. Check disk space: df -h\n"+
			"2. Verify parent directory is writable\n"+
			"3. Create manually: mkdir -p %s", dataDir, err, dataDir)
	}

	dbPath := filepath.Join(dataDir, "history.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database at %s: %w\n\n"+
			"This usually means:\n"+
			"1. Permission denied (check file permissions)\n"+
			"2. SQLite library not available\n"+
			"3. Corrupted database file\n\n"+
			"Fix:\n"+
			"1. Check file permissions: ls -l %s\n"+
			"2. Try removing corrupted database (will lose data):\n"+
			"   mv %s %s.backup\n"+
			"3. Restart the server (will create fresh database)", dbPath, err, dbPath, dbPath, dbPath)
	}

	store := &Store{db: db}
	if err := store.init(); err != nil {
		db.Close()
		return nil, fmt.Errorf("initializing database schema: %w\n\n"+
			"Database schema initialization failed.\n"+
			"This may indicate database corruption.\n\n"+
			"Fix:\n"+
			"1. Backup existing database:\n"+
			"   cp %s %s.backup\n"+
			"2. Remove corrupted database:\n"+
			"   rm %s\n"+
			"3. Restart the server (will create fresh database)\n"+
			"4. If needed, restore from backup later", err, dbPath, dbPath, dbPath)
	}

	return store, nil
}

// init creates the database schema
func (s *Store) init() error {
	query := `
	CREATE TABLE IF NOT EXISTS history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title_id TEXT NOT NULL,
		title TEXT NOT NULL,
		author TEXT NOT NULL,
		publisher TEXT,
		isbn TEXT,
		timestamp INTEGER NOT NULL,
		activity TEXT NOT NULL,
		details TEXT,
		library TEXT NOT NULL,
		library_key TEXT NOT NULL,
		format TEXT NOT NULL,
		cover_url TEXT,
		color TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(title_id, timestamp, activity)
	);

	CREATE INDEX IF NOT EXISTS idx_timestamp ON history(timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_title ON history(title);
	CREATE INDEX IF NOT EXISTS idx_author ON history(author);
	CREATE INDEX IF NOT EXISTS idx_library ON history(library);
	CREATE INDEX IF NOT EXISTS idx_activity ON history(activity);

	-- Sync state tracking table
	CREATE TABLE IF NOT EXISTS sync_state (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title_id TEXT NOT NULL,
		activity TEXT NOT NULL,
		timestamp INTEGER NOT NULL,
		target_system TEXT NOT NULL,
		target_book_id TEXT,
		sync_status TEXT NOT NULL,
		error_message TEXT,
		synced_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(title_id, timestamp, activity, target_system)
	);

	CREATE INDEX IF NOT EXISTS idx_sync_state_status ON sync_state(sync_status);
	CREATE INDEX IF NOT EXISTS idx_sync_state_target ON sync_state(target_system);

	-- Book identity cache for cross-platform ID mappings
	CREATE TABLE IF NOT EXISTS book_identities (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		libby_title_id TEXT UNIQUE NOT NULL,
		hardcover_id TEXT,
		isbn10 TEXT,
		isbn13 TEXT,
		title TEXT NOT NULL,
		author TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_book_identities_hardcover ON book_identities(hardcover_id);
	CREATE INDEX IF NOT EXISTS idx_book_identities_isbn ON book_identities(isbn10, isbn13);

	-- Book enrichment table for external metadata
	CREATE TABLE IF NOT EXISTS book_enrichment (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		history_id INTEGER NOT NULL,
		title TEXT NOT NULL,
		author TEXT NOT NULL,

		-- External IDs
		openlibrary_id TEXT,
		googlebooks_id TEXT,

		-- Enriched metadata
		description TEXT,
		themes TEXT,  -- JSON array of themes
		topics TEXT,  -- JSON array of topics/subjects
		mood TEXT,    -- JSON array of mood tags
		complexity TEXT,

		-- Series information
		series_name TEXT,
		series_position REAL,
		series_total INTEGER,

		-- Source tracking
		enrichment_sources TEXT,  -- JSON array of sources used
		enriched_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,

		FOREIGN KEY (history_id) REFERENCES history(id),
		UNIQUE(history_id)
	);

	CREATE INDEX IF NOT EXISTS idx_enrichment_openlibrary ON book_enrichment(openlibrary_id);
	CREATE INDEX IF NOT EXISTS idx_enrichment_themes ON book_enrichment(title);
	CREATE INDEX IF NOT EXISTS idx_enrichment_series ON book_enrichment(series_name);

	-- Book relationships for graph traversal
	CREATE TABLE IF NOT EXISTS book_relationships (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		from_history_id INTEGER NOT NULL,
		to_history_id INTEGER NOT NULL,
		relationship_type TEXT NOT NULL,  -- same_author, same_series, similar_theme, also_read
		strength REAL DEFAULT 1.0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,

		FOREIGN KEY (from_history_id) REFERENCES history(id),
		FOREIGN KEY (to_history_id) REFERENCES history(id),
		UNIQUE(from_history_id, to_history_id, relationship_type)
	);

	CREATE INDEX IF NOT EXISTS idx_relationships_from ON book_relationships(from_history_id);
	CREATE INDEX IF NOT EXISTS idx_relationships_to ON book_relationships(to_history_id);
	CREATE INDEX IF NOT EXISTS idx_relationships_type ON book_relationships(relationship_type);

	-- Reading profile for aggregated user patterns
	CREATE TABLE IF NOT EXISTS reading_profile (
		id INTEGER PRIMARY KEY AUTOINCREMENT,

		-- Preferences (stored as JSON)
		preferred_formats TEXT,   -- JSON map
		preferred_genres TEXT,    -- JSON map
		preferred_authors TEXT,   -- JSON map

		-- Patterns
		avg_reading_speed REAL,
		completion_rate REAL,
		abandon_triggers TEXT,    -- JSON array
		series_completion TEXT,   -- JSON map

		-- Temporal
		reading_cadence TEXT,     -- JSON map
		streaks TEXT,             -- JSON array
		seasonal TEXT,            -- JSON map

		-- Social signals
		ratings_distribution TEXT, -- JSON map
		avg_review_length INTEGER,

		-- Metadata
		computed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_profile_computed ON reading_profile(computed_at);
	`

	_, err := s.db.Exec(query)
	return err
}

// ImportTimeline imports timeline data from Libby export
func (s *Store) ImportTimeline(timeline *models.TimelineResponse) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("beginning transaction: %w", err)
	}

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO history
		(title_id, title, author, publisher, isbn, timestamp, activity, details, library, library_key, format, cover_url, color)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		tx.Rollback()
		return 0, fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	count := 0
	for _, entry := range timeline.Timeline {
		// Parse cover URL from nested structure
		coverURL := entry.CoverURL
		color := entry.Color

		_, err := stmt.Exec(
			entry.TitleID,
			entry.Title,
			entry.Author,
			entry.Publisher,
			entry.ISBN,
			entry.Timestamp,
			entry.Activity,
			entry.Details,
			entry.Library,
			entry.LibraryKey,
			entry.Format,
			coverURL,
			color,
		)
		if err != nil {
			tx.Rollback()
			return 0, fmt.Errorf("inserting entry %s: %w", entry.Title, err)
		}
		count++
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing transaction: %w", err)
	}

	return count, nil
}

// ImportCurrentLoan imports a current loan from Libby sync
func (s *Store) ImportCurrentLoan(loan models.LibbyLoan) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create a "Borrowed" activity for current loans
	timestamp := loan.CheckoutDate.UnixMilli()

	query := `
		INSERT OR REPLACE INTO history
		(title_id, title, author, timestamp, activity, details, library, library_key, format, cover_url)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.Exec(query,
		loan.ID,
		loan.Title,
		loan.Author,
		timestamp,
		"Borrowed",
		fmt.Sprintf("%d days", int(time.Until(loan.DueDate).Hours()/24)),
		"Current Loan",
		"",
		loan.Format,
		loan.CoverURL,
	)

	return err
}

// GetHistory returns all history entries with pagination
func (s *Store) GetHistory(offset, limit int) ([]models.TimelineEntry, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get total count
	var total int
	err := s.db.QueryRow("SELECT COUNT(*) FROM history").Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting entries: %w", err)
	}

	// Get paginated entries
	query := `
		SELECT title_id, title, author, publisher, isbn, timestamp, activity,
		       details, library, library_key, format, cover_url, color
		FROM history
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?
	`

	rows, err := s.db.Query(query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("querying history: %w", err)
	}
	defer rows.Close()

	entries := []models.TimelineEntry{}
	for rows.Next() {
		var e models.TimelineEntry
		err := rows.Scan(
			&e.TitleID, &e.Title, &e.Author, &e.Publisher, &e.ISBN,
			&e.Timestamp, &e.Activity, &e.Details, &e.Library,
			&e.LibraryKey, &e.Format, &e.CoverURL, &e.Color,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning entry: %w", err)
		}
		entries = append(entries, e)
	}

	return entries, total, nil
}

// SearchHistory searches history by title or author
func (s *Store) SearchHistory(query string, offset, limit int) ([]models.TimelineEntry, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get total count
	var total int
	err := s.db.QueryRow("SELECT COUNT(*) FROM history WHERE title LIKE ? OR author LIKE ?",
		"%"+query+"%", "%"+query+"%").Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("counting entries: %w", err)
	}

	// Get paginated entries
	searchQuery := `
		SELECT title_id, title, author, publisher, isbn, timestamp, activity,
		       details, library, library_key, format, cover_url, color
		FROM history
		WHERE title LIKE ? OR author LIKE ?
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?
	`

	rows, err := s.db.Query(searchQuery, "%"+query+"%", "%"+query+"%", limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("querying history: %w", err)
	}
	defer rows.Close()

	entries := []models.TimelineEntry{}
	for rows.Next() {
		var e models.TimelineEntry
		err := rows.Scan(
			&e.TitleID, &e.Title, &e.Author, &e.Publisher, &e.ISBN,
			&e.Timestamp, &e.Activity, &e.Details, &e.Library,
			&e.LibraryKey, &e.Format, &e.CoverURL, &e.Color,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning entry: %w", err)
		}
		entries = append(entries, e)
	}

	return entries, total, nil
}

// GetStats returns reading statistics
func (s *Store) GetStats() (map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := make(map[string]interface{})

	// Total entries
	var total int
	err := s.db.QueryRow("SELECT COUNT(*) FROM history").Scan(&total)
	if err != nil {
		return nil, err
	}
	stats["total_entries"] = total

	// Books borrowed
	var borrowed int
	err = s.db.QueryRow("SELECT COUNT(DISTINCT title_id) FROM history WHERE activity = 'Borrowed'").Scan(&borrowed)
	if err != nil {
		return nil, err
	}
	stats["unique_borrows"] = borrowed

	// Format breakdown
	formatQuery := `
		SELECT format, COUNT(*) as count
		FROM history
		GROUP BY format
	`
	rows, err := s.db.Query(formatQuery)
	if err == nil {
		formats := make(map[string]int)
		for rows.Next() {
			var format string
			var count int
			rows.Scan(&format, &count)
			formats[format] = count
		}
		rows.Close()
		stats["by_format"] = formats
	}

	// Library breakdown
	libraryQuery := `
		SELECT library, COUNT(*) as count
		FROM history
		GROUP BY library
		ORDER BY count DESC
	`
	rows, err = s.db.Query(libraryQuery)
	if err == nil {
		libraries := make(map[string]int)
		for rows.Next() {
			var library string
			var count int
			rows.Scan(&library, &count)
			libraries[library] = count
		}
		rows.Close()
		stats["by_library"] = libraries
	}

	// Date range
	var firstTimestamp, lastTimestamp int64
	s.db.QueryRow("SELECT MIN(timestamp), MAX(timestamp) FROM history WHERE timestamp IS NOT NULL AND timestamp > 0").Scan(&firstTimestamp, &lastTimestamp)
	if firstTimestamp > 0 {
		stats["first_activity"] = time.UnixMilli(firstTimestamp).Format("2006-01-02")
	}
	if lastTimestamp > 0 {
		stats["last_activity"] = time.UnixMilli(lastTimestamp).Format("2006-01-02")
	}

	return stats, nil
}

// GetYearlyStats returns statistics grouped by year
func (s *Store) GetYearlyStats() ([]map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT
			IFNULL(strftime('%Y', datetime(timestamp/1000, 'unix')), 'Unknown') as year,
			COUNT(*) as total,
			SUM(CASE WHEN activity = 'Borrowed' THEN 1 ELSE 0 END) as borrowed
		FROM history
		WHERE timestamp IS NOT NULL AND timestamp > 0
		GROUP BY year
		ORDER BY year DESC
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("querying yearly stats: %w", err)
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var year string
		var total, borrowed int
		err := rows.Scan(&year, &total, &borrowed)
		if err != nil {
			return nil, fmt.Errorf("scanning yearly stat: %w", err)
		}
		results = append(results, map[string]interface{}{
			"year":     year,
			"total":    total,
			"borrowed": borrowed,
		})
	}

	return results, nil
}

// Close closes the database connection
func (s *Store) Close() error {
	return s.db.Close()
}

// ValidateSchema checks if the database schema is properly initialized
func (s *Store) ValidateSchema() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check if book_enrichment table exists
	var tableName string
	err := s.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='book_enrichment'").Scan(&tableName)
	if err != nil {
		return fmt.Errorf("book_enrichment table does not exist: %w\n\n"+
			"The database schema is not initialized.\n\n"+
			"Fix:\n"+
			"1. Restart the BookLife server to initialize schema\n"+
			"2. Or delete and recreate database:\n"+
			"   rm ~/.local/share/booklife/history.db", err)
	}

	return nil
}

// GetUnenrichedCount returns the count of books that need enrichment
func (s *Store) GetUnenrichedCount() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	query := `
		SELECT COUNT(DISTINCT h.id)
		FROM history h
		LEFT JOIN book_enrichment e ON h.id = e.history_id
		WHERE h.activity = 'Returned'
			AND e.id IS NULL
	`
	err := s.db.QueryRow(query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting unenriched books: %w", err)
	}

	return count, nil
}

// ExportJSON exports all history as JSON
func (s *Store) ExportJSON() ([]byte, error) {
	entries, _, err := s.GetHistory(0, 10000) // Get all entries
	if err != nil {
		return nil, err
	}

	timeline := &models.TimelineResponse{
		Version:  1,
		Timeline: entries,
	}

	return json.MarshalIndent(timeline, "", "  ")
}

// SyncStatus constants
const (
	SyncStatusPending   = "pending"
	SyncStatusCompleted = "completed"
	SyncStatusFailed    = "failed"
	SyncStatusSkipped   = "skipped"
)

// SyncState represents the sync state of a history entry
type SyncState struct {
	TitleID      string    `json:"title_id"`
	Activity     string    `json:"activity"`
	Timestamp    int64     `json:"timestamp"`
	TargetSystem string    `json:"target_system"`
	TargetBookID string    `json:"target_book_id"`
	SyncStatus   string    `json:"sync_status"`
	ErrorMessage string    `json:"error_message"`
	SyncedAt     time.Time `json:"synced_at"`
}

// GetUnsyncedReturns returns all "Returned" activities that haven't been synced to the target system
// Deduplicates by title+author, keeping only the most recent return for each unique book
// If limit > 0, only returns that many entries
func (s *Store) GetUnsyncedReturns(targetSystem string, limit ...int) ([]models.TimelineEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Deduplicate: keep only the most recent return for each unique book (title+author)
	query := `
		SELECT h.title_id, h.title, h.author, h.publisher, h.isbn, h.timestamp, h.activity,
		       h.details, h.library, h.library_key, h.format, h.cover_url, h.color
		FROM history h
		INNER JOIN (
			SELECT title || '|' || COALESCE(author, '') as book_key,
				   MAX(timestamp) as max_timestamp
			FROM history
			WHERE activity = 'Returned'
			GROUP BY title || '|' || COALESCE(author, '')
		) latest ON h.title || '|' || COALESCE(h.author, '') = latest.book_key
			AND h.timestamp = latest.max_timestamp
		LEFT JOIN sync_state ss ON h.title_id = ss.title_id
			AND h.timestamp = ss.timestamp
			AND h.activity = ss.activity
			AND ss.target_system = ?
		WHERE h.activity = 'Returned'
			AND (ss.id IS NULL OR ss.sync_status IN ('pending', 'failed'))
			-- Only retry books that are pending, failed, or never synced
			-- Exclude completed and permanently skipped books
		ORDER BY h.timestamp DESC
	`

	// Add LIMIT if specified
	limitVal := 0
	if len(limit) > 0 && limit[0] > 0 {
		limitVal = limit[0]
		query += fmt.Sprintf(" LIMIT %d", limitVal)
	}

	rows, err := s.db.Query(query, targetSystem)
	if err != nil {
		return nil, fmt.Errorf("querying unsynced returns: %w", err)
	}
	defer rows.Close()

	var entries []models.TimelineEntry
	for rows.Next() {
		var e models.TimelineEntry
		err := rows.Scan(
			&e.TitleID, &e.Title, &e.Author, &e.Publisher, &e.ISBN,
			&e.Timestamp, &e.Activity, &e.Details, &e.Library,
			&e.LibraryKey, &e.Format, &e.CoverURL, &e.Color,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning entry: %w", err)
		}
		entries = append(entries, e)
	}

	return entries, nil
}

// MarkEntrySynced records that a history entry has been synced to a target system
func (s *Store) MarkEntrySynced(titleID string, timestamp int64, activity, targetSystem, targetBookID, status, errorMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
		INSERT OR REPLACE INTO sync_state
		(title_id, timestamp, activity, target_system, target_book_id, sync_status, error_message, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`

	_, err := s.db.Exec(query, titleID, timestamp, activity, targetSystem, targetBookID, status, errorMsg)
	return err
}

// GetSyncState returns the sync state for a specific history entry
func (s *Store) GetSyncState(titleID, activity string, timestamp int64, targetSystem string) (*SyncState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT title_id, activity, timestamp, target_system, target_book_id, sync_status, error_message, synced_at
		FROM sync_state
		WHERE title_id = ? AND activity = ? AND timestamp = ? AND target_system = ?
	`

	var state SyncState
	err := s.db.QueryRow(query, titleID, activity, timestamp, targetSystem).Scan(
		&state.TitleID, &state.Activity, &state.Timestamp, &state.TargetSystem,
		&state.TargetBookID, &state.SyncStatus, &state.ErrorMessage, &state.SyncedAt,
	)
	if err != nil {
		return nil, err
	}

	return &state, nil
}

// GetSyncStats returns statistics about sync operations
func (s *Store) GetSyncStats(targetSystem string) (map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := make(map[string]interface{})

	// Count by status
	query := `
		SELECT sync_status, COUNT(*) as count
		FROM sync_state
		WHERE target_system = ?
		GROUP BY sync_status
	`
	rows, err := s.db.Query(query, targetSystem)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	statusCounts := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		rows.Scan(&status, &count)
		statusCounts[status] = count
	}
	stats["by_status"] = statusCounts

	// Count unsynced returns
	var unsyncedCount int
	unsyncedQuery := `
		SELECT COUNT(DISTINCT h.title_id || '-' || h.timestamp)
		FROM history h
		LEFT JOIN sync_state ss ON h.title_id = ss.title_id
			AND h.timestamp = ss.timestamp
			AND h.activity = ss.activity
			AND ss.target_system = ?
		WHERE h.activity = 'Returned'
			AND (ss.id IS NULL OR ss.sync_status NOT IN ('completed', 'skipped'))
	`
	s.db.QueryRow(unsyncedQuery, targetSystem).Scan(&unsyncedCount)
	stats["unsynced_returns"] = unsyncedCount

	// Last sync time
	var lastSync *time.Time
	s.db.QueryRow("SELECT MAX(synced_at) FROM sync_state WHERE target_system = ?", targetSystem).Scan(&lastSync)
	if lastSync != nil {
		stats["last_sync"] = lastSync.Format(time.RFC3339)
	}

	return stats, nil
}

// GetByTitleID returns the most recent entry for a title
func (s *Store) GetByTitleID(titleID string) (*models.TimelineEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT title_id, title, author, publisher, isbn, timestamp, activity,
		       details, library, library_key, format, cover_url, color
		FROM history
		WHERE title_id = ?
		ORDER BY timestamp DESC
		LIMIT 1
	`

	var e models.TimelineEntry
	err := s.db.QueryRow(query, titleID).Scan(
		&e.TitleID, &e.Title, &e.Author, &e.Publisher, &e.ISBN,
		&e.Timestamp, &e.Activity, &e.Details, &e.Library,
		&e.LibraryKey, &e.Format, &e.CoverURL, &e.Color,
	)
	if err != nil {
		return nil, err
	}

	return &e, nil
}

// GetAllActivitiesForTitle returns all activities for a title
func (s *Store) GetAllActivitiesForTitle(titleID string) ([]models.TimelineEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT title_id, title, author, publisher, isbn, timestamp, activity,
		       details, library, library_key, format, cover_url, color
		FROM history
		WHERE title_id = ?
		ORDER BY timestamp DESC
	`

	rows, err := s.db.Query(query, titleID)
	if err != nil {
		return nil, fmt.Errorf("querying activities: %w", err)
	}
	defer rows.Close()

	var entries []models.TimelineEntry
	for rows.Next() {
		var e models.TimelineEntry
		err := rows.Scan(
			&e.TitleID, &e.Title, &e.Author, &e.Publisher, &e.ISBN,
			&e.Timestamp, &e.Activity, &e.Details, &e.Library,
			&e.LibraryKey, &e.Format, &e.CoverURL, &e.Color,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning entry: %w", err)
		}
		entries = append(entries, e)
	}

	return entries, nil
}

// BookIdentity represents a cross-platform book identity mapping
type BookIdentity struct {
	LibbyTitleID string
	HardcoverID  string
	ISBN10       string
	ISBN13       string
	Title        string
	Author       string
}

// GetBookIdentityByLibbyID looks up a book identity by Libby TitleID
func (s *Store) GetBookIdentityByLibbyID(libbyTitleID string) (*BookIdentity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT libby_title_id, hardcover_id, isbn10, isbn13, title, author
		FROM book_identities
		WHERE libby_title_id = ?
	`

	var bi BookIdentity
	err := s.db.QueryRow(query, libbyTitleID).Scan(
		&bi.LibbyTitleID, &bi.HardcoverID, &bi.ISBN10, &bi.ISBN13,
		&bi.Title, &bi.Author,
	)
	if err != nil {
		return nil, err
	}

	return &bi, nil
}

// GetBookIdentityByISBN looks up a book identity by ISBN (tries ISBN10 first, then ISBN13)
func (s *Store) GetBookIdentityByISBN(isbn string) (*BookIdentity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var query string
	if len(isbn) == 10 {
		query = `
			SELECT libby_title_id, hardcover_id, isbn10, isbn13, title, author
			FROM book_identities
			WHERE isbn10 = ?
		`
	} else {
		query = `
			SELECT libby_title_id, hardcover_id, isbn10, isbn13, title, author
			FROM book_identities
			WHERE isbn13 = ?
		`
	}

	var bi BookIdentity
	err := s.db.QueryRow(query, isbn).Scan(
		&bi.LibbyTitleID, &bi.HardcoverID, &bi.ISBN10, &bi.ISBN13,
		&bi.Title, &bi.Author,
	)
	if err != nil {
		return nil, err
	}

	return &bi, nil
}

// SaveBookIdentity saves or updates a book identity mapping
func (s *Store) SaveBookIdentity(bi *BookIdentity) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
		INSERT INTO book_identities
		(libby_title_id, hardcover_id, isbn10, isbn13, title, author, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(libby_title_id) DO UPDATE SET
			hardcover_id = excluded.hardcover_id,
			isbn10 = excluded.isbn10,
			isbn13 = excluded.isbn13,
			title = excluded.title,
			author = excluded.author,
			updated_at = CURRENT_TIMESTAMP
	`

	_, err := s.db.Exec(query, bi.LibbyTitleID, bi.HardcoverID, bi.ISBN10, bi.ISBN13, bi.Title, bi.Author)
	return err
}

// GetFailedSyncs returns entries that failed to sync, optionally filtered by ISBN presence
// filterType: "isbn" (has ISBN but failed), "no_isbn" (no ISBN and failed), or "all" (all failed)
func (s *Store) GetFailedSyncs(targetSystem, filterType string) ([]models.TimelineEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var query string
	switch filterType {
	case "isbn":
		// Books with ISBN that failed to sync (likely not in Hardcover)
		query = `
			SELECT DISTINCT h.title_id, h.title, h.author, h.publisher, h.isbn, h.timestamp, h.activity,
			       h.details, h.library, h.library_key, h.format, h.cover_url, h.color
			FROM history h
			INNER JOIN sync_state ss ON h.title_id = ss.title_id
				AND h.timestamp = ss.timestamp
				AND h.activity = ss.activity
				AND ss.target_system = ?
			WHERE h.activity = 'Returned'
				AND ss.sync_status = 'failed'
				AND h.isbn IS NOT NULL AND h.isbn != ''
			ORDER BY h.timestamp DESC
		`
	case "no_isbn":
		// Books without ISBN that failed to sync
		query = `
			SELECT DISTINCT h.title_id, h.title, h.author, h.publisher, h.isbn, h.timestamp, h.activity,
			       h.details, h.library, h.library_key, h.format, h.cover_url, h.color
			FROM history h
			INNER JOIN sync_state ss ON h.title_id = ss.title_id
				AND h.timestamp = ss.timestamp
				AND h.activity = ss.activity
				AND ss.target_system = ?
			WHERE h.activity = 'Returned'
				AND ss.sync_status = 'failed'
				AND (h.isbn IS NULL OR h.isbn = '')
			ORDER BY h.timestamp DESC
		`
	default: // "all"
		query = `
			SELECT DISTINCT h.title_id, h.title, h.author, h.publisher, h.isbn, h.timestamp, h.activity,
			       h.details, h.library, h.library_key, h.format, h.cover_url, h.color
			FROM history h
			INNER JOIN sync_state ss ON h.title_id = ss.title_id
				AND h.timestamp = ss.timestamp
				AND h.activity = ss.activity
				AND ss.target_system = ?
			WHERE h.activity = 'Returned'
				AND ss.sync_status = 'failed'
			ORDER BY h.timestamp DESC
		`
	}

	rows, err := s.db.Query(query, targetSystem)
	if err != nil {
		return nil, fmt.Errorf("querying failed syncs: %w", err)
	}
	defer rows.Close()

	var entries []models.TimelineEntry
	for rows.Next() {
		var e models.TimelineEntry
		err := rows.Scan(
			&e.TitleID, &e.Title, &e.Author, &e.Publisher, &e.ISBN,
			&e.Timestamp, &e.Activity, &e.Details, &e.Library,
			&e.LibraryKey, &e.Format, &e.CoverURL, &e.Color,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning entry: %w", err)
		}
		entries = append(entries, e)
	}

	return entries, nil
}
