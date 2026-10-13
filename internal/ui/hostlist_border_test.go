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

func TestJetBrainsPeekAndModalEmojiWidth(t *testing.T) {
	t.Setenv("TERMINAL_EMULATOR", "JetBrains-JediTerm")
	t.Setenv("JETBRAINS_INTELLIJ_COMMAND_X", "")
	t.Setenv("IDEA_INITIAL_DIRECTORY", "")
	if !isJetBrainsTerminal() {
		t.Fatal("expected JetBrains detection")
	}

	lightning := "\U000026A1" // ⚡
	hourglass := "\U000023F3" // ⏳

	if tw, sw := terminalDisplayWidth(lightning), ansi.StringWidth(lightning); tw != sw-1 {
		t.Fatalf("lightning: terminalDisplayWidth=%d ansi.StringWidth=%d want ansi-1", tw, sw)
	}
	if tw, sw := terminalDisplayWidth(hourglass), ansi.StringWidth(hourglass); tw != sw-1 {
		t.Fatalf("hourglass: terminalDisplayWidth=%d ansi.StringWidth=%d want ansi-1", tw, sw)
	}

	line := padToTerminalWidth(lightning+" Quick Peek", 30)
	if tw := terminalDisplayWidth(line); tw != 30 {
		t.Fatalf("lightning pad terminalDisplayWidth=%d want 30 (%q)", tw, line)
	}
}

func TestJetBrainsPeekModalBorderAlignment(t *testing.T) {
	for _, lang := range []string{i18n.LangZHCN, i18n.LangEN} {
		t.Run("Lang_"+lang, func(t *testing.T) {
			i18n.SetLang(lang)
			t.Setenv("TERMINAL_EMULATOR", "JetBrains-JediTerm")
			t.Setenv("JETBRAINS_INTELLIJ_COMMAND_X", "")
			t.Setenv("IDEA_INITIAL_DIRECTORY", "")

			hosts := []config.SSHHost{
				{Name: "prod-server-01", Hostname: "10.0.0.1"},
			}
			m := NewModel(hosts, "", false, "v1.1.0", true)
			m.width = 100
			m.height = 30
			m.peekHost = &hosts[0]
			m.peekLoading = true

			// 1. Check loading peek modal: contains ⚡ and ⏳
			loadingBox := m.renderPeekModal()
			if !strings.Contains(loadingBox, "⚡") || !strings.Contains(loadingBox, "⏳") {
				t.Fatalf("expected ⚡ and ⏳ in loading modal, got:\n%s", loadingBox)
			}
			assertBoxRightBordersAligned(t, "loadingBox", loadingBox)

			loadingPlaced := renderConfirmModal(m.width, m.height, loadingBox)
			assertPlacedModalBordersAligned(t, "loadingPlaced", loadingPlaced, m.width)

			// 2. Check loaded stats peek modal: contains ⚡ and metrics
			m.peekLoading = false
			m.peekStats = &HostStats{
				Uptime:      "15 days, 4:20",
				Users:       "2",
				Load1:       "0.15",
				Load5:       "0.20",
				Load15:      "0.18",
				MemTotalMB:  8192,
				MemUsedMB:   4096,
				MemPercent:  50.0,
				DiskTotal:   "100G",
				DiskUsed:    "25G",
				DiskPercent: 25.0,
			}
			statsBox := m.renderPeekModal()
			if !strings.Contains(statsBox, "⚡") {
				t.Fatalf("expected ⚡ in stats modal, got:\n%s", statsBox)
			}
			assertBoxRightBordersAligned(t, "statsBox", statsBox)

			statsPlaced := renderConfirmModal(m.width, m.height, statsBox)
			assertPlacedModalBordersAligned(t, "statsPlaced", statsPlaced, m.width)
		})
	}
}

