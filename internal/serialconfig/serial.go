package serialconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SerialDevice represents a saved serial connection configuration.
type SerialDevice struct {
	Name        string  `json:"name"`         // User-friendly alias, e.g. "Switch-Console"
	Device      string  `json:"device"`       // Device path, e.g. /dev/cu.usbserial-1420
	BaudRate    int     `json:"baud_rate"`    // e.g. 9600, 115200
	DataBits    int     `json:"data_bits"`    // 5, 6, 7, 8 (default 8)
	Parity      string  `json:"parity"`       // "none", "even", "odd" (default "none")
	StopBits    float64 `json:"stop_bits"`    // 1, 1.5 or 2 (default 1)
	FlowControl string  `json:"flow_control"` // "none", "hardware", "software" (default "none")
	// Mode picks the UI used when this device is opened: ModeAssistant
	// (in-app monitor) or ModeRaw (terminal passthrough). ModeInherit,
	// the zero value, follows the global setting and then the built-in
	// per-entry default, so old config files keep their behaviour.
	Mode string `json:"mode,omitempty"`
}

// Connect modes shared by the TUI, the settings page and the CLI.
const (
	ModeInherit   = ""          // follow settings, then the built-in default
	ModeAssistant = "assistant" // in-app assistant monitor
	ModeRaw       = "raw"       // raw terminal passthrough
)

// NormalizeMode maps user input (and legacy aliases) onto one of the three
// connect modes. Anything unrecognised inherits.
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
// 'ctty serial' launches (scriptable, no chrome) and the assistant monitor
// when opened from the main TUI.
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

// SetMode stores the connect mode for a saved device. Unknown names are a
// no-op so a stale form cannot resurrect a deleted device.
func SetMode(name, mode string) error {
	devices, err := Load()
	if err != nil {
		return err
	}
	changed := false
	for i := range devices {
		if devices[i].Name != name {
			continue
		}
		devices[i].Mode = NormalizeMode(mode)
		changed = true
	}
	if !changed {
		return nil
	}
	return Save(devices)
}

// Config is the on-disk JSON structure for saved serial devices.
type Config struct {
	Devices []SerialDevice `json:"devices"`
}

// getConfigPath returns the path to the serial config file.
// Lives alongside ctty's config dir: ~/.config/ctty/serial.json
func getConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	// Prefer XDG
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "ctty", "serial.json"), nil
	}
	return filepath.Join(home, ".config", "ctty", "serial.json"), nil
}

// getConfigDir returns the directory containing the serial config.
func getConfigDir() (string, error) {
	p, err := getConfigPath()
	if err != nil {
		return "", err
	}
	return filepath.Dir(p), nil
}

// Load reads saved serial devices from disk. Returns empty list if file
// does not exist (not an error).
func Load() ([]SerialDevice, error) {
	path, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []SerialDevice{}, nil
	}
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing serial config: %w", err)
	}
	return cfg.Devices, nil
}

// Save writes serial devices to disk, creating the directory if needed.
func Save(devices []SerialDevice) error {
	dir, err := getConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating serial config directory: %w", err)
	}

	path, err := getConfigPath()
	if err != nil {
		return err
	}

	// Sort by name for stable output
	sorted := make([]SerialDevice, len(devices))
	copy(sorted, devices)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	cfg := Config{Devices: sorted}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// Add appends a new serial device and saves.
func Add(device SerialDevice) error {
	devices, err := Load()
	if err != nil {
		return err
	}
	devices = append(devices, device)
	return Save(devices)
}

// Delete removes a serial device by name and saves.
func Delete(name string) error {
	devices, err := Load()
	if err != nil {
		return err
	}
	filtered := devices[:0]
	for _, d := range devices {
		if d.Name != name {
			filtered = append(filtered, d)
		}
	}
	return Save(filtered)
}

// DefaultDevice returns sensible defaults for a new serial connection.
func DefaultDevice() SerialDevice {
	return SerialDevice{
		BaudRate:    115200,
		DataBits:    8,
		Parity:      "none",
		StopBits:    1,
		FlowControl: "none",
	}
}

// Find returns the saved serial device with the given name.
func Find(name string) (SerialDevice, bool) {
	devices, err := Load()
	if err != nil {
		return SerialDevice{}, false
	}
	for _, d := range devices {
		if d.Name == name {
			return d, true
		}
	}
	return SerialDevice{}, false
}
