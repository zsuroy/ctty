package cmd

import (
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"

	"github.com/zsuroy/ctty/internal/telnetclient"
	"github.com/zsuroy/ctty/internal/telnetconfig"
	"github.com/zsuroy/ctty/internal/ui"

	"github.com/spf13/cobra"
)

// telnetCmd opens the telnet device manager or connects directly.
//
// Forms:
//
//	ctty telnet                 → saved-device manager TUI
//	ctty telnet <name>          → connect to a saved device by name
//	ctty telnet <host[:port]>   → one-off direct connection
var telnetCmd = &cobra.Command{
	Use:   "telnet [host|name]",
	Short: "Open telnet device manager or connect to a telnet host",
	Long: `Open the telnet device manager TUI directly, or connect to a telnet endpoint.

Telnet targets are lab equipment, console servers, and legacy network gear.
Traffic (including passwords) is transmitted in cleartext — use SSH where possible.

Forms:
  ctty telnet                  List and manage saved telnet devices
  ctty telnet core-sw          Connect to a saved device by name
  ctty telnet 192.168.1.1      Connect to host on port 23
  ctty telnet 10.0.0.5:2001    Connect with an explicit port

Press Ctrl-] during a session to disconnect.`,
	Args: cobra.MaximumNArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		hosts, err := telnetconfig.Load()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		completions := make([]string, 0, len(hosts))
		toCompleteLower := strings.ToLower(toComplete)
		for _, h := range hosts {
			if strings.HasPrefix(strings.ToLower(h.Name), toCompleteLower) {
				completions = append(completions, h.Name)
			}
		}
		return completions, cobra.ShellCompDirectiveNoFileComp
	},
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			if err := ui.RunTelnetMode(AppVersion, noUpdateCheck); err != nil {
				log.Fatalf("Error running telnet mode: %v", err)
			}
			fmt.Println()
			return
		}

		target := args[0]

		// Saved-name match wins over raw address interpretation.
		if dev, ok := telnetconfig.Find(target); ok {
			connectTelnet(dev.Host, dev.Port)
			return
		}
		host, port, err := telnetclient.ParseHostPort(target, telnetconfig.DefaultPort)
		if err != nil {
			log.Fatalf("Error: %v", err)
		}
		connectTelnet(host, port)
	},
}

// connectTelnet dials and runs the interactive bridge until disconnect.
func connectTelnet(host string, port int) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := telnetclient.Dial(addr)
	if err != nil {
		log.Fatalf("Error connecting to %s: %v", addr, err)
	}
	defer conn.Close()
	fmt.Printf("Connected to %s. Press Ctrl-] to disconnect.\n", addr)
	bridge := telnetclient.NewBridge(conn)
	if err := bridge.Run(nil, nil, nil); err != nil {
		log.Fatalf("Session error: %v", err)
	}
}

func init() {
	RootCmd.AddCommand(telnetCmd)
}
