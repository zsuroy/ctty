package ui

import (
	"context"
	"errors"
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
	"github.com/zsuroy/ctty/internal/s3client"
	"github.com/zsuroy/ctty/internal/s3config"
)

type s3Mode int

const (
	s3Browse s3Mode = iota
	s3LocalBrowse
	s3DownloadConfirm
	s3MkdirInput
	s3RenameInput
	s3DeleteConfirm
	s3Error
)

const s3NarrowWidth = 80

type s3DoneMsg struct{}

type s3FormModel struct {
	styles     Styles
	width      int
	height     int
	siteName   string
	site       s3config.S3Site
	client     *s3client.Client
	layout     config.S3Layout
	table      table.Model // remote table
	localTbl   table.Model
	entries    []s3client.RemoteEntry
	cwd        string // remote path: "" (buckets), "bucket", "bucket/prefix"
	localCwd   string
	mode       s3Mode
	loading    bool
	refreshing bool
	loadError  string
	focusLocal bool

	progressDone   int64
	progressTotal  int64
	progressFile   string
	progressGen    int
	transferring   bool
	transferCancel context.CancelFunc
	queue          s3TransferQueue

	inputBuffer  string
	inputPrompt  string
	selected     *s3client.RemoteEntry
	statusMsg    string
	statusExpiry time.Time
	statusGen    int

	localOp          bool
	pendingLocalPath string

	showInfo   bool
	entryInfo  *s3EntryInfo
	infoScroll int

	localFiles  []string
	searchInput textinput.Model
	searchMode  bool
}

type s3EntryInfo struct {
	name    string
	isDir   bool
	size    int64
	modTime time.Time
	path    string
	etag    string
}

type s3ConnectedMsg struct {
	client *s3client.Client
	cwd    string
}

type s3EntriesMsg struct {
	entries []s3client.RemoteEntry
	cwd     string
	err     error
}

type s3ErrorMsg struct{ err error }

type s3DownloadResultMsg struct {
	gen      int
	filename string
	success  bool
	err      error
}

type s3UploadResultMsg struct {
	gen      int
	filename string
	success  bool
	err      error
}

type s3ProgressMsg struct {
	gen   int
	done  int64
	total int64
}

type s3StatusExpiredMsg struct {
	gen int
}

type s3MkdirResultMsg struct {
	name    string
	success bool
	err     error
}

type s3RenameResultMsg struct {
	oldName string
	newName string
	success bool
	err     error
}

type s3DeleteResultMsg struct {
	filename string
	success  bool
	err      error
}

func connectS3Cmd(site s3config.S3Site) tea.Cmd {
	return func() tea.Msg {
		client, err := s3client.Connect(site, "")
		if err != nil {
			return s3ErrorMsg{err: err}
		}
		initialCwd := site.Bucket
		return s3ConnectedMsg{client: client, cwd: initialCwd}
	}
}

func fetchS3EntriesCmd(client *s3client.Client, remotePath string) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return s3EntriesMsg{cwd: remotePath, err: errors.New("client disconnected")}
		}
		entries, err := client.List(context.Background(), remotePath)
		return s3EntriesMsg{entries: entries, cwd: remotePath, err: err}
	}
}

// NewS3Form creates an S3 browser model.
func NewS3Form(styles Styles, width, height int, siteName string) *s3FormModel {
	return NewS3FormWithLayout(styles, width, height, siteName, config.S3LayoutDual)
}

// NewS3FormWithLayout creates an S3 browser model with explicit layout preference.
func NewS3FormWithLayout(styles Styles, width, height int, siteName string, layout config.S3Layout) *s3FormModel {
	site, ok := s3config.Find(siteName)
	if !ok {
		site = s3config.S3Site{Name: siteName}
	}

	m := &s3FormModel{
		styles:     styles,
		width:      width,
		height:     height,
		siteName:   siteName,
		site:       site,
		layout:     config.NormalizeS3Layout(layout),
		cwd:        site.Bucket,
		localCwd:   defaultLocalUploadDir(),
		loading:    true,
		focusLocal: false,
	}

	m.searchInput = textinput.New()
	m.searchInput.Placeholder = i18n.T("search.placeholder")
	m.searchInput.CharLimit = 50
	m.searchInput.Width = searchInputWidth(width, i18n.T("search.prompt"))

	h := m.paneTableHeight()
	m.table = table.New(table.WithColumns(m.remoteColumns()), table.WithHeight(h), table.WithFocused(true))
	m.localTbl = table.New(table.WithColumns(m.localColumns()), table.WithHeight(h), table.WithFocused(false))

	m.refreshLocal()
	return m
}

