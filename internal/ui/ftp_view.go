package ui

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/ftpclient"
	"github.com/zsuroy/ctty/internal/ftpconfig"
	"github.com/zsuroy/ctty/internal/ftpcred"
	"github.com/zsuroy/ctty/internal/i18n"
)

// FTP dual-pane browser modes (modeled on SFTP browse / upload-select).
type ftpMode int

const (
	ftpBrowse      ftpMode = iota // remote pane focused
	ftpLocalBrowse                // local pane focused
	ftpDownloadConfirm
	ftpMkdirInput
	ftpRenameInput
	ftpDeleteConfirm
	ftpPasswordInput
	ftpError
)

// Narrow terminals show one focused pane (SFTP-style) instead of cramped dual columns.
const ftpNarrowWidth = 80

type ftpFormModel struct {
	styles   Styles
	width    int
	height   int
	siteName string
	site     ftpconfig.FTPSite
	layout   config.FTPLayout

	client     *ftpclient.Client
	remoteTbl  table.Model
	localTbl   table.Model
	entries    []ftpclient.RemoteEntry
	cwd        string
	localCwd   string
	mode       ftpMode
	loading    bool
	refreshing bool
	loadError  string
	password   string
	focusLocal bool

	progressDone   int64
	progressTotal  int64
	progressFile   string
	progressGen    int
	transferring   bool
	transferCancel context.CancelFunc
	queue          ftpTransferQueue

	inputBuffer  string
	inputPrompt  string
	selected     *ftpclient.RemoteEntry
	statusMsg    string
	statusExpiry time.Time
	statusGen    int

	// Local-target management: when true, the active mkdir/rename input
	// or delete confirm operates on pendingLocalPath via os calls.
	localOp          bool
	pendingLocalPath string

	showInfo  bool
	entryInfo *ftpEntryInfo

	localFiles  []string
	searchInput textinput.Model
	searchMode  bool
}

// ftpEntryInfo is a snapshot of the file or directory shown in the info overlay.
type ftpEntryInfo struct {
	name    string
	isDir   bool
	size    int64
	modTime time.Time
	path    string
}

type ftpConnectedMsg struct {
	client *ftpclient.Client
	cwd    string
}
type ftpEntriesMsg struct {
	entries []ftpclient.RemoteEntry
	cwd     string
	err     error
}
type ftpErrorMsg struct{ err error }
type ftpPasswordPromptMsg struct{}
type ftpDownloadResultMsg struct {
	gen      int
	filename string
	success  bool
	err      error
}
type ftpUploadResultMsg struct {
	gen      int
	filename string
	success  bool
	err      error
}
type ftpMkdirResultMsg struct {
	name    string
	success bool
	err     error
}
type ftpDeleteResultMsg struct {
	filename string
	success  bool
	err      error
}
type ftpRenameResultMsg struct {
	oldName string
	newName string
	success bool
	err     error
}
type ftpProgressMsg struct {
	gen      int
	filename string
	done     int64
	total    int64
	isUpload bool
}
type ftpDoneMsg struct{}
type ftpBackToSitesMsg struct{}

type ftpStatusExpiredMsg struct {
	gen int
}

// NewFTPForm creates the dual-pane FTP browser for a saved site.
func NewFTPForm(styles Styles, width, height int, siteName string) *ftpFormModel {
	return NewFTPFormWithLayout(styles, width, height, siteName, config.FTPLayoutDual)
}

// NewFTPFormWithLayout creates an FTP browser using the configured pane layout.
func NewFTPFormWithLayout(styles Styles, width, height int, siteName string, layout config.FTPLayout) *ftpFormModel {
	site, _ := ftpconfig.Find(siteName)
	m := &ftpFormModel{
		styles:     styles,
		width:      width,
		height:     height,
		siteName:   siteName,
		site:       site,
		layout:     config.NormalizeFTPLayout(layout),
		mode:       ftpBrowse,
		loading:    true,
		localCwd:   defaultLocalUploadDir(),
		focusLocal: false,
	}
	h := m.paneTableHeight()
	cols := m.remoteColumns()
	m.remoteTbl = table.New(table.WithColumns(cols), table.WithHeight(h), table.WithFocused(true))
	m.localTbl = table.New(table.WithColumns(m.localColumns()), table.WithHeight(h), table.WithFocused(false))
	m.searchInput = textinput.New()
	m.searchInput.Placeholder = i18n.T("search.placeholder")
	m.searchInput.CharLimit = 50
	m.searchInput.Width = searchInputWidth(m.width, i18n.T("search.prompt"))
	return m
}

func (m *ftpFormModel) Init() tea.Cmd {
	return m.connectCmd("")
}

func (m *ftpFormModel) connectCmd(password string) tea.Cmd {
	return func() tea.Msg {
		client, err := ftpclient.Connect(m.site, password)
		if err != nil {
			msg := err.Error()
			if strings.Contains(msg, "login") || strings.Contains(msg, "530") ||
				strings.Contains(msg, "auth") || strings.Contains(msg, "Login") {
				return ftpPasswordPromptMsg{}
			}
			return ftpErrorMsg{err: err}
		}
		cwd, err := client.CurrentDir()
		if err != nil {
			cwd = "/"
		}
		return ftpConnectedMsg{client: client, cwd: cwd}
	}
}

func (m *ftpFormModel) loadDirCmd(dir string) tea.Cmd {
	return func() tea.Msg {
		entries, err := m.client.ListDir(dir)
		if err != nil {
			return ftpEntriesMsg{cwd: dir, err: err}
		}
		return ftpEntriesMsg{entries: entries, cwd: dir}
	}
}

