package graph

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/user/booklife-mcp/internal/models"
)

// Builder builds and manages the book relationship graph
type Builder struct {
	db *sql.DB
}

// NewBuilder creates a new relationship graph builder
func NewBuilder(db *sql.DB) *Builder {
	return &Builder{db: db}
}

// BuildRelationships builds relationships for a history entry
func (b *Builder) BuildRelationships(ctx context.Context, historyID int) error {
	// Get the history entry details
	var title, author string
	err := b.db.QueryRowContext(ctx, "SELECT title, author FROM history WHERE id = ?", historyID).Scan(&title, &author)
	if err != nil {
		return err
	}

	// Build same-author relationships
	if err := b.buildSameAuthorRelationships(ctx, historyID, author); err != nil {
		return fmt.Errorf("building same-author relationships: %w", err)
	}

	// Build same-series relationships
	if err := b.buildSameSeriesRelationships(ctx, historyID, title, author); err != nil {
		return fmt.Errorf("building same-series relationships: %w", err)
	}

	return nil
}

// BuildAllRelationships builds relationships for all history entries
func (b *Builder) BuildAllRelationships(ctx context.Context) error {
	// Get all history IDs
	rows, err := b.db.QueryContext(ctx, "SELECT id FROM history ORDER BY timestamp DESC")
	if err != nil {
		return err
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}

	// Build relationships for each
	count := 0
	for _, id := range ids {
		if err := b.BuildRelationships(ctx, id); err != nil {
			// Log but continue
			fmt.Printf("Warning: failed to build relationships for history ID %d: %v\n", id, err)
			continue
		}
		count++
	}

	fmt.Printf("Built relationships for %d history entries\n", count)
	return nil
}

// buildSameAuthorRelationships creates relationships between books by the same author
func (b *Builder) buildSameAuthorRelationships(ctx context.Context, historyID int, author string) error {
	// Parse author list to handle "Author1, Author2" format
	authors := parseAuthorList(author)

	// Collect all relationships to create, then batch insert
	type relToCreate struct {
		fromID   int
		toID     int
		relType  string
		strength float64
	}
	var relationships []relToCreate

	// Find all other books by the same author
	query := `
		SELECT id, title, author
		FROM history
		WHERE id != ? AND author LIKE ?
		ORDER BY timestamp DESC
	`

	for _, a := range authors {
		rows, err := b.db.QueryContext(ctx, query, historyID, "%"+a+"%")
		if err != nil {
			return err
		}

		for rows.Next() {
			var otherID int
			var otherTitle, otherAuthor string
			if err := rows.Scan(&otherID, &otherTitle, &otherAuthor); err != nil {
				rows.Close()
				return err
			}

			// Calculate strength based on exact author match
			strength := 0.5
			if strings.EqualFold(author, otherAuthor) {
				strength = 1.0
			}

			// Collect relationship to create (both directions)
			relationships = append(relationships, relToCreate{
				fromID:   historyID,
				toID:     otherID,
				relType:  models.RelSameAuthor,
				strength: strength,
			})
		}
		rows.Close()
	}

	// Batch create all relationships
	for _, rel := range relationships {
		if err := b.createRelationship(ctx, rel.fromID, rel.toID, rel.relType, rel.strength); err != nil {
			return err
		}
	}

	return nil
}

// buildSameSeriesRelationships creates relationships between books in the same series
func (b *Builder) buildSameSeriesRelationships(ctx context.Context, historyID int, title, author string) error {
	// Check if we have enrichment data with series info
	var seriesName sql.NullString
	var seriesPosition sql.NullFloat64

	err := b.db.QueryRowContext(ctx,
		"SELECT series_name, series_position FROM book_enrichment WHERE history_id = ?",
		historyID).Scan(&seriesName, &seriesPosition)

	if err != nil || !seriesName.Valid {
		// No enrichment data or no series, try to infer from title
		seriesName, seriesPosition = inferSeriesFromTitle(title)
		if !seriesName.Valid {
			return nil // Not a series book
		}
	}

	// Find other books in the same series
	query := `
		SELECT h.id, be.series_position
		FROM history h
		INNER JOIN book_enrichment be ON h.id = be.history_id
		WHERE h.id != ? AND be.series_name = ?
		ORDER BY be.series_position
	`

	rows, err := b.db.QueryContext(ctx, query, historyID, seriesName.String)
	if err != nil {
		// Might be no enrichment table yet, that's OK
		return nil
	}

	// Collect all relationships to create, then batch insert
	type relToCreate struct {
		fromID   int
		toID     int
		relType  string
		strength float64
	}
	var relationships []relToCreate

	for rows.Next() {
		var otherID int
		var otherPosition sql.NullFloat64
		if err := rows.Scan(&otherID, &otherPosition); err != nil {
			rows.Close()
			return err
		}

		// Calculate strength (books close in series are more related)
		strength := 0.8
		if seriesPosition.Valid && otherPosition.Valid {
			diff := seriesPosition.Float64 - otherPosition.Float64
			if diff < 0 {
				diff = -diff
			}
			// Reduce strength for books further apart in series
			if diff > 5 {
				strength = 0.4
			} else if diff > 2 {
				strength = 0.6
			}
		}

		// Collect relationship to create
		relationships = append(relationships, relToCreate{
			fromID:   historyID,
			toID:     otherID,
			relType:  models.RelSameSeries,
			strength: strength,
		})
	}
	rows.Close()

	// Batch create all relationships
	for _, rel := range relationships {
		if err := b.createRelationship(ctx, rel.fromID, rel.toID, rel.relType, rel.strength); err != nil {
			return err
		}
	}

	return nil
}

