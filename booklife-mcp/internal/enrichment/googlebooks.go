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

// GoogleBooksEnricher fetches enrichment data from Google Books API
type GoogleBooksEnricher struct {
	client   *http.Client
	apiKey   string
	endpoint string
	limiter  *rate.Limiter
}

// NewGoogleBooksEnricher creates a new Google Books enricher
func NewGoogleBooksEnricher(apiKey string) *GoogleBooksEnricher {
	endpoint := "https://www.googleapis.com/books/v1"

	return &GoogleBooksEnricher{
		client:   &http.Client{Timeout: 10 * time.Second},
		apiKey:   apiKey,
		endpoint: endpoint,
		limiter:  rate.NewLimiter(rate.Every(100*time.Millisecond), 1), // 10 req/sec
	}
}

// GoogleBooksData represents enriched data from Google Books
type GoogleBooksData struct {
	GoogleBooksID  string
	Title          string
	Authors        []string
	Description    string
	Categories     []string // BISAC categories
	Themes         []string // Extracted themes
	SeriesName     string
	SeriesPosition float64
	PublishDate    string
	Publisher      string
	PageCount      int
	CoverURL       string
	ISBN10         string
	ISBN13         string
}

// VolumeInfo represents the common structure for Google Books volume info
type VolumeInfo struct {
	Title               string     `json:"title"`
	Subtitle            string     `json:"subtitle"`
	Authors             []string   `json:"authors"`
	Publisher           string     `json:"publisher"`
	PublishedDate       string     `json:"publishedDate"`
	Description         string     `json:"description"`
	PageCount           int        `json:"pageCount"`
	Categories          []string   `json:"categories"`
	ImageLinks          ImageLinks `json:"imageLinks"`
	IndustryIdentifiers []struct {
		Type       string `json:"type"`
		Identifier string `json:"identifier"`
	} `json:"industryIdentifiers"`
}

// ImageLinks represents Google Books image links
type ImageLinks struct {
	SmallThumbnail string `json:"smallThumbnail"`
	Thumbnail      string `json:"thumbnail"`
	Small          string `json:"small"`
	Medium         string `json:"medium"`
	Large          string `json:"large"`
}

// GetByISBN retrieves enrichment data by ISBN
func (g *GoogleBooksEnricher) GetByISBN(ctx context.Context, isbn string) (*GoogleBooksData, error) {
	if err := g.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	isbn = strings.ReplaceAll(isbn, "-", "")
	params := url.Values{}
	params.Set("q", fmt.Sprintf("ISBN:%s", isbn))

	if g.apiKey != "" {
		params.Set("key", g.apiKey)
	}

	endpoint := fmt.Sprintf("%s/volumes?%s", g.endpoint, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		TotalItems int `json:"totalItems"`
		Items      []struct {
			ID         string     `json:"id"`
			VolumeInfo VolumeInfo `json:"volumeInfo"`
		} `json:"items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if result.TotalItems == 0 || len(result.Items) == 0 {
		return nil, fmt.Errorf("book not found")
	}

	item := result.Items[0]
	return g.parseVolumeData(item.ID, item.VolumeInfo), nil
}

// SearchByTitleAuthor searches for enrichment data by title and author
func (g *GoogleBooksEnricher) SearchByTitleAuthor(ctx context.Context, title, author string) (*GoogleBooksData, error) {
	if err := g.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	query := fmt.Sprintf("%s %s", title, author)
	params := url.Values{}
	params.Set("q", query)
	params.Set("maxResults", "1")

	if g.apiKey != "" {
		params.Set("key", g.apiKey)
	}

	endpoint := fmt.Sprintf("%s/volumes?%s", g.endpoint, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		TotalItems int `json:"totalItems"`
		Items      []struct {
			ID         string     `json:"id"`
			VolumeInfo VolumeInfo `json:"volumeInfo"`
		} `json:"items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if result.TotalItems == 0 || len(result.Items) == 0 {
		return nil, fmt.Errorf("book not found")
	}

	item := result.Items[0]
	return g.parseVolumeData(item.ID, item.VolumeInfo), nil
}

// parseVolumeData parses volume info into GoogleBooksData
func (g *GoogleBooksEnricher) parseVolumeData(id string, volInfo VolumeInfo) *GoogleBooksData {
	data := &GoogleBooksData{
		GoogleBooksID: id,
		Title:         volInfo.Title,
		Authors:       volInfo.Authors,
		Publisher:     volInfo.Publisher,
		PublishDate:   volInfo.PublishedDate,
		PageCount:     volInfo.PageCount,
		Description:   volInfo.Description,
		Categories:    volInfo.Categories,
	}

	// Get best cover URL
	if volInfo.ImageLinks.Medium != "" {
		data.CoverURL = volInfo.ImageLinks.Medium
	} else if volInfo.ImageLinks.Large != "" {
		data.CoverURL = volInfo.ImageLinks.Large
	} else if volInfo.ImageLinks.Thumbnail != "" {
		data.CoverURL = volInfo.ImageLinks.Thumbnail
	}

	// Parse ISBNs
	for _, ident := range volInfo.IndustryIdentifiers {
		switch ident.Type {
		case "ISBN_10":
			data.ISBN10 = ident.Identifier
		case "ISBN_13":
			data.ISBN13 = ident.Identifier
		}
	}

	// Try to extract series info from title
	if volInfo.Subtitle != "" && strings.Contains(strings.ToLower(volInfo.Subtitle), "book") {
		// Might be a series
		// For now we just store the subtitle, series extraction is complex
	}

	return data
}
