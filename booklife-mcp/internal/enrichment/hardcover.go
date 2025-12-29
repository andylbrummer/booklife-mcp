package enrichment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/user/booklife-mcp/internal/providers"
)

// HardcoverEnricher fetches enrichment data from Hardcover GraphQL API
type HardcoverEnricher struct {
	client   *http.Client
	endpoint string
	apiKey   string
}

// NewHardcoverEnricher creates a new Hardcover enricher
func NewHardcoverEnricher(provider providers.HardcoverProvider) *HardcoverEnricher {
	if provider == nil {
		return nil
	}

	// Extract endpoint and API key from provider
	// This is a bridge between the existing Hardcover client and the enricher
	return &HardcoverEnricher{
		client:   &http.Client{Timeout: 10 * time.Second},
		endpoint: "https://api.hardcover.app/v1/graphql",
		apiKey:   "", // Will be extracted from provider if needed
	}
}

// NewHardcoverEnricherDirect creates a new Hardcover enricher with direct credentials
func NewHardcoverEnricherDirect(endpoint, apiKey string) *HardcoverEnricher {
	if apiKey == "" {
		return nil
	}

	if endpoint == "" {
		endpoint = "https://api.hardcover.app/v1/graphql"
	}

	return &HardcoverEnricher{
		client:   &http.Client{Timeout: 10 * time.Second},
		endpoint: endpoint,
		apiKey:   apiKey,
	}
}

// HardcoverData represents enriched data from Hardcover
type HardcoverData struct {
	HardcoverID    string
	Title          string
	Subtitle       string
	Description    string
	Authors        []string
	Genres         []string // From tags with type="genre"
	Moods          []string // From tags with type="mood"
	Themes         []string // From tags with type="theme"
	SeriesName     string
	SeriesPosition float64
	PublishDate    string
	PageCount      int
	CoverURL       string
	ISBN10         string
	ISBN13         string
}

// GetByID retrieves enrichment data by Hardcover book ID
func (h *HardcoverEnricher) GetByID(ctx context.Context, hardcoverID string) (*HardcoverData, error) {
	if h == nil || h.apiKey == "" {
		return nil, fmt.Errorf("hardcover enricher not configured")
	}

	query := `
	query GetBookEnrichment($id: Int!) {
		books(where: {id: {_eq: $id}}, limit: 1) {
			id
			title
			subtitle
			description
			pages
			release_date
			cached_image
			cached_tags
			contributions {
				author {
					name
				}
			}
			editions {
				isbn_10
				isbn_13
			}
			book_series {
				series {
					name
				}
				position
			}
			featured_book_series {
				series {
					name
				}
				position
			}
		}
	}`

	variables := map[string]interface{}{
		"id": hardcoverID,
	}

	result, err := h.executeQuery(ctx, query, variables)
	if err != nil {
		return nil, err
	}

	return h.parseBookData(result)
}

