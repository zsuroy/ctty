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
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/webdavclient"
	"github.com/zsuroy/ctty/internal/webdavconfig"
	"github.com/zsuroy/ctty/internal/webdavcred"
)

type webdavMode int

const (
	webdavBrowse webdavMode = iota
	webdavLocalBrowse
	webdavDownloadConfirm
	webdavMkdirInput
	webdavRenameInput
	webdavDeleteConfirm
	webdavPasswordInput
	webdavError
)

const webdavNarrowWidth = 80

type webdavFormModel struct {
	styles   Styles
	width    int
	height   int
	siteName string
	site     webdavconfig.WebDAVSite
	layout   config.WebDAVLayout

	client     *webdavclient.Client
	remoteTbl  table.Model
	localTbl   table.Model
	entries    []webdavclient.RemoteEntry
	cwd        string
	localCwd   string
	mode       webdavMode
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
	queue          webdavTransferQueue

	inputBuffer  string
	inputPrompt  string
	selected     *webdavclient.RemoteEntry
	statusMsg    string
	statusExpiry time.Time
	statusGen    int

	localOp          bool
	pendingLocalPath string

	showInfo   bool
	entryInfo  *webdavEntryInfo
	infoScroll int

	localFiles  []string
	searchInput textinput.Model
	searchMode  bool
}

type webdavEntryInfo struct {
	name    string
	isDir   bool
	size    int64
	modTime time.Time
	path    string
}

type webdavConnectedMsg struct {
	client *webdavclient.Client
	cwd    string
}

type webdavEntriesMsg struct {
	entries []webdavclient.RemoteEntry
	cwd     string
	err     error
}

type webdavErrorMsg struct{ err error }
type webdavPasswordPromptMsg struct{}

type webdavDownloadResultMsg struct {
	gen      int
	filename string
	success  bool
	err      error
}

type webdavUploadResultMsg struct {
	gen      int
	filename string
	success  bool
	err      error
}

type webdavMkdirResultMsg struct {
	name    string
	success bool
	err     error
}

type webdavDeleteResultMsg struct {
	filename string
	success  bool
	err      error
}

type webdavRenameResultMsg struct {
	oldName string
	newName string
	success bool
	err     error
}

type webdavProgressMsg struct {
	gen      int
	filename string
	done     int64
	total    int64
	isUpload bool
}

type webdavDoneMsg struct{}
type webdavBackToSitesMsg struct{}

type webdavStatusExpiredMsg struct {
	gen int
}

func NewWebDAVForm(styles Styles, width, height int, siteName string) *webdavFormModel {
	return NewWebDAVFormWithLayout(styles, width, height, siteName, config.WebDAVLayoutDual)
}