func (m *ftpFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case ftpConnectedMsg:
		m.client = msg.client
		m.cwd = msg.cwd
		m.loading = false
		m.mode = ftpBrowse
		m.refreshLocal()
		return m, m.loadDirCmd(m.cwd)

	case ftpPasswordPromptMsg:
		m.loading = false
		m.mode = ftpPasswordInput
		m.inputBuffer = ""
		m.inputPrompt = fmt.Sprintf("FTP password for %s@%s:", m.site.User, m.site.Addr())
		return m, nil

	case ftpErrorMsg:
		m.loading = false
		m.mode = ftpError
		m.loadError = msg.err.Error()
		return m, nil

	case ftpEntriesMsg:
		m.loading = false
		if msg.err != nil {
			m.refreshing = false
			return m, m.setStatus("Error: " + msg.err.Error())
		}
		m.cwd = msg.cwd
		m.entries = msg.entries
		sort.Slice(m.entries, func(i, j int) bool {
			if m.entries[i].IsDir != m.entries[j].IsDir {
				return m.entries[i].IsDir
			}
			return strings.ToLower(m.entries[i].Name) < strings.ToLower(m.entries[j].Name)
		})
		m.updateRemoteRows()
		m.clampActiveCursor()
		if m.refreshing {
			m.refreshing = false
			return m, m.setStatus(i18n.T("ftp.refreshed"))
		}
		return m, nil

	case ftpStatusExpiredMsg:
		if msg.gen == m.statusGen && !m.refreshing && !m.transferring && !m.statusActive() {
			m.statusMsg = ""
			m.statusExpiry = time.Time{}
		}
		return m, nil

	case ftpProgressMsg:
		if staleFTPProgress(m.transferring, m.progressGen, msg.gen) {
			return m, nil
		}
		m.progressDone = msg.done
		m.progressTotal = msg.total
		m.progressFile = msg.filename
		job := m.queue.current()
		if job.filename == "" {
			job = ftpTransferJob{filename: msg.filename, isUpload: msg.isUpload}
		}
		cur, total := m.queue.position()
		m.statusMsg = formatFTPProgress(job, msg.done, msg.total, cur, total)
		m.statusExpiry = time.Now().Add(10 * time.Second)
		return m, tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
			return ftpProgressMsg{
				gen: msg.gen, filename: job.filename,
				done: m.progressDone, total: m.progressTotal, isUpload: job.isUpload,
			}
		})

	case ftpDownloadResultMsg:
		return m, m.handleTransferResult(msg.gen, msg.filename, msg.success, msg.err, false)
	case ftpUploadResultMsg:
		return m, m.handleTransferResult(msg.gen, msg.filename, msg.success, msg.err, true)

	case ftpMkdirResultMsg:
		m.mode = ftpBrowse
		m.inputBuffer = ""
		m.loading = false
		if msg.success {
			statusCmd := m.setStatus(i18n.T("ftp.dir_created", msg.name))
			return m, tea.Batch(statusCmd, m.loadDirCmd(m.cwd))
		}
		return m, m.setStatus(i18n.T("ftp.mkdir_failed", msg.err))

	case ftpDeleteResultMsg:
		m.mode = ftpBrowse
		m.selected = nil
		m.loading = false
		if msg.success {
			statusCmd := m.setStatus(i18n.T("ftp.delete_success", msg.filename))
			return m, tea.Batch(statusCmd, m.loadDirCmd(m.cwd))
		}
		return m, m.setStatus(i18n.T("ftp.delete_failed", msg.err))

	case ftpRenameResultMsg:
		m.mode = ftpBrowse
		m.selected = nil
		m.inputBuffer = ""
		m.loading = false
		if msg.success {
			statusCmd := m.setStatus(i18n.T("ftp.rename_success", msg.oldName, msg.newName))
			return m, tea.Batch(statusCmd, m.loadDirCmd(m.cwd))
		}
		return m, m.setStatus(i18n.T("ftp.rename_failed", msg.err))

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.searchInput.Width = searchInputWidth(m.width, i18n.T("search.prompt"))
		h := m.paneTableHeight()
		m.remoteTbl.SetHeight(h)
		m.localTbl.SetHeight(h)
		m.updateRemoteRows()
		m.updateLocalRows()
		m.clampActiveCursor()
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
		if m.mode == ftpError {
			if msg.String() == "esc" || msg.String() == "enter" || msg.String() == "q" || msg.String() == "ctrl+c" {
				return m, func() tea.Msg { return ftpDoneMsg{} }
			}
			return m, nil
		}
		if m.mode == ftpPasswordInput {
			return m.handlePasswordInput(msg)
		}
		if m.mode == ftpDownloadConfirm {
			return m.handleDownloadConfirm(msg)
		}
		if m.mode == ftpMkdirInput {
			return m.handleMkdirInput(msg)
		}
		if m.mode == ftpRenameInput {
			return m.handleRenameInput(msg)
		}
		if m.mode == ftpDeleteConfirm {
			return m.handleDeleteConfirm(msg)
		}
		if m.searchMode {
			return m.handleSearchKeys(msg)
		}
		return m.handleBrowseKeys(msg)
	}

	if m.focusLocal {
		m.localTbl, cmd = m.localTbl.Update(msg)
	} else {
		m.remoteTbl, cmd = m.remoteTbl.Update(msg)
	}
	return m, cmd
}

