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

	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/sftpconfig"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// SFTP view modes (dual-pane, FTP-aligned).
type sftpMode int

const (
	sftpBrowse sftpMode = iota
	sftpLocalBrowse
	sftpDownloadConfirm
	sftpMkdirInput
	sftpRenameInput
	sftpDeleteConfirm
	sftpPasswordInput
	sftpError
)

// Narrow terminals show one focused pane instead of cramped dual columns.
const sftpNarrowWidth = 80

// sftpFormModel manages the dual-pane SFTP file browser.
type sftpFormModel struct {
	styles     Styles
	width      int
	height     int
	hostName   string
	configFile string
	layout     config.SFTPLayout

	client     *sftpconfig.SFTPClient
	remoteTbl  table.Model
	localTbl   table.Model
	entries    []sftpconfig.RemoteEntry
	cwd        string // current working directory on remote
	mode       sftpMode
	ready      bool
	loading    bool
	loadError  string
	password   string
	focusLocal bool

	// Download/upload progress (shared between goroutine and TUI)
	progressDone   int64
	progressTotal  int64
	progressFile   string
	progressGen    int
	transferring   bool
	transferCancel context.CancelFunc // cancels the in-flight upload/download goroutine
	queue          sftpTransferQueue

	// For input modes
	inputBuffer string
	inputPrompt string

	// Local-target management: when true, the active mkdir/rename input
	// or delete confirm operates on pendingLocalPath via os calls.
	localOp          bool
	pendingLocalPath string

	// Entry snapshot for the info overlay.
	showInfo  bool
	entryInfo *sftpEntryInfo

	// For confirm dialogs
	selectedEntry *sftpconfig.RemoteEntry

	// Status message
	statusMsg    string
	statusExpiry time.Time

	// Local pane state
	localFiles         []string
	localShowingDrives bool // Windows: browsing drive list above volume roots
	localCwd           string

	// Search
	searchInput textinput.Model
	searchMode  bool
}

// sftpEntryInfo is a snapshot of the file or directory shown in the info overlay.
type sftpEntryInfo struct {
	name    string
	isDir   bool
	size    int64
	modTime time.Time
	path    string
}

// Messages for async SFTP operations
type sftpConnectedMsg struct {
	client *sftpconfig.SFTPClient
	cwd    string
}

type sftpEntriesMsg struct {
	entries []sftpconfig.RemoteEntry
	cwd     string
	err     error
}

type sftpErrorMsg struct {
	err error
}

type sftpDownloadResultMsg struct {
	gen      int
	filename string
	success  bool
	err      error
}

type sftpProgressMsg struct {
	gen        int
	filename   string
	downloaded int64
	total      int64
	isUpload   bool
}

type sftpUploadResultMsg struct {
	gen      int
	filename string
	success  bool
	err      error
}

type sftpDeleteResultMsg struct {
	filename string
	success  bool
	err      error
}

type sftpMkdirResultMsg struct {
	success bool
	err     error
}

type sftpRenameResultMsg struct {
	oldName string
	newName string
	success bool
	err     error
}

// sftpDoneMsg tells the parent model to return to the host list.
type sftpDoneMsg struct{}

// NewSFTPForm creates the SFTP file browser.
func NewSFTPForm(styles Styles, width, height int, hostName, configFile string) *sftpFormModel {
	return NewSFTPFormWithLayout(styles, width, height, hostName, configFile, config.SFTPLayoutDual)
}

// NewSFTPFormWithLayout creates an SFTP browser using the configured pane layout.
func NewSFTPFormWithLayout(styles Styles, width, height int, hostName, configFile string, layout config.SFTPLayout) *sftpFormModel {
	m := &sftpFormModel{
		styles:     styles,
		width:      width,
		height:     height,
		hostName:   hostName,
		configFile: configFile,
		layout:     config.NormalizeSFTPLayout(layout),
		mode:       sftpBrowse,
		loading:    true,
		localCwd:   defaultLocalUploadDir(),
		focusLocal: false,
	}
	h := m.paneTableHeight()
	cols := m.remoteColumns()
	m.remoteTbl = table.New(table.WithColumns(cols), table.WithHeight(h), table.WithFocused(true))
	m.localTbl = table.New(table.WithColumns(m.localColumns()), table.WithHeight(h), table.WithFocused(false))
	m.searchInput = textinput.New()
	m.searchInput.Placeholder = i18n.T("sftp.search_placeholder")
	m.searchInput.CharLimit = 50
	m.searchInput.Width = searchInputWidth(m.width, i18n.T("search.prompt"))
	return m
}

// Init starts the async SSH connection
func (m *sftpFormModel) Init() tea.Cmd {
	return m.connectCmd()
}

// sftpPasswordPromptMsg tells the parent to switch to password input mode.
type sftpPasswordPromptMsg struct{}

func (m *sftpFormModel) connectCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := sftpconfig.ConnectWithPassword(m.hostName, m.configFile, "")
		if err != nil {
			if strings.Contains(err.Error(), "unable to authenticate") ||
				strings.Contains(err.Error(), "no supported methods") {
				return sftpPasswordPromptMsg{}
			}
			return sftpErrorMsg{err: err}
		}

		cwd, err := client.RealPath(".")
		if err != nil {
			cwd = "/"
		}

		return sftpConnectedMsg{client: client, cwd: cwd}
	}
}

