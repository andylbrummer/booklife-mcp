# Local Metadata Expansion Design

## Overview

This document describes the expansion of BookLife's local database schema and internal processes to support:

1. **Automatic metadata download** from public sources (Open Library, Hardcover, Libby)
2. **Category/genre-based search** with normalized taxonomy
3. **Summaries and reviews** comparison from multiple sources
4. **Hardcover updates from Libby data** (sync reading progress, finished books)
5. **User and LLM-generated annotations/tags** for enhanced search

## Design Principles

- **Fail fast**: No fallback data generation; report errors immediately
- **Lock-free where possible**: Use atomic operations and channels over mutexes
- **Exception neutral code**: Propagate errors cleanly
- **KDL for configuration**: All settings in booklife.kdl
- **SQLite for local storage**: Single database file for all local data

---

## Expanded Database Schema

### 1. Core Book Metadata Cache

```sql
-- Cached book metadata from all sources
-- Primary key is internal UUID, with cross-platform IDs for linking
CREATE TABLE books (
    id TEXT PRIMARY KEY,  -- Internal BookLife UUID

    -- Cross-platform identifiers
    isbn10 TEXT,
    isbn13 TEXT,
    hardcover_id TEXT,
    openlibrary_id TEXT,
    overdrive_id TEXT,
    wikidata_id TEXT,

    -- Core metadata
    title TEXT NOT NULL,
    subtitle TEXT,
    publisher TEXT,
    published_date TEXT,
    page_count INTEGER,
    audio_duration_seconds INTEGER,
    cover_url TEXT,

    -- Series info (denormalized for query performance)
    series_id TEXT,
    series_name TEXT,
    series_position REAL,  -- Supports 1.5 for novellas
    series_total INTEGER,

    -- Community ratings (cached)
    hardcover_rating REAL,
    hardcover_rating_count INTEGER,
    openlibrary_rating REAL,
    goodreads_rating REAL,  -- If available via other sources

    -- Sync metadata
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    metadata_sources TEXT,  -- JSON array of sources that contributed
    last_sync_at DATETIME,
    sync_priority INTEGER DEFAULT 0,  -- Higher = sync sooner

    -- Full-text search support
    search_text TEXT  -- Concatenated searchable content
);

CREATE INDEX idx_books_isbn10 ON books(isbn10);
CREATE INDEX idx_books_isbn13 ON books(isbn13);
CREATE INDEX idx_books_hardcover ON books(hardcover_id);
CREATE INDEX idx_books_openlibrary ON books(openlibrary_id);
CREATE INDEX idx_books_overdrive ON books(overdrive_id);
CREATE INDEX idx_books_title ON books(title);
CREATE INDEX idx_books_series ON books(series_id);
CREATE INDEX idx_books_updated ON books(updated_at);
CREATE INDEX idx_books_sync_priority ON books(sync_priority DESC, last_sync_at ASC);

-- Full-text search index
CREATE VIRTUAL TABLE books_fts USING fts5(
    title, subtitle, search_text,
    content='books',
    content_rowid='rowid'
);
```

### 2. Contributors (Authors, Narrators, etc.)

```sql
-- Normalized contributor table
CREATE TABLE contributors (
    id TEXT PRIMARY KEY,  -- UUID or provider ID
    name TEXT NOT NULL,
    sort_name TEXT,  -- "Last, First" for sorting

    -- Cross-platform IDs
    hardcover_author_id TEXT,
    openlibrary_author_id TEXT,
    wikidata_id TEXT,

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_contributors_name ON contributors(name);
CREATE INDEX idx_contributors_sort ON contributors(sort_name);

-- Many-to-many: books <-> contributors with role
CREATE TABLE book_contributors (
    book_id TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    contributor_id TEXT NOT NULL REFERENCES contributors(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'author',  -- author, narrator, illustrator, editor, translator
    position INTEGER DEFAULT 0,  -- Order of appearance
    PRIMARY KEY (book_id, contributor_id, role)
);

CREATE INDEX idx_book_contributors_book ON book_contributors(book_id);
CREATE INDEX idx_book_contributors_contributor ON book_contributors(contributor_id);
CREATE INDEX idx_book_contributors_role ON book_contributors(role);
```

