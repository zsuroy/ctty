package ui

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/sftpconfig"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SFTP view modes
type sftpMode int

const (
	sftpBrowse sftpMode = iota
	sftpDownloadConfirm
	sftpUploadSelect
	sftpDeleteConfirm
	sftpMkdirInput
	sftpPasswordInput
	sftpError
)

// sftpFormModel manages the SFTP file browser.
type sftpFormModel struct {
	styles     Styles
	width      int
	height     int
	hostName   string
	configFile string

	client    *sftpconfig.SFTPClient
	table     table.Model
	entries   []sftpconfig.RemoteEntry
	cwd       string // current working directory on remote
	mode      sftpMode
	ready     bool
	loading   bool
	loadError string
	password  string

	// Download/upload progress (shared between goroutine and TUI)
	progressDone  int64
	progressTotal int64
	progressFile  string
	progressGen   int
	transferring  bool
	queue         sftpTransferQueue

	// For input modes
	inputBuffer string
	inputPrompt string

	// For confirm dialogs
	selectedEntry *sftpconfig.RemoteEntry

	// Status message
	statusMsg    string
	statusExpiry time.Time

	// Local file selector for upload
	localFiles []string

	// Search
	searchInput     textinput.Model
	searchMode      bool
	filteredEntries []sftpconfig.RemoteEntry
	localCwd        string
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
	success bool
	err     error
}

type sftpMkdirResultMsg struct {
	success bool
	err     error
}

// sftpDoneMsg tells the parent model to return to the host list.
type sftpDoneMsg struct{}

// NewSFTPForm creates the SFTP file browser.
func NewSFTPForm(styles Styles, width, height int, hostName, configFile string) *sftpFormModel {
	m := &sftpFormModel{
		styles:     styles,
		width:      width,
		height:     height,
		hostName:   hostName,
		configFile: configFile,
		mode:       sftpBrowse,
		loading:    true,
	}

	initHeight := m.calculateTableHeight()

	cols := m.getColumns(false)
	// Initialize table
	m.table = table.New(
		table.WithColumns(cols),
		table.WithHeight(initHeight),
		table.WithFocused(true),
	)

	// Initialize search input
	m.searchInput = textinput.New()
	m.searchInput.Placeholder = i18n.T("sftp.search_placeholder")
	m.searchInput.CharLimit = 50
	m.searchInput.Width = searchInputWidth(m.width, i18n.T("search.prompt"))
	m.filteredEntries = m.entries

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

func (m *sftpFormModel) downloadCmd(remotePath, localPath string) tea.Cmd {
	filename := filepath.Base(remotePath)
	m.progressGen++
	gen := m.progressGen
	m.progressFile = filename
	m.progressDone = 0
	m.progressTotal = 0

	download := func() tea.Msg {
		err := m.client.DownloadWithProgress(remotePath, localPath, func(downloaded, total int64) {
			m.progressDone = downloaded
			m.progressTotal = total
		})
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

	upload := func() tea.Msg {
		err := m.client.UploadWithProgress(localPath, remotePath, func(uploaded, total int64) {
			m.progressDone = uploaded
			m.progressTotal = total
		})
		return sftpUploadResultMsg{gen: gen, filename: filename, success: err == nil, err: err}
	}

	return tea.Batch(
		upload,
		tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
			return sftpProgressMsg{gen: gen, filename: filename, downloaded: m.progressDone, total: m.progressTotal, isUpload: true}
		}),
	)
}

func (m *sftpFormModel) deleteCmd(remotePath string) tea.Cmd {
	return func() tea.Msg {
		err := m.client.Remove(remotePath)
		return sftpDeleteResultMsg{success: err == nil, err: err}
	}
}

func (m *sftpFormModel) mkdirCmd(remotePath string) tea.Cmd {
	return func() tea.Msg {
		err := m.client.Mkdir(remotePath)
		return sftpMkdirResultMsg{success: err == nil, err: err}
	}
}

