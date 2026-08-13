package cmd

import (
	"fmt"
	"log"

	"github.com/zsuroy/ctty/internal/ui"

	"github.com/spf13/cobra"
)

// serialCmd represents the serial command
var serialCmd = &cobra.Command{
	Use:   "serial",
	Short: "Open serial device manager",
	Long: `Open the serial device manager TUI directly.

Lists auto-detected serial ports and saved device configurations.
Press Enter to connect, e to edit parameters, a to add, d to delete.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := ui.RunSerialMode(AppVersion, noUpdateCheck); err != nil {
			log.Fatalf("Error running serial mode: %v", err)
		}
		fmt.Println()
	},
}

func init() {
	RootCmd.AddCommand(serialCmd)
}
