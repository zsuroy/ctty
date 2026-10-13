package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/ftpconfig"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/s3config"
	"github.com/zsuroy/ctty/internal/telnetconfig"
	"github.com/zsuroy/ctty/internal/webdavconfig"
)

// drawerGeometry reproduces the terminal the tag-drawer corruption was
// reported on: the PyCharm embedded terminal, stty size 22x171.
const (
	drawerW = 171
	drawerH = 22
)

// tagEmoji is the picker title glyph exactly as the UI writes it:
// U+1F3F7 + U+FE0F. The two-codepoint grapheme advances one column wider
// than ansi.StringWidth counts on the IDE terminals (the variation
// selector takes a visible cell there).
const tagEmoji = labelTagEmoji

// setJetBrainsJediTermEnv reproduces the PyCharm embedded-terminal
// environment: JediTerm emulates xterm-256color and advertises itself via
// TERMINAL_EMULATOR=JetBrains-JediTerm.
func setJetBrainsJediTermEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TERMINAL_EMULATOR", "JetBrains-JediTerm")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("JETBRAINS_INTELLIJ_COMMAND_X", "")
	t.Setenv("IDEA_INITIAL_DIRECTORY", "")
	if !isJetBrainsTerminal() {
		t.Fatal("expected JetBrains/JediTerm detection")
	}
}

// setStandardTerminalEnv clears every IDE marker so the deficit tables stay
// inactive, giving baseline (non-compensated) behavior.
func setStandardTerminalEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TERMINAL_EMULATOR", "xterm-256color")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("JETBRAINS_INTELLIJ_COMMAND_X", "")
	t.Setenv("IDEA_INITIAL_DIRECTORY", "")
	if isJetBrainsTerminal() || isVSCodeTerminal() {
		t.Fatal("expected standard terminal detection")
	}
}

// setVSCodeTerminalEnv reproduces the VS Code integrated terminal, detected
// via TERM_PROGRAM=vscode.
func setVSCodeTerminalEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TERM_PROGRAM", "vscode")
	t.Setenv("TERMINAL_EMULATOR", "")
	t.Setenv("JETBRAINS_INTELLIJ_COMMAND_X", "")
	t.Setenv("IDEA_INITIAL_DIRECTORY", "")
	if isJetBrainsTerminal() || !isVSCodeTerminal() {
		t.Fatal("expected VS Code detection")
	}
}

// isolateConfigDir points config lookups at an empty temp dir so form
// constructors that reload from disk start from an empty inventory instead
// of the developer's real ~/.config/ctty files.
func isolateConfigDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
}

// assertCanvasFrame verifies a rendered frame is structurally sound for the
// terminal it will be drawn on:
//   - one row per screen line;
//   - no row exceeding the canvas width by terminalDisplayWidth or
//     ansi.StringWidth (a row wider than the terminal soft-wraps and
//     desyncs Bubble Tea's renderer; StringWidth over it gets truncated,
//     losing visible content).
//
// On compensation terminals each row additionally carries an explicit
// EraseLineRight, which is what clears stale cells from the previous frame
// regardless of how glyph advances differ from ansi.StringWidth.
func assertCanvasFrame(t *testing.T, name, frame string, width, height int) {
	t.Helper()
	lines := strings.Split(frame, "\n")
	if len(lines) != height {
		t.Fatalf("%s: frame rows=%d want %d", name, len(lines), height)
	}
	for i, ln := range lines {
		if w := terminalDisplayWidth(ln); w > width {
			t.Fatalf("%s: row %d display width=%d exceeds canvas %d: %q", name, i, w, width, ln)
		}
		if sw := ansi.StringWidth(ln); sw > width {
			t.Fatalf("%s: row %d StringWidth=%d exceeds canvas %d — renderer truncates visible content or the row soft-wraps: %q", name, i, sw, width, ln)
		}
	}
	if needsWidthCompensation() {
		for i, ln := range lines {
			if !strings.HasSuffix(ln, eraseLineRight) {
				t.Fatalf("%s: row %d missing EraseLineRight: %q", name, i, ln)
			}
		}
	}
}