func assertBoxRightBordersAligned(t *testing.T, name, box string) {
	t.Helper()
	lines := strings.Split(box, "\n")
	var rightCols []int
	for _, line := range lines {
		s := ansi.Strip(line)
		for _, mark := range []string{"╮", "│", "╯"} {
			if idx := strings.LastIndex(s, mark); idx >= 0 {
				rightCols = append(rightCols, terminalDisplayWidth(s[:idx])+1)
				break
			}
		}
	}
	if len(rightCols) < 3 {
		t.Fatalf("%s: expected at least 3 border lines, got %v\n%s", name, rightCols, ansi.Strip(box))
	}
	for i := 1; i < len(rightCols); i++ {
		if rightCols[i] != rightCols[0] {
			t.Fatalf("%s: right border mismatch at line %d (col=%d, want %d):\n%s",
				name, i, rightCols[i], rightCols[0], ansi.Strip(box))
		}
	}
}

func assertPlacedModalBordersAligned(t *testing.T, name, modal string, width int) {
	t.Helper()
	lines := strings.Split(modal, "\n")
	var leftCols, rightCols []int
	bordered := 0
	for _, line := range lines {
		s := ansi.Strip(line)
		if strings.TrimSpace(s) == "" {
			continue
		}
		if w := terminalDisplayWidth(line); w != width {
			t.Fatalf("%s: line display width=%d want %d (%q)", name, w, width, line)
		}

		leftIdx := -1
		for _, mark := range []string{"╭", "│", "╰"} {
			if idx := strings.Index(s, mark); idx >= 0 {
				if leftIdx == -1 || idx < leftIdx {
					leftIdx = idx
				}
			}
		}
		rightIdx := -1
		for _, mark := range []string{"╮", "│", "╯"} {
			if idx := strings.LastIndex(s, mark); idx >= 0 {
				if idx > rightIdx {
					rightIdx = idx
				}
			}
		}
		// Every content row must carry both borders: a missing one (e.g.
		// trimmed away by the excess alignment in alignJetBrainsBox) would
		// be invisible on screen while still "aligning" with the rest.
		if leftIdx < 0 || rightIdx < 0 {
			t.Fatalf("%s: row missing a border (left=%d right=%d): %q", name, leftIdx, rightIdx, s)
		}
		bordered++
		leftCols = append(leftCols, terminalDisplayWidth(s[:leftIdx])+1)
		rightCols = append(rightCols, terminalDisplayWidth(s[:rightIdx])+1)
	}

	if bordered < 3 {
		t.Fatalf("%s: expected at least 3 content border lines, got %d", name, bordered)
	}
	for i := 1; i < len(leftCols); i++ {
		if leftCols[i] != leftCols[0] {
			t.Fatalf("%s: left border mismatch at line %d (col=%d, want %d)",
				name, i, leftCols[i], leftCols[0])
		}
		if rightCols[i] != rightCols[0] {
			t.Fatalf("%s: right border mismatch at line %d (col=%d, want %d)",
				name, i, rightCols[i], rightCols[0])
		}
	}
}

func TestJetBrainsUpdateSuccessModalBorderAlignment(t *testing.T) {
	for _, lang := range []string{i18n.LangZHCN, i18n.LangEN} {
		t.Run("Lang_"+lang, func(t *testing.T) {
			i18n.SetLang(lang)
			t.Setenv("TERMINAL_EMULATOR", "JetBrains-JediTerm")
			t.Setenv("JETBRAINS_INTELLIJ_COMMAND_X", "")
			t.Setenv("IDEA_INITIAL_DIRECTORY", "")
			if !isJetBrainsTerminal() {
				t.Fatal("expected JetBrains detection")
			}

			// The update-success modal line starts with ✅ (U+2705), which
			// advances 1 column on JediTerm while ansi.StringWidth counts 2.
			// The success line must carry the deficit so the right border
			// aligns with the corners.
			check := "\U00002705"
			if tw, sw := terminalDisplayWidth(check), ansi.StringWidth(check); tw != sw-1 {
				t.Fatalf("check mark: terminalDisplayWidth=%d ansi.StringWidth=%d want ansi-1", tw, sw)
			}

		form := NewUpdateForm(NewStyles(100), 100, 30, "1.0.0", "1.0.1", "")
		form.phase = updateDone
		modal := form.View()
		assertPlacedModalBordersAligned(t, "updateSuccessModal", modal, form.width)
		})
	}
}

