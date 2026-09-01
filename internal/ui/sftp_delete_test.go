package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/sftpconfig"
)

func TestSFTPDeleteConfirmLeavesOnYes(t *testing.T) {
	i18n.SetLang("en")
	m := NewSFTPForm(NewStyles(80), 80, 24, "host", "")
	m.client = &sftpconfig.SFTPClient{}
	m.ready = true
	m.cwd = "/tmp"
	m.mode = sftpDeleteConfirm
	m.selectedEntry = &sftpconfig.RemoteEntry{Name: "dockerview"}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	sm := updated.(*sftpFormModel)
	if sm.mode == sftpDeleteConfirm {
		t.Fatal("confirm dialog must close as soon as Y is pressed")
	}
	if strings.Contains(sm.View(), "Delete 'dockerview'") {
		t.Fatalf("confirm still visible:\n%s", sm.View())
	}
}

func TestSFTPDeleteSuccessHidesConfirmPrompt(t *testing.T) {
	i18n.SetLang("en")
	m := NewSFTPForm(NewStyles(80), 80, 24, "host", "")
	m.client = &sftpconfig.SFTPClient{}
	m.ready = true
	m.cwd = "/tmp"
	m.mode = sftpDeleteConfirm
	m.selectedEntry = &sftpconfig.RemoteEntry{Name: "dockerview"}

	_, _ = m.Update(sftpDeleteResultMsg{filename: "dockerview", success: true})
	if m.mode != sftpBrowse {
		t.Fatalf("mode=%v, want browse after delete", m.mode)
	}
	view := m.View()
	if strings.Contains(view, "Delete 'dockerview'") {
		t.Fatalf("confirm stacked on success:\n%s", view)
	}
	if !strings.Contains(view, "dockerview") || !strings.Contains(strings.ToLower(view), "deleted") {
		t.Fatalf("missing delete success status:\n%s", view)
	}
}
