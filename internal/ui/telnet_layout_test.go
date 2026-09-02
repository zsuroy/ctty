package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/telnetconfig"
)

func newTestTelnetForm(width, height int, hosts []telnetconfig.TelnetHost) *telnetFormModel {
	m := NewTelnetForm(NewStyles(width), width, height)
	m.hosts = hosts
	m.filtered = hosts
	m.buildTable()
	return m
}

func TestTelnetTableDoesNotDuplicatePortInHost(t *testing.T) {
	i18n.SetLang("en")
	m := newTestTelnetForm(80, 24, []telnetconfig.TelnetHost{
		{Name: "telehack", Host: "telehack.com", Port: 23, Tags: []string{"lab"}},
	})
	rows := m.table.Rows()
	if len(rows) == 0 {
		t.Fatal("expected a row")
	}
	row := rows[0]
	if len(row) < 3 {
		t.Fatalf("columns=%d, want at least name/host/port", len(row))
	}
	if strings.Contains(row[1], ":23") || strings.Contains(row[1], ":") {
		t.Fatalf("host column %q should be the hostname only; port is its own column", row[1])
	}
	if row[1] != "telehack.com" {
		t.Fatalf("host = %q, want telehack.com", row[1])
	}
	if row[2] != "23" {
		t.Fatalf("port = %q, want 23", row[2])
	}
}

func TestTelnetListFitsTerminalWidth(t *testing.T) {
	i18n.SetLang("en")
	m := newTestTelnetForm(80, 24, []telnetconfig.TelnetHost{
		{Name: "core-sw", Host: "192.168.1.1", Port: 23},
	})
	for i, line := range strings.Split(m.View(), "\n") {
		if ansi.StringWidth(line) > 80 {
			t.Errorf("line %d width %d exceeds 80: %q", i, ansi.StringWidth(line), line)
		}
	}
}

func boxTopWidth(line, left, right string) int {
	plain := ansi.Strip(line)
	i := strings.Index(plain, left)
	j := strings.LastIndex(plain, right)
	if i < 0 || j < i {
		return 0
	}
	return ansi.StringWidth(plain[i : j+len(right)])
}

func TestTelnetSearchBarMatchesTableWidth(t *testing.T) {
	i18n.SetLang("zh")
	m := newTestTelnetForm(80, 24, []telnetconfig.TelnetHost{
		{Name: "telehack", Host: "telehack.com", Port: 23, Tags: []string{"dev"}},
	})
	view := m.View()
	searchW, tableW := 0, 0
	for _, line := range strings.Split(view, "\n") {
		if searchW == 0 {
			searchW = boxTopWidth(line, "╭", "╮")
		}
		if tableW == 0 {
			tableW = boxTopWidth(line, "┌", "┐")
		}
	}
	if searchW == 0 || tableW == 0 {
		t.Fatalf("missing search or table border (search=%d table=%d)\n%s", searchW, tableW, view)
	}
	if searchW != tableW {
		t.Fatalf("search bar width %d != table width %d\n%s", searchW, tableW, view)
	}
}

func TestTelnetListRendersColoredTags(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	i18n.SetLang("en")
	m := newTestTelnetForm(80, 24, []telnetconfig.TelnetHost{
		{Name: "telehack", Host: "telehack.com", Port: 23, Tags: []string{"dev"}},
	})
	view := m.View()
	if !strings.Contains(view, "#dev") {
		t.Fatalf("list missing tag text:\n%s", view)
	}
	colored := FormatColoredTag("dev")
	if colored == "#dev" {
		t.Fatal("expected color codes for tag 'dev' under TrueColor")
	}
	if !strings.Contains(view, colored) {
		t.Fatalf("list tags should use the same colors as the SSH host list, got:\n%s", view)
	}
}

func TestTelnetDeleteKeepsListVisible(t *testing.T) {
	i18n.SetLang("en")
	m := newTestTelnetForm(80, 24, []telnetconfig.TelnetHost{
		{Name: "dockerview", Host: "10.0.0.2", Port: 23},
	})
	m.mode = telnetDeleteConfirm
	m.deleteIndex = 0
	view := m.View()
	if !strings.Contains(view, "Telnet") {
		t.Fatalf("list chrome missing during delete confirm:\n%s", view)
	}
	if !strings.Contains(view, "dockerview") {
		t.Fatalf("device name missing:\n%s", view)
	}
	if !strings.Contains(strings.ToLower(view), "delete") {
		t.Fatalf("confirm prompt missing:\n%s", view)
	}
}

func roundedTopBorderWidth(view string) int {
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "╭") {
			return ansi.StringWidth(line)
		}
	}
	return 0
}

