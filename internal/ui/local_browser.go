package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/zsuroy/ctty/internal/i18n"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Standalone local file browser modes.
type localBrowserMode int

const (
	localBrowse localBrowserMode = iota
	localMkdirInput
	localRenameInput
	localDeleteConfirm
)

// Standalone local file browser sort modes.
type localSortMode int

const (
	localSortByName localSortMode = iota
	localSortBySize
	localSortByModified
	numLocalSortModes
)

// localBrowserModel is a standalone single-pane local file manager
// (ctty browse): navigate, search, mkdir/delete/rename with confirms,
// file details, and open-with-default-app.
type localBrowserModel struct {
	styles Styles
	width  int
	height int

	cwd           string
	files         []string
	showingDrives bool
	table         table.Model
	mode          localBrowserMode
	sortMode      localSortMode

	inputBuffer string
	inputPrompt string
	pendingPath string

	showInfo  bool
	entryInfo *localEntryInfo

	statusMsg    string
	statusExpiry time.Time

	searchInput textinput.Model
	searchMode  bool
}

// localEntryInfo is a snapshot of the file shown in the info overlay.
type localEntryInfo struct {
	name    string
	isDir   bool
	size    int64
	modTime time.Time
	path    string
}

// localDoneMsg tells the runner to exit back to the shell.
type localDoneMsg struct{}

// NewLocalBrowserForm creates the standalone local file browser.
func NewLocalBrowserForm(styles Styles, width, height int, startDir string) *localBrowserModel {
	if startDir == "" {
		startDir, _ = os.Getwd()
	}
	m := &localBrowserModel{
		styles: styles,
		width:  width,
		height: height,
		cwd:    startDir,
		mode:   localBrowse,
	}
	h := m.tableHeight()
	m.table = table.New(
		table.WithColumns(m.columns()),
		table.WithHeight(h),
		table.WithFocused(true),
	)
	m.searchInput = textinput.New()
	m.searchInput.Placeholder = i18n.T("sftp.search_placeholder")
	m.searchInput.CharLimit = 128
	m.searchInput.Width = searchInputWidth(m.width, i18n.T("search.prompt"))
	m.refresh()
	return m
}

// RunLocalBrowserMode runs the standalone local file browser TUI.
func RunLocalBrowserMode(startDir, currentVersion string, noUpdateCheck bool) error {
	i18n.Init("")
	m := NewLocalBrowserForm(NewStyles(80), 80, 24, startDir)
	_ = currentVersion
	_ = noUpdateCheck
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	if err != nil {
		return fmt.Errorf("error running local browser: %w", err)
	}
	return nil
}

func (m *localBrowserModel) Init() tea.Cmd {
	return nil
}

func (m *localBrowserModel) tableHeight() int {
	// Frame budget: header(1) + path(1) + search(3) + table(h+2) +
	// help(2 tall, 1 compact). The frame must never exceed the terminal
	// height: on overflow the alt-screen scrolls and the diff renderer
	// never rewrites the unchanged header, losing it permanently.
	overhead := 9
	if m.height < 20 {
		overhead = 7
	}
	h := m.height - overhead
	if h < 5 {
		h = 5
	}
	return h
}

func (m *localBrowserModel) columns() []table.Column {
	inner := m.width - 4 - 6
	if inner < 12 {
		inner = 12
	}
	nameW := inner - 5 - 8
	if nameW < 8 {
		nameW = 8
	}
	return []table.Column{
		{Title: i18n.T("sftp.col_name"), Width: nameW},
		{Title: i18n.T("sftp.col_type"), Width: 5},
		{Title: i18n.T("sftp.col_size"), Width: 8},
	}
}

func (m *localBrowserModel) refresh() {
	m.files = nil
	if m.showingDrives {
		m.files = listLocalDrives()
		m.updateRows()
		return
	}
	entries, err := os.ReadDir(m.cwd)
	if err != nil {
		return
	}
	for _, e := range entries {
		m.files = append(m.files, filepath.Join(m.cwd, e.Name()))
	}
	m.sortFiles()
	m.updateRows()
}

