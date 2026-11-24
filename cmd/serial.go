package cmd

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/serialconfig"
	"github.com/zsuroy/ctty/internal/ui"

	"github.com/spf13/cobra"
)

var serialFormat string

// Direct-connect flags for 'ctty serial <name|device>'.
var (
	serialRaw     bool
	serialMonitor bool
	serialBaud    int
)

// serialCmd represents the serial command.
// No-args opens the TUI. Subcommands list|search|info support --format json
// for agents without breaking shell completions for the parent.
var serialCmd = &cobra.Command{
	Use:   "serial [list|search|info|<name|device>]",
	Short: "Open serial device manager or query saved devices",
	Long: `Open the serial device manager TUI, or query saved devices non-interactively.

Forms:
  ctty serial                                  Open TUI device manager
  ctty serial <name|device> [--raw|--monitor]   Connect straight away
  ctty serial list [--format json]              List saved devices
  ctty serial search [query] [--format json]
  ctty serial info <name> [--format json]

Direct connect resolves a saved device name first and otherwise treats the
argument as a device path (e.g. COM3, /dev/ttyUSB0), using --baud when given.
--raw keeps the plain terminal passthrough (Ctrl+] disconnects); --monitor
opens the assistant monitor. Without a flag the saved device mode wins, then
the "Serial connect mode" setting, then the standalone default (raw).

Auto-detected ports appear in the TUI only; CLI list/search/info read
~/.config/ctty/serial.json.`,
	Args: cobra.ArbitraryArgs,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) >= 1 {
			if args[0] == "info" && len(args) == 1 {
				return completeSerialNames(toComplete), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		base := []string{"list", "search", "info"}
		names := completeSerialNames(toComplete)
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
			if err := ui.RunSerialMode(AppVersion, noUpdateCheck); err != nil {
				log.Fatalf("Error running serial mode: %v", err)
			}
			fmt.Println()
			return
		}

		switch args[0] {
		case "list":
			runSerialList()
		case "search":
			q := ""
			if len(args) > 1 {
				q = strings.Join(args[1:], " ")
			}
			runSerialSearch(q)
		case "info":
			if len(args) < 2 {
				fmt.Fprintf(os.Stderr, "Error: serial info requires a device name\n")
				os.Exit(1)
			}
			runSerialInfo(args[1])
		default:
			runSerialDirect(args[0])
		}
	},
}

func completeSerialNames(toComplete string) []string {
	devices, err := serialconfig.Load()
	if err != nil {
		return nil
	}
	lower := strings.ToLower(toComplete)
	var out []string
	for _, d := range devices {
		if strings.HasPrefix(strings.ToLower(d.Name), lower) {
			out = append(out, d.Name)
		}
	}
	return out
}

// runSerialDirect connects to a saved device name or a raw device path
// without going through the device list — the CLI-friendly form
// 'ctty serial COM3 --raw'. Mode resolution matches the TUI: --raw /
// --monitor win, then the saved device mode, then the global setting,
// then the standalone default (raw).
func runSerialDirect(target string) {
	if serialRaw && serialMonitor {
		fmt.Fprintln(os.Stderr, "Error: --raw and --monitor are mutually exclusive")
		os.Exit(2)
	}

	dev, ok := serialconfig.Find(target)
	if !ok {
		dev = serialconfig.DefaultDevice()
		dev.Name = target
		dev.Device = target
	}
	if serialBaud > 0 {
		dev.BaudRate = serialBaud
	}

	globalMode := ""
	if cfg, err := config.LoadAppConfig(); err == nil && cfg != nil {
		globalMode = config.NormalizeSerialStartMode(cfg.SerialStartMode)
	}
	override := ""
	switch {
	case serialRaw:
		override = serialconfig.ModeRaw
	case serialMonitor:
		override = serialconfig.ModeAssistant
	}
	mode := serialconfig.ResolveMode(override, dev.Mode, globalMode, true)

	if mode == serialconfig.ModeRaw {
		// No TUI at all for the passthrough path: the terminal is already
		// in its normal state, so the bridge can own stdin/stdout directly.
		if err := serialconfig.NewExecCommand(dev).Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := ui.RunSerialConnect(dev, mode, AppVersion, noUpdateCheck); err != nil {
		log.Fatalf("Error running serial mode: %v", err)
	}
	fmt.Println()
}

func runSerialList() {
	devices, err := serialconfig.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	outputSerialDevices(devices)
}

func runSerialSearch(query string) {
	devices, err := serialconfig.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	query = strings.TrimSpace(query)
	if query == "" {
		outputSerialDevices(devices)
		return
	}
	words := strings.Fields(strings.ToLower(query))
	var matched []serialconfig.SerialDevice
	for _, d := range devices {
		hay := strings.ToLower(d.Name + " " + d.Device)
		ok := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			matched = append(matched, d)
		}
	}
	outputSerialDevices(matched)
}

func runSerialInfo(name string) {
	dev, ok := serialconfig.Find(name)
	if !ok {
		if serialFormat == "json" {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
				"ok": false, "error": "NOT_FOUND", "name": name,
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: serial device %q not found\n", name)
		}
		os.Exit(2)
	}
	if serialFormat == "json" {
		_ = json.NewEncoder(os.Stdout).Encode(dev)
		return
	}
	fmt.Printf("Name:         %s\n", dev.Name)
	fmt.Printf("Device:       %s\n", dev.Device)
	fmt.Printf("Baud Rate:    %d\n", dev.BaudRate)
	fmt.Printf("Data Bits:    %d\n", dev.DataBits)
	fmt.Printf("Parity:       %s\n", dev.Parity)
	fmt.Printf("Stop Bits:    %s\n", serialconfig.FormatStopBits(dev.StopBits))
	fmt.Printf("Flow Control: %s\n", dev.FlowControl)
}

func outputSerialDevices(devices []serialconfig.SerialDevice) {
	if serialFormat == "json" {
		if devices == nil {
			devices = []serialconfig.SerialDevice{}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(devices)
		return
	}
	if len(devices) == 0 {
		fmt.Println("No serial devices found.")
		return
	}
	for _, d := range devices {
		fmt.Printf("%-20s %s  %d %d%s%s %s\n",
			d.Name, d.Device, d.BaudRate, d.DataBits, parityShort(d.Parity), serialconfig.FormatStopBits(d.StopBits), d.FlowControl)
	}
}

func parityShort(p string) string {
	if p == "" {
		return "?"
	}
	return p[:1]
}

func init() {
	RootCmd.AddCommand(serialCmd)
	serialCmd.Flags().StringVar(&serialFormat, "format", "", "Output format: json")
	serialCmd.Flags().BoolVar(&serialRaw, "raw", false, "Connect with the plain terminal passthrough")
	serialCmd.Flags().BoolVar(&serialMonitor, "monitor", false, "Connect into the assistant monitor")
	serialCmd.Flags().IntVar(&serialBaud, "baud", 0, "Baud rate for a direct device path (default 115200)")
}