### 3. Normalized Taxonomy (Categories, Genres, Subjects)

```sql
-- Unified taxonomy with hierarchical support
-- Supports categories, genres, subjects, themes, tropes
CREATE TABLE taxonomy_terms (
    id TEXT PRIMARY KEY,  -- Slugified canonical name
    name TEXT NOT NULL,  -- Display name
    term_type TEXT NOT NULL,  -- category, genre, subject, theme, trope

    -- Hierarchy
    parent_id TEXT REFERENCES taxonomy_terms(id),
    level INTEGER DEFAULT 0,  -- Depth in hierarchy
    path TEXT,  -- Materialized path for efficient queries: "/fiction/fantasy/epic"

    -- Mappings to external systems
    hardcover_genre_id TEXT,
    openlibrary_subject TEXT,
    bisac_code TEXT,  -- Industry standard book categorization

    -- Usage stats
    book_count INTEGER DEFAULT 0,

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_taxonomy_type ON taxonomy_terms(term_type);
CREATE INDEX idx_taxonomy_parent ON taxonomy_terms(parent_id);
CREATE INDEX idx_taxonomy_path ON taxonomy_terms(path);
CREATE INDEX idx_taxonomy_name ON taxonomy_terms(name);

-- Many-to-many: books <-> taxonomy terms
CREATE TABLE book_taxonomy (
    book_id TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    term_id TEXT NOT NULL REFERENCES taxonomy_terms(id) ON DELETE CASCADE,
    source TEXT NOT NULL,  -- hardcover, openlibrary, user, llm
    confidence REAL DEFAULT 1.0,  -- 0.0-1.0 for LLM-generated
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (book_id, term_id, source)
);

CREATE INDEX idx_book_taxonomy_book ON book_taxonomy(book_id);
CREATE INDEX idx_book_taxonomy_term ON book_taxonomy(term_id);
CREATE INDEX idx_book_taxonomy_source ON book_taxonomy(source);

-- Taxonomy aliases for search
CREATE TABLE taxonomy_aliases (
    alias TEXT NOT NULL,
    term_id TEXT NOT NULL REFERENCES taxonomy_terms(id) ON DELETE CASCADE,
    PRIMARY KEY (alias, term_id)
);

CREATE INDEX idx_taxonomy_aliases ON taxonomy_aliases(alias);
```

### 4. Descriptions, Summaries, and Reviews

```sql
-- Book descriptions/summaries from multiple sources
CREATE TABLE book_descriptions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    book_id TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    source TEXT NOT NULL,  -- openlibrary, hardcover, libby, wikipedia, user, llm
    description_type TEXT NOT NULL,  -- blurb, summary, synopsis, first_sentence
    content TEXT NOT NULL,
    content_length INTEGER,  -- For quick filtering by length
    language TEXT DEFAULT 'en',

    -- LLM-specific metadata
    llm_model TEXT,  -- e.g., "claude-3-opus"
    llm_prompt_hash TEXT,  -- Hash of prompt for cache invalidation

    -- Quality indicators
    is_spoiler_free INTEGER DEFAULT 1,
    quality_score REAL,  -- 0-1, can be user-rated or computed

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,

    UNIQUE(book_id, source, description_type)
);

CREATE INDEX idx_descriptions_book ON book_descriptions(book_id);
CREATE INDEX idx_descriptions_source ON book_descriptions(source);
CREATE INDEX idx_descriptions_type ON book_descriptions(description_type);

-- Reviews from various sources (for comparison)
CREATE TABLE book_reviews (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    book_id TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    source TEXT NOT NULL,  -- hardcover, libby, storygraph, user, llm

    -- Review content
    rating REAL,  -- Normalized 0-5 scale
    review_text TEXT,
    headline TEXT,

    -- Attribution
    reviewer_id TEXT,  -- External reviewer ID if available
    reviewer_name TEXT,
    reviewer_url TEXT,

    -- Metadata
    review_date DATETIME,
    is_verified_reader INTEGER DEFAULT 0,
    helpful_count INTEGER DEFAULT 0,

    -- For LLM-generated analysis
    llm_model TEXT,
    sentiment_score REAL,  -- -1 to 1
    key_themes TEXT,  -- JSON array

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,

    UNIQUE(book_id, source, reviewer_id)
);

CREATE INDEX idx_reviews_book ON book_reviews(book_id);
CREATE INDEX idx_reviews_source ON book_reviews(source);
CREATE INDEX idx_reviews_rating ON book_reviews(rating);
```