func NewWebDAVFormWithLayout(styles Styles, width, height int, siteName string, layout config.WebDAVLayout) *webdavFormModel {
	site, ok := webdavconfig.Find(siteName)
	if !ok && (strings.HasPrefix(siteName, "http://") || strings.HasPrefix(siteName, "https://")) {
		site = webdavconfig.WebDAVSite{Name: siteName, URL: siteName}
	}
	m := &webdavFormModel{
		styles:     styles,
		width:      width,
		height:     height,
		siteName:   siteName,
		site:       site,
		layout:     config.NormalizeWebDAVLayout(layout),
		mode:       webdavBrowse,
		loading:    true,
		cwd:        "/",
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

func (m *webdavFormModel) Init() tea.Cmd {
	return m.connectCmd("")
}

func (m *webdavFormModel) connectCmd(password string) tea.Cmd {
	return func() tea.Msg {
		client, err := webdavclient.Connect(m.site, password)
		if err != nil {
			msg := err.Error()
			if strings.Contains(msg, "401") || strings.Contains(msg, "auth") || strings.Contains(msg, "Unauthorized") {
				return webdavPasswordPromptMsg{}
			}
			return webdavErrorMsg{err: err}
		}
		return webdavConnectedMsg{client: client, cwd: "/"}
	}
}

func (m *webdavFormModel) loadDirCmd(dir string) tea.Cmd {
	return func() tea.Msg {
		if dir == "" {
			dir = "/"
		}
		entries, err := m.client.List(dir)
		if err != nil {
			return webdavEntriesMsg{cwd: dir, err: err}
		}
		return webdavEntriesMsg{entries: entries, cwd: dir}
	}
}

func (m *webdavFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case webdavConnectedMsg:
		m.client = msg.client
		m.cwd = msg.cwd
		m.loading = false
		if m.password != "" {
			_ = webdavcred.SetPassword(m.siteName, m.password)
		}
		m.setStatus(fmt.Sprintf(i18n.T("webdav.connected"), m.siteName))
		m.refreshLocal()
		return m, m.loadDirCmd(m.cwd)

	case webdavPasswordPromptMsg:
		m.loading = false
		m.mode = webdavPasswordInput
		m.inputBuffer = ""
		m.inputPrompt = fmt.Sprintf(i18n.T("webdav.password_prompt"), m.siteName)
		return m, nil

	case webdavEntriesMsg:
		m.loading = false
		m.refreshing = false
		if msg.err != nil {
			m.setStatus(fmt.Sprintf(i18n.T("webdav.list_error"), msg.err.Error()))
			return m, nil
		}
		m.entries = msg.entries
		m.cwd = msg.cwd
		m.sortEntries()
		m.updateRemoteRows()
		m.clampActiveCursor()
		return m, nil

	case webdavErrorMsg:
		m.loading = false
		m.refreshing = false
		m.loadError = msg.err.Error()
		m.mode = webdavError
		return m, nil

	case webdavProgressMsg:
		if staleWebDAVProgress(m.transferring, m.progressGen, msg.gen) {
			return m, nil
		}
		m.progressDone = msg.done
		m.progressTotal = msg.total
		m.progressFile = msg.filename
		cur, count := m.queue.position()
		m.statusMsg = formatWebDAVProgress(m.queue.current(), msg.done, msg.total, cur, count)
		return m, nil

	case webdavDownloadResultMsg:
		return m, m.handleTransferResult(msg.gen, msg.filename, msg.success, msg.err, false)

	case webdavUploadResultMsg:
		return m, m.handleTransferResult(msg.gen, msg.filename, msg.success, msg.err, true)

	case webdavMkdirResultMsg:
		m.loading = false
		m.localOp = false
		if msg.success {
			m.setStatus(fmt.Sprintf(i18n.T("webdav.dir_created"), msg.name))
		} else {
			m.setStatus(fmt.Sprintf(i18n.T("webdav.mkdir_failed"), msg.err.Error()))
		}
		m.mode = webdavBrowse
		m.inputBuffer = ""
		return m, m.loadDirCmd(m.cwd)

	case webdavDeleteResultMsg:
		m.loading = false
		m.localOp = false
		if msg.success {
			m.setStatus(fmt.Sprintf(i18n.T("webdav.delete_success"), msg.filename))
		} else {
			m.setStatus(fmt.Sprintf(i18n.T("webdav.delete_failed"), msg.err.Error()))
		}
		m.mode = webdavBrowse
		return m, m.loadDirCmd(m.cwd)

	case webdavRenameResultMsg:
		m.loading = false
		m.localOp = false
		if msg.success {
			m.setStatus(fmt.Sprintf(i18n.T("webdav.rename_success"), msg.oldName, msg.newName))
		} else {
			m.setStatus(fmt.Sprintf(i18n.T("webdav.rename_failed"), msg.err.Error()))
		}
		m.mode = webdavBrowse
		m.inputBuffer = ""
		return m, m.loadDirCmd(m.cwd)

	case webdavStatusExpiredMsg:
		if msg.gen == m.statusGen && time.Now().After(m.statusExpiry) {
			m.statusMsg = ""
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.styles = NewStyles(m.width)
		m.searchInput.Width = searchInputWidth(m.width, i18n.T("search.prompt"))
		h := m.paneTableHeight()
		m.remoteTbl.SetHeight(h)
		m.localTbl.SetHeight(h)
		m.updateRemoteRows()
		m.updateLocalRows()
		m.clampActiveCursor()
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	}

	if m.focusLocal {
		m.localTbl, cmd = m.localTbl.Update(msg)
	} else {
		m.remoteTbl, cmd = m.remoteTbl.Update(msg)
	}
	return m, cmd
}

func (m *webdavFormModel) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case webdavPasswordInput:
		return m.handlePasswordInput(msg)
	case webdavMkdirInput:
		return m.handleMkdirInput(msg)
	case webdavRenameInput:
		return m.handleRenameInput(msg)
	case webdavDownloadConfirm, webdavDeleteConfirm:
		return m.handleConfirmDialog(msg)
	case webdavError:
		if msg.String() == "esc" || msg.String() == "enter" || msg.String() == "q" {
			return m, func() tea.Msg { return webdavDoneMsg{} }
		}
		return m, nil
	}

	if m.showInfo {
		switch msg.String() {
		case "esc", "i", "enter", "q":
			m.showInfo = false
			m.entryInfo = nil
			return m, nil
		case "up", "k", "down", "j":
			scrollInfoKey(msg.String(), &m.infoScroll)
			return m, nil
		}
		return m, nil
	}

	if m.searchMode {
		return m.handleSearchKeys(msg)
	}

	return m.handleBrowseKeys(msg)
}

