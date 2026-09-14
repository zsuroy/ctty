package ui

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/telnetconfig"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// telnetFormModel manages the telnet device list view.
// It shows saved telnet hosts and lets the user connect, add,
// edit, or delete entries.
type telnetFormModel struct {
	styles          Styles
	width           int
	height          int
	table           table.Model
	hosts           []telnetconfig.TelnetHost
	filtered        []telnetconfig.TelnetHost
	mode            telnetMode
	addForm         *telnetAddFormModel
	deleteIndex     int
	infoIndex       int
	infoScroll      int
	probing         bool
	status          map[string]bool
	latencies       map[string]time.Duration
	ready           bool
	searchInput     textinput.Model
	searchMode      bool
	statusMessage   string
	statusExpiry    time.Time
	tagPickerOpen   bool
	tagPickerCursor int
	selectedTag     string
	selectedHosts   map[string]bool
}

func (m *telnetFormModel) setStatus(msg string) {
	m.statusMessage = msg
	m.statusExpiry = time.Now().Add(3 * time.Second)
}

func (m *telnetFormModel) statusActive() bool {
	return m.statusMessage != "" && time.Now().Before(m.statusExpiry)
}

func telnetHasTag(h telnetconfig.TelnetHost, targetTag string) bool {
	cleanTarget := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(targetTag), "#"))
	if cleanTarget == "" {
		return false
	}
	for _, t := range h.Tags {
		clean := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(t), "#"))
		if clean == cleanTarget {
			return true
		}
	}
	return false
}

type telnetTagCountItem struct {
	tag   string
	count int
}

