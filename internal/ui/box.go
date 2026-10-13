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

// alignJetBrainsBox adjusts the right border alignment for bordered boxes
// rendered on JetBrains/JediTerm. Because JediTerm advances only 1 column for
// certain glyphs (e.g. U+26A1 ⚡, U+23F3 ⏳, U+26AB) where ansi.StringWidth
// counts 2, lipgloss pads fewer spaces than necessary, leaving the right border
// shifted 1 column to the left.
// alignJetBrainsBox inserts the missing spaces before the right border character
// (and before any ANSI SGR sequence styling that border) so that all rows align
// perfectly with the top and bottom corners. The mirror case — glyphs that
// advance WIDER than ansi.StringWidth counts (U+1F3F7 on JediTerm, measured in
// PyCharm) — trims the same slack back so the row does not overhang.
func alignJetBrainsBox(box string) string {
	if !needsWidthCompensation() {
		return box
	}
	lines := strings.Split(box, "\n")
	if len(lines) <= 2 {
		return box
	}

	// Ground truth for the target: each row measured WITHOUT its excess.
	// A row rendering wider than ansi.StringWidth counts must be trimmed
	// down to the rest of the box; letting it drive the target would widen
	// every other row instead.
	targetWidth := 0
	for _, l := range lines {
		if w := terminalDisplayWidth(l) - glyphExcessFor(l); w > targetWidth {
			targetWidth = w
		}
	}
	if targetWidth <= 0 {
		return box
	}

	rightBorders := "│┃║"
	var fixed []string
	for _, line := range lines {
		curWidth := terminalDisplayWidth(line)
		diff := targetWidth - curWidth
		if diff == 0 {
			fixed = append(fixed, line)
			continue
		}

		lastBorderRuneIdx := -1
		for _, b := range rightBorders {
			idx := strings.LastIndex(line, string(b))
			if idx > lastBorderRuneIdx {
				lastBorderRuneIdx = idx
			}
		}

		if lastBorderRuneIdx < 0 {
			// No right border: only the deficit case can be fixed, by
			// padding from the end of the line.
			if diff > 0 {
				fixed = append(fixed, line+strings.Repeat(" ", diff))
			} else {
				fixed = append(fixed, line)
			}
			continue
		}

		insertIdx := lastBorderRuneIdx
		if escIdx := strings.LastIndex(line[:lastBorderRuneIdx], "\x1b["); escIdx >= 0 {
			sub := line[escIdx:lastBorderRuneIdx]
			if isSGRSequence(sub) {
				insertIdx = escIdx
			}
		}

		if diff > 0 {
			fixed = append(fixed, line[:insertIdx]+strings.Repeat(" ", diff)+line[insertIdx:])
			continue
		}

		// diff < 0: excess glyphs push this row past the target. Trim the
		// slack padding before the right border so its real advance matches
		// the rest of the box.
		trim := -diff
		end := insertIdx
		for trim > 0 && end > 0 && line[end-1] == ' ' {
			end--
			trim--
		}
		fixed = append(fixed, line[:end]+line[insertIdx:])
	}
	return strings.Join(fixed, "\n")
}

func isSGRSequence(s string) bool {
	if !strings.HasPrefix(s, "\x1b[") || !strings.HasSuffix(s, "m") {
		return false
	}
	params := s[2 : len(s)-1]
	for i := 0; i < len(params); i++ {
		c := params[i]
		if (c < '0' || c > '9') && c != ';' && c != ':' {
			return false
		}
	}
	return true
}

// placeBox positions a block inside a canvas of dimensions width x height.
// On non-JetBrains terminals, it delegates directly to lipgloss.Place.
// On JetBrains/JediTerm, uniform padding is applied across all rows to prevent
// per-row advance discrepancies (e.g. status/modal emoji) from staggering
// outer edges or borders.
func placeBox(width, height int, hAlign, vAlign lipgloss.Position, box string) string {
	if !needsWidthCompensation() {
		return lipgloss.Place(width, height, hAlign, vAlign, box)
	}
	lines := strings.Split(box, "\n")
	boxH := len(lines)
	if boxH == 0 {
		return ""
	}
	boxW := 0
	for _, l := range lines {
		if w := terminalDisplayWidth(l); w > boxW {
			boxW = w
		}
	}

	topPad := 0
	bottomPad := 0
	if height > boxH {
		switch vAlign {
		case lipgloss.Top:
			topPad = 0
			bottomPad = height - boxH
		case lipgloss.Bottom:
			topPad = height - boxH
			bottomPad = 0
		default: // lipgloss.Center
			topPad = (height - boxH) / 2
			bottomPad = height - boxH - topPad
		}
	}

	leftPad := 0
	rightPad := 0
	if width > boxW {
		switch hAlign {
		case lipgloss.Left:
			leftPad = 0
			rightPad = width - boxW
		case lipgloss.Right:
			leftPad = width - boxW
			rightPad = 0
		default: // lipgloss.Center
			leftPad = (width - boxW) / 2
			rightPad = width - boxW - leftPad
		}
	}

	leftSpaces := strings.Repeat(" ", leftPad)
	rightSpaces := strings.Repeat(" ", rightPad)
	emptyLine := strings.Repeat(" ", width)

	var res []string
	for i := 0; i < topPad; i++ {
		res = append(res, emptyLine)
	}
	for _, l := range lines {
		res = append(res, leftSpaces+l+rightSpaces)
	}
	for i := 0; i < bottomPad; i++ {
		res = append(res, emptyLine)
	}
	return strings.Join(res, "\n")
}