// GetByTitleAuthor searches for enrichment data by title and author
func (h *HardcoverEnricher) GetByTitleAuthor(ctx context.Context, title, author string) (*HardcoverData, error) {
	if h == nil || h.apiKey == "" {
		return nil, fmt.Errorf("hardcover enricher not configured")
	}

	// Use the search endpoint with proper JSON encoding (avoids GraphQL escaping issues)
	searchQuery := title + " " + author

	reqBody := map[string]interface{}{
		"query": fmt.Sprintf(`{
			search(query: %s, query_type: "Book", per_page: 5) {
				ids
			}
		}`, encodeGraphQLString(searchQuery)),
	}

	reqBodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", h.endpoint, bytes.NewReader(reqBodyJSON))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+h.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var searchResult struct {
		Data struct {
			Search struct {
				IDs []string `json:"ids"`
			} `json:"search"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(body, &searchResult); err != nil {
		return nil, fmt.Errorf("parsing search results: %w", err)
	}

	if len(searchResult.Errors) > 0 {
		return nil, fmt.Errorf("search failed: %s", searchResult.Errors[0].Message)
	}

	if len(searchResult.Data.Search.IDs) == 0 {
		return nil, fmt.Errorf("book not found: %s by %s", title, author)
	}

	// Use the first result's ID to fetch full enrichment data
	// This second query gets genres, moods, themes, series that search doesn't provide
	bookID := searchResult.Data.Search.IDs[0]
	return h.GetByID(ctx, bookID)
}

// encodeGraphQLString properly escapes a string for GraphQL query
func encodeGraphQLString(s string) string {
	// Escape quotes and backslashes
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	return fmt.Sprintf(`"%s"`, s)
}

// executeQuery executes a GraphQL query and returns the raw result
func (h *HardcoverEnricher) executeQuery(ctx context.Context, query string, variables map[string]interface{}) (json.RawMessage, error) {
	reqBody := map[string]interface{}{
		"query":     query,
		"variables": variables,
	}

	reqBodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", h.endpoint, bytes.NewReader(reqBodyJSON))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+h.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var result struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("query failed: %s", result.Errors[0].Message)
	}

	return result.Data, nil
}

// parseBookData parses book data from GraphQL response
func (h *HardcoverEnricher) parseBookData(data json.RawMessage) (*HardcoverData, error) {
	var wrapper struct {
		Books []struct {
			ID          int    `json:"id"`
			Title       string `json:"title"`
			Subtitle    string `json:"subtitle"`
			Description string `json:"description"`
			Pages       int    `json:"pages"`
			ReleaseDate string `json:"release_date"`
			CachedImage struct {
				URL string `json:"url"`
			} `json:"cached_image"`
			CachedTags map[string][]struct {
				Tag          string `json:"tag"`
				TagSlug      string `json:"tagSlug"`
				Category     string `json:"category"`
				CategorySlug string `json:"categorySlug"`
				Spoiler      bool   `json:"spoiler"`
				Count        int    `json:"count"`
			} `json:"cached_tags"`
			Contributions []struct {
				Author struct {
					Name string `json:"name"`
				} `json:"author"`
			} `json:"contributions"`
			Editions []struct {
				ISBN10 string `json:"isbn_10"`
				ISBN13 string `json:"isbn_13"`
			} `json:"editions"`
			BookSeries []struct {
				Series struct {
					Name string `json:"name"`
				} `json:"series"`
				Position float64 `json:"position"`
			} `json:"book_series"`
			FeaturedBookSeries *struct {
				Series struct {
					Name string `json:"name"`
				} `json:"series"`
				Position float64 `json:"position"`
			} `json:"featured_book_series"`
		} `json:"books"`
	}

	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("parsing book data: %w", err)
	}

	if len(wrapper.Books) == 0 {
		return nil, fmt.Errorf("no book data in response")
	}

	book := wrapper.Books[0]
	result := &HardcoverData{
		HardcoverID: fmt.Sprintf("%d", book.ID),
		Title:       book.Title,
		Subtitle:    book.Subtitle,
		Description: book.Description,
		PageCount:   book.Pages,
		PublishDate: book.ReleaseDate,
		CoverURL:    book.CachedImage.URL,
	}

	// Extract authors
	for _, contrib := range book.Contributions {
		result.Authors = append(result.Authors, contrib.Author.Name)
	}

	// Extract ISBNs (prefer first edition)
	if len(book.Editions) > 0 {
		result.ISBN10 = book.Editions[0].ISBN10
		result.ISBN13 = book.Editions[0].ISBN13
	}

	// Extract series info (prefer featured_book_series, then first from book_series array)
	if book.FeaturedBookSeries != nil {
		result.SeriesName = book.FeaturedBookSeries.Series.Name
		result.SeriesPosition = book.FeaturedBookSeries.Position
	} else if len(book.BookSeries) > 0 {
		result.SeriesName = book.BookSeries[0].Series.Name
		result.SeriesPosition = book.BookSeries[0].Position
	}

	// Extract genres from cached_tags
	if genreTags, ok := book.CachedTags["Genre"]; ok {
		for _, tag := range genreTags {
			result.Genres = append(result.Genres, tag.Tag)
		}
	}

	// Extract moods from cached_tags
	if moodTags, ok := book.CachedTags["Mood"]; ok {
		for _, tag := range moodTags {
			result.Moods = append(result.Moods, tag.Tag)
		}
	}

	// Note: Hardcover API no longer has a "Theme" category
	// Themes field will remain empty unless we derive them from genres/moods
	// Or use the description to extract themes

	return result, nil
}