### 5. User and LLM Annotations/Tags

```sql
-- User-defined tags (simple key-value with optional hierarchy)
CREATE TABLE user_tags (
    id TEXT PRIMARY KEY,  -- Slugified tag name
    name TEXT NOT NULL,
    color TEXT,  -- Hex color for display
    icon TEXT,  -- Optional icon name

    -- Hierarchy support
    parent_id TEXT REFERENCES user_tags(id),
    path TEXT,  -- Materialized path

    -- Usage stats
    book_count INTEGER DEFAULT 0,

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_user_tags_name ON user_tags(name);
CREATE INDEX idx_user_tags_path ON user_tags(path);

-- Book-tag associations
CREATE TABLE book_user_tags (
    book_id TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    tag_id TEXT NOT NULL REFERENCES user_tags(id) ON DELETE CASCADE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (book_id, tag_id)
);

CREATE INDEX idx_book_user_tags_book ON book_user_tags(book_id);
CREATE INDEX idx_book_user_tags_tag ON book_user_tags(tag_id);

-- Rich annotations (notes, highlights, analysis)
CREATE TABLE annotations (
    id TEXT PRIMARY KEY,  -- UUID
    book_id TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE,

    -- Annotation content
    annotation_type TEXT NOT NULL,  -- note, highlight, bookmark, analysis, comparison
    content TEXT NOT NULL,

    -- Location (optional, for highlights/bookmarks)
    chapter TEXT,
    page_number INTEGER,
    position_percent REAL,  -- 0.0-100.0
    quote TEXT,  -- The highlighted text

    -- Source
    created_by TEXT NOT NULL,  -- user, llm, import
    llm_model TEXT,
    llm_prompt TEXT,

    -- Organization
    tags TEXT,  -- JSON array of tag IDs for quick filtering
    is_private INTEGER DEFAULT 1,
    is_spoiler INTEGER DEFAULT 0,

    -- Timestamps
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_annotations_book ON annotations(book_id);
CREATE INDEX idx_annotations_type ON annotations(annotation_type);
CREATE INDEX idx_annotations_created_by ON annotations(created_by);

-- LLM-generated book analysis (structured)
CREATE TABLE llm_analysis (
    id TEXT PRIMARY KEY,
    book_id TEXT NOT NULL REFERENCES books(id) ON DELETE CASCADE,

    -- Analysis type
    analysis_type TEXT NOT NULL,  -- themes, mood, pacing, writing_style, comparisons, recommendations

    -- Structured results (JSON)
    results TEXT NOT NULL,  -- JSON object with analysis results

    -- Model info
    llm_model TEXT NOT NULL,
    prompt_version TEXT,  -- For invalidation when prompts change
    input_sources TEXT,  -- JSON array of source descriptions used

    -- Quality
    confidence REAL,  -- 0-1

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME,  -- Optional TTL for re-analysis

    UNIQUE(book_id, analysis_type, llm_model)
);

CREATE INDEX idx_llm_analysis_book ON llm_analysis(book_id);
CREATE INDEX idx_llm_analysis_type ON llm_analysis(analysis_type);
```

### 6. Reading Status and Progress (Local Mirror)

