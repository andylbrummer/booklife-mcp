package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/user/booklife-mcp/internal/config"
	"github.com/user/booklife-mcp/internal/dirs"
	"github.com/user/booklife-mcp/internal/history"
	"github.com/user/booklife-mcp/internal/providers/hardcover"
	"github.com/user/booklife-mcp/internal/providers/libby"
	"github.com/user/booklife-mcp/internal/server"
	"github.com/user/booklife-mcp/internal/sync"
)

// Version information set by ldflags during build
var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		cmdServe()
	case "libby-connect":
		cmdLibbyConnect()
	case "sync":
		cmdSync()
	case "import-timeline":
		cmdImportTimeline()
	case "version", "-v", "--version":
		cmdVersion()
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func cmdVersion() {
	fmt.Printf("BookLife MCP Server v%s (built %s)\n", Version, BuildTime)
}

func printUsage() {
	defaultPath, _ := config.DefaultPath()
	configDir, _ := dirs.ConfigDir()

	fmt.Println(`BookLife MCP Server - Personal Reading Life Assistant

Usage:
  booklife <command> [options]

Commands:
  serve            Start the MCP server
  libby-connect    Connect a Libby clone code (you have 40 seconds!)
  sync             Sync reading history to Hardcover
  import-timeline  Import Libby timeline JSON file
  version          Show version information
  help             Show this help message

Serve Options:
  --config PATH    Path to configuration file
                   (default: ` + defaultPath + `)

Sync Options:
  --dry-run        Show what would be synced without making changes
  --limit N        Only sync N records (for testing)
  --config PATH    Path to configuration file

Import Timeline:
  booklife import-timeline <json_file> [--config PATH]

  Imports Libby timeline from a JSON file (exported from Libby app).

Config directories:
  Linux/macOS:     ` + configDir + `
  Windows:         %APPDATA%\BookLife

Libby Connect:
  booklife libby-connect <8-digit-code> [--skip-tls-verify]

  To get a clone code:
    1. Open Libby app on your phone
    2. Go to Settings > Copy To Another Device
    3. Tap "Sonos Speakers" or "Android Automotive"
    4. Run this command immediately with the displayed code
    5. You have ~40 seconds before the code expires!

Options:
  --skip-tls-verify  Skip TLS certificate verification (insecure)`)
}

func cmdServe() {
	// Use default config path if not specified
	configPath, _ := config.DefaultPath()

	// Parse serve-specific flags
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config", "-c":
			if i+1 < len(args) {
				configPath = args[i+1]
				i++
			}
		}
	}

	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Configuration Error\n\n%v\n", err)
		os.Exit(1)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals (silently - MCP uses stdio)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	// Create and run the MCP server
	srv, err := server.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Server Initialization Error\n\n%v\n", err)
		os.Exit(1)
	}

	// Note: No startup message - MCP uses stdio for communication
	if err := srv.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Server Runtime Error\n\n%v\n", err)
		os.Exit(1)
	}
}

func cmdLibbyConnect() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Error: clone code required")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage: booklife libby-connect <8-digit-code> [--skip-tls-verify]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Options:")
		fmt.Fprintln(os.Stderr, "  --skip-tls-verify  Skip TLS certificate verification (use if OverDrive cert is broken)")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "To get a clone code:")
		fmt.Fprintln(os.Stderr, "  1. Open Libby app on your phone")
		fmt.Fprintln(os.Stderr, "  2. Go to Settings > Copy To Another Device")
		fmt.Fprintln(os.Stderr, "  3. Tap \"Sonos Speakers\" or \"Android Automotive\"")
		fmt.Fprintln(os.Stderr, "  4. Run this command immediately with the displayed code")
		os.Exit(1)
	}

	code := os.Args[2]
	skipTLSVerify := false
	for _, arg := range os.Args[3:] {
		if arg == "--skip-tls-verify" {
			skipTLSVerify = true
		}
	}

	// Validate code format
	if len(code) != 8 {
		fmt.Fprintf(os.Stderr, "Error: clone code must be exactly 8 digits (got %d)\n", len(code))
		os.Exit(1)
	}

	for _, c := range code {
		if c < '0' || c > '9' {
			fmt.Fprintf(os.Stderr, "Error: clone code must contain only digits\n")
			os.Exit(1)
		}
	}

	fmt.Println("⏱️  Connecting to Libby...")
	fmt.Printf("   Code: %s\n", code)
	if skipTLSVerify {
		fmt.Println("   ⚠️  TLS verification disabled (insecure)")
	}

	// Connect and save identity
	identity, libraries, err := libby.ConnectWithOptions(code, skipTLSVerify)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n❌ Failed to connect: %v\n", err)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Common issues:")
		fmt.Fprintln(os.Stderr, "  - Code expired (you only have ~40 seconds)")
		fmt.Fprintln(os.Stderr, "  - Typo in the code")
		fmt.Fprintln(os.Stderr, "  - Network connectivity issue")
		os.Exit(1)
	}

	// Save identity to disk
	if err := libby.SaveIdentity(identity); err != nil {
		fmt.Fprintf(os.Stderr, "\n⚠️  Connected but failed to save identity: %v\n", err)
		os.Exit(1)
	}

	// Get the actual path where identity was saved
	configDir, _ := dirs.ConfigDir()
	identityPath := configDir + "/libby-identity.json"

	fmt.Println("\n✅ Successfully connected to Libby!")
	fmt.Println("")
	fmt.Printf("   Linked libraries (%d):\n", len(libraries))
	for _, lib := range libraries {
		fmt.Printf("     • %s\n", lib.Name)
	}
	fmt.Println("")
	fmt.Printf("   Identity saved to %s\n", identityPath)
	fmt.Println("   You can now use BookLife with your Libby account!")
}

