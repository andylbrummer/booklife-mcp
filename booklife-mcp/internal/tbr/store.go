package tbr

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Source constants for tracking where TBR entries came from
const (
	SourcePhysical  = "physical"
	SourceHardcover = "hardcover"
	SourceLibby     = "libby"
)

// Store manages local TBR (to-be-read) list using SQLite
type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewStore creates or opens a local TBR database
func NewStore(dataDir string) (*Store, error) {
	// Ensure data directory exists
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("creating data directory %s: %w", dataDir, err)
	}

	dbPath := filepath.Join(dataDir, "tbr.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database at %s: %w", dbPath, err)
	}

	store := &Store{db: db}
	if err := store.init(); err != nil {
		db.Close()
		return nil, fmt.Errorf("initializing database schema: %w", err)
	}

	return store, nil
}

// init creates the database schema
func (s *Store) init() error {
	query := `
	-- Main TBR books table
	CREATE TABLE IF NOT EXISTS tbr_books (
		id INTEGER PRIMARY KEY AUTOINCREMENT,

		-- Core identification
		title TEXT NOT NULL,
		subtitle TEXT,
		author TEXT NOT NULL,

		-- ISBNs (for cross-referencing)
		isbn10 TEXT,
		isbn13 TEXT,

		-- External IDs
		hardcover_id TEXT,
		libby_media_id TEXT,
		openlibrary_id TEXT,

		-- Metadata
		publisher TEXT,
		published_date TEXT,
		page_count INTEGER,
		description TEXT,
		cover_url TEXT,
		genres TEXT,  -- JSON array

		-- Series information
		series_name TEXT,
		series_position REAL,
		series_total INTEGER,

		-- Source tracking
		source TEXT NOT NULL,  -- physical, hardcover, libby
		source_metadata TEXT,   -- JSON with source-specific data

		-- Libby-specific fields
		libby_tags TEXT,       -- JSON array of tags
		libby_hold_id TEXT,    -- If book is on hold
		libby_available BOOLEAN DEFAULT 0,
		libby_waitlist_size INTEGER,

		-- User notes and priority
		notes TEXT,
		priority INTEGER DEFAULT 0,  -- Higher = more priority

		-- Timestamps
		added_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,

		-- Prevent duplicates by title+author
		UNIQUE(title, author)
	);

	CREATE INDEX IF NOT EXISTS idx_tbr_source ON tbr_books(source);
	CREATE INDEX IF NOT EXISTS idx_tbr_hardcover_id ON tbr_books(hardcover_id);
	CREATE INDEX IF NOT EXISTS idx_tbr_libby_media_id ON tbr_books(libby_media_id);
	CREATE INDEX IF NOT EXISTS idx_tbr_isbn ON tbr_books(isbn10, isbn13);
	CREATE INDEX IF NOT EXISTS idx_tbr_priority ON tbr_books(priority DESC);
	CREATE INDEX IF NOT EXISTS idx_tbr_added_at ON tbr_books(added_at DESC);
	CREATE INDEX IF NOT EXISTS idx_tbr_series ON tbr_books(series_name);

	-- Libby tag metadata cache (full book info for tagged items)
	CREATE TABLE IF NOT EXISTS libby_tag_metadata (
		id INTEGER PRIMARY KEY AUTOINCREMENT,

		-- Libby identifiers
		media_id TEXT UNIQUE NOT NULL,
		title_id TEXT,

		-- Full book metadata
		title TEXT NOT NULL,
		subtitle TEXT,
		author TEXT NOT NULL,
		isbn TEXT,
		publisher TEXT,
		published_date TEXT,
		cover_url TEXT,

		-- Format and availability
		format TEXT,  -- ebook, audiobook, magazine
		is_available BOOLEAN DEFAULT 0,
		waitlist_size INTEGER,

		-- Tags associated with this book
		tags TEXT,  -- JSON array of tag names

		-- Sync tracking
		synced_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_tag_metadata_media_id ON libby_tag_metadata(media_id);
	CREATE INDEX IF NOT EXISTS idx_tag_metadata_title ON libby_tag_metadata(title);
	CREATE INDEX IF NOT EXISTS idx_tag_metadata_author ON libby_tag_metadata(author);
	CREATE INDEX IF NOT EXISTS idx_tag_metadata_synced ON libby_tag_metadata(synced_at DESC);

	-- Sync state for tracking what's been synced from external sources
	CREATE TABLE IF NOT EXISTS tbr_sync_state (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		source TEXT NOT NULL,          -- hardcover, libby_holds, libby_tags
		source_id TEXT NOT NULL,       -- ID from the source system
		tbr_book_id INTEGER,           -- Reference to tbr_books if synced
		sync_status TEXT NOT NULL,     -- synced, removed, error
		last_synced DATETIME DEFAULT CURRENT_TIMESTAMP,

		FOREIGN KEY (tbr_book_id) REFERENCES tbr_books(id),
		UNIQUE(source, source_id)
	);

	CREATE INDEX IF NOT EXISTS idx_sync_state_source ON tbr_sync_state(source);
	CREATE INDEX IF NOT EXISTS idx_sync_state_status ON tbr_sync_state(sync_status);
	`

	_, err := s.db.Exec(query)
	return err
}

