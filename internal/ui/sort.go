package ui

import (
	"sort"
	"strings"

	"github.com/zsuroy/ctty/internal/config"
)

// sortHosts sorts hosts according to the current sort mode
func (m Model) sortHosts(hosts []config.SSHHost) []config.SSHHost {
	switch m.sortMode {
	case SortByName:
		return sortHostsByName(hosts)
	case SortByHostname:
		return sortHostsByHostname(hosts)
	case SortByTags:
		return sortHostsByTags(hosts)
	case SortByLastUsed:
		if m.historyManager != nil {
			return m.historyManager.SortHostsByLastUsed(hosts)
		}
		return sortHostsByName(hosts)
	default:
		return sortHostsByName(hosts)
	}
}

// sortHostsByName sorts a slice of SSH hosts alphabetically by name
func sortHostsByName(hosts []config.SSHHost) []config.SSHHost {
	sorted := make([]config.SSHHost, len(hosts))
	copy(sorted, hosts)

	sort.Slice(sorted, func(i, j int) bool {
		return strings.ToLower(sorted[i].Name) < strings.ToLower(sorted[j].Name)
	})

	return sorted
}

// sortHostsByHostname sorts a slice of SSH hosts alphabetically by hostname
func sortHostsByHostname(hosts []config.SSHHost) []config.SSHHost {
	sorted := make([]config.SSHHost, len(hosts))
	copy(sorted, hosts)

	sort.Slice(sorted, func(i, j int) bool {
		h1 := strings.ToLower(sorted[i].Hostname)
		h2 := strings.ToLower(sorted[j].Hostname)
		if h1 == h2 {
			return strings.ToLower(sorted[i].Name) < strings.ToLower(sorted[j].Name)
		}
		return h1 < h2
	})

	return sorted
}

// sortHostsByTags sorts a slice of SSH hosts by tags (hosts with tags first, alphabetically)
func sortHostsByTags(hosts []config.SSHHost) []config.SSHHost {
	sorted := make([]config.SSHHost, len(hosts))
	copy(sorted, hosts)

	sort.Slice(sorted, func(i, j int) bool {
		t1 := strings.ToLower(strings.Join(sorted[i].Tags, " "))
		t2 := strings.ToLower(strings.Join(sorted[j].Tags, " "))

		// If one has tags and the other doesn't, the one with tags comes first
		if t1 != "" && t2 == "" {
			return true
		}
		if t1 == "" && t2 != "" {
			return false
		}
		if t1 == t2 {
			return strings.ToLower(sorted[i].Name) < strings.ToLower(sorted[j].Name)
		}
		return t1 < t2
	})

	return sorted
}

// filterHosts filters hosts according to the search query (name, hostname, user, or tags)
func (m Model) filterHosts(query string) []config.SSHHost {
	words := strings.Fields(strings.TrimSpace(query))
	if len(words) == 0 {
		return m.sortHosts(m.hosts)
	}

	var current []config.SSHHost = m.hosts
	for _, word := range words {
		var matched []config.SSHHost
		wordLower := strings.ToLower(word)
		cleanWord := strings.TrimPrefix(wordLower, "#")

		for _, host := range current {
			isMatch := false

			// Match Name
			if strings.Contains(strings.ToLower(host.Name), wordLower) || strings.Contains(strings.ToLower(host.Name), cleanWord) {
				isMatch = true
			}
			// Match Hostname
			if !isMatch && (strings.Contains(strings.ToLower(host.Hostname), wordLower) || strings.Contains(strings.ToLower(host.Hostname), cleanWord)) {
				isMatch = true
			}
			// Match User
			if !isMatch && (strings.Contains(strings.ToLower(host.User), wordLower) || strings.Contains(strings.ToLower(host.User), cleanWord)) {
				isMatch = true
			}
			// Match Tags
			if !isMatch {
				for _, tag := range host.Tags {
					tagLower := strings.ToLower(tag)
					if strings.Contains(tagLower, cleanWord) || strings.Contains("#"+tagLower, wordLower) {
						isMatch = true
						break
					}
				}
			}

			if isMatch {
				matched = append(matched, host)
			}
		}
		current = matched
		if len(current) == 0 {
			break
		}
	}

	return m.sortHosts(current)
}