func (m *webdavFormModel) handleBrowseKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc", "q":
		if m.transferring {
			m.cancelTransfer()
			return m, nil
		}
		if m.focusLocal {
			m.setFocusLocal(false)
			return m, nil
		}
		return m, func() tea.Msg { return webdavDoneMsg{} }

	case "tab", "shift+tab":
		m.setFocusLocal(!m.focusLocal)
		return m, nil

	case "u":
		if !m.focusLocal {
			m.setFocusLocal(true)
		}
		return m, nil

	case "v":
		return m.toggleLayout()

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
		m.mode = webdavMkdirInput
		m.inputBuffer = ""
		m.inputPrompt = i18n.T("webdav.mkdir_prompt")
		return m, nil

	case "R":
		if m.transferring {
			return m, m.busyStatus()
		}
		return m.startRenameInput()

	case "i":
		return m.handleShowInfo()

	case "/", "ctrl+f":
		m.searchMode = true
		m.searchInput.Focus()
		m.remoteTbl.Blur()
		m.localTbl.Blur()
		return m, textinput.Blink

	case "r":
		if m.focusLocal {
			m.refreshLocal()
			m.setStatus(i18n.T("webdav.refreshed"))
			return m, nil
		}
		if m.client != nil {
			m.refreshing = true
			m.setStatus(i18n.T("webdav.refreshing"))
			return m, m.loadDirCmd(m.cwd)
		}
		return m, nil
	}

	var cmd tea.Cmd
	if m.focusLocal {
		m.localTbl, cmd = m.localTbl.Update(msg)
	} else {
		m.remoteTbl, cmd = m.remoteTbl.Update(msg)
	}
	return m, cmd
}

func (m *webdavFormModel) handleSearchKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searchMode = false
		m.searchInput.Blur()
		m.searchInput.SetValue("")
		m.updateRemoteRows()
		m.updateLocalRows()
		m.refocusTable()
		return m, nil
	case "enter", "tab":
		m.searchMode = false
		m.searchInput.Blur()
		m.refocusTable()
		return m, nil
	default:
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		m.updateRemoteRows()
		m.updateLocalRows()
		return m, cmd
	}
}

func (m *webdavFormModel) refocusTable() {
	if m.focusLocal {
		m.localTbl.Focus()
		m.remoteTbl.Blur()
	} else {
		m.remoteTbl.Focus()
		m.localTbl.Blur()
	}
}

func (m *webdavFormModel) setFocusLocal(local bool) {
	m.focusLocal = local
	if local {
		m.mode = webdavLocalBrowse
		m.localTbl.Focus()
		m.remoteTbl.Blur()
	} else {
		m.mode = webdavBrowse
		m.remoteTbl.Focus()
		m.localTbl.Blur()
	}
	m.updateRemoteRows()
	m.updateLocalRows()
	m.clampActiveCursor()
}

func (m *webdavFormModel) handleRemoteEnter() (tea.Model, tea.Cmd) {
	e := m.selectedRemote()
	if e == nil {
		return m, nil
	}
	if e.IsDir {
		next := path.Join(m.cwd, e.Name)
		m.loading = true
		return m, m.loadDirCmd(next)
	}
	job := webdavTransferJob{
		filename:   e.Name,
		localPath:  filepath.Join(m.localCwd, e.Name),
		remotePath: path.Join(m.cwd, e.Name),
		isUpload:   false,
	}
	if m.transferring {
		return m, m.requestTransfer(job)
	}
	m.selected = e
	m.mode = webdavDownloadConfirm
	return m, nil
}

func (m *webdavFormModel) handleLocalEnter() (tea.Model, tea.Cmd) {
	f := m.selectedLocal()
	if f == "" {
		return m, nil
	}
	fi, err := os.Stat(f)
	if err != nil {
		return m, nil
	}
	if fi.IsDir() {
		m.localCwd = f
		m.refreshLocal()
		return m, nil
	}
	name := filepath.Base(f)
	job := webdavTransferJob{
		filename:   name,
		localPath:  f,
		remotePath: path.Join(m.cwd, name),
		isUpload:   true,
	}
	return m, m.requestTransfer(job)
}

func (m *webdavFormModel) handleRemoteOpenDir() (tea.Model, tea.Cmd) {
	e := m.selectedRemote()
	if e == nil || !e.IsDir {
		return m, nil
	}
	m.loading = true
	return m, m.loadDirCmd(path.Join(m.cwd, e.Name))
}

func (m *webdavFormModel) handleLocalOpenDir() (tea.Model, tea.Cmd) {
	f := m.selectedLocal()
	if f == "" {
		return m, nil
	}
	if fi, err := os.Stat(f); err == nil && fi.IsDir() {
		m.localCwd = f
		m.refreshLocal()
	}
	return m, nil
}

func (m *webdavFormModel) remoteUp() tea.Cmd {
	if m.cwd == "/" || m.cwd == "." || m.cwd == "" {
		return nil
	}
	parent := path.Dir(m.cwd)
	m.loading = true
	return m.loadDirCmd(parent)
}

// toggleLayout flips dual/single pane layout and persists it to app config.
func (m *webdavFormModel) toggleLayout() (tea.Model, tea.Cmd) {
	if m.narrow() {
		return m, m.setStatus(i18n.T("webdav.too_narrow"))
	}
	if m.layout == config.WebDAVLayoutSingle {
		m.layout = config.WebDAVLayoutDual
	} else {
		m.layout = config.WebDAVLayoutSingle
	}
	m.updateRemoteRows()
	m.updateLocalRows()
	m.clampActiveCursor()
	persistWebDAVLayout(m.layout)
	name := i18n.T("webdav.layout_dual")
	if m.layout == config.WebDAVLayoutSingle {
		name = i18n.T("webdav.layout_single")
	}
	return m, m.setStatus(fmt.Sprintf(i18n.T("webdav.layout_status"), name))
}