// BuildAlsoReadRelationships creates "also read" relationships based on temporal patterns
// Books read around the same time are likely thematically related
func (b *Builder) BuildAlsoReadRelationships(ctx context.Context) error {
	// For each book, find books read within a time window
	query := `
		SELECT h1.id as h1_id, h2.id as h2_id,
		       ABS(h1.timestamp - h2.timestamp) as time_diff
		FROM history h1
		CROSS JOIN history h2
		WHERE h1.id < h2.id
		  AND h1.timestamp IS NOT NULL
		  AND h1.timestamp > 0
		  AND h2.timestamp IS NOT NULL
		  AND h2.timestamp > 0
		  AND ABS(h1.timestamp - h2.timestamp) < 2592000000 -- 30 days in milliseconds
		  AND h1.activity = 'Returned'
		  AND h2.activity = 'Returned'
		ORDER BY h1.timestamp DESC
	`

	rows, err := b.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}

	// Collect all relationships to create, then batch insert
	type relToCreate struct {
		fromID   int
		toID     int
		relType  string
		strength float64
	}
	var relationships []relToCreate

	for rows.Next() {
		var id1, id2 int
		var timeDiff int64
		if err := rows.Scan(&id1, &id2, &timeDiff); err != nil {
			rows.Close()
			return err
		}

		// Calculate strength based on time proximity
		// Books read on same day = higher strength
		strength := 0.3
		if timeDiff < 86400000 { // 1 day
			strength = 0.7
		} else if timeDiff < 604800000 { // 1 week
			strength = 0.5
		}

		// Collect relationship to create
		relationships = append(relationships, relToCreate{
			fromID:   id1,
			toID:     id2,
			relType:  models.RelAlsoRead,
			strength: strength,
		})
	}
	rows.Close()

	// Batch create all relationships
	count := 0
	for _, rel := range relationships {
		if err := b.createRelationship(ctx, rel.fromID, rel.toID, rel.relType, rel.strength); err != nil {
			return err
		}
		count++
	}

	fmt.Printf("Built %d 'also read' relationships\n", count)
	return nil
}

