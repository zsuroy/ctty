package telnetconfig

import (
	"strings"
)

// Connect modes for telnet sessions. ModeInherit defers to the global
// setting; ModeAssistant opens the in-app assistant monitor; ModeRaw
// keeps the classic terminal passthrough (Ctrl-] disconnects, Ctrl+C
// reaches the remote shell).
const (
	ModeInherit   = ""
	ModeAssistant = "assistant"
	ModeRaw       = "raw"
)

// NormalizeMode canonicalises a stored or user-typed connect mode,
// accepting the same aliases the serial side does. Anything unknown is
// inherit, so a hand-edited telnet.json can never brick the list.
func NormalizeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ModeAssistant, "monitor", "assistant-monitor":
		return ModeAssistant
	case ModeRaw, "terminal", "passthrough", "direct":
		return ModeRaw
	}
	return ModeInherit
}

// ResolveMode decides how a connection opens. Precedence: an explicit
// override (CLI flag) wins, then the device's own mode, then the global
// setting, and finally the built-in default — raw for standalone
// 'ctty telnet' launches (scriptable, no chrome) and the assistant
// monitor when opened from the main TUI.
func ResolveMode(override, deviceMode, globalMode string, standalone bool) string {
	if m := NormalizeMode(override); m != ModeInherit {
		return m
	}
	if m := NormalizeMode(deviceMode); m != ModeInherit {
		return m
	}
	switch strings.ToLower(strings.TrimSpace(globalMode)) {
	case ModeAssistant:
		return ModeAssistant
	case ModeRaw:
		return ModeRaw
	}
	if standalone {
		return ModeRaw
	}
	return ModeAssistant
}

// SetMode stores the connect mode for a saved host. Unknown names are a
// no-op so a stale form cannot resurrect a deleted device.
func SetMode(name, mode string) error {
	hosts, err := Load()
	if err != nil {
		return err
	}
	changed := false
	for i := range hosts {
		if hosts[i].Name != name {
			continue
		}
		hosts[i].Mode = NormalizeMode(mode)
		changed = true
	}
	if !changed {
		return nil
	}
	return Save(hosts)
}
