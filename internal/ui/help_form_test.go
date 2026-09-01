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
