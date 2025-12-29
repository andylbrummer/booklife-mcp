package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sblinch/kdl-go"
)

// Config represents the complete BookLife configuration
type Config struct {
	Server    ServerConfig    `kdl:"server"`
	User      UserConfig      `kdl:"user"`
	Providers ProvidersConfig `kdl:"providers"`
	Cache     CacheConfig     `kdl:"cache"`
	Features  FeaturesConfig  `kdl:"features"`
	Sync      SyncConfig      `kdl:"sync"`
}

// SyncConfig configures cross-service synchronization
type SyncConfig struct {
	// LibbyToHardcover syncs library returns to Hardcover "read" status
	LibbyToHardcover LibbyToHardcoverSync `kdl:"libby-to-hardcover"`
}

type LibbyToHardcoverSync struct {
	Enabled       bool `kdl:"enabled"`        // Enable automatic sync (default: false)
	SyncOnReturn  bool `kdl:"sync-on-return"` // Sync when book is returned
	AutoMarkRead  bool `kdl:"auto-mark-read"` // Automatically mark as "read" (default: true)
	IncludeAudio  bool `kdl:"include-audio"`  // Include audiobooks (default: true)
	IncludeEbooks bool `kdl:"include-ebooks"` // Include ebooks (default: true)
}

type ServerConfig struct {
	Name      string `kdl:"name"`
	Version   string `kdl:"version"`
	Transport string `kdl:"transport"`
}

type UserConfig struct {
	Name        string            `kdl:"name"`
	Timezone    string            `kdl:"timezone"`
	Preferences PreferencesConfig `kdl:"preferences"`
}

type PreferencesConfig struct {
	Genres           []string `kdl:"genres"`
	AvoidGenres      []string `kdl:"avoid-genres"`
	PreferredFormats []string `kdl:"preferred-formats"`
	MaxTBRSize       int      `kdl:"max-tbr-size"`
}

type ProvidersConfig struct {
	Hardcover       HardcoverConfig       `kdl:"hardcover"`
	Libby           LibbyConfig           `kdl:"libby"`
	OpenLibrary     OpenLibraryConfig     `kdl:"open-library"`
	Wikidata        WikidataConfig        `kdl:"wikidata"`
	YouTube         YouTubeConfig         `kdl:"youtube"`
	TikTok          TikTokConfig          `kdl:"tiktok"`
	LocalBookstores LocalBookstoresConfig `kdl:"local-bookstores"`
}

type HardcoverConfig struct {
	Enabled  bool   `kdl:"enabled"`
	APIKey   string `kdl:"api-key"`
	Endpoint string `kdl:"endpoint"`
	Sync     struct {
		AutoImportLibby bool   `kdl:"auto-import-libby"`
		DefaultStatus   string `kdl:"default-status"`
	} `kdl:"sync"`
}

type LibbyConfig struct {
	Enabled       bool `kdl:"enabled"`
	SkipTLSVerify bool `kdl:"skip-tls-verify"`
	// Libraries are synced from Libby automatically via 'booklife libby-connect'
	Notifications struct {
		HoldAvailable bool `kdl:"hold-available"`
		DueSoonDays   int  `kdl:"due-soon-days"`
	} `kdl:"notifications"`
	// TimelineURL is the optional Libby timeline export URL for importing reading history
	// Format: https://share.libbyapp.com/data/{uuid}/libbytimeline-all-loans.json
	TimelineURL string `kdl:"timeline-url"`
}

type OpenLibraryConfig struct {
	Enabled        bool   `kdl:"enabled"`
	Endpoint       string `kdl:"endpoint"`
	CoversEndpoint string `kdl:"covers-endpoint"`
	RateLimitMS    int    `kdl:"rate-limit-ms"`
}

type WikidataConfig struct {
	Enabled        bool   `kdl:"enabled"`
	SPARQLEndpoint string `kdl:"sparql-endpoint"`
}

type YouTubeConfig struct {
	Enabled          bool     `kdl:"enabled"`
	APIKey           string   `kdl:"api-key"`
	BookTubeChannels []string `kdl:"booktube-channels"`
}

type TikTokConfig struct {
	Enabled    bool     `kdl:"enabled"`
	ScraperAPI string   `kdl:"scraper-api"`
	Hashtags   []string `kdl:"hashtags"`
}

type LocalBookstoresConfig struct {
	Enabled bool          `kdl:"enabled"`
	Stores  []StoreConfig `kdl:"store,multiple"`
}

type StoreConfig struct {
	ID            string   `kdl:",arg"`
	Name          string   `kdl:"name"`
	Website       string   `kdl:"website"`
	Location      string   `kdl:"location"`
	Phone         string   `kdl:"phone"`
	EventsURL     string   `kdl:"events-url"`
	SearchEnabled bool     `kdl:"search-enabled"`
	Specialties   []string `kdl:"specialties"`
}

