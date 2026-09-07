package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestSFTPErrorViewScalesWithTerminalWidth(t *testing.T) {
	styles := NewStyles(80)
	m := NewSFTPForm(styles, 80, 24, "server1", "")
	m.mode = sftpError
	m.loadError = strings.Repeat("hostkey-mismatch-", 20)

	var prev int
	for _, w := range []int{40, 80, 120} {
		m.width = w
		m.height = 24
		view := m.renderErrorView()
		got := lipgloss.Width(view)
		if got > w {
			t.Fatalf("width=%d: rendered %d wider than terminal", w, got)
		}
		if prev != 0 && got < prev {
			t.Fatalf("width=%d: rendered %d did not grow with terminal (prev %d)", w, got, prev)
		}
		// Box should track terminal (App + FormContainer ≈ full width).
		if w >= 40 && got < w-4 {
			t.Fatalf("width=%d: rendered %d too narrow (expected near full width)", w, got)
		}
		prev = got
	}
}