func (m *sftpFormModel) calculateTableHeight() int {
	overhead := 13
	if m.height < 20 {
		overhead = 9
	}
	h := m.height - overhead
	if h < 2 {
		h = 2
	}
	return h
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
		if m.mode != sftpUploadSelect {
			m.updateTableRows()
		}
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
		if msg.success {
			m.setStatus("Deleted successfully")
		} else {
			m.loadError = msg.err.Error()
			m.mode = sftpError
		}
		return m, m.loadDirCmd(m.cwd)

	case sftpMkdirResultMsg:
		if msg.success {
			m.setStatus("Directory created")
		} else {
			m.loadError = msg.err.Error()
			m.mode = sftpError
		}
		m.mode = sftpBrowse
		m.inputBuffer = ""
		return m, m.loadDirCmd(m.cwd)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.searchInput.Width = searchInputWidth(m.width, i18n.T("search.prompt"))
		tableHeight := m.calculateTableHeight()
		m.table.SetHeight(tableHeight)
		if m.mode == sftpUploadSelect {
			m.updateLocalTableRows()
		} else {
			m.updateTableRows()
		}
		return m, nil

	case tea.KeyMsg:
		// Handle error mode
		if m.mode == sftpError {
			if msg.String() == "esc" || msg.String() == "enter" || msg.String() == "q" || msg.String() == "ctrl+c" {
				return m, func() tea.Msg { return sftpDoneMsg{} }
			}
			return m, nil
		}

		// Handle input modes (mkdir)
		if m.mode == sftpMkdirInput {
			return m.handleMkdirInput(msg)
		}

		// Handle password input mode
		if m.mode == sftpPasswordInput {
			return m.handlePasswordInput(msg)
		}
		// Handle confirm dialogs
		if m.mode == sftpDownloadConfirm || m.mode == sftpDeleteConfirm {
			return m.handleConfirmDialog(msg)
		}

		// Handle upload file selection
		if m.mode == sftpUploadSelect {
			return m.handleUploadSelect(msg)
		}

		// Normal browse mode
		return m.handleBrowseKeys(msg)
	}

	// Default: update table
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *sftpFormModel) handleBrowseKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle search mode
	if m.searchMode {
		return m.handleSearchKeys(msg)
	}

	var cmd tea.Cmd
	key := msg.String()

	switch key {
	case "esc", "q":
		if m.transferring || (m.loading && m.client != nil) {
			m.loading = false
			m.transferring = false
			m.progressGen++
			m.queue.clear()
			m.setStatus(fmt.Sprintf("Cancelled: %s", m.progressFile))
			if m.mode != sftpUploadSelect {
				m.mode = sftpBrowse
				m.updateTableRows()
			}
			return m, nil
		}
		return m, func() tea.Msg { return sftpDoneMsg{} }
	case "/", "ctrl+f":
		m.searchMode = true
		m.searchInput.Focus()
		m.table.Blur()
		return m, textinput.Blink

	case "enter":
		// Enter: download file only (not directories)
		selected := m.table.SelectedRow()
		if len(selected) == 0 {
			return m, nil
		}
		name := selected[0]
		entry := m.findEntry(name)
		if entry == nil || entry.IsDir {
			return m, nil
		}
		job := sftpTransferJob{
			filename:   entry.Name,
			localPath:  m.localDownloadPath(entry.Name),
			remotePath: path.Join(m.cwd, entry.Name),
			isUpload:   false,
		}
		if m.transferring {
			return m, m.requestTransfer(job)
		}
		m.selectedEntry = entry
		m.mode = sftpDownloadConfirm
		return m, nil

	case "right", "l":
		if m.transferring {
			return m, nil
		}
		// Right/l: open directory
		selected := m.table.SelectedRow()
		if len(selected) == 0 {
			return m, nil
		}
		name := selected[0]
		entry := m.findEntry(name)
		if entry == nil || !entry.IsDir {
			return m, nil
		}
		newPath := path.Join(m.cwd, entry.Name)
		m.loading = true
		m.statusMsg = "Loading " + entry.Name + "..."
		return m, m.loadDirCmd(newPath)

	case "left", "h", "backspace":
		if m.transferring {
			return m, nil
		}
		// Left/h/backspace: go to parent directory
		parent := path.Dir(m.cwd)
		if parent == m.cwd {
			return m, nil
		}
		m.loading = true
		m.statusMsg = "Loading " + parent + "..."
		return m, m.loadDirCmd(parent)

	case "u", "tab", "shift+tab":
		if m.transferring {
			return m, nil
		}
		// Upload: switch table to show local files
		m.mode = sftpUploadSelect
		if m.localCwd == "" {
			m.localCwd, _ = os.UserHomeDir()
		}
		m.localFiles = m.listLocalFiles()
		m.updateLocalTableRows()
		m.table.SetCursor(0)
		return m, nil

	case "d":
		if m.transferring {
			return m, nil
		}
		// Delete selected file
		selected := m.table.SelectedRow()
		if len(selected) == 0 {
			return m, nil
		}
		name := selected[0]
		entry := m.findEntry(name)
		if entry == nil || entry.IsDir {
			return m, nil
		}
		m.selectedEntry = entry
		m.mode = sftpDeleteConfirm
		return m, nil

	case "n":
		if m.transferring {
			return m, nil
		}
		// New directory
		m.mode = sftpMkdirInput
		m.inputBuffer = ""
		m.inputPrompt = i18n.T("sftp.mkdir_prompt")
		return m, nil

	case "r":
		if m.transferring {
			return m, nil
		}
		// Refresh
		return m, m.loadDirCmd(m.cwd)

	case "up", "down", "k", "j", "pgup", "pgdown", "home", "end", "g", "G":
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *sftpFormModel) handleSearchKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searchMode = false
		m.searchInput.Blur()
		m.table.Focus()
		return m, nil
	case "enter", "tab":
		m.searchMode = false
		m.searchInput.Blur()
		m.table.Focus()
		return m, nil
	}
	var cmd tea.Cmd
	oldValue := m.searchInput.Value()
	m.searchInput, cmd = m.searchInput.Update(msg)
	if m.searchInput.Value() != oldValue {
		m.filterEntries()
	}
	return m, cmd
}