func (m *telnetFormModel) getTagCounts() []telnetTagCountItem {
	counts := make(map[string]int)
	for _, h := range m.hosts {
		for _, t := range h.Tags {
			clean := strings.TrimSpace(strings.TrimPrefix(t, "#"))
			if clean == "" {
				continue
			}
			counts[clean]++
		}
	}
	items := make([]telnetTagCountItem, 0, len(counts))
	for tag, count := range counts {
		items = append(items, telnetTagCountItem{tag: tag, count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count != items[j].count {
			return items[i].count > items[j].count
		}
		return items[i].tag < items[j].tag
	})
	return items
}

func (m *telnetFormModel) isHostSelected(name string) bool {
	return m.selectedHosts != nil && m.selectedHosts[name]
}

func (m *telnetFormModel) toggleHostSelected(name string) {
	if m.selectedHosts == nil {
		m.selectedHosts = make(map[string]bool)
	}
	if m.selectedHosts[name] {
		delete(m.selectedHosts, name)
	} else {
		m.selectedHosts[name] = true
	}
}

func (m *telnetFormModel) clearSelection() {
	m.selectedHosts = make(map[string]bool)
}

func (m *telnetFormModel) toggleSelectAllVisible() {
	if m.selectedHosts == nil {
		m.selectedHosts = make(map[string]bool)
	}
	if len(m.filtered) == 0 {
		return
	}
	allSelected := true
	for _, h := range m.filtered {
		if !m.selectedHosts[h.Name] {
			allSelected = false
			break
		}
	}
	if allSelected {
		for _, h := range m.filtered {
			delete(m.selectedHosts, h.Name)
		}
	} else {
		for _, h := range m.filtered {
			m.selectedHosts[h.Name] = true
		}
	}
}

func (m *telnetFormModel) getSelectedHosts() []telnetconfig.TelnetHost {
	if len(m.selectedHosts) == 0 {
		return nil
	}
	var res []telnetconfig.TelnetHost
	for _, h := range m.hosts {
		if m.selectedHosts[h.Name] {
			res = append(res, h)
		}
	}
	return res
}

func (m *telnetFormModel) startProbeSelectedCmd() tea.Cmd {
	if len(m.selectedHosts) == 0 {
		return m.startProbeCmd()
	}
	var cmds []tea.Cmd
	for _, h := range m.hosts {
		if m.selectedHosts[h.Name] {
			cmds = append(cmds, probeTelnetHostCmd(h))
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m *telnetFormModel) renderTagPicker() string {
	tagItems := m.getTagCounts()
	title := m.styles.FocusedLabel.Bold(true).Render("🏷️  " + i18n.T("tags.title"))
	var rows []string
	rows = append(rows, title, "")
	totalCount := len(m.hosts)
	allCount := fmt.Sprintf("(%d)", totalCount)
	cursor0 := "  "
	if m.tagPickerCursor == 0 {
		cursor0 = "> "
	}
	activeMark0 := "○"
	if m.selectedTag == "" {
		activeMark0 = "●"
	}
	line0 := fmt.Sprintf("%s%s [%s]  %s", cursor0, activeMark0, i18n.T("tags.all"), allCount)
	if m.tagPickerCursor == 0 {
		line0 = m.styles.Selected.Render(line0)
	}
	rows = append(rows, line0)
	for i, item := range tagItems {
		idx := i + 1
		cursor := "  "
		if m.tagPickerCursor == idx {
			cursor = "> "
		}
		activeMark := "○"
		if m.selectedTag == item.tag {
			activeMark = "●"
		}
		shortcut := ""
		if idx <= 9 {
			shortcut = fmt.Sprintf("%d. ", idx)
		}
		tagFormatted := FormatColoredTags([]string{item.tag})
		line := fmt.Sprintf("%s%s %s%s (%d)", cursor, activeMark, shortcut, tagFormatted, item.count)
		if m.tagPickerCursor == idx {
			line = m.styles.Selected.Render(line)
		}
		rows = append(rows, line)
	}
	rows = append(rows, "", m.styles.HelpText.Render(i18n.T("tags.help")))
	return renderCardBox(m.styles.FormContainer, m.width, rows...)
}

type telnetMode int

const (
	telnetList telnetMode = iota
	telnetAdd
	telnetEdit
	telnetDeleteConfirm
	telnetInfo
)

// telnetConnectMsg tells the parent model to suspend the TUI and connect.
type telnetConnectMsg struct {
	host telnetconfig.TelnetHost
}

// telnetDoneMsg tells the parent model to return to the SSH host list.
type telnetDoneMsg struct{}

// NewTelnetForm creates the telnet device list view.
func NewTelnetForm(styles Styles, width, height int) *telnetFormModel {
	m := &telnetFormModel{
		styles: styles,
		width:  width,
		height: height,
	}

	return continueTelnetFormInit(m)
}

// telnetProbeMsg reports the TCP reachability and latency of one telnet host.
type telnetProbeMsg struct {
	name     string
	up       bool
	duration time.Duration
}

// probeTelnetHostCmd dials the host with a short timeout; the result
// updates the status indicator in the device list.
func probeTelnetHostCmd(h telnetconfig.TelnetHost) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		addr := net.JoinHostPort(h.Host, strconv.Itoa(h.Port))
		conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
		dur := time.Since(start)
		if err == nil {
			_ = conn.Close()
		}
		return telnetProbeMsg{name: h.Name, up: err == nil, duration: dur}
	}
}

// startProbeCmd fans out one dial per saved host.
func (m *telnetFormModel) startProbeCmd() tea.Cmd {
	var cmds []tea.Cmd
	for _, h := range m.hosts {
		cmds = append(cmds, probeTelnetHostCmd(h))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// continueTelnetFormInit finishes constructing the telnet form.
func continueTelnetFormInit(m *telnetFormModel) *telnetFormModel {
	m.searchInput = textinput.New()
	m.searchInput.Placeholder = i18n.T("telnet.search_placeholder")
	m.searchInput.CharLimit = 50
	m.searchInput.Width = searchInputWidth(m.width, i18n.T("search.prompt"))

	m.loadHosts()
	m.filtered = m.hosts
	m.buildTable()
	m.mode = telnetList
	m.ready = true
	return m
}

func (m *telnetFormModel) loadHosts() {
	hosts, err := telnetconfig.Load()
	if err != nil {
		hosts = []telnetconfig.TelnetHost{}
	}
	m.hosts = hosts
}

func (m *telnetFormModel) getColumns() []table.Column {
	w := m.width
	if w <= 0 {
		w = 80
	}

	// Custom renderTable uses renderCell (no per-cell padding).
	// TableFocused border (2) + App padding (2) = 4, matching the search bar.
	portW := 6
	if w < 60 {
		rem := w - 4 - portW
		if rem < 8 {
			rem = 8
		}
		nameW := rem / 2
		hostW := rem - nameW
		return []table.Column{
			{Title: i18n.T("telnet.col_name"), Width: nameW},
			{Title: i18n.T("telnet.col_host"), Width: hostW},
			{Title: i18n.T("telnet.col_port"), Width: portW},
		}
	}
	rem := w - 4 - portW
	if rem < 12 {
		rem = 12
	}
	nameW := rem * 2 / 5
	hostW := rem * 2 / 5
	if nameW < 8 {
		nameW = 8
	}
	if hostW < 8 {
		hostW = 8
	}
	tagW := rem - nameW - hostW
	if tagW < 6 {
		tagW = 6
		hostW = rem - nameW - tagW
	}
	return []table.Column{
		{Title: i18n.T("telnet.col_name"), Width: nameW},
		{Title: i18n.T("telnet.col_host"), Width: hostW},
		{Title: i18n.T("telnet.col_port"), Width: portW},
		{Title: i18n.T("table.col.tags"), Width: tagW},
	}
}

func (m *telnetFormModel) hostDisplayName(h telnetconfig.TelnetHost) string {
	prefix := "[ ] "
	if m.isHostSelected(h.Name) {
		prefix = "[✓] "
	}
	name := h.Name
	if st, ok := m.status[h.Name]; ok {
		if st {
			if dur, exists := m.latencies[h.Name]; exists {
				ms := dur.Milliseconds()
				if ms < 50 {
					name = "🟢 " + name
				} else if ms < 150 {
					name = "🟡 " + name
				} else {
					name = "🟠 " + name
				}
			} else {
				name = "🟢 " + name
			}
		} else {
			name = "🔴 " + name
		}
	} else if m.probing {
		name = "🔵 " + name
	} else {
		name = "⚫ " + name
	}
	return prefix + name
}

func (m *telnetFormModel) buildTable() {
	columns := m.getColumns()

	savedCursor := m.table.Cursor()
	hasTable := m.table.Columns() != nil && len(m.table.Columns()) > 0

	rows := []table.Row{}
	for _, h := range m.filtered {
		name := m.hostDisplayName(h)
		if len(columns) == 3 {
			rows = append(rows, table.Row{name, h.Host, strconv.Itoa(h.Port)})
		} else {
			rows = append(rows, table.Row{name, h.Host, strconv.Itoa(h.Port), FormatPlainTags(h.Tags)})
		}
	}
	if len(rows) == 0 {
		emptyRow := make(table.Row, len(columns))
		emptyRow[0] = i18n.T("telnet.no_hosts")
		rows = append(rows, emptyRow)
	}

	s := table.DefaultStyles()
	s.Selected = m.styles.Selected
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(m.styles.Theme.Primary).
		BorderBottom(true).
		Bold(false)

	availHeight := m.height - 9
	if m.height < 20 {
		availHeight = m.height - 7
	}
	if m.height >= 18 && m.width >= 64 {
		availHeight--
	}
	if availHeight < 2 {
		availHeight = 2
	}
	tableHeight := 1 + len(rows)
	if tableHeight > availHeight {
		tableHeight = availHeight
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(tableHeight),
		table.WithStyles(s),
	)
	m.table = t
	if hasTable {
		maxCursor := len(m.filtered) - 1
		if maxCursor < 0 {
			maxCursor = 0
		}
		if savedCursor < 0 {
			savedCursor = 0
		}
		if savedCursor > maxCursor {
			savedCursor = maxCursor
		}
		m.table.SetCursor(savedCursor)
	}
}

func (m *telnetFormModel) refreshTable() {
	cursor := m.table.Cursor()
	m.loadHosts()
	m.filterHosts()
	m.buildTable()
	if cursor >= m.table.Height()-1 && cursor > 0 {
		cursor = m.table.Height() - 2
	}
	if cursor < 0 {
		cursor = 0
	}
	m.table.SetCursor(cursor)
}

// filterHosts filters the host list by the current search input.
func (m *telnetFormModel) filterHosts() {
	var base []telnetconfig.TelnetHost
	if m.selectedTag != "" {
		for _, h := range m.hosts {
			if telnetHasTag(h, m.selectedTag) {
				base = append(base, h)
			}
		}
	} else {
		base = m.hosts
	}
	query := strings.ToLower(m.searchInput.Value())
	if query == "" {
		m.filtered = base
		return
	}
	filtered := make([]telnetconfig.TelnetHost, 0, len(base))
	for _, h := range base {
		if strings.Contains(strings.ToLower(h.Name), query) ||
			strings.Contains(strings.ToLower(h.Host), query) ||
			strings.Contains(strings.ToLower(FormatPlainTags(h.Tags)), query) {
			filtered = append(filtered, h)
		}
	}
	m.filtered = filtered
}

// Title returns the view title for breadcrumb display.
func (m *telnetFormModel) Title() string {
	return "Telnet Connections"
}

func (m *telnetFormModel) Init() tea.Cmd {
	return nil
}

func (m *telnetFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.styles = NewStyles(m.width)
		m.searchInput.Width = searchInputWidth(m.width, i18n.T("search.prompt"))
		m.buildTable()
		if m.addForm != nil {
			updated, cmd := m.addForm.Update(msg)
			if sm, ok := updated.(*telnetAddFormModel); ok {
				m.addForm = sm
			}
			return m, cmd
		}
		return m, nil

	case telnetProbeMsg:
		if m.status == nil {
			m.status = make(map[string]bool)
		}
		if m.latencies == nil {
			m.latencies = make(map[string]time.Duration)
		}
		m.status[msg.name] = msg.up
		m.latencies[msg.name] = msg.duration
		if len(m.selectedHosts) > 0 {
			done := true
			for name := range m.selectedHosts {
				if _, ok := m.status[name]; !ok {
					done = false
					break
				}
			}
			if done {
				m.probing = false
			}
		} else if len(m.status) >= len(m.hosts) {
			m.probing = false
		}
		m.buildTable()
		return m, nil

	case tea.KeyMsg:
		if m.tagPickerOpen {
			switch msg.String() {
			case "esc", "q":
				m.tagPickerOpen = false
				return m, nil
			case "up", "k":
				if m.tagPickerCursor > 0 {
					m.tagPickerCursor--
				}
				return m, nil
			case "down", "j":
				tagItems := m.getTagCounts()
				if m.tagPickerCursor < len(tagItems) {
					m.tagPickerCursor++
				}
				return m, nil
			case "c":
				m.selectedTag = ""
				m.tagPickerOpen = false
				m.filterHosts()
				m.buildTable()
				m.setStatus(i18n.T("tags.cleared"))
				return m, nil
			case "enter":
				tagItems := m.getTagCounts()
				if m.tagPickerCursor == 0 {
					m.selectedTag = ""
				} else if m.tagPickerCursor-1 < len(tagItems) {
					m.selectedTag = tagItems[m.tagPickerCursor-1].tag
				}
				m.tagPickerOpen = false
				m.filterHosts()
				m.buildTable()
				return m, nil
			case "1", "2", "3", "4", "5", "6", "7", "8", "9":
				tagItems := m.getTagCounts()
				idx := int(msg.String()[0] - '1')
				if idx < len(tagItems) {
					m.selectedTag = tagItems[idx].tag
					m.tagPickerOpen = false
					m.filterHosts()
					m.buildTable()
					return m, nil
				}
			}
			return m, nil
		}
		switch m.mode {
		case telnetList:
			return m.handleListKeys(msg)
		case telnetAdd, telnetEdit:
			return m.handleAddKeys(msg)
		case telnetInfo:
			return m.handleInfoKeys(msg)
		case telnetDeleteConfirm:
			return m.handleDeleteConfirmKeys(msg)
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *telnetFormModel) handleListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searchMode {
		return m.handleSearchKeys(msg)
	}

	key := msg.String()
	switch key {
	case "esc", "q":
		if len(m.selectedHosts) > 0 {
			m.clearSelection()
			m.buildTable()
			m.setStatus(i18n.T("main.selection_cleared"))
			return m, nil
		}
		if m.selectedTag != "" {
			m.selectedTag = ""
			m.filterHosts()
			m.buildTable()
			m.setStatus(i18n.T("tags.cleared"))
			return m, nil
		}
		return m, func() tea.Msg { return telnetDoneMsg{} }
	case "t":
		return m, func() tea.Msg { return switchProtocolMsg{target: ViewSerial} }
	case "F", "]":
		return m, func() tea.Msg { return switchProtocolMsg{target: ViewFTP} }
	case "b":
		return m, func() tea.Msg { return switchProtocolMsg{target: ViewLocalBrowser} }
	case "[":
		return m, func() tea.Msg { return switchProtocolMsg{target: ViewSerial} }
	case "g", "home":
		m.table.SetCursor(0)
		return m, nil
	case "G", "end":
		if len(m.filtered) > 0 {
			m.table.SetCursor(len(m.filtered) - 1)
		}
		return m, nil
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(key[0] - '1')
		if idx < len(m.filtered) {
			m.table.SetCursor(idx)
		}
		return m, nil
	case " ":
		if len(m.filtered) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx < 0 || idx >= len(m.filtered) {
			return m, nil
		}
		m.toggleHostSelected(m.filtered[idx].Name)
		if idx < len(m.filtered)-1 {
			m.table.SetCursor(idx + 1)
		}
		m.buildTable()
		return m, nil
	case "ctrl+a":
		m.toggleSelectAllVisible()
		m.buildTable()
		return m, nil
	case "/", "ctrl+f":
		m.searchMode = true
		m.searchInput.Focus()
		m.table.Blur()
		return m, textinput.Blink
	case "tab":
		m.searchMode = true
		m.searchInput.Focus()
		m.table.Blur()
		return m, textinput.Blink
	case "w":
		tagItems := m.getTagCounts()
		if len(tagItems) == 0 {
			m.setStatus(i18n.T("tags.none"))
			return m, nil
		}
		m.tagPickerOpen = true
		m.tagPickerCursor = 0
		for i, it := range tagItems {
			if it.tag == m.selectedTag {
				m.tagPickerCursor = i + 1
				break
			}
		}
		return m, nil
	case "c":
		if m.selectedTag != "" {
			m.selectedTag = ""
			m.filterHosts()
			m.buildTable()
			m.setStatus(i18n.T("tags.cleared"))
			return m, nil
		}
		return m, nil
	case "p":
		if m.probing {
			return m, nil
		}
		if len(m.selectedHosts) > 0 {
			m.probing = true
			m.status = make(map[string]bool)
			m.latencies = make(map[string]time.Duration)
			m.buildTable()
			m.setStatus(fmt.Sprintf(i18n.T("main.ping_selected"), len(m.selectedHosts)))
			return m, m.startProbeSelectedCmd()
		}
		if len(m.hosts) > 0 {
			m.probing = true
			m.status = nil
			m.latencies = nil
			m.buildTable()
			m.setStatus("Probing all devices...")
			return m, m.startProbeCmd()
		}
		return m, nil
	case "enter":
		if len(m.filtered) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx >= 0 && idx < len(m.filtered) {
			dev := m.filtered[idx]
			return m, func() tea.Msg { return telnetConnectMsg{host: dev} }
		}
	case "i":
		if len(m.filtered) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx >= 0 && idx < len(m.filtered) {
			m.infoIndex = idx
			m.infoScroll = 0
			m.mode = telnetInfo
			return m, nil
		}
	case "a":
		m.addForm = newTelnetAddForm(m.styles, m.width, m.height, nil)
		m.mode = telnetAdd
		return m, m.addForm.Init()
	case "e":
		if len(m.filtered) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx >= 0 && idx < len(m.filtered) {
			m.addForm = newTelnetAddForm(m.styles, m.width, m.height, &m.filtered[idx])
			m.mode = telnetEdit
			return m, m.addForm.Init()
		}
	case "d":
		if len(m.selectedHosts) > 0 {
			m.mode = telnetDeleteConfirm
			m.deleteIndex = m.table.Cursor()
			return m, nil
		}
		if len(m.filtered) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx >= 0 && idx < len(m.filtered) {
			m.mode = telnetDeleteConfirm
			m.deleteIndex = idx
			return m, nil
		}
	case "y":
		if len(m.selectedHosts) > 0 {
			var cmds []string
			for _, h := range m.getSelectedHosts() {
				cmds = append(cmds, FormatTelnetCommand(h))
			}
			joined := strings.Join(cmds, "\n")
			copyToClipboard(joined)
			m.setStatus(fmt.Sprintf(i18n.T("main.copied"), fmt.Sprintf("%d hosts", len(cmds))))
			return m, nil
		}
		if len(m.filtered) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx >= 0 && idx < len(m.filtered) {
			cmdStr := FormatTelnetCommand(m.filtered[idx])
			copyToClipboard(cmdStr)
			m.setStatus(fmt.Sprintf(i18n.T("main.copied"), cmdStr))
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *telnetFormModel) handleSearchKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		m.filterHosts()
		m.buildTable()
	}
	return m, cmd
}

func (m *telnetFormModel) handleDeleteConfirmKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		if len(m.selectedHosts) > 0 {
			for name := range m.selectedHosts {
				_ = telnetconfig.Delete(name)
			}
			m.clearSelection()
		} else if m.deleteIndex >= 0 && m.deleteIndex < len(m.filtered) {
			h := m.filtered[m.deleteIndex]
			_ = telnetconfig.Delete(h.Name)
		}
		m.mode = telnetList
		m.refreshTable()
		return m, nil
	case "n", "N", "esc":
		m.mode = telnetList
		m.deleteIndex = -1
		return m, nil
	}
	return m, nil
}

func (m *telnetFormModel) handleInfoKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "i":
		m.mode = telnetList
		m.infoIndex = -1
		return m, nil
	case "e", "enter":
		if m.infoIndex >= 0 && m.infoIndex < len(m.filtered) {
			h := m.filtered[m.infoIndex]
			m.addForm = newTelnetAddForm(m.styles, m.width, m.height, &h)
			m.mode = telnetEdit
			m.infoIndex = -1
			return m, m.addForm.Init()
		}
	}
	if scrollInfoKey(msg.String(), &m.infoScroll) {
		return m, nil
	}
	return m, nil
}

