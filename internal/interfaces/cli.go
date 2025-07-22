package interfaces

import (
	"context"
	"io"
)

// CLI defines the interface for command-line operations
type CLI interface {
	// Execute runs the CLI with the given arguments
	Execute(args []string) error
	
	// SetOutput sets the output writer for CLI commands
	SetOutput(out io.Writer)
	
	// SetInput sets the input reader for CLI commands
	SetInput(in io.Reader)
	
	// AddCommand registers a new command
	AddCommand(cmd Command) error
	
	// GetCommand returns a command by name
	GetCommand(name string) (Command, error)
	
	// ListCommands returns all available commands
	ListCommands() []Command
}

// Command represents a CLI command
type Command interface {
	// Name returns the command name
	Name() string
	
	// Description returns the command description
	Description() string
	
	// Usage returns the command usage string
	Usage() string
	
	// Execute runs the command with given arguments
	Execute(ctx context.Context, args []string) error
	
	// Validate checks if the command arguments are valid
	Validate(args []string) error
	
	// Complete provides command completion suggestions
	Complete(args []string) []string
}

// OutputFormatter defines the interface for formatting output
type OutputFormatter interface {
	// FormatEmail formats an email for display
	FormatEmail(email interface{}, format OutputFormat) (string, error)
	
	// FormatEmailList formats a list of emails for display
	FormatEmailList(emails interface{}, format OutputFormat) (string, error)
	
	// FormatSyncStatus formats sync status for display
	FormatSyncStatus(status *SyncStatus, format OutputFormat) (string, error)
	
	// FormatError formats an error for display
	FormatError(err error, format OutputFormat) string
}

// OutputFormat defines output formatting options
type OutputFormat string

const (
	FormatTable OutputFormat = "table"
	FormatJSON  OutputFormat = "json"
	FormatPlain OutputFormat = "plain"
	FormatCSV   OutputFormat = "csv"
)

// InteractiveEditor defines the interface for email composition
type InteractiveEditor interface {
	// ComposeEmail opens an editor for composing a new email
	ComposeEmail(ctx context.Context, template *Email) (*Email, error)
	
	// EditEmail opens an editor for editing an existing email
	EditEmail(ctx context.Context, email *Email) (*Email, error)
	
	// SetEditor sets the external editor command
	SetEditor(command string)
	
	// GetEditor returns the current editor command
	GetEditor() string
}