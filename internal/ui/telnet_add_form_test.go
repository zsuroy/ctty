package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/telnetconfig"
)

func TestTelnetAddForm_Basic(t *testing.T) {
	i18n.SetLang("en")
	styles := NewStyles(80)

	form := newTelnetAddForm(styles, 80, 24, nil)
	if form == nil {
		t.Fatal("expected newTelnetAddForm to return non-nil")
	}

	out := form.View()
	if !strings.Contains(out, "Add Telnet") {
		t.Fatalf("expected View to contain 'Add Telnet', got:\n%s", out)
	}

	// Esc cancels
	_, _ = form.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !form.cancelled {
		t.Fatal("expected form to be cancelled on Esc")
	}
}

func TestTelnetAddForm_EditMode(t *testing.T) {
	i18n.SetLang("en")
	styles := NewStyles(80)

	initial := &telnetconfig.TelnetHost{
		Name: "router-gw",
		Host: "192.168.1.254",
		Port: 2323,
		Tags: []string{"network", "lab"},
	}

	form := newTelnetAddForm(styles, 80, 24, initial)
	if form.nameVal != "router-gw" {
		t.Fatalf("expected nameVal router-gw, got %s", form.nameVal)
	}
	if form.portVal != "2323" {
		t.Fatalf("expected portVal 2323, got %s", form.portVal)
	}

	out := form.View()
	if !strings.Contains(out, "Edit Telnet") {
		t.Fatalf("expected View to contain 'Edit Telnet', got:\n%s", out)
	}
}

func TestTelnetAddForm_TabNavigation(t *testing.T) {
	styles := NewStyles(80)
	form := newTelnetAddForm(styles, 80, 24, nil)

	// Focus initially on first field (name)
	if form.form.GetFocusedField().GetKey() != "name" {
		t.Fatalf("expected focus on name, got %s", form.form.GetFocusedField().GetKey())
	}

	// Tab advances
	_, _ = form.Update(tea.KeyMsg{Type: tea.KeyTab})
	if form.form.GetFocusedField().GetKey() != "host" {
		t.Fatalf("expected focus on host, got %s", form.form.GetFocusedField().GetKey())
	}

	// Shift+Tab returns
	_, _ = form.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if form.form.GetFocusedField().GetKey() != "name" {
		t.Fatalf("expected focus back on name, got %s", form.form.GetFocusedField().GetKey())
	}
}

func TestTelnetAddForm_Submit(t *testing.T) {
	styles := NewStyles(80)
	form := newTelnetAddForm(styles, 80, 24, nil)
	_ = form.Init()

	// Enter name
	form.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("myrouter")})
	// Press Enter to go to host
	form.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if form.form.GetFocusedField().GetKey() != "host" {
		t.Fatalf("expected focus on host, got %s", form.form.GetFocusedField().GetKey())
	}

	// Enter host
	form.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("10.0.0.1")})
	// Press Enter to go to port
	form.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if form.form.GetFocusedField().GetKey() != "port" {
		t.Fatalf("expected focus on port, got %s", form.form.GetFocusedField().GetKey())
	}

	// Press Enter to go to tags
	form.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if form.form.GetFocusedField().GetKey() != "tags" {
		t.Fatalf("expected focus on tags, got %s", form.form.GetFocusedField().GetKey())
	}

	// Enter tags
	form.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("lab,cisco")})
	// Press Enter to go to confirm
	form.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if form.form.GetFocusedField().GetKey() != "confirm" {
		t.Fatalf("expected focus on confirm, got %s", form.form.GetFocusedField().GetKey())
	}

	// Now press Enter on confirm
	form.Update(tea.KeyMsg{Type: tea.KeyEnter})
	t.Logf("done: %v, cancelled: %v, err: %q, nameVal: %q, hostVal: %q, tagsVal: %q, confirmVal: %v",
		form.done, form.cancelled, form.err, form.nameVal, form.hostVal, form.tagsVal, form.confirmVal)
}

func TestTelnetAddForm_ConfirmNavigationAndAutoScroll(t *testing.T) {
	i18n.SetLang("zh")
	styles := NewStyles(80)
	// Short terminal height to force viewport scrolling
	form := newTelnetAddForm(styles, 80, 14, nil)
	_ = form.Init()

	// Advance through all fields to confirm: name -> host -> port -> tags -> confirm
	for i := 0; i < 4; i++ {
		form.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	if form.form.GetFocusedField().GetKey() != "confirm" {
		t.Fatalf("expected focus on confirm, got %s", form.form.GetFocusedField().GetKey())
	}

	// View should have auto-scrolled so Save button is visible
	view := form.View()
	if !strings.Contains(view, i18n.T("form.btn_save")) {
		t.Fatalf("expected view to contain Save button, got:\n%s", view)
	}

	// Up key on confirm should move back to tags
	form.Update(tea.KeyMsg{Type: tea.KeyUp})
	if form.form.GetFocusedField().GetKey() != "tags" {
		t.Fatalf("expected Up on confirm to go to tags, got %s", form.form.GetFocusedField().GetKey())
	}

	// Down key on tags should move back to confirm
	form.Update(tea.KeyMsg{Type: tea.KeyDown})
	if form.form.GetFocusedField().GetKey() != "confirm" {
		t.Fatalf("expected Down on tags to go to confirm, got %s", form.form.GetFocusedField().GetKey())
	}

	// Down key on confirm should wrap around to name
	form.Update(tea.KeyMsg{Type: tea.KeyDown})
	if form.form.GetFocusedField().GetKey() != "name" {
		t.Fatalf("expected Down on confirm to wrap to name, got %s", form.form.GetFocusedField().GetKey())
	}
}

func TestTelnetAddForm_ValidationErrorUsesTelnetLocale(t *testing.T) {
	i18n.SetLang("zh")
	styles := NewStyles(80)
	form := newTelnetAddForm(styles, 80, 24, nil)
	_ = form.Init()

	// Submit with empty name and host
	form.submit()
	if form.err != i18n.T("telnet.err_name_host_req") {
		t.Fatalf("expected telnet error %q, got %q", i18n.T("telnet.err_name_host_req"), form.err)
	}
	if strings.Contains(form.err, "站点") {
		t.Fatalf("telnet error should not mention FTP site '站点': %s", form.err)
	}
}