func (m *sftpFormModel) loadDirCmd(path string) tea.Cmd {
	return func() tea.Msg {
		entries, err := m.client.ListDir(path)
		if err != nil {
			return sftpEntriesMsg{cwd: path, err: err}
		}
		return sftpEntriesMsg{entries: entries, cwd: path}
	}
}

func (m *sftpFormModel) requestTransfer(job sftpTransferJob) tea.Cmd {
	started, now := m.queue.startOrEnqueue(job)
	if !now {
		return nil
	}
	return m.beginJob(started)
}

func (m *sftpFormModel) beginJob(job sftpTransferJob) tea.Cmd {
	m.transferring = true
	m.loading = true
	m.progressFile = job.filename
	m.progressDone = 0
	m.progressTotal = 0
	cur, total := m.queue.position()
	m.statusMsg = formatSFTPProgress(job, 0, 0, cur, total)
	m.statusExpiry = time.Now().Add(10 * time.Second)
	if job.isUpload {
		return m.uploadCmd(job.localPath, job.remotePath)
	}
	return m.downloadCmd(job.remotePath, job.localPath)
}

func (m *sftpFormModel) handleTransferResult(gen int, filename string, success bool, err error, isUpload bool) tea.Cmd {
	if !m.transferring || gen != m.progressGen {
		return nil
	}
	next, ok := m.queue.finishCurrent()
	if ok {
		return m.beginJob(next)
	}
	m.transferring = false
	m.loading = false
	if success {
		if isUpload {
			m.setStatus("Uploaded: " + filename)
		} else {
			m.setStatus(fmt.Sprintf("Downloaded: %s → %s", filename, m.localDownloadPath(filename)))
		}
	} else if err != nil {
		m.setStatus("Failed: " + filename + ": " + err.Error())
	}
	return m.loadDirCmd(m.cwd)
}

func (m *sftpFormModel) cancelTransfer() {
	if m.transferCancel != nil {
		m.transferCancel()
		m.transferCancel = nil
	}
}

func (m *sftpFormModel) downloadCmd(remotePath, localPath string) tea.Cmd {
	filename := filepath.Base(remotePath)
	m.progressGen++
	gen := m.progressGen
	m.progressFile = filename
	m.progressDone = 0
	m.progressTotal = 0

	ctx, cancel := context.WithCancel(context.Background())
	m.transferCancel = cancel

	download := func() tea.Msg {
		err := m.client.DownloadWithProgressCtx(ctx, remotePath, localPath, func(downloaded, total int64) {
			m.progressDone = downloaded
			m.progressTotal = total
		})
		if ctx.Err() != nil {
			return sftpDownloadResultMsg{gen: gen, filename: filename, success: false, err: ctx.Err()}
		}
		return sftpDownloadResultMsg{gen: gen, filename: filename, success: err == nil, err: err}
	}

	return tea.Batch(
		download,
		tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
			return sftpProgressMsg{gen: gen, filename: filename, downloaded: m.progressDone, total: m.progressTotal, isUpload: false}
		}),
	)
}

func (m *sftpFormModel) uploadCmd(localPath, remotePath string) tea.Cmd {
	filename := filepath.Base(localPath)
	m.progressGen++
	gen := m.progressGen
	m.progressFile = filename
	m.progressDone = 0
	m.progressTotal = 0

	ctx, cancel := context.WithCancel(context.Background())
	m.transferCancel = cancel

	upload := func() tea.Msg {
		err := m.client.UploadWithProgressCtx(ctx, localPath, remotePath, func(uploaded, total int64) {
			m.progressDone = uploaded
			m.progressTotal = total
		})
		if ctx.Err() != nil {
			return sftpUploadResultMsg{gen: gen, filename: filename, success: false, err: ctx.Err()}
		}
		return sftpUploadResultMsg{gen: gen, filename: filename, success: err == nil, err: err}
	}

	return tea.Batch(
		upload,
		tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
			return sftpProgressMsg{gen: gen, filename: filename, downloaded: m.progressDone, total: m.progressTotal, isUpload: true}
		}),
	)
}

func (m *sftpFormModel) deleteCmd(entry sftpconfig.RemoteEntry, remotePath string) tea.Cmd {
	return func() tea.Msg {
		var err error
		if entry.IsDir {
			err = m.client.RemoveAll(remotePath)
		} else {
			err = m.client.Remove(remotePath)
		}
		return sftpDeleteResultMsg{filename: entry.Name, success: err == nil, err: err}
	}
}

func (m *sftpFormModel) mkdirCmd(remotePath string) tea.Cmd {
	return func() tea.Msg {
		err := m.client.Mkdir(remotePath)
		return sftpMkdirResultMsg{success: err == nil, err: err}
	}
}

func (m *sftpFormModel) renameCmd(oldName, oldPath, newPath string) tea.Cmd {
	return func() tea.Msg {
		err := m.client.Rename(oldPath, newPath)
		return sftpRenameResultMsg{oldName: oldName, newName: filepath.Base(newPath), success: err == nil, err: err}
	}
}

