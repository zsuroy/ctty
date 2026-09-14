package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/connectivity"
	"github.com/zsuroy/ctty/internal/i18n"
)

func TestSwitchProtocolMsgTransitions(t *testing.T) {
	i18n.SetLang("en")
	hosts := []config.SSHHost{{Name: "test-host", Hostname: "1.2.3.4"}}
	m := NewModel(hosts, "", false, "v1.0.0", true)
	m.width = 100
	m.height = 30
	m.styles = NewStyles(100)

	// Initial view is ViewList
	if m.viewMode != ViewList {
		t.Fatalf("expected initial ViewList, got %v", m.viewMode)
	}

	// Switch to ViewSerial
	m1, _ := m.Update(switchProtocolMsg{target: ViewSerial})
	mod1 := m1.(Model)
	if mod1.viewMode != ViewSerial || mod1.serialForm == nil {
		t.Fatalf("expected ViewSerial with serialForm, got %v", mod1.viewMode)
	}

	// Switch directly to ViewTelnet
	m2, _ := mod1.Update(switchProtocolMsg{target: ViewTelnet})
	mod2 := m2.(Model)
	if mod2.viewMode != ViewTelnet || mod2.telnetForm == nil || mod2.serialForm != nil {
		t.Fatalf("expected ViewTelnet with telnetForm, got %v", mod2.viewMode)
	}

	// Switch directly to ViewFTP
	m3, _ := mod2.Update(switchProtocolMsg{target: ViewFTP})
	mod3 := m3.(Model)
	if mod3.viewMode != ViewFTP || mod3.ftpSitesForm == nil || mod3.telnetForm != nil {
		t.Fatalf("expected ViewFTP with ftpSitesForm, got %v", mod3.viewMode)
	}

	// Switch directly to ViewLocalBrowser
	m4, _ := mod3.Update(switchProtocolMsg{target: ViewLocalBrowser})
	mod4 := m4.(Model)
	if mod4.viewMode != ViewLocalBrowser || mod4.localBrowserForm == nil || mod4.ftpSitesForm != nil {
		t.Fatalf("expected ViewLocalBrowser with localBrowserForm, got %v", mod4.viewMode)
	}

	// Switch back to ViewList
	m5, _ := mod4.Update(switchProtocolMsg{target: ViewList})
	mod5 := m5.(Model)
	if mod5.viewMode != ViewList || mod5.localBrowserForm != nil {
		t.Fatalf("expected ViewList with localBrowserForm nil, got %v", mod5.viewMode)
	}
}