// TBREntry represents a book in the TBR list
type TBREntry struct {
	ID              int64
	Title           string
	Subtitle        string
	Author          string
	ISBN10          string
	ISBN13          string
	HardcoverID     string
	LibbyMediaID    string
	OpenLibraryID   string
	Publisher       string
	PublishedDate   string
	PageCount       int
	Description     string
	CoverURL        string
	Genres          []string
	SeriesName      string
	SeriesPosition  float64
	SeriesTotal     int
	Source          string
	SourceMetadata  map[string]interface{}
	LibbyTags       []string
	LibbyHoldID     string
	LibbyAvailable  bool
	LibbyWaitlist   int
	Notes           string
	Priority        int
	AddedAt         time.Time
	UpdatedAt       time.Time
}

// LibbyTagMeta represents full metadata for a Libby tagged book
type LibbyTagMeta struct {
	ID            int64
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
	Tags          []string
	SyncedAt      time.Time
	UpdatedAt     time.Time
}

// AddBook adds a book to the TBR list
func (s *Store) AddBook(entry *TBREntry) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Serialize JSON fields
	genresJSON, _ := json.Marshal(entry.Genres)
	sourceMetaJSON, _ := json.Marshal(entry.SourceMetadata)
	libbyTagsJSON, _ := json.Marshal(entry.LibbyTags)

	query := `
		INSERT INTO tbr_books
		(title, subtitle, author, isbn10, isbn13, hardcover_id, libby_media_id, openlibrary_id,
		 publisher, published_date, page_count, description, cover_url, genres,
		 series_name, series_position, series_total, source, source_metadata,
		 libby_tags, libby_hold_id, libby_available, libby_waitlist_size, notes, priority, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(title, author) DO UPDATE SET
			subtitle = excluded.subtitle,
			isbn10 = COALESCE(excluded.isbn10, isbn10),
			isbn13 = COALESCE(excluded.isbn13, isbn13),
			hardcover_id = COALESCE(excluded.hardcover_id, hardcover_id),
			libby_media_id = COALESCE(excluded.libby_media_id, libby_media_id),
			publisher = COALESCE(excluded.publisher, publisher),
			published_date = COALESCE(excluded.published_date, published_date),
			page_count = COALESCE(excluded.page_count, page_count),
			description = COALESCE(excluded.description, description),
			cover_url = COALESCE(excluded.cover_url, cover_url),
			genres = COALESCE(excluded.genres, genres),
			series_name = COALESCE(excluded.series_name, series_name),
			series_position = COALESCE(excluded.series_position, series_position),
			series_total = COALESCE(excluded.series_total, series_total),
			libby_tags = COALESCE(excluded.libby_tags, libby_tags),
			libby_hold_id = COALESCE(excluded.libby_hold_id, libby_hold_id),
			libby_available = excluded.libby_available,
			libby_waitlist_size = COALESCE(excluded.libby_waitlist_size, libby_waitlist_size),
			notes = COALESCE(excluded.notes, notes),
			priority = COALESCE(excluded.priority, priority),
			updated_at = CURRENT_TIMESTAMP
	`

	result, err := s.db.Exec(query,
		entry.Title, entry.Subtitle, entry.Author,
		entry.ISBN10, entry.ISBN13, entry.HardcoverID, entry.LibbyMediaID, entry.OpenLibraryID,
		entry.Publisher, entry.PublishedDate, entry.PageCount, entry.Description, entry.CoverURL,
		string(genresJSON), entry.SeriesName, entry.SeriesPosition, entry.SeriesTotal,
		entry.Source, string(sourceMetaJSON), string(libbyTagsJSON),
		entry.LibbyHoldID, entry.LibbyAvailable, entry.LibbyWaitlist,
		entry.Notes, entry.Priority,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting TBR entry: %w", err)
	}

	id, _ := result.LastInsertId()
	return id, nil
}