func cmdSync() {
	// Parse flags
	configPath, _ := config.DefaultPath()
	dryRun := false
	limit := 0 // 0 = all

	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config", "-c":
			if i+1 < len(args) {
				configPath = args[i+1]
				i++
			}
		case "--dry-run", "-n":
			dryRun = true
		case "--limit", "-l":
			if i+1 < len(args) {
				var lim int
				_, err := fmt.Sscanf(args[i+1], "%d", &lim)
				if err == nil {
					limit = lim
				}
				i++
			}
		}
	}

	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Configuration Error\n\n%v\n", err)
		os.Exit(1)
	}

	// Check Hardcover is configured
	if !cfg.Providers.Hardcover.Enabled || cfg.Providers.Hardcover.APIKey == "" {
		fmt.Fprintln(os.Stderr, "Error: Hardcover provider not configured")
		fmt.Fprintln(os.Stderr, "Add hardcover configuration to your booklife.kdl")
		os.Exit(1)
	}

	// Initialize history store
	dataDir, err := dirs.DataDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get data directory: %v\n", err)
		os.Exit(1)
	}

	store, err := history.NewStore(dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open history store: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	// Initialize Hardcover client
	hcClient, err := hardcover.NewClient(
		cfg.Providers.Hardcover.Endpoint,
		cfg.Providers.Hardcover.APIKey,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create Hardcover client: %v\n", err)
		os.Exit(1)
	}

	// Create sync processor
	adapter := sync.NewStoreAdapter(store)
	syncer := sync.NewHardcoverSync(hcClient, adapter)
	syncer.SetDryRun(dryRun)
	if limit > 0 {
		syncer.SetLimit(limit)
		fmt.Printf("📊 Limiting to %d records\n\n", limit)
	}

	// Run sync
	ctx := context.Background()

	if dryRun {
		fmt.Println("🔍 Dry run - no changes will be made")
		fmt.Println()
	}

	fmt.Println("📚 Syncing returned books to Hardcover...")
	fmt.Println()

	summary, err := syncer.SyncReturnedBooks(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Sync failed: %v\n", err)
		os.Exit(1)
	}

	// Print results
	fmt.Printf("Processed: %d books\n", summary.TotalProcessed)
	fmt.Printf("  ✓ Successful: %d\n", summary.Successful)
	fmt.Printf("  ⊘ Skipped: %d\n", summary.Skipped)
	fmt.Printf("  ✗ Failed: %d\n", summary.Failed)
	fmt.Println()

	// Show details
	if len(summary.Results) > 0 {
		fmt.Println("\nMatched Books:")
		fmt.Println()
		for _, r := range summary.Results {
			libbyTitle := r.Operation.Title
			if len(libbyTitle) > 40 {
				libbyTitle = libbyTitle[:37] + "..."
			}

			status := "✓"
			detail := ""
			if r.Skipped {
				status = "⊘"
				detail = r.SkipReason
			} else if !r.Success {
				status = "✗"
				detail = r.ErrorMessage
			} else if r.TargetBookID != "" {
				detail = fmt.Sprintf("→ hardcover:%s", r.TargetBookID)
			}

			if r.Success && r.TargetTitle != "" {
				// Show both titles side by side for successful matches
				hardcoverTitle := r.TargetTitle
				if len(hardcoverTitle) > 40 {
					hardcoverTitle = hardcoverTitle[:37] + "..."
				}
				fmt.Printf("  %s Libby:  \"%s\"\n", status, libbyTitle)
				fmt.Printf("       HC:    \"%s\"", hardcoverTitle)
				if detail != "" {
					fmt.Printf(" (%s)", detail)
				}
				fmt.Println()
			} else if r.Skipped || !r.Success {
				fmt.Printf("  %s \"%s\"", status, libbyTitle)
				if detail != "" {
					fmt.Printf(" (%s)", detail)
				}
				fmt.Println()
			}
		}
	}

	if len(summary.Errors) > 0 {
		fmt.Println()
		fmt.Println("Errors:")
		for _, e := range summary.Errors {
			fmt.Printf("  • %s\n", e)
		}
	}

	if dryRun {
		fmt.Println()
		fmt.Println("Run without --dry-run to apply changes")
	}
}

func cmdImportTimeline() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Error: JSON file path required")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage: booklife import-timeline <json_file> [--config PATH]")
		os.Exit(1)
	}

	jsonPath := os.Args[2]

	// Get data directory
	dataDir, err := dirs.DataDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get data directory: %v\n", err)
		os.Exit(1)
	}

	// Read JSON file
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read JSON file: %v\n", err)
		os.Exit(1)
	}

	// Initialize history store
	store, err := history.NewStore(dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open history store: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	// Import timeline
	importer := history.NewImporter(store)
	count, err := importer.ImportTimelineBytes(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to import timeline: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Imported %d timeline entries from %s\n", count, jsonPath)
}
