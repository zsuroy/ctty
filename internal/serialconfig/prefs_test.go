package serialconfig

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSerialPrefsDefaults(t *testing.T) {
	p := DefaultSerialPrefs()
	if p.HexView || p.HexSend || p.ShowTime {
		t.Fatalf("defaults should have all toggles off: %+v", p)
	}
	if p.IntervalMs != DefaultIntervalMs || p.MaxLines != DefaultMaxLines {
		t.Fatalf("default numbers = %d/%d, want %d/%d", p.IntervalMs, p.MaxLines, DefaultIntervalMs, DefaultMaxLines)
	}
	if p.Interval() != time.Second {
		t.Fatalf("default interval = %v, want 1s", p.Interval())
	}
	if p.PageKey != "alt" {
		t.Fatalf("default page key = %q, want alt", p.PageKey)
	}
}

func TestSerialPrefsPageKeyNormalize(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", "alt"},
		{"ctrl", "ctrl"},
		{"alt", "alt"},
		{"garbage", "alt"},
	} {
		got := (SerialPrefs{PageKey: tc.in}).Normalize().PageKey
		if got != tc.want {
			t.Errorf("normalize page key %q = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSerialPrefsNormalize(t *testing.T) {
	cases := []struct {
		in        SerialPrefs
		wantMs    int
		wantLines int
	}{
		{SerialPrefs{IntervalMs: 0, MaxLines: 0}, DefaultIntervalMs, DefaultMaxLines},
		{SerialPrefs{IntervalMs: 50, MaxLines: 10}, minIntervalMs, minMaxLines},
		{SerialPrefs{IntervalMs: 99999, MaxLines: 999999}, maxIntervalMs, maxMaxLines},
		{SerialPrefs{IntervalMs: 500, MaxLines: 3000}, 500, 3000},
	}
	for _, c := range cases {
		got := c.in.Normalize()
		if got.IntervalMs != c.wantMs || got.MaxLines != c.wantLines {
			t.Errorf("normalize(%+v) = %d/%d, want %d/%d", c.in, got.IntervalMs, got.MaxLines, c.wantMs, c.wantLines)
		}
	}
}

func TestSerialPrefsLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// Missing file returns defaults without creating anything.
	p, err := LoadSerialPrefs()
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if p != DefaultSerialPrefs() {
		t.Fatalf("missing file should yield defaults, got %+v", p)
	}
	path := filepath.Join(dir, "ctty", "serial-prefs.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("missing prefs file should not be created on load")
	}

	p.HexView = true
	p.HexSend = true
	p.ShowTime = true
	p.IntervalMs = 500
	p.MaxLines = 4000
	if err := SaveSerialPrefs(p); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("prefs file not created after save: %v", err)
	}

	got, err := LoadSerialPrefs()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got != p {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, p)
	}
}

func TestSerialPrefsMalformedFallsBack(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "ctty", "serial-prefs.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := LoadSerialPrefs()
	if err == nil {
		t.Fatal("malformed file should report an error")
	}
	if p != DefaultSerialPrefs() {
		t.Fatalf("malformed file should fall back to defaults, got %+v", p)
	}
}

func TestSerialPrefsSaveNormalizesOnDisk(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	// Save an out-of-range value; it must be clamped before writing.
	if err := SaveSerialPrefs(SerialPrefs{IntervalMs: 5, MaxLines: 999999}); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadSerialPrefs()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.IntervalMs != minIntervalMs || got.MaxLines != maxMaxLines {
		t.Fatalf("saved values not normalized: %+v", got)
	}
}

func TestSerialPrefsCrlfDefaultAndLegacy(t *testing.T) {
	// Default is ON (AT modules need CRLF to respond).
	if !DefaultSerialPrefs().Crlf {
		t.Fatal("CRLF should default to on")
	}

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	// Simulate a pre-CRLF config file that has no "crlf" key.
	cfgDir := filepath.Join(dir, "ctty")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"hex_view":true,"hex_send":false,"show_time":true,"interval_ms":500,"max_lines":4000}`
	if err := os.WriteFile(filepath.Join(cfgDir, "serial-prefs.json"), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	p, err := LoadSerialPrefs()
	if err != nil {
		t.Fatalf("load legacy: %v", err)
	}
	if !p.Crlf {
		t.Fatal("legacy config missing crlf must fall back to default true")
	}
	if !p.HexView || !p.ShowTime || p.IntervalMs != 500 || p.MaxLines != 4000 {
		t.Fatalf("legacy values lost: %+v", p)
	}

	// An explicit false must be honored (not overridden by the fallback).
	off := `{"crlf":false}`
	if err := os.WriteFile(filepath.Join(cfgDir, "serial-prefs.json"), []byte(off), 0644); err != nil {
		t.Fatal(err)
	}
	p, err = LoadSerialPrefs()
	if err != nil || p.Crlf {
		t.Fatalf("explicit crlf:false not honored: %+v err=%v", p, err)
	}
}