func (m *ftpFormModel) handlePasswordInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		return m, func() tea.Msg { return ftpDoneMsg{} }
	case "enter":
		m.password = m.inputBuffer
		m.loading = true
		m.mode = ftpBrowse
		// Persist to dedicated FTP cred store (not SSH vault).
		_ = ftpcred.SetPassword(m.siteName, m.password)
		return m, m.connectCmd(m.password)
	case "backspace":
		if len(m.inputBuffer) > 0 {
			m.inputBuffer = m.inputBuffer[:len(m.inputBuffer)-1]
		}
		return m, nil
	default:
		if len(msg.Runes) == 1 && msg.Type == tea.KeyRunes {
			m.inputBuffer += string(msg.Runes)
		}
		return m, nil
	}
}

func (m *ftpFormModel) handleDownloadConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		m.mode = ftpBrowse
		if m.selected == nil {
			return m, nil
		}
		remote := path.Join(m.cwd, m.selected.Name)
		local := filepath.Join(m.localCwd, m.selected.Name)
		job := ftpTransferJob{
			filename: m.selected.Name, localPath: local, remotePath: remote, isUpload: false,
		}
		return m, m.requestTransfer(job)
	case "n", "N", "esc":
		m.mode = ftpBrowse
		return m, nil
	}
	return m, nil
}

func (m *ftpFormModel) handleMkdirInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = ftpBrowse
		m.inputBuffer = ""
		m.localOp = false
		return m, nil
	case "enter":
		name := strings.TrimSpace(m.inputBuffer)
		m.inputBuffer = ""
		m.mode = ftpBrowse
		if name == "" {
			m.localOp = false
			return m, nil
		}
		if m.localOp {
			m.localOp = false
			if err := os.Mkdir(filepath.Join(m.localCwd, name), 0755); err != nil {
				return m, m.setStatus(i18n.T("ftp.mkdir_failed", err))
			}
			m.refreshLocal()
			m.clampActiveCursor()
			return m, m.setStatus(i18n.T("ftp.dir_created", name))
		}
		m.loading = true
		return m, m.mkdirCmd(name, path.Join(m.cwd, name))
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

func (m *ftpFormModel) handleRenameInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = ftpBrowse
		m.selected = nil
		m.inputBuffer = ""
		m.localOp = false
		m.pendingLocalPath = ""
		return m, nil
	case "enter":
		newName := strings.TrimSpace(m.inputBuffer)
		m.inputBuffer = ""
		m.mode = ftpBrowse
		if m.localOp {
			m.localOp = false
			old := m.pendingLocalPath
			m.pendingLocalPath = ""
			if newName == "" || old == "" || newName == filepath.Base(old) {
				return m, nil
			}
			if err := os.Rename(old, filepath.Join(filepath.Dir(old), newName)); err != nil {
				return m, m.setStatus(i18n.T("ftp.rename_failed", err))
			}
			m.refreshLocal()
			m.clampActiveCursor()
			return m, m.setStatus(i18n.T("ftp.rename_success", filepath.Base(old), newName))
		}
		if newName == "" || m.selected == nil || newName == m.selected.Name {
			m.selected = nil
			return m, nil
		}
		oldName := m.selected.Name
		m.selected = nil
		m.loading = true
		return m, m.renameCmd(oldName, path.Join(m.cwd, oldName), path.Join(m.cwd, newName))
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

func (m *ftpFormModel) handleDeleteConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		m.mode = ftpBrowse
		if m.localOp {
			m.localOp = false
			target := m.pendingLocalPath
			m.pendingLocalPath = ""
			if target == "" {
				return m, nil
			}
			if err := os.Remove(target); err != nil {
				return m, m.setStatus(i18n.T("ftp.delete_failed", err))
			}
			m.refreshLocal()
			m.clampActiveCursor()
			return m, m.setStatus(i18n.T("ftp.delete_success", filepath.Base(target)))
		}
		if m.selected == nil {
			return m, nil
		}
		name := m.selected.Name
		remote := path.Join(m.cwd, name)
		m.selected = nil
		m.loading = true
		return m, m.deleteCmd(name, remote)
	case "n", "N", "esc":
		m.mode = ftpBrowse
		m.selected = nil
		m.localOp = false
		m.pendingLocalPath = ""
		return m, nil
	}
	return m, nil
}

func (m *ftpFormModel) setFocusLocal(local bool) {
	m.focusLocal = local
	if local {
		m.mode = ftpLocalBrowse
		m.localTbl.Focus()
		m.remoteTbl.Blur()
	} else {
		m.mode = ftpBrowse
		m.remoteTbl.Focus()
		m.localTbl.Blur()
	}
	m.updateRemoteRows()
	m.updateLocalRows()
	m.clampActiveCursor()
}

// toggleLayout flips dual/single pane layout and persists it to app config.
func (m *ftpFormModel) toggleLayout() tea.Cmd {
	if m.layout == config.FTPLayoutSingle {
		m.layout = config.FTPLayoutDual
	} else {
		m.layout = config.FTPLayoutSingle
	}
	m.updateRemoteRows()
	m.updateLocalRows()
	m.clampActiveCursor()
	persistFTPLayout(m.layout)
	name := i18n.T("ftp.layout_dual")
	if m.layout == config.FTPLayoutSingle {
		name = i18n.T("ftp.layout_single")
	}
	return m.setStatus(i18n.T("ftp.layout_status", name))
}

