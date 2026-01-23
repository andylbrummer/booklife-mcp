package models

import "time"

// Book represents a unified book entity across all sources
type Book struct {
	// Universal identifiers
	ID     string `json:"id"` // Internal BookLife ID
	ISBN10 string `json:"isbn10,omitempty"`
	ISBN13 string `json:"isbn13,omitempty"`

	// Cross-platform IDs
	HardcoverID string `json:"hardcover_id,omitempty"`
	OpenLibID   string `json:"openlibrary_id,omitempty"`
	WikidataID  string `json:"wikidata_id,omitempty"`
	OverdriveID string `json:"overdrive_id,omitempty"`

	// Core metadata
	Title    string        `json:"title"`
	Subtitle string        `json:"subtitle,omitempty"`
	Authors  []Contributor `json:"authors"`

	// Extended metadata
	Publisher     string `json:"publisher,omitempty"`
	PublishedDate string `json:"published_date,omitempty"`
	PageCount     int    `json:"page_count,omitempty"`
	AudioDuration int    `json:"audio_duration_seconds,omitempty"`

	// Classification
	Genres   []string `json:"genres,omitempty"`
	Subjects []string `json:"subjects,omitempty"`

	// Series info
	Series *SeriesInfo `json:"series,omitempty"`

	// Description
	Description string `json:"description,omitempty"`

	// Cover images
	CoverURL string `json:"cover_url,omitempty"`

	// User-specific data (from Hardcover)
	UserStatus *UserBookStatus `json:"user_status,omitempty"`

	// Availability
	LibraryAvailability *LibraryAvailability `json:"library_availability,omitempty"`

	// Community data
	HardcoverRating float64 `json:"hardcover_rating,omitempty"`
	HardcoverCount  int     `json:"hardcover_rating_count,omitempty"`
}

// Contributor represents an author, narrator, illustrator, etc.
type Contributor struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
	Role string `json:"role"` // author, narrator, illustrator, etc.
}

// NewContributor creates a Contributor with the default role of "author".
func NewContributor(name string) Contributor {
	return Contributor{Name: name, Role: "author"}
}

// NewContributorWithRole creates a Contributor with an explicit role.
func NewContributorWithRole(name, role string) Contributor {
	return Contributor{Name: name, Role: role}
}

// ContributorsFromNames creates a slice of author Contributors from names.
func ContributorsFromNames(names []string) []Contributor {
	result := make([]Contributor, 0, len(names))
	for _, name := range names {
		result = append(result, NewContributor(name))
	}
	return result
}

// SeriesInfo represents book's position in a series
type SeriesInfo struct {
	ID       string  `json:"id,omitempty"`
	Name     string  `json:"name"`
	Position float64 `json:"position"` // Supports 1.5 for novellas
	Total    int     `json:"total,omitempty"`
}

// UserBookStatus represents user's personal data about a book
type UserBookStatus struct {
	Status       string     `json:"status"`             // reading, read, want-to-read, dnf
	Progress     int        `json:"progress,omitempty"` // 0-100
	Rating       float64    `json:"rating,omitempty"`
	Review       string     `json:"review,omitempty"`
	DateStarted  *time.Time `json:"date_started,omitempty"`
	DateFinished *time.Time `json:"date_finished,omitempty"`
	DateAdded    time.Time  `json:"date_added"`
}

// LibraryAvailability represents availability at a library
type LibraryAvailability struct {
	LibraryName string   `json:"library_name"`
	MediaID     string   `json:"media_id"`
	Formats     []string `json:"formats"` // ebook, audiobook

	// Per-format availability
	EbookAvailable    bool `json:"ebook_available"`
	EbookCopies       int  `json:"ebook_copies"`
	EbookWaitlistSize int  `json:"ebook_waitlist_size"`

	AudiobookAvailable    bool `json:"audiobook_available"`
	AudiobookCopies       int  `json:"audiobook_copies"`
	AudiobookWaitlistSize int  `json:"audiobook_waitlist_size"`

	// Per-format identifiers (for cross-format ISBN lookup)
	EbookID     string `json:"ebook_id,omitempty"`     // Overdrive format ID for ebook
	AudiobookID string `json:"audiobook_id,omitempty"` // Overdrive format ID for audiobook

	EstimatedWaitDays int `json:"estimated_wait_days,omitempty"`
}

// LibbyLoan represents a current library loan
type LibbyLoan struct {
	ID           string    `json:"id"`
	MediaID      string    `json:"media_id"`
	Title        string    `json:"title"`
	Author       string    `json:"author"`
	CoverURL     string    `json:"cover_url"`
	Format       string    `json:"format"` // ebook, audiobook
	CheckoutDate time.Time `json:"checkout_date"`
	DueDate      time.Time `json:"due_date"`
	Progress     float64   `json:"progress"` // 0.0-1.0
	IsReturned   bool      `json:"is_returned"`
}

