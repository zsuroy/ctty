package ui

import (
	"os"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
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
	"\U0001F7E0", // orange circle
	"\U0001F535", // blue circle
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
		"\U000026AB":  -1, // black circle — advances 1 in JediTerm
		"\U000026A1":  -1, // lightning — advances 1 in JediTerm
		"\U000023F3":  -1, // hourglass — advances 1 in JediTerm
		"\U00002705":  -1, // check mark — advances 1 in JediTerm
		labelTagEmoji: 1,  // label tag + FE0F — advances StringWidth+1 in JediTerm
	}
	// VS Code's entry is measured evidence, not a guess: with a zero delta
	// the drawer title row rendered one column wide there (the variation
	// selector takes a visible cell in xterm.js too). The startup probe
	// overrides this whenever the terminal can be measured.
	vsCodeGlyphDelta = map[string]int{
		labelTagEmoji: 1,
	}
	// Windows legacy console (ConHost) fallbacks, used when the startup
	// probe cannot measure the terminal. ConHost's GDI font chain has no
	// color-emoji glyphs at all: each of these is painted as a single-cell
	// fallback glyph (the status circles famously show as one-cell tofu
	// boxes) while ansi.StringWidth counts 2, hence -1 across the board —
	// the same values JediTerm measured for the shared entries. The
	// circles are additionally substituted with a one-cell drawn glyph at
	// render time (see statusCircle / legacyGlyphFallback), which keeps
	// the real advance at exactly the 1 column this table budgets.
	windowsLegacyGlyphDelta = map[string]int{
		"\U000026AB":  -1, // black circle
		"\U0001F7E2":  -1, // green circle
		"\U0001F534":  -1, // red circle
		"\U0001F7E1":  -1, // yellow circle
		"\U0001F7E0":  -1, // orange circle
		"\U0001F535":  -1, // blue circle
		"\U000026A1":  -1, // lightning
		"\U000023F3":  -1, // hourglass
		"\U00002705":  -1, // check mark
		labelTagEmoji: -1, // label tag + FE0F
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

// isWindowsTerminal reports whether we are running inside Windows Terminal,
// which renders emoji/box drawing predictably with ANSI/VT support.
func isWindowsTerminal() bool {
	return os.Getenv("WT_SESSION") != ""
}

// isWindowsGitBashTerminal reports whether we are running inside Git Bash /
// MSYS2's mintty on Windows, where emoji advance and rendering can differ
// from ansi.StringWidth and cause TUI frame misalignment.
func isWindowsGitBashTerminal() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	return os.Getenv("MSYSTEM") != "" || os.Getenv("TERM_PROGRAM") == "mintty"
}

// isWindowsLegacyConsole reports whether we are running in the classic
// Windows console host (cmd.exe / PowerShell in ConHost), outside Windows
// Terminal and mintty. Its glyph metrics and line clearing behavior differ
// from what ansi.StringWidth and lipgloss assume.
func isWindowsLegacyConsole() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	if isWindowsTerminal() || isWindowsGitBashTerminal() {
		return false
	}
	return true
}

// needsWidthCompensation reports whether the current terminal advances
// some emoji differently from ansi.StringWidth, requiring glyph delta
// measurement/compensation (JetBrains/JediTerm, VS Code integrated terminal,
// Windows legacy console, and Git Bash/mintty on Windows).
func needsWidthCompensation() bool {
	return isJetBrainsTerminal() || isVSCodeTerminal() || isWindowsLegacyConsole() || isWindowsGitBashTerminal()
}

// glyphDeltaFor reports (real advance - ansi.StringWidth) for s in the
// current terminal: negative where the terminal advances a glyph narrower
// than ansi.StringWidth counts (rows are padded out to align), positive
// where it advances wider (rows are trimmed in). Measured values from the
// startup probe take precedence over the static per-terminal fallbacks.
// consoleOutputCPDBCS is set on Windows (console_windows.go) when the
// console output code page is a double-byte East Asian one.
var consoleOutputCPDBCS bool

