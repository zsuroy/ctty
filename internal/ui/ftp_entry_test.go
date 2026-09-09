package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/ftpclient"
	"github.com/zsuroy/ctty/internal/i18n"
)

func TestMainListFOpensFTPSites(t *testing.T) {
	i18n.SetLang("en")
	hosts := []config.SSHHost{{Name: "h1", Hostname: "example.com"}}
	m := NewModel(hosts, "", false, "v0.6.3", true)
	m.ready = true
	m.width, m.height = 100, 30
	m.viewMode = ViewList

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'F'}})
	um := updated.(Model)
	if um.viewMode != ViewFTP {
		t.Fatalf("viewMode=%v want ViewFTP", um.viewMode)
	}
	if um.ftpSitesForm == nil {
		t.Fatal("ftpSitesForm not created")
	}

	// Lowercase f must remain port-forward, not FTP.
	m2 := NewModel(hosts, "", false, "v0.6.3", true)
	m2.ready = true
	m2.width, m2.height = 100, 30
	m2.viewMode = ViewList
	m2.updateTableRows()
	updated, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	um = updated.(Model)
	if um.viewMode == ViewFTP {
		t.Fatal("lowercase f must not open FTP")
	}
}

func TestMainHelpAndHelpFormIncludeFTPKey(t *testing.T) {
	i18n.SetLang("en")
	if !strings.Contains(i18n.T("main.help"), "F: ftp") {
		t.Fatalf("main.help missing F: ftp: %q", i18n.T("main.help"))
	}
	i18n.SetLang("zh")
	if !strings.Contains(i18n.T("main.help"), "F: FTP") {
		t.Fatalf("zh main.help missing F: FTP: %q", i18n.T("main.help"))
	}
	i18n.SetLang("en")
	help := NewHelpForm(NewStyles(100), 100, 40, "v0.6.3")
	view := help.View()
	if !strings.Contains(view, "F") || !strings.Contains(view, "FTP") {
		t.Fatalf("help form missing F/FTP entry: %q", view)
	}
}

// TestFTPSitesTableFitsTerminalWidth verifies the site-manager table never
// renders wider than the terminal at any width (bubbles cell padding + border
// + app padding are budgeted inside column widths, mirroring serial/telnet).
func TestFTPSitesTableFitsTerminalWidth(t *testing.T) {
	i18n.SetLang("en")
	for _, width := range []int{20, 24, 30, 40, 50, 60, 70, 73, 74, 79, 80, 81, 90, 100, 120, 160, 200} {
		t.Run("width", func(t *testing.T) {
			m := NewFTPSitesForm(NewStyles(width), width, 24)
			view := m.View()
			for _, line := range strings.Split(view, "\n") {
				if w := lineDisplayWidth(line); w > width {
					t.Errorf("width=%d: rendered line %d cols exceeds terminal\n%q", width, w, line)
				}
			}
		})
	}
}

// TestFTPBrowserSearchBarAbovePanes verifies the FTP browser search bar
// renders above the panes like every other view's search (host list, serial,
// telnet, SFTP, FTP sites), not at the bottom of the screen.
func TestFTPBrowserSearchBarAbovePanes(t *testing.T) {
	i18n.SetLang("en")
	m := NewFTPForm(NewStyles(100), 100, 30, "site")
	m.client = &ftpclient.Client{}
	m.loading = false
	m.mode = ftpBrowse
	m.searchMode = true
	m.cwd = "/"
	m.entries = []ftpclient.RemoteEntry{{Name: "a.txt", IsDir: false, Size: 4}}
	m.refreshLocal()
	m.updateRemoteRows()

	view := ansi.Strip(m.View())
	searchIdx := strings.Index(view, "╭") // rounded search bar chrome
	tableIdx := strings.Index(view, "┌")  // pane border chrome
	if searchIdx < 0 || tableIdx < 0 || searchIdx > tableIdx {
		t.Fatalf("search bar must render above the panes: search at %d, panes at %d\n%s", searchIdx, tableIdx, view)
	}
}

func TestFTPBrowserSearchPreservesPaneFocus(t *testing.T) {
	i18n.SetLang("en")
	m := NewFTPForm(NewStyles(100), 100, 30, "site")
	m.client = &ftpclient.Client{}
	m.loading = false
	m.mode = ftpBrowse
	m.cwd = "/"
	m.entries = []ftpclient.RemoteEntry{
		{Name: "readme.txt", Size: 4},
		{Name: "notes.txt", Size: 5},
	}
	m.refreshLocal()
	m.updateRemoteRows()

	if !m.remoteTbl.Focused() || m.localTbl.Focused() {
		t.Fatal("remote pane should start focused")
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(*ftpFormModel)
	if !m.focusLocal || !m.localTbl.Focused() || m.remoteTbl.Focused() {
		t.Fatal("Tab should switch focus to the local pane")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(*ftpFormModel)
	if m.focusLocal || !m.remoteTbl.Focused() || m.localTbl.Focused() {
		t.Fatal("Shift+Tab should switch focus to the remote pane")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(*ftpFormModel)
	if !m.searchMode || !m.searchInput.Focused() || m.remoteTbl.Focused() || m.localTbl.Focused() {
		t.Fatal("search should own focus while both panes are blurred")
	}

	for _, r := range "readme" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(*ftpFormModel)
	}
	if got := len(m.filteredRemote()); got != 1 || m.filteredRemote()[0].Name != "readme.txt" {
		t.Fatalf("remote search results = %#v, want readme.txt", m.filteredRemote())
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*ftpFormModel)
	if m.searchMode || m.searchInput.Focused() || !m.remoteTbl.Focused() || m.localTbl.Focused() {
		t.Fatal("Enter should return focus to the previously active remote pane")
	}
}
