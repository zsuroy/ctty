package ui

import (
	"strings"
	"testing"

	"github.com/zsuroy/ctty/internal/i18n"
)

func firstNonEmptyLine(view string) string {
	for _, line := range strings.Split(view, "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}

func TestHelpFormTopBorderVisibleOnSmallHeight(t *testing.T) {
	i18n.SetLang("en")
	for _, h := range []int{10, 12, 14, 16, 18, 20, 24} {
		for _, w := range []int{40, 60, 80, 100} {
			help := NewHelpForm(NewStyles(w), w, h, "v0.5.0")
			view := help.View()
			first := firstNonEmptyLine(view)
			if !strings.Contains(first, "╭") {
				t.Errorf("w=%d h=%d: top border not visible, first line=%q", w, h, first)
			}
			lines := strings.Split(view, "\n")
			if len(lines) > h {
				t.Errorf("w=%d h=%d: view has %d lines, exceeds terminal height %d", w, h, len(lines), h)
			}
		}
	}
}

func TestHelpFormIncludesV1Shortcuts(t *testing.T) {
	for _, lang := range []string{"en", "zh"} {
		i18n.SetLang(lang)
		help := NewHelpForm(NewStyles(120), 120, 50, "v1.0.0")
		view := help.View()

		expectedKeys := []string{
			"v/P", "Space", "^A", "y", "[/]", "w", "c", "g/G", "1-9", "E",
		}
		for _, k := range expectedKeys {
			if !strings.Contains(view, k) {
				t.Errorf("[%s] help form missing shortcut %q:\n%s", lang, k, view)
			}
		}

		expectedDescriptions := []string{
			i18n.T("help.peek"),
			i18n.T("help.multi_select"),
			i18n.T("help.select_all"),
			i18n.T("help.copy_cmd"),
			i18n.T("help.protocol_tab"),
			i18n.T("help.tag_drawer"),
			i18n.T("help.clear_tag"),
			i18n.T("help.jump_top_bottom"),
			i18n.T("help.quick_jump"),
			i18n.T("help.edit_config"),
		}
		for _, desc := range expectedDescriptions {
			if !strings.Contains(view, desc) {
				t.Errorf("[%s] help form missing description %q:\n%s", lang, desc, view)
			}
		}
	}
	i18n.SetLang("en")
}
