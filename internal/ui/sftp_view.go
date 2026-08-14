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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
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
}

type sftpErrorMsg struct {
	err error
}

type sftpDownloadResultMsg struct {
	filename string
	success  bool
	err      error
}

type sftpProgressMsg struct {
	filename   string
	downloaded int64
	total      int64
	isUpload   bool
}

type sftpUploadResultMsg struct {
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

	// Initialize table
	m.table = table.New(
		table.WithColumns([]table.Column{
			{Title: i18n.T("sftp.col_name"), Width: 40},
			{Title: i18n.T("sftp.col_size"), Width: 10},
			{Title: i18n.T("sftp.col_modified"), Width: 16},
			{Title: i18n.T("sftp.col_type"), Width: 6},
		}),
		table.WithHeight(20),
		table.WithFocused(true),
	)

	// Initialize search input
	m.searchInput = textinput.New()
	m.searchInput.Placeholder = i18n.T("sftp.search_placeholder")
	m.searchInput.CharLimit = 50
	m.searchInput.Width = 25
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
			return sftpErrorMsg{err: err}
		}
		return sftpEntriesMsg{entries: entries, cwd: path}
	}
}

func (m *sftpFormModel) downloadCmd(remotePath, localPath string) tea.Cmd {
	filename := filepath.Base(remotePath)
	m.progressFile = filename
	m.progressDone = 0
	m.progressTotal = 0

	download := func() tea.Msg {
		err := m.client.DownloadWithProgress(remotePath, localPath, func(downloaded, total int64) {
			m.progressDone = downloaded
			m.progressTotal = total
		})
		return sftpDownloadResultMsg{filename: filename, success: err == nil, err: err}
	}

	return tea.Batch(
		download,
		tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
			return sftpProgressMsg{filename: filename, downloaded: m.progressDone, total: m.progressTotal, isUpload: false}
		}),
	)
}

