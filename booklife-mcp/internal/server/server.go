package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/user/booklife-mcp/internal/analytics"
	"github.com/user/booklife-mcp/internal/config"
	"github.com/user/booklife-mcp/internal/enrichment"
	"github.com/user/booklife-mcp/internal/graph"
	"github.com/user/booklife-mcp/internal/history"
	"github.com/user/booklife-mcp/internal/providers"
	"github.com/user/booklife-mcp/internal/providers/hardcover"
	"github.com/user/booklife-mcp/internal/providers/libby"
	"github.com/user/booklife-mcp/internal/providers/openlibrary"
	"github.com/user/booklife-mcp/internal/tbr"
)

// Server wraps the MCP server with BookLife providers
type Server struct {
	cfg       *config.Config
	mcpServer *mcp.Server
	DataDir   string // Exported for recommendation handlers

	// Providers (using interfaces for testability)
	hardcover   providers.HardcoverProvider
	libby       providers.LibbyProvider
	openlibrary providers.OpenLibraryProvider

	// Local history store
	historyStore *history.Store

	// Local TBR store
	tbrStore *tbr.Store

	// Recommendation services (lazy initialization)
	enrichmentService *enrichment.Service
	graphBuilder      *graph.Builder
	profileService    *analytics.ComputeService
}

// New creates a new BookLife MCP server
func New(cfg *config.Config) (*Server, error) {
	s := &Server{
		cfg: cfg,
	}

	// Initialize MCP server
	s.mcpServer = mcp.NewServer(&mcp.Implementation{
		Name:    cfg.Server.Name,
		Version: cfg.Server.Version,
	}, nil)

	// Initialize providers
	if err := s.initProviders(); err != nil {
		return nil, fmt.Errorf("initializing providers: %w", err)
	}

	// Register tools
	s.registerTools()

	// Register resources
	s.registerResources()

	// Register prompts
	s.registerPrompts()

	return s, nil
}

func (s *Server) initProviders() error {
	// Initialize local history store
	dataDir := os.Getenv("BOOKLIFE_DATA_DIR")
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".local", "share", "booklife")
	}
	s.DataDir = dataDir

	store, err := history.NewStore(dataDir)
	if err != nil {
		return fmt.Errorf("initializing history store: %w", err)
	}
	s.historyStore = store

	// Initialize TBR store
	tbrStore, err := tbr.NewStore(dataDir)
	if err != nil {
		return fmt.Errorf("initializing TBR store: %w", err)
	}
	s.tbrStore = tbrStore

	// Hardcover
	if s.cfg.Providers.Hardcover.Enabled {
		client, err := hardcover.NewClient(
			s.cfg.Providers.Hardcover.Endpoint,
			s.cfg.Providers.Hardcover.APIKey,
		)
		if err != nil {
			return fmt.Errorf("initializing Hardcover client: %w", err)
		}
		s.hardcover = client
	}

	// Libby - uses saved identity from 'booklife libby-connect'
	if s.cfg.Providers.Libby.Enabled {
		if !libby.HasSavedIdentity() {
			identityPath, _ := libby.IdentityPath()
			fmt.Fprintf(os.Stderr, "Warning: Libby enabled but no saved identity found\n\n"+
				"Libby tools will not be available until you connect.\n\n"+
				"To connect:\n"+
				"1. Run: booklife libby-connect <code>\n"+
				"2. Get clone code from Libby app:\n"+
				"   Settings > Copy To Another Device > Sonos Speakers\n"+
				"3. You have ~40 seconds to complete the connection\n\n"+
				"Identity will be saved to: %s\n\n", identityPath)
			// Continue without Libby - don't fail server initialization
		} else {
			client, err := libby.NewClientFromSavedIdentityWithOptions(s.cfg.Providers.Libby.SkipTLSVerify)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to initialize Libby client: %v\n\n"+
					"Libby tools will not be available.\n\n"+
					"This usually means:\n"+
					"1. Identity file is corrupted\n"+
					"2. Network connectivity issues\n"+
					"3. OverDrive API changes\n\n"+
					"To fix:\n"+
					"1. Reconnect: booklife libby-connect <code>\n"+
					"2. Check network connection\n"+
					"3. If TLS errors, enable skip-tls-verify in config\n\n", err)
				// Continue without Libby - don't fail server initialization
			} else {
				s.libby = client

				// Auto-import timeline if URL is configured
				if s.cfg.Providers.Libby.TimelineURL != "" {
					importer := history.NewImporter(store)
					count, err := importer.ImportTimeline(s.cfg.Providers.Libby.TimelineURL)
					if err != nil {
						fmt.Printf("Warning: Failed to import timeline: %v\n", err)
					} else {
						fmt.Printf("Imported %d timeline entries\n", count)
					}
				}
			}
		}
	}

	// Open Library
	if s.cfg.Providers.OpenLibrary.Enabled {
		s.openlibrary = openlibrary.NewClient(
			s.cfg.Providers.OpenLibrary.Endpoint,
			s.cfg.Providers.OpenLibrary.CoversEndpoint,
		)
	}

	return nil
}

// Run starts the MCP server
func (s *Server) Run(ctx context.Context) error {
	// Use stdio transport
	transport := &mcp.StdioTransport{}
	return s.mcpServer.Run(ctx, transport)
}
