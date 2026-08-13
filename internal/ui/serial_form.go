package ui

import (
	"fmt"
	"strings"

	"github.com/zsuroy/ctty/internal/serialconfig"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// serialFormModel manages the serial device list view.
// It shows saved serial devices + detected ports, and lets the user
// connect, add, or delete entries.
type serialFormModel struct {
	styles     Styles
	width       int
	height      int
	table       table.Model
	devices     []serialconfig.SerialDevice
	availablePorts []string
	mode        serialMode
	connectForm *serialConnectFormModel
	addForm     *serialAddFormModel
	deleteMode  bool
	deleteIndex int
	infoIndex   int
	ready       bool
	searchInput    textinput.Model
	searchMode     bool
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
	m.searchInput.Placeholder = "Search devices..."
	m.searchInput.CharLimit = 50
	m.searchInput.Width = 25

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

func (m *serialFormModel) buildTable() {
	columns := []table.Column{
		{Title: "Name", Width: 22},
		{Title: "Device", Width: 30},
		{Title: "Baud", Width: 8},
		{Title: "Data", Width: 6},
		{Title: "Parity", Width: 8},
		{Title: "Stop", Width: 6},
	}

	rows := []table.Row{}
	for _, d := range m.filteredDevices {
		rows = append(rows, table.Row{
			d.Name,
			d.Device,
			fmt.Sprintf("%d", d.BaudRate),
			fmt.Sprintf("%d", d.DataBits),
			d.Parity,
			fmt.Sprintf("%d", d.StopBits),
		})
	}
	if len(rows) == 0 {
		rows = append(rows, table.Row{"(no serial ports detected)", "", "", "", "", ""})
	}

	s := table.DefaultStyles()
	s.Selected = m.styles.Selected
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(PrimaryColor)).
		BorderBottom(true).
		Bold(false)

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(min(len(rows)+2, 15)),
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
		// Toggle focus between search and table
		if m.searchMode {
			m.searchMode = false
			m.searchInput.Blur()
			m.table.Focus()
		}
		return m, nil
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

	saved := "yes"
	if strings.HasPrefix(dev.Name, "(auto)") {
		saved = "no (auto-detected)"
	}

	lines := []string{
		m.styles.Header.Render("  Serial Device Info"),
		"",
		fmt.Sprintf("  Name:       %s", dev.Name),
		fmt.Sprintf("  Device:     %s", dev.Device),
		fmt.Sprintf("  Baud Rate:  %d", dev.BaudRate),
		fmt.Sprintf("  Data Bits:  %d", dev.DataBits),
		fmt.Sprintf("  Parity:     %s", dev.Parity),
		fmt.Sprintf("  Stop Bits:  %d", dev.StopBits),
		fmt.Sprintf("  Saved:      %s", saved),
		"",
		m.styles.HelpText.Render("  e/Enter: edit params • Esc/i: back to list"),
	}

	return m.styles.FormContainer.Render(strings.Join(lines, "\n"))
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
		return "Loading..."
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
	var b strings.Builder

	// Header
	b.WriteString(m.styles.Header.Render("  Serial Connections"))
	b.WriteString("\n\n")

	// Search bar
	searchPrompt := "Search (/ to focus): "
	if m.searchMode {
		b.WriteString(m.styles.SearchFocused.Render(searchPrompt + m.searchInput.View()))
	} else {
		b.WriteString(m.styles.SearchUnfocused.Render(searchPrompt + m.searchInput.View()))
	}
	b.WriteString("\n\n")

	// Table
	b.WriteString(m.styles.TableFocused.Render(m.table.View()))
	b.WriteString("\n\n")

	// Help
	var helpText string
	if m.searchMode {
		helpText = "  Type to filter • Enter: confirm • Tab: switch • ESC: back"
	} else {
		helpText = "  ↑/↓: navigate • Enter: connect • i: info • a: add • d: delete • /: search • Esc: back"
	}
	b.WriteString(m.styles.HelpText.Render(helpText))
	return m.styles.FormContainer.Render(b.String())
}

func (m *serialFormModel) renderDeleteConfirm() string {
	if m.deleteIndex < 0 || m.deleteIndex >= len(m.filteredDevices) {
		return ""
	}
	dev := m.filteredDevices[m.deleteIndex]
	msg := fmt.Sprintf("Delete serial device '%s' (%s)? [y/n]", dev.Name, dev.Device)
	return m.styles.Error.Render(msg)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