// LibbySearchResult represents a library catalog search result
type LibbySearchResult struct {
	ID             string `json:"id"`
	MediaID        string `json:"media_id"`
	Title          string `json:"title"`
	Subtitle       string `json:"subtitle,omitempty"`
	Author         string `json:"author"`
	ISBN           string `json:"isbn,omitempty"`
	Publisher      string `json:"publisher,omitempty"`
	PublishingYear int    `json:"publishing_year,omitempty"`
	CoverURL       string `json:"cover_url,omitempty"`
	Format         struct {
		Ebook     bool `json:"ebook"`
		Audiobook bool `json:"audiobook"`
		Magazine  bool `json:"magazine"`
	} `json:"format"`
	IsAvailable  bool `json:"is_available"`
	WaitListSize int  `json:"wait_list_size"`
}

// LibbyHistoryItem represents a past loan or activity
type LibbyHistoryItem struct {
	ID           string    `json:"id"`
	MediaID      string    `json:"media_id"`
	Title        string    `json:"title"`
	Author       string    `json:"author"`
	Format       string    `json:"format"`
	CheckoutDate time.Time `json:"checkout_date"`
	ReturnDate   time.Time `json:"return_date"`
	DaysKept     int       `json:"days_kept"` // How long the user had it checked out
}

// TimelineEntry represents an entry from Libby's timeline export
type TimelineEntry struct {
	Title      string `json:"title"`
	TitleID    string `json:"title_id"`
	Author     string `json:"author"`
	Publisher  string `json:"publisher"`
	ISBN       string `json:"isbn"`
	Timestamp  int64  `json:"timestamp"` // Unix milliseconds
	Activity   string `json:"activity"`  // "Borrowed", "Returned", etc.
	Details    string `json:"details"`
	Library    string `json:"library"`
	LibraryKey string `json:"library_key"`
	Format     string `json:"format"` // audiobook, ebook, magazine
	CoverURL   string `json:"cover_url"`
	Color      string `json:"color"`
}

// TimelineResponse represents the JSON response from Libby timeline export
type TimelineResponse struct {
	Version  int             `json:"version"`
	Timeline []TimelineEntry `json:"timeline"`
}

// LibbyHold represents a library hold
type LibbyHold struct {
	ID                string    `json:"id"`
	MediaID           string    `json:"media_id"`
	Title             string    `json:"title"`
	Author            string    `json:"author"`
	CoverURL          string    `json:"cover_url"`
	Format            string    `json:"format"`
	HoldPlacedDate    time.Time `json:"hold_placed_date"`
	EstimatedWaitDays int       `json:"estimated_wait_days"`
	QueuePosition     int       `json:"queue_position"`
	IsReady           bool      `json:"is_ready"`
	AutoBorrow        bool      `json:"auto_borrow"`
}

// BookstoreEvent represents an event at a local bookstore
type BookstoreEvent struct {
	Store       string    `json:"store"`
	Title       string    `json:"title"`
	Author      string    `json:"author,omitempty"`
	Book        string    `json:"book,omitempty"`
	DateTime    time.Time `json:"datetime"`
	Description string    `json:"description"`
	URL         string    `json:"url"`
	EventType   string    `json:"event_type"` // signing, reading, discussion
}

// TrendingBook represents a trending book from community sources
type TrendingBook struct {
	Book       Book     `json:"book"`
	TrendScore float64  `json:"trend_score"`
	Hashtags   []string `json:"hashtags"`
	VideoCount int      `json:"video_count"`
	Source     string   `json:"source"` // booktok, booktube
}

// ReadingStats represents reading statistics
type ReadingStats struct {
	Year             int            `json:"year"`
	BooksRead        int            `json:"books_read"`
	PagesRead        int            `json:"pages_read"`
	AudioHours       float64        `json:"audio_hours"`
	AverageRating    float64        `json:"average_rating"`
	GenreBreakdown   map[string]int `json:"genre_breakdown"`
	MonthlyBreakdown map[string]int `json:"monthly_breakdown"`
	TopRated         []Book         `json:"top_rated"`
	LongestBook      *Book          `json:"longest_book,omitempty"`
	ShortestBook     *Book          `json:"shortest_book,omitempty"`
}

// UnifiedBookResult represents the result of find_book_everywhere
type UnifiedBookResult struct {
	Query               string               `json:"query"`
	Book                *Book                `json:"book,omitempty"`
	LibraryAvailability *LibraryAvailability `json:"library_availability,omitempty"`
	LocalStores         []LocalStoreAvail    `json:"local_stores,omitempty"`
	CommunityInfo       *CommunityInfo       `json:"community_info,omitempty"`
	Recommendation      string               `json:"recommendation"`
}

// LocalStoreAvail represents availability at a local bookstore
type LocalStoreAvail struct {
	StoreName string `json:"store_name"`
	StoreID   string `json:"store_id"`
	InStock   bool   `json:"in_stock"`
	Price     string `json:"price,omitempty"`
	URL       string `json:"url,omitempty"`
}

// CommunityInfo represents community sentiment about a book
type CommunityInfo struct {
	BookTokMentions   int      `json:"booktok_mentions"`
	BookTubeMentions  int      `json:"booktube_mentions"`
	TrendingHashtags  []string `json:"trending_hashtags"`
	AverageSentiment  float64  `json:"average_sentiment"` // -1 to 1
	CommonDescriptors []string `json:"common_descriptors"`
}
