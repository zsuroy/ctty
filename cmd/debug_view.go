package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// runDebugView renders a manager's list and tag picker drawer headlessly
// (no TUI loop, no network) and prints the raw frames. It exists to
// diagnose terminal-specific rendering issues: run it inside the suspect
// terminal and compare the frame text against a good terminal.
//
// For ssh the real SSH config is used (or --config). For the other
// managers a synthetic two-entry inventory is written into an isolated
// config dir so the dump is deterministic across machines and always has
// tags for the picker to show.
func runDebugView(out io.Writer, cfgFile string, width, height int, drawer string) error {
	switch drawer {
	case "", "ssh":
		return runDebugViewSSH(out, cfgFile, width, height)
	case "ftp", "s3", "webdav", "telnet":
		return runDebugViewManager(out, width, height, drawer)
	default:
		return fmt.Errorf("unknown --drawer %q (want ssh|ftp|s3|webdav|telnet)", drawer)
	}
}

func runDebugViewSSH(out io.Writer, cfgFile string, width, height int) error {
	var hosts []config.SSHHost
	var err error
	if cfgFile != "" {
		hosts, err = config.ParseSSHConfigFile(cfgFile)
	} else {
		hosts, err = config.ParseSSHConfig()
	}
	if err != nil {
		return err
	}

	m := ui.NewModel(hosts, cfgFile, false, AppVersion, true)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m = updated.(ui.Model)
	fmt.Fprintln(out, "===== LIST =====")
	dumpFrame(out, width, height, "LIST", m.View())

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	m = updated.(ui.Model)
	fmt.Fprintln(out, "===== TAG PICKER =====")
	dumpFrame(out, width, height, "TAG PICKER", m.View())
	return nil
}

// managerSeed is a minimal tagged inventory per manager, matching each
// package's on-disk JSON schema. Two entries per manager so the tag
// picker renders multiple rows and shared tags.
var managerSeed = map[string]string{
	"ftp": `{"sites":[
		{"name":"lab-nas","host":"10.0.0.9","port":21,"user":"admin","tags":["lab","backup"]},
		{"name":"ftp.scene.org","host":"ftp.scene.org","port":21,"user":"anonymous","tags":["scene"]}
	]}`,
	"s3": `{"sites":[
		{"name":"minio-lab","endpoint":"127.0.0.1:9000","bucket":"backups","region":"us-east-1","tags":["lab","backup"]},
		{"name":"r2-prod","endpoint":"abc.r2.cloudflarestorage.com","bucket":"prod","region":"auto","tags":["prod"]}
	]}`,
	"webdav": `{"sites":[
		{"name":"nextcloud","url":"https://dav.example.com","user":"admin","tags":["lab"]},
		{"name":"nas-dav","url":"http://10.0.0.5/dav","tags":["backup"]}
	]}`,
	"telnet": `{"hosts":[
		{"name":"core-switch","host":"10.0.0.254","port":2323,"tags":["lab","console"]},
		{"name":"console01","host":"192.168.1.1","port":23,"tags":["console"]}
	]}`,
}

func runDebugViewManager(out io.Writer, width, height int, manager string) error {
	dir, err := os.MkdirTemp("", "ctty-debug-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	cfgDir := filepath.Join(dir, "ctty")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(cfgDir, manager+".json"), []byte(managerSeed[manager]), 0o600); err != nil {
		return err
	}

	prevXDG, hadXDG := os.LookupEnv("XDG_CONFIG_HOME")
	if err := os.Setenv("XDG_CONFIG_HOME", dir); err != nil {
		return err
	}
	defer func() {
		if hadXDG {
			_ = os.Setenv("XDG_CONFIG_HOME", prevXDG)
		} else {
			_ = os.Unsetenv("XDG_CONFIG_HOME")
		}
	}()

	styles := ui.NewStyles(width)
	var form tea.Model
	switch manager {
	case "ftp":
		form = ui.NewFTPSitesForm(styles, width, height)
	case "s3":
		form = ui.NewS3SitesForm(styles, width, height)
	case "webdav":
		form = ui.NewWebDAVSitesForm(styles, width, height)
	case "telnet":
		form = ui.NewTelnetForm(styles, width, height)
	}

	updated, _ := form.Update(tea.WindowSizeMsg{Width: width, Height: height})
	form = updated
	label := strings.ToUpper(manager)
	fmt.Fprintf(out, "===== %s LIST =====\n", label)
	dumpFrame(out, width, height, label+" LIST", form.View())

	updated, _ = form.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	fmt.Fprintf(out, "===== %s TAG PICKER =====\n", label)
	dumpFrame(out, width, height, label+" TAG PICKER", updated.View())
	return nil
}

// dumpFrame prints the rendered frame followed by a self-measurement line so
// the dump can be judged inside any terminal.
func dumpFrame(out io.Writer, width, height int, label, view string) {
	frame := ui.RenderCanvas(width, height, view)
	fmt.Fprintln(out, frame)
	fmt.Fprintf(out, "REPORT %s: %s\n", label, ui.DebugRowReport(frame, width, height))
}

var (
	debugViewWidth  int
	debugViewHeight int
	debugViewDrawer string
)

var debugViewCmd = &cobra.Command{
	Use:           "debug-view",
	Short:         "Dump TUI frames as text (debugging)",
	Long:          "Render a manager's list and tag picker drawer headlessly and print the raw frames to stdout. Used to diagnose terminal-specific rendering issues: run it inside the suspect terminal and compare the frame text against a good terminal.",
	Hidden:        true,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDebugView(cmd.OutOrStdout(), configFile, debugViewWidth, debugViewHeight, debugViewDrawer)
	},
}

func init() {
	debugViewCmd.Flags().IntVar(&debugViewWidth, "width", 120, "Terminal width for the dumped frames")
	debugViewCmd.Flags().IntVar(&debugViewHeight, "height", 30, "Terminal height for the dumped frames")
	debugViewCmd.Flags().StringVar(&debugViewDrawer, "drawer", "ssh", "Manager frames to dump: ssh, ftp, s3, webdav, telnet")
	RootCmd.AddCommand(debugViewCmd)
}
