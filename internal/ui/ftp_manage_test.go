package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/credential"
	"github.com/zsuroy/ctty/internal/ftpclient"
	"github.com/zsuroy/ctty/internal/ftpconfig"
	"github.com/zsuroy/ctty/internal/i18n"
)

func newManageTestForm(t *testing.T) *ftpFormModel {
	t.Helper()
	i18n.SetLang("en")
	m := NewFTPForm(NewStyles(100), 100, 30, "site")
	m.client = &ftpclient.Client{}
	m.loading = false
	m.mode = ftpBrowse
	m.cwd = "/pub"
	m.localCwd = t.TempDir()
	m.entries = []ftpclient.RemoteEntry{
		{Name: "a.bin", Size: 10},
		{Name: "docs", IsDir: true},
	}
	m.updateRemoteRows()
	m.remoteTbl.SetCursor(0)
	return m
}

func typeRunes(t *testing.T, m *ftpFormModel, s string) *ftpFormModel {
	t.Helper()
	updated := tea.Model(m)
	for _, r := range s {
		updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	fm, ok := updated.(*ftpFormModel)
	if !ok {
		t.Fatal("Update did not return *ftpFormModel")
	}
	return fm
}

func TestFTPMkdirFlow(t *testing.T) {
	m := newManageTestForm(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	fm := updated.(*ftpFormModel)
	if fm.mode != ftpMkdirInput {
		t.Fatalf("mode = %v, want mkdir input", fm.mode)
	}

	fm = typeRunes(t, fm, "newdir")
	updated, cmd := fm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm = updated.(*ftpFormModel)
	if fm.mode != ftpBrowse {
		t.Fatalf("mode = %v, want browse after enter", fm.mode)
	}
	if cmd == nil {
		t.Fatal("expected mkdir cmd after enter")
	}

	updated, cmd = fm.Update(ftpMkdirResultMsg{name: "newdir", success: true})
	fm = updated.(*ftpFormModel)
	if fm.loading {
		t.Fatal("loading must clear on mkdir result")
	}
	if cmd == nil {
		t.Fatal("expected refresh cmd after mkdir success")
	}
	if !strings.Contains(fm.statusMsg, "newdir") {
		t.Fatalf("status = %q, want newdir", fm.statusMsg)
	}
}

func TestFTPMkdirEmptyNameCancels(t *testing.T) {
	m := newManageTestForm(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	fm := updated.(*ftpFormModel)
	updated, cmd := fm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm = updated.(*ftpFormModel)
	if fm.mode != ftpBrowse {
		t.Fatalf("mode = %v, want browse", fm.mode)
	}
	if cmd != nil {
		t.Fatal("empty name must not produce a cmd")
	}
}

func TestFTPDeleteConfirmFlow(t *testing.T) {
	m := newManageTestForm(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	fm := updated.(*ftpFormModel)
	if fm.mode != ftpDeleteConfirm {
		t.Fatalf("mode = %v, want delete confirm", fm.mode)
	}
	if fm.selected == nil || fm.selected.Name != "a.bin" {
		t.Fatalf("selected = %+v, want a.bin", fm.selected)
	}
	if !strings.Contains(fm.View(), "a.bin") {
		t.Fatal("delete confirm must show the filename")
	}

	updated, cmd := fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	fm = updated.(*ftpFormModel)
	if fm.mode != ftpBrowse {
		t.Fatalf("mode = %v, want browse after confirm", fm.mode)
	}
	if cmd == nil {
		t.Fatal("expected delete cmd after confirm")
	}

	updated, cmd = fm.Update(ftpDeleteResultMsg{filename: "a.bin", success: true})
	fm = updated.(*ftpFormModel)
	if fm.selected != nil {
		t.Fatal("selected must clear on delete result")
	}
	if cmd == nil {
		t.Fatal("expected refresh cmd after delete success")
	}
	if !strings.Contains(fm.statusMsg, "a.bin") {
		t.Fatalf("status = %q, want a.bin", fm.statusMsg)
	}
}

func TestFTPDeleteCancelAndDirGuard(t *testing.T) {
	m := newManageTestForm(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	fm := updated.(*ftpFormModel)
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	fm = updated.(*ftpFormModel)
	if fm.mode != ftpBrowse || fm.selected != nil {
		t.Fatal("esc must cancel delete confirm")
	}
}

func TestFTPDeleteDirectoryFlows(t *testing.T) {
	m := newManageTestForm(t)
	m.remoteTbl.SetCursor(1) // docs directory

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	fm := updated.(*ftpFormModel)
	if fm.mode != ftpDeleteConfirm {
		t.Fatal("d on a directory must open delete confirm")
	}
	if !strings.Contains(fm.View(), "everything inside") {
		t.Fatal("directory confirm must warn about recursive delete")
	}

	updated, cmd := fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	fm = updated.(*ftpFormModel)
	if cmd == nil {
		t.Fatal("expected delete cmd after confirm")
	}
	updated, cmd = fm.Update(ftpDeleteResultMsg{filename: "docs", success: true})
	fm = updated.(*ftpFormModel)
	if cmd == nil {
		t.Fatal("expected refresh cmd after delete success")
	}
	if !strings.Contains(fm.statusMsg, "docs") {
		t.Fatalf("status = %q, want docs", fm.statusMsg)
	}
}

func TestFTPRenameFlow(t *testing.T) {
	m := newManageTestForm(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	fm := updated.(*ftpFormModel)
	if fm.mode != ftpRenameInput {
		t.Fatalf("mode = %v, want rename input", fm.mode)
	}
	if fm.inputBuffer != "a.bin" {
		t.Fatalf("inputBuffer = %q, want prefilled a.bin", fm.inputBuffer)
	}

	for range len("a.bin") {
		updated, _ := fm.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		fm = updated.(*ftpFormModel)
	}
	fm = typeRunes(t, fm, "b.bin")
	updated, cmd := fm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm = updated.(*ftpFormModel)
	if fm.mode != ftpBrowse {
		t.Fatalf("mode = %v, want browse after enter", fm.mode)
	}
	if cmd == nil {
		t.Fatal("expected rename cmd after enter")
	}

	updated, cmd = fm.Update(ftpRenameResultMsg{oldName: "a.bin", newName: "b.bin", success: true})
	fm = updated.(*ftpFormModel)
	if cmd == nil {
		t.Fatal("expected refresh cmd after rename success")
	}
	if !strings.Contains(fm.statusMsg, "a.bin") || !strings.Contains(fm.statusMsg, "b.bin") {
		t.Fatalf("status = %q, want both names", fm.statusMsg)
	}
}

func TestFTPRenameUnchangedCancels(t *testing.T) {
	m := newManageTestForm(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	fm := updated.(*ftpFormModel)
	updated, cmd := fm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm = updated.(*ftpFormModel)
	if fm.mode != ftpBrowse {
		t.Fatalf("mode = %v, want browse", fm.mode)
	}
	if cmd != nil {
		t.Fatal("unchanged name must not produce a cmd")
	}
}

func TestFTPManageKeysBlockedWhenBusy(t *testing.T) {
	m := newManageTestForm(t)
	m.transferring = true
	for _, key := range []rune{'n', 'd', 'R'} {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if fm := updated.(*ftpFormModel); fm.mode != ftpBrowse {
			t.Fatalf("key %q must be ignored while transferring", key)
		}
	}

	m = newManageTestForm(t)
	m.transferring = true
	m.setFocusLocal(true)
	for _, key := range []rune{'n', 'd', 'R'} {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if fm := updated.(*ftpFormModel); fm.mode != ftpLocalBrowse {
			t.Fatalf("key %q must be ignored in local pane while transferring", key)
		}
	}
}

func TestFTPHelpFooterShowsManageHints(t *testing.T) {
	m := newManageTestForm(t)
	view := m.View()
	for _, needle := range []string{"n: mkdir", "R: rename", "d: delete"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("footer missing %q", needle)
		}
	}
}

func TestFTPSitesInfoOverlay(t *testing.T) {
	i18n.SetLang("en")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	credential.ResetStoreForTest()
	t.Cleanup(credential.ResetStoreForTest)

	m := NewFTPSitesForm(NewStyles(100), 100, 30)
	m.sites = []ftpconfig.FTPSite{
		{Name: "lab", Host: "10.0.0.1", Port: 21, User: "admin", Tags: []string{"ops"}},
	}
	m.applyFilter()
	m.rebuildTable()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	fm := updated.(*ftpSitesModel)
	if !fm.showInfo {
		t.Fatal("i must open site info")
	}
	view := fm.View()
	for _, needle := range []string{"lab", "10.0.0.1", "admin", "ops"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("info view missing %q", needle)
		}
	}

	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	fm = updated.(*ftpSitesModel)
	if fm.showInfo {
		t.Fatal("esc must close site info")
	}
}

func TestFTPSitesInfoEditFromInfo(t *testing.T) {
	i18n.SetLang("en")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	credential.ResetStoreForTest()
	t.Cleanup(credential.ResetStoreForTest)

	m := NewFTPSitesForm(NewStyles(100), 100, 30)
	m.sites = []ftpconfig.FTPSite{
		{Name: "lab", Host: "10.0.0.1", Port: 21, User: "admin"},
	}
	m.applyFilter()
	m.rebuildTable()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	fm := updated.(*ftpSitesModel)
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	fm = updated.(*ftpSitesModel)
	if fm.showInfo {
		t.Fatal("e must leave info view")
	}
	if !fm.editMode {
		t.Fatal("e from info must enter edit mode")
	}
	if got := fm.addFields[0].Value(); got != "lab" {
		t.Fatalf("edit name = %q, want lab", got)
	}
}

func TestFTPBrowserInfoOverlay(t *testing.T) {
	m := newManageTestForm(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	fm := updated.(*ftpFormModel)
	if !fm.showInfo {
		t.Fatal("i must open entry info")
	}
	view := fm.View()
	for _, needle := range []string{"a.bin", "10B", "/pub/a.bin"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("info view missing %q", needle)
		}
	}

	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	fm = updated.(*ftpFormModel)
	if fm.showInfo {
		t.Fatal("i must close entry info")
	}
}

func TestFTPBrowserLocalInfoOverlay(t *testing.T) {
	m := newManageTestForm(t)
	f, err := os.Create(filepath.Join(m.localCwd, "up.bin"))
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	m.refreshLocal()
	m.updateLocalRows()
	m.setFocusLocal(true)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	fm := updated.(*ftpFormModel)
	if !fm.showInfo {
		t.Fatal("i must open local entry info")
	}
	view := fm.View()
	if !strings.Contains(view, "up.bin") {
		t.Fatal("info view must show local filename")
	}
	if !strings.Contains(view, "Path:") {
		t.Fatal("info view must show path row")
	}
}

func TestFTPLocalMkdirFlow(t *testing.T) {
	m := newManageTestForm(t)
	m.setFocusLocal(true)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	fm := updated.(*ftpFormModel)
	if fm.mode != ftpMkdirInput || !fm.localOp {
		t.Fatalf("mode = %v localOp = %v, want mkdir input targeting local", fm.mode, fm.localOp)
	}
	fm = typeRunes(t, fm, "newdir")
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm = updated.(*ftpFormModel)
	if fm.mode != ftpBrowse {
		t.Fatalf("mode = %v, want browse", fm.mode)
	}
	st, err := os.Stat(filepath.Join(m.localCwd, "newdir"))
	if err != nil || !st.IsDir() {
		t.Fatalf("local dir not created: %v", err)
	}
	if !strings.Contains(fm.statusMsg, "newdir") {
		t.Fatalf("status = %q, want newdir", fm.statusMsg)
	}
}

func TestFTPLocalDeleteFlow(t *testing.T) {
	m := newManageTestForm(t)
	target := filepath.Join(m.localCwd, "gone.bin")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	m.refreshLocal()
	m.updateLocalRows()
	m.setFocusLocal(true)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	fm := updated.(*ftpFormModel)
	if fm.mode != ftpDeleteConfirm || !fm.localOp {
		t.Fatalf("mode = %v localOp = %v, want local delete confirm", fm.mode, fm.localOp)
	}
	if !strings.Contains(fm.View(), "gone.bin") {
		t.Fatal("delete confirm must show the filename")
	}
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	fm = updated.(*ftpFormModel)
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("local file must be deleted")
	}
	if !strings.Contains(fm.statusMsg, "gone.bin") {
		t.Fatalf("status = %q, want gone.bin", fm.statusMsg)
	}
}

func TestFTPLocalRenameFlow(t *testing.T) {
	m := newManageTestForm(t)
	oldPath := filepath.Join(m.localCwd, "old.bin")
	if err := os.WriteFile(oldPath, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	m.refreshLocal()
	m.updateLocalRows()
	m.setFocusLocal(true)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	fm := updated.(*ftpFormModel)
	if fm.mode != ftpRenameInput || !fm.localOp {
		t.Fatalf("mode = %v localOp = %v, want local rename input", fm.mode, fm.localOp)
	}
	if fm.inputBuffer != "old.bin" {
		t.Fatalf("inputBuffer = %q, want prefilled old.bin", fm.inputBuffer)
	}
	for range len("old.bin") {
		updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		fm = updated.(*ftpFormModel)
	}
	fm = typeRunes(t, fm, "new.bin")
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm = updated.(*ftpFormModel)
	if _, err := os.Stat(filepath.Join(m.localCwd, "new.bin")); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("old file must be gone")
	}
}

// TestFTPBrowserFrameFitsTerminal guards the steady-state frame budget:
// header(1) + paths(2) + search(3) + table(h+2) + help must never exceed
// the terminal height, or the alt-screen scrolls and the diff renderer
// permanently loses the header line.
func TestFTPBrowserFrameFitsTerminal(t *testing.T) {
	i18n.SetLang("en")
	for _, size := range [][2]int{{100, 24}, {100, 30}, {120, 40}, {180, 45}} {
		w, h := size[0], size[1]
		for _, layout := range []config.FTPLayout{config.FTPLayoutDual, config.FTPLayoutSingle} {
			m := NewFTPFormWithLayout(NewStyles(w), w, h, "site", layout)
			m.client = &ftpclient.Client{}
			m.loading = false
			m.mode = ftpBrowse
			m.cwd = "/pub"
			m.localCwd = t.TempDir()
			m.entries = []ftpclient.RemoteEntry{{Name: "a.bin", Size: 10}}
			m.refreshLocal()
			m.updateRemoteRows()
			for _, focusLocal := range []bool{false, true} {
				m.focusLocal = focusLocal
				m.updateRemoteRows()
				m.updateLocalRows()
				view := m.View()
				if got := lipgloss.Height(view); got > h {
					t.Fatalf("w=%d h=%d layout=%q local=%v: frame height %d exceeds terminal",
						w, h, layout, focusLocal, got)
				}
				first := view
				if i := strings.Index(view, "\n"); i >= 0 {
					first = view[:i]
				}
				if !strings.Contains(first, "FTP") {
					t.Fatalf("w=%d h=%d layout=%q local=%v: header missing from first line",
						w, h, layout, focusLocal)
				}
			}
		}
	}
}

func TestFTPSiteFormTagsRoundtrip(t *testing.T) {

	i18n.SetLang("en")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	m := NewFTPSitesForm(NewStyles(100), 100, 30)
	m.startAdd()
	m.addFields[0].SetValue("lab")
	m.addFields[1].SetValue("10.0.0.1")
	m.addFields[2].SetValue("21")
	m.addFields[3].SetValue("admin")
	m.addFields[5].SetValue("ops, backup , ,ops")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm := updated.(*ftpSitesModel)
	if fm.addMode {
		t.Fatalf("save failed: %q", fm.addErr)
	}
	site, ok := ftpconfig.Find("lab")
	if !ok {
		t.Fatal("site not saved")
	}
	if len(site.Tags) != 3 || site.Tags[0] != "ops" || site.Tags[1] != "backup" || site.Tags[2] != "ops" {
		t.Fatalf("tags = %v, want [ops backup ops] trimmed", site.Tags)
	}
	rows := fm.table.Rows()
	if len(rows) == 0 || !strings.Contains(rows[0][3], "#ops") {
		t.Fatalf("list tags cell = %v, want colored #ops", rows)
	}

	fm.startEdit(site)
	if got := fm.addFields[5].Value(); got != "ops, backup, ops" {
		t.Fatalf("tags field = %q, want prefilled", got)
	}
}

func taggedSitesForm(t *testing.T, w int) *ftpSitesModel {
	t.Helper()
	i18n.SetLang("en")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	m := NewFTPSitesForm(NewStyles(w), w, 30)
	m.sites = []ftpconfig.FTPSite{
		{Name: "lab", Host: "10.0.0.1", Port: 21, User: "admin", Tags: []string{"production-environment", "backup"}},
		{Name: "nas", Host: "nas.local", Port: 21, Tags: []string{"Min", "Minio"}},
	}
	m.applyFilter()
	m.rebuildTable()
	return m
}

// Long tags must render in full at wide widths: ANSI bytes used to be
// counted as visible cells by bubbles table truncation, chopping text
// early ("#Min…") and occasionally splitting escapes (ragged borders).
func TestFTPSitesLongTagsNotTruncated(t *testing.T) {
	m := taggedSitesForm(t, 140)
	view := m.View()
	for _, needle := range []string{"#production-environment", "#backup", "#Minio"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("wide view missing full tag %q", needle)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if w := lineDisplayWidth(line); w > 140 {
			t.Errorf("rendered line %d cols exceeds terminal\n%q", w, line)
		}
	}
}

// Longest-first colorize must not corrupt "#Minio" when "#Min" exists.
func TestColorizeSiteTagsLongestFirst(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	got := colorizeSiteTags("#Min #Minio", []ftpconfig.FTPSite{{Tags: []string{"Min", "Minio"}}})
	if !strings.Contains(got, "#Minio\x1b[39m") {
		t.Fatalf("overlapping tags corrupted: %q", got)
	}
}

func TestFTPSitesNarrowWideSwitchNoPanic(t *testing.T) {
	i18n.SetLang("en")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	m := NewFTPSitesForm(NewStyles(100), 100, 30)
	m.sites = []ftpconfig.FTPSite{
		{Name: "lab", Host: "10.0.0.1", Port: 21, User: "admin", Tags: []string{"ops"}},
	}
	m.applyFilter()
	m.rebuildTable()

	for _, w := range []int{60, 100, 60, 140, 40} {
		updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 30})
		m = updated.(*ftpSitesModel)
		if w < 74 && len(m.table.Columns()) != 3 {
			t.Fatalf("width %d: want 3 columns, got %d", w, len(m.table.Columns()))
		}
		if w >= 74 && len(m.table.Columns()) != 4 {
			t.Fatalf("width %d: want 4 columns, got %d", w, len(m.table.Columns()))
		}
		_ = m.View()
	}
}

func TestFTPEscFromLocalReturnsToRemote(t *testing.T) {
	m := newManageTestForm(t)
	m.setFocusLocal(true)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	fm := updated.(*ftpFormModel)
	if fm.focusLocal || fm.mode != ftpBrowse {
		t.Fatal("esc from local must return to remote")
	}
	if cmd != nil {
		t.Fatal("esc from local must not quit")
	}

	updated, cmd = fm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc from remote must quit")
	}
	_ = updated

	m2 := newManageTestForm(t)
	m2.setFocusLocal(true)
	_, cmd = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q must quit from anywhere")
	}
}

