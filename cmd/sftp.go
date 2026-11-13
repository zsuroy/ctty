package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/sftpconfig"
	"github.com/zsuroy/ctty/internal/ui"

	"golang.org/x/term"
)

// sftpCmd is the parent for SFTP. It supports both:
//
//	ctty sftp <host>              → TUI browser (humans)
//	ctty sftp ls|mkdir|rm/...     → headless ops (agents, parity with ftp)
var sftpCmd = &cobra.Command{
	Use:   "sftp [host|ls|mkdir|rm|rmdir|rename]",
	Short: "Open SFTP file browser or run headless file ops",
	Long: `Open the interactive SFTP file browser or run headless SFTP operations.

Forms:
  ctty sftp <host>                            Open TUI file browser for a host
  ctty sftp ls <host> [remotePath] [--format json]   List remote directory
  ctty sftp mkdir <host> <remotePath>                Create remote directory
  ctty sftp rm <host> <remotePath>                   Delete remote file
  ctty sftp rmdir <host> <remotePath>                Delete remote directory (recursive)
  ctty sftp rename <host> <old> <new>                Rename / move remote file

Root-level transfers remain:
  ctty put <host> <local> <remote>   (upload)
  ctty get <host> <remote> <local>   (download)
  ctty scp <src> <dst>               (scp-style)

Agents: prefer --format json for ls; never open the TUI.`,
	Args: cobra.ArbitraryArgs,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// Completion for `ctty sftp <TAB>` and `ctty sftp ls <TAB>`
		if len(args) == 0 {
			subs := []string{"ls", "mkdir", "rm", "rmdir", "rename"}
			var out []string
			lower := strings.ToLower(toComplete)
			for _, s := range subs {
				if strings.HasPrefix(s, lower) {
					out = append(out, s)
				}
			}
			// also suggest hosts
			hosts := completeSFTPHosts(toComplete)
			out = append(out, hosts...)
			return out, cobra.ShellCompDirectiveNoFileComp
		}
		if len(args) == 1 {
			// after subcommand, complete host
			switch args[0] {
			case "ls", "mkdir", "rm", "rmdir", "rename":
				return completeSFTPHosts(toComplete), cobra.ShellCompDirectiveNoFileComp
			}
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			_ = cmd.Help()
			os.Exit(1)
		}
		// If first arg is a known subcommand, Cobra would have dispatched to the subcommand.
		// Reaching here means it's a host name for the TUI.
		if len(args) == 1 {
			hostName := args[0]
			if err := ui.RunSFTPMode(hostName, configFile, AppVersion, noUpdateCheck); err != nil {
				log.Fatalf("Error running SFTP mode: %v", err)
			}
			fmt.Println()
			return
		}
		_ = cmd.Help()
		os.Exit(1)
	},
}

func completeSFTPHosts(toComplete string) []string {
	var hosts []config.SSHHost
	var err error
	if configFile != "" {
		hosts, err = config.ParseSSHConfigFile(configFile)
	} else {
		hosts, err = config.ParseSSHConfig()
	}
	if err != nil {
		return nil
	}
	hosts = config.FilterVisibleHosts(hosts)
	var out []string
	lower := strings.ToLower(toComplete)
	for _, h := range hosts {
		if strings.HasPrefix(strings.ToLower(h.Name), lower) {
			out = append(out, h.Name)
		}
	}
	return out
}

func connectSFTPClient(hostName string) *sftpconfig.SFTPClient {
	client, err := dialSFTPClient(hostName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	return client
}

func dialSFTPClient(hostName string) (*sftpconfig.SFTPClient, error) {
	client, err := sftpconfig.ConnectWithPassword(hostName, configFile, "")
	if sftpconfig.IsPassphraseRequired(err) {
		passphrase, perr := promptPrivateKeyPassphrase(err)
		if perr != nil {
			return nil, perr
		}
		client, err = sftpconfig.ConnectWithAuth(hostName, configFile, "", passphrase)
	}
	return client, err
}

func promptPrivateKeyPassphrase(err error) (string, error) {
	path := "private key"
	var pe *sftpconfig.PassphraseRequiredError
	if errors.As(err, &pe) && pe.Path != "" {
		path = pe.Path
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", fmt.Errorf("%w (not a terminal; cannot prompt for passphrase)", err)
	}
	fmt.Fprintf(os.Stderr, "Passphrase for %s: ", path)
	b, rerr := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if rerr != nil {
		return "", rerr
	}
	return string(b), nil
}

// --- headless SFTP ops ---

var sftpLsFormat string

var sftpLsCmd = &cobra.Command{
	Use:   "ls <host> [remotePath]",
	Short: "List remote directory via SFTP",
	Long: `List a remote directory via SFTP.

Example:
  ctty sftp ls prod-server
  ctty sftp ls prod-server /var/log --format json`,
	Args:              cobra.RangeArgs(1, 2),
	ValidArgsFunction: hostCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		hostName := args[0]
		remotePath := "."
		if len(args) > 1 {
			remotePath = args[1]
		}
		client := connectSFTPClient(hostName)
		defer client.Close()
		entries, err := client.ListDir(remotePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if sftpLsFormat == "json" {
			if entries == nil {
				entries = []sftpconfig.RemoteEntry{}
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(entries)
			return
		}
		if len(entries) == 0 {
			fmt.Println("(empty)")
			return
		}
		for _, e := range entries {
			kind := "file"
			if e.IsDir {
				kind = "dir"
			}
			fmt.Printf("%-6s %10d  %s  %s\n", kind, e.Size, e.ModTime.Format("2006-01-02 15:04"), e.Name)
		}
	},
}

var sftpMkdirCmd = &cobra.Command{
	Use:               "mkdir <host> <remotePath>",
	Short:             "Create a remote directory via SFTP",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: hostCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		client := connectSFTPClient(args[0])
		defer client.Close()
		if err := client.Mkdir(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Created %s:%s\n", args[0], args[1])
	},
}

var sftpRmCmd = &cobra.Command{
	Use:               "rm <host> <remotePath>",
	Short:             "Delete a remote file via SFTP",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: hostCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		client := connectSFTPClient(args[0])
		defer client.Close()
		if err := client.Remove(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Deleted %s:%s\n", args[0], args[1])
	},
}

var sftpRmdirCmd = &cobra.Command{
	Use:               "rmdir <host> <remotePath>",
	Short:             "Delete a remote directory recursively via SFTP",
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: hostCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		client := connectSFTPClient(args[0])
		defer client.Close()
		if err := client.RemoveAll(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Removed %s:%s\n", args[0], args[1])
	},
}

var sftpRenameCmd = &cobra.Command{
	Use:               "rename <host> <oldPath> <newPath>",
	Short:             "Rename / move a remote file via SFTP",
	Args:              cobra.ExactArgs(3),
	ValidArgsFunction: hostCompletionFirstArg,
	Run: func(cmd *cobra.Command, args []string) {
		client := connectSFTPClient(args[0])
		defer client.Close()
		if err := client.Rename(args[1], args[2]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Renamed %s:%s -> %s\n", args[0], args[1], args[2])
	},
}

func init() {
	RootCmd.AddCommand(sftpCmd)
	sftpLsCmd.Flags().StringVar(&sftpLsFormat, "format", "", "Output format: json (for ls)")
	sftpCmd.AddCommand(sftpLsCmd)
	sftpCmd.AddCommand(sftpMkdirCmd)
	sftpCmd.AddCommand(sftpRmCmd)
	sftpCmd.AddCommand(sftpRmdirCmd)
	sftpCmd.AddCommand(sftpRenameCmd)
}