// dbcsAmbiguousWide lists the East-Asian-Ambiguous characters the UI
// uses that a DBCS-codepage ConHost advances two cells while
// ansi.StringWidth counts one: the select-field arrows, the help
// separators and truncators, and the dashes. A row containing them is
// that many cells wider on screen than every width table in the layout
// stack believes — wide enough to wrap at the right edge, and a wrapped
// row scrolls the frame and desyncs the renderer's line accounting,
// which is what smeared focused form fields into duplicated labels on
// zh-CN cmd.exe (the serial form is all select fields, so it suffered
// most). Over-counting would only leave a harmless gap inside the box;
// under-counting wraps, so these are budgeted at their DBCS advance.
var dbcsAmbiguousWide = []string{
	"\u2190", // leftwards arrow (select fields)
	"\u2192", // rightwards arrow (select fields)
	"\u2191", // upwards arrow (help)
	"\u2193", // downwards arrow (help, sort marks)
	"\u2022", // bullet (help separators)
	"\u2026", // horizontal ellipsis (truncation)
	"\u2014", // em dash
	"\u2013", // en dash
	"\u00b7", // middle dot
	"\u00ab", // left guillemet
	"\u00bb", // right guillemet (also the ⚡ substitute)
	"\u2605", // black star (snippet favorite marker; a GB2312 symbol)
	"\u2606", // white star (GB2312 symbol)
}

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
		} else if isWindowsLegacyConsole() {
			delta += windowsLegacyGlyphDelta[g] * strings.Count(plain, g)
		}
	}
	if isWindowsLegacyConsole() && consoleOutputCPDBCS {
		delta += dbcsAmbiguousDelta(plain)
	}
	return delta
}

// dbcsAmbiguousDelta returns the extra columns a DBCS-codepage ConHost
// spends on the ambiguous-width characters in plain (already stripped
// of ANSI sequences): one per occurrence.
func dbcsAmbiguousDelta(plain string) int {
	delta := 0
	for _, g := range dbcsAmbiguousWide {
		delta += strings.Count(plain, g)
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
// padding appends spaces. On compensation terminals it avoids ansi.Truncate
// because that API measures with StringWidth and can clip early when glyph
// advances differ from ansi.StringWidth.
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

// statusCircle returns the display form of a status circle emoji. On the
// Windows legacy console the color-emoji circles are not in any font
// ConHost can fall back to and render as empty tofu boxes, so they are
// swapped for U+25CF (filled circle, present in the console fonts) tinted
// with the matching ANSI foreground color. The substitute advances
// exactly 1 column, matching windowsLegacyGlyphDelta's budgeting for the
// original emoji; everywhere else the emoji is returned unchanged.
func statusCircle(emoji string) string {
	if !isWindowsLegacyConsole() {
		return emoji
	}
	var color string
	switch emoji {
	case "\U0001F7E2": // green
		color = "10"
	case "\U0001F7E1": // yellow
		color = "11"
	case "\U0001F7E0": // orange
		color = "208"
	case "\U0001F534": // red
		color = "9"
	case "\U0001F535": // blue
		color = "12"
	case "\U000026AB": // black/gray
		color = "8"
	default:
		return emoji
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render("\u25cf")
}

// legacyGlyphFallback rewrites, for the Windows legacy console only, the
// decorative glyphs ConHost cannot draw at all. statusCircle handles the
// status circles at their construction sites (with color); this is the
// safety net for everything else that reaches a frame — picker titles,
// modal headers, i18n strings — mapping each missing glyph to a
// single-cell substitute present in the console fonts, so no tofu boxes
// survive to the screen. All substitutes advance 1 column; the budgeted
// originals are counted as 1 column too via windowsLegacyGlyphDelta
// where they are layout-critical.
func legacyGlyphFallback(s string) string {
	if !isWindowsLegacyConsole() {
		return s
	}
	return legacyGlyphReplacer.Replace(s)
}

var legacyGlyphReplacer = strings.NewReplacer(
	labelTagEmoji, "\u25c6", // label tag -> black diamond
	"\U0001F3F7", "\u25c6", // bare label tag
	"\U000026A1", "\u00bb", // lightning -> »
	"\U000023F3", "\u25cb", // hourglass -> hollow circle
	"\U00002705", "\u2713", // heavy check -> check mark
	"\U0000274C", "\u00d7", // cross mark -> multiplication sign
	"\U00002717", "\u00d7", // ballot X (peek errors) -> multiplication sign
	"\U0001F680", "\u25c6", // rocket -> black diamond
	"\U00002139", "i", // information source
	"\U000026A0", "!", // warning sign
	"\U0001F7E2", "\u25cf", // remaining status circles (uncolored net)
	"\U0001F7E1", "\u25cf",
	"\U0001F7E0", "\u25cf",
	"\U0001F534", "\u25cf",
	"\U0001F535", "\u25cf",
	"\U000026AB", "\u25cf",
	"\U0001F4C1", "\u25b6", // folder -> right triangle
	"\U0001F4C4", "\u2022", // page -> bullet
	"\U0001F310", "\u25c6", // globe -> black diamond
	"\U0001F4BB", "\u25a0", // laptop -> black square
	"\U0001F4D6", "\u2022", // book -> bullet
	"\U0001F517", "\u2022", // link -> bullet
	"\U0001F4E6", "\u25a0", // package -> black square
	"\U00002601", "\u25c6", // cloud -> black diamond
)
