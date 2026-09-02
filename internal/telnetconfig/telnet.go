// Package telnetconfig stores saved telnet device configurations.
//
// Telnet targets are lab equipment, console servers, and legacy network
// gear that expose a cleartext RFC 854 telnet service. Saved hosts live
// alongside ctty's other config files at ~/.config/ctty/telnet.json
// with 0600 permissions.
package telnetconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// DefaultPort is the RFC 854 telnet port.
const DefaultPort = 23

// TelnetHost represents a saved telnet connection configuration.
type TelnetHost struct {
	Name string   `json:"name"`           // User-friendly alias, e.g. "core-sw console"
	Host string   `json:"host"`           // Address, e.g. 192.168.1.1 or console01.lab.local
	Port int      `json:"port"`           // Defaults to 23
	Tags []string `json:"tags,omitempty"` // Optional organizational tags
}

// Config is the on-disk JSON structure for saved telnet hosts.
type Config struct {
	Hosts []TelnetHost `json:"hosts"`
}

// getConfigPath returns the path to the telnet config file.
func getConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "ctty", "telnet.json"), nil
	}
	return filepath.Join(home, ".config", "ctty", "telnet.json"), nil
}

// getConfigDir returns the directory containing the telnet config.
func getConfigDir() (string, error) {
	p, err := getConfigPath()
	if err != nil {
		return "", err
	}
	return filepath.Dir(p), nil
}

// Load reads saved telnet hosts from disk. Returns an empty list if the
// file does not exist (not an error).
func Load() ([]TelnetHost, error) {
	path, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []TelnetHost{}, nil
	}
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing telnet config: %w", err)
	}
	for i := range cfg.Hosts {
		cfg.Hosts[i].normalize()
	}
	return cfg.Hosts, nil
}

// Save writes telnet hosts to disk atomically (temp file + rename),
// creating the directory if needed.
func Save(hosts []TelnetHost) error {
	dir, err := getConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating ctty config directory: %w", err)
	}

	path, err := getConfigPath()
	if err != nil {
		return err
	}

	sorted := make([]TelnetHost, len(hosts))
	copy(sorted, hosts)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for i := range sorted {
		sorted[i].normalize()
	}

	data, err := json.MarshalIndent(Config{Hosts: sorted}, "", "  ")
	if err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Add appends a new telnet host and saves.
func Add(host TelnetHost) error {
	host.normalize()
	if host.Name == "" || host.Host == "" {
		return errors.New("telnet host name and address are required")
	}
	hosts, err := Load()
	if err != nil {
		hosts = nil
	}
	for _, h := range hosts {
		if h.Name == host.Name {
			return fmt.Errorf("a telnet host named %q already exists", host.Name)
		}
	}
	hosts = append(hosts, host)
	return Save(hosts)
}

// Update replaces the saved host identified by oldName and saves.
func Update(oldName string, host TelnetHost) error {
	host.normalize()
	hosts, err := Load()
	if err != nil {
		return err
	}
	idx := -1
	for i, h := range hosts {
		if h.Name == oldName {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("no saved telnet host named %q", oldName)
	}
	// Renaming onto another existing name is rejected.
	for i, h := range hosts {
		if i != idx && h.Name == host.Name {
			return fmt.Errorf("a telnet host named %q already exists", host.Name)
		}
	}
	hosts[idx] = host
	return Save(hosts)
}

// Delete removes a saved telnet host by name and saves.
func Delete(name string) error {
	hosts, err := Load()
	if err != nil {
		return err
	}
	out := hosts[:0]
	found := false
	for _, h := range hosts {
		if h.Name == name {
			found = true
			continue
		}
		out = append(out, h)
	}
	if !found {
		return fmt.Errorf("no saved telnet host named %q", name)
	}
	return Save(out)
}

// Find returns the saved host with the given name.
func Find(name string) (TelnetHost, bool) {
	hosts, err := Load()
	if err != nil {
		return TelnetHost{}, false
	}
	for _, h := range hosts {
		if h.Name == name {
			return h, true
		}
	}
	return TelnetHost{}, false
}

// DefaultHost returns sensible defaults for a new telnet connection.
func DefaultHost() TelnetHost {
	return TelnetHost{Port: DefaultPort}
}

// normalize applies defaults and trims whitespace in place. A host
// value carrying a ":port" suffix (e.g. pasted "10.0.0.5:2001") is
// split; bare IPv6 literals are left intact.
func (h *TelnetHost) normalize() {
	h.Name = strings.TrimSpace(h.Name)
	h.Host = strings.TrimSpace(h.Host)
	if !strings.HasPrefix(h.Host, "[") && strings.Count(h.Host, ":") == 1 {
		if idx := strings.LastIndex(h.Host, ":"); idx > 0 {
			if n, err := strconv.Atoi(h.Host[idx+1:]); err == nil && n > 0 && n <= 65535 {
				h.Host = h.Host[:idx]
				h.Port = n
			}
		}
	}
	if h.Port <= 0 || h.Port > 65535 {
		h.Port = DefaultPort
	}
	tags := h.Tags[:0]
	for _, t := range h.Tags {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	h.Tags = tags
}

// Addr returns "host:port" suitable for net.Dial.
func (h TelnetHost) Addr() string {
	return net.JoinHostPort(h.Host, strconv.Itoa(h.Port))
}