// filterEntries filters the remote entries by search query and rebuilds table.
func (m *sftpFormModel) filterEntries() {
	query := strings.ToLower(m.searchInput.Value())
	if query == "" {
		m.filteredEntries = m.entries
	} else {
		filtered := make([]sftpconfig.RemoteEntry, 0, len(m.entries))
		for _, e := range m.entries {
			if strings.Contains(strings.ToLower(e.Name), query) {
				filtered = append(filtered, e)
			}
		}
		m.filteredEntries = filtered
	}
	// Rebuild table with filtered entries
	saved := m.entries
	m.entries = m.filteredEntries
	m.updateTableRows()
	m.entries = saved // restore original for future searches
}

// filterLocalFiles filters the local file list by search query and rebuilds table.
func (m *sftpFormModel) filterLocalFiles() {
	query := strings.ToLower(m.searchInput.Value())
	allFiles := m.listLocalFiles()
	if query == "" {
		m.localFiles = allFiles
	} else {
		filtered := make([]string, 0, len(allFiles))
		for _, f := range allFiles {
			if strings.Contains(strings.ToLower(filepath.Base(f)), query) {
				filtered = append(filtered, f)
			}
		}
		m.localFiles = filtered
	}
	m.updateLocalTableRows()
}

func (m *sftpFormModel) handleMkdirInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		m.mode = sftpBrowse
		m.inputBuffer = ""
		return m, nil

	case "enter":
		dirName := strings.TrimSpace(m.inputBuffer)
		if dirName == "" {
			m.mode = sftpBrowse
			return m, nil
		}
		newPath := path.Join(m.cwd, dirName)
		return m, m.mkdirCmd(newPath)

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

