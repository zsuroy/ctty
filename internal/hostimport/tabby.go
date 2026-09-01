package hostimport

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zsuroy/ctty/internal/config"
	"gopkg.in/yaml.v3"
)

func init() {
	if err := Register(Tabby{}); err != nil {
		panic(err)
	}
}

// Tabby imports SSH profiles from Tabby's config.yaml.
type Tabby struct{}

func (Tabby) Name() string         { return "tabby" }
func (Tabby) DestFileName() string { return "tabby.conf" }

func (Tabby) DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "tabby", "config.yaml"), nil
	case "windows":
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "tabby", "config.yaml"), nil
		}
		return filepath.Join(home, "AppData", "Roaming", "tabby", "config.yaml"), nil
	default:
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, "tabby", "config.yaml"), nil
		}
		return filepath.Join(home, ".config", "tabby", "config.yaml"), nil
	}
}

type tabbyFile struct {
	Profiles []tabbyProfile `yaml:"profiles"`
	Groups   []tabbyGroup   `yaml:"groups"`
}

type tabbyProfile struct {
	Name    string       `yaml:"name"`
	Type    string       `yaml:"type"`
	ID      string       `yaml:"id"`
	Group   string       `yaml:"group"`
	Options tabbyOptions `yaml:"options"`
}

type tabbyGroup struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
}

type tabbyOptions struct {
	Host         string   `yaml:"host"`
	User         string   `yaml:"user"`
	Port         any      `yaml:"port"`
	PrivateKeys  []string `yaml:"privateKeys"`
	ProxyCommand string   `yaml:"proxyCommand"`
	JumpHost     string   `yaml:"jumpHost"`
}

// Parse converts Tabby config.yaml into OpenSSH host records.
func (Tabby) Parse(yamlBytes []byte) ([]config.SSHHost, error) {
	var file tabbyFile
	if err := yaml.Unmarshal(yamlBytes, &file); err != nil {
		return nil, fmt.Errorf("parse tabby config: %w", err)
	}

	groupName := map[string]string{}
	for _, g := range file.Groups {
		if g.ID != "" && g.Name != "" {
			groupName[g.ID] = g.Name
		}
	}

	idToAlias := map[string]string{}
	used := map[string]bool{}
	type pending struct {
		profile tabbyProfile
		host    config.SSHHost
	}
	var keep []pending

	for _, p := range file.Profiles {
		if !strings.EqualFold(p.Type, "ssh") {
			continue
		}
		if strings.TrimSpace(p.Options.Host) == "" {
			continue
		}
		alias := UniqueAlias(SanitizeAlias(p.Name, p.Options.Host), used)
		used[alias] = true
		if p.ID != "" {
			idToAlias[p.ID] = alias
		}

		h := config.SSHHost{
			Name:     alias,
			Hostname: strings.TrimSpace(p.Options.Host),
			User:     strings.TrimSpace(p.Options.User),
			Port:     PortString(p.Options.Port),
		}
		if h.Port == "" {
			h.Port = "22"
		}
		if tag := groupName[p.Group]; tag != "" {
			h.Tags = []string{tag}
		}
		keys := normalizeTabbyKeyPaths(p.Options.PrivateKeys)
		if len(keys) > 0 {
			h.Identity = keys[0]
			if len(keys) > 1 {
				var extra []string
				for _, k := range keys[1:] {
					extra = append(extra, "IdentityFile "+k)
				}
				h.Options = strings.Join(extra, "\n")
			}
		}
		if cmd := strings.TrimSpace(p.Options.ProxyCommand); cmd != "" {
			h.ProxyCommand = cmd
		}
		keep = append(keep, pending{profile: p, host: h})
	}

	out := make([]config.SSHHost, 0, len(keep))
	for _, item := range keep {
		h := item.host
		if jump := strings.TrimSpace(item.profile.Options.JumpHost); jump != "" {
			if alias, ok := idToAlias[jump]; ok {
				h.ProxyJump = alias
			} else {
				h.ProxyJump = jump
			}
		}
		out = append(out, h)
	}
	return out, nil
}

func normalizeTabbyKeyPaths(keys []string) []string {
	var out []string
	for _, k := range keys {
		k = strings.TrimSpace(k)
		k = strings.TrimPrefix(k, "file://")
		if k != "" {
			out = append(out, k)
		}
	}
	return out
}
