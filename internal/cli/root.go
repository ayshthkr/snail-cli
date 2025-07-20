package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"snail-cli/internal/config"
)

var (
	cfgFile string
	cfg     *config.Config
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