// Update handles SFTP view messages
func (m *sftpFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case sftpConnectedMsg:
		m.client = msg.client
		m.cwd = msg.cwd
		m.loading = false
		m.ready = true
		m.setStatus("Connected to " + m.hostName)
		m.refreshLocal()
		return m, m.loadDirCmd(m.cwd)

	case sftpPasswordPromptMsg:
		m.loading = false
		m.mode = sftpPasswordInput
		m.inputBuffer = ""
		m.inputPrompt = "Password for " + m.hostName + ":"
		return m, nil

	case sftpEntriesMsg:
		m.loading = false
		if msg.err != nil {
			if !m.ready {
				m.loadError = msg.err.Error()
				m.mode = sftpError
				return m, nil
			}
			m.setStatus("Refresh failed: " + msg.err.Error())
			return m, nil
		}
		m.entries = msg.entries
		m.cwd = msg.cwd
		m.sortEntries()
		m.updateRemoteRows()
		m.clampActiveCursor()
		return m, nil

	case sftpErrorMsg:
		m.loadError = msg.err.Error()
		m.mode = sftpError
		m.loading = false
		return m, nil

	case sftpProgressMsg:
		if staleSFTPProgress(m.transferring, m.progressGen, msg.gen) {
			return m, nil
		}
		job := m.queue.current()
		if job.filename == "" {
			job = sftpTransferJob{filename: msg.filename, isUpload: msg.isUpload}
		}
		cur, count := m.queue.position()
		m.statusMsg = formatSFTPProgress(job, msg.downloaded, msg.total, cur, count)
		m.statusExpiry = time.Now().Add(10 * time.Second)
		return m, tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
			return sftpProgressMsg{gen: msg.gen, filename: job.filename, downloaded: m.progressDone, total: m.progressTotal, isUpload: job.isUpload}
		})

	case sftpDownloadResultMsg:
		return m, m.handleTransferResult(msg.gen, msg.filename, msg.success, msg.err, false)

	case sftpUploadResultMsg:
		return m, m.handleTransferResult(msg.gen, msg.filename, msg.success, msg.err, true)

	case sftpDeleteResultMsg:
		m.loading = false
		m.selectedEntry = nil
		m.localOp = false
		m.pendingLocalPath = ""
		if !m.inInput() {
			m.mode = sftpBrowse
			m.setFocusLocal(false)
		}
		if msg.success {
			name := msg.filename
			if name == "" {
				name = "file"
			}
			m.setStatus(i18n.T("sftp.delete_success", name))
		} else if msg.err != nil {
			m.setStatus(i18n.T("sftp.delete_failed", msg.err.Error()))
		}
		return m, m.loadDirCmd(m.cwd)

	case sftpMkdirResultMsg:
		m.localOp = false
		if msg.success {
			m.setStatus("Directory created")
		} else if m.inInput() {
			m.setStatus(i18n.T("sftp.mkdir_failed", msg.err))
		} else {
			m.loadError = msg.err.Error()
			m.mode = sftpError
		}
		if !m.inInput() {
			m.mode = sftpBrowse
			m.setFocusLocal(false)
			m.inputBuffer = ""
		}
		return m, m.loadDirCmd(m.cwd)

	case sftpRenameResultMsg:
		m.selectedEntry = nil
		m.localOp = false
		m.pendingLocalPath = ""
		m.loading = false
		if !m.inInput() {
			m.mode = sftpBrowse
			m.setFocusLocal(false)
			m.inputBuffer = ""
		}
		if msg.success {
			m.setStatus(i18n.T("sftp.rename_success", msg.oldName, msg.newName))
			return m, m.loadDirCmd(m.cwd)
		}
		m.setStatus(i18n.T("sftp.rename_failed", msg.err))
		return m, m.loadDirCmd(m.cwd)

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
		// Handle error mode
		if m.mode == sftpError {
			if msg.String() == "esc" || msg.String() == "enter" || msg.String() == "q" || msg.String() == "ctrl+c" {
				return m, func() tea.Msg { return sftpDoneMsg{} }
			}
			return m, nil
		}

		// Handle mkdir input mode
		if m.mode == sftpMkdirInput {
			return m.handleMkdirInput(msg)
		}

		// Handle rename input mode
		if m.mode == sftpRenameInput {
			return m.handleRenameInput(msg)
		}

		// Info overlay sits above browse views.
		if m.showInfo {
			switch msg.String() {
			case "esc", "i", "enter", "q":
				m.showInfo = false
				m.entryInfo = nil
				return m, nil
			}
			return m, nil
		}

		// Handle password input mode
		if m.mode == sftpPasswordInput {
			return m.handlePasswordInput(msg)
		}
		// Handle confirm dialogs
		if m.mode == sftpDownloadConfirm || m.mode == sftpDeleteConfirm {
			return m.handleConfirmDialog(msg)
		}

		// Normal browse mode
		if m.searchMode {
			return m.handleSearchKeys(msg)
		}
		return m.handleBrowseKeys(msg)
	}

	// Default: update focused table
	if m.focusLocal {
		m.localTbl, cmd = m.localTbl.Update(msg)
	} else {
		m.remoteTbl, cmd = m.remoteTbl.Update(msg)
	}
	return m, cmd
}