func (m *s3FormModel) Init() tea.Cmd {
	return connectS3Cmd(m.site)
}

func (m *s3FormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.styles = NewStyles(m.width)
		m.searchInput.Width = searchInputWidth(m.width, i18n.T("search.prompt"))
		h := m.paneTableHeight()
		m.table.SetHeight(h)
		m.localTbl.SetHeight(h)
		m.updateRemoteRows()
		m.updateLocalRows()
		m.clampActiveCursor()
		return m, nil

	case s3ConnectedMsg:
		m.client = msg.client
		m.cwd = msg.cwd
		m.loading = false
		m.setStatus(fmt.Sprintf(i18n.T("webdav.connected"), m.site.Name))
		m.refreshLocal()
		return m, fetchS3EntriesCmd(m.client, m.cwd)

	case s3EntriesMsg:
		m.loading = false
		m.refreshing = false
		if msg.err != nil {
			if len(m.entries) == 0 && (m.cwd == "" || m.cwd == m.site.Bucket) {
				m.loadError = msg.err.Error()
				m.mode = s3Error
				return m, nil
			}
			m.setStatus(fmt.Sprintf("S3 error: %s", msg.err.Error()))
			return m, nil
		}
		m.entries = msg.entries
		m.cwd = msg.cwd
		m.sortEntries()
		m.updateRemoteRows()
		m.clampActiveCursor()
		return m, nil

	case s3ErrorMsg:
		m.loading = false
		m.refreshing = false
		m.loadError = msg.err.Error()
		m.mode = s3Error
		return m, nil

	case s3ProgressMsg:
		if staleS3Progress(m.transferring, m.progressGen, msg.gen) {
			return m, nil
		}
		m.progressDone = msg.done
		m.progressTotal = msg.total
		return m, nil

	case s3DownloadResultMsg:
		return m, m.handleTransferResult(msg.gen, msg.filename, msg.success, msg.err, false)

	case s3UploadResultMsg:
		return m, m.handleTransferResult(msg.gen, msg.filename, msg.success, msg.err, true)

	case s3MkdirResultMsg:
		m.loading = false
		m.localOp = false
		if msg.success {
			m.setStatus(fmt.Sprintf(i18n.T("s3.dir_created"), msg.name))
		} else {
			m.setStatus(fmt.Sprintf(i18n.T("s3.mkdir_failed"), msg.err.Error()))
		}
		m.mode = s3Browse
		m.inputBuffer = ""
		return m, fetchS3EntriesCmd(m.client, m.cwd)

	case s3RenameResultMsg:
		m.loading = false
		m.localOp = false
		if msg.success {
			m.setStatus(fmt.Sprintf(i18n.T("s3.rename_success"), msg.oldName, msg.newName))
		} else {
			m.setStatus(fmt.Sprintf(i18n.T("s3.rename_failed"), msg.err.Error()))
		}
		m.mode = s3Browse
		m.inputBuffer = ""
		return m, fetchS3EntriesCmd(m.client, m.cwd)

	case s3DeleteResultMsg:
		m.loading = false
		m.localOp = false
		if msg.success {
			m.setStatus(fmt.Sprintf(i18n.T("s3.delete_success"), msg.filename))
		} else {
			m.setStatus(fmt.Sprintf(i18n.T("s3.delete_failed"), msg.err.Error()))
		}
		m.mode = s3Browse
		return m, fetchS3EntriesCmd(m.client, m.cwd)

	case s3StatusExpiredMsg:
		if msg.gen == m.statusGen && time.Now().After(m.statusExpiry) {
			m.statusMsg = ""
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	if m.focusLocal {
		m.localTbl, cmd = m.localTbl.Update(msg)
	} else {
		m.table, cmd = m.table.Update(msg)
	}
	return m, cmd
}

func (m *s3FormModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searchMode {
		return m.handleSearchKeys(msg)
	}

	if m.showInfo {
		switch msg.String() {
		case "esc", "q", "i", "enter":
			m.showInfo = false
			m.entryInfo = nil
			return m, nil
		case "up", "k":
			if m.infoScroll > 0 {
				m.infoScroll--
			}
			return m, nil
		case "down", "j":
			m.infoScroll++
			return m, nil
		}
		return m, nil
	}

	switch m.mode {
	case s3MkdirInput:
		return m.handleMkdirInput(msg)
	case s3RenameInput:
		return m.handleRenameInput(msg)
	case s3DeleteConfirm, s3DownloadConfirm:
		return m.handleConfirmDialog(msg)
	case s3Error:
		if msg.String() == "esc" || msg.String() == "enter" || msg.String() == "q" || msg.String() == "ctrl+c" {
			if m.client != nil {
				_ = m.client.Close()
				m.client = nil
			}
			return m, func() tea.Msg { return s3DoneMsg{} }
		}
		return m, nil
	}

	switch msg.String() {
	case "esc", "q", "ctrl+c":
		if m.transferring {
			m.cancelTransfer()
			return m, nil
		}
		if msg.String() == "esc" && m.focusLocal {
			m.setFocusLocal(false)
			return m, nil
		}
		if m.client != nil {
			_ = m.client.Close()
			m.client = nil
		}
		return m, func() tea.Msg { return s3DoneMsg{} }

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
		m.mode = s3MkdirInput
		m.inputBuffer = ""
		m.inputPrompt = i18n.T("s3.mkdir_prompt")
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
		m.table.Blur()
		m.localTbl.Blur()
		return m, textinput.Blink

	case "r":
		if m.focusLocal {
			m.refreshLocal()
			m.setStatus(i18n.T("s3.refreshed"))
			return m, nil
		}
		if m.client != nil {
			m.refreshing = true
			m.setStatus(i18n.T("s3.refreshing"))
			return m, fetchS3EntriesCmd(m.client, m.cwd)
		}
		return m, nil
	}

	var cmd tea.Cmd
	if m.focusLocal {
		m.localTbl, cmd = m.localTbl.Update(msg)
	} else {
		m.table, cmd = m.table.Update(msg)
	}
	return m, cmd
}

func (m *s3FormModel) busyStatus() tea.Cmd {
	return m.setStatus(i18n.T("s3.busy"))
}

func (m *s3FormModel) handleSearchKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (m *s3FormModel) refocusTable() {
	if m.focusLocal {
		m.localTbl.Focus()
		m.table.Blur()
	} else {
		m.table.Focus()
		m.localTbl.Blur()
	}
}

func (m *s3FormModel) setFocusLocal(local bool) {
	m.focusLocal = local
	if local {
		m.mode = s3LocalBrowse
		m.localTbl.Focus()
		m.table.Blur()
	} else {
		m.mode = s3Browse
		m.table.Focus()
		m.localTbl.Blur()
	}
	m.updateRemoteRows()
	m.updateLocalRows()
	m.clampActiveCursor()
}

func (m *s3FormModel) toggleLayout() (tea.Model, tea.Cmd) {
	if m.narrow() {
		return m, m.setStatus(i18n.T("s3.too_narrow"))
	}
	if m.layout == config.S3LayoutSingle {
		m.layout = config.S3LayoutDual
	} else {
		m.layout = config.S3LayoutSingle
	}
	m.updateRemoteRows()
	m.updateLocalRows()
	m.clampActiveCursor()
	persistS3Layout(m.layout)
	name := i18n.T("s3.layout_dual")
	if m.layout == config.S3LayoutSingle {
		name = i18n.T("s3.layout_single")
	}
	return m, m.setStatus(fmt.Sprintf(i18n.T("s3.layout_status"), name))
}

func persistS3Layout(layout config.S3Layout) {
	cfg, err := config.LoadAppConfig()
	if err != nil || cfg == nil {
		fallback := config.GetDefaultAppConfig()
		cfg = &fallback
	}
	cfg.S3Layout = config.NormalizeS3Layout(layout)
	_ = config.SaveAppConfig(cfg)
}

func (m *s3FormModel) handleLocalEnter() (tea.Model, tea.Cmd) {
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
	if m.cwd == "" {
		return m, m.setStatus(i18n.T("s3.upload_need_bucket"))
	}
	name := filepath.Base(f)
	job := s3TransferJob{
		filename:   name,
		localPath:  f,
		remotePath: path.Join(m.cwd, name),
		isUpload:   true,
	}
	return m, m.requestTransfer(job)
}

func (m *s3FormModel) handleLocalUp() tea.Cmd {
	return m.localUp()
}

func (m *s3FormModel) localUp() tea.Cmd {
	parent := filepath.Dir(m.localCwd)
	if parent != m.localCwd {
		m.localCwd = parent
		m.refreshLocal()
	}
	return nil
}

func (m *s3FormModel) handleRemoteEnter() (tea.Model, tea.Cmd) {
	e := m.selectedRemote()
	if e == nil {
		return m, nil
	}
	if e.IsDir {
		next := path.Join(m.cwd, e.Name)
		m.loading = true
		return m, fetchS3EntriesCmd(m.client, next)
	}

	job := s3TransferJob{
		filename:   e.Name,
		localPath:  filepath.Join(m.localCwd, e.Name),
		remotePath: path.Join(m.cwd, e.Name),
		isUpload:   false,
	}
	if m.transferring {
		return m, m.requestTransfer(job)
	}
	m.selected = e
	m.mode = s3DownloadConfirm
	return m, nil
}

func (m *s3FormModel) handleRemoteOpenDir() (tea.Model, tea.Cmd) {
	e := m.selectedRemote()
	if e == nil || !e.IsDir {
		return m, nil
	}
	m.loading = true
	return m, fetchS3EntriesCmd(m.client, path.Join(m.cwd, e.Name))
}

func (m *s3FormModel) handleLocalOpenDir() (tea.Model, tea.Cmd) {
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

func (m *s3FormModel) handleRemoteUp() tea.Cmd {
	return m.remoteUp()
}

func (m *s3FormModel) remoteUp() tea.Cmd {
	if m.cwd == "" {
		return nil
	}
	clean := strings.Trim(m.cwd, "/")
	if clean == m.site.Bucket && m.site.Bucket != "" {
		return nil
	}
	var parent string
	if strings.Contains(clean, "/") {
		parent = path.Dir(clean)
	} else {
		if m.site.Bucket != "" {
			return nil
		}
		parent = ""
	}
	m.loading = true
	return fetchS3EntriesCmd(m.client, parent)
}

func (m *s3FormModel) handleShowInfo() (tea.Model, tea.Cmd) {
	if m.focusLocal {
		f := m.selectedLocal()
		if f == "" {
			return m, nil
		}
		fi, err := os.Stat(f)
		if err != nil {
			return m, nil
		}
		m.entryInfo = &s3EntryInfo{
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
	m.entryInfo = &s3EntryInfo{
		name:    e.Name,
		isDir:   e.IsDir,
		size:    e.Size,
		modTime: e.ModTime,
		path:    path.Join(m.cwd, e.Name),
		etag:    e.ETag,
	}
	m.showInfo = true
	m.infoScroll = 0
	return m, nil
}

func (m *s3FormModel) startDeleteConfirm() (tea.Model, tea.Cmd) {
	if m.focusLocal {
		f := m.selectedLocal()
		if f == "" {
			return m, nil
		}
		m.localOp = true
		m.pendingLocalPath = f
		m.mode = s3DeleteConfirm
		return m, nil
	}
	e := m.selectedRemote()
	if e == nil {
		return m, nil
	}
	m.localOp = false
	m.selected = e
	m.mode = s3DeleteConfirm
	return m, nil
}

func (m *s3FormModel) handleConfirmDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "y", "Y", "enter":
		if m.mode == s3DownloadConfirm {
			m.mode = s3Browse
			if m.selected != nil {
				job := s3TransferJob{
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
		if m.mode == s3DeleteConfirm {
			m.mode = s3Browse
			if m.localOp {
				p := m.pendingLocalPath
				m.pendingLocalPath = ""
				m.localOp = false
				_ = os.RemoveAll(p)
				m.refreshLocal()
				m.setStatus(fmt.Sprintf(i18n.T("s3.delete_success"), filepath.Base(p)))
				return m, nil
			}
			if m.client != nil && m.selected != nil {
				e := *m.selected
				m.selected = nil
				m.loading = true
				client := m.client
				remoteTarget := path.Join(m.cwd, e.Name)
				return m, func() tea.Msg {
					var err error
					if e.IsDir && m.cwd != "" {
						err = client.DeletePrefix(context.Background(), remoteTarget)
					} else {
						err = client.Delete(context.Background(), remoteTarget)
					}
					return s3DeleteResultMsg{filename: e.Name, success: err == nil, err: err}
				}
			}
			return m, nil
		}
	case "n", "N", "esc", "q":
		m.mode = s3Browse
		m.selected = nil
		m.pendingLocalPath = ""
		m.localOp = false
		return m, nil
	}
	return m, nil
}

func (m *s3FormModel) handleMkdirInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		name := strings.TrimSpace(m.inputBuffer)
		m.inputBuffer = ""
		m.mode = s3Browse
		if name == "" {
			return m, nil
		}
		if m.localOp {
			m.localOp = false
			_ = os.MkdirAll(filepath.Join(m.localCwd, name), 0755)
			m.refreshLocal()
			m.setStatus(fmt.Sprintf(i18n.T("s3.dir_created"), name))
			return m, nil
		}
		m.loading = true
		remoteTarget := path.Join(m.cwd, name)
		client := m.client
		return m, func() tea.Msg {
			err := client.Mkdir(context.Background(), remoteTarget)
			return s3MkdirResultMsg{name: name, success: err == nil, err: err}
		}
	case tea.KeyEsc:
		m.mode = s3Browse
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

func (m *s3FormModel) startRenameInput() (tea.Model, tea.Cmd) {
	if m.focusLocal {
		f := m.selectedLocal()
		if f == "" {
			return m, nil
		}
		m.localOp = true
		m.pendingLocalPath = f
		m.mode = s3RenameInput
		m.inputBuffer = filepath.Base(f)
		m.inputPrompt = i18n.T("s3.rename_prompt")
		return m, nil
	}
	if m.cwd == "" {
		return m, m.setStatus(i18n.T("s3.rename_bucket_unsupported"))
	}
	e := m.selectedRemote()
	if e == nil {
		return m, nil
	}
	m.localOp = false
	m.selected = e
	m.mode = s3RenameInput
	m.inputBuffer = e.Name
	m.inputPrompt = i18n.T("s3.rename_prompt")
	return m, nil
}

func (m *s3FormModel) handleRenameInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		newName := strings.Trim(strings.TrimSpace(m.inputBuffer), "/")
		m.inputBuffer = ""
		m.mode = s3Browse
		if newName == "" {
			return m, nil
		}
		if m.localOp {
			p := m.pendingLocalPath
			m.pendingLocalPath = ""
			m.localOp = false
			oldName := filepath.Base(p)
			if oldName == newName {
				return m, nil
			}
			dest := filepath.Join(filepath.Dir(p), newName)
			if err := os.Rename(p, dest); err != nil {
				m.setStatus(fmt.Sprintf(i18n.T("s3.rename_failed"), err.Error()))
				return m, nil
			}
			m.refreshLocal()
			m.setStatus(fmt.Sprintf(i18n.T("s3.rename_success"), oldName, newName))
			return m, nil
		}
		if m.selected != nil {
			oldName := m.selected.Name
			isDir := m.selected.IsDir
			m.selected = nil
			if oldName == newName {
				return m, nil
			}
			m.loading = true
			oldPath := path.Join(m.cwd, oldName)
			newPath := path.Join(m.cwd, newName)
			client := m.client
			return m, func() tea.Msg {
				var err error
				if isDir {
					err = client.RenamePrefix(context.Background(), oldPath, newPath)
				} else {
					err = client.Rename(context.Background(), oldPath, newPath)
				}
				return s3RenameResultMsg{oldName: oldName, newName: newName, success: err == nil, err: err}
			}
		}
		return m, nil
	case tea.KeyEsc:
		m.mode = s3Browse
		m.inputBuffer = ""
		m.pendingLocalPath = ""
		m.localOp = false
		m.selected = nil
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

func (m *s3FormModel) requestTransfer(job s3TransferJob) tea.Cmd {
	next, startNow := m.queue.startOrEnqueue(job)
	if startNow {
		return m.beginJob(next)
	}
	cur, count := m.queue.position()
	m.setStatus(fmt.Sprintf("Queued: %s [%d/%d]", job.filename, cur, count))
	return nil
}

func (m *s3FormModel) beginJob(job s3TransferJob) tea.Cmd {
	m.transferring = true
	m.progressGen++
	gen := m.progressGen
	m.progressDone = 0
	m.progressTotal = 0
	m.progressFile = job.filename

	ctx, cancel := context.WithCancel(context.Background())
	m.transferCancel = cancel

	client := m.client
	return func() tea.Msg {
		onProgress := func(done, total int64) {
			// Progress reports could trigger tea.Program messages
		}
		if job.isUpload {
			err := client.UploadPath(ctx, job.localPath, job.remotePath, onProgress)
			return s3UploadResultMsg{gen: gen, filename: job.filename, success: err == nil, err: err}
		}
		err := client.DownloadPath(ctx, job.remotePath, job.localPath, onProgress)
		return s3DownloadResultMsg{gen: gen, filename: job.filename, success: err == nil, err: err}
	}
}

func (m *s3FormModel) handleTransferResult(gen int, filename string, success bool, err error, isUpload bool) tea.Cmd {
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
			m.setStatus(fmt.Sprintf(i18n.T("s3.upload_success"), filename, m.cwd))
			return fetchS3EntriesCmd(m.client, m.cwd)
		}
		m.setStatus(fmt.Sprintf(i18n.T("s3.download_success"), filename, filepath.Join(m.localCwd, filename)))
		m.refreshLocal()
		return nil
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			m.setStatus(fmt.Sprintf(i18n.T("s3.transfer_cancelled"), filename))
		} else {
			m.setStatus(fmt.Sprintf(i18n.T("s3.transfer_failed"), err.Error()))
		}
	}
	return nil
}

func (m *s3FormModel) cancelTransfer() {
	if m.transferCancel != nil {
		m.transferCancel()
		m.transferCancel = nil
	}
	m.queue.clear()
	m.transferring = false
	m.setStatus(i18n.T("s3.transfer_cancelled"))
}

func (m *s3FormModel) setStatus(s string) tea.Cmd {
	m.statusMsg = s
	m.statusExpiry = time.Now().Add(4 * time.Second)
	m.statusGen++
	gen := m.statusGen
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg {
		return s3StatusExpiredMsg{gen: gen}
	})
}

func (m *s3FormModel) statusActive() bool {
	return m.statusMsg != "" && time.Now().Before(m.statusExpiry)
}

func (m *s3FormModel) sortEntries() {
	sort.Slice(m.entries, func(i, j int) bool {
		if m.entries[i].IsDir != m.entries[j].IsDir {
			return m.entries[i].IsDir
		}
		return strings.ToLower(m.entries[i].Name) < strings.ToLower(m.entries[j].Name)
	})
}

func (m *s3FormModel) refreshLocal() {
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

func (m *s3FormModel) updateRemoteRows() {
	cols := m.remoteColumns()
	m.table.SetColumns(cols)
	var rows []table.Row
	for _, e := range m.filteredRemote() {
		name := e.Name
		kind := i18n.T("sftp.type_file")
		sz := formatSize(e.Size)
		if e.IsDir {
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
	m.table.SetRows(rows)
}

func (m *s3FormModel) updateLocalRows() {
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

func (m *s3FormModel) clampActiveCursor() {
	if m.focusLocal {
		m.clampTableCursor(&m.localTbl, len(m.filteredLocal()))
	} else {
		m.clampTableCursor(&m.table, len(m.filteredRemote()))
	}
}

func (m *s3FormModel) clampTableCursor(t *table.Model, count int) {
	if count == 0 {
		return
	}
	if t.Cursor() >= count {
		t.SetCursor(count - 1)
	} else if t.Cursor() < 0 {
		t.SetCursor(0)
	}
}

func (m *s3FormModel) selectedRemote() *s3client.RemoteEntry {
	rows := m.filteredRemote()
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(rows) {
		return nil
	}
	return &rows[idx]
}

func (m *s3FormModel) selectedLocal() string {
	rows := m.filteredLocal()
	idx := m.localTbl.Cursor()
	if idx < 0 || idx >= len(rows) {
		return ""
	}
	return rows[idx]
}

func (m *s3FormModel) filteredRemote() []s3client.RemoteEntry {
	q := strings.ToLower(strings.TrimSpace(m.searchInput.Value()))
	if q == "" || m.focusLocal {
		return m.entries
	}
	words := strings.Fields(q)
	var out []s3client.RemoteEntry
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

func (m *s3FormModel) filteredLocal() []string {
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

func (m *s3FormModel) rebuildTables() {
	h := m.paneTableHeight()
	m.table = table.New(
		table.WithColumns(m.remoteColumns()),
		table.WithFocused(!m.focusLocal),
		table.WithHeight(h),
	)
	m.localTbl = table.New(
		table.WithColumns(m.localColumns()),
		table.WithFocused(m.focusLocal),
		table.WithHeight(h),
	)
	m.updateRemoteRows()
	m.updateLocalRows()
	m.clampActiveCursor()
}

func (m *s3FormModel) paneTableHeight() int {
	overhead := 11
	if m.height < 20 {
		overhead = 9
	}
	if m.mode == s3MkdirInput || m.mode == s3RenameInput || m.mode == s3DownloadConfirm ||
		m.statusActive() || m.transferring || m.refreshing {
		overhead++
	}
	h := m.height - overhead
	if h < 3 {
		h = 3
	}
	return h
}

// paneContentWidth is the bubbles-table column budget inside Width(paneWidth) content.
func (m *s3FormModel) paneContentWidth() int {
	// 3 cells * Padding(0,1)=2 → 6 chrome inside the bordered content box.
	w := m.paneWidth() - 6
	if w < 12 {
		return 12
	}
	return w
}

func (m *s3FormModel) remoteColumns() []table.Column {
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

func (m *s3FormModel) localColumns() []table.Column {
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

func (m *s3FormModel) narrow() bool {
	return m.width > 0 && m.width < s3NarrowWidth
}

func (m *s3FormModel) singlePane() bool {
	return m.layout == config.S3LayoutSingle || m.narrow()
}

func (m *s3FormModel) focusLabel(local bool) string {
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

func (m *s3FormModel) paneWidth() int {
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

func (m *s3FormModel) renderInfoView() string {
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

	titleText := m.styles.Header.Render(strings.TrimSpace(fmt.Sprintf(i18n.T("s3.entry_info_title"), info.name)))

	rows := [][2]string{
		{i18n.T("sftp.col_name") + ":", info.name},
		{i18n.T("sftp.col_type") + ":", kind},
		{i18n.T("sftp.col_size") + ":", size},
		{i18n.T("sftp.col_modified") + ":", mod},
		{i18n.T("s3.info_path") + ":", info.path},
	}
	if info.etag != "" {
		rows = append(rows, [2]string{"ETag:", info.etag})
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
	helpText := m.styles.HelpText.Width(innerW).Render(i18n.T("s3.browser_info_help"))
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

func (m *s3FormModel) renderErrorView() string {
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
	content := fmt.Sprintf(i18n.T("s3.err_session"), detail)
	return renderFormPage(m.styles, m.width, errStyle.Render(content))
}

func (m *s3FormModel) renderDeleteConfirmBox() string {
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
	confirmKey := "s3.delete_confirm"
	if isDir {
		confirmKey = "s3.delete_dir_confirm"
	}
	return renderConfirmBox(m.styles, m.width,
		m.styles.ErrorText.Render(i18n.T("delete.title")),
		i18n.T(confirmKey, name),
		i18n.T("delete.warning"),
		m.styles.HelpText.Render(i18n.T("delete.help")),
	)
}

func (m *s3FormModel) renderDownloadConfirmBox() string {
	if m.selected == nil {
		return ""
	}
	return renderCardBox(m.styles.FormContainer, m.width,
		m.styles.Header.Render(i18n.T("download.title")),
		i18n.T("s3.download_confirm", m.selected.Name, filepath.Join(m.localCwd, m.selected.Name)),
		m.styles.HelpText.Render(i18n.T("delete.help")),
	)
}

func (m *s3FormModel) View() string {
	th := m.paneTableHeight()
	m.localTbl.SetHeight(th)
	m.table.SetHeight(th)

	if m.mode == s3Error {
		return m.renderErrorView()
	}
	if m.loading && m.client == nil {
		inner := formPageInnerWidth(m.width)
		body := lipgloss.NewStyle().Width(inner).Render(
			m.styles.FormTitle.Render(" S3 — "+m.site.Name+" ") + "\n\n" +
				"  Connecting to S3: " + m.site.Name + "…")
		return renderFormPage(m.styles, m.width, body)
	}
	if m.showInfo && m.entryInfo != nil {
		return m.renderInfoView()
	}

	localStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	remoteStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(PrimaryColor))
	if m.focusLocal {
		localStyle = localStyle.Bold(true)
	} else {
		remoteStyle = remoteStyle.Bold(true)
	}

	headerTitle := fmt.Sprintf(" S3 — %s ", m.site.Name)
	if m.site.Endpoint != "" {
		headerTitle = fmt.Sprintf(" S3 — %s (%s) ", m.site.Name, m.site.Endpoint)
	}
	header := m.styles.Header.Render(headerTitle)

	var paths string
	var panes string
	pw := m.paneWidth()
	pathPW := m.width - 4
	if pathPW < 20 {
		pathPW = 20
	}
	displayCwd := m.cwd
	if displayCwd == "" {
		displayCwd = "(Buckets)"
	}
	if m.singlePane() {
		tableStyle := m.styles.TableFocused
		if m.searchMode {
			tableStyle = m.styles.TableUnfocused
		}
		paths = localStyle.Render(fmt.Sprintf("%s  %s", m.focusLabel(true), truncatePath(m.localCwd, pathPW-ansi.StringWidth(m.focusLabel(true))-4))) + "\n" +
			remoteStyle.Render(fmt.Sprintf("%s %s", m.focusLabel(false), truncatePath(displayCwd, pathPW-ansi.StringWidth(m.focusLabel(false))-3)))
		if m.focusLocal {
			panes = tableStyle.Width(pw).Render(m.localTbl.View())
		} else {
			panes = tableStyle.Width(pw).Render(m.table.View())
		}
	} else {
		paths = localStyle.Render(fmt.Sprintf("%s  %s", m.focusLabel(true), truncatePath(m.localCwd, pathPW-ansi.StringWidth(m.focusLabel(true))-4))) + "\n" +
			remoteStyle.Render(fmt.Sprintf("%s %s", m.focusLabel(false), truncatePath(displayCwd, pathPW-ansi.StringWidth(m.focusLabel(false))-3)))
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
		remoteBox := remoteBoxStyle.Width(pw).Render(m.table.View())
		panes = lipgloss.JoinHorizontal(lipgloss.Top, localBox, "  ", remoteBox)
	}

	var extras []string
	if m.transferring || m.refreshing {
		cur, total := m.queue.position()
		prog := formatS3Progress(m.queue.current(), m.progressDone, m.progressTotal, cur, total)
		if !m.transferring && m.refreshing {
			prog = i18n.T("s3.refreshing")
		}
		progressStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("36"))
		extras = append(extras, progressStyle.Render(" ⏳ "+prog))
	} else if m.statusActive() {
		extras = append(extras, renderStatusToast(m.statusMsg))
	}

	if m.mode == s3MkdirInput || m.mode == s3RenameInput {
		extras = append(extras, lipgloss.NewStyle().Foreground(lipgloss.Color(PrimaryColor)).Render(
			fmt.Sprintf("  %s %s_", m.inputPrompt, m.inputBuffer)))
	}

	var helpParts []string
	if m.height < 20 {
		if m.searchMode {
			helpParts = append(helpParts, i18n.T("s3.help_remote_2"))
		} else if m.focusLocal {
			helpParts = append(helpParts, i18n.T("s3.help_local_1"))
		} else {
			helpParts = append(helpParts, i18n.T("s3.help_remote_1"))
		}
	} else {
		if m.searchMode {
			helpParts = append(helpParts, i18n.T("s3.help_remote_2"))
		} else if m.focusLocal {
			helpParts = append(helpParts, i18n.T("s3.help_local_1"), i18n.T("s3.help_local_2"), i18n.T("s3.help_local_3"))
		} else {
			helpParts = append(helpParts, i18n.T("s3.help_remote_1"), i18n.T("s3.help_remote_2"), i18n.T("s3.help_remote_3"))
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
	case s3DeleteConfirm:
		return renderConfirmModal(m.width, m.height, m.renderDeleteConfirmBox())
	case s3DownloadConfirm:
		if box := m.renderDownloadConfirmBox(); box != "" {
			return renderConfirmModal(m.width, m.height, box)
		}
	}
	return base
}