func TestDirectKeySwitchingFromSubViews(t *testing.T) {
	i18n.SetLang("en")
	styles := NewStyles(100)

	// 1. Serial form -> T, F, b, ]
	sf := NewSerialForm(styles, 100, 30)
	_, cmd := sf.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	if cmd == nil {
		t.Fatal("expected cmd from T in serial form")
	}
	if msg, ok := cmd().(switchProtocolMsg); !ok || msg.target != ViewTelnet {
		t.Fatalf("expected switchProtocolMsg{ViewTelnet}, got %#v", cmd())
	}

	_, cmd = sf.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'F'}})
	if cmd == nil {
		t.Fatal("expected cmd from F in serial form")
	}
	if msg, ok := cmd().(switchProtocolMsg); !ok || msg.target != ViewFTP {
		t.Fatalf("expected switchProtocolMsg{ViewFTP}, got %#v", cmd())
	}

	_, cmd = sf.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if cmd == nil {
		t.Fatal("expected cmd from b in serial form")
	}
	if msg, ok := cmd().(switchProtocolMsg); !ok || msg.target != ViewLocalBrowser {
		t.Fatalf("expected switchProtocolMsg{ViewLocalBrowser}, got %#v", cmd())
	}

	// 2. Telnet form -> t, F, b, [
	tf := NewTelnetForm(styles, 100, 30)
	_, cmd = tf.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if cmd == nil {
		t.Fatal("expected cmd from t in telnet form")
	}
	if msg, ok := cmd().(switchProtocolMsg); !ok || msg.target != ViewSerial {
		t.Fatalf("expected switchProtocolMsg{ViewSerial}, got %#v", cmd())
	}

	_, cmd = tf.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'F'}})
	if cmd == nil {
		t.Fatal("expected cmd from F in telnet form")
	}
	if msg, ok := cmd().(switchProtocolMsg); !ok || msg.target != ViewFTP {
		t.Fatalf("expected switchProtocolMsg{ViewFTP}, got %#v", cmd())
	}

	// 3. FTP Sites form -> t, T, b, ]
	ff := NewFTPSitesForm(styles, 100, 30)
	_, cmd = ff.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if cmd == nil {
		t.Fatal("expected cmd from t in ftp sites form")
	}
	if msg, ok := cmd().(switchProtocolMsg); !ok || msg.target != ViewSerial {
		t.Fatalf("expected switchProtocolMsg{ViewSerial}, got %#v", cmd())
	}

	_, cmd = ff.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	if cmd == nil {
		t.Fatal("expected cmd from T in ftp sites form")
	}
	if msg, ok := cmd().(switchProtocolMsg); !ok || msg.target != ViewTelnet {
		t.Fatalf("expected switchProtocolMsg{ViewTelnet}, got %#v", cmd())
	}

	// 4. Local Browser form -> t, T, F, ]
	lb := NewLocalBrowserForm(styles, 100, 30, t.TempDir())
	_, cmd = lb.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if cmd == nil {
		t.Fatal("expected cmd from t in local browser form")
	}
	if msg, ok := cmd().(switchProtocolMsg); !ok || msg.target != ViewSerial {
		t.Fatalf("expected switchProtocolMsg{ViewSerial}, got %#v", cmd())
	}

	_, cmd = lb.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'F'}})
	if cmd == nil {
		t.Fatal("expected cmd from F in local browser form")
	}
	if msg, ok := cmd().(switchProtocolMsg); !ok || msg.target != ViewFTP {
		t.Fatalf("expected switchProtocolMsg{ViewFTP}, got %#v", cmd())
	}
}

func TestBracketTabCyclingInListView(t *testing.T) {
	i18n.SetLang("en")
	hosts := []config.SSHHost{{Name: "test-host", Hostname: "1.2.3.4"}}
	m := NewModel(hosts, "", false, "v1.0.0", true)
	m.width = 100
	m.height = 30
	m.styles = NewStyles(100)

	// Press ] in main list -> moves forward to Serial
	m1, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	mod1 := m1.(Model)
	if mod1.viewMode != ViewSerial {
		t.Fatalf("expected ViewSerial after ']', got %v", mod1.viewMode)
	}

	// Reset to ViewList and press [ -> moves backward to Local Browser
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}})
	mod2 := m2.(Model)
	if mod2.viewMode != ViewLocalBrowser {
		t.Fatalf("expected ViewLocalBrowser after '[', got %v", mod2.viewMode)
	}
}

func TestVimAndNumberNavigation(t *testing.T) {
	i18n.SetLang("en")
	hosts := make([]config.SSHHost, 10)
	for i := range hosts {
		hosts[i] = config.SSHHost{Name: fmt.Sprintf("host-%d", i), Hostname: "127.0.0.1"}
	}
	m := NewModel(hosts, "", false, "v1.0.0", true)
	m.width = 100
	m.height = 30
	m.ready = true
	m.styles = NewStyles(100)

	// Press 5 -> cursor goes to row 4
	m1, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	mod1 := m1.(Model)
	if mod1.table.Cursor() != 4 {
		t.Fatalf("expected cursor 4 after pressing 5, got %d", mod1.table.Cursor())
	}

	// Press G -> cursor goes to end (9)
	m2, _ := mod1.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	mod2 := m2.(Model)
	if mod2.table.Cursor() != 9 {
		t.Fatalf("expected cursor 9 after pressing G, got %d", mod2.table.Cursor())
	}

	// Press g -> cursor goes to top (0)
	m3, _ := mod2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	mod3 := m3.(Model)
	if mod3.table.Cursor() != 0 {
		t.Fatalf("expected cursor 0 after pressing g, got %d", mod3.table.Cursor())
	}
}

