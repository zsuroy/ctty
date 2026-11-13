package ui

import "testing"

// TestDBCSAmbiguousDelta: on a DBCS-codepage ConHost (GBK 936 etc.)
// the ambiguous-width characters the UI uses advance two cells, one
// more than ansi.StringWidth counts. The delta must charge exactly
// one extra column per occurrence — under-charging wraps rows at the
// right edge (the duplicated form labels on zh-CN cmd.exe), and the
// arrows of a focused select field are the most common carriers.
func TestDBCSAmbiguousDelta(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"plain ascii", 0},
		{"← CNCA0 →", 2},        // focused select field row
		{"↑↓: 移动 • Enter: 连接", 3}, // help line: two arrows + bullet
		{"名称 ↓", 1},             // sort mark in a table header
		{"（暂无已保存的设备 — 按 a 添加）", 1}, // em dash in the telnet empty row
		{"truncated…", 1}, // ellipsis
		{"● status ✓", 0}, // filled circle and check are NOT ambiguous-wide
		{"★ favorite snippet", 1}, // black star is a GB2312 symbol: two cells
	}
	for _, c := range cases {
		if got := dbcsAmbiguousDelta(c.in); got != c.want {
			t.Errorf("dbcsAmbiguousDelta(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestLegacyGlyphFallbackBallotX: the peek error row prefixes its
// message with U+2717 (✗), which ConHost fonts do not carry — it
// reached the screen as a tofu box. The legacy fallback replacer must
// map it to the multiplication sign like the other cross marks.
func TestLegacyGlyphFallbackBallotX(t *testing.T) {
	if got := legacyGlyphReplacer.Replace("  \u2717 failed"); got != "  \u00d7 failed" {
		t.Errorf("ballot X fallback = %q, want %q", got, "  \u00d7 failed")
	}
}
