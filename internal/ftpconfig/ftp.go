// Package ftpconfig stores saved FTP site configurations.
//
// FTP sites (lab NAS boxes, legacy file servers, appliance uploads) live at
// ~/.config/ctty/ftp.json with 0600 permissions. Passwords are NEVER stored
// here — see internal/ftpcred for vault-backed FTP passwords.
package ftpconfig

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

// DefaultPort is the standard FTP control port.
const DefaultPort = 21

// FTPSite represents a saved FTP connection configuration.
type FTPSite struct {
	Name string   `json:"name"`           // User-friendly alias
	Host string   `json:"host"`           // Address
	Port int      `json:"port"`           // Defaults to 21
	User string   `json:"user"`           // Login user (empty → anonymous)
	Tags []string `json:"tags,omitempty"` // Optional organizational tags
}

// Config is the on-disk JSON structure for saved FTP sites.
type Config struct {
	Sites []FTPSite `json:"sites"`
}

func getConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "ctty", "ftp.json"), nil
	}
	return filepath.Join(home, ".config", "ctty", "ftp.json"), nil
}

func getConfigDir() (string, error) {
	p, err := getConfigPath()
	if err != nil {
		return "", err
	}
	return filepath.Dir(p), nil
}

// Load reads saved FTP sites from disk. Missing file → empty list.
func Load() ([]FTPSite, error) {
	path, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []FTPSite{}, nil
	}
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing ftp config: %w", err)
	}
	for i := range cfg.Sites {
		cfg.Sites[i].normalize()
	}
	return cfg.Sites, nil
}

// Save writes FTP sites atomically (temp + rename), creating the directory.
func Save(sites []FTPSite) error {
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

	sorted := make([]FTPSite, len(sites))
	copy(sorted, sites)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for i := range sorted {
		sorted[i].normalize()
	}

	data, err := json.MarshalIndent(Config{Sites: sorted}, "", "  ")
	if err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Add appends a new FTP site and saves.
func Add(site FTPSite) error {
	site.normalize()
	if site.Name == "" || site.Host == "" {
		return errors.New("ftp site name and address are required")
	}
	sites, err := Load()
	if err != nil {
		sites = nil
	}
	for _, s := range sites {
		if s.Name == site.Name {
			return fmt.Errorf("an ftp site named %q already exists", site.Name)
		}
	}
	sites = append(sites, site)
	return Save(sites)
}

// Update replaces the saved site identified by oldName and saves.
func Update(oldName string, site FTPSite) error {
	site.normalize()
	sites, err := Load()
	if err != nil {
		return err
	}
	idx := -1
	for i, s := range sites {
		if s.Name == oldName {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("no saved ftp site named %q", oldName)
	}
	for i, s := range sites {
		if i != idx && s.Name == site.Name {
			return fmt.Errorf("an ftp site named %q already exists", site.Name)
		}
	}
	sites[idx] = site
	return Save(sites)
}

// Delete removes a saved FTP site by name and saves.
func Delete(name string) error {
	sites, err := Load()
	if err != nil {
		return err
	}
	out := sites[:0]
	found := false
	for _, s := range sites {
		if s.Name == name {
			found = true
			continue
		}
		out = append(out, s)
	}
	if !found {
		return fmt.Errorf("no saved ftp site named %q", name)
	}
	return Save(out)
}

// Find returns the saved site with the given name.
func Find(name string) (FTPSite, bool) {
	sites, err := Load()
	if err != nil {
		return FTPSite{}, false
	}
	for _, s := range sites {
		if s.Name == name {
			return s, true
		}
	}
	return FTPSite{}, false
}

// DefaultSite returns sensible defaults for a new FTP site.
func DefaultSite() FTPSite {
	return FTPSite{Port: DefaultPort, User: "anonymous"}
}

func (s *FTPSite) normalize() {
	s.Name = strings.TrimSpace(s.Name)
	s.Host = strings.TrimSpace(s.Host)
	s.User = strings.TrimSpace(s.User)
	if !strings.HasPrefix(s.Host, "[") && strings.Count(s.Host, ":") == 1 {
		if idx := strings.LastIndex(s.Host, ":"); idx > 0 {
			if n, err := strconv.Atoi(s.Host[idx+1:]); err == nil && n > 0 && n <= 65535 {
				s.Host = s.Host[:idx]
				s.Port = n
			}
		}
	}
	if s.Port <= 0 || s.Port > 65535 {
		s.Port = DefaultPort
	}
	tags := s.Tags[:0]
	for _, t := range s.Tags {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	s.Tags = tags
}

// Addr returns "host:port" suitable for net.Dial / FTP Dial.
func (s FTPSite) Addr() string {
	return net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
}
