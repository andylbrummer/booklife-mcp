package enrichment

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// OpenLibraryEnricher fetches enrichment data from Open Library
type OpenLibraryEnricher struct {
	client         *http.Client
	endpoint       string
	coversEndpoint string
	limiter        *rate.Limiter
}

// NewOpenLibraryEnricher creates a new Open Library enricher
func NewOpenLibraryEnricher(endpoint, coversEndpoint string) *OpenLibraryEnricher {
	if endpoint == "" {
		endpoint = "https://openlibrary.org"
	}
	if coversEndpoint == "" {
		coversEndpoint = "https://covers.openlibrary.org"
	}

	return &OpenLibraryEnricher{
		client:         &http.Client{Timeout: 10 * time.Second},
		endpoint:       endpoint,
		coversEndpoint: coversEndpoint,
		limiter:        rate.NewLimiter(rate.Every(100*time.Millisecond), 1), // 10 req/sec
	}
}

// OpenLibraryData represents enriched data from Open Library
type OpenLibraryData struct {
	OpenLibraryID  string
	Title          string
	Authors        []string
	Description    string
	Subjects       []string // LCSH subjects
	Themes         []string // Extracted themes
	SeriesName     string
	SeriesPosition float64
	SeriesTotal    int
	PublishDate    string
	Publisher      string
	PageCount      int
	CoverURL       string
	ISBN10         string
	ISBN13         string
}

// GetByISBN retrieves enrichment data by ISBN
func (o *OpenLibraryEnricher) GetByISBN(ctx context.Context, isbn string) (*OpenLibraryData, error) {
	if err := o.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	isbn = strings.ReplaceAll(isbn, "-", "")
	endpoint := fmt.Sprintf("%s/api/books?bibkeys=ISBN:%s&format=json&jscmd=data", o.endpoint, isbn)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	key := fmt.Sprintf("ISBN:%s", isbn)
	data, ok := result[key]
	if !ok {
		return nil, fmt.Errorf("book not found")
	}

	return o.parseBookData(data)
}

