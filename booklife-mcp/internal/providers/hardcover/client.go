package hardcover

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/hasura/go-graphql-client"
	"github.com/user/booklife-mcp/internal/debug"
	"github.com/user/booklife-mcp/internal/models"
)

// Client is the Hardcover GraphQL API client
type Client struct {
	client   *graphql.Client
	endpoint string
	apiKey   string
}

// NewClient creates a new Hardcover API client
func NewClient(endpoint, apiKey string) (*Client, error) {
	if endpoint == "" {
		endpoint = "https://api.hardcover.app/v1/graphql"
	}

	httpClient := &http.Client{
		Transport: &authTransport{
			apiKey: apiKey,
			base:   http.DefaultTransport,
		},
	}

	client := graphql.NewClient(endpoint, httpClient)

	return &Client{
		client:   client,
		endpoint: endpoint,
		apiKey:   apiKey,
	}, nil
}

// authTransport adds authorization header to requests and logs responses
type authTransport struct {
	apiKey string
	base   http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.base.RoundTrip(req)
	if err == nil && resp != nil && resp.ContentLength > 0 && resp.ContentLength < 1<<20 {
		// Only capture response body for debugging if content length is known and reasonable
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr == nil && len(body) > 0 {
			// Debug logging only if BOOKLIFE_DEBUG=true
			debug.Log("hardcover", body)
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
	}
	return resp, err
}

// SearchBooks searches for books by query with pagination support
func (c *Client) SearchBooks(ctx context.Context, query string, offset, limit int) ([]models.Book, int, error) {
	// Make a direct HTTP POST request to get the raw JSONB results
	queryStr := fmt.Sprintf(`{
		search(query: %q, query_type: "Book", per_page: %d) {
			ids
			results
		}
	}`, query, offset+limit) // Request enough to get offset+limit items

	reqBody := map[string]string{
		"query": queryStr,
	}

	reqBodyJSON, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("POST", c.endpoint, bytes.NewReader(reqBodyJSON))
	if err != nil {
		return nil, 0, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != 200 {
		baseErr := fmt.Sprintf("Hardcover API error (HTTP %d): %s", resp.StatusCode, string(respBody))

		switch resp.StatusCode {
		case 401:
			return nil, 0, fmt.Errorf("%s\n\n"+
				"Authentication failed. Your API key may be invalid or expired.\n\n"+
				"Fix:\n"+
				"1. Verify HARDCOVER_API_KEY environment variable is set\n"+
				"2. Get a new API key from: https://hardcover.app/settings/api\n"+
				"3. Update your config or environment variable", baseErr)
		case 429:
			return nil, 0, fmt.Errorf("%s\n\n"+
				"Rate limit exceeded.\n\n"+
				"Fix: Wait a few minutes before retrying", baseErr)
		case 500, 502, 503, 504:
			return nil, 0, fmt.Errorf("%s\n\n"+
				"Hardcover API is experiencing issues.\n\n"+
				"Fix: Retry in a few minutes or check https://hardcover.app", baseErr)
		default:
			return nil, 0, fmt.Errorf("%s", baseErr)
		}
	}

	var result struct {
		Data struct {
			Search struct {
				IDS     []string        `json:"ids"`
				Results json.RawMessage `json:"results"`
			} `json:"search"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, 0, fmt.Errorf("parsing response: %w", err)
	}

	if len(result.Errors) > 0 {
		return nil, 0, fmt.Errorf("API errors: %v", result.Errors)
	}

	totalCount := len(result.Data.Search.IDS)

	// Parse the JSONB results
	var searchResults struct {
		Hits []struct {
			Document struct {
				ID           string   `json:"id"`
				Title        string   `json:"title"`
				Subtitle     string   `json:"subtitle"`
				Description  string   `json:"description"`
				Pages        int      `json:"pages"`
				ReleaseDate  string   `json:"release_date"`
				Rating       float64  `json:"rating"`
				RatingsCount int      `json:"ratings_count"`
				AuthorNames  []string `json:"author_names"`
				ISBNs        []string `json:"isbns"`
				Image        struct {
					URL string `json:"url"`
				} `json:"image"`
			} `json:"document"`
		} `json:"hits"`
	}

	if err := json.Unmarshal(result.Data.Search.Results, &searchResults); err != nil {
		return nil, totalCount, fmt.Errorf("parsing search results: %w", err)
	}

	// Apply offset to get the correct page
	startIdx := offset
	if startIdx >= len(searchResults.Hits) {
		return []models.Book{}, totalCount, nil
	}
	if startIdx+limit > len(searchResults.Hits) {
		limit = len(searchResults.Hits) - startIdx
	}

	var books []models.Book
	for _, hit := range searchResults.Hits[startIdx : startIdx+limit] {
		doc := hit.Document
		book := models.Book{
			HardcoverID:     doc.ID,
			Title:           doc.Title,
			Subtitle:        doc.Subtitle,
			Description:     doc.Description,
			PageCount:       doc.Pages,
			PublishedDate:   doc.ReleaseDate,
			CoverURL:        doc.Image.URL,
			HardcoverRating: doc.Rating,
			HardcoverCount:  doc.RatingsCount,
		}

		// Extract ISBNs
		for _, isbn := range doc.ISBNs {
			if len(isbn) == 10 {
				book.ISBN10 = isbn
			} else if len(isbn) == 13 {
				book.ISBN13 = isbn
			}
		}

		// Extract authors
		for _, authorName := range doc.AuthorNames {
			book.Authors = append(book.Authors, models.Contributor{
				Name: authorName,
				Role: "author",
			})
		}

		books = append(books, book)
	}

	return books, totalCount, nil
}

// getBookByID retrieves a single book by string ID
func (c *Client) getBookByID(ctx context.Context, idStr string) (*models.Book, error) {
	var q struct {
		Books []struct {
			ID            int     `graphql:"id"`
			Title         string  `graphql:"title"`
			Subtitle      string  `graphql:"subtitle"`
			Description   string  `graphql:"description"`
			PageCount     int     `graphql:"pages"`
			PublishedDate string  `graphql:"release_date"`
			Rating        float64 `graphql:"rating"`
			RatingsCount  int     `graphql:"ratings_count"`
			// ISBNs might not be available in the books table
			// We'll get them from the search results or look them up separately
		} `graphql:"books(where: {id: {_eq: $id}}, limit: 1)"`
	}

	// Convert string ID to int
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid book ID: %w", err)
	}

	variables := map[string]interface{}{
		"id": id,
	}

	err = c.client.Query(ctx, &q, variables)
	if err != nil {
		return nil, fmt.Errorf("book query failed: %w", err)
	}

	if len(q.Books) == 0 {
		return nil, fmt.Errorf("book not found")
	}

	b := q.Books[0]
	book := &models.Book{
		HardcoverID:     idStr, // Keep string ID for consistency
		Title:           b.Title,
		Subtitle:        b.Subtitle,
		Description:     b.Description,
		PageCount:       b.PageCount,
		PublishedDate:   b.PublishedDate,
		HardcoverRating: b.Rating,
		HardcoverCount:  b.RatingsCount,
	}

	return book, nil
}

// GetBook retrieves a single book by ID (public API)
func (c *Client) GetBook(ctx context.Context, bookID string) (*models.Book, error) {
	return c.getBookByID(ctx, bookID)
}

// GetUserBooks retrieves the user's books by status with pagination support
func (c *Client) GetUserBooks(ctx context.Context, status string, offset, limit int) ([]models.Book, int, error) {
	// Map status to Hardcover status_id
	statusID := getStatusID(status)

	// First, get total count
	var countQ struct {
		Me []struct {
			UserBooksAggregate struct {
				Aggregate struct {
					Count int `graphql:"count"`
				} `graphql:"aggregate"`
			} `graphql:"user_books_aggregate(where: {status_id: {_eq: $status}})"`
		} `graphql:"me"`
	}

	countVars := map[string]interface{}{
		"status": statusID,
	}

	err := c.client.Query(ctx, &countQ, countVars)
	if err != nil {
		return nil, 0, fmt.Errorf("user books count query failed: %w", err)
	}

	totalCount := 0
	if len(countQ.Me) > 0 {
		totalCount = countQ.Me[0].UserBooksAggregate.Aggregate.Count
	}

	// Then get the paginated results
	var q struct {
		Me []struct {
			UserBooks []struct {
				ID        int     `graphql:"id"`
				Rating    float64 `graphql:"rating"`
				DateAdded string  `graphql:"date_added"`
				Book      struct {
					ID        int    `graphql:"id"`
					Title     string `graphql:"title"`
					PageCount int    `graphql:"pages"`
				} `graphql:"book"`
			} `graphql:"user_books(where: {status_id: {_eq: $status}}, limit: $limit, offset: $offset, order_by: {date_added: desc})"`
		} `graphql:"me"`
	}

	variables := map[string]interface{}{
		"status": statusID,
		"limit":  limit,
		"offset": offset,
	}

	err = c.client.Query(ctx, &q, variables)
	if err != nil {
		return nil, totalCount, fmt.Errorf("user books query failed: %w", err)
	}

	var books []models.Book
	if len(q.Me) > 0 {
		for _, ub := range q.Me[0].UserBooks {
			book := models.Book{
				ID:          fmt.Sprintf("%d", ub.ID),
				HardcoverID: fmt.Sprintf("%d", ub.Book.ID),
				Title:       ub.Book.Title,
				PageCount:   ub.Book.PageCount,
				UserStatus: &models.UserBookStatus{
					Status:   status,
					Rating:   ub.Rating,
					Progress: 0, // Progress field no longer available in API schema
				},
			}

			books = append(books, book)
		}
	}

	return books, totalCount, nil
}

// UpdateBookStatus updates a book's status in the user's library
// userBookID is the user_book.id (not book_id) from get_my_library
func (c *Client) UpdateBookStatus(ctx context.Context, userBookID, status string, progress int, rating float64) error {
	statusID := getStatusID(status)

	// Build the update object - only include fields that are being set
	updateObj := map[string]interface{}{
		"status_id": statusID,
	}

	// Hardcover API uses update_user_book(id, object) not the Hasura-style mutation
	query := `mutation UpdateUserBook($id: Int!, $object: UserBookUpdateInput!) {
		update_user_book(id: $id, object: $object) {
			id
		}
	}`

	// Parse userBookID as int
	var id int
	if _, err := fmt.Sscanf(userBookID, "%d", &id); err != nil {
		return fmt.Errorf("invalid user_book id: %s", userBookID)
	}

	variables := map[string]interface{}{
		"id":     id,
		"object": updateObj,
	}

	// Use raw GraphQL request since the struct-based client doesn't support this mutation style
	reqBody := map[string]interface{}{
		"query":     query,
		"variables": variables,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Data struct {
			UpdateUserBook struct {
				ID int `json:"id"`
			} `json:"update_user_book"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	if len(result.Errors) > 0 {
		return fmt.Errorf("update status mutation failed: %s", result.Errors[0].Message)
	}

	return nil
}

// normalizeTitle normalizes a title for comparison
func normalizeTitle(title string) string {
	result := ""
	for _, c := range title {
		if c >= 'a' && c <= 'z' {
			result += string(c)
		} else if c >= 'A' && c <= 'Z' {
			result += string(c + 32) // lowercase
		} else if c >= '0' && c <= '9' {
			result += string(c)
		}
	}
	return result
}

// authorsMatch checks if the author matches any of the book's authors
// Handles combined authors like "Peter Attia, MD, Bill Gifford" vs "Peter Attia"
// Also matches by last name to handle initials like "P. Attia" vs "Peter Attia"
func authorsMatch(author string, bookAuthors []string) bool {
	if len(bookAuthors) == 0 {
		return false
	}

	// Extract first author from combined author strings
	firstAuthor := author
	if idx := indexOfString(author, ","); idx > 0 {
		firstAuthor = author[:idx]
	}

	// Extract last name for loose matching
	firstLastName := getLastWord(firstAuthor)

	// Normalize for comparison
	authorNorm := normalizeString(author)
	firstAuthorNorm := normalizeString(firstAuthor)
	firstLastNorm := normalizeString(firstLastName)

	for _, ba := range bookAuthors {
		bookNorm := normalizeString(ba)
		bookLastName := getLastWord(ba)
		bookLastNorm := normalizeString(bookLastName)

		// Check multiple matching strategies:
		// 1. Exact match
		// 2. First author match
		// 3. Last name match (handles "Attia" vs "Peter Attia" or "P. Attia" vs "Peter Attia")
		// 4. Substring match
		if authorNorm == bookNorm || firstAuthorNorm == bookNorm ||
			firstLastNorm == bookLastNorm ||
			indexOfString(authorNorm, bookNorm) >= 0 || indexOfString(bookNorm, authorNorm) >= 0 ||
			indexOfString(firstAuthorNorm, bookNorm) >= 0 || indexOfString(bookNorm, firstAuthorNorm) >= 0 {
			return true
		}
	}
	return false
}

// getLastWord extracts the last word from a name (for last name matching)
func getLastWord(name string) string {
	name = strings.TrimSpace(name)
	words := strings.Fields(name)
	if len(words) == 0 {
		return ""
	}
	return words[len(words)-1]
}

// normalizeString normalizes a string for comparison
func normalizeString(s string) string {
	result := ""
	for _, c := range s {
		if c >= 'a' && c <= 'z' {
			result += string(c)
		} else if c >= 'A' && c <= 'Z' {
			result += string(c + 32)
		} else if c >= '0' && c <= '9' {
			result += string(c)
		}
	}
	return result
}

// AddBook adds a book to the user's library with strict title+author verification
func (c *Client) AddBook(ctx context.Context, isbn, title, author, status string) (string, error) {
	// If we have an ISBN, try exact ISBN match first
	if isbn != "" {
		books, _, err := c.SearchBooks(ctx, isbn, 0, 10)
		if err != nil {
			return "", err
		}
		// Look for exact ISBN match
		for _, book := range books {
			if book.ISBN10 == isbn || book.ISBN13 == isbn {
				return c.addBookToLibrary(ctx, book.HardcoverID, status)
			}
		}
		// If ISBN exists but no exact match, try title+author within the search results
		if len(books) > 0 && title != "" {
			titleNorm := normalizeTitle(title)
			for _, book := range books {
				bookTitleNorm := normalizeTitle(book.Title)
				if titleNorm == bookTitleNorm {
					authorNames := make([]string, len(book.Authors))
					for i, a := range book.Authors {
						authorNames[i] = a.Name
					}
					if authorsMatch(author, authorNames) {
						// Found exact title+author match (different ISBN/edition)
						return c.addBookToLibrary(ctx, book.HardcoverID, status)
					}
				}
			}
		}
	}

	// If no ISBN match or no ISBN, search by title+author with strict verification
	if title != "" {
		// First try: search by title + author
		query := title
		if author != "" {
			query += " " + author
		}
		books, _, err := c.SearchBooks(ctx, query, 0, 5)
		if err != nil {
			return "", err
		}

		// Try to find a match in the title+author search results
		if id := c.findBestMatchToAdd(title, author, books); id != "" {
			return c.addBookToLibrary(ctx, id, status)
		}

		// If title+author search didn't find anything, try title-only search
		// This handles cases where Libby has different author formatting
		if len(books) == 0 || !c.anyTitleMatches(title, books) {
			books, _, err = c.SearchBooks(ctx, title, 0, 10)
			if err != nil {
				return "", err
			}

			if id := c.findBestMatchToAdd(title, author, books); id != "" {
				return c.addBookToLibrary(ctx, id, status)
			}
		}

		// No exact match found - don't add fuzzy matches
		return "", fmt.Errorf("no exact title+author match found for: %s by %s", title, author)
	}

	return "", fmt.Errorf("book not found")
}

// findBestMatchToAdd searches through books for the best title+author match, returns Hardcover ID
func (c *Client) findBestMatchToAdd(title, author string, books []models.Book) string {
	titleNorm := normalizeTitle(title)

	// Look for exact title match first
	for _, book := range books {
		bookTitleNorm := normalizeTitle(book.Title)
		if titleNorm == bookTitleNorm {
			authorNames := make([]string, len(book.Authors))
			for i, a := range book.Authors {
				authorNames[i] = a.Name
			}
			if authorsMatch(author, authorNames) {
				// Found exact title+author match
				return book.HardcoverID
			}
		}
	}

	// Try matching main title (before subtitle) for cases like "Outlive" vs "Outlive: The Science..."
	for _, book := range books {
		if titleMainPartMatches(title, book.Title) {
			authorNames := make([]string, len(book.Authors))
			for i, a := range book.Authors {
				authorNames[i] = a.Name
			}
			if authorsMatch(author, authorNames) {
				// Found main title match
				return book.HardcoverID
			}
		}
	}

	return ""
}

// anyTitleMatches checks if any book in the list has a matching title
func (c *Client) anyTitleMatches(title string, books []models.Book) bool {
	for _, book := range books {
		if normalizeTitle(book.Title) == normalizeTitle(title) {
			return true
		}
		if titleMainPartMatches(title, book.Title) {
			return true
		}
	}
	return false
}

// titleMainPartMatches checks if the main title (before subtitle delimiters) matches
// Handles cases like "Outlive" matching "Outlive: The Science and Art of Longevity"
func titleMainPartMatches(libbyTitle, hardcoverTitle string) bool {
	// Get the main part of the Hardcover title (before subtitle delimiters)
	hardcoverMain := hardcoverTitle
	for _, delimiter := range []string{": ", " - ", " — ", ":  "} {
		if idx := indexOfString(hardcoverTitle, delimiter); idx > 0 {
			hardcoverMain = hardcoverTitle[:idx]
			break
		}
	}

	// Normalize both titles for comparison
	libbyNorm := normalizeTitle(libbyTitle)
	hardcoverNorm := normalizeTitle(hardcoverMain)

	// Check if they match
	return libbyNorm == hardcoverNorm
}

// indexOfString is a helper to find substring index (Go < 1.18 compatibility)
func indexOfString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// addBookToLibrary adds a book by ID to the user's library
func (c *Client) addBookToLibrary(ctx context.Context, bookID string, status string) (string, error) {
	statusID := getStatusID(status)
	bookIDInt, _ := strconv.Atoi(bookID)

	// Use raw HTTP request like UpdateBookStatus
	mutation := fmt.Sprintf(`mutation {
		insert_user_book(object: {book_id: %d, status_id: %d}) {
			id
		}
	}`, bookIDInt, statusID)

	reqBody := map[string]string{"query": mutation}
	reqBodyJSON, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewReader(reqBodyJSON))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("making request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		baseErr := fmt.Sprintf("Hardcover API error (HTTP %d): %s", resp.StatusCode, string(respBody))

		switch resp.StatusCode {
		case 401:
			return "", fmt.Errorf("%s\n\n"+
				"Authentication failed. Your API key may be invalid or expired.\n\n"+
				"Fix:\n"+
				"1. Verify HARDCOVER_API_KEY environment variable is set\n"+
				"2. Get a new API key from: https://hardcover.app/settings/api", baseErr)
		case 429:
			return "", fmt.Errorf("%s\n\n"+
				"Rate limit exceeded.\n\n"+
				"Fix: Wait a few minutes before retrying", baseErr)
		case 500, 502, 503, 504:
			return "", fmt.Errorf("%s\n\n"+
				"Hardcover API is experiencing issues.\n\n"+
				"Fix: Retry in a few minutes", baseErr)
		default:
			return "", fmt.Errorf("%s", baseErr)
		}
	}

	var result struct {
		Data struct {
			InsertUserBook struct {
				ID int `json:"id"`
			} `json:"insert_user_book"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	if len(result.Errors) > 0 {
		return "", fmt.Errorf("mutation failed: %s", result.Errors[0].Message)
	}

	return fmt.Sprintf("%d", result.Data.InsertUserBook.ID), nil
}

// GetReadingStats retrieves reading statistics for a year
func (c *Client) GetReadingStats(ctx context.Context, year int) (*models.ReadingStats, error) {
	// Simplified - would need proper date filtering
	books, _, err := c.GetUserBooks(ctx, "read", 0, 500)
	if err != nil {
		return nil, err
	}

	stats := &models.ReadingStats{
		Year:           year,
		BooksRead:      len(books),
		GenreBreakdown: make(map[string]int),
	}

	var totalRating float64
	var ratingCount int
	for _, book := range books {
		stats.PagesRead += book.PageCount

		if book.UserStatus != nil && book.UserStatus.Rating > 0 {
			totalRating += book.UserStatus.Rating
			ratingCount++
		}

		for _, genre := range book.Genres {
			stats.GenreBreakdown[genre]++
		}
	}

	if ratingCount > 0 {
		stats.AverageRating = totalRating / float64(ratingCount)
	}

	return stats, nil
}

// Helper to convert status string to Hardcover status_id
func getStatusID(status string) int {
	switch status {
	case "want-to-read":
		return 1
	case "reading":
		return 2
	case "read":
		return 3
	case "dnf":
		return 5
	default:
		return 0 // all
	}
}