// GetRelationships gets all relationships for a history entry
func (b *Builder) GetRelationships(ctx context.Context, historyID int) ([]models.BookRelationship, error) {
	query := `
		SELECT id, from_history_id, to_history_id, relationship_type, strength, created_at
		FROM book_relationships
		WHERE from_history_id = ? OR to_history_id = ?
		ORDER BY strength DESC
	`

	rows, err := b.db.QueryContext(ctx, query, historyID, historyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var relationships []models.BookRelationship
	for rows.Next() {
		var r models.BookRelationship
		if err := rows.Scan(&r.ID, &r.FromHistoryID, &r.ToHistoryID, &r.RelationshipType, &r.Strength, &r.CreatedAt); err != nil {
			return nil, err
		}
		relationships = append(relationships, r)
	}

	return relationships, nil
}

// GetRelatedBooks gets books related to a given history entry
func (b *Builder) GetRelatedBooks(ctx context.Context, historyID int, relType string, limit int) ([]RelatedBook, error) {
	query := `
		SELECT
			CASE
				WHEN br.from_history_id = ? THEN br.to_history_id
				ELSE br.from_history_id
			END as related_id,
			h.title, h.author, h.format, h.cover_url,
			br.relationship_type, br.strength,
			be.themes, be.topics, be.mood
		FROM book_relationships br
		INNER JOIN history h ON (
			CASE
				WHEN br.from_history_id = ? THEN br.to_history_id
				ELSE br.from_history_id
			END = h.id
		)
		LEFT JOIN book_enrichment be ON be.history_id = h.id
		WHERE (br.from_history_id = ? OR br.to_history_id = ?)
			AND (? = '' OR br.relationship_type = ?)
		ORDER BY br.strength DESC
		LIMIT ?
	`

	rows, err := b.db.QueryContext(ctx, query, historyID, historyID, historyID, historyID, relType, relType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var books []RelatedBook
	for rows.Next() {
		var b RelatedBook
		var themesJSON, topicsJSON, moodJSON, coverURL sql.NullString

		err := rows.Scan(
			&b.HistoryID, &b.Title, &b.Author, &b.Format, &coverURL,
			&b.RelationshipType, &b.Strength,
			&themesJSON, &topicsJSON, &moodJSON,
		)
		if err != nil {
			return nil, err
		}

		if coverURL.Valid {
			b.CoverURL = coverURL.String
		}
		if themesJSON.Valid {
			json.Unmarshal([]byte(themesJSON.String), &b.Themes)
		}
		if topicsJSON.Valid {
			json.Unmarshal([]byte(topicsJSON.String), &b.Topics)
		}
		if moodJSON.Valid {
			json.Unmarshal([]byte(moodJSON.String), &b.Mood)
		}

		books = append(books, b)
	}

	return books, nil
}

// RelatedBook represents a book related to another with relationship info
type RelatedBook struct {
	HistoryID        int      `json:"history_id"`
	Title            string   `json:"title"`
	Author           string   `json:"author"`
	Format           string   `json:"format"`
	CoverURL         string   `json:"cover_url,omitempty"`
	RelationshipType string   `json:"relationship_type"`
	Strength         float64  `json:"strength"`
	Themes           []string `json:"themes,omitempty"`
	Topics           []string `json:"topics,omitempty"`
	Mood             []string `json:"mood,omitempty"`
}

// createRelationship creates a relationship between two history entries
func (b *Builder) createRelationship(ctx context.Context, fromID, toID int, relType string, strength float64) error {
	query := `
		INSERT OR REPLACE INTO book_relationships
		(from_history_id, to_history_id, relationship_type, strength)
		VALUES (?, ?, ?, ?)
	`

	_, err := b.db.ExecContext(ctx, query, fromID, toID, relType, strength)
	return err
}

// parseAuthorList parses author string into individual names
func parseAuthorList(author string) []string {
	parts := strings.FieldsFunc(author, func(r rune) bool {
		return r == ',' || r == ';' || r == '&'
	})

	result := []string{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}

	return result
}

// inferSeriesFromTitle attempts to infer series information from title
func inferSeriesFromTitle(title string) (sql.NullString, sql.NullFloat64) {
	// Look for common patterns like:
	// - "Book Title, Book 1 of Series"
	// - "Series Name #1"
	// - "Series Name: Book One"

	lower := strings.ToLower(title)

	// Pattern 1: "#1", "#2", etc. at end
	if idx := strings.LastIndex(title, "#"); idx > 0 {
		seriesName := strings.TrimSpace(title[:idx])
		var pos float64
		fmt.Sscanf(title[idx+1:], "%f", &pos)
		if pos > 0 {
			return sql.NullString{String: seriesName, Valid: true}, sql.NullFloat64{Float64: pos, Valid: true}
		}
	}

	// Pattern 2: "Book One", "Book Two" etc.
	ordinalWords := map[string]float64{
		"first": 1, "second": 2, "third": 3, "fourth": 4, "fifth": 5,
		"sixth": 6, "seventh": 7, "eighth": 8, "ninth": 9, "tenth": 10,
		"one": 1, "two": 2, "three": 3, "four": 4, "five": 5,
		"six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10,
		"i": 1, "ii": 2, "iii": 3, "iv": 4, "v": 5,
		"vi": 6, "vii": 7, "viii": 8, "ix": 9, "x": 10,
	}

	for word, pos := range ordinalWords {
		pattern := "book " + word
		if idx := strings.Index(lower, pattern); idx > 0 {
			seriesName := strings.TrimSpace(title[:idx])
			return sql.NullString{String: seriesName, Valid: true}, sql.NullFloat64{Float64: pos, Valid: true}
		}
	}

	return sql.NullString{}, sql.NullFloat64{}
}
