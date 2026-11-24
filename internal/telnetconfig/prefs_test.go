package telnetconfig

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTelnetPrefsDefaults(t *testing.T) {
	p := DefaultTelnetPrefs()
	if p.HexView || p.ShowPrefix || p.HexSend || p.ShowTime {
		t.Fatalf("defaults should have other toggles off: %+v", p)
	}
	if !p.AnsiColor {
		t.Fatalf("ANSI colour rendering should default on: %+v", p)
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
	// CRLF is the RFC 854 NVT end-of-line, so it is the default send suffix.
	if !p.Crlf || p.LineEnding != "crlf" {
		t.Fatalf("default line ending = %+v, want crlf", p)
	}
}

func TestTelnetPrefsPageKeyNormalize(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", "alt"},
		{"ctrl", "ctrl"},
		{"alt", "alt"},
		{"garbage", "alt"},
	} {
		got := (TelnetPrefs{PageKey: tc.in}).Normalize().PageKey
		if got != tc.want {
			t.Errorf("normalize page key %q = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTelnetPrefsNormalize(t *testing.T) {
	cases := []struct {
		in        TelnetPrefs
		wantMs    int
		wantLines int
	}{
		{TelnetPrefs{IntervalMs: 0, MaxLines: 0}, DefaultIntervalMs, DefaultMaxLines},
		{TelnetPrefs{IntervalMs: 50, MaxLines: 10}, minIntervalMs, minMaxLines},
		{TelnetPrefs{IntervalMs: 99999, MaxLines: 999999}, maxIntervalMs, maxMaxLines},
		{TelnetPrefs{IntervalMs: 500, MaxLines: 3000}, 500, 3000},
	}
	for _, c := range cases {
		got := c.in.Normalize()
		if got.IntervalMs != c.wantMs || got.MaxLines != c.wantLines {
			t.Errorf("normalize(%+v) = %d/%d, want %d/%d", c.in, got.IntervalMs, got.MaxLines, c.wantMs, c.wantLines)
		}
	}
}

func TestTelnetPrefsLineEndingNormalize(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		// A zero-value struct has Crlf=false, so the legacy mirror maps an
		// empty ending to "none"; the defaults keep CRLF.
		{"", "none"},
		{"crlf", "crlf"},
		{"cr", "cr"},
		{"lf", "lf"},
		{"lfcr", "lfcr"},
		{"none", "none"},
		{"nonsense", "crlf"},
	} {
		got := (TelnetPrefs{LineEnding: tc.in}).Normalize().LineEnding
		if got != tc.want {
			t.Errorf("normalize line ending %q = %q, want %q", tc.in, got, tc.want)
		}
	}
	// The legacy boolean mirrors the ending.
	if p := (TelnetPrefs{LineEnding: "none"}).Normalize(); p.Crlf {
		t.Errorf("line ending none should clear the legacy crlf flag")
	}
}

func TestTelnetPrefsLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// Missing file returns defaults without creating anything.
	p, err := LoadTelnetPrefs()
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if p != DefaultTelnetPrefs() {
		t.Fatalf("missing file should yield defaults, got %+v", p)
	}
	path := filepath.Join(dir, "ctty", "telnet-prefs.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("missing prefs file should not be created on load")
	}

	p.HexView = true
	p.HexSend = true
	p.ShowTime = true
	p.IntervalMs = 500
	p.MaxLines = 4000
	if err := SaveTelnetPrefs(p); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("prefs file not created after save: %v", err)
	}

	got, err := LoadTelnetPrefs()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got != p {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, p)
	}
}

func TestTelnetPrefsMalformedFallsBack(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "ctty", "telnet-prefs.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := LoadTelnetPrefs()
	if err == nil {
		t.Fatal("malformed file should report an error")
	}
	if p != DefaultTelnetPrefs() {
		t.Fatalf("malformed file should fall back to defaults, got %+v", p)
	}
}

func TestTelnetPrefsSaveNormalizesOnDisk(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	// Save an out-of-range value; it must be clamped before writing.
	if err := SaveTelnetPrefs(TelnetPrefs{IntervalMs: 5, MaxLines: 999999}); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadTelnetPrefs()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.IntervalMs != minIntervalMs || got.MaxLines != maxMaxLines {
		t.Fatalf("saved values not normalized: %+v", got)
	}
}

