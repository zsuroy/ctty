package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/ui/theme"
)

// The height budget must measure header parts AFTER width-wrapping;
// unwrapped they undercount and the bottom border gets clipped out of the
// terminal (previously at 44 cols).
func TestPortForwardFormKeepsBottomBorderAtAnyWidth(t *testing.T) {
	i18n.Init("en")
	styles := NewStylesWithTheme(theme.DefaultTheme)
	for _, w := range []int{24, 30, 40, 44, 48, 60, 80, 120} {
		pf := NewPortForwardForm("server1", styles, w, 24, "", nil)
		_ = pf.Init()
		view := strings.Split(pf.View(), "\n")
		if len(view) > 24 {
			t.Fatalf("width %d: frame is %d rows, exceeds terminal", w, len(view))
		}
		found := false
		for _, ln := range view {
			if strings.Contains(ln, "╯") {
				found = true
			}
		}
		if !found {
			t.Fatalf("width %d: bottom border missing/clipped:\n%s", w, strings.Join(view, "\n"))
		}
	}
}

func TestPortForwardFormInit(t *testing.T) {
	i18n.Init("en")
	styles := NewStylesWithTheme(theme.DefaultTheme)
	pf := NewPortForwardForm("my-server", styles, 80, 24, "", nil)

	if pf.hostName != "my-server" {
		t.Errorf("expected hostName my-server, got %q", pf.hostName)
	}
	if pf.typeVal != "local" {
		t.Errorf("expected default typeVal local, got %q", pf.typeVal)
	}

	view := pf.View()
	if view == "" {
		t.Fatal("expected non-empty view")
	}

	// Must contain borders
	if !strings.Contains(view, "╭") || !strings.Contains(view, "╰") {
		t.Error("view missing border corners")
	}

	// View must not exceed terminal height
	lines := strings.Split(view, "\n")
	if len(lines) > 24 {
		t.Errorf("expected <= 24 lines, got %d", len(lines))
	}
}

func TestPortForwardFormSubmit(t *testing.T) {
	styles := NewStyles(80)
	pf := NewPortForwardForm("server1", styles, 80, 24, "/path/to/ssh_config", nil)
	pf.localPortVal = "8080"
	pf.remoteHostVal = "localhost"
	pf.remotePortVal = "80"

	// Local forward test
	cmd := pf.submitForm()
	if cmd == nil {
		t.Fatal("expected submit cmd")
	}
	msg := cmd()
	submitMsg, ok := msg.(portForwardSubmitMsg)
	if !ok || submitMsg.err != nil {
		t.Fatalf("expected successful submitMsg, got %+v", msg)
	}

	expectedArgs := []string{"-F", "/path/to/ssh_config", "-L", "8080:localhost:80", "server1"}
	if len(submitMsg.sshArgs) != len(expectedArgs) {
		t.Fatalf("expected args %v, got %v", expectedArgs, submitMsg.sshArgs)
	}
	for i, arg := range expectedArgs {
		if submitMsg.sshArgs[i] != arg {
			t.Errorf("arg %d: expected %q, got %q", i, arg, submitMsg.sshArgs[i])
		}
	}
}

func TestPortForwardFormDynamicSubmit(t *testing.T) {
	styles := NewStyles(80)
	pf := NewPortForwardForm("server2", styles, 80, 24, "", nil)
	pf.typeVal = "dynamic"
	pf.localPortVal = "1080"

	cmd := pf.submitForm()
	msg := cmd()
	submitMsg, ok := msg.(portForwardSubmitMsg)
	if !ok || submitMsg.err != nil {
		t.Fatalf("expected successful submitMsg, got %+v", msg)
	}

	expectedArgs := []string{"-D", "1080", "server2"}
	if len(submitMsg.sshArgs) != len(expectedArgs) {
		t.Fatalf("expected args %v, got %v", expectedArgs, submitMsg.sshArgs)
	}
	for i, arg := range expectedArgs {
		if submitMsg.sshArgs[i] != arg {
			t.Errorf("arg %d: expected %q, got %q", i, arg, submitMsg.sshArgs[i])
		}
	}
}

