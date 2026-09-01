package hostimport

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/zsuroy/ctty/internal/config"
)

// Source converts another app's connection list into OpenSSH hosts.
//
// Add a new importer by implementing this interface in a file such as
// putty.go, then calling Register from init(). The CLI picks it up
// automatically via Lookup / Names.
type Source interface {
	// Name is the CLI identifier, e.g. "tabby".
	Name() string
	// DefaultPath is this app's config file on the current OS.
	DefaultPath() (string, error)
	// DestFileName is the OpenSSH include file, e.g. "tabby.conf".
	DestFileName() string
	// Parse converts the source file into SSH hosts. Drop secrets.
	Parse(data []byte) ([]config.SSHHost, error)
}

var (
	mu      sync.RWMutex
	sources = map[string]Source{}
)

// Register adds a source. Names are case-insensitive.
func Register(s Source) error {
	name := strings.ToLower(strings.TrimSpace(s.Name()))
	if name == "" {
		return fmt.Errorf("import source name is empty")
	}
	mu.Lock()
	defer mu.Unlock()
	if _, exists := sources[name]; exists {
		return fmt.Errorf("import source %q already registered", name)
	}
	sources[name] = s
	return nil
}

// Lookup returns a registered source by name.
func Lookup(name string) (Source, bool) {
	mu.RLock()
	defer mu.RUnlock()
	s, ok := sources[strings.ToLower(strings.TrimSpace(name))]
	return s, ok
}

// Names returns registered source identifiers in sorted order.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(sources))
	for name := range sources {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