```sql
-- Local cache of user reading status (mirrors Hardcover + local tracking)
CREATE TABLE reading_status (
    book_id TEXT PRIMARY KEY REFERENCES books(id) ON DELETE CASCADE,

    -- Status
    status TEXT NOT NULL,  -- reading, read, want-to-read, dnf, abandoned
    progress_percent INTEGER DEFAULT 0,
    current_page INTEGER,

    -- Dates
    date_added DATETIME,
    date_started DATETIME,
    date_finished DATETIME,

    -- Rating/Review
    rating REAL,
    review TEXT,

    -- Source tracking
    source TEXT NOT NULL,  -- hardcover, libby, local
    hardcover_user_book_id TEXT,  -- For sync

    -- Sync state
    last_synced_at DATETIME,
    needs_sync INTEGER DEFAULT 0,  -- Flag for pending changes

    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_reading_status_status ON reading_status(status);
CREATE INDEX idx_reading_status_needs_sync ON reading_status(needs_sync);
```

### 7. Sync Queue and State

```sql
-- Pending sync operations (for Libby -> Hardcover, etc.)
CREATE TABLE sync_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    -- Operation
    operation TEXT NOT NULL,  -- update_status, add_book, update_progress, update_rating
    source_system TEXT NOT NULL,  -- libby, local
    target_system TEXT NOT NULL,  -- hardcover

    -- Payload
    book_id TEXT REFERENCES books(id),
    payload TEXT NOT NULL,  -- JSON with operation details

    -- State
    status TEXT DEFAULT 'pending',  -- pending, in_progress, completed, failed, cancelled
    attempts INTEGER DEFAULT 0,
    max_attempts INTEGER DEFAULT 3,
    last_attempt_at DATETIME,
    error_message TEXT,

    -- Priority and scheduling
    priority INTEGER DEFAULT 0,  -- Higher = sooner
    scheduled_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME,

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_sync_queue_status ON sync_queue(status, priority DESC, scheduled_at);
CREATE INDEX idx_sync_queue_book ON sync_queue(book_id);

-- Sync state tracking
CREATE TABLE sync_state (
    system_pair TEXT PRIMARY KEY,  -- e.g., "libby:hardcover"
    last_sync_at DATETIME,
    last_sync_status TEXT,
    cursor_position TEXT,  -- For incremental sync
    items_synced INTEGER DEFAULT 0,
    errors_count INTEGER DEFAULT 0
);

-- ID mappings between systems
CREATE TABLE id_mappings (
    source_system TEXT NOT NULL,
    source_id TEXT NOT NULL,
    target_system TEXT NOT NULL,
    target_id TEXT NOT NULL,
    book_id TEXT REFERENCES books(id),
    confidence REAL DEFAULT 1.0,  -- For fuzzy matches
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (source_system, source_id, target_system)
);

CREATE INDEX idx_id_mappings_target ON id_mappings(target_system, target_id);
CREATE INDEX idx_id_mappings_book ON id_mappings(book_id);
```

---

## Internal Processes

### 1. Metadata Sync Manager

The sync manager handles automatic metadata download from public sources.

```go
// internal/metadata/sync_manager.go

type SyncManager struct {
    db          *sql.DB
    providers   *ProviderSet
    queue       chan SyncJob
    rateLimiter *rate.Limiter
    ctx         context.Context
    cancel      context.CancelFunc
}

type SyncJob struct {
    BookID      string
    ISBN        string
    Title       string
    Sources     []string  // Which sources to query
    Priority    int
    RequireAll  bool      // Wait for all sources or return first
}

// SyncPriorities
const (
    PriorityImmediate = 100  // User requested
    PriorityActive    = 50   // Currently reading
    PriorityRecent    = 25   // Recently added
    PriorityBatch     = 0    // Background sync
)
```

**Sync Workflow:**

1. **On Book Addition**: Queue immediate metadata fetch
2. **On Libby Loan**: Queue metadata + availability update
3. **Background Batch**: Periodically refresh stale metadata (>30 days old)
4. **Priority Queue**: Active reads synced more frequently

### 2. Taxonomy Normalization Engine

Maps external categories to internal normalized taxonomy.

```go
// internal/taxonomy/normalizer.go

type Normalizer struct {
    mappings map[string]string  // External -> internal term ID
    aliases  map[string]string  // Common variations
}

// Example mappings:
// "Science Fiction" -> "sci-fi"
// "SF" -> "sci-fi" (alias)
// "Sci-Fi & Fantasy" -> ["sci-fi", "fantasy"] (split)
// "Literary Fiction" -> "fiction/literary" (hierarchy)

func (n *Normalizer) Normalize(source string, terms []string) []TaxonomyAssignment {
    // 1. Clean and lowercase
    // 2. Check exact mappings
    // 3. Check aliases
    // 4. Fuzzy match with Levenshtein distance
    // 5. Return with confidence scores
}
```