// persistFTPLayout writes the chosen layout to ~/.config/ctty/config.json.
// Failures are silent — the in-memory layout still applies for this session.
func persistFTPLayout(layout config.FTPLayout) {
	cfg, err := config.LoadAppConfig()
	if err != nil || cfg == nil {
		fallback := config.GetDefaultAppConfig()
		cfg = &fallback
	}
	cfg.FTPLayout = config.NormalizeFTPLayout(layout)
	_ = config.SaveAppConfig(cfg)
}
func (m *ftpFormModel) enterSearch() tea.Cmd {
	m.searchMode = true
	m.searchInput.Focus()
	m.remoteTbl.Blur()
	m.localTbl.Blur()
	return textinput.Blink
}

func (m *ftpFormModel) exitSearch() {
	m.searchMode = false
	m.searchInput.Blur()
	if m.focusLocal {
		m.localTbl.Focus()
		m.remoteTbl.Blur()
	} else {
		m.remoteTbl.Focus()
		m.localTbl.Blur()
	}
}
func (m *ftpFormModel) clampTableCursor(tbl *table.Model, count int) {
	if count == 0 || (tbl.Cursor() >= 0 && tbl.Cursor() < count) {
		return
	}
	tbl.SetCursor(0)
}

func (m *ftpFormModel) clampActiveCursor() {
	if m.focusLocal {
		m.clampTableCursor(&m.localTbl, len(m.filteredLocal()))
	} else {
		m.clampTableCursor(&m.remoteTbl, len(m.filteredRemote()))
	}
}

func (m *ftpFormModel) handleBrowseKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc", "q":
		if m.transferring {
			m.cancelTransfer()
			return m, nil
		}
		if m.client != nil {
			_ = m.client.Close()
			m.client = nil
		}
		return m, func() tea.Msg { return ftpDoneMsg{} }
	case "tab", "shift+tab":
		// Keep pane switching consistent with SFTP; search owns Tab while active.
		m.setFocusLocal(!m.focusLocal)
		return m, nil
	case "u":
		if !m.focusLocal {
			m.setFocusLocal(true)
		}
		return m, nil
	case "/", "ctrl+f":
		return m, m.enterSearch()
	case "left", "h", "backspace":
		if m.focusLocal {
			return m, m.localUp()
		}
		return m, m.remoteUp()
	case "right", "l":
		// Open directory only (Enter handles file transfer).
		if m.focusLocal {
			return m.handleLocalOpenDir()
		}
		return m.handleRemoteOpenDir()
	case "enter":
		// Enter = enter dir / transfer file
		if m.focusLocal {
			return m.handleLocalEnter()
		}
		return m.handleRemoteEnter()
	case "d":
		if m.transferring {
			return m, nil
		}
		return m.startDeleteConfirm()
	case "n":
		if m.transferring {
			return m, nil
		}
		m.localOp = m.focusLocal
		m.mode = ftpMkdirInput
		m.inputBuffer = ""
		m.inputPrompt = i18n.T("ftp.mkdir_prompt")
		return m, nil
	case "R":
		if m.transferring {
			return m, nil
		}
		return m.startRenameInput()
	case "v", "V":
		return m, m.toggleLayout()
	case "i":
		if info := m.focusedEntryInfo(); info != nil {
			m.entryInfo = info
			m.showInfo = true
		}
		return m, nil
	case "r":
		if m.transferring {
			return m, nil
		}
		if m.focusLocal {
			m.refreshLocal()
			return m, m.setStatus(i18n.T("ftp.refreshed"))
		}
		m.refreshing = true
		m.loading = true
		statusCmd := m.setStatus(i18n.T("ftp.refreshing"))
		return m, tea.Batch(statusCmd, m.loadDirCmd(m.cwd))
	case "up", "k", "down", "j", "pgup", "pgdown", "home", "end":
		var cmd tea.Cmd
		if m.focusLocal {
			m.localTbl, cmd = m.localTbl.Update(msg)
		} else {
			m.remoteTbl, cmd = m.remoteTbl.Update(msg)
		}
		return m, cmd
	}
	return m, nil
}

func (m *ftpFormModel) handleSearchKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "enter", "tab":
		m.exitSearch()
		m.updateRemoteRows()
		m.updateLocalRows()
		m.clampActiveCursor()
		return m, nil
	}
	var cmd tea.Cmd
	oldValue := m.searchInput.Value()
	m.searchInput, cmd = m.searchInput.Update(msg)
	if m.searchInput.Value() != oldValue {
		m.updateRemoteRows()
		m.updateLocalRows()
		m.clampActiveCursor()
	}
	return m, cmd
}

func (m *ftpFormModel) handleRemoteOpenDir() (tea.Model, tea.Cmd) {
	e := m.selectedRemote()
	if e == nil || !e.IsDir {
		return m, nil
	}
	next := path.Join(m.cwd, e.Name)
	m.loading = true
	return m, m.loadDirCmd(next)
}

func (m *ftpFormModel) handleRemoteEnter() (tea.Model, tea.Cmd) {
	e := m.selectedRemote()
	if e == nil {
		return m, nil
	}
	if e.IsDir {
		next := path.Join(m.cwd, e.Name)
		m.loading = true
		return m, m.loadDirCmd(next)
	}
	// File: one confirm, then transfer.
	if m.transferring {
		remote := path.Join(m.cwd, e.Name)
		local := filepath.Join(m.localCwd, e.Name)
		return m, m.requestTransfer(ftpTransferJob{
			filename: e.Name, localPath: local, remotePath: remote, isUpload: false,
		})
	}
	m.selected = e
	m.mode = ftpDownloadConfirm
	return m, nil
}