// GetAll returns all TBR entries with pagination and filtering
func (s *Store) GetAll(source string, offset, limit int) ([]*TBREntry, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build query with optional source filter
	whereClause := ""
	args := []interface{}{}
	if source != "" {
		whereClause = "WHERE source = ?"
		args = append(args, source)
	}

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM tbr_books %s", whereClause)
	var total int
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting TBR entries: %w", err)
	}

	// Get paginated entries
	query := fmt.Sprintf(`
		SELECT id, title, subtitle, author, isbn10, isbn13, hardcover_id, libby_media_id, openlibrary_id,
		       publisher, published_date, page_count, description, cover_url, genres,
		       series_name, series_position, series_total, source, source_metadata,
		       libby_tags, libby_hold_id, libby_available, libby_waitlist_size, notes, priority,
		       added_at, updated_at
		FROM tbr_books
		%s
		ORDER BY priority DESC, added_at DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	args = append(args, limit, offset)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("querying TBR entries: %w", err)
	}
	defer rows.Close()

	entries := []*TBREntry{}
	for rows.Next() {
		entry := &TBREntry{}
		var genresJSON, sourceMetaJSON, libbyTagsJSON []byte
		var subtitle, isbn10, isbn13, hardcoverID, libbyMediaID, openLibID sql.NullString
		var publisher, pubDate, desc, coverURL sql.NullString
		var seriesName sql.NullString
		var seriesPos sql.NullFloat64
		var seriesTotal, pageCount, priority sql.NullInt64
		var libbyHoldID sql.NullString
		var libbyAvail sql.NullBool
		var libbyWait sql.NullInt64
		var notes sql.NullString

		err := rows.Scan(
			&entry.ID, &entry.Title, &subtitle, &entry.Author,
			&isbn10, &isbn13, &hardcoverID, &libbyMediaID, &openLibID,
			&publisher, &pubDate, &pageCount, &desc, &coverURL, &genresJSON,
			&seriesName, &seriesPos, &seriesTotal, &entry.Source, &sourceMetaJSON,
			&libbyTagsJSON, &libbyHoldID, &libbyAvail, &libbyWait, &notes, &priority,
			&entry.AddedAt, &entry.UpdatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning TBR entry: %w", err)
		}

		// Handle nullable fields
		if subtitle.Valid {
			entry.Subtitle = subtitle.String
		}
		if isbn10.Valid {
			entry.ISBN10 = isbn10.String
		}
		if isbn13.Valid {
			entry.ISBN13 = isbn13.String
		}
		if hardcoverID.Valid {
			entry.HardcoverID = hardcoverID.String
		}
		if libbyMediaID.Valid {
			entry.LibbyMediaID = libbyMediaID.String
		}
		if openLibID.Valid {
			entry.OpenLibraryID = openLibID.String
		}
		if publisher.Valid {
			entry.Publisher = publisher.String
		}
		if pubDate.Valid {
			entry.PublishedDate = pubDate.String
		}
		if pageCount.Valid {
			entry.PageCount = int(pageCount.Int64)
		}
		if desc.Valid {
			entry.Description = desc.String
		}
		if coverURL.Valid {
			entry.CoverURL = coverURL.String
		}
		if seriesName.Valid {
			entry.SeriesName = seriesName.String
		}
		if seriesPos.Valid {
			entry.SeriesPosition = seriesPos.Float64
		}
		if seriesTotal.Valid {
			entry.SeriesTotal = int(seriesTotal.Int64)
		}
		if libbyHoldID.Valid {
			entry.LibbyHoldID = libbyHoldID.String
		}
		if libbyAvail.Valid {
			entry.LibbyAvailable = libbyAvail.Bool
		}
		if libbyWait.Valid {
			entry.LibbyWaitlist = int(libbyWait.Int64)
		}
		if notes.Valid {
			entry.Notes = notes.String
		}
		if priority.Valid {
			entry.Priority = int(priority.Int64)
		}

		// Deserialize JSON fields
		if len(genresJSON) > 0 {
			json.Unmarshal(genresJSON, &entry.Genres)
		}
		if len(sourceMetaJSON) > 0 {
			json.Unmarshal(sourceMetaJSON, &entry.SourceMetadata)
		}
		if len(libbyTagsJSON) > 0 {
			json.Unmarshal(libbyTagsJSON, &entry.LibbyTags)
		}

		entries = append(entries, entry)
	}

	return entries, total, nil
}

// RemoveByID removes a TBR entry by ID
func (s *Store) RemoveByID(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM tbr_books WHERE id = ?", id)
	return err
}

// RemoveByTitleAuthor removes a TBR entry by title and author
func (s *Store) RemoveByTitleAuthor(title, author string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM tbr_books WHERE title = ? AND author = ?", title, author)
	return err
}

// Search searches TBR by title or author
func (s *Store) Search(query string, source string, offset, limit int) ([]*TBREntry, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build WHERE clause
	whereClause := "WHERE (title LIKE ? OR author LIKE ?)"
	args := []interface{}{
		"%" + query + "%",
		"%" + query + "%",
	}

	if source != "" {
		whereClause += " AND source = ?"
		args = append(args, source)
	}

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM tbr_books %s", whereClause)
	var total int
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting search results: %w", err)
	}

	// Get paginated results (reusing GetAll scanning logic)
	searchQuery := fmt.Sprintf(`
		SELECT id, title, subtitle, author, isbn10, isbn13, hardcover_id, libby_media_id, openlibrary_id,
		       publisher, published_date, page_count, description, cover_url, genres,
		       series_name, series_position, series_total, source, source_metadata,
		       libby_tags, libby_hold_id, libby_available, libby_waitlist_size, notes, priority,
		       added_at, updated_at
		FROM tbr_books
		%s
		ORDER BY priority DESC, added_at DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	args = append(args, limit, offset)
	rows, err := s.db.Query(searchQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("searching TBR: %w", err)
	}
	defer rows.Close()

	// Scan results (same as GetAll)
	entries := []*TBREntry{}
	for rows.Next() {
		entry := &TBREntry{}
		var genresJSON, sourceMetaJSON, libbyTagsJSON []byte
		var subtitle, isbn10, isbn13, hardcoverID, libbyMediaID, openLibID sql.NullString
		var publisher, pubDate, desc, coverURL sql.NullString
		var seriesName sql.NullString
		var seriesPos sql.NullFloat64
		var seriesTotal, pageCount, priority sql.NullInt64
		var libbyHoldID sql.NullString
		var libbyAvail sql.NullBool
		var libbyWait sql.NullInt64
		var notes sql.NullString

		err := rows.Scan(
			&entry.ID, &entry.Title, &subtitle, &entry.Author,
			&isbn10, &isbn13, &hardcoverID, &libbyMediaID, &openLibID,
			&publisher, &pubDate, &pageCount, &desc, &coverURL, &genresJSON,
			&seriesName, &seriesPos, &seriesTotal, &entry.Source, &sourceMetaJSON,
			&libbyTagsJSON, &libbyHoldID, &libbyAvail, &libbyWait, &notes, &priority,
			&entry.AddedAt, &entry.UpdatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning search result: %w", err)
		}

		// Handle nullables (same as GetAll)
		if subtitle.Valid {
			entry.Subtitle = subtitle.String
		}
		if isbn10.Valid {
			entry.ISBN10 = isbn10.String
		}
		if isbn13.Valid {
			entry.ISBN13 = isbn13.String
		}
		if hardcoverID.Valid {
			entry.HardcoverID = hardcoverID.String
		}
		if libbyMediaID.Valid {
			entry.LibbyMediaID = libbyMediaID.String
		}
		if openLibID.Valid {
			entry.OpenLibraryID = openLibID.String
		}
		if publisher.Valid {
			entry.Publisher = publisher.String
		}
		if pubDate.Valid {
			entry.PublishedDate = pubDate.String
		}
		if pageCount.Valid {
			entry.PageCount = int(pageCount.Int64)
		}
		if desc.Valid {
			entry.Description = desc.String
		}
		if coverURL.Valid {
			entry.CoverURL = coverURL.String
		}
		if seriesName.Valid {
			entry.SeriesName = seriesName.String
		}
		if seriesPos.Valid {
			entry.SeriesPosition = seriesPos.Float64
		}
		if seriesTotal.Valid {
			entry.SeriesTotal = int(seriesTotal.Int64)
		}
		if libbyHoldID.Valid {
			entry.LibbyHoldID = libbyHoldID.String
		}
		if libbyAvail.Valid {
			entry.LibbyAvailable = libbyAvail.Bool
		}
		if libbyWait.Valid {
			entry.LibbyWaitlist = int(libbyWait.Int64)
		}
		if notes.Valid {
			entry.Notes = notes.String
		}
		if priority.Valid {
			entry.Priority = int(priority.Int64)
		}

		// Deserialize JSON fields
		if len(genresJSON) > 0 {
			json.Unmarshal(genresJSON, &entry.Genres)
		}
		if len(sourceMetaJSON) > 0 {
			json.Unmarshal(sourceMetaJSON, &entry.SourceMetadata)
		}
		if len(libbyTagsJSON) > 0 {
			json.Unmarshal(libbyTagsJSON, &entry.LibbyTags)
		}

		entries = append(entries, entry)
	}

	return entries, total, nil
}