func (m *localBrowserModel) sortFiles() {
	if m.showingDrives {
		sort.Strings(m.files)
		return
	}
	stat := map[string]os.FileInfo{}
	for _, f := range m.files {
		if st, err := os.Stat(f); err == nil {
			stat[f] = st
		}
	}
	sort.SliceStable(m.files, func(i, j int) bool {
		ai, aj := stat[m.files[i]], stat[m.files[j]]
		if ai != nil && aj != nil && ai.IsDir() != aj.IsDir() {
			return ai.IsDir()
		}
		switch m.sortMode {
		case localSortBySize:
			if ai != nil && aj != nil && ai.Size() != aj.Size() {
				return ai.Size() > aj.Size()
			}
		case localSortByModified:
			if ai != nil && aj != nil && !ai.ModTime().Equal(aj.ModTime()) {
				return ai.ModTime().After(aj.ModTime())
			}
		}
		return strings.ToLower(filepath.Base(m.files[i])) < strings.ToLower(filepath.Base(m.files[j]))
	})
}

func (m *localBrowserModel) sortModeLabel() string {
	switch m.sortMode {
	case localSortBySize:
		return i18n.T("sftp.col_size")
	case localSortByModified:
		return i18n.T("sftp.col_modified")
	default:
		return i18n.T("sftp.col_name")
	}
}

func (m *localBrowserModel) filtered() []string {
	q := strings.ToLower(strings.TrimSpace(m.searchInput.Value()))
	if q == "" || m.showingDrives {
		return m.files
	}
	words := strings.Fields(q)
	var out []string
	for _, f := range m.files {
		name := strings.ToLower(filepath.Base(f))
		ok := true
		for _, w := range words {
			if !strings.Contains(name, w) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, f)
		}
	}
	return out
}

func (m *localBrowserModel) updateRows() {
	m.table.SetColumns(m.columns())
	var rows []table.Row
	for _, f := range m.filtered() {
		name := filepath.Base(f)
		if m.showingDrives || name == "" {
			name = f
		}
		kind := i18n.T("sftp.type_file")
		sz := ""
		if !m.showingDrives {
			info, err := os.Stat(f)
			if err != nil {
				continue
			}
			sz = formatSize(info.Size())
			if info.IsDir() {
				name = "📁 " + name
				kind = i18n.T("sftp.type_dir")
				sz = ""
			} else {
				name = "📄 " + name
			}
		}
		rows = append(rows, table.Row{name, kind, sz})
	}
	if len(rows) == 0 {
		emptyRow := make(table.Row, 3)
		emptyRow[0] = i18n.T("sftp.empty_dir")
		rows = append(rows, emptyRow)
	}
	m.table.SetRows(rows)
	if m.table.Cursor() >= len(rows) {
		m.table.SetCursor(0)
	}
}

func (m *localBrowserModel) selected() string {
	files := m.filtered()
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(files) {
		return ""
	}
	return files[idx]
}

func (m *localBrowserModel) setStatus(s string) {
	m.statusMsg = s
	m.statusExpiry = time.Now().Add(4 * time.Second)
}

func (m *localBrowserModel) statusActive() bool {
	return m.statusMsg != "" && time.Now().Before(m.statusExpiry)
}

func (m *localBrowserModel) enterSearch() tea.Cmd {
	m.searchMode = true
	m.searchInput.Focus()
	m.table.Blur()
	return textinput.Blink
}

func (m *localBrowserModel) exitSearch() {
	m.searchMode = false
	m.searchInput.Blur()
	m.table.Focus()
}