func (m *ftpFormModel) handleLocalOpenDir() (tea.Model, tea.Cmd) {
	idx := m.localTbl.Cursor()
	files := m.filteredLocal()
	if idx < 0 || idx >= len(files) {
		return m, nil
	}
	full := files[idx]
	info, err := os.Stat(full)
	if err != nil || !info.IsDir() {
		return m, nil
	}
	m.localCwd = full
	m.refreshLocal()
	return m, nil
}

func (m *ftpFormModel) handleLocalEnter() (tea.Model, tea.Cmd) {
	idx := m.localTbl.Cursor()
	files := m.filteredLocal()
	if idx < 0 || idx >= len(files) {
		return m, nil
	}
	full := files[idx]
	info, err := os.Stat(full)
	if err != nil {
		return m, nil
	}
	if info.IsDir() {
		m.localCwd = full
		m.refreshLocal()
		return m, nil
	}
	remote := path.Join(m.cwd, filepath.Base(full))
	job := ftpTransferJob{
		filename: filepath.Base(full), localPath: full, remotePath: remote, isUpload: true,
	}
	return m, m.requestTransfer(job)
}

func (m *ftpFormModel) startDeleteConfirm() (tea.Model, tea.Cmd) {
	m.localOp = false
	m.pendingLocalPath = ""
	m.selected = nil
	if m.focusLocal {
		full := m.selectedLocal()
		if full == "" {
			return m, nil
		}
		m.localOp = true
		m.pendingLocalPath = full
		m.mode = ftpDeleteConfirm
		return m, nil
	}
	e := m.selectedRemote()
	if e == nil || e.IsDir {
		return m, nil
	}
	m.selected = e
	m.mode = ftpDeleteConfirm
	return m, nil
}

func (m *ftpFormModel) selectedLocal() string {
	files := m.filteredLocal()
	idx := m.localTbl.Cursor()
	if idx < 0 || idx >= len(files) {
		return ""
	}
	return files[idx]
}

func (m *ftpFormModel) startRenameInput() (tea.Model, tea.Cmd) {
	m.localOp = false
	m.pendingLocalPath = ""
	m.selected = nil
	if m.focusLocal {
		full := m.selectedLocal()
		if full == "" {
			return m, nil
		}
		m.localOp = true
		m.pendingLocalPath = full
		m.mode = ftpRenameInput
		m.inputBuffer = filepath.Base(full)
		m.inputPrompt = i18n.T("ftp.rename_prompt")
		return m, nil
	}
	e := m.selectedRemote()
	if e == nil {
		return m, nil
	}
	m.selected = e
	m.mode = ftpRenameInput
	m.inputBuffer = e.Name
	m.inputPrompt = i18n.T("ftp.rename_prompt")
	return m, nil
}

func (m *ftpFormModel) remoteUp() tea.Cmd {
	if m.cwd == "/" || m.cwd == "" {
		return nil
	}
	parent := path.Dir(m.cwd)
	if parent == "." {
		parent = "/"
	}
	m.loading = true
	return m.loadDirCmd(parent)
}

func (m *ftpFormModel) localUp() tea.Cmd {
	if isLocalFilesystemRoot(m.localCwd) {
		return nil
	}
	parent := filepath.Dir(m.localCwd)
	if parent == m.localCwd {
		return nil
	}
	m.localCwd = parent
	m.refreshLocal()
	return nil
}

func (m *ftpFormModel) selectedRemote() *ftpclient.RemoteEntry {
	rows := m.filteredRemote()
	idx := m.remoteTbl.Cursor()
	if idx < 0 || idx >= len(rows) {
		return nil
	}
	return &rows[idx]
}

func (m *ftpFormModel) filteredRemote() []ftpclient.RemoteEntry {
	q := strings.ToLower(strings.TrimSpace(m.searchInput.Value()))
	if q == "" || m.focusLocal {
		return m.entries
	}
	words := strings.Fields(q)
	var out []ftpclient.RemoteEntry
	for _, e := range m.entries {
		ok := true
		for _, w := range words {
			if !strings.Contains(strings.ToLower(e.Name), w) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, e)
		}
	}
	return out
}

