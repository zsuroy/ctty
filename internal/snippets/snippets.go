// Package snippets stores the user's saved command snippets
// (~/.config/ctty/snippets.json). The exec view (`x`) and the in-session
// snippet picker for telnet/serial read the same file.
package snippets

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Snippet is a saved command.
type Snippet struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

// Builtin are the common Linux snippets shown at the top of the exec view.
var Builtin = []Snippet{
	{Name: "docker ps", Command: "docker ps"},
	{Name: "df -h", Command: "df -h"},
	{Name: "free -m", Command: "free -m"},
	{Name: "uptime", Command: "uptime"},
	{Name: "top (batch)", Command: "top -bn1 | head -20"},
	{Name: "last logins", Command: "last -10"},
	{Name: "processes", Command: "ps aux --sort=-%mem | head -15"},
	{Name: "disk usage", Command: "du -sh /* 2>/dev/null | sort -rh | head -10"},
}

// Path returns the snippets file path, or "" when the home directory is unknown.
func Path() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "ctty", "snippets.json")
}

// Load returns the user's saved snippets; a missing or unreadable file
// yields nil.
func Load() []Snippet {
	path := Path()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []Snippet
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

// Save persists the user's snippets.
func Save(list []Snippet) error {
	path := Path()
	if path == "" {
		return fmt.Errorf("cannot determine home directory")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
