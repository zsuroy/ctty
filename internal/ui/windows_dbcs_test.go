package ui

import (
	"testing"

	"github.com/zsuroy/ctty/internal/i18n"
)

// TestDBCSAmbiguousDelta: on a DBCS-codepage ConHost (GBK 936 etc.)
// the ambiguous-width characters that REMAIN in the frames advance two
// cells, one more than ansi.StringWidth counts. The glyphs that caused
// the worst overflow (← → ↑ ↓ arrows, the bullet and em dash) are
// substituted with ASCII at paint time (legacyGlyphReplacer) and charge
// nothing; the painted emoji substitutes (● ✓ ◆ …) stay in the frames
// and charge one column each.
func TestDBCSAmbiguousDelta(t *testing.T) {
	ResetGlyphCalibration() // the heuristic only charges unmeasured glyphs
	cases := []struct {
		in   string
		want int
	}{
		{"plain ascii", 0},
		{"← CNCA0 →", 0},           // arrows are ASCII-substituted at paint time
		{"↑↓: 移动 • Enter: 连接", 0},  // arrows and bullet are ASCII-substituted
		{"名称 ↓", 0},                // sort mark substituted
		{"（暂无已保存的设备 — 按 a 添加）", 0}, // em dash substituted
		{"truncated…", 0},          // ellipsis is ASCII-substituted at paint time
		{"● status ✓", 2},          // painted circle and check are DBCS-ambiguous
		{"★ favorite snippet", 1},  // black star is a GB2312 symbol: two cells
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

func TestLegacyGlyphFallbackEllipsis(t *testing.T) {
	if got := legacyGlyphReplacer.Replace("存储桶 (Buc…"); got != "存储桶 (Buc." {
		t.Errorf("ellipsis fallback = %q, want %q", got, "存储桶 (Buc.")
	}
}

func TestSearchPlaceholdersAvoidDBCSAmbiguousGlyphs(t *testing.T) {
	keys := []string{
		"search.placeholder",
		"telnet.search_placeholder",
		"serial.search_placeholder",
		"sftp.search_placeholder",
		"s3.sites_search_placeholder",
	}
	for _, lang := range []string{"en", "zh"} {
		i18n.SetLang(lang)
		for _, key := range keys {
			text := i18n.T(key)
			if text == key {
				t.Fatalf("%s: missing i18n key %q", lang, key)
			}
			if d := dbcsAmbiguousDelta(text); d != 0 {
				t.Errorf("%s/%s charges %d extra DBCS cells: %q", lang, key, d, text)
			}
		}
	}
	i18n.SetLang("en")
}
