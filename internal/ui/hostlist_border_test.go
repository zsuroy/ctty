package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/version"
)

func rightBorderDisplayCol(line string, markers string) int {
	s := ansi.Strip(line)
	best := -1
	for _, r := range markers {
		idx := strings.LastIndex(s, string(r))
		if idx < 0 {
			continue
		}
		col := terminalDisplayWidth(s[:idx]) + 1 // 1-based end column of border
		if col > best {
			best = col
		}
	}
	return best
}

func TestHostListSearchRoundedTableSquare(t *testing.T) {
	i18n.SetLang(i18n.LangZHCN)
	hosts := []config.SSHHost{
		{Name: "alpha", Hostname: "alpha.example.test", Tags: []string{"lab"}},
		{Name: "beta", Hostname: "10.0.0.2", Tags: []string{"lab", "edge"}},
		{Name: "gamma", Hostname: "gamma.example.test", Tags: []string{"lab"}},
	}
	m := NewModel(hosts, "", false, "v0.6.1", true)
	m.ready = true
	m.width = 120
	m.height = 40
	m.updateInfo = &version.UpdateInfo{
		Available:  true,
		CurrentVer: "0.6.1",
		LatestVer:  "0.6.2",
	}
	m.updateTableColumns()
	m.updateTableRows()

	view := m.View()
	seenSearchTop := false
	seenTableTop := false
	for _, line := range strings.Split(view, "\n") {
		s := ansi.Strip(line)
		// Search uses rounded lipgloss chrome (╭╮); table uses manual ASCII (┌┐).
		// Do not require search and table to share identical chrome / right columns.
		if strings.Contains(s, "╮") {
			if !seenSearchTop {
				if col := rightBorderDisplayCol(line, "╮"); col <= 0 {
					t.Fatalf("search top missing right rounded border:\n%s", s)
				}
				seenSearchTop = true
			}
		}
		if strings.Contains(s, "┐") {
			if !seenTableTop {
				if col := rightBorderDisplayCol(line, "┐"); col <= 0 {
					t.Fatalf("table top missing right square border:\n%s", s)
				}
				seenTableTop = true
			}
		}
	}
	if !seenSearchTop || !seenTableTop {
		t.Fatalf("missing chrome: searchRounded=%v tableSquare=%v\n%s", seenSearchTop, seenTableTop, ansi.Strip(view))
	}
	plain := ansi.Strip(view)
	if strings.Contains(plain, "┌") && !strings.Contains(plain, "╭") {
		t.Fatal("expected rounded search corners ╭╮")
	}
	if !strings.Contains(plain, "╭") || !strings.Contains(plain, "╮") {
		t.Fatalf("search should use rounded ╭╮ chrome\n%s", plain)
	}
	if !strings.Contains(plain, "┌") || !strings.Contains(plain, "┐") {
		t.Fatalf("table should use square ┌┐ chrome\n%s", plain)
	}
}

func TestListBoxInnerWidthMatchesTableBudget(t *testing.T) {
	if got := listBoxInnerWidth(120); got != 116 {
		t.Fatalf("listBoxInnerWidth(120)=%d want 116", got)
	}
	// searchMaxWidth is used by host-list search (rounded Search*) again.
	if got := searchMaxWidth(120); got != 114 {
		t.Fatalf("searchMaxWidth(120)=%d want 114", got)
	}
}

func TestPadToTerminalWidth(t *testing.T) {
	got := padToTerminalWidth("hi", 5)
	if terminalDisplayWidth(got) != 5 {
		t.Fatalf("width=%d want 5 (%q)", terminalDisplayWidth(got), got)
	}
	got = padToTerminalWidth("你好世界多余", 4)
	if terminalDisplayWidth(got) != 4 {
		t.Fatalf("truncate width=%d want 4 (%q)", terminalDisplayWidth(got), got)
	}
}