### 3. Hardcover-Libby Sync Process

Bidirectional sync between Libby activity and Hardcover library.

```go
// internal/sync/libby_hardcover.go

type LibbyHardcoverSync struct {
    libby     LibbyProvider
    hardcover HardcoverProvider
    store     *metadata.Store
    queue     *SyncQueue
}

// Sync operations:

// 1. Libby Loan Start -> Hardcover "reading" status
func (s *LibbyHardcoverSync) OnLoanStart(loan LibbyLoan) error {
    // Find or create book in local cache
    book := s.store.FindByISBN(loan.ISBN)
    if book == nil {
        book = s.store.CreateFromLoan(loan)
        s.queueMetadataFetch(book.ID, PriorityImmediate)
    }

    // Queue Hardcover status update
    return s.queue.Enqueue(SyncOperation{
        Type:   OpUpdateStatus,
        Target: "hardcover",
        BookID: book.ID,
        Status: "reading",
    })
}

// 2. Libby Loan Return -> Hardcover "read" status
func (s *LibbyHardcoverSync) OnLoanReturn(titleID string) error {
    book := s.store.FindByOverdriveID(titleID)
    if book == nil {
        return nil  // Book not tracked
    }

    return s.queue.Enqueue(SyncOperation{
        Type:     OpUpdateStatus,
        Target:   "hardcover",
        BookID:   book.ID,
        Status:   "read",
        Finished: time.Now(),
    })
}

// 3. Libby Progress -> Hardcover progress (periodic)
func (s *LibbyHardcoverSync) SyncProgress() error {
    loans, _ := s.libby.GetLoans(ctx)
    for _, loan := range loans {
        if loan.Progress > 0 {
            s.queue.Enqueue(SyncOperation{
                Type:     OpUpdateProgress,
                Target:   "hardcover",
                BookID:   s.store.FindByOverdriveID(loan.MediaID).ID,
                Progress: int(loan.Progress * 100),
            })
        }
    }
    return nil
}
```

### 4. Annotation and Tag Manager

Handles user and LLM-generated annotations.

```go
// internal/annotations/manager.go

type AnnotationManager struct {
    store *metadata.Store
    llm   LLMClient  // Optional, for LLM-generated analysis
}

// User operations
func (m *AnnotationManager) AddTag(bookID, tagName string) error
func (m *AnnotationManager) RemoveTag(bookID, tagID string) error
func (m *AnnotationManager) AddNote(bookID, content string, location *Location) error
func (m *AnnotationManager) AddHighlight(bookID, quote, note string, loc Location) error

// LLM operations (run async)
func (m *AnnotationManager) GenerateThemeAnalysis(bookID string) error
func (m *AnnotationManager) GenerateMoodTags(bookID string) error
func (m *AnnotationManager) GenerateComparisons(bookID string, compareToIDs []string) error
func (m *AnnotationManager) GenerateSummary(bookID string, spoilerLevel int) error

// Search operations
func (m *AnnotationManager) SearchByTags(tagIDs []string, op string) ([]Book, error)
func (m *AnnotationManager) SearchByAnnotationContent(query string) ([]Annotation, error)
func (m *AnnotationManager) FindSimilarByTags(bookID string, limit int) ([]Book, error)
```

### 5. Category Search Engine

Efficient search by category with hierarchical support.

```go
// internal/search/category_search.go

type CategorySearch struct {
    store *metadata.Store
}

// Search modes
func (s *CategorySearch) ByCategory(termID string, includeChildren bool) ([]Book, error)
func (s *CategorySearch) ByCategoryPath(path string) ([]Book, error)  // "/fiction/fantasy"
func (s *CategorySearch) ByMultipleCategories(termIDs []string, mode string) ([]Book, error)  // AND/OR
func (s *CategorySearch) WithFilters(cats []string, filters SearchFilters) ([]Book, error)

type SearchFilters struct {
    MinRating     float64
    MaxPageCount  int
    MinPageCount  int
    Status        []string  // User reading status filter
    Sources       []string  // Filter by metadata source
    HasAudiobook  bool
    InLibrary     bool      // Available at user's library
    Tags          []string  // User tags
}
```

