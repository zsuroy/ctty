package cmd

import (
	"testing"
)

// TestProbeGlyphsCoverLayoutGlyphs guards the diagnostic's coverage: the
// drawer title and the site status circle are the two glyphs whose advance
// has been misjudged per terminal, and a mismatch between real advance and
// StringWidth in either one is what corrupts frames.
func TestProbeGlyphsCoverLayoutGlyphs(t *testing.T) {
	var haveLabel, haveCircle bool
	for _, g := range probeGlyphs {
		switch g.s {
		case "\U0001F3F7\ufe0f":
			haveLabel = true
		case "\U000026AB":
			haveCircle = true
		}
	}
	if !haveLabel || !haveCircle {
		t.Fatalf("probe glyphs missing label=%v circle=%v", haveLabel, haveCircle)
	}
}
