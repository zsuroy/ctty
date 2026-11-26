package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type FTPLayout string

const (
	FTPLayoutDual   FTPLayout = "dual"
	FTPLayoutSingle FTPLayout = "single"
)

func (l FTPLayout) Valid() bool {
	return l == FTPLayoutDual || l == FTPLayoutSingle
}

func NormalizeFTPLayout(layout FTPLayout) FTPLayout {
	if layout.Valid() {
		return layout
	}
	return FTPLayoutDual
}

type SFTPLayout string

const (
	SFTPLayoutDual   SFTPLayout = "dual"
	SFTPLayoutSingle SFTPLayout = "single"
)

func (l SFTPLayout) Valid() bool {
	return l == SFTPLayoutDual || l == SFTPLayoutSingle
}

func NormalizeSFTPLayout(layout SFTPLayout) SFTPLayout {
	if layout.Valid() {
		return layout
	}
	return SFTPLayoutDual
}

type WebDAVLayout string

const (
	WebDAVLayoutDual   WebDAVLayout = "dual"
	WebDAVLayoutSingle WebDAVLayout = "single"
)

func (l WebDAVLayout) Valid() bool {
	return l == WebDAVLayoutDual || l == WebDAVLayoutSingle
}

func NormalizeWebDAVLayout(layout WebDAVLayout) WebDAVLayout {
	if layout.Valid() {
		return layout
	}
	return WebDAVLayoutDual
}

type S3Layout string

const (
	S3LayoutDual   S3Layout = "dual"
	S3LayoutSingle S3Layout = "single"
)

func (l S3Layout) Valid() bool {
	return l == S3LayoutDual || l == S3LayoutSingle
}

func NormalizeS3Layout(layout S3Layout) S3Layout {
	if layout.Valid() {
		return layout
	}
	return S3LayoutDual
}

// KeyBindings represents configurable key bindings for the application
type KeyBindings struct {
	// Quit keys - keys that will quit the application
	QuitKeys []string `json:"quit_keys"`

	// DisableEscQuit - if true, ESC key won't quit the application (useful for vim users)
	DisableEscQuit bool `json:"disable_esc_quit"`
}

// AppConfig represents the main application configuration
type AppConfig struct {
	CheckForUpdates *bool             `json:"check_for_updates,omitempty"`
	KeyBindings     KeyBindings       `json:"key_bindings"`
	TagColors       map[string]string `json:"tag_colors,omitempty"`
	Language        string            `json:"language,omitempty"`
	Theme           string            `json:"theme,omitempty"`
	FTPLayout       FTPLayout         `json:"ftp_layout,omitempty"`
	SFTPLayout      SFTPLayout        `json:"sftp_layout,omitempty"`
	WebDAVLayout    WebDAVLayout      `json:"webdav_layout,omitempty"`
	S3Layout        S3Layout          `json:"s3_layout,omitempty"`
	// SerialStartMode picks how serial devices open by default:
	// SerialStartAuto keeps the per-entry default (raw for 'ctty serial',
	// assistant monitor from the TUI); SerialStartAssistant / SerialStartRaw
	// force one everywhere. A device's own mode still wins over this.
	SerialStartMode string `json:"serial_start_mode,omitempty"`
	// TelnetStartMode picks how telnet hosts open by default:
	// TelnetStartAuto keeps the per-entry default (raw for 'ctty telnet',
	// assistant monitor from the TUI); TelnetStartAssistant / TelnetStartRaw
	// force one everywhere. A host's own mode still wins over this.
	TelnetStartMode string `json:"telnet_start_mode,omitempty"`
	// Quiet suppresses ctty's own informational output when connecting.
	Quiet bool `json:"quiet,omitempty"`
}

// Default start modes. "auto" preserves the historical split so an
// upgraded config keeps behaving exactly as before.
const (
	SerialStartAuto      = "auto"
	SerialStartAssistant = "assistant"
	SerialStartRaw       = "raw"
	TelnetStartAuto      = "auto"
	TelnetStartAssistant = "assistant"
	TelnetStartRaw       = "raw"
)

