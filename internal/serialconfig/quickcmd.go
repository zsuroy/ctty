package serialconfig

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// QuickCmd is one slot in the serial assistant's quick-command bar. Each
// entry carries its own Hex flag so a global HEX-send toggle can never
// mis-decode a plain "AT" command (or mistake "AB" for a hex byte).
type QuickCmd struct {
	Label string `json:"label"` // Short label shown in the slot, e.g. "AT"
	Value string `json:"value"` // Payload text; hex bytes when Hex is true
	Hex   bool   `json:"hex"`   // Decode Value as hex instead of raw ASCII
}

// DefaultQuickCmds returns the eight built-in slots used when no
// serial-quick.json exists yet.
func DefaultQuickCmds() []QuickCmd {
	return []QuickCmd{
		{Label: "AT", Value: "AT", Hex: false},
		{Label: "AT+RST", Value: "AT+RST", Hex: false},
		{Label: "AT+GMR", Value: "AT+GMR", Hex: false},
		{Label: "AT+VER?", Value: "AT+VER?", Hex: false},
		// Modbus RTU: slave 01, function 03 (read holding registers),
		// start 0x0000, quantity 1, then the CRC over those six bytes.
		// The quantity field is not optional — without it the frame is
		// truncated and the CRC no longer covers what is transmitted.
		{Label: "MODBUS", Value: modbusReadHoldingFrame, Hex: true},
		{Label: "AT+BAUD?", Value: "AT+BAUD?", Hex: false},
		{Label: "AT+STATUS?", Value: "AT+STATUS?", Hex: false},
		{Label: "+++", Value: "+++", Hex: false},
	}
}

const (
	// modbusReadHoldingFrame is 01 03 00 00 00 01 + CRC 84 0A.
	modbusReadHoldingFrame = "01 03 00 00 00 01 84 0A"
	// legacyModbusFrame is what earlier builds shipped for the MODBUS slot.
	// It dropped the quantity field while keeping the CRC of the full
	// request, so every click sent a frame the slave had to reject.
	legacyModbusFrame = "01 03 00 00 84 0A"
)

// migrateQuickCmds repairs the one built-in default that shipped broken.
// Only an exact match is rewritten: a slot the user edited into something
// else keeps its value.
func migrateQuickCmds(cmds []QuickCmd) ([]QuickCmd, bool) {
	changed := false
	for i := range cmds {
		if cmds[i].Hex && normalizeHex(cmds[i].Value) == normalizeHex(legacyModbusFrame) {
			cmds[i].Value = modbusReadHoldingFrame
			changed = true
		}
	}
	return cmds, changed
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

// getQuickCmdPath returns ~/.config/ctty/serial-quick.json (or the XDG /
// Windows equivalent), alongside the serial device config.
func getQuickCmdPath() (string, error) {
	dir, err := getConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "serial-quick.json"), nil
}

// LoadQuickCmds reads the quick-command file. A missing file yields the
// defaults without creating anything; a malformed file is reported so the
// caller can surface the error instead of silently dropping user config.
func LoadQuickCmds() ([]QuickCmd, error) {
	path, err := getQuickCmdPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultQuickCmds(), nil
	}
	if err != nil {
		return nil, err
	}
	var cmds []QuickCmd
	if err := json.Unmarshal(data, &cmds); err != nil {
		return nil, fmt.Errorf("parsing quick commands: %w", err)
	}
	if len(cmds) == 0 {
		return DefaultQuickCmds(), nil
	}
	// Files written by older builds still hold the truncated MODBUS frame;
	// heal it on load so Alt+5 works without editing the slot by hand. The
	// rewrite is persisted so it happens once, not on every launch.
	if fixed, changed := migrateQuickCmds(cmds); changed {
		if err := SaveQuickCmds(fixed); err != nil {
			return fixed, nil // usable in memory even if the write failed
		}
		return fixed, nil
	}
	return cmds, nil
}

// SaveQuickCmds writes the quick-command file, creating the config dir if
// needed.
func SaveQuickCmds(cmds []QuickCmd) error {
	dir, err := getConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating serial config directory: %w", err)
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
func (c QuickCmd) Payload() ([]byte, error) {
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
func (c QuickCmd) ByteCount() int {
	b, err := c.Payload()
	if err != nil {
		return -1
	}
	return len(b)
}
