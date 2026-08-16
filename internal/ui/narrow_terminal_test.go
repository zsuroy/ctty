package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zsuroy/ctty/internal/config"
)

// TestNarrowTerminalColumnWidths verifies that the SSH host table never exceeds
// the terminal width on narrow terminals (e.g. Termux on phones).
func TestNarrowTerminalColumnWidths(t *testing.T) {
	hosts := []config.SSHHost{
		{Name: "prod-server-01", Hostname: "192.168.1.100", Port: "22", Tags: []string{"prod", "web"}},
		{Name: "db", Hostname: "10.0.0.5", Port: "2222"},
	}

	for _, width := range []int{20, 25, 30, 35, 40, 45, 50, 55, 60, 80, 100, 120} {
		t.Run("width", func(t *testing.T) {
			m := NewModel(hosts, "", false, "v0.4.0", true)
			res, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			m = res.(Model)

			cols := m.table.Columns()
			totalColWidth := 0
			activeCols := 0
			for _, col := range cols {
				if col.Width > 0 {
					totalColWidth += col.Width
					activeCols++
				}
			}

			// Total rendered width = column widths + table border (2) + app padding (2)
			renderedWidth := totalColWidth + 4

			if renderedWidth > width {
				t.Errorf("width=%d: rendered width %d exceeds terminal width (cols=%d, activeCols=%d)",
					width, renderedWidth, totalColWidth, activeCols)
			}

			// On very narrow terminals, optional columns should be hidden
			if width < 50 {
				if activeCols > 3 {
					t.Errorf("width=%d: expected at most 3 active columns on narrow terminal, got %d",
						width, activeCols)
				}
			}

			// At least 2 columns (Name + Hostname) should always be visible
			if activeCols < 2 {
				t.Errorf("width=%d: expected at least 2 active columns, got %d",
					width, activeCols)
			}
		})
	}
}

// TestNarrowTerminalEmptyHosts verifies behavior with zero hosts on narrow terminals.
func TestNarrowTerminalEmptyHosts(t *testing.T) {
	hosts := []config.SSHHost{}

	for _, width := range []int{20, 30, 40, 50} {
		t.Run("width", func(t *testing.T) {
			m := NewModel(hosts, "", false, "v0.4.0", true)
			res, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			m = res.(Model)

			// Should not panic
			view := m.View()
			if view == "" {
				t.Errorf("width=%d: empty view for empty hosts", width)
			}

			cols := m.table.Columns()
			totalColWidth := 0
			for _, col := range cols {
				if col.Width > 0 {
					totalColWidth += col.Width
				}
			}
			renderedWidth := totalColWidth + 4
			if renderedWidth > width {
				t.Errorf("width=%d: rendered width %d exceeds terminal width",
					width, renderedWidth)
			}
		})
	}
}
