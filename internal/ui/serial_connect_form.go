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

// Common baud rates selectable via left/right arrows.
var baudRates = []int{9600, 19200, 38400, 57600, 115200, 230400, 460800, 921600}

type serialConnectFormModel struct {
	styles     Styles
	width      int
	height     int
	device     serialconfig.SerialDevice
	inputs     []textinput.Model // 0=baud 1=data 2=parity 3=stop
	focusIndex int
	baudIndex  int  // current preset index, -1 = custom
	done       bool
	cancelled  bool
}

func newSerialConnectForm(styles Styles, width, height int, dev serialconfig.SerialDevice) *serialConnectFormModel {
	inputs := make([]textinput.Model, 4)

	// Baud rate — editable text, also cycleable via arrows
	inputs[0] = textinput.New()
	inputs[0].SetValue(strconv.Itoa(dev.BaudRate))
	inputs[0].CharLimit = 10
	inputs[0].Focus()

	inputs[1] = textinput.New()
	inputs[1].SetValue(strconv.Itoa(dev.DataBits))
	inputs[1].CharLimit = 1

	inputs[2] = textinput.New()
	inputs[2].SetValue(dev.Parity)
	inputs[2].CharLimit = 5

	inputs[3] = textinput.New()
	inputs[3].SetValue(strconv.Itoa(dev.StopBits))
	inputs[3].CharLimit = 1

	for i := range inputs {
		inputs[i].TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	}

	// Find matching preset
	baudIdx := -1
	for i, b := range baudRates {
		if b == dev.BaudRate {
			baudIdx = i
			break
		}
	}

	return &serialConnectFormModel{
		styles:     styles,
		width:      width,
		height:     height,
		device:     dev,
		inputs:     inputs,
		focusIndex: 0,
		baudIndex:  baudIdx,
	}
}

func (m *serialConnectFormModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *serialConnectFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.cancelled = true
			return m, nil
		case "tab", "down":
			m.nextField()
			return m, nil
		case "shift+tab", "up":
			m.prevField()
			return m, nil
		case "left":
			if m.focusIndex == 0 {
				m.cycleBaud(-1)
				return m, nil
			}
		case "right":
			if m.focusIndex == 0 {
				m.cycleBaud(1)
				return m, nil
			}
		case "enter":
			if m.focusIndex == len(m.inputs)-1 {
				m.submit()
				return m, nil
			}
			m.nextField()
			return m, nil
		}
	}

	// Forward to focused textinput
	var cmd tea.Cmd
	m.inputs[m.focusIndex], cmd = m.inputs[m.focusIndex].Update(msg)

	// If user typed in baud field, check if value still matches a preset
	if m.focusIndex == 0 {
		val, err := strconv.Atoi(strings.TrimSpace(m.inputs[0].Value()))
		if err != nil {
			m.baudIndex = -1
		} else {
			m.baudIndex = -1
			for i, b := range baudRates {
				if b == val {
					m.baudIndex = i
					break
				}
			}
		}
	}

	return m, cmd
}

func (m *serialConnectFormModel) cycleBaud(dir int) {
	// If currently custom, snap to nearest preset
	if m.baudIndex < 0 {
		m.baudIndex = 4 // 115200
	} else {
		m.baudIndex += dir
		if m.baudIndex < 0 {
			m.baudIndex = len(baudRates) - 1
		}
		if m.baudIndex >= len(baudRates) {
			m.baudIndex = 0
		}
	}
	m.inputs[0].SetValue(strconv.Itoa(baudRates[m.baudIndex]))
	m.inputs[0].CursorEnd()
}

func (m *serialConnectFormModel) nextField() {
	m.inputs[m.focusIndex].Blur()
	m.focusIndex = (m.focusIndex + 1) % len(m.inputs)
	m.inputs[m.focusIndex].Focus()
}

func (m *serialConnectFormModel) prevField() {
	m.inputs[m.focusIndex].Blur()
	m.focusIndex--
	if m.focusIndex < 0 {
		m.focusIndex = len(m.inputs) - 1
	}
	m.inputs[m.focusIndex].Focus()
}

func (m *serialConnectFormModel) submit() {
	baud, _ := strconv.Atoi(strings.TrimSpace(m.inputs[0].Value()))
	if baud == 0 {
		baud = 115200
	}
	dataBits, _ := strconv.Atoi(strings.TrimSpace(m.inputs[1].Value()))
	if dataBits == 0 {
		dataBits = 8
	}
	parity := strings.TrimSpace(m.inputs[2].Value())
	if parity == "" {
		parity = "none"
	}
	stopBits, _ := strconv.Atoi(strings.TrimSpace(m.inputs[3].Value()))
	if stopBits == 0 {
		stopBits = 1
	}

	m.device.BaudRate = baud
	m.device.DataBits = dataBits
	m.device.Parity = parity
	m.device.StopBits = stopBits
	m.done = true
}

func (m *serialConnectFormModel) View() string {
	var b strings.Builder

	b.WriteString(m.styles.FormTitle.Render("  Edit Serial Parameters"))
	b.WriteString("\n\n")

	b.WriteString(m.styles.Label.Render(fmt.Sprintf("  Device:  %s", m.device.Device)))
	b.WriteString("\n")
	b.WriteString(m.styles.Label.Render(fmt.Sprintf("  Name:    %s", m.device.Name)))
	b.WriteString("\n\n")

	labels := []string{
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
		b.WriteString("  ")
		b.WriteString(style.Render(label))

		if i == 0 {
			// Show preset hint next to baud rate
			hint := ""
			if m.baudIndex >= 0 {
				hint = fmt.Sprintf("  (%d/%d presets)", m.baudIndex+1, len(baudRates))
			} else {
				hint = "  (custom)"
			}
			b.WriteString(m.inputs[0].View())
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(hint))
		} else {
			b.WriteString(m.inputs[i].View())
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(m.styles.FormHelp.Render(
		"  Baud: type directly or left/right to cycle presets • Tab/up/down: next field • Enter: connect • Esc: cancel"))

	return m.styles.FormContainer.Render(b.String())
}
