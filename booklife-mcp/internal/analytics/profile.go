package analytics

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/user/booklife-mcp/internal/models"
)

// ComputeService computes reading profiles from history data
type ComputeService struct {
	db *sql.DB
}

// NewComputeService creates a new profile computation service
func NewComputeService(db *sql.DB) *ComputeService {
	return &ComputeService{db: db}
}

// ComputeProfile computes the user's reading profile from history
func (c *ComputeService) ComputeProfile(ctx context.Context) (*models.UserReadingProfile, error) {
	profile := &models.UserReadingProfile{
		PreferredFormats:    make(map[string]float64),
		PreferredGenres:     make(map[string]int),
		PreferredAuthors:    make(map[string]int),
		SeriesCompletion:    make(map[string]float64),
		ReadingCadence:      make(map[string]int),
		Streaks:             []models.ReadingStreak{},
		Seasonal:            make(map[string][]string),
		RatingsDistribution: make(map[int]int),
		ComputedAt:          time.Now(),
	}

	// Compute format preferences
	if err := c.computeFormatPreferences(ctx, profile); err != nil {
		return nil, fmt.Errorf("computing format preferences: %w", err)
	}

	// Compute genre preferences (from enrichment data)
	if err := c.computeGenrePreferences(ctx, profile); err != nil {
		return nil, fmt.Errorf("computing genre preferences: %w", err)
	}

	// Compute author preferences
	if err := c.computeAuthorPreferences(ctx, profile); err != nil {
		return nil, fmt.Errorf("computing author preferences: %w", err)
	}

	// Compute completion rate
	if err := c.computeCompletionRate(ctx, profile); err != nil {
		return nil, fmt.Errorf("computing completion rate: %w", err)
	}

	// Compute reading cadence
	if err := c.computeReadingCadence(ctx, profile); err != nil {
		return nil, fmt.Errorf("computing reading cadence: %w", err)
	}

	// Compute streaks
	if err := c.computeStreaks(ctx, profile); err != nil {
		return nil, fmt.Errorf("computing streaks: %w", err)
	}

	// Save profile to database
	if err := c.saveProfile(ctx, profile); err != nil {
		return nil, fmt.Errorf("saving profile: %w", err)
	}

	return profile, nil
}

// GetProfile retrieves the current reading profile
func (c *ComputeService) GetProfile(ctx context.Context) (*models.UserReadingProfile, error) {
	query := `
		SELECT id, preferred_formats, preferred_genres, preferred_authors,
		       avg_reading_speed, completion_rate, abandon_triggers,
		       series_completion, reading_cadence, streaks, seasonal,
		       ratings_distribution, avg_review_length, computed_at
		FROM reading_profile
		ORDER BY computed_at DESC
		LIMIT 1
	`

	var profile models.UserReadingProfile
	var formatsJSON, genresJSON, authorsJSON, abandonJSON, seriesJSON, cadenceJSON, streaksJSON, seasonalJSON, ratingsJSON sql.NullString

	err := c.db.QueryRowContext(ctx, query).Scan(
		&profile.ID,
		&formatsJSON, &genresJSON, &authorsJSON,
		&profile.AvgReadingSpeed, &profile.CompletionRate, &abandonJSON,
		&seriesJSON, &cadenceJSON, &streaksJSON, &seasonalJSON,
		&ratingsJSON, &profile.ReviewLength, &profile.ComputedAt,
	)
	if err != nil {
		return nil, err
	}

	// Parse JSON fields
	if formatsJSON.Valid {
		json.Unmarshal([]byte(formatsJSON.String), &profile.PreferredFormats)
	}
	if genresJSON.Valid {
		json.Unmarshal([]byte(genresJSON.String), &profile.PreferredGenres)
	}
	if authorsJSON.Valid {
		json.Unmarshal([]byte(authorsJSON.String), &profile.PreferredAuthors)
	}
	if abandonJSON.Valid {
		json.Unmarshal([]byte(abandonJSON.String), &profile.AbandonTriggers)
	}
	if seriesJSON.Valid {
		json.Unmarshal([]byte(seriesJSON.String), &profile.SeriesCompletion)
	}
	if cadenceJSON.Valid {
		json.Unmarshal([]byte(cadenceJSON.String), &profile.ReadingCadence)
	}
	if streaksJSON.Valid {
		json.Unmarshal([]byte(streaksJSON.String), &profile.Streaks)
	}
	if seasonalJSON.Valid {
		json.Unmarshal([]byte(seasonalJSON.String), &profile.Seasonal)
	}
	if ratingsJSON.Valid {
		json.Unmarshal([]byte(ratingsJSON.String), &profile.RatingsDistribution)
	}

	return &profile, nil
}

