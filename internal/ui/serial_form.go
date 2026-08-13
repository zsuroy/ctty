package ui

import (
	"fmt"
	"strings"

	"github.com/zsuroy/ctty/internal/serialconfig"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/table"
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
	ready       bool
}

type serialMode int

const (
	serialList serialMode = iota
	serialAdd
	serialDeleteConfirm
	serialConnectSettings
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

	m.loadDevices()
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
	for _, d := range m.devices {
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
	switch msg.String() {
	case "esc", "q":
		return m, func() tea.Msg { return serialDoneMsg{} }
	case "enter":
		if len(m.devices) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx >= 0 && idx < len(m.devices) {
			dev := m.devices[idx]
			return m, func() tea.Msg {
				return serialConnectMsg{device: dev}
			}
		}
	case "e":
		if len(m.devices) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx >= 0 && idx < len(m.devices) {
			dev := m.devices[idx]
			m.connectForm = newSerialConnectForm(m.styles, m.width, m.height, dev)
			m.mode = serialConnectSettings
			return m, m.connectForm.Init()
		}
	case "a":
		m.mode = serialAdd
		m.addForm = newSerialAddForm(m.styles, m.width, m.height, m.availablePorts)
		return m, m.addForm.Init()
	case "d":
		if len(m.devices) == 0 {
			return m, nil
		}
		idx := m.table.Cursor()
		if idx >= 0 && idx < len(m.devices) {
			m.mode = serialDeleteConfirm
			m.deleteIndex = idx
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *serialFormModel) handleDeleteConfirmKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if m.deleteIndex >= 0 && m.deleteIndex < len(m.devices) {
			dev := m.devices[m.deleteIndex]
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

	// Table
	b.WriteString(m.styles.TableFocused.Render(m.table.View()))
	b.WriteString("\n\n")

	// Help
	helpText := "  ↑/↓: navigate • Enter: connect • e: edit params • a: add • d: delete • Esc: back"
	b.WriteString(m.styles.HelpText.Render(helpText))

	return m.styles.FormContainer.Render(b.String())
}

func (m *serialFormModel) renderDeleteConfirm() string {
	if m.deleteIndex < 0 || m.deleteIndex >= len(m.devices) {
		return ""
	}
	dev := m.devices[m.deleteIndex]
	msg := fmt.Sprintf("Delete serial device '%s' (%s)? [y/n]", dev.Name, dev.Device)
	return m.styles.Error.Render(msg)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
