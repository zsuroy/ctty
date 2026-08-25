package ui

import (
	"strconv"
	"strings"

	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/telnetconfig"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// telnetAddFormModel is the form for adding or editing a telnet device.
// When initial is non-nil the form edits that host (submit routes to
// telnetconfig.Update with the original name preserved).
type telnetAddFormModel struct {
	styles     Styles
	width      int
	height     int
	inputs     []textinput.Model
	focusIndex int
	editing    *telnetconfig.TelnetHost

	done      bool
	cancelled bool
}

// Field indices
const (
	telnetFieldName = iota
	telnetFieldHost
	telnetFieldPort
	telnetFieldTags
)

func newTelnetAddForm(styles Styles, width, height int, initial *telnetconfig.TelnetHost) *telnetAddFormModel {
	def := telnetconfig.DefaultHost()
	if initial != nil {
		def = *initial
	}

	inputs := make([]textinput.Model, 4)

	inputs[telnetFieldName] = textinput.New()
	inputs[telnetFieldName].Placeholder = "e.g. core-sw console"
	inputs[telnetFieldName].CharLimit = 40
	inputs[telnetFieldName].Focus()

	inputs[telnetFieldHost] = textinput.New()
	inputs[telnetFieldHost].Placeholder = "e.g. 192.168.1.1 or ::1"
	inputs[telnetFieldHost].CharLimit = 120

	inputs[telnetFieldPort] = textinput.New()
	inputs[telnetFieldPort].Placeholder = "23"
	inputs[telnetFieldPort].SetValue(strconv.Itoa(def.Port))
	inputs[telnetFieldPort].CharLimit = 5

	inputs[telnetFieldTags] = textinput.New()
	inputs[telnetFieldTags].Placeholder = "lab,network"
	inputs[telnetFieldTags].CharLimit = 80

	if initial != nil {
		inputs[telnetFieldName].SetValue(initial.Name)
		inputs[telnetFieldHost].SetValue(initial.Host)
		inputs[telnetFieldTags].SetValue(strings.Join(initial.Tags, ","))
	}

	for i := range inputs {
		inputs[i].TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	}

	return &telnetAddFormModel{
		styles:     styles,
		width:      width,
		height:     height,
		inputs:     inputs,
		focusIndex: 0,
		editing:    initial,
	}
}

func (m *telnetAddFormModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *telnetAddFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.styles = NewStyles(m.width)
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			m.cancelled = true
			return m, nil
		case "ctrl+s":
			m.submit()
			return m, nil
		case "tab", "down":
			m.nextField()
			return m, nil
		case "shift+tab", "up":
			m.prevField()
			return m, nil
		case "enter":
			if m.focusIndex == len(m.inputs)-1 {
				m.submit()
				return m, nil
			}
			m.nextField()
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.inputs[m.focusIndex], cmd = m.inputs[m.focusIndex].Update(msg)
	return m, cmd
}

func (m *telnetAddFormModel) nextField() {
	m.inputs[m.focusIndex].Blur()
	m.focusIndex = (m.focusIndex + 1) % len(m.inputs)
	m.inputs[m.focusIndex].Focus()
}

func (m *telnetAddFormModel) prevField() {
	m.inputs[m.focusIndex].Blur()
	m.focusIndex--
	if m.focusIndex < 0 {
		m.focusIndex = len(m.inputs) - 1
	}
	m.inputs[m.focusIndex].Focus()
}

func (m *telnetAddFormModel) submit() {
	name := strings.TrimSpace(m.inputs[telnetFieldName].Value())
	host := strings.TrimSpace(m.inputs[telnetFieldHost].Value())
	if name == "" || host == "" {
		m.cancelled = true
		return
	}

	port, err := strconv.Atoi(strings.TrimSpace(m.inputs[telnetFieldPort].Value()))
	if err != nil || port <= 0 || port > 65535 {
		port = telnetconfig.DefaultPort
	}

	var tags []string
	for _, t := range strings.Split(m.inputs[telnetFieldTags].Value(), ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}

	newHost := telnetconfig.TelnetHost{Name: name, Host: host, Port: port, Tags: tags}

	var err2 error
	if m.editing != nil {
		err2 = telnetconfig.Update(m.editing.Name, newHost)
	} else {
		err2 = telnetconfig.Add(newHost)
	}
	if err2 != nil {
		// Duplicate name or persistence failure: stay in the form.
		m.cancelled = true
		return
	}
	m.done = true
}

func (m *telnetAddFormModel) View() string {
	title := i18n.T("telnet.add_title")
	if m.editing != nil {
		title = i18n.T("telnet.edit_title")
	}

	labels := []string{
		i18n.T("telnet.col_name"),
		i18n.T("telnet.field_host"),
		i18n.T("telnet.field_port"),
		i18n.T("table.col.tags"),
	}
	labelCol := 0
	for _, label := range labels {
		if w := ansi.StringWidth(label); w > labelCol {
			labelCol = w
		}
	}

	inner := formPageInnerWidth(m.width)
	// "  " + label + " " + textinput prompt ("> ")
	inputW := inner - 5 - labelCol
	if inputW < 8 {
		inputW = 8
	}
	for i := range m.inputs {
		m.inputs[i].Width = inputW
	}

	var fields []string
	for i, label := range labels {
		style := m.styles.Label
		if i == m.focusIndex {
			style = m.styles.FocusedLabel
		}
		fields = append(fields, "  "+style.Render(padDisplay(label, labelCol))+" "+m.inputs[i].View())
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		m.styles.FormTitle.Render(" "+strings.TrimSpace(title)+" "),
		"",
		strings.Join(fields, "\n"),
		"",
		m.styles.HelpText.MaxWidth(inner).Render(i18n.T("telnet.help_add")),
	)
	return renderFormPage(m.styles, m.width, body)
}