func (m *ftpFormModel) filteredLocal() []string {
	q := strings.ToLower(strings.TrimSpace(m.searchInput.Value()))
	if q == "" || !m.focusLocal {
		return m.localFiles
	}
	words := strings.Fields(q)
	var out []string
	for _, f := range m.localFiles {
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

func (m *ftpFormModel) refreshLocal() {
	m.localFiles = nil
	entries, err := os.ReadDir(m.localCwd)
	if err != nil {
		return
	}
	for _, e := range entries {
		m.localFiles = append(m.localFiles, filepath.Join(m.localCwd, e.Name()))
	}
	sort.Slice(m.localFiles, func(i, j int) bool {
		ai, _ := os.Stat(m.localFiles[i])
		aj, _ := os.Stat(m.localFiles[j])
		if ai != nil && aj != nil && ai.IsDir() != aj.IsDir() {
			return ai.IsDir()
		}
		return strings.ToLower(filepath.Base(m.localFiles[i])) < strings.ToLower(filepath.Base(m.localFiles[j]))
	})
	m.updateLocalRows()
}

func (m *ftpFormModel) updateRemoteRows() {
	cols := m.remoteColumns()
	m.remoteTbl.SetColumns(cols)
	var rows []table.Row
	for _, e := range m.filteredRemote() {
		name := e.Name
		if e.IsDir {
			name = "📁 " + name
		} else {
			name = "📄 " + name
		}
		kind := "file"
		if e.IsDir {
			kind = "dir"
		}
		rows = append(rows, table.Row{name, kind, formatSize(e.Size)})
	}
	m.remoteTbl.SetRows(rows)
}

func (m *ftpFormModel) updateLocalRows() {
	cols := m.localColumns()
	m.localTbl.SetColumns(cols)
	var rows []table.Row
	for _, f := range m.filteredLocal() {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		kind := "file"
		sz := formatSize(info.Size())
		name := filepath.Base(f)
		if info.IsDir() {
			name = "📁 " + name
			kind = "dir"
			sz = ""
		} else {
			name = "📄 " + name
		}
		rows = append(rows, table.Row{name, kind, sz})
	}
	m.localTbl.SetRows(rows)
}

func (m *ftpFormModel) paneTableHeight() int {
	// Frame budget: header(1) + paths(2) + search(3) + table box(h+2) +
	// help(3 tall, 1 compact). The frame must never exceed the terminal
	// height: on overflow the alt-screen scrolls and the diff renderer
	// never rewrites the unchanged header, losing it permanently.
	overhead := 11
	if m.height < 20 {
		overhead = 9
	}
	h := m.height - overhead
	if h < 5 {
		h = 5
	}
	return h
}

func (m *ftpFormModel) narrow() bool {
	return m.width > 0 && m.width < ftpNarrowWidth
}

func (m *ftpFormModel) singlePane() bool {
	return m.layout == config.FTPLayoutSingle || m.narrow()
}

func (m *ftpFormModel) paneWidth() int {
	if m.singlePane() {
		// Single pane: lipgloss Width is content-only; border (2) sits outside.
		// App pad(2) + border(2) = 4 → content budget = term - 4.
		w := m.width - 4
		if w < 20 {
			w = 20
		}
		return w
	}
	// Dual pane: App pad(2) + gap("  ") + two pane borders(2*2) = 8 outside content.
	// Width(pw) is content width per pane; visual = 2*(pw+2) + 2 + 2 = 2*pw + 8.
	w := (m.width - 8) / 2
	if w < 20 {
		w = 20
	}
	return w
}

// paneContentWidth is the bubbles-table column budget inside Width(paneWidth) content.
func (m *ftpFormModel) paneContentWidth() int {
	// 3 cells * Padding(0,1)=2 → 6 chrome inside the bordered content box.
	w := m.paneWidth() - 6
	if w < 12 {
		return 12
	}
	return w
}

func (m *ftpFormModel) remoteColumns() []table.Column {
	inner := m.paneContentWidth()
	nameW := inner - 5 - 8
	if nameW < 8 {
		nameW = 8
	}
	return []table.Column{
		{Title: i18n.T("ftp.col_remote"), Width: nameW},
		{Title: i18n.T("sftp.col_type"), Width: 5},
		{Title: i18n.T("sftp.col_size"), Width: 8},
	}
}

func (m *ftpFormModel) localColumns() []table.Column {
	inner := m.paneContentWidth()
	nameW := inner - 5 - 8
	if nameW < 8 {
		nameW = 8
	}
	return []table.Column{
		{Title: i18n.T("ftp.col_local"), Width: nameW},
		{Title: i18n.T("sftp.col_type"), Width: 5},
		{Title: i18n.T("sftp.col_size"), Width: 8},
	}
}

func (m *ftpFormModel) requestTransfer(job ftpTransferJob) tea.Cmd {
	started, now := m.queue.startOrEnqueue(job)
	if !now {
		m.setStatus(fmt.Sprintf("Queued: %s", job.filename))
		return nil
	}
	return m.beginJob(started)
}

func (m *ftpFormModel) beginJob(job ftpTransferJob) tea.Cmd {
	m.transferring = true
	m.loading = true
	m.progressFile = job.filename
	m.progressDone = 0
	m.progressTotal = 0
	m.progressGen++
	gen := m.progressGen
	cur, total := m.queue.position()
	m.statusMsg = formatFTPProgress(job, 0, 0, cur, total)
	m.statusExpiry = time.Now().Add(10 * time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	m.transferCancel = cancel

	if job.isUpload {
		return m.uploadCmd(ctx, gen, job)
	}
	return m.downloadCmd(ctx, gen, job)
}

func (m *ftpFormModel) downloadCmd(ctx context.Context, gen int, job ftpTransferJob) tea.Cmd {
	download := func() tea.Msg {
		err := m.client.DownloadWithProgressCtx(ctx, job.remotePath, job.localPath, func(done, total int64) {
			m.progressDone = done
			m.progressTotal = total
		})
		if ctx.Err() != nil {
			return ftpDownloadResultMsg{gen: gen, filename: job.filename, success: false, err: ctx.Err()}
		}
		return ftpDownloadResultMsg{gen: gen, filename: job.filename, success: err == nil, err: err}
	}
	return tea.Batch(
		download,
		tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
			return ftpProgressMsg{
				gen: gen, filename: job.filename,
				done: m.progressDone, total: m.progressTotal, isUpload: false,
			}
		}),
	)
}

func (m *ftpFormModel) uploadCmd(ctx context.Context, gen int, job ftpTransferJob) tea.Cmd {
	upload := func() tea.Msg {
		err := m.client.UploadWithProgressCtx(ctx, job.localPath, job.remotePath, func(done, total int64) {
			m.progressDone = done
			m.progressTotal = total
		})
		if ctx.Err() != nil {
			return ftpUploadResultMsg{gen: gen, filename: job.filename, success: false, err: ctx.Err()}
		}
		return ftpUploadResultMsg{gen: gen, filename: job.filename, success: err == nil, err: err}
	}
	return tea.Batch(
		upload,
		tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
			return ftpProgressMsg{
				gen: gen, filename: job.filename,
				done: m.progressDone, total: m.progressTotal, isUpload: true,
			}
		}),
	)
}