// persistWebDAVLayout writes the chosen layout to ~/.config/ctty/config.json.
// Failures are silent — the in-memory layout still applies for this session.
func persistWebDAVLayout(layout config.WebDAVLayout) {
	cfg, err := config.LoadAppConfig()
	if err != nil || cfg == nil {
		fallback := config.GetDefaultAppConfig()
		cfg = &fallback
	}
	cfg.WebDAVLayout = config.NormalizeWebDAVLayout(layout)
	_ = config.SaveAppConfig(cfg)
}

func (m *webdavFormModel) localUp() tea.Cmd {
	parent := filepath.Dir(m.localCwd)
	if parent != m.localCwd {
		m.localCwd = parent
		m.refreshLocal()
	}
	return nil
}

func (m *webdavFormModel) startDeleteConfirm() (tea.Model, tea.Cmd) {
	if m.focusLocal {
		f := m.selectedLocal()
		if f == "" {
			return m, nil
		}
		m.localOp = true
		m.pendingLocalPath = f
		m.mode = webdavDeleteConfirm
		return m, nil
	}
	e := m.selectedRemote()
	if e == nil {
		return m, nil
	}
	m.localOp = false
	m.selected = e
	m.mode = webdavDeleteConfirm
	return m, nil
}

func (m *webdavFormModel) startRenameInput() (tea.Model, tea.Cmd) {
	if m.focusLocal {
		f := m.selectedLocal()
		if f == "" {
			return m, nil
		}
		m.localOp = true
		m.pendingLocalPath = f
		m.mode = webdavRenameInput
		m.inputBuffer = filepath.Base(f)
		m.inputPrompt = i18n.T("webdav.rename_prompt")
		return m, nil
	}
	e := m.selectedRemote()
	if e == nil {
		return m, nil
	}
	m.localOp = false
	m.selected = e
	m.mode = webdavRenameInput
	m.inputBuffer = e.Name
	m.inputPrompt = i18n.T("webdav.rename_prompt")
	return m, nil
}

func (m *webdavFormModel) handleShowInfo() (tea.Model, tea.Cmd) {
	if m.focusLocal {
		f := m.selectedLocal()
		if f == "" {
			return m, nil
		}
		fi, err := os.Stat(f)
		if err != nil {
			return m, nil
		}
		m.entryInfo = &webdavEntryInfo{
			name:    fi.Name(),
			isDir:   fi.IsDir(),
			size:    fi.Size(),
			modTime: fi.ModTime(),
			path:    f,
		}
		m.showInfo = true
		m.infoScroll = 0
		return m, nil
	}
	e := m.selectedRemote()
	if e == nil {
		return m, nil
	}
	m.entryInfo = &webdavEntryInfo{
		name:    e.Name,
		isDir:   e.IsDir,
		size:    e.Size,
		modTime: e.ModTime,
		path:    path.Join(m.cwd, e.Name),
	}
	m.showInfo = true
	m.infoScroll = 0
	return m, nil
}

func (m *webdavFormModel) handleConfirmDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "y", "Y", "enter":
		if m.mode == webdavDownloadConfirm {
			m.mode = webdavBrowse
			if m.selected != nil {
				job := webdavTransferJob{
					filename:   m.selected.Name,
					localPath:  filepath.Join(m.localCwd, m.selected.Name),
					remotePath: path.Join(m.cwd, m.selected.Name),
					isUpload:   false,
				}
				m.selected = nil
				return m, m.requestTransfer(job)
			}
			return m, nil
		}
		if m.mode == webdavDeleteConfirm {
			m.mode = webdavBrowse
			if m.localOp {
				p := m.pendingLocalPath
				m.pendingLocalPath = ""
				m.localOp = false
				_ = os.RemoveAll(p)
				m.refreshLocal()
				m.setStatus(fmt.Sprintf(i18n.T("webdav.delete_success"), filepath.Base(p)))
				return m, nil
			}
			if m.selected != nil {
				e := m.selected
				m.selected = nil
				m.loading = true
				remPath := path.Join(m.cwd, e.Name)
				return m, func() tea.Msg {
					err := m.client.Delete(remPath)
					return webdavDeleteResultMsg{filename: e.Name, success: err == nil, err: err}
				}
			}
			return m, nil
		}
	case "n", "N", "esc", "q":
		m.mode = webdavBrowse
		m.selected = nil
		m.pendingLocalPath = ""
		m.localOp = false
		return m, nil
	}
	return m, nil
}

func (m *webdavFormModel) handlePasswordInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		pw := m.inputBuffer
		m.password = pw
		m.inputBuffer = ""
		m.loading = true
		m.mode = webdavBrowse
		return m, m.connectCmd(pw)
	case tea.KeyEsc:
		return m, func() tea.Msg { return webdavDoneMsg{} }
	case tea.KeyBackspace:
		if len(m.inputBuffer) > 0 {
			m.inputBuffer = m.inputBuffer[:len(m.inputBuffer)-1]
		}
	case tea.KeyRunes:
		m.inputBuffer += string(msg.Runes)
	}
	return m, nil
}

