package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new email repository",
	Long: `Initialize a new email repository in the specified directory.
This creates the necessary Git repository structure and configuration files.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Initializing email repository...")
		// TODO: Implement repository initialization
		return fmt.Errorf("not implemented yet")
	},
}

func init() {
	initCmd.Flags().String("path", "", "path to initialize repository (default: ~/.snail/emails)")
	initCmd.Flags().Bool("bare", false, "initialize as bare repository")
}