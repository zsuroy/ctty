package ui

import (
	"testing"

	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/ui/theme"
)

func TestThemeIntegration(t *testing.T) {
	// 1. Test NewModel with Catppuccin theme configured
	hosts := []config.SSHHost{
		{Name: "test-server", Hostname: "10.0.0.1"},
	}
	cfg := config.GetDefaultAppConfig()
	cfg.Theme = "catppuccin"

	m := NewModel(hosts, "", false, "v0.8.0", true)
	m.appConfig = &cfg
	th := theme.GetTheme("catppuccin")
	m.styles = ApplyTheme(th)

	if m.styles.Theme.ID != "catppuccin" {
		t.Fatalf("expected styles theme to be 'catppuccin', got %q", m.styles.Theme.ID)
	}

	// 2. Test dynamic theme change
	dracula := theme.GetTheme("dracula")
	m.styles = ApplyTheme(dracula)
	if m.styles.Theme.ID != "dracula" {
		t.Fatalf("expected styles theme to be 'dracula', got %q", m.styles.Theme.ID)
	}

	// 3. Test View rendering with theme
	m.ready = true
	m.width = 80
	m.height = 24
	view := m.View()
	if view == "" {
		t.Fatal("expected non-empty view")
	}
}