func (m *webdavFormModel) handleMkdirInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		name := strings.TrimSpace(m.inputBuffer)
		m.inputBuffer = ""
		m.mode = webdavBrowse
		if name == "" {
			return m, nil
		}
		if m.localOp {
			m.localOp = false
			_ = os.MkdirAll(filepath.Join(m.localCwd, name), 0755)
			m.refreshLocal()
			m.setStatus(fmt.Sprintf(i18n.T("webdav.dir_created"), name))
			return m, nil
		}
		m.loading = true
		remPath := path.Join(m.cwd, name)
		return m, func() tea.Msg {
			err := m.client.Mkdir(remPath)
			return webdavMkdirResultMsg{name: name, success: err == nil, err: err}
		}
	case tea.KeyEsc:
		m.mode = webdavBrowse
		m.inputBuffer = ""
		m.localOp = false
		return m, nil
	case tea.KeyBackspace:
		if len(m.inputBuffer) > 0 {
			m.inputBuffer = m.inputBuffer[:len(m.inputBuffer)-1]
		}
	case tea.KeyRunes:
		m.inputBuffer += string(msg.Runes)
	}
	return m, nil
}

func (m *webdavFormModel) handleRenameInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		newName := strings.TrimSpace(m.inputBuffer)
		m.inputBuffer = ""
		m.mode = webdavBrowse
		if newName == "" {
			return m, nil
		}
		if m.localOp {
			p := m.pendingLocalPath
			m.pendingLocalPath = ""
			m.localOp = false
			dest := filepath.Join(filepath.Dir(p), newName)
			_ = os.Rename(p, dest)
			m.refreshLocal()
			m.setStatus(fmt.Sprintf(i18n.T("webdav.rename_success"), filepath.Base(p), newName))
			return m, nil
		}
		if m.selected != nil {
			oldName := m.selected.Name
			m.selected = nil
			m.loading = true
			oldPath := path.Join(m.cwd, oldName)
			newPath := path.Join(m.cwd, newName)
			return m, func() tea.Msg {
				err := m.client.Rename(oldPath, newPath)
				return webdavRenameResultMsg{oldName: oldName, newName: newName, success: err == nil, err: err}
			}
		}
		return m, nil
	case tea.KeyEsc:
		m.mode = webdavBrowse
		m.inputBuffer = ""
		m.pendingLocalPath = ""
		m.localOp = false
		return m, nil
	case tea.KeyBackspace:
		if len(m.inputBuffer) > 0 {
			m.inputBuffer = m.inputBuffer[:len(m.inputBuffer)-1]
		}
	case tea.KeyRunes:
		m.inputBuffer += string(msg.Runes)
	}
	return m, nil
}

func (m *webdavFormModel) requestTransfer(job webdavTransferJob) tea.Cmd {
	next, startNow := m.queue.startOrEnqueue(job)
	if startNow {
		return m.beginJob(next)
	}
	cur, count := m.queue.position()
	m.setStatus(fmt.Sprintf("Queued: %s [%d/%d]", job.filename, cur, count))
	return nil
}

func (m *webdavFormModel) beginJob(job webdavTransferJob) tea.Cmd {
	m.transferring = true
	m.progressGen++
	gen := m.progressGen
	m.progressDone = 0
	m.progressTotal = 0
	m.progressFile = job.filename

	ctx, cancel := context.WithCancel(context.Background())
	m.transferCancel = cancel

	return func() tea.Msg {
		onProgress := func(done, total int64) {
			// In bubbletea, background progress reports could send tea.Program messages
			// but for simplicity we rely on transfer result or direct channel.
		}

		if job.isUpload {
			err := m.client.UploadWithProgressCtx(ctx, job.localPath, job.remotePath, onProgress)
			return webdavUploadResultMsg{gen: gen, filename: job.filename, success: err == nil, err: err}
		}
		err := m.client.DownloadWithProgressCtx(ctx, job.remotePath, job.localPath, onProgress)
		return webdavDownloadResultMsg{gen: gen, filename: job.filename, success: err == nil, err: err}
	}
}

func (m *webdavFormModel) handleTransferResult(gen int, filename string, success bool, err error, isUpload bool) tea.Cmd {
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
		_, _ = os.Stdout.WriteString("\a")
		if isUpload {
			m.setStatus(fmt.Sprintf(i18n.T("webdav.upload_success"), filename, m.cwd))
			return m.loadDirCmd(m.cwd)
		}
		m.setStatus(fmt.Sprintf(i18n.T("webdav.download_success"), filename, filepath.Join(m.localCwd, filename)))
		m.refreshLocal()
		return nil
	}
	if err != nil {
		if err == context.Canceled {
			m.setStatus(fmt.Sprintf(i18n.T("webdav.transfer_cancelled"), filename))
		} else {
			m.setStatus(fmt.Sprintf(i18n.T("webdav.transfer_failed"), err.Error()))
		}
	}
	return nil
}