// openExternal opens a file with the OS default application.
func openExternal(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// revealInManagerCmd builds the command that selects path in the system
// file manager: Finder/Explorer select it; on Linux it prefers nautilus
// --select, then the FileManager1 ShowItems call, then opening the
// parent dir with xdg-open.
func revealInManagerCmd(path string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", path)
	case "windows":
		return exec.Command("explorer", "/select,"+path)
	default:
		// Ubuntu/GNOME default first, then the desktop-agnostic
		// FileManager1 call (VS Code uses the same), then xdg-open.
		if _, err := exec.LookPath("nautilus"); err == nil {
			return exec.Command("nautilus", "--select", path)
		}
		if _, err := exec.LookPath("dbus-send"); err == nil {
			return exec.Command("dbus-send", "--session",
				"--dest=org.freedesktop.FileManager1",
				"--type=method_call",
				"/org/freedesktop/FileManager1",
				"org.freedesktop.FileManager1.ShowItems",
				"array:string:file://"+path, "string:")
		}
		return exec.Command("xdg-open", filepath.Dir(path))
	}
}

func (m *localBrowserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case localDoneMsg:
		return m, tea.Quit
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.searchInput.Width = searchInputWidth(m.width, i18n.T("search.prompt"))
		m.table.SetHeight(m.tableHeight())
		m.updateRows()
		return m, nil

	case tea.KeyMsg:
		if m.showInfo {
			switch msg.String() {
			case "esc", "i", "enter", "q":
				m.showInfo = false
				m.entryInfo = nil
				return m, nil
			}
			return m, nil
		}
		switch m.mode {
		case localMkdirInput:
			return m.handleMkdirInput(msg)
		case localRenameInput:
			return m.handleRenameInput(msg)
		case localDeleteConfirm:
			return m.handleDeleteConfirm(msg)
		}
		if m.searchMode {
			return m.handleSearchKeys(msg)
		}
		return m.handleBrowseKeys(msg)
	}

	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *localBrowserModel) handleBrowseKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg.String() {
	case "esc", "q":
		return m, func() tea.Msg { return localDoneMsg{} }
	case "/", "ctrl+f":
		return m, m.enterSearch()
	case "left", "h", "backspace":
		return m, m.goParent()
	case "right", "l", "enter", "o":
		return m, m.openSelected()
	case "n":
		m.mode = localMkdirInput
		m.inputBuffer = ""
		m.inputPrompt = i18n.T("sftp.mkdir_prompt")
		return m, nil
	case "d":
		full := m.selected()
		if full == "" || m.showingDrives {
			return m, nil
		}
		m.pendingPath = full
		m.mode = localDeleteConfirm
		return m, nil
	case "R":
		full := m.selected()
		if full == "" || m.showingDrives {
			return m, nil
		}
		m.pendingPath = full
		m.mode = localRenameInput
		m.inputBuffer = filepath.Base(full)
		m.inputPrompt = i18n.T("sftp.rename_prompt")
		return m, nil
	case "e":
		full := m.selected()
		if full == "" || m.showingDrives {
			return m, nil
		}
		if err := revealInManagerCmd(full).Start(); err != nil {
			m.setStatus(err.Error())
		}
		return m, nil
	case "i":
		full := m.selected()
		if full == "" || m.showingDrives {
			return m, nil
		}
		info := &localEntryInfo{name: filepath.Base(full), path: full}
		if st, err := os.Stat(full); err == nil {
			info.isDir = st.IsDir()
			info.size = st.Size()
			info.modTime = st.ModTime()
		}
		m.entryInfo = info
		m.showInfo = true
		return m, nil
	case "r":
		m.refresh()
		m.setStatus(i18n.T("ftp.refreshed"))
		return m, nil
	case "s":
		m.sortMode = (m.sortMode + 1) % numLocalSortModes
		m.sortFiles()
		m.updateRows()
		m.table.SetCursor(0)
		m.setStatus(i18n.T("local.sort_toast", m.sortModeLabel()))
		return m, nil
	case "up", "k", "down", "j", "pgup", "pgdown", "home", "end":
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *localBrowserModel) goParent() tea.Cmd {
	if m.showingDrives {
		return nil
	}
	if isLocalFilesystemRoot(m.cwd) {
		if drives := listLocalDrives(); len(drives) > 0 {
			m.showingDrives = true
			m.refresh()
			m.table.SetCursor(0)
		}
		return nil
	}
	parent := filepath.Dir(m.cwd)
	if parent == m.cwd || parent == "." {
		return nil
	}
	m.cwd = parent
	m.refresh()
	m.table.SetCursor(0)
	return nil
}