func (m *ftpFormModel) mkdirCmd(name, remotePath string) tea.Cmd {
	return func() tea.Msg {
		err := m.client.MakeDir(remotePath)
		return ftpMkdirResultMsg{name: name, success: err == nil, err: err}
	}
}

func (m *ftpFormModel) deleteCmd(name, remotePath string) tea.Cmd {
	return func() tea.Msg {
		err := m.client.Delete(remotePath)
		return ftpDeleteResultMsg{filename: name, success: err == nil, err: err}
	}
}

func (m *ftpFormModel) renameCmd(oldName, oldPath, newPath string) tea.Cmd {
	return func() tea.Msg {
		err := m.client.Rename(oldPath, newPath)
		return ftpRenameResultMsg{oldName: oldName, newName: path.Base(newPath), success: err == nil, err: err}
	}
}

func (m *ftpFormModel) handleTransferResult(gen int, filename string, success bool, err error, isUpload bool) tea.Cmd {
	if !m.transferring || gen != m.progressGen {
		return nil
	}
	next, ok := m.queue.finishCurrent()
	if ok {
		return m.beginJob(next)
	}
	m.transferring = false
	m.loading = false
	m.transferCancel = nil
	if success {
		action := "Downloaded"
		if isUpload {
			action = "Uploaded"
		}
		m.setStatus(fmt.Sprintf("%s %s", action, filename))
		if isUpload {
			return m.loadDirCmd(m.cwd)
		}
		m.refreshLocal()
		return nil
	}
	if err != nil {
		if err == context.Canceled {
			m.setStatus(fmt.Sprintf("Cancelled: %s", filename))
		} else {
			m.setStatus("Error: " + err.Error())
		}
	}
	return nil
}

func (m *ftpFormModel) cancelTransfer() {
	if m.transferCancel != nil {
		m.transferCancel()
		m.transferCancel = nil
	}
	if m.client != nil {
		m.client.AbortTransfer()
	}
	// Keep transferring=true until the transfer result arrives so refresh/List
	// cannot race a still-finishing Read (SFTP-style wait-for-result cancel).
	m.queue.clear()
	m.setStatus(fmt.Sprintf("Cancelling: %s", m.progressFile))
}

func (m *ftpFormModel) setStatus(s string) tea.Cmd {
	m.statusMsg = s
	m.statusExpiry = time.Now().Add(4 * time.Second)
	m.statusGen++
	gen := m.statusGen
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg {
		return ftpStatusExpiredMsg{gen: gen}
	})
}

func (m *ftpFormModel) statusActive() bool {
	return m.statusMsg != "" && time.Now().Before(m.statusExpiry)
}

func (m *ftpFormModel) focusLabel(local bool) string {
	active := local == m.focusLocal
	name := "[REMOTE]"
	if local {
		name = "[LOCAL]"
	}
	if active {
		return "● " + name
	}
	return "  " + name
}

func (m *ftpFormModel) focusedEntryInfo() *ftpEntryInfo {
	if m.focusLocal {
		full := m.selectedLocal()
		if full == "" {
			return nil
		}
		info := &ftpEntryInfo{name: filepath.Base(full), path: full}
		if st, err := os.Stat(full); err == nil {
			info.isDir = st.IsDir()
			info.size = st.Size()
			info.modTime = st.ModTime()
		}
		return info
	}
	e := m.selectedRemote()
	if e == nil {
		return nil
	}
	return &ftpEntryInfo{
		name:    e.Name,
		isDir:   e.IsDir,
		size:    e.Size,
		modTime: e.ModTime,
		path:    path.Join(m.cwd, e.Name),
	}
}

func (m *ftpFormModel) renderInfoView() string {
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
	b.WriteString(m.styles.FormTitle.Render(" "+i18n.T("ftp.entry_info_title", info.name)+" ") + "\n\n")
	rows := [][2]string{
		{i18n.T("sftp.col_name"), info.name},
		{i18n.T("sftp.col_type"), kind},
		{i18n.T("sftp.col_size"), size},
		{i18n.T("sftp.col_modified"), mod},
		{i18n.T("ftp.info_path"), info.path},
	}
	for _, r := range rows {
		label := "  " + r[0] + ":"
		if w := ansi.StringWidth(label); w < 16 {
			label += strings.Repeat(" ", 16-w)
		}
		b.WriteString(m.styles.FormField.Render(label) + " " + r[1] + "\n")
	}
	b.WriteString("\n" + m.styles.HelpText.Render("  "+i18n.T("ftp.browser_info_help")))
	return renderFormPage(m.styles, m.width, b.String())
}

func (m *ftpFormModel) renderErrorView() string {
	inner := formPageInnerWidth(m.width)
	errStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("9")).
		Bold(true).
		Padding(1, 2).
		Width(inner)
	detail := m.loadError
	if detail == "" {
		detail = "(no details)"
	}
	content := i18n.T("ftp.err_session", detail)
	return renderFormPage(m.styles, m.width, errStyle.Render(content))
}

