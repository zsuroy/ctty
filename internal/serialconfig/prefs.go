package serialconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SerialPrefs are the persistent preferences for the in-app serial assistant.
// They are loaded when the assistant opens and saved from the F10 settings
// page; the CLI pass-through path never reads them.
type SerialPrefs struct {
	HexView        bool   `json:"hex_view"`         // receive pane renders hex by default
	HexSend        bool   `json:"hex_send"`         // send box parses hex by default
	ShowTime       bool   `json:"show_time"`        // timestamp receive lines by default
	Crlf           bool   `json:"crlf"`             // legacy: true means line_ending=crlf
	LineEnding     string `json:"line_ending"`      // "crlf", "cr", "lf", "lfcr" or "none"
	HexAscii       bool   `json:"hex_ascii"`        // HEX+ASCII aligned view
	LogDir         string `json:"log_dir"`          // empty = user home
	LogAppend      bool   `json:"log_append"`       // append to an existing log file
	PageKey        string `json:"page_key"`         // "alt" or "ctrl": modifier for quick-bar paging
	QuickKey       string `json:"quick_key"`        // "auto" / "alt" / "option": modifier for Alt/Option+1..8
	ShowPrefix     bool   `json:"show_prefix"`      // paint "RX < " / "TX > " on monitor rows; on by default
	ShowQuickLabel bool   `json:"show_quick_label"` // paint "[label] " on quick-command TX echoes; on by default
	IntervalMs     int    `json:"interval_ms"`      // default loop-send interval
	MaxLines       int    `json:"max_lines"`        // receive buffer line cap
}

const (
	DefaultIntervalMs = 1000
	DefaultMaxLines   = 2000
	minIntervalMs     = 100
	maxIntervalMs     = 10000
	minMaxLines       = 100
	maxMaxLines       = 50000
)

const (
	QuickKeyAuto   = "auto"
	QuickKeyAlt    = "alt"
	QuickKeyOption = "option"
)

// DefaultQuickKeyForOS returns the quick-command modifier that works without
// extra terminal setup on a platform. Windows ConHost and Linux terminals send
// a real Alt+digit (ESC prefix), so "alt" is right there. macOS Option is a
// character key by default — ⌥1 emits "¡", not ESC+1 — so Macs start in the
// "option" mode that recognises those glyphs too.
func DefaultQuickKeyForOS(goos string) string {
	if goos == "darwin" {
		return QuickKeyOption
	}
	return QuickKeyAlt
}

// ResolveQuickKey turns a stored preference into the effective modifier.
// Unknown and empty values fall back to the platform default, so an old
// config file (written before this field existed) just works.
func ResolveQuickKey(v, goos string) string {
	switch v {
	case QuickKeyAlt, QuickKeyOption:
		return v
	default:
		return DefaultQuickKeyForOS(goos)
	}
}

// DefaultSerialPrefs returns the built-in preferences, matching the
// assistant's historical behaviour when no settings file exists.
func DefaultSerialPrefs() SerialPrefs {
	return SerialPrefs{
		HexView:        false,
		HexSend:        false,
		ShowTime:       false,
		Crlf:           true,
		LineEnding:     "crlf",
		PageKey:        "alt",
		QuickKey:       QuickKeyAuto,
		ShowPrefix:     true,
		ShowQuickLabel: true,
		IntervalMs:     DefaultIntervalMs,
		MaxLines:       DefaultMaxLines,
	}
}

func getSerialPrefsPath() (string, error) {
	dir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "serial-prefs.json"), nil
}

