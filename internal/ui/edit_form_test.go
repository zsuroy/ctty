package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zsuroy/ctty/internal/i18n"
)

func TestNewEditForm(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config")
	sshContent := "Host server1\n  HostName 10.0.0.1\n  User root\n  Port 2222\n"
	if err := os.WriteFile(cfgPath, []byte(sshContent), 0600); err != nil {
		t.Fatal(err)
	}

	styles := NewStyles(80)
	form, err := NewEditForm("server1", styles, 80, 24, cfgPath)
	if err != nil {
		t.Fatalf("failed to create edit form: %v", err)
	}

	if form.hostNamesVal != "server1" {
		t.Errorf("expected hostNamesVal 'server1', got %q", form.hostNamesVal)
	}
	if form.hostnameVal != "10.0.0.1" {
		t.Errorf("expected hostnameVal '10.0.0.1', got %q", form.hostnameVal)
	}
	if form.userVal != "root" {
		t.Errorf("expected userVal 'root', got %q", form.userVal)
	}
	if form.portVal != "2222" {
		t.Errorf("expected portVal '2222', got %q", form.portVal)
	}

	view := form.View()
	if view == "" {
		t.Fatal("expected non-empty edit form view")
	}
}

func TestEditFormRendering(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config")
	sshContent := "Host test-host\n  HostName 192.168.1.1\n"
	if err := os.WriteFile(cfgPath, []byte(sshContent), 0600); err != nil {
		t.Fatal(err)
	}

	for _, lang := range []string{i18n.LangZHCN, i18n.LangEN} {
		t.Run(lang, func(t *testing.T) {
			i18n.Init(lang)
			styles := NewStyles(80)
			form, err := NewEditForm("test-host", styles, 90, 30, cfgPath)
			if err != nil {
				t.Fatalf("failed to create edit form: %v", err)
			}
			view := form.View()
			if view == "" {
				t.Fatal("expected non-empty view")
			}

			title := i18n.T("form.edit_title")
			if !strings.Contains(view, title) {
				t.Errorf("expected view to contain title %q", title)
			}
		})
	}
}

func TestEditFormCancel(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config")
	sshContent := "Host cancel-host\n  HostName 192.168.1.1\n"
	if err := os.WriteFile(cfgPath, []byte(sshContent), 0600); err != nil {
		t.Fatal(err)
	}

	styles := NewStyles(80)
	form, err := NewEditForm("cancel-host", styles, 80, 24, cfgPath)
	if err != nil {
		t.Fatalf("failed to create edit form: %v", err)
	}

	_, cmd := form.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected cancel cmd on Esc")
	}
	msg := cmd()
	if _, ok := msg.(editFormCancelMsg); !ok {
		t.Errorf("expected editFormCancelMsg, got %T", msg)
	}
}

func TestEditForm_TabNavigation(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config")
	sshContent := "Host nav-host\n  HostName 192.168.1.1\n"
	if err := os.WriteFile(cfgPath, []byte(sshContent), 0600); err != nil {
		t.Fatal(err)
	}

	styles := NewStyles(80)
	form, err := NewEditForm("nav-host", styles, 80, 24, cfgPath)
	if err != nil {
		t.Fatalf("failed to create edit form: %v", err)
	}
	_ = form.Init()

	if form.form.GetFocusedField().GetKey() != "hosts" {
		t.Fatalf("expected initial focus on hosts, got %s", form.form.GetFocusedField().GetKey())
	}

	// Tab advances to hostname
	_, _ = form.Update(tea.KeyMsg{Type: tea.KeyTab})
	if form.form.GetFocusedField().GetKey() != "hostname" {
		t.Fatalf("expected focus on hostname, got %s", form.form.GetFocusedField().GetKey())
	}

	// Shift+Tab returns to hosts
	_, _ = form.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if form.form.GetFocusedField().GetKey() != "hosts" {
		t.Fatalf("expected focus back on hosts, got %s", form.form.GetFocusedField().GetKey())
	}

	// Shift+Tab on hosts loops to confirm
	_, _ = form.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if form.form.GetFocusedField().GetKey() != "confirm" {
		t.Fatalf("expected loop to confirm, got %s", form.form.GetFocusedField().GetKey())
	}

	// Tab on confirm loops to hosts
	_, _ = form.Update(tea.KeyMsg{Type: tea.KeyTab})
	if form.form.GetFocusedField().GetKey() != "hosts" {
		t.Fatalf("expected loop to hosts, got %s", form.form.GetFocusedField().GetKey())
	}
}

func TestEditForm_BottomBorderAlwaysVisible(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config")
	sshContent := "Host border-host\n  HostName 192.168.1.1\n"
	if err := os.WriteFile(cfgPath, []byte(sshContent), 0600); err != nil {
		t.Fatal(err)
	}

	styles := NewStyles(80)
	heights := []int{16, 20, 24, 30}

	for _, h := range heights {
		form, err := NewEditForm("border-host", styles, 80, h, cfgPath)
		if err != nil {
			t.Fatalf("h=%d: failed to create edit form: %v", h, err)
		}
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