func TestFTPBusyKeysShowHintWhileTransferring(t *testing.T) {
	i18n.SetLang("zh")
	m := newManageTestForm(t)
	m.transferring = true
	for _, key := range []rune{'n', 'd', 'R', 'r'} {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		fm := updated.(*ftpFormModel)
		if fm.mode == ftpMkdirInput || fm.mode == ftpRenameInput || fm.mode == ftpDeleteConfirm {
			t.Fatalf("key %q must not open input while transferring", key)
		}
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	fm := updated.(*ftpFormModel)
	if !strings.Contains(fm.statusMsg, i18n.T("ftp.busy")) {
		t.Fatalf("status = %q, want busy hint", fm.statusMsg)
	}
}

func TestFTPStaleResultPreservesOpenInput(t *testing.T) {
	m := newManageTestForm(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	fm := updated.(*ftpFormModel)
	fm = typeRunes(t, fm, "sec")
	updated, _ = fm.Update(ftpMkdirResultMsg{name: "first", success: true})
	fm = updated.(*ftpFormModel)
	if fm.mode != ftpMkdirInput {
		t.Fatalf("mode = %v, stale result must not close open input", fm.mode)
	}
	if fm.inputBuffer != "sec" {
		t.Fatalf("inputBuffer = %q, typed text must survive stale result", fm.inputBuffer)
	}
}

func TestFTPLayoutToggleRefusedWhenNarrow(t *testing.T) {
	i18n.SetLang("en")
	m := NewFTPFormWithLayout(NewStyles(60), 60, 30, "site", config.FTPLayoutSingle)
	m.client = &ftpclient.Client{}
	m.loading = false
	m.mode = ftpBrowse

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	fm := updated.(*ftpFormModel)
	if fm.layout != config.FTPLayoutSingle {
		t.Fatalf("layout = %q, narrow terminal must refuse dual pane", fm.layout)
	}
	if !strings.Contains(fm.statusMsg, i18n.T("ftp.too_narrow")) {
		t.Fatalf("status = %q, want too-narrow hint", fm.statusMsg)
	}
}
