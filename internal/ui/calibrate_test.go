package ui

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// fakeProbeTTY answers DSR queries: every write ending in ESC[6n is scanned
// for the probed glyph and answered with a cursor report whose column
// encodes the configured real advance for it (defaulting to what
// ansi.StringWidth measures, so unconfigured glyphs produce no delta).
type fakeProbeTTY struct {
	writeBuf bytes.Buffer
	readBuf  bytes.Buffer
	advances map[string]int
}

func (f *fakeProbeTTY) Write(p []byte) (int, error) {
	f.writeBuf.Write(p)
	if !bytes.HasSuffix(p, []byte("\x1b[6n")) {
		return len(p), nil
	}
	s := f.writeBuf.String()
	i := strings.LastIndex(s, "\x1b[1;1HAB")
	if i < 0 {
		return len(p), nil
	}
	rest := s[i+len("\x1b[1;1HAB"):]
	j := strings.Index(rest, "CD\x1b[6n")
	if j < 0 {
		return len(p), nil
	}
	glyph := rest[:j]
	advance, ok := f.advances[glyph]
	if !ok {
		advance = ansi.StringWidth(glyph)
	}
	fmt.Fprintf(&f.readBuf, "\x1b[1;%dR", 5+advance)
	return len(p), nil
}

func (f *fakeProbeTTY) Read(p []byte) (int, error) {
	if f.readBuf.Len() == 0 {
		return 0, io.EOF
	}
	return f.readBuf.Read(p)
}

// silentTTY accepts writes and never answers, like a terminal without
// cursor-position report support.
type silentTTY struct{}

func (silentTTY) Write(p []byte) (int, error) { return len(p), nil }
func (silentTTY) Read(p []byte) (int, error)  { return 0, io.EOF }

func TestReadCursorPositionParsing(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantRow int
		wantCol int
	}{
		{"plain", "\x1b[1;5R", 1, 5},
		{"multi-digit", "\x1b[22;171R", 22, 171},
		{"leading noise", "\x1b[?1;2c\x1b[3;40R", 3, 40},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row, col, err := readCursorPosition(strings.NewReader(tc.input), time.Second)
			if err != nil {
				t.Fatalf("readCursorPosition: %v", err)
			}
			if row != tc.wantRow || col != tc.wantCol {
				t.Fatalf("readCursorPosition = (%d,%d) want (%d,%d)", row, col, tc.wantRow, tc.wantCol)
			}
		})
	}
}

func TestReadCursorPositionTimeout(t *testing.T) {
	if _, _, err := readCursorPosition(strings.NewReader(""), 50*time.Millisecond); err == nil {
		t.Fatal("expected timeout error on empty reader")
	}
}

func TestCalibrateGlyphAdvancesMeasuresDeltas(t *testing.T) {
	ResetGlyphCalibration()
	t.Cleanup(ResetGlyphCalibration)

	// A terminal where the label tag advances one wider than
	// ansi.StringWidth (the PyCharm measurement) and the status circle one
	// narrower; everything else matches the measurement.
	fake := &fakeProbeTTY{advances: map[string]int{
		labelTagEmoji:  ansi.StringWidth(labelTagEmoji) + 1,
		"\U000026AB":   ansi.StringWidth("\U000026AB") - 1,
	}}
	if !CalibrateGlyphAdvances(fake) {
		t.Fatal("CalibrateGlyphAdvances failed against a responsive fake terminal")
	}
	if got := glyphDeltaOverride[labelTagEmoji]; got != 1 {
		t.Fatalf("label tag delta=%d want 1", got)
	}
	if got := glyphDeltaOverride["\U000026AB"]; got != -1 {
		t.Fatalf("black circle delta=%d want -1", got)
	}
	// Measured values must win over the static per-terminal fallback.
	t.Setenv("TERMINAL_EMULATOR", "JetBrains-JediTerm")
	if got, want := terminalDisplayWidth(labelTagEmoji), ansi.StringWidth(labelTagEmoji)+1; got != want {
		t.Fatalf("terminalDisplayWidth(label)=%d want %d", got, want)
	}
	if got, want := terminalDisplayWidth("\U000026AB"), ansi.StringWidth("\U000026AB")-1; got != want {
		t.Fatalf("terminalDisplayWidth(circle)=%d want %d", got, want)
	}
}

func TestCalibrateGlyphAdvancesFallsBackOnSilentTerminal(t *testing.T) {
	ResetGlyphCalibration()
	t.Cleanup(ResetGlyphCalibration)

	if CalibrateGlyphAdvances(silentTTY{}) {
		t.Fatal("expected calibration to fail against a silent terminal")
	}
	if len(glyphDeltaOverride) != 0 {
		t.Fatalf("glyphDeltaOverride=%v want empty", glyphDeltaOverride)
	}
	// The static JetBrains fallback still applies when nothing was measured.
	t.Setenv("TERMINAL_EMULATOR", "JetBrains-JediTerm")
	if got, want := terminalDisplayWidth(labelTagEmoji), ansi.StringWidth(labelTagEmoji)+1; got != want {
		t.Fatalf("terminalDisplayWidth(label)=%d want %d from static fallback", got, want)
	}
}

// TestLayoutGlyphsProbeExactTitleSequence guards the exact-string
// requirement: the drawer title writes U+1F3F7 + U+FE0F, and probing or
// counting the bare U+1F3F7 measures the wrong string (the variation
// selector takes a visible cell on the IDE terminals).
func TestLayoutGlyphsProbeExactTitleSequence(t *testing.T) {
	var haveExact, haveBare bool
	for _, g := range layoutGlyphs {
		switch g {
		case labelTagEmoji:
			haveExact = true
		case "\U0001F3F7":
			haveBare = true
		}
	}
	if !haveExact {
		t.Fatal("layoutGlyphs missing the exact label-tag sequence (U+1F3F7+U+FE0F)")
	}
	if haveBare {
		t.Fatal("layoutGlyphs still contains the bare U+1F3F7 — it would double-count the title sequence")
	}

	// The delta must reach a row that renders the exact sequence.
	t.Setenv("TERMINAL_EMULATOR", "JetBrains-JediTerm")
	title := "  " + labelTagEmoji + "  Tag Filter"
	if got := glyphDeltaFor(title); got != 1 {
		t.Fatalf("glyphDeltaFor(title)=%d want 1", got)
	}
}
