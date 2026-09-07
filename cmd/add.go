package cmd

import (
	"fmt"
	"os"

	"github.com/zsuroy/ctty/internal/ui"

	"github.com/spf13/cobra"
)

var addCmd = &cobra.Command{
	Use:   "add [hostname]",
	Short: "Add a new SSH host configuration",
	Long: `Add a new SSH host configuration.

Interactive (default): opens a Bubble Tea form when no host-config flags are set.

Non-interactive: activated when --non-interactive is set, OR when any of
--name, --hostname, --user, --port, --identity-file, --proxy-jump,
--proxy-command, --option/-o, --tags, --password, or --force is provided.
Requires --name (or the positional alias) and --hostname. Reuses the same
OpenSSH config write/backup/validation path as the TUI form.

Examples:
  ctty add
  ctty add myhost
  ctty add --name web1 --hostname 10.0.0.1 --user deploy --tags prod,web
  ctty add --name web1 --hostname 10.0.0.1 --force --format json`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var positional string
		if len(args) > 0 {
			positional = args[0]
		}

		if wantNonInteractive(cmd) {
			addHostNonInteractive(cmd, positional)
			return
		}

		err := ui.RunAddForm(positional, configFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error adding host: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	RootCmd.AddCommand(addCmd)
	registerHostCLIFlags(addCmd, true)
}
