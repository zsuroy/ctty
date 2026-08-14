package cmd

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/credential"
	"github.com/zsuroy/ctty/internal/history"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/ui"

	"github.com/spf13/cobra"
)

// AppVersion will be set at build time via -ldflags
var AppVersion = "dev"

// configFile holds the path to the SSH config file
var configFile string

// forceTTY forces pseudo-TTY allocation for remote commands
var forceTTY bool

// searchMode enables the focus on search mode at startup
var searchMode bool

// noUpdateCheck disables the async update check in the TUI
var noUpdateCheck bool

// langFlag overrides the interface language
var langFlag string

// RootCmd is the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "ctty [host] [command...]",
	Short: "Connection Manager - SSH, Serial & SFTP",
	Long: `ctty is a lightweight terminal connection manager for SSH, serial, and SFTP.

Main usage:
  Running 'ctty' (without arguments) opens the interactive TUI window to browse, search, and connect to your SSH hosts graphically.
  Running 'ctty <host>' connects directly to the specified host and records the connection in your history.
  Running 'ctty <host> <command>' executes the command on the remote host and returns the output.

You can also use ctty in CLI mode for other operations like adding, editing, or searching hosts.

Hosts are read from your ~/.ssh/config file by default.

Examples:
  ctty                           # Open interactive TUI
  ctty prod-server               # Connect to host interactively
  ctty prod-server uptime        # Execute 'uptime' on remote host
  ctty prod-server ls -la /var   # Execute command with arguments
  ctty -t prod-server sudo reboot # Force TTY for interactive commands
  ctty search prod               # Search hosts by keyword or tag
  ctty sftp prod-server          # Open SFTP file browser for host
  ctty serial                    # Open serial device manager`,
	Version:       AppVersion,
	Args:          cobra.ArbitraryArgs,
	SilenceUsage:  true,
	SilenceErrors: true, // We'll handle errors ourselves
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if langFlag != "" {
			i18n.Init(langFlag)
		} else {
			appConfig, err := config.LoadAppConfig()
			if err == nil && appConfig.Language != "" {
				i18n.Init(appConfig.Language)
			} else {
				i18n.Init("auto")
			}
		}
	},
	// ValidArgsFunction provides shell completion for host names
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// Only complete the first positional argument (host name)
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
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			runInteractiveMode()
			return nil
		}

		hostName := args[0]
		var remoteCommand []string
		if len(args) > 1 {
			remoteCommand = args[1:]
		}
		connectToHost(hostName, remoteCommand)
		return nil
	},
}

func runInteractiveMode() {
	// Parse SSH configurations
	var hosts []config.SSHHost
	var err error

	if configFile != "" {
		hosts, err = config.ParseSSHConfigFile(configFile)
	} else {
		hosts, err = config.ParseSSHConfig()
	}

	if err != nil {
		log.Fatalf("Error reading SSH config file: %v", err)
	}

	// Run the interactive TUI directly (even with 0 hosts)
	if err := ui.RunInteractiveMode(hosts, configFile, searchMode, AppVersion, noUpdateCheck); err != nil {
		log.Fatalf("Error running interactive mode: %v", err)
	}
}

func connectToHost(hostName string, remoteCommand []string) {
	var hostFound bool
	var err error

	if configFile != "" {
		hostFound, err = config.QuickHostExistsInFile(hostName, configFile)
	} else {
		hostFound, err = config.QuickHostExists(hostName)
	}

	if err != nil {
		log.Fatalf("Error checking SSH config: %v", err)
	}

	if !hostFound {
		fmt.Printf("Error: Host '%s' not found in SSH configuration.\n", hostName)
		fmt.Println("Use 'ctty' to see available hosts.")
		os.Exit(1)
	}

	historyManager, err := history.NewHistoryManager()
	if err != nil {
		fmt.Printf("Warning: Could not initialize connection history: %v\n", err)
	} else {
		err = historyManager.RecordConnection(hostName)
		if err != nil {
			fmt.Printf("Warning: Could not record connection history: %v\n", err)
		}
	}

	var args []string

	if configFile != "" {
		args = append(args, "-F", configFile)
	}

	if forceTTY {
		args = append(args, "-t")
	}

	args = append(args, hostName)

	if len(remoteCommand) > 0 {
		args = append(args, remoteCommand...)
	} else {
		fmt.Printf("Connecting to %s...\n", hostName)
	}

	env := os.Environ()
	if pass, ok := credential.GetPassword(hostName); ok && pass != "" {
		selfPath, err := os.Executable()
		if err == nil {
			env = append(env,
				"SSH_ASKPASS="+selfPath,
				"SSH_ASKPASS_REQUIRE=force",
				"CTTY_ASKPASS_TOKEN="+base64.StdEncoding.EncodeToString([]byte(pass)),
				"DISPLAY=ctty:0",
			)
		}
	}

	sshPath, lookErr := exec.LookPath("ssh")
	if lookErr == nil {
		argv := append([]string{"ssh"}, args...)
		// On Unix, Exec replaces the process and never returns on success.
		// On Windows, Exec is not supported and returns an error; fall through to the exec.Command fallback.
		_ = syscall.Exec(sshPath, argv, env)
	}

	// Fallback for Windows or if LookPath failed
	sshCmd := exec.Command("ssh", args...)
	sshCmd.Env = env
	sshCmd.Stdin = os.Stdin
	sshCmd.Stdout = os.Stdout
	sshCmd.Stderr = os.Stderr

	err = sshCmd.Run()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			if status, ok := exitError.Sys().(syscall.WaitStatus); ok {
				os.Exit(status.ExitStatus())
			}
		}
		fmt.Printf("Error executing SSH command: %v\n", err)
		os.Exit(1)
	}
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	// Handle OpenSSH AskPass callback
	if token := os.Getenv("CTTY_ASKPASS_TOKEN"); token != "" && os.Getenv("SSH_ASKPASS") != "" {
		plain, err := base64.StdEncoding.DecodeString(token)
		if err == nil {
			fmt.Print(string(plain))
			os.Exit(0)
		}
	}

	if err := RootCmd.Execute(); err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "unknown command") {
			parts := strings.Split(errStr, "\"")
			if len(parts) >= 2 {
				potentialHost := parts[1]
				connectToHost(potentialHost, nil)
				return
			}
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	RootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "", "SSH config file to use (default: ~/.ssh/config)")
	RootCmd.Flags().BoolVarP(&forceTTY, "tty", "t", false, "Force pseudo-TTY allocation (useful for interactive remote commands)")
	RootCmd.PersistentFlags().BoolVarP(&searchMode, "search", "s", false, "Focus on search input at startup")
	RootCmd.PersistentFlags().BoolVar(&noUpdateCheck, "no-update-check", false, "Disable automatic update check")
	RootCmd.PersistentFlags().StringVar(&langFlag, "lang", "", "Language for interface (auto, en, zh)")

	RootCmd.SetVersionTemplate("{{.Name}} version {{.Version}}\n")
}