func (m *sftpFormModel) handlePasswordInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		return m, func() tea.Msg { return sftpDoneMsg{} }

	case "enter":
		password := m.inputBuffer
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
		m.mode = sftpBrowse
		m.selectedEntry = nil
		return m, nil

	case "enter":
		if m.selectedEntry == nil {
			m.mode = sftpBrowse
			return m, nil
		}

		remotePath := path.Join(m.cwd, m.selectedEntry.Name)

		if m.mode == sftpDownloadConfirm {
			m.mode = sftpBrowse
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
			return m, m.deleteCmd(remotePath)
		}

		return m, nil

	case "y":
		if m.selectedEntry == nil {
			m.mode = sftpBrowse
			return m, nil
		}
		remotePath := path.Join(m.cwd, m.selectedEntry.Name)
		if m.mode == sftpDownloadConfirm {
			m.mode = sftpBrowse
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
			return m, m.deleteCmd(remotePath)
		}

		return m, nil

	case "n":
		m.mode = sftpBrowse
		m.selectedEntry = nil
		return m, nil
	}

	return m, nil
}

func (m *sftpFormModel) handleUploadSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle search mode in upload view
	if m.searchMode {
		return m.handleUploadSearchKeys(msg)
	}

	key := msg.String()

	switch key {
	case "esc", "tab", "shift+tab":
		if m.transferring {
			m.loading = false
			m.transferring = false
			m.progressGen++
			m.queue.clear()
			m.setStatus(fmt.Sprintf("Cancelled: %s", m.progressFile))
			return m, nil
		}
		m.mode = sftpBrowse
		m.updateTableRows()
		return m, nil

	case "/", "ctrl+f":
		m.searchMode = true
		m.searchInput.Focus()
		m.table.Blur()
		return m, textinput.Blink

	case "enter":
		idx := m.table.Cursor()
		if idx >= 0 && idx < len(m.localFiles) {
			localPath := m.localFiles[idx]
			info, err := os.Stat(localPath)
			if err != nil {
				return m, nil
			}
			if info.IsDir() {
				if m.transferring {
					return m, nil
				}
				m.localCwd = localPath
				m.localFiles = m.listLocalFiles()
				m.updateLocalTableRows()
				m.table.SetCursor(0)
				return m, nil
			}
			remotePath := path.Join(m.cwd, filepath.Base(localPath))
			return m, m.requestTransfer(sftpTransferJob{
				filename:   filepath.Base(localPath),
				localPath:  localPath,
				remotePath: remotePath,
				isUpload:   true,
			})
		}
		return m, nil

	case "right", "l":
		if m.transferring {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx >= 0 && idx < len(m.localFiles) {
			localPath := m.localFiles[idx]
			info, err := os.Stat(localPath)
			if err == nil && info.IsDir() {
				m.localCwd = localPath
				m.localFiles = m.listLocalFiles()
				m.updateLocalTableRows()
				m.table.SetCursor(0)
			}
		}
		return m, nil

	case "left", "h", "backspace":
		if m.transferring {
			return m, nil
		}
		parent := filepath.Dir(m.localCwd)
		if parent != m.localCwd {
			m.localCwd = parent
			m.localFiles = m.listLocalFiles()
			m.updateLocalTableRows()
			m.table.SetCursor(0)
			return m, nil
		}
		m.mode = sftpBrowse
		m.updateTableRows()
		return m, nil

	case "up", "down", "k", "j", "pgup", "pgdown", "home", "end", "g", "G":
		m.table, _ = m.table.Update(msg)
		return m, nil
	}

	return m, nil
}