---

## Data Flow Diagrams

### Metadata Sync Flow

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Libby     │     │  Hardcover  │     │ OpenLibrary │
│  Timeline   │     │   Library   │     │    API      │
└──────┬──────┘     └──────┬──────┘     └──────┬──────┘
       │                   │                   │
       ▼                   ▼                   ▼
┌──────────────────────────────────────────────────────┐
│                   Sync Manager                        │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐     │
│  │  Importer  │  │   Queue    │  │Rate Limiter│     │
│  └─────┬──────┘  └─────┬──────┘  └─────┬──────┘     │
└────────┼───────────────┼───────────────┼─────────────┘
         │               │               │
         ▼               ▼               ▼
┌──────────────────────────────────────────────────────┐
│                 SQLite Database                       │
│  ┌────────┐ ┌──────────┐ ┌───────────┐ ┌──────────┐ │
│  │ books  │ │ taxonomy │ │descriptions│ │ reviews  │ │
│  └────────┘ └──────────┘ └───────────┘ └──────────┘ │
│  ┌────────────┐ ┌───────────┐ ┌────────────────────┐ │
│  │ annotations│ │  user_tags │ │ reading_status   │ │
│  └────────────┘ └───────────┘ └────────────────────┘ │
└──────────────────────────────────────────────────────┘
```

### Libby -> Hardcover Sync Flow

```
┌──────────────────┐
│  Libby Provider  │
│  (Loan Events)   │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐     ┌──────────────────┐
│  Event Handler   │────▶│  ID Resolver     │
│  (OnLoan/Return) │     │  (ISBN/Title)    │
└────────┬─────────┘     └────────┬─────────┘
         │                        │
         ▼                        ▼
┌──────────────────┐     ┌──────────────────┐
│  Local Cache     │◀────│ Hardcover Lookup │
│  (books table)   │     │  (GraphQL)       │
└────────┬─────────┘     └──────────────────┘
         │
         ▼
┌──────────────────┐
│   Sync Queue     │
│  (sync_queue)    │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Queue Processor  │
│  (Background)    │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Hardcover API    │
│  (Mutations)     │
└──────────────────┘
```

---

## MCP Tool Additions

### New Tools

```
search_by_category
  - input: category (string), include_children (bool), filters (object)
  - output: paginated books matching category

compare_descriptions
  - input: book_id (string)
  - output: descriptions from all sources for comparison

compare_reviews
  - input: book_id (string), min_rating (float)
  - output: reviews from all sources with sentiment analysis

add_annotation
  - input: book_id, type (note|highlight|bookmark), content, location?
  - output: annotation_id

add_tag
  - input: book_id, tag_name, color?
  - output: tag assignment

search_annotations
  - input: query (string), types? (array), book_id? (string)
  - output: matching annotations with book context

generate_analysis
  - input: book_id, analysis_type (themes|mood|style|comparisons)
  - output: LLM-generated structured analysis

sync_libby_to_hardcover
  - input: none (uses configured credentials)
  - output: sync summary (added, updated, errors)

get_sync_status
  - input: none
  - output: pending sync operations, last sync times
```

### New Resources

```
booklife://categories
  - List all taxonomy terms organized by type

booklife://categories/{path}
  - Books in a specific category path

booklife://book/{id}/descriptions
  - All descriptions for a book from all sources

booklife://book/{id}/reviews
  - All reviews for a book from all sources

booklife://book/{id}/annotations
  - User and LLM annotations for a book

booklife://tags
  - User-defined tags with book counts

booklife://sync/status
  - Current sync queue state