// Normalize clamps numeric fields to their supported ranges and fills in
// defaults for zero/unset values, so a hand-edited or stale file can never
// produce a zero-length interval or an unbounded receive buffer.
func (p SerialPrefs) Normalize() SerialPrefs {
	d := DefaultSerialPrefs()
	if p.LineEnding == "" {
		p.LineEnding = d.LineEnding
		if !p.Crlf {
			p.LineEnding = "none"
		}
	}
	switch p.LineEnding {
	case "crlf", "cr", "lf", "lfcr", "none":
	default:
		p.LineEnding = d.LineEnding
	}
	p.Crlf = p.LineEnding == "crlf"
	// A missing or invalid page key falls back to Alt, the legacy default.
	if p.PageKey != "alt" && p.PageKey != "ctrl" {
		p.PageKey = d.PageKey
	}
	if p.QuickKey != QuickKeyAuto && p.QuickKey != QuickKeyAlt && p.QuickKey != QuickKeyOption {
		p.QuickKey = d.QuickKey
	}
	if p.IntervalMs <= 0 {
		p.IntervalMs = d.IntervalMs
	}
	if p.IntervalMs < minIntervalMs {
		p.IntervalMs = minIntervalMs
	}
	if p.IntervalMs > maxIntervalMs {
		p.IntervalMs = maxIntervalMs
	}
	if p.MaxLines <= 0 {
		p.MaxLines = d.MaxLines
	}
	if p.MaxLines < minMaxLines {
		p.MaxLines = minMaxLines
	}
	if p.MaxLines > maxMaxLines {
		p.MaxLines = maxMaxLines
	}
	return p
}

func (p SerialPrefs) Interval() time.Duration {
	return time.Duration(p.IntervalMs) * time.Millisecond
}

// LoadSerialPrefs reads the settings file. A missing file yields defaults
// without creating anything; a malformed file is reported so the assistant
// can fall back to defaults and surface the error.
func LoadSerialPrefs() (SerialPrefs, error) {
	path, err := getSerialPrefsPath()
	if err != nil {
		return DefaultSerialPrefs(), err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultSerialPrefs(), nil
	}
	if err != nil {
		return DefaultSerialPrefs(), err
	}
	var p SerialPrefs
	// Unmarshal through a shadow type whose Crlf is a pointer so an older
	// config file (written before this field existed) keeps the true default
	// instead of silently decoding the missing key as false.
	var shadow struct {
		HexView    bool   `json:"hex_view"`
		HexSend    bool   `json:"hex_send"`
		ShowTime   bool   `json:"show_time"`
		Crlf       *bool  `json:"crlf"`
		LineEnding string `json:"line_ending"`
		HexAscii   bool   `json:"hex_ascii"`
		LogDir     string `json:"log_dir"`
		LogAppend  bool   `json:"log_append"`
		PageKey    string `json:"page_key"`
		QuickKey   string `json:"quick_key"`
		// Pointers keep the true defaults when an older file has no key:
		// serial has always painted RX/TX prefixes and quick-command labels.
		ShowPrefix     *bool `json:"show_prefix"`
		ShowQuickLabel *bool `json:"show_quick_label"`
		IntervalMs     int   `json:"interval_ms"`
		MaxLines       int   `json:"max_lines"`
	}
	if err := json.Unmarshal(data, &shadow); err != nil {
		return DefaultSerialPrefs(), fmt.Errorf("parsing serial preferences: %w", err)
	}
	p.HexView, p.HexSend, p.ShowTime = shadow.HexView, shadow.HexSend, shadow.ShowTime
	p.IntervalMs, p.MaxLines = shadow.IntervalMs, shadow.MaxLines
	p.Crlf = DefaultSerialPrefs().Crlf
	if shadow.Crlf != nil {
		p.Crlf = *shadow.Crlf
	}
	p.LineEnding = shadow.LineEnding
	p.HexAscii = shadow.HexAscii
	p.LogDir = shadow.LogDir
	p.LogAppend = shadow.LogAppend
	p.PageKey = shadow.PageKey
	p.QuickKey = shadow.QuickKey
	p.ShowPrefix = DefaultSerialPrefs().ShowPrefix
	if shadow.ShowPrefix != nil {
		p.ShowPrefix = *shadow.ShowPrefix
	}
	p.ShowQuickLabel = DefaultSerialPrefs().ShowQuickLabel
	if shadow.ShowQuickLabel != nil {
		p.ShowQuickLabel = *shadow.ShowQuickLabel
	}
	return p.Normalize(), nil
}

// SaveSerialPrefs writes the settings file, creating the config dir if needed.
func SaveSerialPrefs(p SerialPrefs) error {
	p = p.Normalize()
	dir, err := getConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating serial config directory: %w", err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	path, err := getSerialPrefsPath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
