package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/zsuroy/ctty/internal/config"
)

func TestAddEditNonInteractiveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	if err := os.WriteFile(cfgPath, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}

	// Isolate credential/backup dirs
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	origConfig := configFile
	configFile = cfgPath
	defer func() { configFile = origConfig }()

	resetHostFlags()
	t.Cleanup(resetHostFlags)

	addCmd.SetArgs([]string{
		"--name", "web1",
		"--hostname", "10.0.0.1",
		"--user", "deploy",
		"--port", "2222",
		"--tags", "prod,web",
		"--option", "Compression=yes",
		"--format", "json",
	})
	buf := new(bytes.Buffer)
	addCmd.SetOut(buf)
	addCmd.SetErr(buf)

	// Execute via Run to avoid root PersistentPreRun side effects issues
	hostFlagName = "web1"
	hostFlagHostname = "10.0.0.1"
	hostFlagUser = "deploy"
	hostFlagPort = "2222"
	hostFlagTags = "prod,web"
	hostFlagOptions = []string{"Compression=yes"}
	hostFlagFormat = "json"
	hostFlagForce = false
	hostFlagNonInteractive = true

	addHostNonInteractive(addCmd, "")

	hosts, err := config.ParseSSHConfigFile(cfgPath)
	if err != nil {
		t.Fatalf("parse after add: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	h := hosts[0]
	if h.Name != "web1" || h.Hostname != "10.0.0.1" || h.User != "deploy" || h.Port != "2222" {
		t.Fatalf("unexpected host after add: %+v", h)
	}
	if len(h.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %v", h.Tags)
	}
	if !strings.Contains(h.Options, "Compression") {
		t.Fatalf("expected Compression option, got %q", h.Options)
	}

	// Edit: change hostname and port
	resetHostFlags()
	t.Cleanup(resetHostFlags)
	hostFlagHostname = "10.0.0.2"
	hostFlagPort = "22"
	hostFlagFormat = "json"
	hostFlagNonInteractive = true
	// Mark flags as changed by parsing
	if err := editCmd.Flags().Set("hostname", "10.0.0.2"); err != nil {
		t.Fatal(err)
	}
	if err := editCmd.Flags().Set("port", "22"); err != nil {
		t.Fatal(err)
	}
	if err := editCmd.Flags().Set("format", "json"); err != nil {
		t.Fatal(err)
	}
	if err := editCmd.Flags().Set("non-interactive", "true"); err != nil {
		t.Fatal(err)
	}

	// Capture stdout for JSON
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	editHostNonInteractive(editCmd, "web1")
	_ = w.Close()
	os.Stdout = oldStdout
	var outBuf bytes.Buffer
	_, _ = outBuf.ReadFrom(r)

	hosts, err = config.ParseSSHConfigFile(cfgPath)
	if err != nil {
		t.Fatalf("parse after edit: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host after edit, got %d", len(hosts))
	}
	h = hosts[0]
	if h.Hostname != "10.0.0.2" {
		t.Fatalf("hostname not updated: %s", h.Hostname)
	}
	if h.Port != "22" && h.Port != "" {
		// Port 22 may be omitted from file on write; accept either
		t.Fatalf("unexpected port: %s", h.Port)
	}

	var res hostCLIResult
	if err := json.Unmarshal(outBuf.Bytes(), &res); err != nil {
		t.Fatalf("json result: %v\nraw: %s", err, outBuf.String())
	}
	if !res.OK || res.Action != "updated" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestAddForceOverwrite(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	if err := os.WriteFile(cfgPath, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	origConfig := configFile
	configFile = cfgPath
	defer func() { configFile = origConfig }()

	resetHostFlags()
	t.Cleanup(resetHostFlags)
	hostFlagName = "dup"
	hostFlagHostname = "1.1.1.1"
	hostFlagNonInteractive = true
	hostFlagForce = false
	addHostNonInteractive(addCmd, "")

	// Second add without force should fail — capture via checking exists path
	resetHostFlags()
	t.Cleanup(resetHostFlags)
	hostFlagName = "dup"
	hostFlagHostname = "2.2.2.2"
	hostFlagNonInteractive = true
	hostFlagForce = false
	hostFlagFormat = "json"

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stdout = w
	os.Stderr = w

	// writeHostCLIResult calls os.Exit on error — so call underlying pieces
	exists, err := config.HostExistsInFile("dup", cfgPath)
	if err != nil || !exists {
		t.Fatalf("expected host to exist: exists=%v err=%v", exists, err)
	}

	_ = w.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	_, _ = r.Read(make([]byte, 1))

	// With force
	resetHostFlags()
	t.Cleanup(resetHostFlags)
	hostFlagName = "dup"
	hostFlagHostname = "2.2.2.2"
	hostFlagNonInteractive = true
	hostFlagForce = true
	hostFlagFormat = "json"

	r2, w2, _ := os.Pipe()
	os.Stdout = w2
	addHostNonInteractive(addCmd, "")
	_ = w2.Close()
	os.Stdout = oldStdout
	var outBuf bytes.Buffer
	_, _ = outBuf.ReadFrom(r2)

	hosts, err := config.ParseSSHConfigFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range hosts {
		if h.Name == "dup" && h.Hostname == "2.2.2.2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("force overwrite failed; hosts=%v out=%s", hosts, outBuf.String())
	}
}

func TestWantNonInteractive(t *testing.T) {
	resetHostFlags()
	t.Cleanup(resetHostFlags)
	if wantNonInteractive(addCmd) {
		t.Fatal("expected interactive by default")
	}
	if err := addCmd.Flags().Set("hostname", "x"); err != nil {
		t.Fatal(err)
	}
	if !wantNonInteractive(addCmd) {
		t.Fatal("expected non-interactive when --hostname set")
	}
	_ = addCmd.Flags().Set("hostname", "")
	// Changed stays true even if emptied — reset by creating fresh check with non-interactive
	resetHostFlags()
	t.Cleanup(resetHostFlags)
	hostFlagNonInteractive = true
	if !wantNonInteractive(addCmd) {
		// hostFlagNonInteractive alone should trigger even if flag.Changed is false
		// wantNonInteractive checks hostFlagNonInteractive first
	}
	if !hostFlagNonInteractive {
		t.Fatal("flag not set")
	}
	if !wantNonInteractive(addCmd) {
		t.Fatal("expected non-interactive when flag var set")
	}
}

func resetHostFlags() {
	hostFlagName = ""
	hostFlagHostname = ""
	hostFlagUser = ""
	hostFlagPort = ""
	hostFlagIdentityFile = ""
	hostFlagProxyJump = ""
	hostFlagProxyCommand = ""
	hostFlagOptions = nil
	hostFlagTags = ""
	hostFlagPassword = ""
	hostFlagFormat = ""
	hostFlagForce = false
	hostFlagNonInteractive = false
	addCmd.SetArgs(nil)
	editCmd.SetArgs(nil)
	addCmd.SetOut(nil)
	addCmd.SetErr(nil)
	editCmd.SetOut(nil)
	editCmd.SetErr(nil)
	// Reset Changed state on shared flags
	for _, c := range []*cobra.Command{addCmd, editCmd} {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			f.Changed = false
			_ = f.Value.Set(f.DefValue)
		})
	}
}
