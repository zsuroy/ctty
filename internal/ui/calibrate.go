package ui

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// labelTagEmoji is the drawer title glyph exactly as the UI writes it:
// U+1F3F7 followed by the emoji variation selector U+FE0F. It must be
// probed and counted as this exact sequence — the two-codepoint grapheme
// can advance differently from the bare U+1F3F7 (measured: the selector
// takes a visible cell on the IDE terminals), and probing or counting the
// bare prefix measures the wrong string.
const labelTagEmoji = "\U0001F3F7\ufe0f"

// layoutGlyphs are every glyph whose real column advance the layout depends
// on: the status emoji in the tables above plus the picker marks and help
// symbols. The label tag is swapped for its exact two-codepoint form (see
// labelTagEmoji). All of them are measured at startup.
var layoutGlyphs = func() []string {
	glyphs := make([]string, 0, len(statusEmoji)+5)
	for _, g := range statusEmoji {
		if g == "\U0001F3F7" {
			glyphs = append(glyphs, labelTagEmoji)
			continue
		}
		glyphs = append(glyphs, g)
	}
	return append(glyphs,
		"\U000025CF", // filled circle (picker mark)
		"\U000025CB", // hollow circle (picker mark)
		"\U00002191", // up arrow (help)
		"\U00002193", // down arrow (help)
		"\U00002022", // bullet (help)
	)
}()

// glyphDeltaOverride holds measured (real advance - ansi.StringWidth) per
// glyph for the current terminal, filled by CalibrateGlyphAdvances.
var glyphDeltaOverride = map[string]int{}

// CalibrateGlyphAdvances measures the real column advance of every
// layout-critical glyph in the terminal behind tty and records how it
// differs from ansi.StringWidth. This replaces per-terminal guesswork: a
// glyph advancing narrower than measured gets a negative delta (rows are
// padded out to align), one advancing wider gets a positive delta (rows are
// trimmed in) — measured facts instead of tables that have to be right by
// luck. Returns false when the terminal does not answer cursor-position
// reports, leaving the static per-terminal fallbacks in place.
//
// It must run before the Bubble Tea program starts, while the process still
// owns the terminal exclusively.
func CalibrateGlyphAdvances(tty io.ReadWriter) bool {
	for _, g := range layoutGlyphs {
		advance, err := measureGlyphAdvance(tty, g)
		if err != nil {
			return false
		}
		if delta := advance - ansi.StringWidth(g); delta != 0 {
			glyphDeltaOverride[g] = delta
		}
	}
	return true
}

// MeasureGlyphAdvance reports the real column advance of one glyph in the
// terminal behind tty, via a DSR (ESC[6n) cursor-position report.
func MeasureGlyphAdvance(tty io.ReadWriter, glyph string) (int, error) {
	return measureGlyphAdvance(tty, glyph)
}

// ProbeCursorPosition writes content from the home position and reports the
// resulting cursor row and column, via a DSR (ESC[6n) cursor-position
// report. Used to measure glyph advances (bracket the glyph with known
// single-column characters) and the terminal's real wrap width.
func ProbeCursorPosition(tty io.ReadWriter, content string) (row, col int, err error) {
	if _, err := io.WriteString(tty, "\x1b[1;1H"+content+"\x1b[6n"); err != nil {
		return 0, 0, err
	}
	return readCursorPosition(tty, 2*time.Second)
}

// measureGlyphAdvance brackets the glyph with four known single-column
// characters, so its advance is the reported column minus five.
func measureGlyphAdvance(tty io.ReadWriter, glyph string) (int, error) {
	_, col, err := ProbeCursorPosition(tty, "AB"+glyph+"CD")
	if err != nil {
		return 0, err
	}
	return col - 5, nil
}

// readCursorPosition reads one cursor-position report "\x1b[<row>;<col>R"
// from r, tolerating leading noise before the escape sequence.
func readCursorPosition(r io.Reader, timeout time.Duration) (row, col int, err error) {
	type res struct {
		row, col int
		err      error
	}
	ch := make(chan res, 1)
	go func() {
		br := bufio.NewReader(r)
		var buf []byte
		for {
			b, e := br.ReadByte()
			if e != nil {
				ch <- res{err: e}
				return
			}
			buf = append(buf, b)
			if b != 'R' {
				continue
			}
			s := string(buf)
			start := strings.LastIndex(s, "\x1b[")
			if start < 0 {
				continue // noise without an escape sequence; keep reading
			}
			body := strings.TrimSuffix(strings.TrimPrefix(s[start:], "\x1b["), "R")
			parts := strings.SplitN(body, ";", 2)
			if len(parts) != 2 {
				ch <- res{err: fmt.Errorf("malformed cursor report %q", s)}
				return
			}
			r1, e1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			c1, e2 := strconv.Atoi(strings.TrimSpace(parts[1]))
			if e1 != nil || e2 != nil {
				ch <- res{err: fmt.Errorf("malformed cursor report %q", s)}
				return
			}
			ch <- res{row: r1, col: c1}
			return
		}
	}()
	select {
	case r := <-ch:
		return r.row, r.col, r.err
	case <-time.After(timeout):
		return 0, 0, fmt.Errorf("timed out waiting for cursor report")
	}
}

// ResetGlyphCalibration clears measured deltas, restoring the static
// per-terminal fallbacks. Used by tests.
func ResetGlyphCalibration() {
	glyphDeltaOverride = map[string]int{}
}