```

---

## Search Result Format Improvements

### Current Problem

Search results currently return:
- Text content (human-readable)
- Metadata with only counts (`{"query": "...", "total": 1}`)

Missing:
- Entry IDs for follow-up queries
- Compact summaries suitable for LLM context
- Structured data for programmatic use

### Improved Search Response Format

All search tools should return structured results with IDs:

```go
// SearchResultEntry is a compact summary for list views
type SearchResultEntry struct {
    ID          string `json:"id"`           // Entry ID for get_detail calls
    Title       string `json:"title"`
    Author      string `json:"author"`
    Summary     string `json:"summary"`      // 2-line summary
    Activity    string `json:"activity,omitempty"`
    Date        string `json:"date,omitempty"`
    Format      string `json:"format,omitempty"`
}

// SearchResult is the structured response
type SearchResult struct {
    Query       string              `json:"query"`
    Total       int                 `json:"total"`
    Page        int                 `json:"page"`
    PageSize    int                 `json:"page_size"`
    HasMore     bool                `json:"has_more"`
    Entries     []SearchResultEntry `json:"entries"`
}
```

### Updated history_search Handler

```go
func (s *Server) handleSearchLocalHistory(...) (*mcp.CallToolResult, any, error) {
    // ... validation ...

    entries, total, err := s.historyStore.SearchHistory(input.Query, offset, pageSize)
    if err != nil {
        return nil, nil, fmt.Errorf("searching history: %w", err)
    }

    // Build structured results
    results := make([]SearchResultEntry, 0, len(entries))
    for _, entry := range entries {
        date := time.UnixMilli(entry.Timestamp).Format("2006-01-02")
        summary := fmt.Sprintf("%s • %s • %s", entry.Activity, date, entry.Library)

        results = append(results, SearchResultEntry{
            ID:       entry.TitleID,
            Title:    entry.Title,
            Author:   entry.Author,
            Summary:  summary,
            Activity: entry.Activity,
            Date:     date,
            Format:   entry.Format,
        })
    }

    // Build text content with IDs
    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("🔍 \"%s\" - %d results\n\n", input.Query, total))

    for _, r := range results {
        sb.WriteString(fmt.Sprintf("[%s] %s by %s\n", r.ID, r.Title, r.Author))
        sb.WriteString(fmt.Sprintf("    %s\n\n", r.Summary))
    }

    if total > offset+pageSize {
        sb.WriteString(fmt.Sprintf("→ Use page=%d for more results\n", page+1))
        sb.WriteString(fmt.Sprintf("→ Use history_get_entry id=\"<id>\" for full details\n"))
    }

    return &mcp.CallToolResult{
        Content: []mcp.Content{
            &mcp.TextContent{Text: sb.String()},
        },
    }, SearchResult{
        Query:    input.Query,
        Total:    total,
        Page:     page,
        PageSize: pageSize,
        HasMore:  total > offset+pageSize,
        Entries:  results,
    }, nil
}
```

### New Tool: history_get_entry

```go
// GetHistoryEntryInput for fetching a single entry by ID
type GetHistoryEntryInput struct {
    ID string `json:"id"`  // TitleID from search results
}

func (s *Server) handleGetHistoryEntry(...) (*mcp.CallToolResult, any, error) {
    entry, err := s.historyStore.GetByTitleID(input.ID)
    if err != nil {
        return nil, nil, fmt.Errorf("entry not found: %w", err)
    }

    // Return full entry details including all activities for this title
    allActivities, _ := s.historyStore.GetAllActivitiesForTitle(input.ID)

    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("📖 %s\n", entry.Title))
    sb.WriteString(fmt.Sprintf("   Author: %s\n", entry.Author))
    sb.WriteString(fmt.Sprintf("   Publisher: %s\n", entry.Publisher))
    sb.WriteString(fmt.Sprintf("   ISBN: %s\n", entry.ISBN))
    sb.WriteString(fmt.Sprintf("   Format: %s\n", entry.Format))
    sb.WriteString(fmt.Sprintf("   Library: %s\n\n", entry.Library))

    sb.WriteString("Activity History:\n")
    for _, act := range allActivities {
        date := time.UnixMilli(act.Timestamp).Format("2006-01-02")
        sb.WriteString(fmt.Sprintf("  • %s on %s\n", act.Activity, date))
    }

    return &mcp.CallToolResult{
        Content: []mcp.Content{
            &mcp.TextContent{Text: sb.String()},
        },
    }, entry, nil
}
```

### Store Methods to Add

```go
// GetByTitleID returns the most recent entry for a title
func (s *Store) GetByTitleID(titleID string) (*models.TimelineEntry, error) {
    query := `
        SELECT title_id, title, author, publisher, isbn, timestamp, activity,
               details, library, library_key, format, cover_url, color
        FROM history
        WHERE title_id = ?
        ORDER BY timestamp DESC
        LIMIT 1
    `
    // ... scan and return
}