func (m *localBrowserModel) openSelected() tea.Cmd {
	full := m.selected()
	if full == "" {
		return nil
	}
	if m.showingDrives {
		m.showingDrives = false
		m.cwd = full
		m.refresh()
		m.table.SetCursor(0)
		return nil
	}
	info, err := os.Stat(full)
	if err != nil {
		return nil
	}
	if info.IsDir() {
		m.cwd = full
		m.refresh()
		m.table.SetCursor(0)
		return nil
	}
	if err := openExternal(full); err != nil {
		m.setStatus(err.Error())
	}
	return nil
}

func (m *localBrowserModel) handleSearchKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "enter", "tab":
		m.exitSearch()
		m.updateRows()
		return m, nil
	}
	var cmd tea.Cmd
	oldValue := m.searchInput.Value()
	m.searchInput, cmd = m.searchInput.Update(msg)
	if m.searchInput.Value() != oldValue {
		m.updateRows()
	}
	return m, cmd
}

func (m *localBrowserModel) handleMkdirInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = localBrowse
		m.inputBuffer = ""
		return m, nil
	case "enter":
		name := strings.TrimSpace(m.inputBuffer)
		m.inputBuffer = ""
		m.mode = localBrowse
		if name == "" {
			return m, nil
		}
		if err := os.Mkdir(filepath.Join(m.cwd, name), 0755); err != nil {
			m.setStatus(i18n.T("sftp.mkdir_failed", err))
		} else {
			m.setStatus(i18n.T("sftp.mkdir_success", name))
		}
		m.refresh()
		return m, nil
	case "backspace":
		if r := []rune(m.inputBuffer); len(r) > 0 {
			m.inputBuffer = string(r[:len(r)-1])
		}
		return m, nil
	default:
		if len(msg.Runes) == 1 && msg.Type == tea.KeyRunes {
			m.inputBuffer += string(msg.Runes)
		}
		return m, nil
	}
}

func (m *localBrowserModel) handleRenameInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = localBrowse
		m.pendingPath = ""
		m.inputBuffer = ""
		return m, nil
	case "enter":
		newName := strings.TrimSpace(m.inputBuffer)
		m.inputBuffer = ""
		m.mode = localBrowse
		old := m.pendingPath
		m.pendingPath = ""
		if newName == "" || old == "" || newName == filepath.Base(old) {
			return m, nil
		}
		if err := os.Rename(old, filepath.Join(filepath.Dir(old), newName)); err != nil {
			m.setStatus(i18n.T("sftp.rename_failed", err))
		} else {
			m.setStatus(i18n.T("sftp.rename_success", filepath.Base(old), newName))
		}
		m.refresh()
		return m, nil
	case "backspace":
		if r := []rune(m.inputBuffer); len(r) > 0 {
			m.inputBuffer = string(r[:len(r)-1])
		}
		return m, nil
	default:
		if len(msg.Runes) == 1 && msg.Type == tea.KeyRunes {
			m.inputBuffer += string(msg.Runes)
		}
		return m, nil
	}
}

func (m *localBrowserModel) handleDeleteConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		m.mode = localBrowse
		target := m.pendingPath
		m.pendingPath = ""
		if target == "" {
			return m, nil
		}
		if err := os.RemoveAll(target); err != nil {
			m.setStatus(i18n.T("sftp.delete_failed", err.Error()))
		} else {
			m.setStatus(i18n.T("sftp.delete_success", filepath.Base(target)))
		}
		m.refresh()
		return m, nil
	case "n", "N", "esc":
		m.mode = localBrowse
		m.pendingPath = ""
		return m, nil
	}
	return m, nil
}