func (m *telnetFormModel) renderInfo() string {
	if m.infoIndex < 0 || m.infoIndex >= len(m.filtered) {
		return ""
	}
	h := m.filtered[m.infoIndex]

	titleText := m.styles.Header.Render(strings.TrimSpace(i18n.T("telnet.info_title")))

	rows := [][2]string{
		{i18n.T("telnet.col_name") + ":", h.Name},
		{i18n.T("telnet.col_host") + ":", h.Host},
		{i18n.T("telnet.col_port") + ":", strconv.Itoa(h.Port)},
		{i18n.T("table.col.tags") + ":", FormatColoredTags(h.Tags)},
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
	helpText := m.styles.HelpText.Width(innerW).Render(i18n.T("telnet.help_info"))
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

	box := container.Width(boxWidth).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top, box)
}

func (m *telnetFormModel) handleAddKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	sub, cmd := m.addForm.Update(msg)
	if sm, ok := sub.(*telnetAddFormModel); ok {
		m.addForm = sm
	}
	if m.addForm.done {
		m.mode = telnetList
		m.refreshTable()
		m.addForm = nil
		return m, nil
	}
	if m.addForm.cancelled {
		m.mode = telnetList
		m.addForm = nil
		return m, nil
	}
	return m, cmd
}

func (m *telnetFormModel) View() string {
	if !m.ready {
		return fillTerminal(m.width, m.height, i18n.T("table.loading"))
	}

	if m.tagPickerOpen {
		return fillTerminal(m.width, m.height, renderConfirmModal(m.width, m.height, m.renderTagPicker()))
	}

	var content string
	switch m.mode {
	case telnetAdd, telnetEdit:
		if m.addForm != nil {
			content = m.addForm.View()
		}
	case telnetInfo:
		content = m.renderInfo()
	case telnetDeleteConfirm:
		return renderConfirmModal(m.width, m.height, m.renderDeleteConfirm())
	}
	if content == "" {
		content = m.renderList()
	}
	return fillTerminal(m.width, m.height, content)
}