func (m *sftpFormModel) handleBrowseKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	key := msg.String()

	switch key {
	case "esc", "q":
		if m.transferring || (m.loading && m.client != nil) {
			m.cancelTransfer()
			m.loading = false
			m.transferring = false
			m.progressGen++
			m.queue.clear()
			m.setStatus(fmt.Sprintf("Cancelled: %s", m.progressFile))
			return m, nil
		}
		if key == "esc" && m.focusLocal {
			m.setFocusLocal(false)
			return m, nil
		}
		return m, func() tea.Msg { return sftpDoneMsg{} }
	case "tab", "shift+tab":
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
		if m.focusLocal {
			return m.handleLocalOpenDir()
		}
		return m.handleRemoteOpenDir()
	case "enter":
		if m.focusLocal {
			return m.handleLocalEnter()
		}
		return m.handleRemoteEnter()
	case "d":
		if m.transferring {
			return m, m.busyStatus()
		}
		return m.startDeleteConfirm()
	case "n":
		if m.transferring {
			return m, m.busyStatus()
		}
		m.localOp = m.focusLocal
		m.mode = sftpMkdirInput
		m.inputBuffer = ""
		m.inputPrompt = i18n.T("sftp.mkdir_prompt")
		return m, nil
	case "R":
		if m.transferring {
			return m, m.busyStatus()
		}
		return m.startRenameInput()
	case "i":
		if info := m.focusedEntryInfo(); info != nil {
			m.entryInfo = info
			m.showInfo = true
		}
		return m, nil
	case "v", "V":
		return m.toggleLayout()
	case "r":
		if m.transferring {
			return m, m.busyStatus()
		}
		if m.focusLocal {
			m.refreshLocal()
			m.updateLocalRows()
			m.clampActiveCursor()
			m.setStatus(i18n.T("ftp.refreshed"))
			return m, nil
		}
		m.loading = true
		m.setStatus(i18n.T("ftp.refreshing"))
		return m, m.loadDirCmd(m.cwd)
	case "up", "down", "k", "j", "pgup", "pgdown", "home", "end", "g", "G":
		if m.focusLocal {
			m.localTbl, cmd = m.localTbl.Update(msg)
		} else {
			m.remoteTbl, cmd = m.remoteTbl.Update(msg)
		}
		return m, cmd
	}

	return m, nil
}

func (m *sftpFormModel) handleSearchKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (m *sftpFormModel) handleRemoteOpenDir() (tea.Model, tea.Cmd) {
	e := m.selectedRemote()
	if e == nil || !e.IsDir {
		return m, nil
	}
	next := path.Join(m.cwd, e.Name)
	m.loading = true
	m.statusMsg = "Loading " + e.Name + "..."
	return m, m.loadDirCmd(next)
}

func (m *sftpFormModel) handleRemoteEnter() (tea.Model, tea.Cmd) {
	e := m.selectedRemote()
	if e == nil {
		return m, nil
	}
	if e.IsDir {
		next := path.Join(m.cwd, e.Name)
		m.loading = true
		m.statusMsg = "Loading " + e.Name + "..."
		return m, m.loadDirCmd(next)
	}
	job := sftpTransferJob{
		filename:   e.Name,
		localPath:  m.localDownloadPath(e.Name),
		remotePath: path.Join(m.cwd, e.Name),
		isUpload:   false,
	}
	if m.transferring {
		return m, m.requestTransfer(job)
	}
	m.selectedEntry = e
	m.mode = sftpDownloadConfirm
	return m, nil
}

func (m *sftpFormModel) handleLocalOpenDir() (tea.Model, tea.Cmd) {
	files := m.filteredLocal()
	idx := m.localTbl.Cursor()
	if idx < 0 || idx >= len(files) {
		return m, nil
	}
	full := files[idx]
	if m.localShowingDrives {
		if m.transferring {
			return m, nil
		}
		m.localShowingDrives = false
		m.localCwd = full
		m.refreshLocal()
		m.updateLocalRows()
		m.localTbl.SetCursor(0)
		return m, nil
	}
	info, err := os.Stat(full)
	if err != nil || !info.IsDir() {
		return m, nil
	}
	m.localCwd = full
	m.refreshLocal()
	m.updateLocalRows()
	m.localTbl.SetCursor(0)
	return m, nil
}

func (m *sftpFormModel) handleLocalEnter() (tea.Model, tea.Cmd) {
	files := m.filteredLocal()
	idx := m.localTbl.Cursor()
	if idx < 0 || idx >= len(files) {
		return m, nil
	}
	full := files[idx]
	if m.localShowingDrives {
		if m.transferring {
			return m, nil
		}
		m.localShowingDrives = false
		m.localCwd = full
		m.refreshLocal()
		m.updateLocalRows()
		m.localTbl.SetCursor(0)
		return m, nil
	}
	info, err := os.Stat(full)
	if err != nil {
		return m, nil
	}
	if info.IsDir() {
		if m.transferring {
			return m, nil
		}
		m.localCwd = full
		m.refreshLocal()
		m.updateLocalRows()
		m.localTbl.SetCursor(0)
		return m, nil
	}
	remotePath := path.Join(m.cwd, filepath.Base(full))
	return m, m.requestTransfer(sftpTransferJob{
		filename:   filepath.Base(full),
		localPath:  full,
		remotePath: remotePath,
		isUpload:   true,
	})
}

func (m *sftpFormModel) startDeleteConfirm() (tea.Model, tea.Cmd) {
	m.localOp = false
	m.pendingLocalPath = ""
	m.selectedEntry = nil
	if m.focusLocal {
		full := m.selectedLocal()
		if full == "" || m.localShowingDrives {
			return m, nil
		}
		m.localOp = true
		m.pendingLocalPath = full
		m.mode = sftpDeleteConfirm
		return m, nil
	}
	e := m.selectedRemote()
	if e == nil {
		return m, nil
	}
	m.selectedEntry = e
	m.mode = sftpDeleteConfirm
	return m, nil
}