func TestPortForwardFormCancel(t *testing.T) {
	styles := NewStyles(80)
	pf := NewPortForwardForm("server1", styles, 80, 24, "", nil)

	_, cmd := pf.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected cancel cmd")
	}
	msg := cmd()
	if _, ok := msg.(portForwardCancelMsg); !ok {
		t.Errorf("expected portForwardCancelMsg, got %+v", msg)
	}
}

func TestPortForwardFormTab(t *testing.T) {
	styles := NewStyles(80)
	pf := NewPortForwardForm("server1", styles, 80, 24, "", nil)
	_ = pf.Init()

	if pf.form.GetFocusedField().GetKey() != "type" {
		t.Fatalf("expected initial focus 'type', got %q", pf.form.GetFocusedField().GetKey())
	}

	// 1st Tab: local_port
	pf, _ = pf.Update(tea.KeyMsg{Type: tea.KeyTab})
	if pf.form.GetFocusedField().GetKey() != "local_port" {
		t.Fatalf("expected focus 'local_port' after 1st Tab, got %q", pf.form.GetFocusedField().GetKey())
	}

	// 2nd Tab: remote_host (this previously lost cursor / lost focus!)
	pf, _ = pf.Update(tea.KeyMsg{Type: tea.KeyTab})
	if pf.form.GetFocusedField().GetKey() != "remote_host" {
		t.Fatalf("expected focus 'remote_host' after 2nd Tab, got %q", pf.form.GetFocusedField().GetKey())
	}

	// 3rd Tab: remote_port
	pf, _ = pf.Update(tea.KeyMsg{Type: tea.KeyTab})
	if pf.form.GetFocusedField().GetKey() != "remote_port" {
		t.Fatalf("expected focus 'remote_port' after 3rd Tab, got %q", pf.form.GetFocusedField().GetKey())
	}

	// 4th Tab: bind_address
	pf, _ = pf.Update(tea.KeyMsg{Type: tea.KeyTab})
	if pf.form.GetFocusedField().GetKey() != "bind_address" {
		t.Fatalf("expected focus 'bind_address' after 4th Tab, got %q", pf.form.GetFocusedField().GetKey())
	}

	// 5th Tab: confirm
	pf, _ = pf.Update(tea.KeyMsg{Type: tea.KeyTab})
	if pf.form.GetFocusedField().GetKey() != "confirm" {
		t.Fatalf("expected focus 'confirm' after 5th Tab, got %q", pf.form.GetFocusedField().GetKey())
	}

	// 6th Tab: loop back to type
	pf, _ = pf.Update(tea.KeyMsg{Type: tea.KeyTab})
	if pf.form.GetFocusedField().GetKey() != "type" {
		t.Fatalf("expected focus loop to 'type', got %q", pf.form.GetFocusedField().GetKey())
	}

	// Shift+Tab on type: loop to confirm
	pf, _ = pf.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if pf.form.GetFocusedField().GetKey() != "confirm" {
		t.Fatalf("expected Shift+Tab loop to 'confirm', got %q", pf.form.GetFocusedField().GetKey())
	}
}

func TestPortForwardForm_BottomBorderAlwaysVisible(t *testing.T) {
	styles := NewStyles(80)
	heights := []int{16, 20, 24, 30}

	for _, h := range heights {
		pf := NewPortForwardForm("server1", styles, 80, h, "", nil)
		_ = pf.Init()
		view := pf.View()

		lines := strings.Split(view, "\n")
		if len(lines) > h {
			t.Errorf("h=%d: rendered %d lines, exceeds terminal height %d", h, len(lines), h)
		}

		if !strings.Contains(view, "╰") || !strings.Contains(view, "╯") {
			t.Errorf("h=%d: bottom border corners missing in view:\n%s", h, view)
		}
	}
}

