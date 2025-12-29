package models

import "time"

// BookEnrichment represents enriched metadata for a history entry
type BookEnrichment struct {
	ID        int    `json:"id"`
	HistoryID int    `json:"history_id"`
	Title     string `json:"title"`
	Author    string `json:"author"`

	// External IDs
	OpenLibraryID string `json:"openlibrary_id,omitempty"`
	GoogleBooksID string `json:"googlebooks_id,omitempty"`

	// Enriched metadata
	Description string   `json:"description,omitempty"`
	Themes      []string `json:"themes,omitempty"`
	Topics      []string `json:"topics,omitempty"`
	Mood        []string `json:"mood,omitempty"`
	Complexity  string   `json:"complexity,omitempty"` // beginner, intermediate, advanced

	// Series information
	SeriesName     string  `json:"series_name,omitempty"`
	SeriesPosition float64 `json:"series_position,omitempty"`
	SeriesTotal    int     `json:"series_total,omitempty"`

	// Source tracking
	EnrichmentSources []string  `json:"enrichment_sources,omitempty"`
	EnrichedAt        time.Time `json:"enriched_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// RelationshipType constants for book relationships
const (
	RelSameAuthor     = "same_author"
	RelSameSeries     = "same_series"
	RelSimilarTheme   = "similar_theme"
	RelAlsoRead       = "also_read"
	RelFormatAlsoRead = "format_also_read"
	RelPublisherPath  = "publisher_path"
)

// BookRelationship represents a connection between two books
type BookRelationship struct {
	ID               int       `json:"id"`
	FromHistoryID    int       `json:"from_history_id"`
	ToHistoryID      int       `json:"to_history_id"`
	RelationshipType string    `json:"relationship_type"`
	Strength         float64   `json:"strength"` // 0-1 confidence score
	CreatedAt        time.Time `json:"created_at"`
}

// Complexity levels
const (
	ComplexityBeginner     = "beginner"
	ComplexityIntermediate = "intermediate"
	ComplexityAdvanced     = "advanced"
)

// Mood constants for common moods
const (
	MoodHopeful     = "hopeful"
	MoodDark        = "dark"
	MoodFunny       = "funny"
	MoodEducational = "educational"
	MoodThrilling   = "thrilling"
	MoodRomantic    = "romantic"
	MoodMysterious  = "mysterious"
	MoodInspiring   = "inspiring"
	MoodMelancholic = "melancholic"
	MoodWhimsical   = "whimsical"
)

// Theme constants for common themes (non-exhaustive)
const (
	ThemeComingOfAge   = "coming-of-age"
	ThemeGoodVsEvil    = "good-vs-evil"
	ThemeIdentity      = "identity"
	ThemeLove          = "love"
	ThemeFriendship    = "friendship"
	ThemeSurvival      = "survival"
	ThemeRedemption    = "redemption"
	ThemeJustice       = "justice"
	ThemeCourage       = "courage"
	ThemeFamily        = "family"
	ThemeBetrayal      = "betrayal"
	ThemePower         = "power"
	ThemeFreedom       = "freedom"
	ThemePrejudice     = "prejudice"
	ThemeSacrifice     = "sacrifice"
	ThemeDiscovery     = "discovery"
	ThemeRevenge       = "revenge"
	ThemeWar           = "war"
	ThemeTechnology    = "technology"
	ThemeNature        = "nature"
	ThemeAdventure     = "adventure"
	ThemeMystery       = "mystery"
	ThemeHorror        = "horror"
	ThemeSciFi         = "science-fiction"
	ThemeFantasy       = "fantasy"
	ThemeHistorical    = "historical"
	ThemePolitical     = "political"
	ThemePhilosophical = "philosophical"
	ThemePsychological = "psychological"
	ThemeSocial        = "social"
	ThemeSpiritual     = "spiritual"
)

// EnrichmentSource constants
const (
	SourceOpenLibrary = "openlibrary"
	SourceGoogleBooks = "googlebooks"
	SourceHardcover   = "hardcover"
	SourceLibby       = "libby"
	SourceLLM         = "llm"
)

// Topic constants (based on common subjects/genres)
const (
	TopicFiction           = "fiction"
	TopicNonFiction        = "non-fiction"
	TopicMystery           = "mystery"
	TopicThriller          = "thriller"
	TopicRomance           = "romance"
	TopicScienceFiction    = "science-fiction"
	TopicFantasy           = "fantasy"
	TopicHorror            = "horror"
	TopicHistoricalFiction = "historical-fiction"
	TopicBiography         = "biography"
	TopicMemoir            = "memoir"
	TopicHistory           = "history"
	TopicScience           = "science"
	TopicPsychology        = "psychology"
	TopicPhilosophy        = "philosophy"
	TopicSelfHelp          = "self-help"
	TopicBusiness          = "business"
	TopicEconomics         = "economics"
	TopicPolitics          = "politics"
	TopicCurrentEvents     = "current-events"
	TopicTrueCrime         = "true-crime"
	TopicTravel            = "travel"
	TopicFood              = "food"
	TopicArt               = "art"
	TopicMusic             = "music"
	TopicPoetry            = "poetry"
	TopicDrama             = "drama"
	TopicHumor             = "humor"
	TopicReligion          = "religion"
	TopicSpirituality      = "spirituality"
	TopicHealth            = "health"
	TopicFitness           = "fitness"
	TopicParenting         = "parenting"
	TopicRelationships     = "relationships"
	TopicEducation         = "education"
	TopicTechnology        = "technology"
	TopicProgramming       = "programming"
	TopicMathematics       = "mathematics"
	TopicPhysics           = "physics"
	TopicBiology           = "biology"
	TopicChemistry         = "chemistry"
	TopicMedicine          = "medicine"
	TopicEnvironment       = "environment"
	TopicClimate           = "climate-change"
	TopicSocialJustice     = "social-justice"
	TopicFeminism          = "feminism"
	TopicLGBTQ             = "lgbtq"
	TopicRace              = "race"
	TopicClass             = "class"
	TopicImmigration       = "immigration"
)

// UserReadingProfile represents aggregated user reading patterns
type UserReadingProfile struct {
	ID int `json:"-"`

	// Preferences
	PreferredFormats map[string]float64 `json:"preferred_formats,omitempty"` // audiobook: 0.6, ebook: 0.4
	PreferredGenres  map[string]int     `json:"preferred_genres,omitempty"`  // science-fiction: 15
	PreferredAuthors map[string]int     `json:"preferred_authors,omitempty"` // "Brandon Sanderson": 12

	// Patterns
	AvgReadingSpeed  float64            `json:"avg_reading_speed,omitempty"` // pages/day
	CompletionRate   float64            `json:"completion_rate,omitempty"`   // % of books finished
	AbandonTriggers  []string           `json:"abandon_triggers,omitempty"`  // "too long", "dry"
	SeriesCompletion map[string]float64 `json:"series_completion,omitempty"` // How often user finishes series

	// Temporal
	ReadingCadence map[string]int      `json:"reading_cadence,omitempty"` // books per month
	Streaks        []ReadingStreak     `json:"streaks,omitempty"`
	Seasonal       map[string][]string `json:"seasonal,omitempty"` // "summer": ["beach reads"]

	// Social signals
	RatingsDistribution map[int]int `json:"ratings_distribution,omitempty"` // 1-star: 2, 5-star: 45
	ReviewLength        int         `json:"review_length,omitempty"`        // Avg chars per review

	// Computed fields
	ComputedAt time.Time `json:"computed_at"`
}

// ReadingStreak represents a period of consistent reading
type ReadingStreak struct {
	StartDate time.Time `json:"start_date"`
	EndDate   time.Time `json:"end_date"`
	Duration  int       `json:"duration"` // days
	BooksRead int       `json:"books_read"`
	IsCurrent bool      `json:"is_current"`
}