func (m *telnetFormModel) renderTable() string {
	cols := m.table.Columns()
	if len(cols) == 0 {
		return m.table.View()
	}

	headerStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(m.styles.Theme.Primary).
		BorderBottom(true).
		Bold(false)

	var headerCells []string
	for _, col := range cols {
		if col.Width <= 0 {
			continue
		}
		headerCells = append(headerCells, headerStyle.Render(renderCell(col.Title, col.Width)))
	}
	headerRow := lipgloss.JoinHorizontal(lipgloss.Top, headerCells...)

	cursor := m.table.Cursor()
	hostCount := len(m.filtered)
	viewport := m.table.Height()
	if viewport < 1 {
		viewport = 1
	}

	start := 0
	if hostCount > viewport {
		if cursor >= viewport {
			start = cursor - viewport + 1
		}
		if start > hostCount-viewport {
			start = hostCount - viewport
		}
		if start < 0 {
			start = 0
		}
	}
	end := start + viewport
	if end > hostCount {
		end = hostCount
	}

	tagsCol := -1
	if len(cols) > 3 {
		tagsCol = 3
	}
	selectedBg := lipgloss.NewStyle().Background(m.styles.Theme.Primary)

	var renderedRows []string
	if hostCount == 0 {
		rowValues := make([]string, len(cols))
		rowValues[0] = i18n.T("telnet.no_hosts")
		renderedRows = append(renderedRows, m.renderTableRow(cols, rowValues, tagsCol, true, selectedBg))
	}
	for r := start; r < end; r++ {
		h := m.filtered[r]
		rowValues := []string{m.hostDisplayName(h), h.Host, strconv.Itoa(h.Port)}
		if len(cols) > 3 {
			rowValues = append(rowValues, FormatColoredTags(h.Tags))
		}
		renderedRows = append(renderedRows, m.renderTableRow(cols, rowValues, tagsCol, r == cursor, selectedBg))
	}

	tableContentWidth := 0
	for _, col := range cols {
		if col.Width > 0 {
			tableContentWidth += col.Width
		}
	}
	result := headerRow + "\n" + strings.Join(renderedRows, "\n")
	if tableContentWidth > 0 {
		var truncatedLines []string
		for _, line := range strings.Split(result, "\n") {
			truncatedLines = append(truncatedLines, ansi.Truncate(line, tableContentWidth, ""))
		}
		result = strings.Join(truncatedLines, "\n")
	}
	return result
}