func (m *sftpFormModel) handleUploadSearchKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searchMode = false
		m.searchInput.Blur()
		m.table.Focus()
		// Restore full local file list
		m.localFiles = m.listLocalFiles()
		m.updateLocalTableRows()
		return m, nil
	case "enter", "tab":
		m.searchMode = false
		m.searchInput.Blur()
		m.table.Focus()
		return m, nil
	}
	var cmd tea.Cmd
	oldValue := m.searchInput.Value()
	m.searchInput, cmd = m.searchInput.Update(msg)
	if m.searchInput.Value() != oldValue {
		m.filterLocalFiles()
	}
	return m, cmd
}

// View renders the SFTP browser
func (m *sftpFormModel) View() string {
	if m.loading && m.client == nil {
		return m.styles.FormContainer.Render(
			m.styles.FormTitle.Render(" "+i18n.T("sftp.title_remote", m.hostName)+" ") + "\n\n" +
				"  " + i18n.T("sftp.connecting", m.hostName),
		)
	}

	if m.mode == sftpPasswordInput {
		return m.styles.FormContainer.Render(
			m.styles.FormTitle.Render(" SFTP - Password ") + "\n\n" +
				fmt.Sprintf("  %s\n", m.inputPrompt) +
				fmt.Sprintf("  %s_\n", strings.Repeat("*", len(m.inputBuffer))) +
				"\n" +
				m.styles.HelpText.Render("  Enter: connect • Esc: cancel"),
		)
	}

	if m.mode == sftpError {
		return m.renderErrorView()
	}

	// Build the view
	var components []string

	// Title bar
	if m.height < 20 {
		if m.mode == sftpUploadSelect {
			components = append(components, m.styles.Header.Render(i18n.T("sftp.title_local")+" [LOCAL] "+truncatePath(m.localCwd, m.width-25)))
		} else {
			title := i18n.T("sftp.title_remote", m.hostName)
			components = append(components, m.styles.Header.Render(title+" "+truncatePath(m.cwd, m.width-len(title)-10)))
		}
	} else {
		if m.mode == sftpUploadSelect {
			components = append(components, m.styles.Header.Render(i18n.T("sftp.title_local")))
			localStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
			remoteStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("36"))
			components = append(components, localStyle.Render(" [LOCAL]  "+truncatePath(m.localCwd, m.width-12)))
			components = append(components, remoteStyle.Render(" [REMOTE] "+truncatePath(m.cwd, m.width-12)))
		} else {
			components = append(components, m.styles.Header.Render(i18n.T("sftp.title_remote", m.hostName)))
			pathStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(SecondaryColor))
			components = append(components, pathStyle.Render(" "+truncatePath(m.cwd, m.width-10)))
		}
	}

	// Search bar (only in browse mode, not upload mode)
	if m.mode == sftpBrowse || m.mode == sftpUploadSelect {
		searchPrompt := i18n.T("search.prompt")
		components = append(components, renderSearchBar(m.styles, m.searchMode, searchPrompt, m.searchInput.View(), m.width))
	}

	// Table
	components = append(components, m.styles.TableFocused.Render(m.table.View()))

	// Status / progress / input line (below table)
	if m.loading && m.client != nil {
		progressStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("36"))
		components = append(components, progressStyle.Render(fmt.Sprintf("  ⏳ %s...", m.statusMsg)))
	} else if m.statusActive() {
		statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("36"))
		components = append(components, statusStyle.Render(" ✓ "+m.statusMsg))
	} else if m.mode == sftpMkdirInput {
		components = append(components, m.renderInputLine())
	}

	// Confirm dialogs
	if m.mode == sftpDownloadConfirm {
		components = append(components, m.renderDownloadConfirm())
	} else if m.mode == sftpDeleteConfirm {
		components = append(components, m.renderDeleteConfirm())
	}

	// Help text — compact on small heights (< 20 lines)
	if m.height < 20 {
		var helpLine string
		if m.searchMode {
			helpLine = i18n.T("sftp.help_search_1")
		} else if m.mode == sftpUploadSelect {
			helpLine = i18n.T("sftp.help_upload_1")
		} else {
			helpLine = i18n.T("sftp.help_browse_1")
		}
		components = append(components, renderHelpText(m.styles, helpLine, m.width))
	} else {
		var helpLine1, helpLine2 string
		if m.searchMode {
			helpLine1 = i18n.T("sftp.help_search_1")
			helpLine2 = i18n.T("sftp.help_search_2")
		} else if m.mode == sftpUploadSelect {
			helpLine1 = i18n.T("sftp.help_upload_1")
			helpLine2 = i18n.T("sftp.help_upload_2")
		} else {
			helpLine1 = i18n.T("sftp.help_browse_1")
			helpLine2 = i18n.T("sftp.help_browse_2")
		}
		components = append(components, renderHelpText(m.styles, helpLine1, m.width))
		components = append(components, renderHelpText(m.styles, helpLine2, m.width))
	}

	return m.styles.App.Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			components...,
		),
	)
}

