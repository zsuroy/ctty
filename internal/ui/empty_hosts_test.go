package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zsuroy/ctty/internal/config"
)

// TestEmptyHostListNavigation tests that arrow keys don't cause panic when host list is empty
func TestEmptyHostListNavigation(t *testing.T) {
	// Create empty host list
	hosts := []config.SSHHost{}

	// Create model with empty hosts
	model := NewModel(hosts, "", false, "test", true)
	model.ready = true
	model.width = 80
	model.height = 24

	// Simulate arrow key presses that previously caused panic
	keys := []tea.KeyMsg{
		{Type: tea.KeyUp},
		{Type: tea.KeyDown},
		{Type: tea.KeyLeft},
		{Type: tea.KeyRight},
	}

	for _, key := range keys {
		// This should not panic
		_, cmd := model.Update(key)
		if cmd != nil {
			cmd()
		}
	}

	// Also test with filtered hosts being empty
	model.filteredHosts = []config.SSHHost{}
	model.hosts = []config.SSHHost{}
	model.updateTableRows()

	// Try navigating again
	for _, key := range keys {
		_, cmd := model.Update(key)
		if cmd != nil {
			cmd()
		}
	}
}

// TestEmptyHostListWithSearch tests navigation with empty results after search
func TestEmptyHostListWithSearch(t *testing.T) {
	// Create model with some hosts
	hosts := []config.SSHHost{
		{Name: "server1", Hostname: "example.com"},
	}

	model := NewModel(hosts, "", false, "test", true)
	model.ready = true
	model.width = 80
	model.height = 24

	// Enter search mode
	model.searchMode = true
	model.table.Blur()
	model.searchInput.Focus()

	// Search for something that doesn't exist
	model.searchInput.SetValue("nonexistent")
	model.filteredHosts = []config.SSHHost{}
	model.updateTableRows()

	// Try to navigate with arrow keys - should not panic
	keys := []tea.KeyMsg{
		{Type: tea.KeyUp},
		{Type: tea.KeyDown},
		{Type: tea.KeyLeft},
		{Type: tea.KeyRight},
	}

	for _, key := range keys {
		_, cmd := model.Update(key)
		if cmd != nil {
			cmd()
		}
	}
}

// TestPingAllWithEmptyHosts tests that ping all command works with empty host list
func TestPingAllWithEmptyHosts(t *testing.T) {
	hosts := []config.SSHHost{}
	model := NewModel(hosts, "", false, "test", true)

	// This should not panic
	cmd := model.startPingAllCmd()
	if cmd != nil {
		cmd()
	}
}

// TestEmptyHostListWithSmallTerminal tests with small terminal size
func TestEmptyHostListWithSmallTerminal(t *testing.T) {
	hosts := []config.SSHHost{}
	model := NewModel(hosts, "", false, "test", true)
	model.ready = true
	model.width = 80
	model.height = 10 // Small terminal

	// This should not panic
	model.updateTableHeight()
	model.updateTableColumns()
	model.updateTableRows()

	// Try navigating
	keys := []tea.KeyMsg{
		{Type: tea.KeyUp},
		{Type: tea.KeyDown},
		{Type: tea.KeyLeft},
		{Type: tea.KeyRight},
	}

	for _, key := range keys {
		_, cmd := model.Update(key)
		if cmd != nil {
			cmd()
		}
	}
}
