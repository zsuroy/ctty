package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/serialconfig"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// serialFormModel manages the serial device list view.
// It shows saved serial devices + detected ports, and lets the user
// connect, add, or delete entries.
type serialFormModel struct {
	styles          Styles
	width           int
	height          int
	table           table.Model
	devices         []serialconfig.SerialDevice
	availablePorts  []string
	mode            serialMode
	connectForm     *serialConnectFormModel
	addForm         *serialAddFormModel
	deleteMode      bool
	deleteIndex     int
	infoIndex       int
	infoScroll      int
	ready           bool
	searchInput     textinput.Model
	searchMode      bool
	filteredDevices []serialconfig.SerialDevice
	statusMessage   string
	statusExpiry    time.Time
}

func (m *serialFormModel) setStatus(msg string) {
	m.statusMessage = msg
	m.statusExpiry = time.Now().Add(3 * time.Second)
}

func (m *serialFormModel) statusActive() bool {
	return m.statusMessage != "" && time.Now().Before(m.statusExpiry)
}

type serialMode int

const (
	serialList serialMode = iota
	serialAdd
	serialDeleteConfirm
	serialConnectSettings
	serialInfo
)

// serialConnectMsg tells the parent model to suspend the TUI and connect.
type serialConnectMsg struct {
	device serialconfig.SerialDevice
}

// serialDoneMsg tells the parent model to return to the SSH host list.
type serialDoneMsg struct{}

// serialBackMsg tells the parent to go back to host list (no state change).
type serialBackMsg struct{}

// NewSerialForm creates the serial device list view.
func NewSerialForm(styles Styles, width, height int) *serialFormModel {
	m := &serialFormModel{
		styles: styles,
		width:  width,
		height: height,
	}

	m.searchInput = textinput.New()
	m.searchInput.Placeholder = i18n.T("serial.search_placeholder")
	m.searchInput.CharLimit = 50
	m.searchInput.Width = searchInputWidth(m.width, i18n.T("search.prompt"))

	m.loadDevices()
	m.filteredDevices = m.devices
	m.buildTable()
	m.mode = serialList
	m.ready = true
	return m
}

func (m *serialFormModel) loadDevices() {
	// Load user-saved devices first.
	saved, err := serialconfig.Load()
	if err != nil {
		saved = []serialconfig.SerialDevice{}
	}

	// Scan physical ports currently available on the system.
	scanned := serialconfig.AvailablePorts()

	// Build a set of device paths already covered by saved configs
	// so we don't duplicate them in the "auto" section.
	seen := make(map[string]bool, len(saved))
	for _, d := range saved {
		seen[d.Device] = true
	}

	// Merged list: saved first, then newly-detected ports with defaults.
	merged := make([]serialconfig.SerialDevice, 0, len(saved)+len(scanned))
	merged = append(merged, saved...)
	for _, port := range scanned {
		if !seen[port] {
			auto := serialconfig.DefaultDevice()
			// The full path already lives in the Device column; repeating it
			// in Name guaranteed truncation in both.
			auto.Name = "(auto)"
			auto.Device = port
			merged = append(merged, auto)
		}
	}

	m.devices = merged
	m.availablePorts = scanned
}

