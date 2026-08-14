package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/credential"
	"github.com/zsuroy/ctty/internal/i18n"
)

type infoFormModel struct {
	hostName     string
	host         config.SSHHost
	styles       Styles
	width        int
	height       int
	configFile   string
	scrollOffset int
}

// Messages for info form actions
type infoFormCancelMsg struct{}
type infoFormEditMsg struct {
	hostName string
}

// NewInfoForm creates a new host information form
func NewInfoForm(hostName string, styles Styles, width, height int, configFile string) (*infoFormModel, error) {
	// Find the host in config
	var host *config.SSHHost
	var err error

	if configFile != "" {
		host, err = config.GetSSHHostFromFile(hostName, configFile)
	} else {
		host, err = config.GetSSHHost(hostName)
	}

	if err != nil {
		return nil, fmt.Errorf("error finding host: %w", err)
	}

	if host == nil {
		return nil, fmt.Errorf("host %s not found", hostName)
	}

	return &infoFormModel{
		hostName:   hostName,
		host:       *host,
		styles:     styles,
		width:      width,
		height:     height,
		configFile: configFile,
	}, nil
}

func (m *infoFormModel) Init() tea.Cmd {
	return nil
}

func (m *infoFormModel) Update(msg tea.Msg) (*infoFormModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			return m, func() tea.Msg { return infoFormCancelMsg{} }

		case "e", "enter":
			// Switch to edit mode
			return m, func() tea.Msg { return infoFormEditMsg{hostName: m.hostName} }

		case "up", "k":
			if m.scrollOffset > 0 {
				m.scrollOffset--
			}
			return m, nil

		case "down", "j":
			m.scrollOffset++
			return m, nil
		}
	}

	return m, nil
}

func (m *infoFormModel) View() string {
	var bodyLines []string

	// Title
	title := i18n.T("info.title", m.host.Name)
	titleRendered := m.styles.FormTitle.Render(title)

	hasPassword := i18n.T("info.not_set")
	if _, ok := credential.GetPassword(m.host.Name); ok {
		hasPassword = i18n.T("info.password_saved")
	}

	// Create info sections with consistent formatting
	sections := []struct {
		label string
		value string
	}{
		{i18n.T("info.host_name"), m.host.Name},
		{i18n.T("info.config_file"), formatConfigFile(m.host.SourceFile)},
		{i18n.T("info.hostname_ip"), m.host.Hostname},
		{i18n.T("info.user"), formatOptionalValue(m.host.User)},
		{i18n.T("info.port"), formatOptionalValue(m.host.Port)},
		{i18n.T("info.password"), hasPassword},
		{i18n.T("info.identity_file"), formatOptionalValue(m.host.Identity)},
		{i18n.T("info.proxy_jump"), formatOptionalValue(m.host.ProxyJump)},
		{i18n.T("info.proxy_command"), formatOptionalValue(m.host.ProxyCommand)},
		{i18n.T("info.ssh_options"), formatSSHOptions(m.host.Options)},
		{i18n.T("info.tags"), formatTags(m.host.Tags)},
	}

	// Render each section
	for _, section := range sections {
		labelStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39")).
			Width(15).
			AlignHorizontal(lipgloss.Right)

		valueStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("255"))

		if section.value == i18n.T("info.not_set") || (section.value == "22" && section.label == i18n.T("info.port")) {
			valueStyle = valueStyle.Foreground(lipgloss.Color("243"))
		}

		line := lipgloss.JoinHorizontal(
			lipgloss.Top,
			labelStyle.Render(section.label+":"),
			" ",
			valueStyle.Render(section.value),
		)
		bodyLines = append(bodyLines, line)
	}

	totalHeight := m.height
	if totalHeight <= 0 {
		totalHeight = 24
	}

	// Calculate viewport height (reserve 6 lines for title, border, actions)
	viewportHeight := totalHeight - 6
	if viewportHeight < 4 {
		viewportHeight = 4
	}

	if len(bodyLines) <= viewportHeight {
		m.scrollOffset = 0
	} else {
		if m.scrollOffset > len(bodyLines)-viewportHeight {
			m.scrollOffset = len(bodyLines) - viewportHeight
		}
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}
	}

	endIdx := m.scrollOffset + viewportHeight
	if endIdx > len(bodyLines) {
		endIdx = len(bodyLines)
	}
	visibleBody := strings.Join(bodyLines[m.scrollOffset:endIdx], "\n")

	// Action instructions
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")).
		Italic(true)
	actionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("120")).
		Bold(true)

	var actionText string
	if len(bodyLines) > viewportHeight {
		actionText = helpStyle.Render("↑/↓: scroll  •  ")
	}
	actionText += actionStyle.Render("e/Enter") + helpStyle.Render(i18n.T("info.action_edit")) + "  " +
		actionStyle.Render("q/Esc") + helpStyle.Render(i18n.T("info.action_return"))

	var content strings.Builder
	content.WriteString(titleRendered)
	content.WriteString("\n\n")
	content.WriteString(visibleBody)
	content.WriteString("\n\n")
	content.WriteString(actionText)

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("39")).
		Padding(0, 1)

	if m.height >= 22 {
		borderStyle = borderStyle.Padding(1, 2)
	}

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		borderStyle.Render(content.String()),
	)
}

// Helper functions for formatting values

func formatOptionalValue(value string) string {
	if value == "" {
		return i18n.T("info.not_set")
	}
	return value
}

func formatSSHOptions(options string) string {
	if options == "" {
		return i18n.T("info.not_set")
	}
	return options
}

func formatTags(tags []string) string {
	if len(tags) == 0 {
		return i18n.T("info.not_set")
	}
	return FormatColoredTags(tags)
}

// Standalone wrapper for info form (for testing or standalone use)
type standaloneInfoForm struct {
	*infoFormModel
}

func (m standaloneInfoForm) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case infoFormCancelMsg:
		return m, tea.Quit
	case infoFormEditMsg:
		return m, tea.Quit
	}

	newForm, cmd := m.infoFormModel.Update(msg)
	m.infoFormModel = newForm
	return m, cmd
}

// RunInfoForm provides a standalone info form for testing
func RunInfoForm(hostName string, configFile string) error {
	styles := NewStyles(80)
	infoForm, err := NewInfoForm(hostName, styles, 80, 24, configFile)
	if err != nil {
		return err
	}
	m := standaloneInfoForm{infoForm}

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
