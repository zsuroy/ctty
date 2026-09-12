package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/config"
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
	m.sortEntries()
	m.refreshLocal()
	m.updateRemoteRows()
	m.updateLocalRows()
	m.remoteTbl.SetCursor(0)
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

func TestSFTPLayoutTogglePersists(t *testing.T) {
	i18n.SetLang("en")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	m := NewSFTPFormWithLayout(NewStyles(100), 100, 30, "host", "", config.SFTPLayoutDual)
	m.client = &sftpconfig.SFTPClient{}
	m.loading = false
	m.mode = sftpBrowse

	vKey := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}}
	updated, _ := m.Update(vKey)
	fm := updated.(*sftpFormModel)
	if fm.layout != config.SFTPLayoutSingle {
		t.Fatalf("layout after v = %q, want single", fm.layout)
	}
	saved, err := config.LoadAppConfig()
	if err != nil {
		t.Fatalf("LoadAppConfig: %v", err)
	}
	if saved.SFTPLayout != config.SFTPLayoutSingle {
		t.Fatalf("persisted layout = %q, want single", saved.SFTPLayout)
	}

	updated, _ = fm.Update(vKey)
	fm = updated.(*sftpFormModel)
	if fm.layout != config.SFTPLayoutDual {
		t.Fatalf("layout after second v = %q, want dual", fm.layout)
	}
}

func TestSFTPOpenBrowserRespectsConfiguredLayout(t *testing.T) {
	i18n.SetLang("en")
	form := NewSFTPFormWithLayout(NewStyles(100), 100, 30, "host", "", config.SFTPLayoutSingle)
	if form.layout != config.SFTPLayoutSingle {
		t.Fatalf("browser layout = %q, want single", form.layout)
	}
}

func TestSFTPRenameFlow(t *testing.T) {
	m := newSFTPManageTestForm(t)
	m.remoteTbl.SetCursor(1) // directories sort first; a.bin is second

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
	m.remoteTbl.SetCursor(1)

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

func TestSFTPLocalInfoOverlay(t *testing.T) {
	m := newSFTPManageTestForm(t)
	f, err := os.Create(filepath.Join(m.localCwd, "up.bin"))
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	m.refreshLocal()
	m.updateLocalRows()
	m.setFocusLocal(true)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	fm := updated.(*sftpFormModel)
	if !fm.showInfo {
		t.Fatal("i must open local entry info")
	}
	if view := fm.View(); !strings.Contains(view, "up.bin") {
		t.Fatal("info view must show local filename")
	}
}

func TestSFTPLocalMkdirFlow(t *testing.T) {
	m := newSFTPManageTestForm(t)
	m.setFocusLocal(true)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	fm := updated.(*sftpFormModel)
	if fm.mode != sftpMkdirInput || !fm.localOp {
		t.Fatalf("mode = %v localOp = %v, want mkdir input targeting local", fm.mode, fm.localOp)
	}
	fm = typeSFTPRunes(t, fm, "newdir")
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm = updated.(*sftpFormModel)
	if fm.mode != sftpLocalBrowse {
		t.Fatalf("mode = %v, want local browse after local mkdir", fm.mode)
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
	m.refreshLocal()
	m.updateLocalRows()
	m.setFocusLocal(true)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	fm := updated.(*sftpFormModel)
	if fm.mode != sftpDeleteConfirm || !fm.localOp {
		t.Fatalf("mode = %v localOp = %v, want local delete confirm", fm.mode, fm.localOp)
	}
	if !strings.Contains(fm.View(), "gone.bin") {
		t.Fatal("delete confirm must show the filename")
	}
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	fm = updated.(*sftpFormModel)
	if fm.mode != sftpLocalBrowse {
		t.Fatalf("mode = %v, want local browse after local delete", fm.mode)
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
	m.refreshLocal()
	m.updateLocalRows()
	m.setFocusLocal(true)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	fm := updated.(*sftpFormModel)
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
	if fm.mode != sftpLocalBrowse {
		t.Fatalf("mode = %v, want local browse after local rename", fm.mode)
	}
	if _, err := os.Stat(filepath.Join(m.localCwd, "new.bin")); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
}

func TestSFTPManageKeysBlockedWhenBusy(t *testing.T) {
	m := newSFTPManageTestForm(t)
	m.transferring = true
	for _, key := range []rune{'n', 'd', 'R', 'i', 'v'} {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if fm := updated.(*sftpFormModel); fm.mode != sftpBrowse {
			t.Fatalf("key %q must be ignored while transferring", key)
		}
	}
}

func TestSFTPPaneSwitching(t *testing.T) {
	m := newSFTPManageTestForm(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	fm := updated.(*sftpFormModel)
	if !fm.focusLocal || fm.mode != sftpLocalBrowse {
		t.Fatal("Tab must switch focus to the local pane")
	}

	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyTab})
	fm = updated.(*sftpFormModel)
	if fm.focusLocal || fm.mode != sftpBrowse {
		t.Fatal("Tab must switch focus back to the remote pane")
	}
}

func TestSFTPFrameFitsTerminal(t *testing.T) {
	i18n.SetLang("en")
	for _, size := range [][2]int{{100, 24}, {100, 30}, {120, 40}, {180, 45}} {
		w, h := size[0], size[1]
		for _, layout := range []config.SFTPLayout{config.SFTPLayoutDual, config.SFTPLayoutSingle} {
			m := NewSFTPFormWithLayout(NewStyles(w), w, h, "host", "", layout)
			m.client = &sftpconfig.SFTPClient{}
			m.loading = false
			m.ready = true
			m.mode = sftpBrowse
			m.cwd = "/"
			m.localCwd = t.TempDir()
			m.entries = []sftpconfig.RemoteEntry{{Name: "a.bin", Size: 10}}
			m.refreshLocal()
			m.updateRemoteRows()
			m.updateLocalRows()
			for _, focusLocal := range []bool{false, true} {
				m.setFocusLocal(focusLocal)
				view := m.View()
				if got := lipgloss.Height(view); got > h {
					t.Fatalf("w=%d h=%d layout=%q local=%v: frame height %d exceeds terminal",
						w, h, layout, focusLocal, got)
				}
				first := view
				if i := strings.Index(view, "\n"); i >= 0 {
					first = view[:i]
				}
				if !strings.Contains(first, "SFTP") {
					t.Fatalf("w=%d h=%d layout=%q local=%v: header missing from first line",
						w, h, layout, focusLocal)
				}
			}
		}
	}
}

func TestSFTPHelpFooterShowsManageHints(t *testing.T) {
	i18n.SetLang("en")
	m := newSFTPManageTestForm(t)
	view := m.View()
	for _, needle := range []string{"n: mkdir", "R: rename", "d: delete", "v: layout"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("footer missing %q", needle)
		}
	}
	m.setFocusLocal(true)
	view = m.View()
	for _, needle := range []string{"n: mkdir", "R: rename", "d: delete"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("local footer missing %q", needle)
		}
	}
}