func TestJetBrainsTagPickerModalBorderAlignment(t *testing.T) {
	for _, lang := range []string{i18n.LangZHCN, i18n.LangEN} {
		t.Run("Lang_"+lang, func(t *testing.T) {
			i18n.SetLang(lang)
			t.Setenv("TERMINAL_EMULATOR", "JetBrains-JediTerm")
			t.Setenv("JETBRAINS_INTELLIJ_COMMAND_X", "")
			t.Setenv("IDEA_INITIAL_DIRECTORY", "")
			if !isJetBrainsTerminal() {
				t.Fatal("expected JetBrains detection")
			}

			check := labelTagEmoji
			// U+1F3F7+U+FE0F advances StringWidth+1 in JediTerm (measured in
			// PyCharm: the variation selector takes a visible cell). The
			// excess entry is what lets alignJetBrainsBox trim the drawer
			// title row back to the card border instead of overhanging it.
			if tw, sw := terminalDisplayWidth(check), ansi.StringWidth(check); tw != sw+1 {
				t.Fatalf("tag label: terminalDisplayWidth=%d ansi.StringWidth=%d want ansi+1 (excess)", tw, sw)
			}

			hosts := []config.SSHHost{
				{Name: "web-1", Hostname: "10.0.1.1", Tags: []string{"web", "prod"}},
				{Name: "db-1", Hostname: "10.0.2.1", Tags: []string{"db"}},
			}
		m := NewModel(hosts, "", false, "v1.3.0", true)
		m.ready = true
		m.width = 100
		m.height = 30
		m.tagPickerOpen = true
		modal := renderConfirmModal(m.width, m.height, m.renderTagPicker())
		assertPlacedModalBordersAligned(t, "tagPickerModal", modal, m.width)
		})
	}
}

func TestVSCodeTagPickerModalBorderAlignment(t *testing.T) {
	for _, lang := range []string{i18n.LangZHCN, i18n.LangEN} {
		t.Run("Lang_"+lang, func(t *testing.T) {
			i18n.SetLang(lang)
			t.Setenv("TERM_PROGRAM", "vscode")
			t.Setenv("TERMINAL_EMULATOR", "")
			t.Setenv("JETBRAINS_INTELLIJ_COMMAND_X", "")
			t.Setenv("IDEA_INITIAL_DIRECTORY", "")
			if isJetBrainsTerminal() {
				t.Fatal("expected non-JetBrains detection")
			}
			if !isVSCodeTerminal() {
				t.Fatal("expected VS Code detection via TERM_PROGRAM")
			}

			check := labelTagEmoji
			// xterm.js renders the two-codepoint grapheme one column wider
			// than ansi.StringWidth counts as well (measured by report: the
			// title row overhung the card border there). The startup probe
			// re-measures and overrides this whenever the terminal answers.
			if tw, sw := terminalDisplayWidth(check), ansi.StringWidth(check); tw != sw+1 {
				t.Fatalf("tag label: terminalDisplayWidth=%d ansi.StringWidth=%d want ansi+1 (excess)", tw, sw)
			}

			hosts := []config.SSHHost{
				{Name: "web-1", Hostname: "10.0.1.1", Tags: []string{"web", "prod"}},
				{Name: "db-1", Hostname: "10.0.2.1", Tags: []string{"db"}},
			}
			m := NewModel(hosts, "", false, "v1.3.0", true)
			m.ready = true
			m.width = 100
			m.height = 30
			m.tagPickerOpen = true
			modal := renderConfirmModal(m.width, m.height, m.renderTagPicker())
			assertPlacedModalBordersAligned(t, "tagPickerModal", modal, m.width)
		})
	}
}

