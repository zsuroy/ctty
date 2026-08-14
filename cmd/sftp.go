package cmd

import (
	"fmt"
	"log"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/ui"
)

// sftpCmd represents the sftp command
var sftpCmd = &cobra.Command{
	Use:   "sftp <host>",
	Short: "Open SFTP file browser for a host",
	Long: `Open the interactive SFTP file browser for the specified host.

Browse remote files, upload local files, download remote files, create directories,
and manage remote filesystem graphically.`,
	Args: cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		var hosts []config.SSHHost
		var err error

		if configFile != "" {
			hosts, err = config.ParseSSHConfigFile(configFile)
		} else {
			hosts, err = config.ParseSSHConfig()
		}

		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}

		hosts = config.FilterVisibleHosts(hosts)

		var completions []string
		toCompleteLower := strings.ToLower(toComplete)
		for _, host := range hosts {
			if strings.HasPrefix(strings.ToLower(host.Name), toCompleteLower) {
				completions = append(completions, host.Name)
			}
		}

		return completions, cobra.ShellCompDirectiveNoFileComp
	},
	Run: func(cmd *cobra.Command, args []string) {
		hostName := args[0]
		if err := ui.RunSFTPMode(hostName, configFile, AppVersion, noUpdateCheck); err != nil {
			log.Fatalf("Error running SFTP mode: %v", err)
		}
		fmt.Println()
	},
}

func init() {
	RootCmd.AddCommand(sftpCmd)
}
