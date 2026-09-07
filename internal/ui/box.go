package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderAsciiBox draws a NormalBorder-style frame around body without lipgloss
// Width/BorderStyle measurement. Each body line is padded to innerWidth using
// terminalDisplayWidth so JetBrains/JediTerm status-emoji advance matches the
// corner columns.
func renderAsciiBox(innerWidth int, body string, borderColor string) string {
	if innerWidth < 0 {
		innerWidth = 0
	}
	border := lipgloss.NewStyle().Foreground(lipgloss.Color(borderColor))
	top := border.Render("┌" + strings.Repeat("─", innerWidth) + "┐")
	bot := border.Render("└" + strings.Repeat("─", innerWidth) + "┘")
	left := border.Render("│")
	right := border.Render("│")

	var lines []string
	lines = append(lines, top)
	if body == "" {
		lines = append(lines, left+padToTerminalWidth("", innerWidth)+right)
	} else {
		for _, line := range strings.Split(body, "\n") {
			lines = append(lines, left+padToTerminalWidth(line, innerWidth)+right)
		}
	}
	lines = append(lines, bot)
	return strings.Join(lines, "\n")
}
