package telnetconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTelnetQuickCmdDefaults(t *testing.T) {
	cmds := DefaultTelnetQuickCmds()
	if len(cmds) != 8 {
		t.Fatalf("default quick cmds = %d, want 8", len(cmds))
	}
	// The stock slots are plain ASCII shell/network-gear commands.
	for i, c := range cmds {
		if c.Hex {
			t.Fatalf("slot %d (%s) should default to ASCII", i+1, c.Label)
		}
		if c.Value == "" {
			t.Fatalf("slot %d (%s) should carry a payload", i+1, c.Label)
		}
	}
	if cmds[0].Value != "ls -la" || cmds[0].Label != "ls -la" {
		t.Fatalf("slot 1 should be ls -la, got %+v", cmds[0])
	}
	if cmds[3].Value != "show version" {
		t.Fatalf("slot 4 should be show version, got %+v", cmds[3])
	}
}

func TestTelnetQuickCmdLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// Missing file returns defaults without creating anything.
	cmds, err := LoadTelnetQuickCmds()
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if len(cmds) != 8 {
		t.Fatalf("defaults = %d, want 8", len(cmds))
	}
	path := filepath.Join(dir, "ctty", "telnet-quick.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("missing quick file should not be created on load")
	}

	cmds[0] = TelnetQuickCmd{Label: "uname", Value: "uname -a", Hex: false}
	if err := SaveTelnetQuickCmds(cmds); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("quick file not created after save: %v", err)
	}

	got, err := LoadTelnetQuickCmds()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got[0].Label != "uname" || got[0].Value != "uname -a" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestTelnetQuickCmdEmptyFileFallsBack(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfgDir := filepath.Join(dir, "ctty")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "telnet-quick.json"), []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	cmds, err := LoadTelnetQuickCmds()
	if err != nil {
		t.Fatalf("load empty: %v", err)
	}
	if len(cmds) != 8 {
		t.Fatalf("empty file should yield defaults, got %d", len(cmds))
	}
}

func TestTelnetQuickCmdPayloadAndByteCount(t *testing.T) {
	cases := []struct {
		cmd  TelnetQuickCmd
		want []byte
	}{
		{TelnetQuickCmd{Value: "uname -a"}, []byte("uname -a")},
		{TelnetQuickCmd{Value: "01 03 00 00 84 0A", Hex: true}, []byte{1, 3, 0, 0, 0x84, 0x0A}},
		{TelnetQuickCmd{Value: "0x0103", Hex: true}, []byte{1, 3}},
		// Label-only slot: the add flow fills the label first, so the label
		// has to be what gets sent instead of an empty payload.
		{TelnetQuickCmd{Label: "show log"}, []byte("show log")},
		// An explicit value still wins over the label.
		{TelnetQuickCmd{Label: "v", Value: "show version"}, []byte("show version")},
	}
	for _, c := range cases {
		b, err := c.cmd.Payload()
		if err != nil || string(b) != string(c.want) {
			t.Fatalf("payload %+v = %v,%v want %v", c.cmd, b, err, c.want)
		}
		if n := c.cmd.ByteCount(); n != len(c.want) {
			t.Fatalf("byte count = %d, want %d", n, len(c.want))
		}
	}
	// Malformed hex is flagged as -1 for the editor.
	if n := (TelnetQuickCmd{Value: "zz", Hex: true}).ByteCount(); n != -1 {
		t.Fatalf("malformed hex byte count = %d, want -1", n)
	}
}

// A hex slot accepts the same spellings the send box does: a frame pasted
// from a datasheet should not need hand-editing to decode.
func TestTelnetQuickCmdHexAcceptsCommonSpellings(t *testing.T) {
	want := []byte{0x01, 0x03}
	for _, v := range []string{"01 03", "01,03", "0x01,0x03", "01-03", "01:03", `\x01\x03`, "01_03"} {
		got, err := (TelnetQuickCmd{Value: v, Hex: true}).Payload()
		if err != nil {
			t.Fatalf("Payload(%q) error: %v", v, err)
		}
		if string(got) != string(want) {
			t.Fatalf("Payload(%q) = % X, want % X", v, got, want)
		}
	}
}
