package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init [path]",
	Short: "Initialize a new email repository",
	Long: `Initialize a new email repository in the specified directory.

This command creates the necessary Git repository structure and configuration files
for storing emails as version-controlled plain-text files. The repository will
include:

- Git repository initialization
- Directory structure for email organization
- Configuration files for snail-cli
- Index files for search functionality
- Filter directory for custom scripts

If no path is specified, the repository will be created at ~/.snail/emails.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if err := ValidateRangeArgs(cmd, args, 0, 1); err != nil {
			return err
		}
		
		if len(args) == 1 {
			path := strings.TrimSpace(args[0])
			if path == "" {
				return fmt.Errorf("repository path cannot be empty")
			}
			
			// Check if path is absolute or relative
			if !filepath.IsAbs(path) {
				// Convert to absolute path
				absPath, err := filepath.Abs(path)
				if err != nil {
					return fmt.Errorf("invalid path: %w", err)
				}
				args[0] = absPath
			}
		}
		
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		var repoPath string
		
		if len(args) == 1 {
			repoPath = args[0]
		} else {
			// Use flag value or default
			flagPath, _ := cmd.Flags().GetString("path")
			if flagPath != "" {
				repoPath = flagPath
			} else {
				// Default path
				homeDir, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("failed to get user home directory: %w", err)
				}
				repoPath = filepath.Join(homeDir, ".snail", "emails")
			}
		}
		
		// Expand tilde if present
		if strings.HasPrefix(repoPath, "~/") {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get user home directory: %w", err)
			}
			repoPath = filepath.Join(homeDir, repoPath[2:])
		}
		
		if IsVerbose(cmd) {
			fmt.Printf("Initializing email repository at: %s\n", repoPath)
		}
		
		// Check if directory already exists and is not empty
		if info, err := os.Stat(repoPath); err == nil {
			if info.IsDir() {
				entries, err := os.ReadDir(repoPath)
				if err != nil {
					return fmt.Errorf("failed to read directory: %w", err)
				}
				if len(entries) > 0 {
					return fmt.Errorf("directory %s already exists and is not empty", repoPath)
				}
			} else {
				return fmt.Errorf("path %s exists but is not a directory", repoPath)
			}
		}
		
		// TODO: Implement repository initialization
		return fmt.Errorf("repository initialization not implemented yet")
	},
	Example: `  # Initialize in default location (~/.snail/emails)
  snail init
  
  # Initialize in specific directory
  snail init /path/to/email/repo
  
  # Initialize with custom path flag
  snail init --path ~/my-emails
  
  # Initialize as bare repository
  snail init --bare`,
}

func init() {
	initCmd.Flags().String("path", "", "path to initialize repository (default: ~/.snail/emails)")
	initCmd.Flags().Bool("bare", false, "initialize as bare repository")
	initCmd.Flags().Bool("force", false, "force initialization even if directory exists")
}