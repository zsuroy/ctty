package ui

import (
	"net"
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
	styles      Styles
	width       int
	height      int
	table       table.Model
	hosts       []telnetconfig.TelnetHost
	filtered    []telnetconfig.TelnetHost
	mode        telnetMode
	addForm     *telnetAddFormModel
	deleteIndex int
	infoIndex   int
	probing     bool
	status      map[string]bool
	ready       bool
	searchInput textinput.Model
	searchMode  bool
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

// telnetProbeMsg reports the TCP reachability of one telnet host.
type telnetProbeMsg struct {
	name string
	up   bool
}

// probeTelnetHostCmd dials the host with a short timeout; the result
// updates the status indicator in the device list.
func probeTelnetHostCmd(h telnetconfig.TelnetHost) tea.Cmd {
	return func() tea.Msg {
		addr := net.JoinHostPort(h.Host, strconv.Itoa(h.Port))
		conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
		if err == nil {
			_ = conn.Close()
		}
		return telnetProbeMsg{name: h.Name, up: err == nil}
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
	if st, ok := m.status[h.Name]; ok {
		if st {
			return "🟢 " + h.Name
		}
		return "🔴 " + h.Name
	}
	if m.probing {
		return "🟡 " + h.Name
	}
	return h.Name
}

func (m *telnetFormModel) buildTable() {
	columns := m.getColumns()

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
		BorderForeground(lipgloss.Color(PrimaryColor)).
		BorderBottom(true).
		Bold(false)

	availHeight := m.height - 9
	if m.height < 20 {
		availHeight = m.height - 7
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
	query := strings.ToLower(m.searchInput.Value())
	if query == "" {
		m.filtered = m.hosts
		return
	}
	filtered := make([]telnetconfig.TelnetHost, 0, len(m.hosts))
	for _, h := range m.hosts {
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

	case tea.KeyMsg:
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

	case telnetProbeMsg:
		if m.status == nil {
			m.status = make(map[string]bool)
		}
		m.status[msg.name] = msg.up
		if len(m.status) >= len(m.hosts) {
			m.probing = false
		}
		m.buildTable()
		return m, nil
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *telnetFormModel) handleListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searchMode {
		return m.handleSearchKeys(msg)
	}

	switch msg.String() {
	case "esc", "q":
		return m, func() tea.Msg { return telnetDoneMsg{} }
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
	case "p":
		if !m.probing && len(m.hosts) > 0 {
			m.probing = true
			m.status = nil
			m.buildTable()
			return m, m.startProbeCmd()
		}
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
		if len(m.filtered) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx >= 0 && idx < len(m.filtered) {
			m.mode = telnetDeleteConfirm
			m.deleteIndex = idx
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
	case "y", "Y":
		if m.deleteIndex >= 0 && m.deleteIndex < len(m.filtered) {
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
	return m, nil
}

func (m *telnetFormModel) renderInfo() string {
	if m.infoIndex < 0 || m.infoIndex >= len(m.filtered) {
		return ""
	}
	h := m.filtered[m.infoIndex]

	infoLabels := []string{
		i18n.T("telnet.col_name"),
		i18n.T("telnet.col_host"),
		i18n.T("telnet.col_port"),
		i18n.T("table.col.tags"),
	}
	labelCol := 0
	for _, label := range infoLabels {
		if w := ansi.StringWidth(label); w > labelCol {
			labelCol = w
		}
	}
	row := func(label, value string) string {
		return "  " + padDisplay(label, labelCol) + " " + value
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		m.styles.FormTitle.Render(" "+strings.TrimSpace(i18n.T("telnet.info_title"))+" "),
		"",
		row(i18n.T("telnet.col_name"), h.Name),
		row(i18n.T("telnet.col_host"), h.Host),
		row(i18n.T("telnet.col_port"), strconv.Itoa(h.Port)),
		row(i18n.T("table.col.tags"), FormatColoredTags(h.Tags)),
		"",
		m.styles.HelpText.MaxWidth(formPageInnerWidth(m.width)).Render(i18n.T("telnet.help_info")),
	)
	return renderFormPage(m.styles, m.width, body)
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

	var content string
	switch m.mode {
	case telnetAdd, telnetEdit:
		if m.addForm != nil {
			content = m.addForm.View()
		}
	case telnetInfo:
		content = m.renderInfo()
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
		BorderForeground(lipgloss.Color(PrimaryColor)).
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
	selectedBg := lipgloss.NewStyle().Background(lipgloss.Color(PrimaryColor))

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
	searchPrompt := i18n.T("search.prompt")
	components = append(components, renderSearchBar(m.styles, m.searchMode, searchPrompt, m.searchInput.View(), m.width))
	components = append(components, m.styles.TableFocused.Render(m.renderTable()))

	if m.mode == telnetDeleteConfirm {
		components = append(components, m.renderDeleteConfirm())
	}

	if m.searchMode {
		components = append(components, renderHelpText(m.styles, i18n.T("telnet.help_search"), m.width))
	} else if m.height < 20 {
		components = append(components, renderHelpText(m.styles, i18n.T("telnet.help_list"), m.width))
	} else {
		components = append(components, renderHelpText(m.styles, i18n.T("telnet.help_list_1"), m.width))
		components = append(components, renderHelpText(m.styles, i18n.T("telnet.help_list_2"), m.width))
	}

	return m.styles.App.Render(
		lipgloss.JoinVertical(lipgloss.Left, components...),
	)
}

func (m *telnetFormModel) renderDeleteConfirm() string {
	if m.deleteIndex < 0 || m.deleteIndex >= len(m.filtered) {
		return ""
	}
	h := m.filtered[m.deleteIndex]
	msg := i18n.T("telnet.delete_confirm", h.Name, net.JoinHostPort(h.Host, strconv.Itoa(h.Port)))
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	return style.Render(msg)
}