type CacheConfig struct {
	Path           string           `kdl:"path"`
	BookMetadata   bool             `kdl:"book-metadata"`
	CoverImages    bool             `kdl:"cover-images"`
	ReadingHistory bool             `kdl:"reading-history"`
	Embeddings     EmbeddingsConfig `kdl:"embeddings"`
}

type EmbeddingsConfig struct {
	Enabled   bool   `kdl:"enabled"`
	Model     string `kdl:"model"`
	IndexPath string `kdl:"index-path"`
}

type FeaturesConfig struct {
	SemanticSearch bool `kdl:"semantic-search"`
	AutoHold       bool `kdl:"auto-hold"`
	MoodTracking   bool `kdl:"mood-tracking"`
}

// DefaultPath returns the platform-specific default configuration file path.
// Checks in order:
// 1. ./booklife.kdl (current directory, for development)
// 2. ~/.config/booklife/booklife.kdl (platform-specific config dir)
func DefaultPath() (string, error) {
	// Check current directory first (for development)
	localPath := "booklife.kdl"
	if _, err := os.Stat(localPath); err == nil {
		return localPath, nil
	}

	// Use platform-specific config directory (or XDG_CONFIG_HOME)
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	xdgConfig := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfig == "" {
		xdgConfig = filepath.Join(home, ".config")
	}
	return filepath.Join(xdgConfig, "booklife", "booklife.kdl"), nil
}

// Load reads and parses the KDL configuration file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config file not found: %s\n\n"+
				"Fix:\n"+
				"1. Create a config file at: %s\n"+
				"2. Or use --config to specify a different location\n"+
				"3. See example: https://github.com/user/booklife-mcp/blob/main/booklife.kdl.example", path, path)
		}
		return nil, fmt.Errorf("reading config file %s: %w", path, err)
	}

	var cfg Config
	if err := kdl.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %s: %w\n\n"+
			"Fix:\n"+
			"1. Check KDL syntax (https://kdl.dev)\n"+
			"2. Verify all brackets and quotes are balanced\n"+
			"3. Check for typos in field names", path, err)
	}

	// Resolve environment variables (fails fast on missing vars)
	if err := resolveEnvVars(&cfg); err != nil {
		return nil, err
	}

	// Validate configuration
	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// resolveEnvVars replaces env="VAR_NAME" patterns with actual environment values
// Returns error if required environment variables are not set
func resolveEnvVars(cfg *Config) error {
	var err error

	// Hardcover API key (required if enabled)
	cfg.Providers.Hardcover.APIKey, err = resolveEnv(cfg.Providers.Hardcover.APIKey, "HARDCOVER_API_KEY", cfg.Providers.Hardcover.Enabled)
	if err != nil {
		return err
	}

	// YouTube API key (optional)
	cfg.Providers.YouTube.APIKey, err = resolveEnv(cfg.Providers.YouTube.APIKey, "YOUTUBE_API_KEY", false)
	if err != nil {
		return err
	}

	// TikTok scraper API (optional)
	cfg.Providers.TikTok.ScraperAPI, err = resolveEnv(cfg.Providers.TikTok.ScraperAPI, "TIKTOK_SCRAPER_API", false)
	if err != nil {
		return err
	}

	return nil
}

// resolveEnv resolves a single environment variable reference
// If required=true and the env var is not set, returns an error
func resolveEnv(value, varName string, required bool) (string, error) {
	if !strings.HasPrefix(value, "env=") {
		return value, nil
	}

	envVar := strings.TrimPrefix(value, "env=")
	envVar = strings.Trim(envVar, "\"")

	envValue := os.Getenv(envVar)
	if envValue == "" && required {
		return "", fmt.Errorf("required environment variable %q is not set\n\n"+
			"Fix:\n"+
			"1. Set the environment variable:\n"+
			"   export %s='your-value-here'\n"+
			"2. Or add it to your shell profile (~/.bashrc, ~/.zshrc, etc.)\n"+
			"3. Or pass it when starting the server:\n"+
			"   %s='your-value' booklife serve", envVar, envVar, envVar)
	}

	return envValue, nil
}

func validate(cfg *Config) error {
	if cfg.Server.Name == "" {
		return fmt.Errorf("server.name is required in config\n\n" +
			"Fix: Add to your config file:\n" +
			"  server {\n" +
			"    name \"booklife\"\n" +
			"    version \"1.0.0\"\n" +
			"  }")
	}

	if cfg.Providers.Hardcover.Enabled && cfg.Providers.Hardcover.APIKey == "" {
		return fmt.Errorf("Hardcover API key required when Hardcover is enabled\n\n" +
			"Fix:\n" +
			"1. Get an API key from: https://hardcover.app/settings/api\n" +
			"2. Set environment variable: export HARDCOVER_API_KEY='your-key'\n" +
			"3. Or disable Hardcover in config: hardcover { enabled false }")
	}

	// Note: Libby no longer requires clone code in config.
	// Identity is stored in ~/.config/booklife/libby-identity.json
	// via 'booklife libby-connect <code>'

	return nil
}
