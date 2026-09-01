package ui

import (
	"strings"
	"testing"

	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/version"
)

func TestPercentFromMessage(t *testing.T) {
	tests := []struct {
		msg  string
		want int
	}{
		{"Downloading ctty_Darwin_arm64.tar.gz ... 1.2 MB / 4.5 MB (27%)", 27},
		{"Downloading x ... 0%", 0},
		{"Downloading x ... 100%", 100}, // trailing bare token
		{"Verifying checksum ...", -1},
		{"Checksum OK", -1},
		{"garbage (abc%)", -1},
	}
	for _, tt := range tests {
		if got := percentFromMessage(tt.msg); got != tt.want {
			t.Errorf("percentFromMessage(%q) = %d, want %d", tt.msg, got, tt.want)
		}
	}
}

func TestProgressBar(t *testing.T) {
	if got := progressBar(50, 10); got != "[=====     ]  50%" {
		t.Errorf("progressBar(50,10) = %q", got)
	}
	if got := progressBar(-1, 5); got != "[     ]" {
		t.Errorf("indeterminate bar = %q", got)
	}
	if got := progressBar(100, 5); got != "[=====] 100%" {
		t.Errorf("full bar = %q", got)
	}
}

func TestUpdateModalShowsReleaseURL(t *testing.T) {
	i18n.SetLang("en")
	releaseURL := "https://github.com/zsuroy/ctty/releases/tag/v0.6.0"
	form := NewUpdateForm(NewStyles(80), 80, 24, "v0.5.0", "v0.6.0", releaseURL)
	view := form.View()
	if !strings.Contains(view, releaseURL) {
		t.Errorf("update modal missing release URL:\n%s", view)
	}
	if strings.Contains(view, "Release:") {
		t.Errorf("update modal should not prefix the URL with a label:\n%s", view)
	}
}

func TestHelpFormShowsRepoURL(t *testing.T) {
	i18n.SetLang("en")
	h := NewHelpForm(NewStyles(100), 100, 40, "v0.5.0")
	view := h.View()
	if !strings.Contains(view, version.RepoURL()) {
		t.Errorf("help form missing repo URL %q:\n%s", version.RepoURL(), view)
	}
}