// TestFTPTableRightBorderSurvivesCanvas locks the PyCharm/VS Code report
// where the FTP list's right border disappeared: the table is budgeted to
// the full terminal width, so the right border is the last visible column of
// every row. Canvas preparation that trimmed rows by ansi.StringWidth — or
// to any fixed short target — silently cut that border off on compensation
// terminals. Rows must keep their border and instead gain an explicit
// EraseLineRight that clears the tail.
func TestFTPTableRightBorderSurvivesCanvas(t *testing.T) {
	for _, term := range []struct {
		name  string
		setup func(*testing.T)
	}{
		{"JetBrains", setJetBrainsJediTermEnv},
		{"VSCode", setVSCodeTerminalEnv},
		{"Standard", setStandardTerminalEnv},
	} {
		t.Run(term.name, func(t *testing.T) {
			term.setup(t)
			i18n.SetLang(i18n.LangEN)
			isolateConfigDir(t)

			m := NewFTPSitesForm(NewStyles(drawerW), drawerW, drawerH)
			m.sites = []ftpconfig.FTPSite{
				{Name: "ftp.scene.org", Host: "ftp.scene.org", Port: 21, User: "ftp", Tags: []string{"prod"}},
				{Name: "lab-nas", Host: "10.0.0.9", Port: 21, Tags: []string{"lab"}},
			}
			m.applyFilter()
			m.rebuildTable()

			frame := RenderCanvas(drawerW, drawerH, m.View())
			assertCanvasFrame(t, "ftp-list", frame, drawerW, drawerH)

			bordered := 0
			for _, ln := range strings.Split(frame, "\n") {
				plain := ansi.Strip(ln) // drops SGR and the trailing EraseLineRight
				if !strings.Contains(plain, "ftp.scene.org") && !strings.Contains(plain, "lab-nas") {
					continue
				}
				// Standard terminals keep the App padding space after the
				// border; compensation terminals trim it. Either way the
				// border must be the last visible column.
				if !strings.HasSuffix(strings.TrimRight(plain, " "), "│") {
					t.Fatalf("site row lost its right border: %q", plain)
				}
				bordered++
			}
			if bordered != 2 {
				t.Fatalf("bordered site rows=%d want 2", bordered)
			}
		})
	}
}

