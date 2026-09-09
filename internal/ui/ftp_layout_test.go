package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/ftpclient"
	"github.com/zsuroy/ctty/internal/i18n"
)

func isolateAppConfigDir(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("APPDATA", tmp)
}

func TestFTPLayoutTogglePersists(t *testing.T) {
	i18n.SetLang("en")
	isolateAppConfigDir(t)

	m := NewFTPFormWithLayout(NewStyles(100), 100, 30, "site", config.FTPLayoutDual)
	m.client = &ftpclient.Client{}
	m.loading = false
	m.mode = ftpBrowse

	vKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}}
	updated, _ := m.Update(vKey)
	fm := updated.(*ftpFormModel)
	if fm.layout != config.FTPLayoutSingle {
		t.Fatalf("layout after v = %q, want single", fm.layout)
	}
	saved, err := config.LoadAppConfig()
	if err != nil {
		t.Fatalf("LoadAppConfig: %v", err)
	}
	if saved.FTPLayout != config.FTPLayoutSingle {
		t.Fatalf("persisted layout = %q, want single", saved.FTPLayout)
	}

	updated, _ = fm.Update(vKey)
	fm = updated.(*ftpFormModel)
	if fm.layout != config.FTPLayoutDual {
		t.Fatalf("layout after second v = %q, want dual", fm.layout)
	}
	saved, err = config.LoadAppConfig()
	if err != nil {
		t.Fatalf("LoadAppConfig: %v", err)
	}
	if saved.FTPLayout != config.FTPLayoutDual {
		t.Fatalf("persisted layout = %q, want dual", saved.FTPLayout)
	}
}

func TestFTPOpenBrowserRespectsConfiguredLayout(t *testing.T) {
	i18n.SetLang("en")
	single := config.AppConfig{FTPLayout: config.FTPLayoutSingle}
	m := Model{
		appConfig: &single,
		styles:    NewStyles(100),
		width:     100,
		height:    30,
	}
	updated, _ := m.Update(ftpOpenBrowserMsg{siteName: "site"})
	um := updated.(Model)
	if um.ftpForm == nil {
		t.Fatal("ftpForm not created")
	}
	if um.ftpForm.layout != config.FTPLayoutSingle {
		t.Fatalf("browser layout = %q, want single", um.ftpForm.layout)
	}
}