func (m *webdavFormModel) cancelTransfer() {
	if m.transferCancel != nil {
		m.transferCancel()
		m.transferCancel = nil
	}
	m.transferring = false
	m.loading = false
	m.progressGen++
	m.queue.clear()
	m.setStatus(i18n.T("webdav.transfer_cancelled"))
}

func (m *webdavFormModel) busyStatus() tea.Cmd {
	return m.setStatus(i18n.T("webdav.busy"))
}

func (m *webdavFormModel) setStatus(s string) tea.Cmd {
	m.statusMsg = s
	m.statusExpiry = time.Now().Add(4 * time.Second)
	m.statusGen++
	gen := m.statusGen
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg {
		return webdavStatusExpiredMsg{gen: gen}
	})
}

func (m *webdavFormModel) statusActive() bool {
	return m.statusMsg != "" && time.Now().Before(m.statusExpiry)
}

func (m *webdavFormModel) sortEntries() {
	sort.Slice(m.entries, func(i, j int) bool {
		if m.entries[i].IsDir != m.entries[j].IsDir {
			return m.entries[i].IsDir
		}
		return strings.ToLower(m.entries[i].Name) < strings.ToLower(m.entries[j].Name)
	})
}

func (m *webdavFormModel) refreshLocal() {
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

func (m *webdavFormModel) updateRemoteRows() {
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
		sz := formatSize(e.Size)
		if e.IsDir {
			sz = ""
		}
		rows = append(rows, table.Row{name, kind, sz})
	}
	if len(rows) == 0 {
		emptyRow := make(table.Row, len(cols))
		emptyRow[0] = i18n.T("sftp.empty_dir")
		rows = append(rows, emptyRow)
	}
	m.remoteTbl.SetRows(rows)
}