func TestTelnetPrefsCrlfDefaultAndLegacy(t *testing.T) {
	// Default is ON (RFC 854 NVT lines end with CR-LF).
	if !DefaultTelnetPrefs().Crlf {
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
	if err := os.WriteFile(filepath.Join(cfgDir, "telnet-prefs.json"), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	p, err := LoadTelnetPrefs()
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
	if err := os.WriteFile(filepath.Join(cfgDir, "telnet-prefs.json"), []byte(off), 0644); err != nil {
		t.Fatal(err)
	}
	p, err = LoadTelnetPrefs()
	if err != nil || p.Crlf {
		t.Fatalf("explicit crlf:false not honored: %+v err=%v", p, err)
	}
}

func TestTelnetPrefsDisplayRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	p := DefaultTelnetPrefs()
	if p.HexView || p.ShowPrefix {
		t.Fatalf("display defaults = hex:%v prefix:%v, want false/false", p.HexView, p.ShowPrefix)
	}
	p.HexView = true
	p.ShowPrefix = true
	if err := SaveTelnetPrefs(p); err != nil {
		t.Fatal(err)
	}
	got, err := LoadTelnetPrefs()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !got.HexView || !got.ShowPrefix {
		t.Fatalf("display prefs lost: %+v", got)
	}
	// An old file without show_prefix must keep the off default.
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfgDir := filepath.Join(dir, "ctty")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "telnet-prefs.json"), []byte(`{"hex_view":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	old, err := LoadTelnetPrefs()
	if err != nil || !old.HexView || old.ShowPrefix {
		t.Fatalf("legacy display prefs = %+v err=%v", old, err)
	}
}

func TestTelnetPrefsQuickLabelRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	p := DefaultTelnetPrefs()
	if !p.ShowQuickLabel {
		t.Fatal("telnet has always echoed quick-command labels; default must be on")
	}
	// Explicit off must survive a save/load round trip.
	p.ShowQuickLabel = false
	if err := SaveTelnetPrefs(p); err != nil {
		t.Fatal(err)
	}
	got, err := LoadTelnetPrefs()
	if err != nil || got.ShowQuickLabel {
		t.Fatalf("show_quick_label = %v err=%v, want false", got.ShowQuickLabel, err)
	}
	// A config written before the key existed keeps the historical on default.
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfgDir := filepath.Join(dir, "ctty")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "telnet-prefs.json"), []byte(`{"hex_view":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	old, err := LoadTelnetPrefs()
	if err != nil || !old.ShowQuickLabel {
		t.Fatalf("legacy prefs = %+v err=%v, want quick label on", old, err)
	}
}

func TestTelnetPrefsQuickKeyDefaultsAndNormalize(t *testing.T) {
	if p := DefaultTelnetPrefs(); p.QuickKey != QuickKeyAuto {
		t.Fatalf("default quick key = %q, want auto", p.QuickKey)
	}
	for _, tc := range []struct{ in, want string }{
		{"", QuickKeyAuto},
		{"auto", QuickKeyAuto},
		{"alt", QuickKeyAlt},
		{"option", QuickKeyOption},
		{"garbage", QuickKeyAuto},
	} {
		got := (TelnetPrefs{QuickKey: tc.in}).Normalize().QuickKey
		if got != tc.want {
			t.Errorf("normalize quick key %q = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDefaultQuickKeyForOS(t *testing.T) {
	if got := DefaultQuickKeyForOS("darwin"); got != QuickKeyOption {
		t.Errorf("darwin default = %q, want option", got)
	}
	for _, goos := range []string{"windows", "linux", "freebsd"} {
		if got := DefaultQuickKeyForOS(goos); got != QuickKeyAlt {
			t.Errorf("%s default = %q, want alt", goos, got)
		}
	}
}

func TestResolveQuickKey(t *testing.T) {
	// An explicit choice wins on every platform.
	for _, goos := range []string{"darwin", "windows", "linux"} {
		if got := ResolveQuickKey(QuickKeyAlt, goos); got != QuickKeyAlt {
			t.Errorf("resolve alt on %s = %q", goos, got)
		}
		if got := ResolveQuickKey(QuickKeyOption, goos); got != QuickKeyOption {
			t.Errorf("resolve option on %s = %q", goos, got)
		}
	}
	// auto — and empty/unknown values that fall back to it — follow the OS.
	if got := ResolveQuickKey(QuickKeyAuto, "darwin"); got != QuickKeyOption {
		t.Errorf("resolve auto on darwin = %q, want option", got)
	}
	if got := ResolveQuickKey(QuickKeyAuto, "windows"); got != QuickKeyAlt {
		t.Errorf("resolve auto on windows = %q, want alt", got)
	}
	if got := ResolveQuickKey("", "darwin"); got != QuickKeyOption {
		t.Errorf("resolve empty on darwin = %q, want option", got)
	}
	if got := ResolveQuickKey("garbage", "linux"); got != QuickKeyAlt {
		t.Errorf("resolve garbage on linux = %q, want alt", got)
	}
}

func TestTelnetPrefsAnsiColorDefaultsOnForOldConfigs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "ctty", "telnet-prefs.json")

	// A file written before ansi_color existed must keep the true default.
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"hex_view":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	p, err := LoadTelnetPrefs()
	if err != nil || !p.AnsiColor {
		t.Fatalf("legacy config ansiColor = %v err=%v, want true", p.AnsiColor, err)
	}

	// An explicit false must be honoured.
	if err := os.WriteFile(path, []byte(`{"ansi_color":false}`), 0644); err != nil {
		t.Fatal(err)
	}
	p, err = LoadTelnetPrefs()
	if err != nil || p.AnsiColor {
		t.Fatalf("explicit ansi_color:false = %v err=%v, want false", p.AnsiColor, err)
	}
}
