package openlibrary

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/user/booklife-mcp/internal/models"
	"golang.org/x/time/rate"
)

// Client is the Open Library API client
type Client struct {
	httpClient     *http.Client
	endpoint       string
	coversEndpoint string
	limiter        *rate.Limiter
}

// NewClient creates a new Open Library client
func NewClient(endpoint, coversEndpoint string) *Client {
	if endpoint == "" {
		endpoint = "https://openlibrary.org"
	}
	if coversEndpoint == "" {
		coversEndpoint = "https://covers.openlibrary.org"
	}

	return &Client{
		httpClient:     &http.Client{Timeout: 10 * time.Second},
		endpoint:       endpoint,
		coversEndpoint: coversEndpoint,
		limiter:        rate.NewLimiter(rate.Every(100*time.Millisecond), 1), // 10 req/sec
	}
}

// GetByISBN retrieves a book by ISBN
func (c *Client) GetByISBN(ctx context.Context, isbn string) (*models.Book, error) {
	// Rate limit
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	// Clean ISBN
	isbn = strings.ReplaceAll(isbn, "-", "")

	// Use the books API
	endpoint := fmt.Sprintf("%s/api/books?bibkeys=ISBN:%s&format=json&jscmd=data", c.endpoint, isbn)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]struct {
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
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	key := fmt.Sprintf("ISBN:%s", isbn)
	data, ok := result[key]
	if !ok {
		return nil, fmt.Errorf("book not found")
	}

	book := &models.Book{
		Title:         data.Title,
		Subtitle:      data.Subtitle,
		PublishedDate: data.PublishDate,
		PageCount:     data.NumberOfPages,
	}

	if len(data.Identifiers.ISBN10) > 0 {
		book.ISBN10 = data.Identifiers.ISBN10[0]
	}
	if len(data.Identifiers.ISBN13) > 0 {
		book.ISBN13 = data.Identifiers.ISBN13[0]
	}
	if len(data.Identifiers.OpenLibrary) > 0 {
		book.OpenLibID = data.Identifiers.OpenLibrary[0]
	}

	if len(data.Publishers) > 0 {
		book.Publisher = data.Publishers[0].Name
	}

	for _, author := range data.Authors {
		book.Authors = append(book.Authors, models.Contributor{
			Name: author.Name,
			Role: "author",
		})
	}

	for _, subject := range data.Subjects {
		book.Subjects = append(book.Subjects, subject.Name)
	}

	if data.Cover.Large != "" {
		book.CoverURL = data.Cover.Large
	} else if data.Cover.Medium != "" {
		book.CoverURL = data.Cover.Medium
	}

	return book, nil
}

// Search searches for books with pagination support
func (c *Client) Search(ctx context.Context, query string, offset, limit int) ([]models.Book, int, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, 0, err
	}

	params := url.Values{}
	params.Set("q", query)
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("offset", fmt.Sprintf("%d", offset))

	endpoint := fmt.Sprintf("%s/search.json?%s", c.endpoint, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, 0, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	var result struct {
		NumFound int `json:"numFound"`
		Start    int `json:"start"`
		Docs     []struct {
			Key           string   `json:"key"`
			Title         string   `json:"title"`
			AuthorName    []string `json:"author_name"`
			ISBN          []string `json:"isbn"`
			PublishYear   []int    `json:"publish_year"`
			NumberOfPages int      `json:"number_of_pages_median"`
			Subject       []string `json:"subject"`
			CoverI        int      `json:"cover_i"`
		} `json:"docs"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, 0, fmt.Errorf("decoding search results: %w", err)
	}

	var books []models.Book
	for _, doc := range result.Docs {
		book := models.Book{
			OpenLibID: doc.Key,
			Title:     doc.Title,
			PageCount: doc.NumberOfPages,
			Subjects:  doc.Subject,
		}

		for _, author := range doc.AuthorName {
			book.Authors = append(book.Authors, models.Contributor{
				Name: author,
				Role: "author",
			})
		}

		// Find ISBN-13 or ISBN-10
		for _, isbn := range doc.ISBN {
			if len(isbn) == 13 {
				book.ISBN13 = isbn
				break
			} else if len(isbn) == 10 && book.ISBN10 == "" {
				book.ISBN10 = isbn
			}
		}

		if len(doc.PublishYear) > 0 {
			book.PublishedDate = fmt.Sprintf("%d", doc.PublishYear[0])
		}

		if doc.CoverI > 0 {
			book.CoverURL = fmt.Sprintf("%s/b/id/%d-L.jpg", c.coversEndpoint, doc.CoverI)
		}

		books = append(books, book)
	}

	return books, result.NumFound, nil
}

// GetCoverURL returns a cover URL for a book
func (c *Client) GetCoverURL(isbn string, size string) string {
	if size == "" {
		size = "L" // Large
	}
	isbn = strings.ReplaceAll(isbn, "-", "")
	return fmt.Sprintf("%s/b/isbn/%s-%s.jpg", c.coversEndpoint, isbn, size)
}

// GetDescription retrieves description from works API
func (c *Client) GetDescription(ctx context.Context, workID string) (string, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return "", err
	}

	endpoint := fmt.Sprintf("%s%s.json", c.endpoint, workID)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(req)
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