// computeFormatPreferences calculates format distribution
func (c *ComputeService) computeFormatPreferences(ctx context.Context, profile *models.UserReadingProfile) error {
	query := `
		SELECT format, COUNT(*) as count
		FROM history
		WHERE activity = 'Returned'
		GROUP BY format
	`

	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	total := 0
	formatCounts := make(map[string]int)

	for rows.Next() {
		var format string
		var count int
		if err := rows.Scan(&format, &count); err != nil {
			return err
		}
		formatCounts[format] = count
		total += count
	}

	// Convert to percentages
	for format, count := range formatCounts {
		profile.PreferredFormats[format] = float64(count) / float64(total)
	}

	return nil
}

// computeGenrePreferences calculates genre preferences from enrichment data
func (c *ComputeService) computeGenrePreferences(ctx context.Context, profile *models.UserReadingProfile) error {
	query := `
		SELECT be.topics
		FROM book_enrichment be
		INNER JOIN history h ON be.history_id = h.id
		WHERE h.activity = 'Returned'
	`

	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	genreCounts := make(map[string]int)

	for rows.Next() {
		var topicsJSON sql.NullString
		if err := rows.Scan(&topicsJSON); err != nil {
			return err
		}

		if topicsJSON.Valid {
			var topics []string
			json.Unmarshal([]byte(topicsJSON.String), &topics)
			for _, topic := range topics {
				genreCounts[topic]++
			}
		}
	}

	profile.PreferredGenres = genreCounts
	return nil
}

// computeAuthorPreferences calculates author preferences
func (c *ComputeService) computeAuthorPreferences(ctx context.Context, profile *models.UserReadingProfile) error {
	query := `
		SELECT author, COUNT(*) as count
		FROM history
		WHERE activity = 'Returned'
		GROUP BY author
		ORDER BY count DESC
		LIMIT 50
	`

	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var author string
		var count int
		if err := rows.Scan(&author, &count); err != nil {
			return err
		}
		profile.PreferredAuthors[author] = count
	}

	return nil
}

// computeCompletionRate calculates the percentage of books finished
func (c *ComputeService) computeCompletionRate(ctx context.Context, profile *models.UserReadingProfile) error {
	query := `
		SELECT
			COUNT(CASE WHEN activity = 'Returned' THEN 1 END) as returned,
			COUNT(CASE WHEN activity = 'Borrowed' THEN 1 END) as borrowed
		FROM history
	`

	var returned, borrowed int
	err := c.db.QueryRowContext(ctx, query).Scan(&returned, &borrowed)
	if err != nil {
		return err
	}

	total := returned + borrowed
	if total > 0 {
		profile.CompletionRate = float64(returned) / float64(total)
	}

	return nil
}

// computeReadingCadence calculates books read per month
func (c *ComputeService) computeReadingCadence(ctx context.Context, profile *models.UserReadingProfile) error {
	query := `
		SELECT
			strftime('%Y-%m', datetime(timestamp/1000, 'unix')) as month,
			COUNT(*) as count
		FROM history
		WHERE activity = 'Returned'
		  AND timestamp IS NOT NULL
		  AND timestamp > 0
		GROUP BY month
		HAVING month IS NOT NULL
		ORDER BY month DESC
		LIMIT 24
	`

	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var month string
		var count int
		if err := rows.Scan(&month, &count); err != nil {
			return err
		}
		profile.ReadingCadence[month] = count
	}

	return nil
}