// SaveLibbyTagMetadata saves or updates Libby tag metadata
func (s *Store) SaveLibbyTagMetadata(meta *LibbyTagMeta) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tagsJSON, _ := json.Marshal(meta.Tags)

	query := `
		INSERT INTO libby_tag_metadata
		(media_id, title_id, title, subtitle, author, isbn, publisher, published_date,
		 cover_url, format, is_available, waitlist_size, tags, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(media_id) DO UPDATE SET
			title = excluded.title,
			subtitle = excluded.subtitle,
			author = excluded.author,
			isbn = COALESCE(excluded.isbn, isbn),
			publisher = COALESCE(excluded.publisher, publisher),
			published_date = COALESCE(excluded.published_date, published_date),
			cover_url = COALESCE(excluded.cover_url, cover_url),
			format = excluded.format,
			is_available = excluded.is_available,
			waitlist_size = excluded.waitlist_size,
			tags = excluded.tags,
			updated_at = CURRENT_TIMESTAMP
	`

	_, err := s.db.Exec(query,
		meta.MediaID, meta.TitleID, meta.Title, meta.Subtitle, meta.Author,
		meta.ISBN, meta.Publisher, meta.PublishedDate, meta.CoverURL,
		meta.Format, meta.IsAvailable, meta.WaitlistSize, string(tagsJSON),
	)
	return err
}

