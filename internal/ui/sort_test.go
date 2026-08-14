package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zsuroy/ctty/internal/config"
)

func TestSortModes(t *testing.T) {
	hosts := []config.SSHHost{
		{Name: "z-host", Hostname: "a-server.com", Tags: []string{"web"}},
		{Name: "a-host", Hostname: "z-server.com", Tags: []string{"prod"}},
		{Name: "m-host", Hostname: "m-server.com", Tags: nil},
	}

	m := createTestModel()
	m.hosts = hosts
	m.filteredHosts = hosts

	// 1. Sort by Name (A-Z)
	m.sortMode = SortByName
	sortedByName := m.sortHosts(hosts)
	if sortedByName[0].Name != "a-host" || sortedByName[1].Name != "m-host" || sortedByName[2].Name != "z-host" {
		t.Errorf("SortByName failed: got %v", sortedByName)
	}

	// 2. Sort by Hostname (A-Z)
	m.sortMode = SortByHostname
	sortedByHostname := m.sortHosts(hosts)
	if sortedByHostname[0].Name != "z-host" || sortedByHostname[1].Name != "m-host" || sortedByHostname[2].Name != "a-host" {
		t.Errorf("SortByHostname failed: got %v", sortedByHostname)
	}

	// 3. Sort by Tags (A-Z, tagged first)
	m.sortMode = SortByTags
	sortedByTags := m.sortHosts(hosts)
	// prod ("a-host") < web ("z-host") < untagged ("m-host")
	if sortedByTags[0].Name != "a-host" || sortedByTags[1].Name != "z-host" || sortedByTags[2].Name != "m-host" {
		t.Errorf("SortByTags failed: got %v", sortedByTags)
	}
}

func TestSortCycling(t *testing.T) {
	m := createTestModel()
	if m.sortMode != SortByName {
		t.Errorf("Expected initial sortMode to be SortByName, got %v", m.sortMode)
	}

	// Press 's' -> SortByHostname
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")}
	newModel, _ := m.Update(keyMsg)
	m = newModel.(Model)
	if m.sortMode != SortByHostname {
		t.Errorf("Expected SortByHostname after 1st 's', got %v", m.sortMode)
	}

	// Press 's' -> SortByTags
	newModel, _ = m.Update(keyMsg)
	m = newModel.(Model)
	if m.sortMode != SortByTags {
		t.Errorf("Expected SortByTags after 2nd 's', got %v", m.sortMode)
	}

	// Press 's' -> SortByLastUsed
	newModel, _ = m.Update(keyMsg)
	m = newModel.(Model)
	if m.sortMode != SortByLastUsed {
		t.Errorf("Expected SortByLastUsed after 3rd 's', got %v", m.sortMode)
	}

	// Press 's' -> back to SortByName
	newModel, _ = m.Update(keyMsg)
	m = newModel.(Model)
	if m.sortMode != SortByName {
		t.Errorf("Expected SortByName after 4th 's', got %v", m.sortMode)
	}
}