func (m *sftpFormModel) uploadCmd(localPath, remotePath string) tea.Cmd {
	filename := filepath.Base(localPath)
	m.progressFile = filename
	m.progressDone = 0
	m.progressTotal = 0

	upload := func() tea.Msg {
		err := m.client.UploadWithProgress(localPath, remotePath, func(uploaded, total int64) {
			m.progressDone = uploaded
			m.progressTotal = total
		})
		return sftpUploadResultMsg{filename: filename, success: err == nil, err: err}
	}

	return tea.Batch(
		upload,
		tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
			return sftpProgressMsg{filename: filename, downloaded: m.progressDone, total: m.progressTotal, isUpload: true}
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
		m.entries = msg.entries
		m.cwd = msg.cwd
		m.updateTableRows()
		return m, nil

	case sftpErrorMsg:
		m.loadError = msg.err.Error()
		m.mode = sftpError
		m.loading = false
		return m, nil

	case sftpProgressMsg:
		// Ignore progress if download was cancelled
		if !m.loading {
			return m, nil
		}
		// Update progress display
		action := "Downloading"
		if msg.isUpload {
			action = "Uploading"
		}
		if msg.total > 0 {
			pct := msg.downloaded * 100 / msg.total
			m.statusMsg = fmt.Sprintf("%s %s: %s / %s (%d%%)",
				action, msg.filename, formatSize(msg.downloaded), formatSize(msg.total), pct)
		} else {
			m.statusMsg = fmt.Sprintf("%s %s: %s", action, msg.filename, formatSize(msg.downloaded))
		}
		m.statusExpiry = time.Now().Add(10 * time.Second)
		// Keep ticking if still loading
		return m, tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
			return sftpProgressMsg{filename: msg.filename, downloaded: m.progressDone, total: m.progressTotal, isUpload: msg.isUpload}
		})

	case sftpDownloadResultMsg:
		m.loading = false
		if msg.success {
			m.setStatus(fmt.Sprintf("Downloaded: %s → %s", msg.filename, m.localDownloadPath(msg.filename)))
		} else {
			m.loadError = msg.err.Error()
			m.mode = sftpError
		}
		return m, nil

	case sftpUploadResultMsg:
		m.loading = false
		if msg.success {
			m.setStatus("Uploaded: " + msg.filename)
		} else {
			m.loadError = msg.err.Error()
			m.mode = sftpError
		}
		return m, m.loadDirCmd(m.cwd)
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
		m.styles = NewStyles(m.width)
		// Dynamic table sizing
		// otherCols = Size(10) + Modified(16) + Type(6) = 32
		// overhead = container border(2) + padding(4) + table border(2) + emoji(2) = 10
		otherCols := 32
		overhead := 10
		nameWidth := msg.Width - otherCols - overhead
		if nameWidth > 50 {
			nameWidth = 50 // cap for readability
		}
		if nameWidth < 20 {
			nameWidth = 20
		}
		// Use more of the terminal height: title(1) + path(1) + table border(2) + help(1) + padding(2) = 7
		tableHeight := msg.Height - 8
		if tableHeight < 5 {
			tableHeight = 5
		}
		m.table.SetColumns([]table.Column{
			{Title: "Name", Width: nameWidth},
			{Title: "Size", Width: 10},
			{Title: "Modified", Width: 16},
			{Title: "Type", Width: 6},
		})
		m.table.SetHeight(tableHeight)
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
		if m.loading && m.client != nil {
			// Cancel ongoing download/upload
			m.loading = false
			m.setStatus(fmt.Sprintf("Cancelled: %s", m.progressFile))
			m.mode = sftpBrowse
			m.updateTableRows()
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
		// File selected → show download confirm
		m.selectedEntry = entry
		m.mode = sftpDownloadConfirm
		return m, nil

	case "right", "l":
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
		// Left/h/backspace: go to parent directory
		parent := path.Dir(m.cwd)
		if parent == m.cwd {
			return m, nil
		}
		m.loading = true
		m.statusMsg = "Loading " + parent + "..."
		return m, m.loadDirCmd(parent)

	case "u", "tab", "shift+tab":
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
		// New directory
		m.mode = sftpMkdirInput
		m.inputBuffer = ""
		m.inputPrompt = i18n.T("sftp.mkdir_prompt")
		return m, nil

	case "r":
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
			localPath := m.localDownloadPath(m.selectedEntry.Name)
			m.loading = true
			m.setStatus(fmt.Sprintf("Downloading %s", m.selectedEntry.Name))
			m.mode = sftpBrowse
			return m, m.downloadCmd(remotePath, localPath)
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
			localPath := m.localDownloadPath(m.selectedEntry.Name)
			m.loading = true
			m.setStatus(fmt.Sprintf("Downloading %s", m.selectedEntry.Name))
			m.mode = sftpBrowse
			return m, m.downloadCmd(remotePath, localPath)
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
				m.localCwd = localPath
				m.localFiles = m.listLocalFiles()
				m.updateLocalTableRows()
				m.table.SetCursor(0)
				return m, nil
			}
			remotePath := path.Join(m.cwd, filepath.Base(localPath))
			m.mode = sftpBrowse
			m.loading = true
			m.statusMsg = fmt.Sprintf("Uploading %s...", filepath.Base(localPath))
			m.progressFile = filepath.Base(localPath)
			m.progressDone = 0
			m.progressTotal = 0
			return m, m.uploadCmd(localPath, remotePath)
		}
		return m, nil

	case "right", "l":
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
	if m.mode == sftpUploadSelect {
		components = append(components, m.styles.FormTitle.Render(" "+i18n.T("sftp.title_local")+" "))
		localStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
		remoteStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("36"))
		components = append(components, localStyle.Render(" [LOCAL]  "+m.localCwd))
		components = append(components, remoteStyle.Render(" [REMOTE] "+m.cwd))
	} else {
		components = append(components, m.styles.FormTitle.Render(" "+i18n.T("sftp.title_remote", m.hostName)+" "))
		pathStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(SecondaryColor))
		components = append(components, pathStyle.Render(" "+m.cwd))
	}

	// Search bar (only in browse mode, not upload mode)
	if m.mode == sftpBrowse || m.mode == sftpUploadSelect {
		searchPrompt := i18n.T("search.prompt")
		if m.searchMode {
			components = append(components, m.styles.SearchFocused.Render(searchPrompt+m.searchInput.View()))
		} else {
			components = append(components, m.styles.SearchUnfocused.Render(searchPrompt+m.searchInput.View()))
		}
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

	// Help text — two lines for better readability
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
	components = append(components, m.styles.HelpText.Render(helpLine1))
	components = append(components, m.styles.HelpText.Render(helpLine2))

	content := lipgloss.JoinVertical(lipgloss.Left, components...)
	// Use MaxWidth/MaxHeight instead of Width/Height to avoid forcing
	// the container to fill the terminal (which causes layout issues
	// with border + padding). MaxWidth truncates instead of stretching.
	container := m.styles.FormContainer.MaxWidth(m.width).MaxHeight(m.height)
	return container.Render(content)
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

func (m *sftpFormModel) updateTableRows() {
	// Sort: directories first, then by name
	sort.Slice(m.entries, func(i, j int) bool {
		if m.entries[i].IsDir != m.entries[j].IsDir {
			return m.entries[i].IsDir
		}
		return m.entries[i].Name < m.entries[j].Name
	})

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
		rows = append(rows, table.Row{name, size, modTime, entryType})
	}
	m.table.SetRows(rows)

	// Adjust column widths dynamically — same formula as WindowSizeMsg
	otherCols := 32 // Size(10) + Modified(16) + Type(6)
	overhead := 10  // container border(2) + padding(4) + table border(2) + emoji(2)
	nameWidth := m.width - otherCols - overhead
	if nameWidth > 50 {
		nameWidth = 50
	}
	if nameWidth < 20 {
		nameWidth = 20
	}
	m.table.SetColumns([]table.Column{
		{Title: i18n.T("sftp.col_name"), Width: nameWidth},
		{Title: i18n.T("sftp.col_size"), Width: 10},
		{Title: i18n.T("sftp.col_modified"), Width: 16},
		{Title: i18n.T("sftp.col_type"), Width: 6},
	})
}

// updateLocalTableRows populates the table with local files for upload.
func (m *sftpFormModel) updateLocalTableRows() {
	// Sort localFiles first: dirs first, then alphabetical.
	// This ensures table row index aligns with localFiles index.
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
		rows = append(rows, table.Row{name, size, modTime, entryType})
	}
	m.table.SetRows(rows)

	otherCols := 32
	overhead := 10
	nameWidth := m.width - otherCols - overhead
	if nameWidth > 50 {
		nameWidth = 50
	}
	if nameWidth < 20 {
		nameWidth = 20
	}
	m.table.SetColumns([]table.Column{
		{Title: i18n.T("sftp.col_local_file"), Width: nameWidth},
		{Title: i18n.T("sftp.col_size"), Width: 10},
		{Title: i18n.T("sftp.col_modified"), Width: 16},
		{Title: i18n.T("sftp.col_type"), Width: 6},
	})
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
	return fmt.Sprintf("%.1f%cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
