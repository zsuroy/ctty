package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
		m.styles = NewStyles(m.width)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q", "i":
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
	titleRendered := m.styles.Header.Render(title)

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

	maxLabelW := 0
	for _, section := range sections {
		if w := ansi.StringWidth(section.label + ":"); w > maxLabelW {
			maxLabelW = w
		}
	}

	labelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(m.styles.Theme.Primary)

	// Render each section
	for _, section := range sections {
		paddedLabel := padDisplay(section.label+":", maxLabelW)
		valueStyle := lipgloss.NewStyle()

		if section.value == i18n.T("info.not_set") || (section.value == "22" && section.label == i18n.T("info.port")) {
			valueStyle = valueStyle.Foreground(m.styles.Theme.HelpText)
		}

		line := lipgloss.JoinHorizontal(
			lipgloss.Top,
			labelStyle.Render("  "+paddedLabel),
			" ",
			valueStyle.Render(section.value),
		)
		bodyLines = append(bodyLines, line)
	}

	totalHeight := m.height
	if totalHeight <= 0 {
		totalHeight = 24
	}

	container := m.styles.FormContainer
	if totalHeight < 24 {
		container = container.Padding(0, 1)
	}

	boxWidth := m.width - 4
	if boxWidth < 20 {
		boxWidth = 20
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
	headerH := lipgloss.Height(titleRendered)

	// Action instructions
	actionText := m.styles.HelpText.Width(innerW).Render(i18n.T("info.help"))
	helpH := lipgloss.Height(actionText)
	overhead := frameH + headerH + helpH + 2
	viewportHeight := targetBoxH - overhead
	if viewportHeight < 3 {
		viewportHeight = 3
	}

	bodyLines = wrapInfoLines(bodyLines, innerW)
	visibleBody := scrollInfoWindow(bodyLines, viewportHeight, &m.scrollOffset)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		titleRendered,
		"",
		visibleBody,
		"",
		actionText,
	)

	box := container.Width(boxWidth).Render(content)
	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Top,
		box,
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