// SearchByTitleAuthor searches for enrichment data by title and author
func (o *OpenLibraryEnricher) SearchByTitleAuthor(ctx context.Context, title, author string) (*OpenLibraryData, error) {
	if err := o.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	query := fmt.Sprintf("%s %s", title, author)
	params := url.Values{}
	params.Set("q", query)
	params.Set("limit", "1")
	params.Set("fields", "key,title,author_name,subject,first_publish_year,number_of_pages,series,cover_i,isbn")

	endpoint := fmt.Sprintf("%s/search.json?%s", o.endpoint, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		NumFound int `json:"numFound"`
		Docs     []struct {
			Key              string   `json:"key"`
			Title            string   `json:"title"`
			AuthorName       []string `json:"author_name"`
			Subject          []string `json:"subject"`
			FirstPublishYear int      `json:"first_publish_year"`
			NumberOfPages    int      `json:"number_of_pages"`
			Series           []string `json:"series"`
			CoverI           int      `json:"cover_i"`
			ISBN             []string `json:"isbn"`
		} `json:"docs"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding search results: %w", err)
	}

	if len(result.Docs) == 0 {
		return nil, fmt.Errorf("book not found")
	}

	doc := result.Docs[0]
	data := &OpenLibraryData{
		OpenLibraryID: doc.Key,
		Title:         doc.Title,
		Authors:       doc.AuthorName,
		Subjects:      doc.Subject,
		PublishDate:   fmt.Sprintf("%d", doc.FirstPublishYear),
		PageCount:     doc.NumberOfPages,
	}

	// Parse ISBN
	for _, isbn := range doc.ISBN {
		if len(isbn) == 13 {
			data.ISBN13 = isbn
		} else if len(isbn) == 10 {
			data.ISBN10 = isbn
		}
	}

	// Parse series
	if len(doc.Series) > 0 {
		// Open Library series format varies, try to extract name and position
		seriesStr := doc.Series[0]
		data.SeriesName, data.SeriesPosition = parseSeriesString(seriesStr)
	}

	// Get cover URL
	if doc.CoverI > 0 {
		data.CoverURL = fmt.Sprintf("%s/b/id/%d-L.jpg", o.coversEndpoint, doc.CoverI)
	}

	// Fetch description from works API
	if desc, err := o.GetDescription(ctx, doc.Key); err == nil && desc != "" {
		data.Description = desc
	}

	return data, nil
}

// GetDescription retrieves description from works API
func (o *OpenLibraryEnricher) GetDescription(ctx context.Context, workID string) (string, error) {
	if err := o.limiter.Wait(ctx); err != nil {
		return "", err
	}

	endpoint := fmt.Sprintf("%s%s.json", o.endpoint, workID)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return "", err
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Description interface{} `json:"description"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	switch d := result.Description.(type) {
	case string:
		return d, nil
	case map[string]interface{}:
		if value, ok := d["value"].(string); ok {
			return value, nil
		}
	}

	return "", nil
}

// parseBookData parses book data from JSON
func (o *OpenLibraryEnricher) parseBookData(data json.RawMessage) (*OpenLibraryData, error) {
	var raw struct {
		Title      string `json:"title"`
		Subtitle   string `json:"subtitle"`
		Publishers []struct {
			Name string `json:"name"`
		} `json:"publishers"`
		PublishDate   string `json:"publish_date"`
		NumberOfPages int    `json:"number_of_pages"`
		Subjects      []struct {
			Name string `json:"name"`
		} `json:"subjects"`
		Authors []struct {
			Name string `json:"name"`
		} `json:"authors"`
		Cover struct {
			Small  string `json:"small"`
			Medium string `json:"medium"`
			Large  string `json:"large"`
		} `json:"cover"`
		Identifiers struct {
			ISBN10      []string `json:"isbn_10"`
			ISBN13      []string `json:"isbn_13"`
			OpenLibrary []string `json:"openlibrary"`
		} `json:"identifiers"`
		Series []struct {
			Name string `json:"name"`
		} `json:"series"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing book data: %w", err)
	}

	result := &OpenLibraryData{
		Title:       raw.Title,
		PublishDate: raw.PublishDate,
		PageCount:   raw.NumberOfPages,
	}

	if len(raw.Identifiers.ISBN10) > 0 {
		result.ISBN10 = raw.Identifiers.ISBN10[0]
	}
	if len(raw.Identifiers.ISBN13) > 0 {
		result.ISBN13 = raw.Identifiers.ISBN13[0]
	}
	if len(raw.Identifiers.OpenLibrary) > 0 {
		result.OpenLibraryID = raw.Identifiers.OpenLibrary[0]
	}

	if len(raw.Publishers) > 0 {
		result.Publisher = raw.Publishers[0].Name
	}

	for _, author := range raw.Authors {
		result.Authors = append(result.Authors, author.Name)
	}

	for _, subject := range raw.Subjects {
		result.Subjects = append(result.Subjects, subject.Name)
	}

	if raw.Cover.Large != "" {
		result.CoverURL = raw.Cover.Large
	} else if raw.Cover.Medium != "" {
		result.CoverURL = raw.Cover.Medium
	}

	// Parse series
	if len(raw.Series) > 0 {
		result.SeriesName = raw.Series[0].Name
	}

	return result, nil
}

// parseSeriesString attempts to parse a series string to extract name and position
// Examples: "The Stormlight Archive #1", "Harry Potter, Book 2", "Mistborn #3"
func parseSeriesString(s string) (name string, position float64) {
	s = strings.TrimSpace(s)

	// Try pattern with # first
	if idx := strings.Index(s, "#"); idx > 0 {
		name = strings.TrimSpace(s[:idx])
		posStr := strings.TrimSpace(s[idx+1:])
		if posStr != "" {
			fmt.Sscanf(posStr, "%f", &position)
		}
		return
	}

	// Try "Book N" pattern
	lower := strings.ToLower(s)
	if idx := strings.Index(lower, "book"); idx > 0 {
		name = strings.TrimSpace(s[:idx])
		posStr := strings.TrimSpace(s[idx+4:])
		if posStr != "" {
			fmt.Sscanf(posStr, "%f", &position)
		}
		return
	}

	// No pattern found, return as-is
	name = s
	return
}