func (m *sftpFormModel) renderErrorView() string {
	errStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("9")).
		Bold(true).
		Padding(1, 2)

	// Friendly error message, hide technical details
	content := i18n.T("sftp.err_session")
	return m.styles.FormContainer.Render(errStyle.Render(content))
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
	if m.selectedEntry == nil {
		return ""
	}
	msg := i18n.T("sftp.delete_confirm", m.selectedEntry.Name)
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	return style.Render(msg)
}

func (m *sftpFormModel) renderLocalFileList() string {
	var rows []string
	header := fmt.Sprintf("  %-40s %10s", "Local Files", "Size")
	rows = append(rows, header)
	rows = append(rows, strings.Repeat("-", 52))

	for i, f := range m.localFiles {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		name := filepath.Base(f)
		size := formatSize(info.Size())
		cursor := "  "
		if i == m.table.Cursor() {
			cursor = "→ "
		}
		rows = append(rows, fmt.Sprintf("%s%-40s %10s", cursor, name, size))
	}

	return strings.Join(rows, "\n")
}

func (m *sftpFormModel) getColumns(isLocal bool) []table.Column {
	w := m.width
	if w <= 0 {
		w = 80
	}

	titleKey := "sftp.col_name"
	if isLocal {
		titleKey = "sftp.col_local_file"
	}

	// Bubbles table renders each cell with Padding(0,1) = 2 extra cols per cell.
	// TableFocused style adds border(2). App style adds padding(2).
	// So: rendered = colWidths + numCols*2 + 4 = tw
	//   colWidths = tw - 4 - numCols*2
	if w < 55 {
		sizeW := 8
		if w < 30 {
			sizeW = 6
		}
		nameW := w - 4 - 2*2 - sizeW
		if nameW < 8 {
			nameW = 8
		}
		return []table.Column{
			{Title: i18n.T(titleKey), Width: nameW},
			{Title: i18n.T("sftp.col_size"), Width: sizeW},
		}
	} else if w < 75 {
		nameW := w - 4 - 3*2 - 10 - 6
		if nameW < 12 {
			nameW = 12
		}
		return []table.Column{
			{Title: i18n.T(titleKey), Width: nameW},
			{Title: i18n.T("sftp.col_size"), Width: 10},
			{Title: i18n.T("sftp.col_type"), Width: 6},
		}
	}

	nameW := w - 4 - 4*2 - 10 - 16 - 6
	if nameW < 20 {
		nameW = 20
	}
	return []table.Column{
		{Title: i18n.T(titleKey), Width: nameW},
		{Title: i18n.T("sftp.col_size"), Width: 10},
		{Title: i18n.T("sftp.col_modified"), Width: 16},
		{Title: i18n.T("sftp.col_type"), Width: 6},
	}
}