func TestPortForwardForm_RoleTitles(t *testing.T) {
	i18n.SetLang("zh")
	styles := NewStyles(80)

	// Test Local role titles
	pfLocal := NewPortForwardForm("my-srv", styles, 80, 24, "", nil)
	pfLocal.typeVal = "local"
	pfLocal.buildForm()
	viewLocal := pfLocal.View()

	if !strings.Contains(viewLocal, "本地监听") {
		t.Errorf("expected local forward view to contain '本地监听', got:\n%s", viewLocal)
	}
	if !strings.Contains(viewLocal, "远程目标") {
		t.Errorf("expected local forward view to contain '远程目标', got:\n%s", viewLocal)
	}

	// Test Remote role titles
	pfRemote := NewPortForwardForm("my-srv", styles, 80, 24, "", nil)
	pfRemote.typeVal = "remote"
	pfRemote.buildForm()
	viewRemote := pfRemote.View()

	if !strings.Contains(viewRemote, "远程监听") {
		t.Errorf("expected remote forward view to contain '远程监听', got:\n%s", viewRemote)
	}
	if !strings.Contains(viewRemote, "本地目标") {
		t.Errorf("expected remote forward view to contain '本地目标', got:\n%s", viewRemote)
	}

	// Test Dynamic role titles
	pfDynamic := NewPortForwardForm("my-srv", styles, 80, 24, "", nil)
	pfDynamic.typeVal = "dynamic"
	pfDynamic.buildForm()
	viewDynamic := pfDynamic.View()

	if !strings.Contains(viewDynamic, "SOCKS5") {
		t.Errorf("expected dynamic forward view to contain 'SOCKS5', got:\n%s", viewDynamic)
	}
}

func TestPortForwardForm_NoRawI18nKeys(t *testing.T) {
	for _, lang := range []string{"zh", "en"} {
		i18n.SetLang(lang)
		styles := NewStyles(80)

		// 1. Full view test (height=40)
		pfFull := NewPortForwardForm("server1", styles, 80, 40, "", nil)
		_ = pfFull.Init()
		viewFull := pfFull.View()

		if strings.Contains(viewFull, "pf.") {
			t.Errorf("lang=%s: full view contains raw i18n key 'pf.':\n%s", lang, viewFull)
		}
		if lang == "zh" {
			if !strings.Contains(viewFull, "启动转发") || !strings.Contains(viewFull, "取消") {
				t.Errorf("zh view missing localized action buttons: %s", viewFull)
			}
			if !strings.Contains(viewFull, "确认启动端口转发？") {
				t.Errorf("zh view missing localized confirm prompt: %s", viewFull)
			}
		} else {
			if !strings.Contains(viewFull, "Start Forwarding") || !strings.Contains(viewFull, "Cancel") {
				t.Errorf("en view missing localized action buttons: %s", viewFull)
			}
			if !strings.Contains(viewFull, "Start port forwarding?") {
				t.Errorf("en view missing localized confirm prompt: %s", viewFull)
			}
		}

		// 2. Compact view test with tabbing to confirm field (height=24)
		pfCompact := NewPortForwardForm("server1", styles, 80, 24, "", nil)
		_ = pfCompact.Init()
		// Tab to confirm (type -> local_port -> remote_host -> remote_port -> bind_address -> confirm)
		for i := 0; i < 5; i++ {
			pfCompact, _ = pfCompact.Update(tea.KeyMsg{Type: tea.KeyTab})
		}
		if pfCompact.form.GetFocusedField().GetKey() != "confirm" {
			t.Fatalf("expected focus on confirm, got %q", pfCompact.form.GetFocusedField().GetKey())
		}
		viewScrolled := pfCompact.View()
		if strings.Contains(viewScrolled, "pf.") {
			t.Errorf("lang=%s: scrolled view contains raw i18n key 'pf.':\n%s", lang, viewScrolled)
		}
		if lang == "zh" {
			if !strings.Contains(viewScrolled, "启动转发") || !strings.Contains(viewScrolled, "取消") {
				t.Errorf("zh scrolled view missing localized action buttons: %s", viewScrolled)
			}
			if !strings.Contains(viewScrolled, "确认启动端口转发？") {
				t.Errorf("zh scrolled view missing localized confirm prompt: %s", viewScrolled)
			}
		} else {
			if !strings.Contains(viewScrolled, "Start Forwarding") || !strings.Contains(viewScrolled, "Cancel") {
				t.Errorf("en scrolled view missing localized action buttons: %s", viewScrolled)
			}
			if !strings.Contains(viewScrolled, "Start port forwarding?") {
				t.Errorf("en scrolled view missing localized confirm prompt: %s", viewScrolled)
			}
		}
	}
}