func TestAddWithInitialNameFromSearch(t *testing.T) {
	i18n.SetLang("en")
	hosts := []config.SSHHost{{Name: "my-server", Hostname: "1.2.3.4"}}
	m := NewModel(hosts, "", false, "v1.0.0", true)
	m.width = 100
	m.height = 30
	m.ready = true
	m.styles = NewStyles(100)

	// Simulate search query with no matching host
	m.searchInput.SetValue("new-database")
	m.searchMode = false

	// Press a -> should transition to ViewAdd with initial name
	m1, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	mod1 := m1.(Model)
	if mod1.viewMode != ViewAdd || mod1.addForm == nil {
		t.Fatalf("expected ViewAdd with addForm, got %v", mod1.viewMode)
	}
	if mod1.addForm.nameVal != "new-database" {
		t.Fatalf("expected pre-filled initial name 'new-database', got %q", mod1.addForm.nameVal)
	}
}

func TestLatencyStatusGradient(t *testing.T) {
	i18n.SetLang("en")
	hosts := []config.SSHHost{
		{Name: "fast", Hostname: "1.1.1.1"},
		{Name: "medium", Hostname: "2.2.2.2"},
		{Name: "slow", Hostname: "3.3.3.3"},
		{Name: "down", Hostname: "4.4.4.4"},
	}
	m := NewModel(hosts, "", false, "v1.0.0", true)
	pm := connectivity.NewPingManager(time.Second, "")
	pm.SetResultForTesting("fast", connectivity.StatusOnline, 20*time.Millisecond)
	pm.SetResultForTesting("medium", connectivity.StatusOnline, 80*time.Millisecond)
	pm.SetResultForTesting("slow", connectivity.StatusOnline, 220*time.Millisecond)
	pm.SetResultForTesting("down", connectivity.StatusOffline, 0)
	m.pingManager = pm

	if got := m.getPingStatusIndicator("fast"); got != "🟢" {
		t.Fatalf("expected 🟢 for <50ms, got %q", got)
	}
	if got := m.getPingStatusIndicator("medium"); got != "🟡" {
		t.Fatalf("expected 🟡 for 50-150ms, got %q", got)
	}
	if got := m.getPingStatusIndicator("slow"); got != "🟠" {
		t.Fatalf("expected 🟠 for >150ms, got %q", got)
	}
	if got := m.getPingStatusIndicator("down"); got != "🔴" {
		t.Fatalf("expected 🔴 for offline, got %q", got)
	}
}

func TestCounterBadgeInSearchBar(t *testing.T) {
	i18n.SetLang("en")
	hosts := []config.SSHHost{
		{Name: "srv-1", Hostname: "1.1.1.1"},
		{Name: "srv-2", Hostname: "2.2.2.2"},
	}
	m := NewModel(hosts, "", false, "v1.0.0", true)
	m.width = 100
	m.height = 30
	m.ready = true
	m.styles = NewStyles(100)

	view := m.View()
	if !strings.Contains(view, "[1/2]") {
		t.Fatalf("expected badge '[1/2]' in view, got: %s", view)
	}

	// Filter
	m.searchInput.SetValue("srv-1")
	m.filteredHosts = m.filterHosts("srv-1")
	filteredView := m.View()
	if !strings.Contains(filteredView, "1/2") || !strings.Contains(filteredView, "matched") {
		t.Fatalf("expected match badge '1/2 matched' in filtered view, got: %s", filteredView)
	}
}