func TestSFTPLocalPanePopulatedOnConnect(t *testing.T) {
	m := NewSFTPForm(NewStyles(100), 100, 30, "host", "")
	m.localCwd = t.TempDir()
	if err := os.WriteFile(filepath.Join(m.localCwd, "a.bin"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	updated, _ := m.Update(sftpConnectedMsg{client: &sftpconfig.SFTPClient{}, cwd: "/"})
	fm := updated.(*sftpFormModel)
	updated, _ = fm.Update(sftpEntriesMsg{entries: []sftpconfig.RemoteEntry{{Name: "r.bin"}}, cwd: "/"})
	fm = updated.(*sftpFormModel)
	if len(fm.localTbl.Rows()) == 0 {
		t.Fatal("local pane must render rows right after connect, without Tab")
	}
}

func TestSFTPEscFromLocalReturnsToRemote(t *testing.T) {
	for _, layout := range []config.SFTPLayout{config.SFTPLayoutSingle, config.SFTPLayoutDual} {
		m := NewSFTPFormWithLayout(NewStyles(100), 100, 30, "host", "", layout)
		m.client = &sftpconfig.SFTPClient{}
		m.loading = false
		m.ready = true
		m.mode = sftpBrowse
		m.cwd = "/"
		m.localCwd = t.TempDir()
		m.refreshLocal()
		m.updateRemoteRows()
		m.updateLocalRows()
		m.setFocusLocal(true)

		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		fm := updated.(*sftpFormModel)
		if fm.focusLocal || fm.mode != sftpBrowse {
			t.Fatalf("layout %q: esc from local must return to remote", layout)
		}
		if cmd != nil {
			t.Fatalf("layout %q: esc from local must not quit", layout)
		}

		updated, cmd = fm.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if cmd == nil {
			t.Fatalf("layout %q: esc from remote must quit", layout)
		}
		_ = updated

		m2 := NewSFTPFormWithLayout(NewStyles(100), 100, 30, "host", "", layout)
		m2.client = &sftpconfig.SFTPClient{}
		m2.loading = false
		m2.mode = sftpBrowse
		m2.setFocusLocal(true)
		_, cmd = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
		if cmd == nil {
			t.Fatalf("layout %q: q must quit from anywhere", layout)
		}
	}
}

func TestSFTPDeleteDirectoryFlows(t *testing.T) {
	m := newSFTPManageTestForm(t)
	m.remoteTbl.SetCursor(0) // docs directory sorts first

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	fm := updated.(*sftpFormModel)
	if fm.mode != sftpDeleteConfirm {
		t.Fatal("d on a directory must open delete confirm")
	}
	if !strings.Contains(fm.View(), "everything inside") {
		t.Fatal("directory confirm must warn about recursive delete")
	}

	updated, cmd := fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	fm = updated.(*sftpFormModel)
	if cmd == nil {
		t.Fatal("expected delete cmd after confirm")
	}
	updated, cmd = fm.Update(sftpDeleteResultMsg{filename: "docs", success: true})
	fm = updated.(*sftpFormModel)
	if cmd == nil {
		t.Fatal("expected refresh cmd after delete success")
	}
	if !strings.Contains(fm.statusMsg, "docs") {
		t.Fatalf("status = %q, want docs", fm.statusMsg)
	}
}

func TestSFTPLocalDeleteNonEmptyDir(t *testing.T) {
	m := newSFTPManageTestForm(t)
	sub := filepath.Join(m.localCwd, "full")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "x.bin"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	m.refreshLocal()
	m.updateLocalRows()
	m.setFocusLocal(true)
	for i, f := range m.localFiles {
		if f == sub {
			m.localTbl.SetCursor(i)
		}
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	fm := updated.(*sftpFormModel)
	if fm.mode != sftpDeleteConfirm {
		t.Fatal("d on a local directory must open delete confirm")
	}
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Fatal("non-empty local dir must be removed recursively")
	}
}

func TestSFTPBusyKeysShowHintWhileTransferring(t *testing.T) {
	i18n.SetLang("zh")
	m := newSFTPManageTestForm(t)
	m.transferring = true
	for _, key := range []rune{'n', 'd', 'R', 'r'} {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		fm := updated.(*sftpFormModel)
		if fm.mode == sftpMkdirInput || fm.mode == sftpRenameInput || fm.mode == sftpDeleteConfirm {
			t.Fatalf("key %q must not open input while transferring", key)
		}
	}
	if !strings.Contains(m.statusMsg, i18n.T("ftp.busy")) {
		t.Fatalf("status = %q, want busy hint", m.statusMsg)
	}
}

func TestSFTPStaleResultPreservesOpenInput(t *testing.T) {
	m := newSFTPManageTestForm(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	fm := updated.(*sftpFormModel)
	fm = typeSFTPRunes(t, fm, "sec")
	updated, _ = fm.Update(sftpMkdirResultMsg{success: true})
	fm = updated.(*sftpFormModel)
	if fm.mode != sftpMkdirInput {
		t.Fatalf("mode = %v, stale result must not close open input", fm.mode)
	}
	if fm.inputBuffer != "sec" {
		t.Fatalf("inputBuffer = %q, typed text must survive stale result", fm.inputBuffer)
	}
}

func TestSFTPInputVisibleWhileLoading(t *testing.T) {
	m := newSFTPManageTestForm(t)
	m.loading = true // previous op still in flight on a slow link

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	fm := updated.(*sftpFormModel)
	if fm.mode != sftpMkdirInput {
		t.Fatalf("mode = %v, input must open even while loading", fm.mode)
	}
	if view := fm.View(); !strings.Contains(view, fm.inputPrompt) {
		t.Fatal("input line must render on top of progress while loading")
	}
}

func TestSFTPLayoutToggleRefusedWhenNarrow(t *testing.T) {
	i18n.SetLang("en")
	m := NewSFTPFormWithLayout(NewStyles(60), 60, 30, "host", "", config.SFTPLayoutSingle)
	m.client = &sftpconfig.SFTPClient{}
	m.loading = false
	m.mode = sftpBrowse

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	fm := updated.(*sftpFormModel)
	if fm.layout != config.SFTPLayoutSingle {
		t.Fatalf("layout = %q, narrow terminal must refuse dual pane", fm.layout)
	}
	if !strings.Contains(fm.statusMsg, i18n.T("sftp.too_narrow")) {
		t.Fatalf("status = %q, want too-narrow hint", fm.statusMsg)
	}
}
