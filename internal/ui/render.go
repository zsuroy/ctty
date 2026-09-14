package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// RenderCanvas renders content onto a guaranteed canvas of size (width x height).
// It solves two critical terminal UI issues:
//  1. Soft-wrap desync: Truncates every line to width, ensuring no line physically
//     wraps in the terminal emulator (which would distort cursor offsets and cause jitter/ghosting).
//  2. Residual lines/ghosting: Uses lipgloss.Place to pad the output to the full height,
//     cleanly overwriting previous frame artifacts when resizing or switching views.
func RenderCanvas(width, height int, content string) string {
	if width <= 0 || height <= 0 {
		return content
	}

	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "")
	}
	clean := strings.Join(lines, "\n")

	return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top, clean)
}
