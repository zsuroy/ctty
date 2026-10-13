package ui

import (
	"os"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Status emoji used in the host list ping column and modals (quick peek /
// batch / self-update / tag picker). Source stays ASCII-only via \U escapes
// (editors / checkouts that strip emoji).
var statusEmoji = []string{
	"\U000026AB", // black circle
	"\U0001F7E2", // green circle
	"\U0001F534", // red circle
	"\U0001F7E1", // yellow circle
	"\U000026A1", // lightning (⚡)
	"\U000023F3", // hourglass (⏳)
	"\U00002705", // white heavy check mark (✅)
	"\U0001F3F7", // label tag (🏷️ tag picker title)
}

// jetBrainsGlyphDelta / vsCodeGlyphDelta: static fallbacks for how the IDE
// embedded terminals advance these glyphs relative to ansi.StringWidth
// (negative = narrower than measured, positive = wider). Used only when the
// startup cursor-position probe could not measure the terminal — measured
// values take precedence (see CalibrateGlyphAdvances). The JetBrains entries
// come from PyCharm measurements; VS Code's table is empty because no static
// guess for it has ever held up, so the probe fills it there.
var (
	jetBrainsGlyphDelta = map[string]int{
		"\U000026AB": -1, // black circle — advances 1 in JediTerm
		"\U000026A1": -1, // lightning — advances 1 in JediTerm
		"\U000023F3": -1, // hourglass — advances 1 in JediTerm
		"\U00002705": -1, // check mark — advances 1 in JediTerm
		labelTagEmoji: 1, // label tag + FE0F — advances StringWidth+1 in JediTerm
	}
	// VS Code's entry is measured evidence, not a guess: with a zero delta
	// the drawer title row rendered one column wide there (the variation
	// selector takes a visible cell in xterm.js too). The startup probe
	// overrides this whenever the terminal can be measured.
	vsCodeGlyphDelta = map[string]int{
		labelTagEmoji: 1,
	}
)

// isJetBrainsTerminal reports whether we are running inside a JetBrains IDE
// embedded terminal (JediTerm). Status circle (U+26AB), lightning (U+26A1),
// and hourglass (U+23F3) advance one column there while ansi.StringWidth counts 2;
// green/red/yellow circles keep full-width advance 2.
func isJetBrainsTerminal() bool {
	if strings.Contains(os.Getenv("TERMINAL_EMULATOR"), "JetBrains") {
		return true
	}
	if os.Getenv("JETBRAINS_INTELLIJ_COMMAND_X") != "" {
		return true
	}
	if os.Getenv("IDEA_INITIAL_DIRECTORY") != "" {
		return true
	}
	return false
}

// isVSCodeTerminal reports whether we are running inside VS Code's
// integrated terminal, which sets TERM_PROGRAM=vscode.
func isVSCodeTerminal() bool {
	return os.Getenv("TERM_PROGRAM") == "vscode"
}

// needsWidthCompensation reports whether the current terminal advances
// some emoji differently from ansi.StringWidth, requiring the glyph delta
// tables (JetBrains/JediTerm and VS Code integrated terminal).
func needsWidthCompensation() bool {
	return isJetBrainsTerminal() || isVSCodeTerminal()
}

// glyphDeltaFor reports (real advance - ansi.StringWidth) for s in the
// current terminal: negative where the terminal advances a glyph narrower
// than ansi.StringWidth counts (rows are padded out to align), positive
// where it advances wider (rows are trimmed in). Measured values from the
// startup probe take precedence over the static per-terminal fallbacks.
func glyphDeltaFor(s string) int {
	plain := ansi.Strip(s)
	delta := 0
	for _, g := range layoutGlyphs {
		if v, ok := glyphDeltaOverride[g]; ok {
			delta += v * strings.Count(plain, g)
			continue
		}
		if isJetBrainsTerminal() {
			delta += jetBrainsGlyphDelta[g] * strings.Count(plain, g)
		} else if isVSCodeTerminal() {
			delta += vsCodeGlyphDelta[g] * strings.Count(plain, g)
		}
	}
	return delta
}

// glyphExcessFor returns the part of the active delta that makes s advance
// WIDER than ansi.StringWidth counts. Such rows must be trimmed down to the
// rest of the box instead of driving the alignment target (see
// alignJetBrainsBox).
func glyphExcessFor(s string) int {
	if d := glyphDeltaFor(s); d > 0 {
		return d
	}
	return 0
}

// terminalDisplayWidth returns the column advance of s in the current terminal:
// ansi.StringWidth corrected by the active per-glyph deltas — measured at
// startup by the cursor-position probe, static per-terminal fallbacks when
// the terminal could not be probed.
func terminalDisplayWidth(s string) int {
	w := ansi.StringWidth(s) + glyphDeltaFor(s)
	if w < 0 {
		w = 0
	}
	return w
}

// padToTerminalWidth forces s to exactly width columns of terminalDisplayWidth.
// Truncation removes trailing runes (cells are already per-column truncated);
// padding appends spaces. Does not use ansi.Truncate on JetBrains because that
// API measures with StringWidth and would clip one column too early for U+26AB.
func padToTerminalWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if !needsWidthCompensation() {
		s = ansi.Truncate(s, width, "")
		sw := ansi.StringWidth(s)
		if sw < width {
			s += strings.Repeat(" ", width-sw)
		}
		return s
	}
	for terminalDisplayWidth(s) > width {
		r, size := utf8.DecodeLastRuneInString(s)
		if size <= 0 || r == utf8.RuneError && size == 1 {
			break
		}
		s = s[:len(s)-size]
	}
	for terminalDisplayWidth(s) < width {
		s += " "
	}
	return s
}