func (m *serialFormModel) getColumns() []table.Column {
	w := m.width
	if w <= 0 {
		w = 80
	}

	// Bubbles table renders each cell with Padding(0,1) = 2 extra cols per cell.
	// TableFocused style adds border(2). App style adds padding(2).
	// So: rendered = colWidths + numCols*2 + 4
	// To match search bar (rendered = tw - 2 + 2 = tw):
	//   colWidths + numCols*2 + 4 = tw
	//   colWidths = tw - 4 - numCols*2
	//
	// Name and Device split the flexible remainder by their longest actual
	// content (device paths like /dev/cu.Bluetooth-Incoming-Port are 31+
	// chars; a 50/50 split truncated them while padding short aliases).
	nameNeed, devNeed := 10, 16 // header-length floors
	for _, d := range m.filteredDevices {
		if n := ansi.StringWidth(d.Name); n > nameNeed {
			nameNeed = n
		}
		if n := ansi.StringWidth(d.Device); n > devNeed {
			devNeed = n
		}
	}

	split := func(rem int) (int, int) {
		nameW := rem * nameNeed / (nameNeed + devNeed)
		if nameW < 8 {
			nameW = 8
		}
		if dw := rem - nameW; dw < 8 {
			nameW = rem - 8
			if nameW < 0 {
				nameW = 0
			}
		}
		return nameW, rem - nameW
	}

	if w < 60 {
		// 3 columns: name, device, baud(8)
		rem := w - 4 - 3*2 - 8
		if rem < 8 {
			rem = 8
		}
		nameW, devW := split(rem)
		return []table.Column{
			{Title: i18n.T("serial.col_name"), Width: nameW},
			{Title: i18n.T("serial.col_device"), Width: devW},
			{Title: i18n.T("serial.col_baud"), Width: 8},
		}
	} else if w < 90 {
		// 5 columns: name, device, baud(8), data(5), stop(5)
		rem := w - 4 - 5*2 - 8 - 5 - 5
		if rem < 10 {
			rem = 10
		}
		nameW, devW := split(rem)
		return []table.Column{
			{Title: i18n.T("serial.col_name"), Width: nameW},
			{Title: i18n.T("serial.col_device"), Width: devW},
			{Title: i18n.T("serial.col_baud"), Width: 8},
			{Title: i18n.T("serial.col_data"), Width: 5},
			{Title: i18n.T("serial.col_stop"), Width: 5},
		}
	}

	// 6 columns: name, device, baud(8), data(6), parity(8), stop(6)
	rem := w - 4 - 6*2 - 8 - 6 - 8 - 6
	if rem < 12 {
		rem = 12
	}
	nameW, devW := split(rem)
	return []table.Column{
		{Title: i18n.T("serial.col_name"), Width: nameW},
		{Title: i18n.T("serial.col_device"), Width: devW},
		{Title: i18n.T("serial.col_baud"), Width: 8},
		{Title: i18n.T("serial.col_data"), Width: 6},
		{Title: i18n.T("serial.col_parity"), Width: 8},
		{Title: i18n.T("serial.col_stop"), Width: 6},
	}
}

