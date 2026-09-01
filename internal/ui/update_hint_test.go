package ui

import (
	"strings"
	"testing"

	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/version"
)

func TestUpdateBannerIncludesPressUHint(t *testing.T) {
	m := NewModel(nil, "", false, "v0.5.0", true)
	m.ready = true
	m.width = 100
	m.height = 24
	m.updateInfo = &version.UpdateInfo{
		Available:  true,
		CurrentVer: "v0.5.0",
		LatestVer:  "v0.6.0",
	}

	cases := []struct {
		lang string
		want string
	}{
		{"en", "press U"},
		{"zh_CN", "按 U"},
	}
	for _, tt := range cases {
		i18n.SetLang(tt.lang)
		view := m.View()
		if !strings.Contains(view, tt.want) {
			t.Errorf("lang=%s: update banner missing %q hint\n%s", tt.lang, tt.want, view)
		}
		if !strings.Contains(view, "v0.5.0") || !strings.Contains(view, "v0.6.0") {
			t.Errorf("lang=%s: banner missing version numbers", tt.lang)
		}
	}
}

func TestUpdateBannerHiddenWhenNoUpdate(t *testing.T) {
	i18n.SetLang("en")
	m := NewModel(nil, "", false, "v0.5.0", true)
	m.ready = true
	m.width = 100
	m.height = 24

	view := m.View()
	if strings.Contains(view, "press U") {
		t.Errorf("press-U hint shown when no update is available:\n%s", view)
	}
}

func TestHelpFormIncludesUpdateKey(t *testing.T) {
	i18n.SetLang("en")
	h := NewHelpForm(NewStyles(100), 100, 40, "v0.5.0")
	view := h.View()
	if !strings.Contains(view, i18n.T("help.update")) {
		t.Errorf("help form missing self-update shortcut:\n%s", view)
	}
}