func (m *sftpFormModel) updateTableRows() {
	// Sort: directories first, then by name
	sort.Slice(m.entries, func(i, j int) bool {
		if m.entries[i].IsDir != m.entries[j].IsDir {
			return m.entries[i].IsDir
		}
		return m.entries[i].Name < m.entries[j].Name
	})

	cols := m.getColumns(false)
	var rows []table.Row
	for _, entry := range m.entries {
		name := entry.Name
		if entry.IsDir {
			name = "📁 " + name
		} else {
			name = "📄 " + name
		}
		size := formatSize(entry.Size)
		modTime := entry.ModTime.Format("Jan 02 15:04")
		entryType := i18n.T("sftp.type_file")
		if entry.IsDir {
			entryType = i18n.T("sftp.type_dir")
		}

		if len(cols) == 2 {
			rows = append(rows, table.Row{name, size})
		} else if len(cols) == 3 {
			rows = append(rows, table.Row{name, size, entryType})
		} else {
			rows = append(rows, table.Row{name, size, modTime, entryType})
		}
	}
	s := table.DefaultStyles()
	s.Selected = m.styles.Selected
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(PrimaryColor)).
		BorderBottom(true).
		Bold(false)

	cursor := m.table.Cursor()
	h := m.calculateTableHeight()
	m.table = table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(h),
		table.WithStyles(s),
	)
	m.table.SetCursor(cursor)
}

// updateLocalTableRows populates the table with local files for upload.
func (m *sftpFormModel) updateLocalTableRows() {
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

	cols := m.getColumns(true)
	var rows []table.Row
	for _, f := range m.localFiles {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		name := filepath.Base(f)
		entryType := i18n.T("sftp.type_file")
		if info.IsDir() {
			name = "📁 " + name
			entryType = i18n.T("sftp.type_dir")
		} else {
			name = "📄 " + name
		}
		size := formatSize(info.Size())
		modTime := info.ModTime().Format("Jan 02 15:04")

		if len(cols) == 2 {
			rows = append(rows, table.Row{name, size})
		} else if len(cols) == 3 {
			rows = append(rows, table.Row{name, size, entryType})
		} else {
			rows = append(rows, table.Row{name, size, modTime, entryType})
		}
	}
	if len(rows) == 0 {
		emptyRow := make(table.Row, len(cols))
		emptyRow[0] = i18n.T("sftp.empty_dir")
		rows = append(rows, emptyRow)
	}

	s := table.DefaultStyles()
	s.Selected = m.styles.Selected
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(PrimaryColor)).
		BorderBottom(true).
		Bold(false)

	cursor := m.table.Cursor()
	h := m.calculateTableHeight()
	m.table = table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(h),
		table.WithStyles(s),
	)
	m.table.SetCursor(cursor)
}

// findEntry finds an entry by name (stripping the emoji prefix)
func (m *sftpFormModel) findEntry(name string) *sftpconfig.RemoteEntry {
	// Strip emoji prefix
	cleanName := strings.TrimPrefix(name, "📁 ")
	cleanName = strings.TrimPrefix(cleanName, "📄 ")

	for i := range m.entries {
		if m.entries[i].Name == cleanName {
			return &m.entries[i]
		}
	}
	return nil
}

// listLocalFiles returns all entries (files and dirs) in localCwd.
func (m *sftpFormModel) listLocalFiles() []string {
	if m.localCwd == "" {
		m.localCwd, _ = os.UserHomeDir()
	}
	entries, err := os.ReadDir(m.localCwd)
	if err != nil {
		return nil
	}
	var files []string
	for _, entry := range entries {
		// Skip hidden files
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		fullPath := filepath.Join(m.localCwd, entry.Name())
		files = append(files, fullPath)
	}
	sort.Strings(files)
	return files
}

// localDownloadPath returns the local path for a downloaded file
func (m *sftpFormModel) localDownloadPath(filename string) string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, "Downloads", filename)
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
