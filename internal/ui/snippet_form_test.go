package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zsuroy/ctty/internal/i18n"
)

func TestSnippetForm_Browse(t *testing.T) {
	i18n.SetLang("en")
	styles := NewStyles(80)

	form := NewSnippetForm(styles, 80, 24, "server1", "")
	if form == nil {
		t.Fatal("expected NewSnippetForm to return non-nil")
	}

	view := form.View()
	if !strings.Contains(view, "server1") {
		t.Fatalf("expected view to contain host name, got:\n%s", view)
	}

	// Down moves cursor
	initialCursor := form.cursor
	_, _ = form.Update(tea.KeyMsg{Type: tea.KeyDown})
	if form.cursor != initialCursor+1 {
		t.Errorf("expected cursor %d, got %d", initialCursor+1, form.cursor)
	}

	// Esc sends snippetCloseMsg
	_, cmd := form.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected close cmd on Esc")
	}
	msg := cmd()
	if _, ok := msg.(snippetCloseMsg); !ok {
		t.Errorf("expected snippetCloseMsg, got %T", msg)
	}
}

func TestSnippetForm_AddMode(t *testing.T) {
	i18n.SetLang("en")
	styles := NewStyles(80)

	form := NewSnippetForm(styles, 80, 24, "server1", "")

	// Press 'n' to enter add mode
	_, _ = form.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if form.mode != snippetAdd {
		t.Fatalf("expected mode snippetAdd, got %v", form.mode)
	}

	view := form.View()
	if !strings.Contains(view, "Add Custom Snippet") {
		t.Fatalf("expected view to contain 'Add Custom Snippet', got:\n%s", view)
	}

	// Esc returns to browse
	_, _ = form.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if form.mode != snippetBrowse {
		t.Fatalf("expected mode snippetBrowse, got %v", form.mode)
	}
}
