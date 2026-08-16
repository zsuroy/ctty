package ui

import (
	"fmt"
	"strings"

	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/serialconfig"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	ready           bool
	searchInput     textinput.Model
	searchMode      bool
	filteredDevices []serialconfig.SerialDevice
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
			auto.Name = "(auto) " + port
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

	if w < 60 {
		rem := w - 20
		if rem < 16 {
			rem = 16
		}
		nameW := rem / 2
		devW := rem - nameW
		return []table.Column{
			{Title: i18n.T("serial.col_name"), Width: nameW},
			{Title: i18n.T("serial.col_device"), Width: devW},
			{Title: i18n.T("serial.col_baud"), Width: 8},
		}
	} else if w < 90 {
		rem := w - 34
		if rem < 20 {
			rem = 20
		}
		nameW := rem / 2
		devW := rem - nameW
		return []table.Column{
			{Title: i18n.T("serial.col_name"), Width: nameW},
			{Title: i18n.T("serial.col_device"), Width: devW},
			{Title: i18n.T("serial.col_baud"), Width: 8},
			{Title: i18n.T("serial.col_data"), Width: 5},
			{Title: i18n.T("serial.col_stop"), Width: 5},
		}
	}

	rem := w - 46
	if rem < 24 {
		rem = 24
	}
	nameW := rem / 2
	devW := rem - nameW
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
		BorderForeground(lipgloss.Color(PrimaryColor)).
		BorderBottom(true).
		Bold(false)

	availHeight := m.height - 8
	if availHeight < 2 {
		availHeight = 2
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(availHeight),
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

	switch msg.String() {
	case "esc", "q":
		return m, func() tea.Msg { return serialDoneMsg{} }
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
			m.mode = serialInfo
			return m, nil
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
	case "y", "Y":
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

	var components []string
	components = append(components, m.styles.Header.Render(i18n.T("serial.info_title")))
	components = append(components, fmt.Sprintf("  %-14s %s", i18n.T("serial.field_name"), dev.Name))
	components = append(components, fmt.Sprintf("  %-14s %s", i18n.T("serial.field_device"), dev.Device))
	components = append(components, fmt.Sprintf("  %-14s %d", i18n.T("serial.field_baud"), dev.BaudRate))
	components = append(components, fmt.Sprintf("  %-14s %d", i18n.T("serial.field_data"), dev.DataBits))
	components = append(components, fmt.Sprintf("  %-14s %s", i18n.T("serial.field_parity"), dev.Parity))
	components = append(components, fmt.Sprintf("  %-14s %d", i18n.T("serial.field_stop"), dev.StopBits))
	components = append(components, fmt.Sprintf("  %-14s %s", "Saved Config:", saved))
	components = append(components, m.styles.HelpText.Render(i18n.T("serial.help_info")))

	return m.styles.App.Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			components...,
		),
	)
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
		return m.renderDeleteConfirm()
	}

	return m.renderList()
}

func (m *serialFormModel) renderList() string {
	components := []string{}

	// Header
	components = append(components, m.styles.Header.Render(i18n.T("serial.title")))

	// Search bar
	searchPrompt := i18n.T("search.prompt")
	components = append(components, renderSearchBar(m.styles, m.searchMode, searchPrompt, m.searchInput.View(), m.width))

	// Table
	components = append(components, m.styles.TableFocused.Render(m.table.View()))

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

func (m *serialFormModel) renderDeleteConfirm() string {
	if m.deleteIndex < 0 || m.deleteIndex >= len(m.filteredDevices) {
		return ""
	}
	dev := m.filteredDevices[m.deleteIndex]
	msg := i18n.T("serial.delete_confirm", dev.Name, dev.Device)
	return m.styles.Error.Render(msg)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