func (m *ftpFormModel) View() string {
	if m.loading && m.client == nil && m.mode != ftpPasswordInput {
		inner := formPageInnerWidth(m.width)
		body := lipgloss.NewStyle().Width(inner).Render(
			m.styles.FormTitle.Render(" FTP — "+m.siteName+" ") + "\n\n" +
				"  Connecting to " + m.site.Addr() + "…")
		return renderFormPage(m.styles, m.width, body)
	}
	if m.mode == ftpPasswordInput {
		inner := formPageInnerWidth(m.width)
		body := lipgloss.NewStyle().Width(inner).Render(
			m.styles.FormTitle.Render(" FTP — Password ") + "\n\n" +
				fmt.Sprintf("  %s\n", m.inputPrompt) +
				fmt.Sprintf("  %s_\n", strings.Repeat("*", len(m.inputBuffer))) +
				"\n" + m.styles.HelpText.Render("  "+i18n.T("ftp.help_password")) +
				"\n" + m.styles.HelpText.Render("  "+i18n.T("ftp.cred_hint")))
		return renderFormPage(m.styles, m.width, body)
	}
	if m.mode == ftpError {
		return m.renderErrorView()
	}
	if m.showInfo && m.entryInfo != nil {
		return m.renderInfoView()
	}

	localStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	remoteStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("36"))
	if m.focusLocal {
		localStyle = localStyle.Bold(true)
	} else {
		remoteStyle = remoteStyle.Bold(true)
	}

	header := m.styles.Header.Render(fmt.Sprintf(" FTP — %s (%s@%s) ", m.siteName, m.site.User, m.site.Addr()))

	var paths string
	var panes string
	pw := m.paneWidth()
	if m.singlePane() {
		// SFTP-style single focused pane on narrow terminals.
		tableStyle := m.styles.TableFocused
		if m.searchMode {
			tableStyle = m.styles.TableUnfocused
		}
		paths = localStyle.Render(fmt.Sprintf("%s  %s", m.focusLabel(true), truncatePath(m.localCwd, pw-4))) + "\n" +
			remoteStyle.Render(fmt.Sprintf("%s %s", m.focusLabel(false), truncatePath(m.cwd, pw-4)))
		if m.focusLocal {
			panes = tableStyle.Width(pw).Render(m.localTbl.View())
		} else {
			panes = tableStyle.Width(pw).Render(m.remoteTbl.View())
		}
	} else {
		paths = localStyle.Render(fmt.Sprintf("%s  %s", m.focusLabel(true), truncatePath(m.localCwd, pw-4))) + "\n" +
			remoteStyle.Render(fmt.Sprintf("%s %s", m.focusLabel(false), truncatePath(m.cwd, pw-4)))
		localBoxStyle := m.styles.TableUnfocused
		remoteBoxStyle := m.styles.TableUnfocused
		if !m.searchMode {
			if m.focusLocal {
				localBoxStyle = m.styles.TableFocused
			} else {
				remoteBoxStyle = m.styles.TableFocused
			}
		}
		localBox := localBoxStyle.Width(pw).Render(m.localTbl.View())
		remoteBox := remoteBoxStyle.Width(pw).Render(m.remoteTbl.View())
		panes = lipgloss.JoinHorizontal(lipgloss.Top, localBox, "  ", remoteBox)
	}

	var extras []string
	if m.statusActive() || m.transferring || m.refreshing {
		progressStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("36"))
		prefix := " "
		if m.transferring || m.refreshing {
			prefix = " ⏳ "
		}
		extras = append(extras, progressStyle.Render(prefix+m.statusMsg))
	}
	if m.mode == ftpDownloadConfirm && m.selected != nil {
		extras = append(extras, lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Render(
			i18n.T("ftp.download_confirm", m.selected.Name, filepath.Join(m.localCwd, m.selected.Name))))
	}
	if m.mode == ftpDeleteConfirm {
		name := ""
		if m.selected != nil {
			name = m.selected.Name
		} else if m.pendingLocalPath != "" {
			name = filepath.Base(m.pendingLocalPath)
		}
		if name != "" {
			extras = append(extras, lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(
				i18n.T("ftp.delete_confirm", name)))
		}
	}
	if m.mode == ftpMkdirInput || m.mode == ftpRenameInput {
		extras = append(extras, lipgloss.NewStyle().Foreground(lipgloss.Color(PrimaryColor)).Render(
			fmt.Sprintf("  %s %s_", m.inputPrompt, m.inputBuffer)))
	}

	var helpParts []string
	if m.height < 20 {
		if m.searchMode {
			helpParts = append(helpParts, i18n.T("ftp.help_search_1"))
		} else if m.focusLocal {
			helpParts = append(helpParts, i18n.T("ftp.help_local_1"))
		} else {
			helpParts = append(helpParts, i18n.T("ftp.help_remote_1"))
		}
	} else {
		if m.searchMode {
			helpParts = append(helpParts, i18n.T("ftp.help_search_1"), i18n.T("ftp.help_search_2"))
		} else if m.focusLocal {
			helpParts = append(helpParts, i18n.T("ftp.help_local_1"), i18n.T("ftp.help_local_2"), i18n.T("ftp.help_local_3"))
		} else {
			helpParts = append(helpParts, i18n.T("ftp.help_remote_1"), i18n.T("ftp.help_remote_2"), i18n.T("ftp.help_remote_3"))
		}
	}
	helpParts = dedupeStrings(helpParts)

	// Search bar sits above the panes like every other view (host list/serial/
	// telnet/FTP sites) so focus never jumps to the bottom of the screen.
	parts := []string{header, paths, renderSearchBar(m.styles, m.searchMode, i18n.T("search.prompt"), m.searchInput.View(), m.width)}
	parts = append(parts, panes)
	parts = append(parts, extras...)
	if help := strings.Join(helpParts, "\n"); help != "" {
		// Single renderHelpText so the footer block is not joined twice.
		parts = append(parts, renderHelpText(m.styles, help, m.width))
	}
	return m.styles.App.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}