// assertTableRowsShareBorderColumn checks that every row identified by one
// of marks ends with its right border on ONE shared display column — the
// invariant that keeps deficit-glyph rows (⚫) aligned with the emoji-free
// header row on compensation terminals.
func assertTableRowsShareBorderColumn(t *testing.T, name, frame string, marks ...string) {
	t.Helper()
	borderCols := map[int]bool{}
	rows := 0
	for _, ln := range strings.Split(frame, "\n") {
		s := ansi.Strip(ln)
		matched := false
		for _, m := range marks {
			if strings.Contains(s, m) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		idx := strings.LastIndex(s, "│")
		if idx < 0 {
			t.Fatalf("%s: table row missing its right border: %q", name, s)
		}
		borderCols[terminalDisplayWidth(s[:idx])] = true
		rows++
	}
	if rows < 2 {
		t.Fatalf("%s: table rows found=%d want at least 2", name, rows)
	}
	if len(borderCols) != 1 {
		t.Fatalf("%s: table rows right-border columns=%v want one shared column", name, borderCols)
	}
}

// TestListTableRowsAlignOnCompensationTerminals locks the JetBrains report
// about the ⚫ status circle: bubbles budgets the table columns with
// ansi.StringWidth, so every row carrying the circle (one column narrower
// on the IDE terminals) came out one column short of the emoji-free header
// row and the right borders misaligned. The rendered tables now go through
// alignJetBrainsBox AFTER the border is drawn (lipgloss pads block lines to
// the widest one by ansi.StringWidth, which undoes any earlier alignment),
// so all rows must end at the same real column.
func TestListTableRowsAlignOnCompensationTerminals(t *testing.T) {
	for _, term := range []struct {
		name  string
		setup func(*testing.T)
	}{
		{"JetBrains", setJetBrainsJediTermEnv},
		{"VSCode", setVSCodeTerminalEnv},
		{"Standard", setStandardTerminalEnv},
	} {
		t.Run(term.name, func(t *testing.T) {
			term.setup(t)
			i18n.SetLang(i18n.LangEN)

			t.Run("FTP", func(t *testing.T) {
				isolateConfigDir(t)
				m := NewFTPSitesForm(NewStyles(drawerW), drawerW, drawerH)
				m.sites = []ftpconfig.FTPSite{
					{Name: "ftp.scene.org", Host: "ftp.scene.org", Port: 21, User: "ftp", Tags: []string{"prod"}},
				}
				m.applyFilter()
				m.rebuildTable()
				frame := RenderCanvas(drawerW, drawerH, m.View())
				assertCanvasFrame(t, "ftp-list", frame, drawerW, drawerH)
				assertTableRowsShareBorderColumn(t, "ftp-list", frame,
					"ftp.scene.org", "User@Hostname")
			})

			t.Run("Telnet", func(t *testing.T) {
				isolateConfigDir(t)
				m := NewTelnetForm(NewStyles(drawerW), drawerW, drawerH)
				m.hosts = []telnetconfig.TelnetHost{
					{Name: "core-switch", Host: "10.0.0.254", Port: 2323, Tags: []string{"lab"}},
				}
				m.filtered = m.hosts
				m.buildTable()
				frame := RenderCanvas(drawerW, drawerH, m.View())
				assertCanvasFrame(t, "telnet-list", frame, drawerW, drawerH)
				assertTableRowsShareBorderColumn(t, "telnet-list", frame,
					"core-switch", "Host")
			})
		})
	}
}

// TestTagPickerFrameNeverWrapsWithMisjudgedGlyph locks the safety net for
// the PyCharm failure mode: U+1F3F7 advances full width there, so a deficit
// entry that wrongly assumes it is narrow inserts one extra column before
// the drawer's right border and the title row overruns the terminal. The
// canvas slack plus clamp must keep the frame structurally sound (rows at
// their target width, none exceeding the canvas) even with the wrong
// table, so the renderer never wraps a row.
func TestTagPickerFrameNeverWrapsWithMisjudgedGlyph(t *testing.T) {
	setJetBrainsJediTermEnv(t)
	for _, lang := range []string{i18n.LangZHCN, i18n.LangEN} {
		t.Run("Lang_"+lang, func(t *testing.T) {
			i18n.SetLang(lang)
			orig, had := jetBrainsGlyphDelta[tagEmoji]
			jetBrainsGlyphDelta[tagEmoji] = -1 // deliberately wrong: claims narrow while it advances wide
			t.Cleanup(func() {
				if had {
					jetBrainsGlyphDelta[tagEmoji] = orig
				} else {
					delete(jetBrainsGlyphDelta, tagEmoji)
				}
			})

			fm := openFTPDrawer(t)
			root := drawerRoot(t)
			root.viewMode = ViewFTP
			root.ftpSitesForm = fm
			assertCanvasFrame(t, "ftp-root-misjudged", root.View(), drawerW, drawerH)
			assertNoLeak(t, "ftp-root-misjudged", root.View(),
				"archive-box", "lab-nas", "ftp.scene.org", "10.0.0.9", "[ ]")
			assertHasDrawer(t, "ftp-root-misjudged", root.View(), "scene")
		})
	}
}

func assertNoLeak(t *testing.T, name, frame string, needles ...string) {
	t.Helper()
	for _, bad := range needles {
		if strings.Contains(frame, bad) {
			t.Fatalf("%s: frame leaks underlying content %q", name, bad)
		}
	}
}

func assertHasDrawer(t *testing.T, name, frame, tag string) {
	t.Helper()
	if !strings.Contains(frame, tagEmoji) {
		t.Fatalf("%s: drawer missing title emoji", name)
	}
	if !strings.Contains(frame, tag) {
		t.Fatalf("%s: drawer missing tag %q", name, tag)
	}
}

// drawerRoot builds a root model at the drawer geometry. The per-view form
// is assigned by the caller, mirroring how Update wires switchProtocolMsg.
func drawerRoot(t *testing.T) Model {
	t.Helper()
	root := NewModel(
		[]config.SSHHost{{Name: "web-1", Hostname: "10.0.1.1", Tags: []string{"web"}}},
		"", false, "v1.3.0", true,
	)
	root.ready = true
	root.width = drawerW
	root.height = drawerH
	root.styles = NewStyles(drawerW)
	return root
}

func openSSHDrawer(t *testing.T) Model {
	t.Helper()
	root := drawerRoot(t)
	root.viewMode = ViewList
	root.updateTableRows()
	updated, _ := root.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	rm := updated.(Model)
	if !rm.tagPickerOpen {
		t.Fatal("w must open the host-list tag picker")
	}
	return rm
}

func openFTPDrawer(t *testing.T) *ftpSitesModel {
	t.Helper()
	isolateConfigDir(t)
	m := NewFTPSitesForm(NewStyles(drawerW), drawerW, drawerH)
	m.sites = []ftpconfig.FTPSite{
		{Name: "archive-box", Host: "ftp.scene.org", Port: 21, User: "anonymous", Tags: []string{"scene"}},
		{Name: "lab-nas", Host: "10.0.0.9", Port: 21, Tags: []string{"lab"}},
	}
	m.applyFilter()
	m.rebuildTable()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	fm := updated.(*ftpSitesModel)
	if !fm.tagPickerOpen {
		t.Fatal("w must open the FTP tag picker")
	}
	return fm
}

func openS3Drawer(t *testing.T) *s3SitesModel {
	t.Helper()
	isolateConfigDir(t)
	m := NewS3SitesForm(NewStyles(drawerW), drawerW, drawerH)
	m.sites = []s3config.S3Site{
		{Name: "minio-lab", Endpoint: "127.0.0.1:9000", Bucket: "backups", Tags: []string{"lab"}},
	}
	m.applyFilter()
	m.rebuildTable()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	fm := updated.(*s3SitesModel)
	if !fm.tagPickerOpen {
		t.Fatal("w must open the S3 tag picker")
	}
	return fm
}

func openWebDAVDrawer(t *testing.T) *webdavSitesModel {
	t.Helper()
	isolateConfigDir(t)
	m := NewWebDAVSitesForm(NewStyles(drawerW), drawerW, drawerH)
	m.sites = []webdavconfig.WebDAVSite{
		{Name: "nextcloud", URL: "https://dav.example.com", User: "admin", Tags: []string{"dav"}},
	}
	m.applyFilter()
	m.rebuildTable()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	fm := updated.(*webdavSitesModel)
	if !fm.tagPickerOpen {
		t.Fatal("w must open the WebDAV tag picker")
	}
	return fm
}

func openTelnetDrawer(t *testing.T) *telnetFormModel {
	t.Helper()
	isolateConfigDir(t)
	m := NewTelnetForm(NewStyles(drawerW), drawerW, drawerH)
	m.hosts = []telnetconfig.TelnetHost{
		{Name: "core-switch", Host: "10.0.0.254", Port: 2323, Tags: []string{"lab"}},
	}
	m.filtered = m.hosts
	m.buildTable()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	fm := updated.(*telnetFormModel)
	if !fm.tagPickerOpen {
		t.Fatal("w must open the telnet tag picker")
	}
	return fm
}

// TestEveryTagDrawerFillsCanvas reproduces the PyCharm report end to end:
// every manager's tag drawer must paint a blank full-screen canvas through
// the real root View() pipeline (sub-form View + RenderCanvas). Any row
// narrower than the terminal — or any missing row — leaves cells from the
// previously rendered list visible next to the drawer, which is exactly the
// leftover FTP site rows the user captured in the PyCharm terminal.
func TestEveryTagDrawerFillsCanvas(t *testing.T) {
	terminals := []struct {
		name  string
		setup func(*testing.T)
	}{
		{"JetBrains", setJetBrainsJediTermEnv},
		{"Standard", setStandardTerminalEnv},
	}
	for _, term := range terminals {
		for _, lang := range []string{i18n.LangZHCN, i18n.LangEN} {
			t.Run(term.name+"/Lang_"+lang, func(t *testing.T) {
				term.setup(t)
				i18n.SetLang(lang)

				t.Run("SSH", func(t *testing.T) {
					rm := openSSHDrawer(t)
					// Sub-form level: the host list renders the drawer itself.
					sub := rm.renderListView()
					assertCanvasFrame(t, "ssh sub-form", RenderCanvas(drawerW, drawerH, sub), drawerW, drawerH)
					// Root level: exactly what Bubble Tea receives.
					frame := rm.View()
					assertCanvasFrame(t, "ssh root", frame, drawerW, drawerH)
					assertNoLeak(t, "ssh root", frame,
						"web-1", "db-1", "10.0.1.1", "Serial", "Telnet", "WebDAV", "S3", "FTP")
					assertHasDrawer(t, "ssh root", frame, "web")
				})

				t.Run("FTP", func(t *testing.T) {
					fm := openFTPDrawer(t)
					root := drawerRoot(t)
					root.viewMode = ViewFTP
					root.ftpSitesForm = fm
					sub := fm.View()
					assertCanvasFrame(t, "ftp sub-form", RenderCanvas(drawerW, drawerH, sub), drawerW, drawerH)
					frame := root.View()
					assertCanvasFrame(t, "ftp root", frame, drawerW, drawerH)
					assertNoLeak(t, "ftp root", frame,
						"archive-box", "lab-nas", "ftp.scene.org", "10.0.0.9", "[ ]",
						"Serial", "Telnet", "WebDAV", "S3", "FTP", "SSH (")
					assertHasDrawer(t, "ftp root", frame, "scene")
				})

				t.Run("S3", func(t *testing.T) {
					fm := openS3Drawer(t)
					root := drawerRoot(t)
					root.viewMode = ViewS3
					root.s3SitesForm = fm
					sub := fm.View()
					assertCanvasFrame(t, "s3 sub-form", RenderCanvas(drawerW, drawerH, sub), drawerW, drawerH)
					frame := root.View()
					assertCanvasFrame(t, "s3 root", frame, drawerW, drawerH)
					assertNoLeak(t, "s3 root", frame,
						"minio-lab", "127.0.0.1:9000", "backups",
						"Serial", "Telnet", "WebDAV", "S3", "FTP", "SSH (")
					assertHasDrawer(t, "s3 root", frame, "lab")
				})

				t.Run("WebDAV", func(t *testing.T) {
					fm := openWebDAVDrawer(t)
					root := drawerRoot(t)
					root.viewMode = ViewWebDAV
					root.webdavSitesForm = fm
					sub := fm.View()
					assertCanvasFrame(t, "webdav sub-form", RenderCanvas(drawerW, drawerH, sub), drawerW, drawerH)
					frame := root.View()
					assertCanvasFrame(t, "webdav root", frame, drawerW, drawerH)
					assertNoLeak(t, "webdav root", frame,
						"nextcloud", "dav.example.com",
						"Serial", "Telnet", "WebDAV", "S3", "FTP", "SSH (")
					assertHasDrawer(t, "webdav root", frame, "dav")
				})

				t.Run("Telnet", func(t *testing.T) {
					fm := openTelnetDrawer(t)
					root := drawerRoot(t)
					root.viewMode = ViewTelnet
					root.telnetForm = fm
					sub := fm.View()
					assertCanvasFrame(t, "telnet sub-form", RenderCanvas(drawerW, drawerH, sub), drawerW, drawerH)
					frame := root.View()
					assertCanvasFrame(t, "telnet root", frame, drawerW, drawerH)
					assertNoLeak(t, "telnet root", frame,
						"core-switch", "10.0.0.254", "2323",
						"Serial", "Telnet", "WebDAV", "S3", "FTP", "SSH (")
					assertHasDrawer(t, "telnet root", frame, "lab")
				})
			})
		}
	}
}
