package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/sftpconfig"
)

func newSFTPManageTestForm(t *testing.T) *sftpFormModel {
	t.Helper()
	i18n.SetLang("en")
	m := NewSFTPForm(NewStyles(100), 100, 30, "host", "")
	m.client = &sftpconfig.SFTPClient{}
	m.loading = false
	m.ready = true
	m.mode = sftpBrowse
	m.cwd = "/"
	m.localCwd = t.TempDir()
	m.entries = []sftpconfig.RemoteEntry{
		{Name: "a.bin", Size: 10},
		{Name: "docs", IsDir: true},
	}
	m.filteredEntries = m.entries
	m.updateTableRows()
	m.table.SetCursor(0)
	return m
}

func typeSFTPRunes(t *testing.T, m *sftpFormModel, s string) *sftpFormModel {
	t.Helper()
	updated := tea.Model(m)
	for _, r := range s {
		updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	fm, ok := updated.(*sftpFormModel)
	if !ok {
		t.Fatal("Update did not return *sftpFormModel")
	}
	return fm
}

func enterSFTPUpload(t *testing.T, m *sftpFormModel) *sftpFormModel {
	t.Helper()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	fm := updated.(*sftpFormModel)
	if fm.mode != sftpUploadSelect {
		t.Fatalf("mode = %v, want upload select", fm.mode)
	}
	return fm
}

func TestSFTPRenameFlow(t *testing.T) {
	m := newSFTPManageTestForm(t)
	m.table.SetCursor(1) // directories sort first; a.bin is second

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	fm := updated.(*sftpFormModel)
	if fm.mode != sftpRenameInput {
		t.Fatalf("mode = %v, want rename input", fm.mode)
	}
	if fm.inputBuffer != "a.bin" {
		t.Fatalf("inputBuffer = %q, want prefilled a.bin", fm.inputBuffer)
	}
	for range len("a.bin") {
		updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		fm = updated.(*sftpFormModel)
	}
	fm = typeSFTPRunes(t, fm, "b.bin")
	updated, cmd := fm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm = updated.(*sftpFormModel)
	if cmd == nil {
		t.Fatal("expected rename cmd after enter")
	}

	updated, cmd = fm.Update(sftpRenameResultMsg{oldName: "a.bin", newName: "b.bin", success: true})
	fm = updated.(*sftpFormModel)
	if fm.mode != sftpBrowse {
		t.Fatalf("mode = %v, want browse", fm.mode)
	}
	if cmd == nil {
		t.Fatal("expected refresh cmd after rename success")
	}
	if !strings.Contains(fm.statusMsg, "a.bin") || !strings.Contains(fm.statusMsg, "b.bin") {
		t.Fatalf("status = %q, want both names", fm.statusMsg)
	}
}

func TestSFTPRenameUnchangedCancels(t *testing.T) {
	m := newSFTPManageTestForm(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	fm := updated.(*sftpFormModel)
	updated, cmd := fm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm = updated.(*sftpFormModel)
	if fm.mode != sftpBrowse {
		t.Fatalf("mode = %v, want browse", fm.mode)
	}
	if cmd != nil {
		t.Fatal("unchanged name must not produce a cmd")
	}
}

func TestSFTPBrowserInfoOverlay(t *testing.T) {
	m := newSFTPManageTestForm(t)
	m.table.SetCursor(1) // directories sort first; a.bin is second

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	fm := updated.(*sftpFormModel)
	if !fm.showInfo {
		t.Fatal("i must open entry info")
	}
	view := fm.View()
	for _, needle := range []string{"a.bin", "10B", "/a.bin"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("info view missing %q", needle)
		}
	}

	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	fm = updated.(*sftpFormModel)
	if fm.showInfo {
		t.Fatal("esc must close entry info")
	}
}

func TestSFTPUploadInfoOverlay(t *testing.T) {
	m := newSFTPManageTestForm(t)
	f, err := os.Create(filepath.Join(m.localCwd, "up.bin"))
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	fm := enterSFTPUpload(t, m)

	updated, _ := fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	fm = updated.(*sftpFormModel)
	if !fm.showInfo {
		t.Fatal("i must open local entry info")
	}
	if view := fm.View(); !strings.Contains(view, "up.bin") {
		t.Fatal("info view must show local filename")
	}
}

func TestSFTPLocalMkdirFlow(t *testing.T) {
	m := newSFTPManageTestForm(t)
	fm := enterSFTPUpload(t, m)

	updated, _ := fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	fm = updated.(*sftpFormModel)
	if fm.mode != sftpMkdirInput || !fm.localOp {
		t.Fatalf("mode = %v localOp = %v, want mkdir input targeting local", fm.mode, fm.localOp)
	}
	fm = typeSFTPRunes(t, fm, "newdir")
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm = updated.(*sftpFormModel)
	if fm.mode != sftpUploadSelect {
		t.Fatalf("mode = %v, want upload select after local mkdir", fm.mode)
	}
	st, err := os.Stat(filepath.Join(m.localCwd, "newdir"))
	if err != nil || !st.IsDir() {
		t.Fatalf("local dir not created: %v", err)
	}
}

func TestSFTPLocalDeleteFlow(t *testing.T) {
	m := newSFTPManageTestForm(t)
	target := filepath.Join(m.localCwd, "gone.bin")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	fm := enterSFTPUpload(t, m)

	updated, _ := fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	fm = updated.(*sftpFormModel)
	if fm.mode != sftpDeleteConfirm || !fm.localOp {
		t.Fatalf("mode = %v localOp = %v, want local delete confirm", fm.mode, fm.localOp)
	}
	if !strings.Contains(fm.View(), "gone.bin") {
		t.Fatal("delete confirm must show the filename")
	}
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	fm = updated.(*sftpFormModel)
	if fm.mode != sftpUploadSelect {
		t.Fatalf("mode = %v, want upload select after local delete", fm.mode)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("local file must be deleted")
	}
}

func TestSFTPLocalRenameFlow(t *testing.T) {
	m := newSFTPManageTestForm(t)
	oldPath := filepath.Join(m.localCwd, "old.bin")
	if err := os.WriteFile(oldPath, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	fm := enterSFTPUpload(t, m)

	updated, _ := fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	fm = updated.(*sftpFormModel)
	if fm.mode != sftpRenameInput || !fm.localOp {
		t.Fatalf("mode = %v localOp = %v, want local rename input", fm.mode, fm.localOp)
	}
	for range len("old.bin") {
		updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		fm = updated.(*sftpFormModel)
	}
	fm = typeSFTPRunes(t, fm, "new.bin")
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm = updated.(*sftpFormModel)
	if fm.mode != sftpUploadSelect {
		t.Fatalf("mode = %v, want upload select after local rename", fm.mode)
	}
	if _, err := os.Stat(filepath.Join(m.localCwd, "new.bin")); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
}

func TestSFTPManageKeysBlockedWhenBusy(t *testing.T) {
	m := newSFTPManageTestForm(t)
	m.transferring = true
	for _, key := range []rune{'R', 'i', 'n'} {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if fm := updated.(*sftpFormModel); fm.mode != sftpBrowse {
			t.Fatalf("key %q must be ignored while transferring", key)
		}
	}
}

func TestSFTPFrameFitsTerminal(t *testing.T) {
	i18n.SetLang("en")
	for _, size := range [][2]int{{100, 24}, {100, 30}, {120, 40}} {
		w, h := size[0], size[1]
		for _, upload := range []bool{false, true} {
			m := NewSFTPForm(NewStyles(w), w, h, "host", "")
			m.client = &sftpconfig.SFTPClient{}
			m.loading = false
			m.ready = true
			m.mode = sftpBrowse
			m.cwd = "/"
			m.localCwd = t.TempDir()
			m.entries = []sftpconfig.RemoteEntry{{Name: "a.bin", Size: 10}}
			m.filteredEntries = m.entries
			m.updateTableRows()
			if upload {
				m.mode = sftpUploadSelect
				m.localFiles = m.listLocalFiles()
				m.updateLocalTableRows()
			}
			view := m.View()
			if got := lipgloss.Height(view); got > h {
				t.Fatalf("w=%d h=%d upload=%v: frame height %d exceeds terminal", w, h, upload, got)
			}
		}
	}
}