func (m *serialFormModel) buildTable() {
	columns := m.getColumns()

	rows := []table.Row{}
	for _, d := range m.filteredDevices {
		if len(columns) == 3 {
			rows = append(rows, table.Row{
				d.Name,
				d.Device,
				fmt.Sprintf("%d", d.BaudRate),
			})
		} else if len(columns) == 5 {
			rows = append(rows, table.Row{
				d.Name,
				d.Device,
				fmt.Sprintf("%d", d.BaudRate),
				fmt.Sprintf("%d", d.DataBits),
				fmt.Sprintf("%d", d.StopBits),
			})
		} else {
			rows = append(rows, table.Row{
				d.Name,
				d.Device,
				fmt.Sprintf("%d", d.BaudRate),
				fmt.Sprintf("%d", d.DataBits),
				d.Parity,
				fmt.Sprintf("%d", d.StopBits),
			})
		}
	}
	if len(rows) == 0 {
		emptyRow := make(table.Row, len(columns))
		emptyRow[0] = i18n.T("serial.no_ports")
		rows = append(rows, emptyRow)
	}

	s := table.DefaultStyles()
	s.Selected = m.styles.Selected
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(m.styles.Theme.Primary).
		BorderBottom(true).
		Bold(false)

	availHeight := m.height - 8
	if m.height >= 18 && m.width >= 64 {
		availHeight--
	}
	if availHeight < 2 {
		availHeight = 2
	}
	// Limit table height to actual content: header(1) + data rows
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

func (m *serialFormModel) refreshTable() {
	m.loadDevices()
	m.filterDevices()
	m.buildTable()
}

// filterDevices filters the device list by the current search input.
func (m *serialFormModel) filterDevices() {
	query := strings.ToLower(m.searchInput.Value())
	if query == "" {
		m.filteredDevices = m.devices
		return
	}
	filtered := m.devices[:0]
	for _, d := range m.devices {
		if strings.Contains(strings.ToLower(d.Name), query) ||
			strings.Contains(strings.ToLower(d.Device), query) {
			filtered = append(filtered, d)
		}
	}
	m.filteredDevices = filtered
}

// updateFilteredDevices re-applies the filter and rebuilds the table rows.
func (m *serialFormModel) updateFilteredDevices() {
	m.filterDevices()
	m.buildTable()
}

// Title returns the view title for breadcrumb display.
func (m *serialFormModel) Title() string {
	return "Serial Connections"
}

func (m *serialFormModel) Init() tea.Cmd {
	return nil
}

func (m *serialFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.styles = NewStyles(m.width)
		m.searchInput.Width = searchInputWidth(m.width, i18n.T("search.prompt"))
		m.buildTable()
		if m.addForm != nil {
			updated, cmd := m.addForm.Update(msg)
			if sm, ok := updated.(*serialAddFormModel); ok {
				m.addForm = sm
			}
			return m, cmd
		}
		if m.connectForm != nil {
			updated, cmd := m.connectForm.Update(msg)
			if sm, ok := updated.(*serialConnectFormModel); ok {
				m.connectForm = sm
			}
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case serialList:
			return m.handleListKeys(msg)
		case serialAdd:
			return m.handleAddKeys(msg)
		case serialConnectSettings:
			return m.handleConnectSettingsKeys(msg)
		case serialInfo:
			return m.handleInfoKeys(msg)
		case serialDeleteConfirm:
			return m.handleDeleteConfirmKeys(msg)
		}
	}

	// Default: update table
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *serialFormModel) handleListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle search mode separately
	if m.searchMode {
		return m.handleSearchKeys(msg)
	}

	key := msg.String()
	switch key {
	case "esc", "q":
		return m, func() tea.Msg { return serialDoneMsg{} }
	case "T", "]":
		return m, func() tea.Msg { return switchProtocolMsg{target: ViewTelnet} }
	case "F":
		return m, func() tea.Msg { return switchProtocolMsg{target: ViewFTP} }
	case "b":
		return m, func() tea.Msg { return switchProtocolMsg{target: ViewLocalBrowser} }
	case "[":
		return m, func() tea.Msg { return switchProtocolMsg{target: ViewList} }
	case "g", "home":
		m.table.SetCursor(0)
		return m, nil
	case "G", "end":
		if len(m.filteredDevices) > 0 {
			m.table.SetCursor(len(m.filteredDevices) - 1)
		}
		return m, nil
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(key[0] - '1')
		if idx < len(m.filteredDevices) {
			m.table.SetCursor(idx)
		}
		return m, nil
	case "/", "ctrl+f":
		m.searchMode = true
		m.searchInput.Focus()
		m.table.Blur()
		return m, textinput.Blink
	case "tab":
		// Switch focus from table to search input
		m.searchMode = true
		m.searchInput.Focus()
		m.table.Blur()
		return m, textinput.Blink
	case "enter":
		if len(m.filteredDevices) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx >= 0 && idx < len(m.filteredDevices) {
			dev := m.filteredDevices[idx]
			return m, func() tea.Msg {
				return serialConnectMsg{device: dev}
			}
		}
	case "i":
		if len(m.filteredDevices) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx >= 0 && idx < len(m.filteredDevices) {
			m.infoIndex = idx
			m.infoScroll = 0
			m.mode = serialInfo
			return m, nil
		}
	case "e":
		if len(m.filteredDevices) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx >= 0 && idx < len(m.filteredDevices) {
			dev := m.filteredDevices[idx]
			m.connectForm = newSerialConnectForm(m.styles, m.width, m.height, dev)
			m.mode = serialConnectSettings
			return m, m.connectForm.Init()
		}
	case "a":
		m.mode = serialAdd
		m.addForm = newSerialAddForm(m.styles, m.width, m.height, m.availablePorts)
		return m, m.addForm.Init()
	case "d":
		if len(m.filteredDevices) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx >= 0 && idx < len(m.filteredDevices) {
			m.mode = serialDeleteConfirm
			m.deleteIndex = idx
			return m, nil
		}
	case "y":
		if len(m.filteredDevices) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx >= 0 && idx < len(m.filteredDevices) {
			cmdStr := FormatSerialCommand(m.filteredDevices[idx])
			copyToClipboard(cmdStr)
			m.setStatus(fmt.Sprintf(i18n.T("main.copied"), cmdStr))
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *serialFormModel) handleSearchKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	case "up", "down":
		m.searchMode = false
		m.searchInput.Blur()
		m.table.Focus()
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	oldValue := m.searchInput.Value()
	m.searchInput, cmd = m.searchInput.Update(msg)
	if m.searchInput.Value() != oldValue {
		m.updateFilteredDevices()
	}
	return m, cmd
}

func (m *serialFormModel) handleDeleteConfirmKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		if m.deleteIndex >= 0 && m.deleteIndex < len(m.filteredDevices) {
			dev := m.filteredDevices[m.deleteIndex]
			_ = serialconfig.Delete(dev.Name)
		}
		m.mode = serialList
		m.refreshTable()
		return m, nil
	case "n", "N", "esc":
		m.mode = serialList
		m.deleteIndex = -1
		return m, nil
	}
	return m, nil
}
func (m *serialFormModel) handleInfoKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "i":
		m.mode = serialList
		m.infoIndex = -1
		return m, nil
	case "e", "enter":
		if m.infoIndex >= 0 && m.infoIndex < len(m.filteredDevices) {
			dev := m.filteredDevices[m.infoIndex]
			m.connectForm = newSerialConnectForm(m.styles, m.width, m.height, dev)
			m.mode = serialConnectSettings
			m.infoIndex = -1
			return m, m.connectForm.Init()
		}
	}
	if scrollInfoKey(msg.String(), &m.infoScroll) {
		return m, nil
	}
	return m, nil
}