func TestJetBrainsStatusEmojiPadWidth(t *testing.T) {
	t.Setenv("TERMINAL_EMULATOR", "JetBrains-JediTerm")
	t.Setenv("JETBRAINS_INTELLIJ_COMMAND_X", "")
	t.Setenv("IDEA_INITIAL_DIRECTORY", "")
	if !isJetBrainsTerminal() {
		t.Fatal("expected JetBrains detection via TERMINAL_EMULATOR")
	}

	// JediTerm: only the gray circle (U+26AB) advances 1 while ansi counts 2.
	// Green/red/yellow ping circles advance as full width 2 — do not subtract.
	gray := "\U000026AB"
	green := "\U0001F7E2"

	if tw, sw := terminalDisplayWidth(gray), ansi.StringWidth(gray); tw != sw-1 {
		t.Fatalf("gray: terminalDisplayWidth=%d ansi.StringWidth=%d want ansi-1", tw, sw)
	}
	if tw, sw := terminalDisplayWidth(green), ansi.StringWidth(green); tw != sw {
		t.Fatalf("green: terminalDisplayWidth=%d ansi.StringWidth=%d want equal (no deficit)", tw, sw)
	}

	line := padToTerminalWidth(gray+" host", 20)
	if tw := terminalDisplayWidth(line); tw != 20 {
		t.Fatalf("gray pad terminalDisplayWidth=%d want 20 (%q)", tw, line)
	}
	if sw := ansi.StringWidth(line); sw != 21 {
		t.Fatalf("gray pad ansi.StringWidth=%d want 21 (%q)", sw, line)
	}

	// Green row padded to N must not get an extra space from a false deficit.
	const N = 20
	greenRow := padToTerminalWidth(green+" host", N)
	if tw := terminalDisplayWidth(greenRow); tw != N {
		t.Fatalf("green pad terminalDisplayWidth=%d want %d (%q)", tw, N, greenRow)
	}
	if sw := ansi.StringWidth(greenRow); sw != N {
		t.Fatalf("green pad ansi.StringWidth=%d want %d (no extra pad) (%q)", sw, N, greenRow)
	}
}

func TestRenderAsciiBoxRightEdgeConsistent(t *testing.T) {
	body := "hello\n" + "\U0001F7E2" + " world"
	box := renderAsciiBox(20, body, SecondaryColor)
	var rightCols []int
	for _, line := range strings.Split(box, "\n") {
		s := ansi.Strip(line)
		for _, mark := range []string{"┐", "│", "┘"} {
			if idx := strings.LastIndex(s, mark); idx >= 0 {
				rightCols = append(rightCols, terminalDisplayWidth(s[:idx])+1)
				break
			}
		}
	}
	if len(rightCols) < 3 {
		t.Fatalf("expected border lines, got %v\n%s", rightCols, ansi.Strip(box))
	}
	for i := 1; i < len(rightCols); i++ {
		if rightCols[i] != rightCols[0] {
			t.Fatalf("right edge mismatch: %v\n%s", rightCols, ansi.Strip(box))
		}
	}

	// JetBrains: emoji row still shares the corner column.
	t.Setenv("TERMINAL_EMULATOR", "JetBrains-JediTerm")
	boxJB := renderAsciiBox(20, body, SecondaryColor)
	var jbCols []int
	for _, line := range strings.Split(boxJB, "\n") {
		s := ansi.Strip(line)
		for _, mark := range []string{"┐", "│", "┘"} {
			if idx := strings.LastIndex(s, mark); idx >= 0 {
				jbCols = append(jbCols, terminalDisplayWidth(s[:idx])+1)
				break
			}
		}
	}
	for i := 1; i < len(jbCols); i++ {
		if jbCols[i] != jbCols[0] {
			t.Fatalf("JB right edge mismatch: %v\n%s", jbCols, ansi.Strip(boxJB))
		}
	}
}

func TestJetBrainsEnvVariants(t *testing.T) {
	t.Setenv("TERMINAL_EMULATOR", "")
	t.Setenv("JETBRAINS_INTELLIJ_COMMAND_X", "/path/to/idea")
	t.Setenv("IDEA_INITIAL_DIRECTORY", "")
	if !isJetBrainsTerminal() {
		t.Fatal("JETBRAINS_INTELLIJ_COMMAND_X should detect")
	}
	t.Setenv("JETBRAINS_INTELLIJ_COMMAND_X", "")
	t.Setenv("IDEA_INITIAL_DIRECTORY", "/project")
	if !isJetBrainsTerminal() {
		t.Fatal("IDEA_INITIAL_DIRECTORY should detect")
	}
}
