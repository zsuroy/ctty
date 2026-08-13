package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/zsuroy/ctty/internal/serialconfig"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// serialAddFormModel is the form for adding a new serial device.
type serialAddFormModel struct {
	styles    Styles
	width      int
	height     int
	inputs     []textinput.Model
	focusIndex int
	ports      []string
	portIndex  int

	// State flags checked by parent
	done      bool
	cancelled bool
}

// Field indices
const (
	serialFieldName = iota
	serialFieldDevice
	serialFieldBaud
	serialFieldDataBits
	serialFieldParity
	serialFieldStopBits
)

func newSerialAddForm(styles Styles, width, height int, ports []string) *serialAddFormModel {
	dev := serialconfig.DefaultDevice()

	inputs := make([]textinput.Model, 6)

	// Name
	inputs[serialFieldName] = textinput.New()
	inputs[serialFieldName].Placeholder = "e.g. Switch-Console"
	inputs[serialFieldName].CharLimit = 40
	inputs[serialFieldName].Focus()

	// Device
	inputs[serialFieldDevice] = textinput.New()
	if len(ports) > 0 {
		inputs[serialFieldDevice].Placeholder = fmt.Sprintf("e.g. %s (←/→ to pick)", ports[0])
	} else {
		inputs[serialFieldDevice].Placeholder = "e.g. /dev/cu.usbserial-1420"
	}
	inputs[serialFieldDevice].CharLimit = 120

	// Baud rate
	inputs[serialFieldBaud] = textinput.New()
	inputs[serialFieldBaud].Placeholder = "115200"
	inputs[serialFieldBaud].SetValue(strconv.Itoa(dev.BaudRate))
	inputs[serialFieldBaud].CharLimit = 10

	// Data bits
	inputs[serialFieldDataBits] = textinput.New()
	inputs[serialFieldDataBits].Placeholder = "5-8"
	inputs[serialFieldDataBits].SetValue(strconv.Itoa(dev.DataBits))
	inputs[serialFieldDataBits].CharLimit = 1

	// Parity
	inputs[serialFieldParity] = textinput.New()
	inputs[serialFieldParity].Placeholder = "none / even / odd"
	inputs[serialFieldParity].SetValue(dev.Parity)
	inputs[serialFieldParity].CharLimit = 5

	// Stop bits
	inputs[serialFieldStopBits] = textinput.New()
	inputs[serialFieldStopBits].Placeholder = "1 or 2"
	inputs[serialFieldStopBits].SetValue(strconv.Itoa(dev.StopBits))
	inputs[serialFieldStopBits].CharLimit = 1

	// Style inputs
	for i := range inputs {
		inputs[i].TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	}

	return &serialAddFormModel{
		styles:    styles,
		width:      width,
		height:     height,
		inputs:     inputs,
		ports:      ports,
		portIndex:  0,
		focusIndex: 0,
	}
}

func (m *serialAddFormModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *serialAddFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			m.cancelled = true
			return m, nil
		case "tab", "down":
			m.nextField()
			return m, nil
		case "shift+tab", "up":
			m.prevField()
			return m, nil
		case "left":
			if m.focusIndex == serialFieldDevice && len(m.ports) > 0 {
				m.portIndex--
				if m.portIndex < 0 {
					m.portIndex = len(m.ports) - 1
				}
				m.inputs[serialFieldDevice].SetValue(m.ports[m.portIndex])
				return m, nil
			}
		case "right":
			if m.focusIndex == serialFieldDevice && len(m.ports) > 0 {
				m.portIndex++
				if m.portIndex >= len(m.ports) {
					m.portIndex = 0
				}
				m.inputs[serialFieldDevice].SetValue(m.ports[m.portIndex])
				return m, nil
			}
		case "enter":
			if m.focusIndex == len(m.inputs)-1 {
				// Submit
				m.submit()
				return m, nil
			}
			m.nextField()
			return m, nil
		}
	}

	// Default: update focused input
	var cmd tea.Cmd
	m.inputs[m.focusIndex], cmd = m.inputs[m.focusIndex].Update(msg)
	return m, cmd
}

func (m *serialAddFormModel) nextField() {
	m.inputs[m.focusIndex].Blur()
	m.focusIndex = (m.focusIndex + 1) % len(m.inputs)
	m.inputs[m.focusIndex].Focus()
}

func (m *serialAddFormModel) prevField() {
	m.inputs[m.focusIndex].Blur()
	m.focusIndex--
	if m.focusIndex < 0 {
		m.focusIndex = len(m.inputs) - 1
	}
	m.inputs[m.focusIndex].Focus()
}

func (m *serialAddFormModel) submit() {
	name := strings.TrimSpace(m.inputs[serialFieldName].Value())
	device := strings.TrimSpace(m.inputs[serialFieldDevice].Value())
	if name == "" || device == "" {
		m.cancelled = true
		return
	}

	baud, _ := strconv.Atoi(m.inputs[serialFieldBaud].Value())
	if baud == 0 {
		baud = 115200
	}
	dataBits, _ := strconv.Atoi(m.inputs[serialFieldDataBits].Value())
	if dataBits == 0 {
		dataBits = 8
	}
	stopBits, _ := strconv.Atoi(m.inputs[serialFieldStopBits].Value())
	if stopBits == 0 {
		stopBits = 1
	}
	parity := strings.TrimSpace(m.inputs[serialFieldParity].Value())
	if parity == "" {
		parity = "none"
	}

	dev := serialconfig.SerialDevice{
		Name:     name,
		Device:   device,
		BaudRate: baud,
		DataBits: dataBits,
		Parity:   parity,
		StopBits: stopBits,
	}
	_ = serialconfig.Add(dev)
	m.done = true
}

func (m *serialAddFormModel) View() string {
	var b strings.Builder

	b.WriteString(m.styles.FormTitle.Render("  Add Serial Device"))
	b.WriteString("\n\n")

	labels := []string{
		"Name:      ",
		"Device:    ",
		"Baud Rate: ",
		"Data Bits: ",
		"Parity:    ",
		"Stop Bits: ",
	}

	for i, label := range labels {
		style := m.styles.Label
		if i == m.focusIndex {
			style = m.styles.FocusedLabel
		}
		b.WriteString(style.Render(label))
		b.WriteString(m.inputs[i].View())
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(m.styles.FormHelp.Render(
		"  Tab/↑↓: next field • ←/→ on Device: pick port • Enter: submit • Esc: cancel"))

	return m.styles.FormContainer.Render(b.String())
}