func (m *webdavFormModel) updateLocalRows() {
	cols := m.localColumns()
	m.localTbl.SetColumns(cols)
	var rows []table.Row
	for _, f := range m.filteredLocal() {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		kind := i18n.T("sftp.type_file")
		sz := formatSize(info.Size())
		name := filepath.Base(f)
		if info.IsDir() {
			name = "📁 " + name
			kind = i18n.T("sftp.type_dir")
			sz = ""
		} else {
			name = "📄 " + name
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

func (m *webdavFormModel) clampActiveCursor() {
	if m.focusLocal {
		m.clampTableCursor(&m.localTbl, len(m.filteredLocal()))
	} else {
		m.clampTableCursor(&m.remoteTbl, len(m.filteredRemote()))
	}
}

func (m *webdavFormModel) clampTableCursor(t *table.Model, count int) {
	if count == 0 {
		return
	}
	if t.Cursor() >= count {
		t.SetCursor(count - 1)
	} else if t.Cursor() < 0 {
		t.SetCursor(0)
	}
}

func (m *webdavFormModel) selectedRemote() *webdavclient.RemoteEntry {
	rows := m.filteredRemote()
	idx := m.remoteTbl.Cursor()
	if idx < 0 || idx >= len(rows) {
		return nil
	}
	return &rows[idx]
}

func (m *webdavFormModel) selectedLocal() string {
	rows := m.filteredLocal()
	idx := m.localTbl.Cursor()
	if idx < 0 || idx >= len(rows) {
		return ""
	}
	return rows[idx]
}

func (m *webdavFormModel) filteredRemote() []webdavclient.RemoteEntry {
	q := strings.ToLower(strings.TrimSpace(m.searchInput.Value()))
	if q == "" || m.focusLocal {
		return m.entries
	}
	words := strings.Fields(q)
	var out []webdavclient.RemoteEntry
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

func (m *webdavFormModel) filteredLocal() []string {
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

func (m *webdavFormModel) paneTableHeight() int {
	// Frame budget: header(1) + paths(2) + search(3) + table box(h+2) +
	// help(3 tall, 1 compact) + status/extras row(1 when a toast is up or
	// an mkdir/rename input is open). The frame must never exceed the
	// terminal height: on overflow the alt-screen scrolls and the diff
	// renderer never rewrites the unchanged header, losing it permanently.
	overhead := 11
	if m.height < 20 {
		overhead = 9
	}
	if m.mode == webdavMkdirInput || m.mode == webdavRenameInput ||
		m.mode == webdavDownloadConfirm || m.statusActive() || m.transferring || m.refreshing {
		overhead++
	}
	h := m.height - overhead
	if h < 3 {
		h = 3
	}
	return h
}

// paneContentWidth is the bubbles-table column budget inside Width(paneWidth) content.
func (m *webdavFormModel) paneContentWidth() int {
	// 3 cells * Padding(0,1)=2 → 6 chrome inside the bordered content box.
	w := m.paneWidth() - 6
	if w < 12 {
		return 12
	}
	return w
}

func (m *webdavFormModel) remoteColumns() []table.Column {
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

func (m *webdavFormModel) localColumns() []table.Column {
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

func (m *webdavFormModel) narrow() bool {
	return m.width > 0 && m.width < webdavNarrowWidth
}

func (m *webdavFormModel) singlePane() bool {
	return m.layout == config.WebDAVLayoutSingle || m.narrow()
}

func (m *webdavFormModel) focusLabel(local bool) string {
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

func (m *webdavFormModel) paneWidth() int {
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

func (m *webdavFormModel) renderInfoView() string {
	info := m.entryInfo
	if info == nil {
		return ""
	}
	kind := i18n.T("sftp.type_file")
	if info.isDir {
		kind = i18n.T("sftp.type_dir")
	}
	size := formatSize(info.size)
	mod := info.modTime.Format("2006-01-02 15:04:05")
	if info.isDir {
		size = i18n.T("info.not_set")
	}
	if info.modTime.IsZero() {
		mod = i18n.T("info.not_set")
	}

	titleText := m.styles.Header.Render(strings.TrimSpace(i18n.T("webdav.entry_info_title", info.name)))

	rows := [][2]string{
		{i18n.T("sftp.col_name") + ":", info.name},
		{i18n.T("sftp.col_type") + ":", kind},
		{i18n.T("sftp.col_size") + ":", size},
		{i18n.T("sftp.col_modified") + ":", mod},
		{i18n.T("webdav.info_path") + ":", info.path},
	}

	maxLabelW := 0
	for _, r := range rows {
		if w := ansi.StringWidth(r[0]); w > maxLabelW {
			maxLabelW = w
		}
	}

	labelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(m.styles.Theme.Primary)

	var bodyLines []string
	for _, r := range rows {
		line := lipgloss.JoinHorizontal(
			lipgloss.Top,
			labelStyle.Render("  "+padDisplay(r[0], maxLabelW)),
			" ",
			r[1],
		)
		bodyLines = append(bodyLines, line)
	}

	totalHeight := m.height
	if totalHeight <= 0 {
		totalHeight = 24
	}

	boxWidth := m.width - 4
	if boxWidth < 20 {
		boxWidth = 20
	}

	container := m.styles.FormContainer
	if totalHeight < 24 {
		container = container.Padding(0, 1)
	}

	innerW := boxWidth - container.GetHorizontalFrameSize()
	if innerW < 10 {
		innerW = 10
	}

	targetBoxH := totalHeight
	if totalHeight >= 14 {
		targetBoxH = totalHeight - 1
	}

	frameH := container.GetVerticalFrameSize()
	headerH := lipgloss.Height(titleText)
	helpText := m.styles.HelpText.Width(innerW).Render(i18n.T("ftp.browser_info_help"))
	helpH := lipgloss.Height(helpText)

	overhead := frameH + headerH + helpH + 2
	viewportHeight := targetBoxH - overhead
	if viewportHeight < 3 {
		viewportHeight = 3
	}
	bodyLines = wrapInfoLines(bodyLines, innerW)
	visibleBody := scrollInfoWindow(bodyLines, viewportHeight, &m.infoScroll)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		titleText,
		"",
		visibleBody,
		"",
		helpText,
	)

	box := alignJetBrainsBox(container.Width(boxWidth).Render(content))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top, box)
}

func (m *webdavFormModel) renderErrorView() string {
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
	content := i18n.T("webdav.err_session", detail)
	return renderFormPage(m.styles, m.width, errStyle.Render(content))
}

func (m *webdavFormModel) renderDeleteConfirmBox() string {
	name, isDir := "", false
	if m.selected != nil {
		name, isDir = m.selected.Name, m.selected.IsDir
	} else if m.pendingLocalPath != "" {
		name = filepath.Base(m.pendingLocalPath)
		if st, err := os.Stat(m.pendingLocalPath); err == nil {
			isDir = st.IsDir()
		}
	}
	if name == "" {
		return ""
	}
	confirmKey := "webdav.delete_confirm"
	if isDir {
		confirmKey = "webdav.delete_dir_confirm"
	}
	return renderConfirmBox(m.styles, m.width,
		m.styles.ErrorText.Render(i18n.T("delete.title")),
		i18n.T(confirmKey, name),
		i18n.T("delete.warning"),
		m.styles.HelpText.Render(i18n.T("delete.help")),
	)
}

func (m *webdavFormModel) renderDownloadConfirmBox() string {
	if m.selected == nil {
		return ""
	}
	return renderCardBox(m.styles.FormContainer, m.width,
		m.styles.Header.Render(i18n.T("download.title")),
		i18n.T("webdav.download_confirm", m.selected.Name, filepath.Join(m.localCwd, m.selected.Name)),
		m.styles.HelpText.Render(i18n.T("delete.help")),
	)
}

func (m *webdavFormModel) View() string {
	th := m.paneTableHeight()
	m.localTbl.SetHeight(th)
	m.remoteTbl.SetHeight(th)

	if m.loading && m.client == nil && m.mode != webdavPasswordInput {
		inner := formPageInnerWidth(m.width)
		body := lipgloss.NewStyle().Width(inner).Render(
			m.styles.FormTitle.Render(" WebDAV — "+m.siteName+" ") + "\n\n" +
				"  Connecting to " + m.site.URL + "…")
		return renderFormPage(m.styles, m.width, body)
	}
	if m.mode == webdavPasswordInput {
		inner := formPageInnerWidth(m.width)
		body := lipgloss.NewStyle().Width(inner).Render(
			m.styles.FormTitle.Render(" WebDAV — Password ") + "\n\n" +
				fmt.Sprintf("  %s\n", m.inputPrompt) +
				fmt.Sprintf("  %s_\n", strings.Repeat("*", len(m.inputBuffer))) +
				"\n" + m.styles.HelpText.Render("  "+i18n.T("webdav.help_password")) +
				"\n" + m.styles.HelpText.Render("  "+i18n.T("webdav.cred_hint")))
		return renderFormPage(m.styles, m.width, body)
	}
	if m.mode == webdavError {
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

	var headerTitle string
	if m.site.User != "" && m.siteName != m.site.URL {
		headerTitle = fmt.Sprintf(" WebDAV — %s (%s@%s) ", m.siteName, m.site.User, m.site.URL)
	} else if m.site.User != "" {
		headerTitle = fmt.Sprintf(" WebDAV — %s (%s) ", m.site.URL, m.site.User)
	} else {
		headerTitle = fmt.Sprintf(" WebDAV — %s ", m.siteName)
	}
	header := m.styles.Header.Render(headerTitle)

	var paths string
	var panes string
	pw := m.paneWidth()
	pathPW := m.width - 4
	if pathPW < 20 {
		pathPW = 20
	}
	if m.singlePane() {
		tableStyle := m.styles.TableFocused
		if m.searchMode {
			tableStyle = m.styles.TableUnfocused
		}
		paths = localStyle.Render(fmt.Sprintf("%s  %s", m.focusLabel(true), truncatePath(m.localCwd, pathPW-ansi.StringWidth(m.focusLabel(true))-4))) + "\n" +
			remoteStyle.Render(fmt.Sprintf("%s %s", m.focusLabel(false), truncatePath(m.cwd, pathPW-ansi.StringWidth(m.focusLabel(false))-3)))
		if m.focusLocal {
			panes = tableStyle.Width(pw).Render(m.localTbl.View())
		} else {
			panes = tableStyle.Width(pw).Render(m.remoteTbl.View())
		}
	} else {
		paths = localStyle.Render(fmt.Sprintf("%s  %s", m.focusLabel(true), truncatePath(m.localCwd, pathPW-ansi.StringWidth(m.focusLabel(true))-4))) + "\n" +
			remoteStyle.Render(fmt.Sprintf("%s %s", m.focusLabel(false), truncatePath(m.cwd, pathPW-ansi.StringWidth(m.focusLabel(false))-3)))
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
	if m.transferring || m.refreshing {
		progressStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("36"))
		extras = append(extras, progressStyle.Render(" ⏳ "+m.statusMsg))
	} else if m.statusActive() {
		extras = append(extras, renderStatusToast(m.statusMsg))
	}

	if m.mode == webdavMkdirInput || m.mode == webdavRenameInput {
		extras = append(extras, lipgloss.NewStyle().Foreground(lipgloss.Color(PrimaryColor)).Render(
			fmt.Sprintf("  %s %s_", m.inputPrompt, m.inputBuffer)))
	}

	var helpParts []string
	if m.height < 20 {
		if m.searchMode {
			helpParts = append(helpParts, i18n.T("webdav.help_search_1"))
		} else if m.focusLocal {
			helpParts = append(helpParts, i18n.T("webdav.help_local_1"))
		} else {
			helpParts = append(helpParts, i18n.T("webdav.help_remote_1"))
		}
	} else {
		if m.searchMode {
			helpParts = append(helpParts, i18n.T("webdav.help_search_1"), i18n.T("webdav.help_search_2"))
		} else if m.focusLocal {
			helpParts = append(helpParts, i18n.T("webdav.help_local_1"), i18n.T("webdav.help_local_2"), i18n.T("webdav.help_local_3"))
		} else {
			helpParts = append(helpParts, i18n.T("webdav.help_remote_1"), i18n.T("webdav.help_remote_2"), i18n.T("webdav.help_remote_3"))
		}
	}
	helpParts = dedupeStrings(helpParts)

	parts := []string{header, paths, renderSearchBar(m.styles, m.searchMode, i18n.T("search.prompt"), m.searchInput.View(), m.width)}
	parts = append(parts, panes)
	parts = append(parts, extras...)
	if help := strings.Join(helpParts, "\n"); help != "" {
		parts = append(parts, renderHelpText(m.styles, help, m.width))
	}
	base := m.styles.App.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
	switch m.mode {
	case webdavDeleteConfirm:
		return renderConfirmModal(m.width, m.height, m.renderDeleteConfirmBox())
	case webdavDownloadConfirm:
		if box := m.renderDownloadConfirmBox(); box != "" {
			return renderConfirmModal(m.width, m.height, box)
		}
	}
	return base
}
