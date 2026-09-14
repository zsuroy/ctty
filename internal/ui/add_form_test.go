package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zsuroy/ctty/internal/i18n"
)

func TestNewAddForm(t *testing.T) {
	styles := NewStyles(80)
	form := NewAddForm("my-server", styles, 80, 24, "")

	if form.nameVal != "my-server" {
		t.Errorf("expected nameVal 'my-server', got %q", form.nameVal)
	}
	if form.portVal != "22" {
		t.Errorf("expected portVal '22', got %q", form.portVal)
	}
	if form.form == nil {
		t.Fatal("expected huh.Form to be initialized")
	}

	view := form.View()
	if view == "" {
		t.Fatal("expected non-empty view")
	}
}

func TestAddFormRendering(t *testing.T) {
	for _, lang := range []string{i18n.LangZHCN, i18n.LangEN} {
		t.Run(lang, func(t *testing.T) {
			i18n.Init(lang)
			styles := NewStyles(80)
			form := NewAddForm("", styles, 90, 30, "")
			view := form.View()
			if view == "" {
				t.Fatal("expected non-empty view")
			}

			title := i18n.T("form.add_title")
			if !strings.Contains(view, title) {
				t.Errorf("expected view to contain title %q", title)
			}
		})
	}
}

func TestAddFormCancel(t *testing.T) {
	styles := NewStyles(80)
	form := NewAddForm("", styles, 80, 24, "")

	_, cmd := form.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected cancel cmd on Esc")
	}
	msg := cmd()
	if _, ok := msg.(addFormCancelMsg); !ok {
		t.Errorf("expected addFormCancelMsg, got %T", msg)
	}
}

func TestAddForm_TabNavigation(t *testing.T) {
	styles := NewStyles(80)
	form := NewAddForm("", styles, 80, 24, "")
	_ = form.Init()

	if form.form.GetFocusedField().GetKey() != "name" {
		t.Fatalf("expected initial focus on name, got %s", form.form.GetFocusedField().GetKey())
	}

	// Tab advances to hostname
	_, _ = form.Update(tea.KeyMsg{Type: tea.KeyTab})
	if form.form.GetFocusedField().GetKey() != "hostname" {
		t.Fatalf("expected focus on hostname, got %s", form.form.GetFocusedField().GetKey())
	}

	// Shift+Tab returns to name
	_, _ = form.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if form.form.GetFocusedField().GetKey() != "name" {
		t.Fatalf("expected focus back on name, got %s", form.form.GetFocusedField().GetKey())
	}

	// Shift+Tab on name loops to confirm
	_, _ = form.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if form.form.GetFocusedField().GetKey() != "confirm" {
		t.Fatalf("expected loop to confirm, got %s", form.form.GetFocusedField().GetKey())
	}

	// Tab on confirm loops to name
	_, _ = form.Update(tea.KeyMsg{Type: tea.KeyTab})
	if form.form.GetFocusedField().GetKey() != "name" {
		t.Fatalf("expected loop to name, got %s", form.form.GetFocusedField().GetKey())
	}
}

func TestAddForm_BottomBorderAlwaysVisible(t *testing.T) {
	styles := NewStyles(80)
	heights := []int{16, 20, 24, 30}

	for _, h := range heights {
		form := NewAddForm("", styles, 80, h, "")
		_ = form.Init()
		view := form.View()

		lines := strings.Split(view, "\n")
		if len(lines) > h {
			t.Errorf("h=%d: rendered %d lines, exceeds terminal height %d", h, len(lines), h)
		}

		if !strings.Contains(view, "╰") || !strings.Contains(view, "╯") {
			t.Errorf("h=%d: bottom border corners missing in view:\n%s", h, view)
		}
	}
}
