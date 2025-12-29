package history

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/user/booklife-mcp/internal/models"
)

// Importer handles importing timeline data from Libby exports
type Importer struct {
	store *Store
}

// NewImporter creates a new timeline importer
func NewImporter(store *Store) *Importer {
	return &Importer{store: store}
}

// FetchTimeline fetches timeline data from a Libby share URL
func (im *Importer) FetchTimeline(url string) (*models.TimelineResponse, error) {
	client := &http.Client{Timeout: 60 * time.Second}

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetching timeline: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	// Use ParseTimeline which handles nested objects correctly
	return ParseTimeline(data)
}

// ImportTimeline fetches and imports timeline data from a URL
func (im *Importer) ImportTimeline(url string) (int, error) {
	// Fetch timeline
	timeline, err := im.FetchTimeline(url)
	if err != nil {
		return 0, err
	}

	// Import to store
	count, err := im.store.ImportTimeline(timeline)
	if err != nil {
		return 0, fmt.Errorf("importing to store: %w", err)
	}

	return count, nil
}

// ParseTimeline parses timeline JSON data
func ParseTimeline(data []byte) (*models.TimelineResponse, error) {
	var raw struct {
		Version  int                      `json:"version"`
		Timeline []map[string]interface{} `json:"timeline"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}

	entries := make([]models.TimelineEntry, 0, len(raw.Timeline))
	for _, item := range raw.Timeline {
		entry := models.TimelineEntry{
			Activity:  "Borrowed",
			Format:    "ebook",
			Timestamp: time.Now().UnixMilli(),
		}

		// Parse title
		if title, ok := item["title"].(map[string]interface{}); ok {
			if text, ok := title["text"].(string); ok {
				entry.Title = text
			}
			if titleID, ok := title["titleId"].(string); ok {
				entry.TitleID = titleID
			}
		}

		// Simple fields
		if v, ok := item["author"].(string); ok {
			entry.Author = v
		}
		if v, ok := item["publisher"].(string); ok {
			entry.Publisher = v
		}
		if v, ok := item["isbn"].(string); ok {
			entry.ISBN = v
		}
		if v, ok := item["activity"].(string); ok {
			entry.Activity = v
		}
		if v, ok := item["details"].(string); ok {
			entry.Details = v
		}
		if v, ok := item["color"].(string); ok {
			entry.Color = v
		}

		// Timestamp
		if v, ok := item["timestamp"].(float64); ok {
			entry.Timestamp = int64(v)
		}

		// Library
		if lib, ok := item["library"].(map[string]interface{}); ok {
			if text, ok := lib["text"].(string); ok {
				entry.Library = text
			}
			if key, ok := lib["key"].(string); ok {
				entry.LibraryKey = key
			}
		}

		// Cover
		if cover, ok := item["cover"].(map[string]interface{}); ok {
			if format, ok := cover["format"].(string); ok {
				entry.Format = format
			}
			if url, ok := cover["url"].(string); ok {
				entry.CoverURL = url
			}
		}

		entries = append(entries, entry)
	}

	return &models.TimelineResponse{
		Version:  raw.Version,
		Timeline: entries,
	}, nil
}

// ImportTimelineBytes imports timeline from raw JSON bytes
func (im *Importer) ImportTimelineBytes(data []byte) (int, error) {
	timeline, err := ParseTimeline(data)
	if err != nil {
		return 0, err
	}

	return im.store.ImportTimeline(timeline)
}

// ExportForImport exports timeline entries as Goodreads-compatible CSV
// Returns file path and count of exported entries
func (im *Importer) ExportForImport(entries []models.TimelineEntry, activity string) (string, int, error) {
	// Determine output directory and file path
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", 0, fmt.Errorf("getting home directory: %w", err)
	}

	booksDir := filepath.Join(homeDir, "books")
	if err := os.MkdirAll(booksDir, 0755); err != nil {
		return "", 0, fmt.Errorf("creating books directory: %w", err)
	}

	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("libby_export_%s_%s.csv", activity, timestamp)
	filePath := filepath.Join(booksDir, filename)

	// Create CSV file
	file, err := os.Create(filePath)
	if err != nil {
		return "", 0, fmt.Errorf("creating CSV file: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	header := []string{
		"Book Id", "Title", "Author", "Author l-f", "Additional Authors",
		"ISBN", "ISBN13", "My Rating", "Average Rating", "Publisher",
		"Binding", "Number of Pages", "Year Published", "Original Publication Year",
		"Date Read", "Date Added", "Bookshelves", "Bookshelves with positions",
		"Exclusive Shelf", "My Review", "Spoiler", "Private Notes",
		"Read Count", "Owned Copies",
	}
	if err := writer.Write(header); err != nil {
		return "", 0, fmt.Errorf("writing header: %w", err)
	}

	// Write data rows
	count := 0
	for _, entry := range entries {
		// Format: remove "The Great Courses" suffix if present
		author := entry.Author
		if strings.Contains(author, "The Great Courses") {
			parts := strings.Split(author, ",")
			author = strings.TrimSpace(parts[0])
		}

		// Format binding
		bindingMap := map[string]string{
			"audiobook": "Audible Audio",
			"ebook":     "Kindle Edition",
			"book":      "Hardcover",
			"magazine":  "Paperback",
		}
		binding := bindingMap[entry.Format]
		if binding == "" {
			binding = entry.Format
		}

		// Format dates
		dateRead := formatTimestamp(entry.Timestamp)
		dateAdded := formatTimestamp(entry.Timestamp)

		// ISBN handling
		bookID := entry.ISBN
		if bookID == "" {
			bookID = entry.TitleID
		}

		var isbnField, isbn13Field string
		if entry.ISBN != "" {
			if len(entry.ISBN) == 10 {
				isbnField = fmt.Sprintf(`"%s"`, entry.ISBN)
			} else if len(entry.ISBN) == 13 {
				isbn13Field = fmt.Sprintf(`"%s"`, entry.ISBN)
			}
		}

		// Escape commas and quotes in title
		title := entry.Title
		if strings.Contains(title, ",") || strings.Contains(title, `"`) {
			title = fmt.Sprintf(`"%s"`, strings.ReplaceAll(title, `"`, `""`))
		}

		// Format author as "Last, First"
		authorLF := formatAuthorLF(author)

		row := []string{
			bookID,          // Book Id
			title,           // Title
			author,          // Author
			authorLF,        // Author l-f
			"",              // Additional Authors
			isbnField,       // ISBN
			isbn13Field,     // ISBN13
			"",              // My Rating
			"",              // Average Rating
			entry.Publisher, // Publisher
			binding,         // Binding
			"",              // Number of Pages
			"",              // Year Published
			"",              // Original Publication Year
			dateRead,        // Date Read
			dateAdded,       // Date Added
			"read",          // Bookshelves
			"",              // Bookshelves with positions
			"read",          // Exclusive Shelf
			"",              // My Review
			"",              // Spoiler
			"",              // Private Notes
			"1",             // Read Count
			"0",             // Owned Copies
		}

		if err := writer.Write(row); err != nil {
			return "", 0, fmt.Errorf("writing row: %w", err)
		}
		count++
	}

	return filePath, count, nil
}

// formatAuthorLF converts "First Last" to "Last, First"
func formatAuthorLF(author string) string {
	author = strings.TrimSpace(author)
	if author == "" {
		return ""
	}

	// If already in "Last, First" format (contains comma), return as-is
	if strings.Contains(author, ",") {
		return fmt.Sprintf(`"%s"`, author)
	}

	// Otherwise convert "First Last" to "Last, First"
	parts := strings.Fields(author)
	if len(parts) >= 2 {
		lastName := parts[len(parts)-1]
		firstNames := strings.Join(parts[:len(parts)-1], " ")
		return fmt.Sprintf(`"%s, %s"`, lastName, firstNames)
	}

	return fmt.Sprintf(`"%s"`, author)
}

// formatTimestamp converts Unix timestamp to YYYY/MM/DD format
func formatTimestamp(ts int64) string {
	if ts == 0 {
		return ""
	}

	// Convert milliseconds to seconds
	t := time.Unix(ts/1000, 0)
	return t.Format("2006/01/02")
}