func (m *sftpFormModel) startRenameInput() (tea.Model, tea.Cmd) {
	m.localOp = false
	m.pendingLocalPath = ""
	m.selectedEntry = nil
	if m.focusLocal {
		full := m.selectedLocal()
		if full == "" || m.localShowingDrives {
			return m, nil
		}
		m.localOp = true
		m.pendingLocalPath = full
		m.mode = sftpRenameInput
		m.inputBuffer = filepath.Base(full)
		m.inputPrompt = i18n.T("sftp.rename_prompt")
		return m, nil
	}
	e := m.selectedRemote()
	if e == nil {
		return m, nil
	}
	m.selectedEntry = e
	m.mode = sftpRenameInput
	m.inputBuffer = e.Name
	m.inputPrompt = i18n.T("sftp.rename_prompt")
	return m, nil
}

func (m *sftpFormModel) remoteUp() tea.Cmd {
	if m.cwd == "/" || m.cwd == "" {
		return nil
	}
	parent := path.Dir(m.cwd)
	if parent == m.cwd {
		return nil
	}
	m.loading = true
	m.statusMsg = "Loading " + parent + "..."
	return m.loadDirCmd(parent)
}

func (m *sftpFormModel) localUp() tea.Cmd {
	if isLocalFilesystemRoot(m.localCwd) {
		if drives := listLocalDrives(); len(drives) > 0 {
			m.localShowingDrives = true
			m.localFiles = drives
			m.updateLocalRows()
			m.localTbl.SetCursor(0)
		}
		return nil
	}
	if m.localShowingDrives {
		return nil
	}
	parent := filepath.Dir(m.localCwd)
	if parent == m.localCwd || parent == "." {
		return nil
	}
	m.localCwd = parent
	m.refreshLocal()
	m.updateLocalRows()
	m.localTbl.SetCursor(0)
	return nil
}

func (m *sftpFormModel) selectedRemote() *sftpconfig.RemoteEntry {
	rows := m.filteredRemote()
	idx := m.remoteTbl.Cursor()
	if idx < 0 || idx >= len(rows) {
		return nil
	}
	return &rows[idx]
}

func (m *sftpFormModel) selectedLocal() string {
	files := m.filteredLocal()
	idx := m.localTbl.Cursor()
	if idx < 0 || idx >= len(files) {
		return ""
	}
	return files[idx]
}

func (m *sftpFormModel) filteredRemote() []sftpconfig.RemoteEntry {
	q := strings.ToLower(strings.TrimSpace(m.searchInput.Value()))
	if q == "" || m.focusLocal {
		return m.entries
	}
	words := strings.Fields(q)
	var out []sftpconfig.RemoteEntry
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

func (m *sftpFormModel) filteredLocal() []string {
	if m.localShowingDrives {
		return m.localFiles
	}
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

func (m *sftpFormModel) sortEntries() {
	sort.Slice(m.entries, func(i, j int) bool {
		if m.entries[i].IsDir != m.entries[j].IsDir {
			return m.entries[i].IsDir
		}
		return m.entries[i].Name < m.entries[j].Name
	})
}

func (m *sftpFormModel) refreshLocal() {
	m.localFiles = nil
	if m.localShowingDrives {
		m.localFiles = listLocalDrives()
		return
	}
	if m.localCwd == "" {
		m.localCwd = defaultLocalUploadDir()
	}
	entries, err := os.ReadDir(m.localCwd)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		m.localFiles = append(m.localFiles, filepath.Join(m.localCwd, entry.Name()))
	}
	sort.Slice(m.localFiles, func(i, j int) bool {
		ii, ei := os.Stat(m.localFiles[i])
		ij, ej := os.Stat(m.localFiles[j])
		if ei != nil || ej != nil {
			return m.localFiles[i] < m.localFiles[j]
		}
		if ii.IsDir() != ij.IsDir() {
			return ii.IsDir()
		}
		return filepath.Base(m.localFiles[i]) < filepath.Base(m.localFiles[j])
	})
	m.updateLocalRows()
}

// listLocalFiles returns all visible entries (files and dirs) in localCwd.
func (m *sftpFormModel) listLocalFiles() []string {
	if m.localShowingDrives {
		return listLocalDrives()
	}
	if m.localCwd == "" {
		m.localCwd = defaultLocalUploadDir()
	}
	entries, err := os.ReadDir(m.localCwd)
	if err != nil {
		return nil
	}
	var files []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		files = append(files, filepath.Join(m.localCwd, entry.Name()))
	}
	sort.Strings(files)
	return files
}

func (m *sftpFormModel) updateRemoteRows() {
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
		kind := i18n.T("sftp.type_file")
		if e.IsDir {
			kind = i18n.T("sftp.type_dir")
		}
		rows = append(rows, table.Row{name, kind, formatSize(e.Size)})
	}
	m.remoteTbl.SetRows(rows)
}