func promptColumn(line string) int {
	plain := ansi.Strip(line)
	i := strings.Index(plain, ">")
	if i < 0 {
		return -1
	}
	return ansi.StringWidth(plain[:i])
}

func TestTelnetEditFormPromptsAlignInChinese(t *testing.T) {
	i18n.SetLang("zh")
	host := telnetconfig.TelnetHost{Name: "telehack", Host: "telehack.com", Port: 23}
	m := newTestTelnetForm(80, 24, []telnetconfig.TelnetHost{host})
	m.addForm = newTelnetAddForm(m.styles, 80, 24, &host)
	m.mode = telnetEdit

	var cols []int
	for _, line := range strings.Split(m.View(), "\n") {
		if col := promptColumn(line); col >= 0 {
			cols = append(cols, col)
		}
	}
	if len(cols) < 4 {
		t.Fatalf("expected 4 field prompts, got %d\n%s", len(cols), m.View())
	}
	for i := 1; i < len(cols); i++ {
		if cols[i] != cols[0] {
			t.Fatalf("prompt columns not aligned: %v\n%s", cols, m.View())
		}
	}
}

func TestTelnetEditFormBorderTracksWidth(t *testing.T) {
	i18n.SetLang("zh")
	host := telnetconfig.TelnetHost{Name: "core-sw", Host: "192.168.1.1", Port: 23}
	m := newTestTelnetForm(80, 24, []telnetconfig.TelnetHost{host})
	m.addForm = newTelnetAddForm(m.styles, 80, 24, &host)
	m.mode = telnetEdit

	narrowView := m.View()
	narrow := roundedTopBorderWidth(narrowView)
	if narrow == 0 {
		t.Fatal("edit form is missing a rounded top border")
	}
	for i, line := range strings.Split(narrowView, "\n") {
		if w := ansi.StringWidth(line); w > 80 {
			t.Errorf("80-col line %d width %d exceeds 80: %q", i, w, line)
		}
	}

	res, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 24})
	m = res.(*telnetFormModel)
	view := m.View()
	wide := roundedTopBorderWidth(view)
	if wide <= narrow {
		t.Fatalf("edit border did not grow on resize: 80-col=%d 140-col=%d\n%s", narrow, wide, view)
	}
	if wide < 136 {
		t.Fatalf("edit border width %d should fill a 140-col terminal, got:\n%s", wide, view)
	}
	for i, line := range strings.Split(view, "\n") {
		if w := ansi.StringWidth(line); w > 140 {
			t.Errorf("line %d width %d exceeds 140: %q", i, w, line)
		}
	}
}

func TestTelnetAddFormCtrlSSavesFromAnyField(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	i18n.SetLang("en")

	m := newTestTelnetForm(80, 24, nil)
	m.addForm = newTelnetAddForm(m.styles, 80, 24, nil)
	m.mode = telnetAdd
	m.addForm.inputs[telnetFieldName].SetValue("lab-sw")
	m.addForm.inputs[telnetFieldHost].SetValue("10.0.0.1")
	m.addForm.focusIndex = telnetFieldName

	view := m.View()
	if !strings.Contains(view, "Ctrl+S") {
		t.Fatalf("help should mention Ctrl+S:\n%s", view)
	}

	res, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = res.(*telnetFormModel)
	if m.mode != telnetList {
		t.Fatalf("mode = %v, want list after Ctrl+S save", m.mode)
	}
	if m.addForm != nil {
		t.Fatal("add form should close after Ctrl+S save")
	}
}

func TestTelnetListClearsPreviousFrameOnResize(t *testing.T) {
	i18n.SetLang("zh")
	m := newTestTelnetForm(120, 40, []telnetconfig.TelnetHost{
		{Name: "doom", Host: "doom.w-graj.net", Port: 666},
		{Name: "telehack", Host: "telehack.com", Port: 23, Tags: []string{"dev"}},
	})
	_ = m.View()

	res, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 18})
	m = res.(*telnetFormModel)
	view := m.View()
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	if len(lines) < 18 {
		t.Fatalf("view has %d lines, want 18 to overwrite the previous taller frame\n%s", len(lines), view)
	}
	for i, line := range lines {
		w := ansi.StringWidth(line)
		if w > 60 {
			t.Errorf("line %d width %d exceeds 60 (wraps and smears):\n%q", i, w, ansi.Strip(line))
		}
		if i < 18 && w < 60 {
			t.Errorf("line %d width %d < 60 (won't clear leftover cells):\n%q", i, w, ansi.Strip(line))
		}
	}
}