// GetLibbyTagMetadata retrieves all Libby tag metadata with pagination
func (s *Store) GetLibbyTagMetadata(tag string, offset, limit int) ([]*LibbyTagMeta, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build WHERE clause for tag filtering
	whereClause := ""
	args := []interface{}{}
	if tag != "" {
		whereClause = "WHERE tags LIKE ?"
		args = append(args, "%\""+tag+"\"%") // JSON array contains
	}

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM libby_tag_metadata %s", whereClause)
	var total int
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting tag metadata: %w", err)
	}

	// Get paginated entries
	query := fmt.Sprintf(`
		SELECT id, media_id, title_id, title, subtitle, author, isbn, publisher, published_date,
		       cover_url, format, is_available, waitlist_size, tags, synced_at, updated_at
		FROM libby_tag_metadata
		%s
		ORDER BY updated_at DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	args = append(args, limit, offset)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("querying tag metadata: %w", err)
	}
	defer rows.Close()

	metas := []*LibbyTagMeta{}
	for rows.Next() {
		meta := &LibbyTagMeta{}
		var tagsJSON []byte
		var subtitle, titleID, isbn, publisher, pubDate, coverURL, format sql.NullString
		var isAvail sql.NullBool
		var waitlist sql.NullInt64

		err := rows.Scan(
			&meta.ID, &meta.MediaID, &titleID, &meta.Title, &subtitle, &meta.Author,
			&isbn, &publisher, &pubDate, &coverURL, &format, &isAvail, &waitlist,
			&tagsJSON, &meta.SyncedAt, &meta.UpdatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scanning tag metadata: %w", err)
		}

		// Handle nullables
		if subtitle.Valid {
			meta.Subtitle = subtitle.String
		}
		if titleID.Valid {
			meta.TitleID = titleID.String
		}
		if isbn.Valid {
			meta.ISBN = isbn.String
		}
		if publisher.Valid {
			meta.Publisher = publisher.String
		}
		if pubDate.Valid {
			meta.PublishedDate = pubDate.String
		}
		if coverURL.Valid {
			meta.CoverURL = coverURL.String
		}
		if format.Valid {
			meta.Format = format.String
		}
		if isAvail.Valid {
			meta.IsAvailable = isAvail.Bool
		}
		if waitlist.Valid {
			meta.WaitlistSize = int(waitlist.Int64)
		}

		// Deserialize tags
		if len(tagsJSON) > 0 {
			json.Unmarshal(tagsJSON, &meta.Tags)
		}

		metas = append(metas, meta)
	}

	return metas, total, nil
}

// GetStats returns TBR statistics
func (s *Store) GetStats() (map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := make(map[string]interface{})

	// Total books
	var total int
	s.db.QueryRow("SELECT COUNT(*) FROM tbr_books").Scan(&total)
	stats["total_books"] = total

	// By source
	rows, err := s.db.Query(`
		SELECT source, COUNT(*) as count
		FROM tbr_books
		GROUP BY source
	`)
	if err == nil {
		bySrc := make(map[string]int)
		for rows.Next() {
			var src string
			var count int
			rows.Scan(&src, &count)
			bySrc[src] = count
		}
		rows.Close()
		stats["by_source"] = bySrc
	}

	// Libby availability
	var available, onHold int
	s.db.QueryRow("SELECT COUNT(*) FROM tbr_books WHERE libby_available = 1").Scan(&available)
	s.db.QueryRow("SELECT COUNT(*) FROM tbr_books WHERE libby_hold_id IS NOT NULL AND libby_hold_id != ''").Scan(&onHold)
	stats["libby_available"] = available
	stats["libby_on_hold"] = onHold

	// Libby tag metadata count
	var taggedBooks int
	s.db.QueryRow("SELECT COUNT(*) FROM libby_tag_metadata").Scan(&taggedBooks)
	stats["libby_tagged_books"] = taggedBooks

	return stats, nil
}

// Close closes the database connection
func (s *Store) Close() error {
	return s.db.Close()
}