func (m *telnetFormModel) renderTableRow(cols []table.Column, rowValues []string, tagsCol int, selected bool, selectedBg lipgloss.Style) string {
	var cells []string
	for i, col := range cols {
		if col.Width <= 0 {
			continue
		}
		val := ""
		if i < len(rowValues) {
			val = rowValues[i]
		}
		cell := renderCell(val, col.Width)
		if selected {
			if i == tagsCol {
				cell = selectedBg.Render(cell)
			} else {
				cell = m.styles.Selected.Render(cell)
			}
		}
		cells = append(cells, cell)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cells...)
}

func (m *telnetFormModel) renderList() string {
	components := []string{}

	components = append(components, m.styles.Header.Render(i18n.T("telnet.title")))

	if m.height >= 18 {
		if tabs := renderProtocolTabs(m.styles, "telnet", len(m.filtered), m.width); tabs != "" {
			components = append(components, tabs)
		}
	}

	if m.selectedTag != "" {
		tagBannerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
		bannerText := fmt.Sprintf(i18n.T("tags.active_banner"), m.selectedTag, len(m.filtered))
		components = append(components, tagBannerStyle.Render(bannerText))
	}

	searchPrompt := i18n.T("search.prompt")
	searchContent := searchPrompt + m.searchInput.View()
	searchMaxW := searchMaxWidth(m.width)
	if m.width >= 50 && len(m.hosts) > 0 {
		var badge string
		if len(m.selectedHosts) > 0 {
			badge = fmt.Sprintf("[%s]", fmt.Sprintf(i18n.T("main.selected_count"), len(m.selectedHosts)))
		} else if m.searchInput.Value() != "" {
			badge = fmt.Sprintf("[%d/%d %s]", len(m.filtered), len(m.hosts), i18n.T("search.matched"))
		} else {
			cursor := m.table.Cursor() + 1
			if cursor > len(m.hosts) {
				cursor = len(m.hosts)
			}
			if len(m.filtered) != len(m.hosts) {
				cursor = m.table.Cursor() + 1
				if cursor > len(m.filtered) {
					cursor = len(m.filtered)
				}
				badge = fmt.Sprintf("[%d/%d]", cursor, len(m.filtered))
			} else {
				badge = fmt.Sprintf("[%d/%d]", cursor, len(m.hosts))
			}
		}
		gap := searchMaxW - ansi.StringWidth(searchContent) - ansi.StringWidth(badge)
		if gap >= 2 {
			searchContent = searchContent + strings.Repeat(" ", gap) + badge
		}
	}
	searchContent = ansi.Truncate(searchContent, searchMaxW, "")
	if m.searchMode {
		components = append(components, m.styles.SearchFocused.Render(searchContent))
	} else {
		components = append(components, m.styles.SearchUnfocused.Render(searchContent))
	}
	components = append(components, m.styles.TableFocused.Render(m.renderTable()))

	if m.statusActive() {
		components = append(components, renderStatusToast(m.statusMessage))
	}

	if m.searchMode {
		components = append(components, renderHelpText(m.styles, i18n.T("telnet.help_search"), m.width))
	} else {
		helpExtra := " • Space: select • Ctrl+A: all • w: tags • p: probe"
		if m.height < 20 {
			components = append(components, renderHelpText(m.styles, i18n.T("telnet.help_list")+helpExtra, m.width))
		} else {
			components = append(components, renderHelpText(m.styles, i18n.T("telnet.help_list_1")+helpExtra, m.width))
			components = append(components, renderHelpText(m.styles, i18n.T("telnet.help_list_2"), m.width))
		}
	}

	return m.styles.App.Render(
		lipgloss.JoinVertical(lipgloss.Left, components...),
	)
}

// renderDeleteConfirm builds the centered delete confirmation card.
func (m *telnetFormModel) renderDeleteConfirm() string {
	if len(m.selectedHosts) > 0 {
		return renderConfirmBox(m.styles, m.width,
			m.styles.ErrorText.Render(i18n.T("delete.title")),
			i18n.T("telnet.delete_batch_confirm", len(m.selectedHosts)),
			i18n.T("delete.warning"),
			m.styles.HelpText.Render(i18n.T("delete.help")),
		)
	}
	if m.deleteIndex < 0 || m.deleteIndex >= len(m.filtered) {
		return ""
	}
	h := m.filtered[m.deleteIndex]
	return renderConfirmBox(m.styles, m.width,
		m.styles.ErrorText.Render(i18n.T("delete.title")),
		i18n.T("telnet.delete_confirm", h.Name, net.JoinHostPort(h.Host, strconv.Itoa(h.Port))),
		i18n.T("delete.warning"),
		m.styles.HelpText.Render(i18n.T("delete.help")),
	)
}