// GetAllActivitiesForTitle returns all activities for a title (for history view)
func (s *Store) GetAllActivitiesForTitle(titleID string) ([]models.TimelineEntry, error) {
    query := `
        SELECT title_id, title, author, publisher, isbn, timestamp, activity,
               details, library, library_key, format, cover_url, color
        FROM history
        WHERE title_id = ?
        ORDER BY timestamp DESC
    `
    // ... scan and return all
}
```

### Search Pattern for All Tools

Apply this pattern to all search/list tools:

| Tool | Summary Line Format | Detail Tool |
|------|---------------------|-------------|
| `history_search` | `Activity • Date • Library` | `history_get_entry` |
| `search_books` | `Author • Year • Rating` | `get_book` |
| `search_library` | `Format • Availability • Wait` | `get_library_item` |
| `search_by_category` | `Author • Genres • Status` | `get_book` |
| `search_annotations` | `Type • Book • Created` | `get_annotation` |

---

## Configuration Additions

```kdl
// booklife.kdl - sync configuration

sync {
    libby-to-hardcover {
        enabled true
        sync-on-return true      // Sync when book returned
        auto-mark-read true      // Mark as "read" in Hardcover
        include-audio true       // Include audiobooks
        include-ebooks true      // Include ebooks
    }
}

// booklife.kdl additions

metadata {
    // Automatic sync settings
    auto-sync true
    sync-interval-hours 6

    // Stale data threshold
    refresh-after-days 30

    // Priority settings
    active-reading-priority 50
    recent-add-priority 25
}

sync {
    // Libby -> Hardcover sync
    libby-to-hardcover {
        enabled true
        sync-on-loan true
        sync-on-return true
        sync-progress true
        progress-sync-interval-hours 1
    }

    // Conflict resolution
    conflict-strategy "newest-wins"  // or "source-priority"
    source-priority ["hardcover", "libby", "local"]
}

annotations {
    // LLM analysis settings
    llm-analysis-enabled true
    llm-model "claude-3-haiku"  // Use cheaper model for analysis

    // Auto-generate on book add
    auto-analyze-themes false
    auto-analyze-mood false
    auto-generate-summary true
}

taxonomy {
    // Custom category mappings file (optional)
    custom-mappings-path "~/.config/booklife/taxonomy-mappings.kdl"

    // Include user-defined categories in search
    include-user-tags true
}
```

---

## Migration Strategy

### Phase 1: Schema Creation
1. Add new tables with proper indexes
2. Maintain backward compatibility with existing history table
3. Add migration version tracking

### Phase 2: Data Population
1. Import existing Hardcover library to local cache
2. Run metadata enrichment from Open Library
3. Build initial taxonomy from existing genres/subjects

### Phase 3: Sync Activation
1. Enable Libby -> Hardcover sync
2. Start background metadata refresh
3. Enable LLM analysis for new books

### Phase 4: Search Enhancement
1. Enable category-based search
2. Expose annotation search
3. Add comparison tools

---

## Performance Considerations

1. **Indexes**: All foreign keys and search columns indexed
2. **FTS5**: Full-text search for descriptions and annotations
3. **Materialized Paths**: Fast hierarchical queries for taxonomy
4. **Batch Operations**: Sync queue processes in batches
5. **Rate Limiting**: Respect API limits (Open Library: 10 req/sec)
6. **Lazy Loading**: Descriptions/reviews fetched on demand
7. **Cache TTL**: Configurable staleness thresholds
