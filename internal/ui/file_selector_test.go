package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zsuroy/ctty/internal/i18n"
)

func TestFileSelector_Basic(t *testing.T) {
	i18n.SetLang("en")
	styles := NewStyles(80)
	files := []string{"/home/user/.ssh/config", "/home/user/.ssh/conf.d/work"}

	selector, err := newFileSelectorFromFiles("Choose Config", styles, 80, 24, files)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	view := selector.View()
	if !strings.Contains(view, "Choose Config") {
		t.Fatalf("expected view to contain title, got:\n%s", view)
	}

	// Down moves cursor
	_, _ = selector.Update(tea.KeyMsg{Type: tea.KeyDown})
	if selector.selected != 1 {
		t.Errorf("expected selected 1, got %d", selector.selected)
	}

	// Up moves cursor back
	_, _ = selector.Update(tea.KeyMsg{Type: tea.KeyUp})
	if selector.selected != 0 {
		t.Errorf("expected selected 0, got %d", selector.selected)
	}

	// Enter selects file
	_, cmd := selector.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected select cmd on Enter")
	}
	msg := cmd()
	selMsg, ok := msg.(fileSelectorMsg)
	if !ok {
		t.Fatalf("expected fileSelectorMsg, got %T", msg)
	}
	if selMsg.selectedFile != files[0] {
		t.Errorf("expected selectedFile %s, got %s", files[0], selMsg.selectedFile)
	}

	// Esc cancels
	_, cmd = selector.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected cancel cmd on Esc")
	}
	msg = cmd()
	selMsg, ok = msg.(fileSelectorMsg)
	if !ok || !selMsg.cancelled {
		t.Fatalf("expected cancelled fileSelectorMsg, got %+v", msg)
	}
}

func TestFileSelector_EmptyFiles(t *testing.T) {
	i18n.SetLang("en")
	styles := NewStyles(80)
	selector, err := newFileSelectorFromFiles("Choose Config", styles, 80, 24, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	view := selector.View()
	if !strings.Contains(view, i18n.T("file_selector.empty")) {
		t.Fatalf("expected view to indicate no files, got:\n%s", view)
	}
}