func (m *sftpFormModel) updateLocalRows() {
	cols := m.localColumns()
	m.localTbl.SetColumns(cols)
	var rows []table.Row
	for _, f := range m.filteredLocal() {
		name := filepath.Base(f)
		if m.localShowingDrives || name == "" || name == string(filepath.Separator) || name == "\\" {
			name = f
		}
		kind := i18n.T("sftp.type_file")
		sz := ""
		if !m.localShowingDrives {
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
		emptyRow := make(table.Row, len(cols))
		emptyRow[0] = i18n.T("sftp.empty_dir")
		rows = append(rows, emptyRow)
	}
	m.localTbl.SetRows(rows)
}

// findEntry finds an entry by name (stripping the emoji prefix)
func (m *sftpFormModel) findEntry(name string) *sftpconfig.RemoteEntry {
	cleanName := strings.TrimPrefix(name, "📁 ")
	cleanName = strings.TrimPrefix(cleanName, "📄 ")

	for i := range m.entries {
		if m.entries[i].Name == cleanName {
			return &m.entries[i]
		}
	}
	return nil
}

// inInput reports whether the user is typing in mkdir/rename input:
// late results must not clobber the open input session.
func (m *sftpFormModel) inInput() bool {
	return m.mode == sftpMkdirInput || m.mode == sftpRenameInput
}

func (m *sftpFormModel) setFocusLocal(local bool) {
	m.focusLocal = local
	if local {
		m.mode = sftpLocalBrowse
		m.localTbl.Focus()
		m.remoteTbl.Blur()
	} else {
		m.mode = sftpBrowse
		m.remoteTbl.Focus()
		m.localTbl.Blur()
	}
	m.updateRemoteRows()
	m.updateLocalRows()
	m.clampActiveCursor()
}

func (m *sftpFormModel) enterSearch() tea.Cmd {
	m.searchMode = true
	m.searchInput.Focus()
	m.remoteTbl.Blur()
	m.localTbl.Blur()
	return textinput.Blink
}

func (m *sftpFormModel) exitSearch() {
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

func (m *sftpFormModel) clampTableCursor(tbl *table.Model, count int) {
	if count == 0 || (tbl.Cursor() >= 0 && tbl.Cursor() < count) {
		return
	}
	tbl.SetCursor(0)
}

func (m *sftpFormModel) clampActiveCursor() {
	if m.focusLocal {
		m.clampTableCursor(&m.localTbl, len(m.filteredLocal()))
	} else {
		m.clampTableCursor(&m.remoteTbl, len(m.filteredRemote()))
	}
}

func (m *sftpFormModel) focusedEntryInfo() *sftpEntryInfo {
	if m.focusLocal {
		full := m.selectedLocal()
		if full == "" || m.localShowingDrives {
			return nil
		}
		info := &sftpEntryInfo{name: filepath.Base(full), path: full}
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
	return &sftpEntryInfo{
		name:    e.Name,
		isDir:   e.IsDir,
		size:    e.Size,
		modTime: e.ModTime,
		path:    path.Join(m.cwd, e.Name),
	}
}

func (m *sftpFormModel) paneTableHeight() int {
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

func (m *sftpFormModel) narrow() bool {
	return m.width > 0 && m.width < sftpNarrowWidth
}

func (m *sftpFormModel) singlePane() bool {
	return m.layout == config.SFTPLayoutSingle || m.narrow()
}

func (m *sftpFormModel) paneWidth() int {
	if m.singlePane() {
		w := m.width - 4
		if w < 20 {
			w = 20
		}
		return w
	}
	w := (m.width - 8) / 2
	if w < 20 {
		w = 20
	}
	return w
}

func (m *sftpFormModel) paneContentWidth() int {
	w := m.paneWidth() - 6
	if w < 12 {
		return 12
	}
	return w
}

func (m *sftpFormModel) remoteColumns() []table.Column {
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

func (m *sftpFormModel) localColumns() []table.Column {
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

// toggleLayout flips dual/single pane layout and persists it to app config.
func (m *sftpFormModel) toggleLayout() (tea.Model, tea.Cmd) {
	if m.layout == config.SFTPLayoutSingle {
		m.layout = config.SFTPLayoutDual
	} else {
		m.layout = config.SFTPLayoutSingle
	}
	m.updateRemoteRows()
	m.updateLocalRows()
	m.clampActiveCursor()
	persistSFTPLayout(m.layout)
	name := i18n.T("sftp.layout_dual")
	if m.layout == config.SFTPLayoutSingle {
		name = i18n.T("sftp.layout_single")
	}
	m.setStatus(i18n.T("sftp.layout_status", name))
	return m, nil
}

// persistSFTPLayout writes the chosen layout to ~/.config/ctty/config.json.
// Failures are silent — the in-memory layout still applies for this session.
func persistSFTPLayout(layout config.SFTPLayout) {
	cfg, err := config.LoadAppConfig()
	if err != nil || cfg == nil {
		fallback := config.GetDefaultAppConfig()
		cfg = &fallback
	}
	cfg.SFTPLayout = config.NormalizeSFTPLayout(layout)
	_ = config.SaveAppConfig(cfg)
}

func (m *sftpFormModel) focusLabel(local bool) string {
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

func (m *sftpFormModel) handleMkdirInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		m.inputBuffer = ""
		m.mode = sftpBrowse
		if m.localOp {
			m.mode = sftpLocalBrowse
		}
		m.localOp = false
		return m, nil

	case "enter":
		dirName := strings.TrimSpace(m.inputBuffer)
		m.inputBuffer = ""
		upload := m.localOp
		m.localOp = false
		if dirName == "" {
			m.mode = sftpBrowse
			if upload {
				m.mode = sftpLocalBrowse
			}
			return m, nil
		}
		if upload {
			m.mode = sftpLocalBrowse
			if err := os.Mkdir(filepath.Join(m.localCwd, dirName), 0755); err != nil {
				m.setStatus(i18n.T("sftp.mkdir_failed", err))
			} else {
				m.setStatus(i18n.T("sftp.mkdir_success", dirName))
			}
			m.refreshLocal()
			m.updateLocalRows()
			m.clampActiveCursor()
			return m, nil
		}
		m.mode = sftpBrowse
		return m, m.mkdirCmd(path.Join(m.cwd, dirName))

	case "backspace":
		if len(m.inputBuffer) > 0 {
			m.inputBuffer = m.inputBuffer[:len(m.inputBuffer)-1]
		}
		return m, nil

	default:
		// Only accept printable characters
		if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
			m.inputBuffer += key
		}
		return m, nil
	}
}

func (m *sftpFormModel) handleRenameInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.inputBuffer = ""
		m.mode = sftpBrowse
		if m.localOp {
			m.mode = sftpLocalBrowse
		}
		m.localOp = false
		m.pendingLocalPath = ""
		m.selectedEntry = nil
		return m, nil

	case "enter":
		newName := strings.TrimSpace(m.inputBuffer)
		m.inputBuffer = ""
		upload := m.localOp
		m.localOp = false
		oldLocal := m.pendingLocalPath
		m.pendingLocalPath = ""
		if upload {
			m.mode = sftpLocalBrowse
			if newName == "" || oldLocal == "" || newName == filepath.Base(oldLocal) {
				return m, nil
			}
			if err := os.Rename(oldLocal, filepath.Join(filepath.Dir(oldLocal), newName)); err != nil {
				m.setStatus(i18n.T("sftp.rename_failed", err))
			} else {
				m.setStatus(i18n.T("sftp.rename_success", filepath.Base(oldLocal), newName))
			}
			m.refreshLocal()
			m.updateLocalRows()
			m.clampActiveCursor()
			return m, nil
		}
		m.mode = sftpBrowse
		if newName == "" || m.selectedEntry == nil || newName == m.selectedEntry.Name {
			m.selectedEntry = nil
			return m, nil
		}
		oldName := m.selectedEntry.Name
		m.selectedEntry = nil
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

func (m *sftpFormModel) handlePasswordInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		return m, func() tea.Msg { return sftpDoneMsg{} }

	case "enter":
		password := m.inputBuffer
		m.password = password
		m.inputBuffer = ""
		m.loading = true
		m.mode = sftpBrowse
		return m, func() tea.Msg {
			client, err := sftpconfig.ConnectWithPassword(m.hostName, m.configFile, password)
			if err != nil {
				return sftpErrorMsg{err: err}
			}
			cwd, err := client.RealPath(".")
			if err != nil {
				cwd = "/"
			}
			return sftpConnectedMsg{client: client, cwd: cwd}
		}

	case "backspace":
		if len(m.inputBuffer) > 0 {
			m.inputBuffer = m.inputBuffer[:len(m.inputBuffer)-1]
		}
		return m, nil

	default:
		if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
			m.inputBuffer += key
		}
		return m, nil
	}
}

func (m *sftpFormModel) handleConfirmDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		upload := m.localOp
		m.selectedEntry = nil
		m.localOp = false
		m.pendingLocalPath = ""
		m.mode = sftpBrowse
		m.setFocusLocal(false)
		if upload {
			m.mode = sftpLocalBrowse
			m.setFocusLocal(true)
		}
		return m, nil

	case "enter", "y":
		if m.mode == sftpDeleteConfirm && m.localOp {
			m.localOp = false
			target := m.pendingLocalPath
			m.pendingLocalPath = ""
			m.selectedEntry = nil
			m.mode = sftpLocalBrowse
			m.setFocusLocal(true)
			if target == "" {
				return m, nil
			}
			if err := os.RemoveAll(target); err != nil {
				m.setStatus(i18n.T("sftp.delete_failed", err.Error()))
			} else {
				m.setStatus(i18n.T("sftp.delete_success", filepath.Base(target)))
			}
			m.refreshLocal()
			m.updateLocalRows()
			m.clampActiveCursor()
			return m, nil
		}
		if m.selectedEntry == nil {
			m.mode = sftpBrowse
			m.setFocusLocal(false)
			m.localOp = false
			m.pendingLocalPath = ""
			return m, nil
		}

		remotePath := path.Join(m.cwd, m.selectedEntry.Name)

		if m.mode == sftpDownloadConfirm {
			m.mode = sftpBrowse
			m.setFocusLocal(false)
			job := sftpTransferJob{
				filename:   m.selectedEntry.Name,
				localPath:  m.localDownloadPath(m.selectedEntry.Name),
				remotePath: remotePath,
				isUpload:   false,
			}
			m.selectedEntry = nil
			return m, m.requestTransfer(job)
		}

		if m.mode == sftpDeleteConfirm {
			entry := *m.selectedEntry
			m.mode = sftpBrowse
			m.setFocusLocal(false)
			m.selectedEntry = nil
			m.loading = true
			m.statusMsg = i18n.T("sftp.deleting", entry.Name)
			m.statusExpiry = time.Now().Add(10 * time.Second)
			return m, m.deleteCmd(entry, remotePath)
		}

		return m, nil

	case "n":
		upload := m.localOp
		m.selectedEntry = nil
		m.localOp = false
		m.pendingLocalPath = ""
		m.mode = sftpBrowse
		m.setFocusLocal(false)
		if upload {
			m.mode = sftpLocalBrowse
			m.setFocusLocal(true)
		}
		return m, nil
	}

	return m, nil
}

