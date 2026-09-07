package cmd

import (
	"fmt"
	"os"

	"github.com/zsuroy/ctty/internal/ui"

	"github.com/spf13/cobra"
)

var editCmd = &cobra.Command{
	Use:   "edit <hostname>",
	Short: "Edit an existing SSH host configuration",
	Long: `Edit an existing SSH host configuration.

Interactive (default): opens a Bubble Tea form when no host-config flags are set.

Non-interactive: activated when --non-interactive is set, OR when any of
--name, --hostname, --user, --port, --identity-file, --proxy-jump,
--proxy-command, --option/-o, --tags, or --password is provided.
Only changed flags are applied on top of the existing host.

Examples:
  ctty edit web1
  ctty edit web1 --hostname 10.0.0.2 --port 2222
  ctty edit web1 --tags prod,api --format json`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		hostname := args[0]

		if wantNonInteractive(cmd) {
			editHostNonInteractive(cmd, hostname)
			return
		}

		err := ui.RunEditForm(hostname, configFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error editing host: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	RootCmd.AddCommand(editCmd)
	registerHostCLIFlags(editCmd, false)
}
