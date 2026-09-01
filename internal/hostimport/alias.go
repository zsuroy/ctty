package hostimport

import (
	"fmt"
	"strconv"
	"strings"
)

// SanitizeAlias turns a display name into an OpenSSH Host token.
func SanitizeAlias(name, fallback string) string {
	alias := strings.Join(strings.Fields(strings.TrimSpace(name)), "-")
	if alias == "" {
		alias = strings.Join(strings.Fields(strings.TrimSpace(fallback)), "-")
	}
	return alias
}

// UniqueAlias returns base, or base-2, base-3, … if already used.
func UniqueAlias(base string, used map[string]bool) string {
	if base == "" {
		base = "imported-host"
	}
	if !used[base] {
		return base
	}
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s-%d", base, i)
		if !used[cand] {
			return cand
		}
	}
}

// PortString normalizes YAML/JSON numbers or strings into a port.
func PortString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case float64:
		return strconv.Itoa(int(x))
	case string:
		return strings.TrimSpace(x)
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}