// View renders the SFTP browser
func (m *sftpFormModel) View() string {
	if m.loading && m.client == nil {
		body := m.styles.FormTitle.Render(" "+i18n.T("sftp.title_remote", m.hostName)+" ") + "\n\n" +
			"  " + i18n.T("sftp.connecting", m.hostName)
		return renderFormPage(m.styles, m.width, body)
	}

	if m.mode == sftpPasswordInput {
		body := m.styles.FormTitle.Render(" SFTP - Password ") + "\n\n" +
			fmt.Sprintf("  %s\n", m.inputPrompt) +
			fmt.Sprintf("  %s_\n", strings.Repeat("*", len(m.inputBuffer))) +
			"\n" +
			m.styles.HelpText.Render("  Enter: connect • Esc: cancel")
		return renderFormPage(m.styles, m.width, body)
	}

	if m.mode == sftpError {
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

	header := m.styles.Header.Render(i18n.T("sftp.title_remote", m.hostName))

	var paths string
	var panes string
	pw := m.paneWidth()
	if m.singlePane() {
		tableStyle := m.styles.TableFocused
		if m.searchMode {
			tableStyle = m.styles.TableUnfocused
		}
		paths = localStyle.Render(fmt.Sprintf("%s  %s", m.focusLabel(true), truncatePath(localBrowseLabel(m.localCwd, m.localShowingDrives), pw-4))) + "\n" +
			remoteStyle.Render(fmt.Sprintf("%s %s", m.focusLabel(false), truncatePath(m.cwd, pw-4)))
		if m.focusLocal {
			panes = tableStyle.Width(pw).Render(m.localTbl.View())
		} else {
			panes = tableStyle.Width(pw).Render(m.remoteTbl.View())
		}
	} else {
		paths = localStyle.Render(fmt.Sprintf("%s  %s", m.focusLabel(true), truncatePath(localBrowseLabel(m.localCwd, m.localShowingDrives), pw-4))) + "\n" +
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
	if m.loading && m.client != nil {
		progressStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("36"))
		extras = append(extras, progressStyle.Render(fmt.Sprintf("  ⏳ %s...", m.statusMsg)))
	} else if m.statusActive() {
		statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("36"))
		extras = append(extras, statusStyle.Render(" ✓ "+m.statusMsg))
	}
	if m.mode == sftpMkdirInput || m.mode == sftpRenameInput {
		extras = append(extras, m.renderInputLine())
	}

	if m.mode == sftpDownloadConfirm {
		extras = append(extras, m.renderDownloadConfirm())
	} else if m.mode == sftpDeleteConfirm {
		extras = append(extras, m.renderDeleteConfirm())
	}

	var helpParts []string
	if m.height < 20 {
		if m.searchMode {
			helpParts = append(helpParts, i18n.T("sftp.help_search_1"))
		} else if m.focusLocal {
			helpParts = append(helpParts, i18n.T("sftp.help_local_1"))
		} else {
			helpParts = append(helpParts, i18n.T("sftp.help_remote_1"))
		}
	} else {
		if m.searchMode {
			helpParts = append(helpParts, i18n.T("sftp.help_search_1"), i18n.T("sftp.help_search_2"))
		} else if m.focusLocal {
			helpParts = append(helpParts, i18n.T("sftp.help_local_1"), i18n.T("sftp.help_local_2"), i18n.T("sftp.help_local_3"))
		} else {
			helpParts = append(helpParts, i18n.T("sftp.help_remote_1"), i18n.T("sftp.help_remote_2"), i18n.T("sftp.help_remote_3"))
		}
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

func (m *sftpFormModel) renderErrorView() string {
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
	content := i18n.T("sftp.err_session", detail)
	return renderFormPage(m.styles, m.width, errStyle.Render(content))
}

func (m *sftpFormModel) renderInfoView() string {
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

func (m *sftpFormModel) renderInputLine() string {
	inputStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(PrimaryColor))
	return inputStyle.Render(fmt.Sprintf("  %s %s_", m.inputPrompt, m.inputBuffer))
}

func (m *sftpFormModel) renderDownloadConfirm() string {
	if m.selectedEntry == nil {
		return ""
	}
	msg := i18n.T("sftp.download_confirm", m.selectedEntry.Name, m.localDownloadPath(m.selectedEntry.Name))
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("229"))
	return style.Render(msg)
}

func (m *sftpFormModel) renderDeleteConfirm() string {
	name, isDir := "", false
	if m.selectedEntry != nil {
		name, isDir = m.selectedEntry.Name, m.selectedEntry.IsDir
	} else if m.pendingLocalPath != "" {
		name = filepath.Base(m.pendingLocalPath)
		if st, err := os.Stat(m.pendingLocalPath); err == nil {
			isDir = st.IsDir()
		}
	}
	if name == "" {
		return ""
	}
	confirmKey := "sftp.delete_confirm"
	if isDir {
		confirmKey = "sftp.delete_dir_confirm"
	}
	msg := i18n.T(confirmKey, name)
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	return style.Render(msg)
}

// localDownloadPath returns the local path for a downloaded file
func (m *sftpFormModel) localDownloadPath(filename string) string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, "Downloads", filename)
}

func (m *sftpFormModel) busyStatus() tea.Cmd {
	m.setStatus(i18n.T("ftp.busy"))
	return nil
}

// setStatus sets a status message that expires after 3 seconds
func (m *sftpFormModel) setStatus(msg string) {
	m.statusMsg = msg
	m.statusExpiry = time.Now().Add(3 * time.Second)
}

func (m *sftpFormModel) statusActive() bool {
	return m.statusMsg != "" && time.Now().Before(m.statusExpiry)
}

// formatSize formats a file size in human-readable form
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < 0 {
		return "-"
	}
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	if exp >= len("KMGTPE") {
		exp = len("KMGTPE") - 1
	}
	return fmt.Sprintf("%.1f%cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func truncatePath(path string, maxLen int) string {
	if maxLen <= 5 {
		return path
	}
	if len(path) <= maxLen {
		return path
	}
	return "..." + path[len(path)-maxLen+3:]
}
