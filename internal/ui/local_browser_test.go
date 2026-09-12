package ui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/i18n"
)

func newLocalBrowserTestForm(t *testing.T) *localBrowserModel {
	t.Helper()
	i18n.SetLang("en")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.bin"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	m := NewLocalBrowserForm(NewStyles(100), 100, 30, dir)
	return m
}

func typeLocalRunes(t *testing.T, m *localBrowserModel, s string) *localBrowserModel {
	t.Helper()
	updated := tea.Model(m)
	for _, r := range s {
		updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	fm, ok := updated.(*localBrowserModel)
	if !ok {
		t.Fatal("Update did not return *localBrowserModel")
	}
	return fm
}

func TestLocalBrowserNavigateEnterDir(t *testing.T) {
	m := newLocalBrowserTestForm(t)
	top := m.cwd
	// sub sorts before a.bin (directories first)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm := updated.(*localBrowserModel)
	if fm.cwd == top {
		t.Fatal("enter on a directory must descend")
	}
	if !strings.HasSuffix(fm.cwd, "sub") {
		t.Fatalf("cwd = %q, want sub", fm.cwd)
	}

	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	fm = updated.(*localBrowserModel)
	if fm.cwd != top {
		t.Fatalf("h must go back to parent, cwd = %q", fm.cwd)
	}
}

func TestLocalBrowserMkdirDeleteRename(t *testing.T) {
	m := newLocalBrowserTestForm(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	fm := updated.(*localBrowserModel)
	if fm.mode != localMkdirInput {
		t.Fatalf("mode = %v, want mkdir input", fm.mode)
	}
	fm = typeLocalRunes(t, fm, "newdir")
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm = updated.(*localBrowserModel)
	if st, err := os.Stat(filepath.Join(m.cwd, "newdir")); err != nil || !st.IsDir() {
		t.Fatalf("dir not created: %v", err)
	}

	// Rename a.bin -> b.bin (cursor to a.bin first).
	for i, f := range fm.files {
		if filepath.Base(f) == "a.bin" {
			fm.table.SetCursor(i)
		}
	}
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	fm = updated.(*localBrowserModel)
	if fm.mode != localRenameInput || fm.inputBuffer != "a.bin" {
		t.Fatalf("mode = %v input = %q, want rename prefilled", fm.mode, fm.inputBuffer)
	}
	for range len("a.bin") {
		updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		fm = updated.(*localBrowserModel)
	}
	fm = typeLocalRunes(t, fm, "b.bin")
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	fm = updated.(*localBrowserModel)
	if _, err := os.Stat(filepath.Join(m.cwd, "b.bin")); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}

	// Delete sub (non-empty-safe recursive path is the same code).
	for i, f := range fm.files {
		if filepath.Base(f) == "sub" {
			fm.table.SetCursor(i)
		}
	}
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	fm = updated.(*localBrowserModel)
	if fm.mode != localDeleteConfirm {
		t.Fatalf("mode = %v, want delete confirm", fm.mode)
	}
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	fm = updated.(*localBrowserModel)
	if _, err := os.Stat(filepath.Join(m.cwd, "sub")); !os.IsNotExist(err) {
		t.Fatal("dir must be deleted")
	}
	_ = fm
}

func TestLocalBrowserInfoAndSearch(t *testing.T) {
	m := newLocalBrowserTestForm(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	fm := updated.(*localBrowserModel)
	if !fm.showInfo {
		t.Fatal("i must open info")
	}
	if view := fm.View(); !strings.Contains(view, "sub") {
		t.Fatal("info must show selected entry")
	}
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	fm = updated.(*localBrowserModel)
	if fm.showInfo {
		t.Fatal("esc must close info")
	}

	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	fm = updated.(*localBrowserModel)
	fm = typeLocalRunes(t, fm, "a.b")
	if got := len(fm.filtered()); got != 1 {
		t.Fatalf("filtered = %d, want 1", got)
	}
	updated, _ = fm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	fm = updated.(*localBrowserModel)
	if fm.searchMode {
		t.Fatal("esc must exit search")
	}
}

func TestLocalBrowserFrameFitsTerminal(t *testing.T) {
	i18n.SetLang("en")
	for _, size := range [][2]int{{100, 24}, {100, 30}, {60, 30}} {
		w, h := size[0], size[1]
		m := NewLocalBrowserForm(NewStyles(w), w, h, t.TempDir())
		view := m.View()
		if got := lipgloss.Height(view); got > h {
			t.Fatalf("w=%d h=%d: frame height %d exceeds terminal", w, h, got)
		}
		first := view
		if i := strings.Index(view, "\n"); i >= 0 {
			first = view[:i]
		}
		if !strings.Contains(first, "Local") {
			t.Fatalf("w=%d h=%d: header missing from first line", w, h)
		}
	}
}

func TestLocalBrowserQuit(t *testing.T) {
	m := newLocalBrowserTestForm(t)
	for _, key := range []tea.Msg{
		tea.KeyMsg{Type: tea.KeyEsc},
	} {
		updated, cmd := m.Update(key)
		_ = updated
		if cmd == nil {
			t.Fatal("esc must quit with a done message")
		}
		if _, ok := cmd().(localDoneMsg); !ok {
			t.Fatalf("want localDoneMsg, got %T", cmd())
		}
	}
}

func TestLocalBrowserDoneQuitsStandalone(t *testing.T) {
	m := newLocalBrowserTestForm(t)
	updated, cmd := m.Update(localDoneMsg{})
	_ = updated
	if cmd == nil {
		t.Fatal("done message must produce quit in standalone mode")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("want tea.QuitMsg, got %T", cmd())
	}
}

func TestMainListBOpensLocalBrowser(t *testing.T) {
	i18n.SetLang("en")
	hosts := []config.SSHHost{{Name: "h1", Hostname: "example.com"}}
	m := NewModel(hosts, "", false, "v0.6.3", true)
	m.ready = true
	m.width, m.height = 100, 30
	m.viewMode = ViewList

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	um := updated.(Model)
	if um.viewMode != ViewLocalBrowser {
		t.Fatalf("viewMode = %v, want ViewLocalBrowser", um.viewMode)
	}
	if um.localBrowserForm == nil {
		t.Fatal("localBrowserForm not created")
	}
	if !strings.Contains(um.View(), i18n.T("local.title")) {
		t.Fatal("list must render the local browser")
	}

	updated, _ = um.Update(localDoneMsg{})
	um = updated.(Model)
	if um.viewMode != ViewList || um.localBrowserForm != nil {
		t.Fatal("done must return to the host list")
	}
}

func TestLocalBrowserSortCyclesModes(t *testing.T) {
	m := newLocalBrowserTestForm(t)
	big := filepath.Join(m.cwd, "big.bin")
	if err := os.WriteFile(big, bytes.Repeat([]byte("x"), 2048), 0644); err != nil {
		t.Fatal(err)
	}
	m.refresh()

	press := func(key rune) *localBrowserModel {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		m = updated.(*localBrowserModel)
		return m
	}
	press('s') // size: biggest file first (dirs still on top)
	if got := filepath.Base(m.files[1]); got != "big.bin" {
		t.Fatalf("size sort = %v, want big.bin second", m.files)
	}
	press('s') // modified
	press('s') // back to name
	if m.sortMode != localSortByName {
		t.Fatal("sort must cycle back to name")
	}
	if got := filepath.Base(m.files[0]); got != "sub" {
		t.Fatalf("name sort first = %q, want dirs-first sub", got)
	}
}

func TestLocalBrowserInputVisibleOverStatus(t *testing.T) {
	m := newLocalBrowserTestForm(t)
	m.setStatus("lingering status")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	fm := updated.(*localBrowserModel)
	if fm.mode != localMkdirInput {
		t.Fatalf("mode = %v, want mkdir input", fm.mode)
	}
	if view := fm.View(); !strings.Contains(view, fm.inputPrompt) {
		t.Fatal("input line must render on top of active status")
	}
}

func TestRevealInManagerCmd(t *testing.T) {
	cmd := revealInManagerCmd("/tmp/a.bin")
	if cmd == nil || len(cmd.Args) == 0 {
		t.Fatal("must build a reveal command")
	}
	t.Logf("reveal argv0 = %s args = %v", cmd.Path, cmd.Args)
}

func TestLocalBrowserRevealKeyKeepsState(t *testing.T) {
	m := NewLocalBrowserForm(NewStyles(100), 100, 30, t.TempDir())
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	fm := updated.(*localBrowserModel)
	if fm.mode != localBrowse {
		t.Fatal("reveal with empty selection must be a no-op")
	}
	if fm.statusMsg != "" {
		t.Fatal("no-op reveal must not set a status")
	}
}

func TestHelpFormIncludesLocalCategory(t *testing.T) {
	i18n.SetLang("en")
	help := NewHelpForm(NewStyles(200), 200, 200, "v0.6.3")
	view := help.View()
	for _, needle := range []string{"Local", "reveal in file manager", "cycle sort modes"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("help missing %q", needle)
		}
	}
	if strings.Contains(view, "help.cat_local") {
		t.Fatal("untranslated i18n key leaked into help")
	}
}
