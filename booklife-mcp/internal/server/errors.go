package server

import (
	"fmt"
	"strings"
)

// ConfigError provides helpful context for configuration errors
type ConfigError struct {
	Provider string
	Reason   string
	FixSteps []string
	HelpURL  string
}

func (e ConfigError) Error() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s is not configured\n\n", e.Provider))

	if e.Reason != "" {
		sb.WriteString(fmt.Sprintf("Reason: %s\n\n", e.Reason))
	}

	if len(e.FixSteps) > 0 {
		sb.WriteString("Fix:\n")
		for i, step := range e.FixSteps {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, step))
		}
		sb.WriteString("\n")
	}

	if e.HelpURL != "" {
		sb.WriteString(fmt.Sprintf("More info: %s\n", e.HelpURL))
	}

	return sb.String()
}

// NewHardcoverNotConfiguredError creates a helpful error for Hardcover configuration issues
func NewHardcoverNotConfiguredError() error {
	return ConfigError{
		Provider: "Hardcover",
		Reason:   "API key not configured or provider disabled",
		FixSteps: []string{
			"Add HARDCOVER_API_KEY to environment",
			"Update booklife.kdl:\n   providers {\n     hardcover enabled=true {\n       api-key env=\"HARDCOVER_API_KEY\"\n     }\n   }",
			"Restart the MCP server",
		},
		HelpURL: "https://hardcover.app/settings/api",
	}
}

// NewLibbyNotConfiguredError creates a helpful error for Libby configuration issues
func NewLibbyNotConfiguredError() error {
	return ConfigError{
		Provider: "Libby",
		Reason:   "No saved identity found or provider disabled",
		FixSteps: []string{
			"Get clone code from Libby app: Settings > Copy To Another Device",
			"Connect to Libby: booklife libby-connect <code>",
			"You have ~40 seconds to complete the connection",
			"Enable in booklife.kdl:\n   providers {\n     libby enabled=true\n   }",
			"Restart the MCP server",
		},
		HelpURL: "https://github.com/user/booklife-mcp#libby-setup",
	}
}

// InputValidationError provides helpful context for input validation errors
type InputValidationError struct {
	Field        string
	Value        string
	Reason       string
	Suggestion   string
	ValidOptions []string
}

func (e InputValidationError) Error() string {
	var sb strings.Builder

	if e.Value != "" {
		sb.WriteString(fmt.Sprintf("Invalid value for %s: \"%s\"\n", e.Field, e.Value))
	} else {
		sb.WriteString(fmt.Sprintf("%s is required\n", e.Field))
	}

	if e.Reason != "" {
		sb.WriteString(fmt.Sprintf("\nReason: %s\n", e.Reason))
	}

	if e.Suggestion != "" {
		sb.WriteString(fmt.Sprintf("\nSuggestion: %s\n", e.Suggestion))
	}

	if len(e.ValidOptions) > 0 {
		sb.WriteString(fmt.Sprintf("\nValid options: %s\n", strings.Join(e.ValidOptions, ", ")))
	}

	return sb.String()
}

// NewMissingQueryError creates a helpful error for missing query parameters
func NewMissingQueryError() error {
	return InputValidationError{
		Field:      "query",
		Reason:     "Search query is required to find books",
		Suggestion: "Provide a book title, author name, or ISBN",
	}
}

// NewInvalidStatusError creates a helpful error for invalid status values
func NewInvalidStatusError(value string) error {
	return InputValidationError{
		Field:        "status",
		Value:        value,
		Reason:       "Unknown reading status",
		ValidOptions: []string{"reading", "read", "want-to-read", "dnf", "all"},
		Suggestion:   "Use one of the valid status values",
	}
}

// NewInvalidActionError creates a helpful error for invalid action parameters
func NewInvalidActionError(tool, value string, validActions []string) error {
	return InputValidationError{
		Field:        "action",
		Value:        value,
		Reason:       fmt.Sprintf("Unknown action for %s tool", tool),
		ValidOptions: validActions,
		Suggestion:   "Use one of the valid actions listed below",
	}
}

// NewQueryTooLongError creates a helpful error for query length violations
func NewQueryTooLongError(length, maxLength int) error {
	return InputValidationError{
		Field:      "query",
		Reason:     fmt.Sprintf("Query is too long (%d characters, max %d)", length, maxLength),
		Suggestion: "Shorten your search query or be more specific",
	}
}