// computeStreaks calculates reading streaks
func (c *ComputeService) computeStreaks(ctx context.Context, profile *models.UserReadingProfile) error {
	query := `
		SELECT DISTINCT DATE(datetime(timestamp/1000, 'unix')) as read_date
		FROM history
		WHERE activity = 'Returned'
		  AND timestamp IS NOT NULL
		  AND timestamp > 0
		ORDER BY read_date DESC
	`

	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	var dates []time.Time
	for rows.Next() {
		var dateStr sql.NullString
		if err := rows.Scan(&dateStr); err != nil {
			return err
		}
		if !dateStr.Valid {
			continue
		}
		date, err := time.Parse("2006-01-02", dateStr.String)
		if err != nil {
			continue
		}
		dates = append(dates, date)
	}

	if len(dates) == 0 {
		return nil
	}

	// Find current streak
	currentStreak := models.ReadingStreak{IsCurrent: true}
	for i := 0; i < len(dates); i++ {
		if i == 0 {
			currentStreak.EndDate = dates[i]
			currentStreak.StartDate = dates[i]
			currentStreak.Duration = 1
			currentStreak.BooksRead = 1
		} else {
			diff := dates[i-1].Sub(dates[i]).Hours() / 24
			if diff <= 2 { // Allow 2 day gap
				currentStreak.StartDate = dates[i]
				currentStreak.Duration++
				currentStreak.BooksRead++
			} else {
				break
			}
		}
	}

	if currentStreak.Duration > 0 {
		profile.Streaks = append(profile.Streaks, currentStreak)
	}

	// Find longest streak (simplified - just check if current is long)
	// A full implementation would scan all dates to find the historical max

	return nil
}

// saveProfile saves the computed profile to the database
func (c *ComputeService) saveProfile(ctx context.Context, profile *models.UserReadingProfile) error {
	formatsJSON, _ := json.Marshal(profile.PreferredFormats)
	genresJSON, _ := json.Marshal(profile.PreferredGenres)
	authorsJSON, _ := json.Marshal(profile.PreferredAuthors)
	abandonJSON, _ := json.Marshal(profile.AbandonTriggers)
	seriesJSON, _ := json.Marshal(profile.SeriesCompletion)
	cadenceJSON, _ := json.Marshal(profile.ReadingCadence)
	streaksJSON, _ := json.Marshal(profile.Streaks)
	seasonalJSON, _ := json.Marshal(profile.Seasonal)
	ratingsJSON, _ := json.Marshal(profile.RatingsDistribution)

	query := `
		INSERT INTO reading_profile
		(preferred_formats, preferred_genres, preferred_authors,
		 avg_reading_speed, completion_rate, abandon_triggers,
		 series_completion, reading_cadence, streaks, seasonal,
		 ratings_distribution, avg_review_length, computed_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`

	_, err := c.db.ExecContext(ctx, query,
		string(formatsJSON), string(genresJSON), string(authorsJSON),
		profile.AvgReadingSpeed, profile.CompletionRate, string(abandonJSON),
		string(seriesJSON), string(cadenceJSON), string(streaksJSON), string(seasonalJSON),
		string(ratingsJSON), profile.ReviewLength, profile.ComputedAt,
	)

	return err
}

// GetProfileSummary returns a human-readable profile summary
func (c *ComputeService) GetProfileSummary(ctx context.Context) (string, error) {
	profile, err := c.GetProfile(ctx)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString("📊 Reading Profile\n\n")

	// Format preferences
	sb.WriteString("📚 Format Preferences:\n")
	for format, pct := range profile.PreferredFormats {
		sb.WriteString(fmt.Sprintf("   %s: %.1f%%\n", format, pct*100))
	}

	// Top genres
	sb.WriteString("\n🏷️  Top Genres:\n")
	count := 0
	for genre, n := range profile.PreferredGenres {
		if count >= 5 {
			break
		}
		sb.WriteString(fmt.Sprintf("   %s: %d books\n", genre, n))
		count++
	}

	// Top authors
	sb.WriteString("\n✍️  Most Read Authors:\n")
	count = 0
	for author, n := range profile.PreferredAuthors {
		if count >= 5 {
			break
		}
		sb.WriteString(fmt.Sprintf("   %s: %d books\n", author, n))
		count++
	}

	// Completion rate
	sb.WriteString(fmt.Sprintf("\n✅ Completion Rate: %.1f%%\n", profile.CompletionRate*100))

	// Recent cadence
	sb.WriteString("\n📅 Recent Reading Cadence:\n")
	count = 0
	for month, n := range profile.ReadingCadence {
		if count >= 6 {
			break
		}
		sb.WriteString(fmt.Sprintf("   %s: %d books\n", month, n))
		count++
	}

	// Current streak
	if len(profile.Streaks) > 0 {
		for _, streak := range profile.Streaks {
			if streak.IsCurrent {
				sb.WriteString(fmt.Sprintf("\n🔥 Current Streak: %d days (%d books)\n", streak.Duration, streak.BooksRead))
				break
			}
		}
	}

	return sb.String(), nil
}
