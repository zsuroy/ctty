package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// eraseLineRight clears from the cursor to the end of the line. Emitting it
// ourselves per row makes the terminal clear the row tail, which is the only
// reliable way to overwrite stale cells when glyph advances differ from what
// ansi.StringWidth counts (Bubble Tea's renderer only appends its own erase
// when a row's StringWidth is strictly below the canvas width).
const eraseLineRight = ansi.EraseLineRight

// trimRowTrailingSpaces removes trailing padding from a rendered row while
// preserving a trailing SGR reset. Trailing spaces must never be relied on to
// fill the canvas: rows are width-critical content (the FTP table is budgeted
// to the full terminal width, so its right border IS the last column), and
// trimming by ansi.StringWidth — or to any fixed short target — cuts that
// border off.
func trimRowTrailingSpaces(s string) string {
	if strings.HasSuffix(s, "\x1b[0m") {
		return strings.TrimRight(strings.TrimSuffix(s, "\x1b[0m"), " ") + "\x1b[0m"
	}
	return strings.TrimRight(s, " ")
}

// canvasRow prepares one row for emission on compensation terminals:
// trailing padding trimmed, a hard no-wrap guard, and an explicit
// EraseLineRight so the terminal clears whatever the row does not cover.
func canvasRow(line string, width int) string {
	row := trimRowTrailingSpaces(line)
	if sw := ansi.StringWidth(row); sw > width {
		row = ansi.Truncate(row, width, "")
	}
	return row + eraseLineRight
}

// RenderCanvas renders content onto a guaranteed canvas of size (width x height).
// It solves two critical terminal UI issues:
//  1. Soft-wrap desync: Truncates every line to width, ensuring no line physically
//     wraps in the terminal emulator (which would distort cursor offsets and cause jitter/ghosting).
//  2. Residual lines/ghosting: Uses lipgloss.Place to pad the output to the full height,
//     cleanly overwriting previous frame artifacts when resizing or switching views.
//
// On JetBrains/JediTerm and VS Code the deficit/excess tables describe how
// glyph advances differ from ansi.StringWidth there, and rows are emitted
// trailing-space-free with an explicit EraseLineRight (see canvasRow): the
// terminal clears the row tail itself, so stale cells from the previous frame
// disappear no matter which way the glyph metrics are off — and no visible
// content is ever trimmed to make room.
func RenderCanvas(width, height int, content string) string {
	if width <= 0 || height <= 0 {
		return content
	}

	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	if needsWidthCompensation() {
		out := make([]string, 0, height)
		for _, line := range lines {
			out = append(out, canvasRow(line, width))
		}
		for len(out) < height {
			out = append(out, eraseLineRight)
		}
		return strings.Join(out, "\n")
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "")
	}
	clean := strings.Join(lines, "\n")

	return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top, clean)
}

// DebugRowReport summarizes how a rendered frame's rows measure against the
// target canvas. The hidden debug-view command prints it so a user can run
// the dump inside a suspect terminal: a row whose StringWidth exceeds the
// canvas gets truncated by Bubble Tea's renderer (visible content lost), and
// one that is unexpectedly short may leave stale cells. "deviating=0" proves
// every row fits the canvas by our measurement.
func DebugRowReport(frame string, width, height int) string {
	lines := strings.Split(frame, "\n")
	minD, maxD, minS, maxS, bad := -1, -1, -1, -1, 0
	for _, ln := range lines {
		d, s := terminalDisplayWidth(ln), ansi.StringWidth(ln)
		if minD < 0 || d < minD {
			minD = d
		}
		if d > maxD {
			maxD = d
		}
		if minS < 0 || s < minS {
			minS = s
		}
		if s > maxS {
			maxS = s
		}
		if d > width || s > width {
			bad++
		}
	}
	return fmt.Sprintf("rows=%d canvas=%dx%d display=[%d..%d] stringWidth=[%d..%d] exceeding=%d",
		len(lines), width, height, minD, maxD, minS, maxS, bad)
}