// NormalizeSerialStartMode maps stored values onto a known start mode,
// falling back to auto for empty or legacy values.
func NormalizeSerialStartMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case SerialStartAssistant:
		return SerialStartAssistant
	case SerialStartRaw:
		return SerialStartRaw
	}
	return SerialStartAuto
}

// NormalizeTelnetStartMode maps stored values onto a known start mode,
// falling back to auto for empty or legacy values.
func NormalizeTelnetStartMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case TelnetStartAssistant:
		return TelnetStartAssistant
	case TelnetStartRaw:
		return TelnetStartRaw
	}
	return TelnetStartAuto
}

// IsUpdateCheckEnabled returns true if the update check is enabled (default: true)
func (c *AppConfig) IsUpdateCheckEnabled() bool {
	if c == nil || c.CheckForUpdates == nil {
		return true
	}
	return *c.CheckForUpdates
}

// GetDefaultKeyBindings returns the default key bindings configuration
func GetDefaultKeyBindings() KeyBindings {
	return KeyBindings{
		QuitKeys:       []string{"q", "ctrl+c"}, // Default keeps current behavior minus ESC
		DisableEscQuit: false,                   // Default to false for backward compatibility
	}
}

// GetDefaultAppConfig returns the default application configuration
func GetDefaultAppConfig() AppConfig {
	return AppConfig{
		KeyBindings:     GetDefaultKeyBindings(),
		Theme:           "default",
		FTPLayout:       FTPLayoutDual,
		SFTPLayout:      SFTPLayoutDual,
		WebDAVLayout:    WebDAVLayoutDual,
		SerialStartMode: SerialStartAuto,
		TelnetStartMode: TelnetStartAuto,
	}
}

// GetAppConfigPath returns the path to the application config file
func GetAppConfigPath() (string, error) {
	configDir, err := GetcttyConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(configDir, "config.json"), nil
}

// LoadAppConfig loads the application configuration from file
// If the file doesn't exist, it returns the default configuration
func LoadAppConfig() (*AppConfig, error) {
	configPath, err := GetAppConfigPath()
	if err != nil {
		return nil, err
	}

	// If config file doesn't exist, return default config and create the file
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		defaultConfig := GetDefaultAppConfig()

		// Create config directory if it doesn't exist
		configDir := filepath.Dir(configPath)
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return nil, err
		}

		// Save default config to file
		if err := SaveAppConfig(&defaultConfig); err != nil {
			// If we can't save, just return the default config without erroring
			// This allows the app to work even if config file can't be created
			return &defaultConfig, nil
		}

		return &defaultConfig, nil
	}

	// Read existing config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config AppConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	// Validate and fill in missing fields with defaults
	config = mergeWithDefaults(config)

	return &config, nil
}

// SaveAppConfig saves the application configuration to file
func SaveAppConfig(config *AppConfig) error {
	if config == nil {
		return errors.New("config cannot be nil")
	}
	config.FTPLayout = NormalizeFTPLayout(config.FTPLayout)
	config.SFTPLayout = NormalizeSFTPLayout(config.SFTPLayout)

	configPath, err := GetAppConfigPath()
	if err != nil {
		return err
	}

	// Create config directory if it doesn't exist
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

// mergeWithDefaults ensures all required fields are set with defaults if missing
func mergeWithDefaults(config AppConfig) AppConfig {
	defaults := GetDefaultAppConfig()

	// If QuitKeys is empty, use defaults
	if len(config.KeyBindings.QuitKeys) == 0 {
		config.KeyBindings.QuitKeys = defaults.KeyBindings.QuitKeys
	}
	if config.Theme == "" {
		config.Theme = defaults.Theme
	}
	config.FTPLayout = NormalizeFTPLayout(config.FTPLayout)
	config.SFTPLayout = NormalizeSFTPLayout(config.SFTPLayout)

	return config
}

// ShouldQuitOnKey checks if the given key should trigger quit based on configuration
func (kb *KeyBindings) ShouldQuitOnKey(key string) bool {
	// Special handling for ESC key
	if key == "esc" {
		return !kb.DisableEscQuit
	}

	// Check if key is in the quit keys list
	for _, quitKey := range kb.QuitKeys {
		if quitKey == key {
			return true
		}
	}

	return false
}