func (m *localBrowserModel) renderInfoView() string {
	info := m.entryInfo
	if info == nil {
		return ""
	}
	kind := i18n.T("sftp.type_file")
	if info.isDir {
		kind = i18n.T("sftp.type_dir")
	}
	size := formatSize(info.size)
	mod := info.modTime.Format("Jan 02 15:04")
	if info.isDir {
		size = i18n.T("info.not_set")
	}
	if info.modTime.IsZero() {
		mod = i18n.T("info.not_set")
	}
	var b strings.Builder
	b.WriteString(m.styles.FormTitle.Render(" "+i18n.T("sftp.entry_info_title", info.name)+" ") + "\n\n")
	rows := [][2]string{
		{i18n.T("sftp.col_name"), info.name},
		{i18n.T("sftp.col_type"), kind},
		{i18n.T("sftp.col_size"), size},
		{i18n.T("sftp.col_modified"), mod},
		{i18n.T("sftp.info_path"), info.path},
	}
	for _, r := range rows {
		label := "  " + r[0] + ":"
		if w := ansi.StringWidth(label); w < 16 {
			label += strings.Repeat(" ", 16-w)
		}
		b.WriteString(m.styles.FormField.Render(label) + " " + r[1] + "\n")
	}
	b.WriteString("\n" + m.styles.HelpText.Render("  "+i18n.T("sftp.info_help")))
	return renderFormPage(m.styles, m.width, b.String())
}

func (m *localBrowserModel) View() string {
	if m.showInfo && m.entryInfo != nil {
		return m.renderInfoView()
	}

	pathStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("36")).Bold(true)
	header := m.styles.Header.Render(i18n.T("local.title"))
	paths := pathStyle.Render(fmt.Sprintf("  %s", truncatePath(localBrowseLabel(m.cwd, m.showingDrives), m.width-4)))

	pw := m.width - 4
	if pw < 20 {
		pw = 20
	}
	tableStyle := m.styles.TableFocused
	if m.searchMode {
		tableStyle = m.styles.TableUnfocused
	}
	panes := tableStyle.Width(pw).Render(m.table.View())

	var extras []string
	if m.statusActive() {
		statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("36"))
		extras = append(extras, statusStyle.Render(" ✓ "+m.statusMsg))
	}
	if m.mode == localMkdirInput || m.mode == localRenameInput {
		inputStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(PrimaryColor))
		extras = append(extras, inputStyle.Render(fmt.Sprintf("  %s %s_", m.inputPrompt, m.inputBuffer)))
	}
	if m.mode == localDeleteConfirm && m.pendingPath != "" {
		name := filepath.Base(m.pendingPath)
		confirmKey := "sftp.delete_confirm"
		if st, err := os.Stat(m.pendingPath); err == nil && st.IsDir() {
			confirmKey = "sftp.delete_dir_confirm"
		}
		extras = append(extras, lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(
			i18n.T(confirmKey, name)))
	}

	var helpParts []string
	if m.height < 20 {
		helpParts = append(helpParts, i18n.T("local.help_1"))
	} else {
		helpParts = append(helpParts, i18n.T("local.help_1"), i18n.T("local.help_2"))
	}
	helpParts = dedupeStrings(helpParts)

	parts := []string{header, paths, renderSearchBar(m.styles, m.searchMode, i18n.T("search.prompt"), m.searchInput.View(), m.width)}
	parts = append(parts, panes)
	parts = append(parts, extras...)
	if help := strings.Join(helpParts, "\n"); help != "" {
		parts = append(parts, renderHelpText(m.styles, help, m.width))
	}
	return m.styles.App.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}
