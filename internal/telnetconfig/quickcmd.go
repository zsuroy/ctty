package telnetconfig

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TelnetQuickCmd is one slot in the telnet assistant's quick-command bar.
// Each entry carries its own Hex flag so a global HEX-send toggle can
// never mis-decode a plain "ls" command (or mistake "AB" for a hex byte).
type TelnetQuickCmd struct {
	Label string `json:"label"` // Short label shown in the slot, e.g. "ls"
	Value string `json:"value"` // Payload text; hex bytes when Hex is true
	Hex   bool   `json:"hex"`   // Decode Value as hex instead of raw ASCII
}

// DefaultTelnetQuickCmds returns the eight built-in slots used when no
// telnet-quick.json exists yet. Telnet targets are shells and network
// gear, so the stock slots are read-only inspection commands — all ASCII,
// with the assistant appending the configured line ending on send.
func DefaultTelnetQuickCmds() []TelnetQuickCmd {
	return []TelnetQuickCmd{
		{Label: "ls -la", Value: "ls -la", Hex: false},
		{Label: "ps", Value: "ps", Hex: false},
		{Label: "df -h", Value: "df -h", Hex: false},
		{Label: "show version", Value: "show version", Hex: false},
		{Label: "display version", Value: "display version", Hex: false},
		{Label: "enable", Value: "enable", Hex: false},
		{Label: "show running", Value: "show running-config", Hex: false},
		{Label: "exit", Value: "exit", Hex: false},
	}
}

// normalizeHex strips the spellings people paste into a hex field: spaces,
// tabs, 0x/\x prefixes and the common separators. The send box accepts the
// same set, so a frame pasted in either place decodes identically.
func normalizeHex(v string) string {
	return strings.NewReplacer(
		"0x", "", "\\x", "",
		" ", "", "\t", "", "\r", "", "\n", "", "\v", "", "\f", "",
		",", "", "-", "", ":", "", "_", "",
	).Replace(strings.ToLower(v))
}

// getQuickCmdPath returns ~/.config/ctty/telnet-quick.json (or the XDG /
// Windows equivalent), alongside the telnet host config.
func getQuickCmdPath() (string, error) {
	dir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "telnet-quick.json"), nil
}

// LoadTelnetQuickCmds reads the quick-command file. A missing file yields
// the defaults without creating anything; a malformed file is reported so
// the caller can surface the error instead of silently dropping config.
func LoadTelnetQuickCmds() ([]TelnetQuickCmd, error) {
	path, err := getQuickCmdPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultTelnetQuickCmds(), nil
	}
	if err != nil {
		return nil, err
	}
	var cmds []TelnetQuickCmd
	if err := json.Unmarshal(data, &cmds); err != nil {
		return nil, fmt.Errorf("parsing quick commands: %w", err)
	}
	if len(cmds) == 0 {
		return DefaultTelnetQuickCmds(), nil
	}
	return cmds, nil
}

// SaveTelnetQuickCmds writes the quick-command file, creating the config
// dir if needed.
func SaveTelnetQuickCmds(cmds []TelnetQuickCmd) error {
	dir, err := getConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating telnet config directory: %w", err)
	}
	data, err := json.MarshalIndent(cmds, "", "  ")
	if err != nil {
		return err
	}
	path, err := getQuickCmdPath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// Payload resolves a slot to the exact bytes to write. Hex slots tolerate
// spaces, tabs and 0x prefixes; ASCII slots are sent verbatim.
//
// A slot with an empty Value falls back to its Label: the add flow lands the
// user on the label field first, and a named command that sends zero bytes is
// never what someone means. An explicit Value always wins, so slots that use
// the label purely as a display name keep their own payload.
func (c TelnetQuickCmd) Payload() ([]byte, error) {
	if !c.Hex {
		if c.Value == "" {
			return []byte(c.Label), nil
		}
		return []byte(c.Value), nil
	}
	clean := normalizeHex(c.Value)
	b, err := hex.DecodeString(clean)
	if err != nil {
		return nil, fmt.Errorf("invalid hex command %q: %w", c.Value, err)
	}
	return b, nil
}

// ByteCount is the number of bytes this slot sends, or -1 when its hex text
// is malformed (the editor uses this to flag a bad entry live).
func (c TelnetQuickCmd) ByteCount() int {
	b, err := c.Payload()
	if err != nil {
		return -1
	}
	return len(b)
}
