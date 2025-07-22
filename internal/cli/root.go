package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"snail-cli/internal/config"
	"snail-cli/internal/interfaces"
)

var (
	cfgFile string
	cfg     *config.Config
	output  io.Writer = os.Stdout
	input   io.Reader = os.Stdin
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "snail",
	Short: "A terminal-based, offline-first email client",
	Long: `Snail-cli is a terminal-based, offline-first email client that treats emails 
as version-controlled assets. It stores emails as plain-text files in a Git-backed 
repository, providing developers with familiar versioned workflows for email management.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		initConfig()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.snail/config.yaml)")
	rootCmd.PersistentFlags().Bool("offline", false, "run in offline mode")
	rootCmd.PersistentFlags().Bool("verbose", false, "enable verbose output")
	rootCmd.PersistentFlags().String("format", "table", "output format (table, json, plain, csv)")

	// Add subcommands
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(readCmd)
	rootCmd.AddCommand(composeCmd)
	rootCmd.AddCommand(replyCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(filterCmd)
	rootCmd.AddCommand(statusCmd)
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfg != nil {
		return // Already initialized
	}

	// Load default configuration
	cfg = config.DefaultConfig()

	// TODO: Load configuration from file if specified
	if cfgFile != "" {
		// Load from specified config file
		fmt.Printf("Loading config from: %s\n", cfgFile)
	} else {
		// Load from default locations
		// $HOME/.snail/config.yaml
		// $HOME/.config/snail/config.yaml
	}
}

// GetConfig returns the current configuration
func GetConfig() *config.Config {
	if cfg == nil {
		initConfig()
	}
	return cfg
}

// CLI implementation that satisfies the interfaces.CLI interface
type CLI struct {
	rootCmd *cobra.Command
	output  io.Writer
	input   io.Reader
}

// NewCLI creates a new CLI instance
func NewCLI() *CLI {
	return &CLI{
		rootCmd: rootCmd,
		output:  output,
		input:   input,
	}
}

// Execute runs the CLI with the given arguments
func (c *CLI) Execute(args []string) error {
	c.rootCmd.SetArgs(args)
	c.rootCmd.SetOut(c.output)
	c.rootCmd.SetIn(c.input)
	return c.rootCmd.Execute()
}

// SetOutput sets the output writer for CLI commands
func (c *CLI) SetOutput(out io.Writer) {
	c.output = out
	c.rootCmd.SetOut(out)
}

// SetInput sets the input reader for CLI commands
func (c *CLI) SetInput(in io.Reader) {
	c.input = in
	c.rootCmd.SetIn(in)
}

// AddCommand registers a new command (placeholder for interface compliance)
func (c *CLI) AddCommand(cmd interfaces.Command) error {
	// This would need to be implemented to convert interfaces.Command to cobra.Command
	return fmt.Errorf("AddCommand not implemented yet")
}

// GetCommand returns a command by name (placeholder for interface compliance)
func (c *CLI) GetCommand(name string) (interfaces.Command, error) {
	// This would need to be implemented to find and return commands
	return nil, fmt.Errorf("GetCommand not implemented yet")
}

// ListCommands returns all available commands (placeholder for interface compliance)
func (c *CLI) ListCommands() []interfaces.Command {
	// This would need to be implemented to return all commands
	return []interfaces.Command{}
}

// ValidateArgs validates command arguments with helpful error messages
func ValidateArgs(cmd *cobra.Command, args []string, expectedCount int) error {
	if len(args) != expectedCount {
		if expectedCount == 0 {
			return fmt.Errorf("command '%s' does not accept any arguments, got %d", cmd.Name(), len(args))
		} else if expectedCount == 1 {
			return fmt.Errorf("command '%s' requires exactly 1 argument, got %d", cmd.Name(), len(args))
		} else {
			return fmt.Errorf("command '%s' requires exactly %d arguments, got %d", cmd.Name(), expectedCount, len(args))
		}
	}
	return nil
}

// ValidateMinArgs validates minimum number of arguments
func ValidateMinArgs(cmd *cobra.Command, args []string, minCount int) error {
	if len(args) < minCount {
		if minCount == 1 {
			return fmt.Errorf("command '%s' requires at least 1 argument, got %d", cmd.Name(), len(args))
		} else {
			return fmt.Errorf("command '%s' requires at least %d arguments, got %d", cmd.Name(), minCount, len(args))
		}
	}
	return nil
}

// ValidateRangeArgs validates argument count within a range
func ValidateRangeArgs(cmd *cobra.Command, args []string, minCount, maxCount int) error {
	if len(args) < minCount || len(args) > maxCount {
		if minCount == maxCount {
			return ValidateArgs(cmd, args, minCount)
		}
		return fmt.Errorf("command '%s' requires between %d and %d arguments, got %d", cmd.Name(), minCount, maxCount, len(args))
	}
	return nil
}

// GetOutputFormat returns the output format from flags
func GetOutputFormat(cmd *cobra.Command) interfaces.OutputFormat {
	format, _ := cmd.Flags().GetString("format")
	switch format {
	case "json":
		return interfaces.FormatJSON
	case "plain":
		return interfaces.FormatPlain
	case "csv":
		return interfaces.FormatCSV
	default:
		return interfaces.FormatTable
	}
}

// IsOfflineMode returns true if offline mode is enabled
func IsOfflineMode(cmd *cobra.Command) bool {
	offline, _ := cmd.Flags().GetBool("offline")
	return offline
}

// IsVerbose returns true if verbose output is enabled
func IsVerbose(cmd *cobra.Command) bool {
	verbose, _ := cmd.Flags().GetBool("verbose")
	return verbose
}