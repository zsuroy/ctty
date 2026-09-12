package cmd

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/zsuroy/ctty/internal/ui"

	"github.com/spf13/cobra"
)

// browseCmd opens the standalone local file browser.
var browseCmd = &cobra.Command{
	Use:   "browse [path]",
	Short: "Open the local file browser",
	Long: `Open the standalone local file browser TUI.

Browse the local filesystem with search, create/rename/delete (with
confirm), file details, and open-with-default-application:

  ctty browse              Browse the current directory
  ctty browse ~/Downloads  Browse a given directory`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		startDir := ""
		if len(args) > 0 {
			abs, err := filepath.Abs(args[0])
			if err != nil {
				log.Fatalf("Error resolving path: %v", err)
			}
			info, err := os.Stat(abs)
			if err != nil || !info.IsDir() {
				log.Fatalf("Not a directory: %s", args[0])
			}
			startDir = abs
		}
		if err := ui.RunLocalBrowserMode(startDir, AppVersion, noUpdateCheck); err != nil {
			log.Fatalf("Error running local browser: %v", err)
		}
		fmt.Println()
	},
}

func init() {
	RootCmd.AddCommand(browseCmd)
}
