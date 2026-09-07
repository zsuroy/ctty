package cmd

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/zsuroy/ctty/internal/telnetclient"
	"github.com/zsuroy/ctty/internal/telnetconfig"
	"github.com/zsuroy/ctty/internal/ui"

	"github.com/spf13/cobra"
)

var telnetFormat string

// telnetCmd opens the telnet device manager, queries saved devices, or connects.
//
// Forms:
//
//	ctty telnet                         → saved-device manager TUI
//	ctty telnet list|search|info …      → non-interactive JSON/human query
//	ctty telnet <name>                  → connect to a saved device by name
//	ctty telnet <host[:port]>           → one-off direct connection
var telnetCmd = &cobra.Command{
	Use:   "telnet [list|search|info|host|name]",
	Short: "Open telnet device manager, query devices, or connect",
	Long: `Open the telnet device manager TUI, query saved devices, or connect.

Telnet targets are lab equipment, console servers, and legacy network gear.
Traffic (including passwords) is transmitted in cleartext — use SSH where possible.

Forms:
  ctty telnet                          List and manage saved telnet devices (TUI)
  ctty telnet list [--format json]     List saved devices
  ctty telnet search [query] [--format json]
  ctty telnet info <name> [--format json]
  ctty telnet core-sw                  Connect to a saved device by name
  ctty telnet 192.168.1.1              Connect to host on port 23
  ctty telnet 10.0.0.5:2001            Connect with an explicit port

Press Ctrl-] during a session to disconnect.`,
	Args: cobra.ArbitraryArgs,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) >= 1 {
			if args[0] == "info" && len(args) == 1 {
				return completeTelnetNames(toComplete), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		base := []string{"list", "search", "info"}
		names := completeTelnetNames(toComplete)
		var out []string
		lower := strings.ToLower(toComplete)
		for _, b := range base {
			if strings.HasPrefix(b, lower) {
				out = append(out, b)
			}
		}
		out = append(out, names...)
		return out, cobra.ShellCompDirectiveNoFileComp
	},
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			if err := ui.RunTelnetMode(AppVersion, noUpdateCheck); err != nil {
				log.Fatalf("Error running telnet mode: %v", err)
			}
			fmt.Println()
			return
		}

		switch args[0] {
		case "list":
			runTelnetList()
			return
		case "search":
			q := ""
			if len(args) > 1 {
				q = strings.Join(args[1:], " ")
			}
			runTelnetSearch(q)
			return
		case "info":
			if len(args) < 2 {
				fmt.Fprintf(os.Stderr, "Error: telnet info requires a device name\n")
				os.Exit(1)
			}
			runTelnetInfo(args[1])
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

func completeTelnetNames(toComplete string) []string {
	hosts, err := telnetconfig.Load()
	if err != nil {
		return nil
	}
	lower := strings.ToLower(toComplete)
	var out []string
	for _, h := range hosts {
		if strings.HasPrefix(strings.ToLower(h.Name), lower) {
			out = append(out, h.Name)
		}
	}
	return out
}

func runTelnetList() {
	hosts, err := telnetconfig.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	outputTelnetHosts(hosts)
}

func runTelnetSearch(query string) {
	hosts, err := telnetconfig.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	query = strings.TrimSpace(query)
	if query == "" {
		outputTelnetHosts(hosts)
		return
	}
	words := strings.Fields(strings.ToLower(query))
	var matched []telnetconfig.TelnetHost
	for _, h := range hosts {
		hay := strings.ToLower(h.Name + " " + h.Host + " " + strings.Join(h.Tags, " "))
		ok := true
		for _, w := range words {
			w = strings.TrimPrefix(w, "#")
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			matched = append(matched, h)
		}
	}
	outputTelnetHosts(matched)
}

func runTelnetInfo(name string) {
	dev, ok := telnetconfig.Find(name)
	if !ok {
		if telnetFormat == "json" {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
				"ok": false, "error": "NOT_FOUND", "name": name,
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: telnet host %q not found\n", name)
		}
		os.Exit(2)
	}
	if telnetFormat == "json" {
		_ = json.NewEncoder(os.Stdout).Encode(dev)
		return
	}
	fmt.Printf("Name: %s\n", dev.Name)
	fmt.Printf("Host: %s\n", dev.Host)
	fmt.Printf("Port: %d\n", dev.Port)
	if len(dev.Tags) > 0 {
		fmt.Printf("Tags: %s\n", strings.Join(dev.Tags, ", "))
	}
}

func outputTelnetHosts(hosts []telnetconfig.TelnetHost) {
	if telnetFormat == "json" {
		if hosts == nil {
			hosts = []telnetconfig.TelnetHost{}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(hosts)
		return
	}
	if len(hosts) == 0 {
		fmt.Println("No telnet hosts found.")
		return
	}
	for _, h := range hosts {
		tags := ""
		if len(h.Tags) > 0 {
			tags = " [" + strings.Join(h.Tags, ", ") + "]"
		}
		fmt.Printf("%-20s %s:%d%s\n", h.Name, h.Host, h.Port, tags)
	}
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
	telnetCmd.Flags().StringVar(&telnetFormat, "format", "", "Output format: json (for list/search/info)")
}