func TestTagPickerModalCoversFullCanvas(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T)
	}{
		{"JetBrains", func(t *testing.T) {
			t.Setenv("TERMINAL_EMULATOR", "JetBrains-JediTerm")
			t.Setenv("TERM_PROGRAM", "")
			t.Setenv("JETBRAINS_INTELLIJ_COMMAND_X", "")
			t.Setenv("IDEA_INITIAL_DIRECTORY", "")
			if !isJetBrainsTerminal() {
				t.Fatal("expected JetBrains detection")
			}
		}},
		{"Standard", func(t *testing.T) {
			t.Setenv("TERMINAL_EMULATOR", "xterm-256color")
			t.Setenv("TERM_PROGRAM", "")
			t.Setenv("JETBRAINS_INTELLIJ_COMMAND_X", "")
			t.Setenv("IDEA_INITIAL_DIRECTORY", "")
			if isJetBrainsTerminal() {
				t.Fatal("expected standard terminal detection")
			}
		}},
	}
	for _, tc := range cases {
		for _, lang := range []string{i18n.LangZHCN, i18n.LangEN} {
			t.Run(tc.name+"/Lang_"+lang, func(t *testing.T) {
				tc.setup(t)
				i18n.SetLang(lang)
				hosts := []config.SSHHost{
					{Name: "web-1", Hostname: "10.0.1.1", Tags: []string{"web", "prod"}},
					{Name: "db-1", Hostname: "10.0.2.1", Tags: []string{"db"}},
				}
				m := NewModel(hosts, "", false, "v1.3.0", true)
				m.ready = true
				m.width = 171
				m.height = 22
				m.tagPickerOpen = true
				modal := renderConfirmModal(m.width, m.height, m.renderTagPicker())
				lines := strings.Split(modal, "\n")
				if len(lines) != 22 {
					t.Fatalf("modal rows=%d want 22", len(lines))
				}
				for i, ln := range lines {
					if w := terminalDisplayWidth(ln); w != 171 {
						t.Fatalf("row %d display width=%d want 171 (%q)", i, w, ln)
					}
				}
				for _, bad := range []string{"Serial", "Telnet", "WebDAV", "SSH ("} {
					if strings.Contains(modal, bad) {
						t.Fatalf("modal contains tab text %q", bad)
					}
				}
			})
		}
	}
}

func TestStandardTerminalPeekModalBorderAlignment(t *testing.T) {
	for _, lang := range []string{i18n.LangZHCN, i18n.LangEN} {
		t.Run("Lang_"+lang, func(t *testing.T) {
		i18n.SetLang(lang)
		t.Setenv("TERMINAL_EMULATOR", "xterm-256color")
		t.Setenv("TERM_PROGRAM", "")
		t.Setenv("JETBRAINS_INTELLIJ_COMMAND_X", "")
		t.Setenv("IDEA_INITIAL_DIRECTORY", "")
		if isJetBrainsTerminal() {
			t.Fatal("expected standard terminal detection")
		}

			hosts := []config.SSHHost{
				{Name: "prod-server-01", Hostname: "10.0.0.1"},
			}
			m := NewModel(hosts, "", false, "v1.1.0", true)
			m.width = 100
			m.height = 30
			m.peekHost = &hosts[0]
			m.peekLoading = true

			loadingBox := m.renderPeekModal()
			assertBoxRightBordersAligned(t, "loadingBox", loadingBox)

			loadingPlaced := renderConfirmModal(m.width, m.height, loadingBox)
			assertPlacedModalBordersAligned(t, "loadingPlaced", loadingPlaced, m.width)

		m.peekLoading = false
		m.peekStats = &HostStats{
			Uptime:      "15 days, 4:20",
			Users:       "2",
			Load1:       "0.15",
			Load5:       "0.20",
			Load15:      "0.18",
			MemTotalMB:  8192,
			MemUsedMB:   4096,
			MemPercent:  50.0,
			DiskTotal:   "100G",
			DiskUsed:    "25G",
			DiskPercent: 25.0,
		}
		statsBox := m.renderPeekModal()
		assertBoxRightBordersAligned(t, "statsBox", statsBox)

		statsPlaced := renderConfirmModal(m.width, m.height, statsBox)
		assertPlacedModalBordersAligned(t, "statsPlaced", statsPlaced, m.width)
		})
	}
}
