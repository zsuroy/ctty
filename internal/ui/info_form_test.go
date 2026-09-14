package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/ui/theme"
)

func TestInfoFormRendering(t *testing.T) {
	i18n.Init("en")
	th := theme.DraculaTheme
	styles := NewStylesWithTheme(th)

	m := &infoFormModel{
		hostName: "test-host",
		host: config.SSHHost{
			Name:     "test-host",
			Hostname: "192.168.1.100",
			User:     "admin",
			Port:     "2222",
			Tags:     []string{"prod", "web"},
		},
		styles: styles,
		width:  80,
		height: 24,
	}

	view := m.View()
	if view == "" {
		t.Fatal("expected non-empty info view")
	}

	// Must contain top and bottom border corners
	if !strings.Contains(view, "╭") {
		t.Error("missing top border corner (╭)")
	}
	if !strings.Contains(view, "╰") {
		t.Error("missing bottom border corner (╰)")
	}

	// Must contain host details
	if !strings.Contains(view, "test-host") {
		t.Error("missing host name")
	}
	if !strings.Contains(view, "192.168.1.100") {
		t.Error("missing hostname IP")
	}

	// View must not exceed terminal height
	lines := strings.Split(view, "\n")
	if len(lines) > 24 {
		t.Errorf("expected <= 24 lines, got %d", len(lines))
	}
}

func TestInfoFormKeyNavigation(t *testing.T) {
	m := &infoFormModel{
		hostName: "test-host",
		host: config.SSHHost{
			Name: "test-host",
		},
		styles: NewStyles(80),
		width:  80,
		height: 24,
	}

	// Test edit key
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if cmd == nil {
		t.Fatal("expected edit cmd")
	}
	msg := cmd()
	if editMsg, ok := msg.(infoFormEditMsg); !ok || editMsg.hostName != "test-host" {
		t.Errorf("expected editMsg with test-host, got %+v", msg)
	}

	// Test cancel keys (Esc, q, i)
	for _, key := range []string{"esc", "q", "i"} {
		var keyMsg tea.KeyMsg
		if key == "esc" {
			keyMsg = tea.KeyMsg{Type: tea.KeyEsc}
		} else {
			keyMsg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
		}
		_, cancelCmd := m.Update(keyMsg)
		if cancelCmd == nil {
			t.Fatalf("expected cancel cmd for key %s", key)
		}
		cancelMsg := cancelCmd()
		if _, ok := cancelMsg.(infoFormCancelMsg); !ok {
			t.Errorf("expected infoFormCancelMsg for key %s, got %+v", key, cancelMsg)
		}
	}
}