func (m *serialFormModel) renderInfo() string {
	if m.infoIndex < 0 || m.infoIndex >= len(m.filteredDevices) {
		return ""
	}
	dev := m.filteredDevices[m.infoIndex]

	saved := i18n.T("serial.saved_yes")
	if strings.HasPrefix(dev.Name, "(auto)") {
		saved = i18n.T("serial.saved_no")
	}

	titleText := m.styles.Header.Render(strings.TrimSpace(i18n.T("serial.info_title")))

	rows := [][2]string{
		{strings.TrimSpace(i18n.T("serial.field_name")), dev.Name},
		{strings.TrimSpace(i18n.T("serial.field_device")), dev.Device},
		{strings.TrimSpace(i18n.T("serial.field_baud")), fmt.Sprintf("%d", dev.BaudRate)},
		{strings.TrimSpace(i18n.T("serial.field_data")), fmt.Sprintf("%d", dev.DataBits)},
		{strings.TrimSpace(i18n.T("serial.field_parity")), dev.Parity},
		{strings.TrimSpace(i18n.T("serial.field_stop")), fmt.Sprintf("%d", dev.StopBits)},
		{i18n.T("serial.col_saved") + ":", saved},
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
	helpText := m.styles.HelpText.Width(innerW).Render(i18n.T("serial.help_info"))
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

func (m *serialFormModel) handleAddKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Delegate to the add sub-form
	sub, cmd := m.addForm.Update(msg)
	if sm, ok := sub.(*serialAddFormModel); ok {
		m.addForm = sm
	}
	// Check if the sub-form emitted a done/cancel message
	if m.addForm.done {
		m.mode = serialList
		m.refreshTable()
		m.addForm = nil
		return m, nil
	}
	if m.addForm.cancelled {
		m.mode = serialList
		m.addForm = nil
		return m, nil
	}
	return m, cmd
}

func (m *serialFormModel) handleConnectSettingsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	sub, cmd := m.connectForm.Update(msg)
	if sm, ok := sub.(*serialConnectFormModel); ok {
		m.connectForm = sm
	}
	if m.connectForm.done {
		// User confirmed settings — send connect message
		dev := m.connectForm.device
		m.connectForm = nil
		m.mode = serialList
		return m, func() tea.Msg {
			return serialConnectMsg{device: dev}
		}
	}
	if m.connectForm.cancelled {
		m.connectForm = nil
		m.mode = serialList
		return m, nil
	}
	return m, cmd
}

func (m *serialFormModel) View() string {
	if !m.ready {
		return i18n.T("table.loading")
	}

	switch m.mode {
	case serialAdd:
		if m.addForm != nil {
			return m.addForm.View()
		}
	case serialConnectSettings:
		if m.connectForm != nil {
			return m.connectForm.View()
		}
	case serialInfo:
		return m.renderInfo()
	case serialDeleteConfirm:
		return renderConfirmModal(m.width, m.height, m.renderDeleteConfirm())
	}

	return m.renderList()
}

func (m *serialFormModel) renderList() string {
	components := []string{}

	// Header
	components = append(components, m.styles.Header.Render(i18n.T("serial.title")))

	if m.height >= 18 {
		if tabs := renderProtocolTabs(m.styles, "serial", len(m.filteredDevices), m.width); tabs != "" {
			components = append(components, tabs)
		}
	}

	// Search bar
	searchPrompt := i18n.T("search.prompt")
	components = append(components, renderSearchBar(m.styles, m.searchMode, searchPrompt, m.searchInput.View(), m.width))

	// Table
	components = append(components, m.styles.TableFocused.Render(m.table.View()))

	if m.statusActive() {
		components = append(components, renderStatusToast(m.statusMessage))
	}

	// Help
	var helpText string
	if m.searchMode {
		helpText = i18n.T("serial.help_search")
	} else {
		helpText = i18n.T("serial.help_list")
	}
	components = append(components, renderHelpText(m.styles, helpText, m.width))

	return m.styles.App.Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			components...,
		),
	)
}

// renderDeleteConfirm builds the centered delete confirmation card.
func (m *serialFormModel) renderDeleteConfirm() string {
	if m.deleteIndex < 0 || m.deleteIndex >= len(m.filteredDevices) {
		return ""
	}
	dev := m.filteredDevices[m.deleteIndex]
	return renderConfirmBox(m.styles, m.width,
		m.styles.ErrorText.Render(i18n.T("delete.title")),
		i18n.T("serial.delete_confirm", dev.Name, dev.Device),
		i18n.T("delete.warning"),
		m.styles.HelpText.Render(i18n.T("delete.help")),
	)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
