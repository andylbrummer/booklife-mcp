package debug

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/user/booklife-mcp/internal/dirs"
)

// Enabled returns true if debug logging is enabled via BOOKLIFE_DEBUG env var
func Enabled() bool {
	return os.Getenv("BOOKLIFE_DEBUG") == "true" || os.Getenv("BOOKLIFE_DEBUG") == "1"
}

// Log writes debug information to a secure log file if debug mode is enabled
// Only writes if BOOKLIFE_DEBUG=true is set
func Log(component string, data []byte) error {
	if !Enabled() {
		return nil
	}

	// Get data directory (secure, user-only location)
	dataDir, err := dirs.DataDir()
	if err != nil {
		// Silently fail - debug logging should never break the app
		return nil
	}

	// Ensure debug directory exists with secure permissions
	debugDir := filepath.Join(dataDir, "debug")
	if err := os.MkdirAll(debugDir, 0700); err != nil {
		return nil
	}

	// Write to component-specific log file (user-only permissions)
	logPath := filepath.Join(debugDir, fmt.Sprintf("%s-debug.log", component))
	if err := os.WriteFile(logPath, data, 0600); err != nil {
		return nil
	}

	return nil
}

// Logf writes formatted debug information
func Logf(component string, format string, args ...interface{}) error {
	if !Enabled() {
		return nil
	}

	data := fmt.Sprintf(format, args...)
	return Log(component, []byte(data))
}
