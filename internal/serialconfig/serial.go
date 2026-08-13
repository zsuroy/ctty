package serialconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// SerialDevice represents a saved serial connection configuration.
type SerialDevice struct {
	Name        string `json:"name"`         // User-friendly alias, e.g. "Switch-Console"
	Device      string `json:"device"`       // Device path, e.g. /dev/cu.usbserial-1420
	BaudRate    int    `json:"baud_rate"`   // e.g. 9600, 115200
	DataBits    int    `json:"data_bits"`    // 5, 6, 7, 8 (default 8)
	Parity      string `json:"parity"`       // "none", "even", "odd" (default "none")
	StopBits    int    `json:"stop_bits"`    // 1 or 2 (default 1)
	FlowControl string `json:"flow_control"` // "none", "hardware", "software" (default "none")
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
