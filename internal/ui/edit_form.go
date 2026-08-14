package ui

import (
	"fmt"
	"strings"

	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/credential"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/validation"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	focusAreaHosts = iota
	focusAreaProperties
)

type editFormSubmitMsg struct {
	hostname string
	err      error
}

type editFormCancelMsg struct{}

type editFormModel struct {
	hostInputs       []textinput.Model // Support for multiple hosts
	inputs           []textinput.Model
	focusArea        int // 0=hosts, 1=properties
	focused          int
	currentTab       int // 0=General, 1=Advanced (only applies when focusArea == focusAreaProperties)
	err              string
	styles           Styles
	originalName     string
	originalHosts    []string        // Store original host names for multi-host detection
	host             *config.SSHHost // Store the original host with SourceFile
	configFile       string          // Configuration file path passed by user
	actualConfigFile string          // Actual config file to use (either configFile or host.SourceFile)
	width            int
	height           int
}

// NewEditForm creates a new edit form model that supports both single and multi-host editing
func NewEditForm(hostName string, styles Styles, width, height int, configFile string) (*editFormModel, error) {
	// Get the existing host configuration
	var host *config.SSHHost
	var err error

	if configFile != "" {
		host, err = config.GetSSHHostFromFile(hostName, configFile)
	} else {
		host, err = config.GetSSHHost(hostName)
	}

	if err != nil {
		return nil, err
	}

	// Check if this host is part of a multi-host declaration
	var actualConfigFile string
	var hostNames []string
	var isMulti bool

	if configFile != "" {
		actualConfigFile = configFile
	} else {
		actualConfigFile = host.SourceFile
	}

	if actualConfigFile != "" {
		isMulti, hostNames, err = config.IsPartOfMultiHostDeclaration(hostName, actualConfigFile)
		if err != nil {
			// If we can't determine multi-host status, treat as single host
			isMulti = false
			hostNames = []string{hostName}
		}
	}

	if !isMulti {
		hostNames = []string{hostName}
	}

	// Create host inputs
	hostInputs := make([]textinput.Model, len(hostNames))
	for i, name := range hostNames {
		hostInputs[i] = textinput.New()
		hostInputs[i].Placeholder = "host-name"
		hostInputs[i].SetValue(name)
		if i == 0 {
			hostInputs[i].Focus()
		}
	}

	inputs := make([]textinput.Model, 11)

	// 0: Hostname input
	inputs[0] = textinput.New()
	inputs[0].Placeholder = "192.168.1.100 or example.com"
	inputs[0].CharLimit = 100
	inputs[0].Width = 30
	inputs[0].SetValue(host.Hostname)

	// 1: User input
	inputs[1] = textinput.New()
	inputs[1].Placeholder = "root"
	inputs[1].CharLimit = 50
	inputs[1].Width = 30
	inputs[1].SetValue(host.User)

	// 2: Port input
	inputs[2] = textinput.New()
	inputs[2].Placeholder = "22"
	inputs[2].CharLimit = 5
	inputs[2].Width = 30
	inputs[2].SetValue(host.Port)

	// 3: Password input
	inputs[3] = textinput.New()
	inputs[3].Placeholder = i18n.T("form.password_placeholder")
	inputs[3].EchoMode = textinput.EchoPassword
	inputs[3].EchoCharacter = '•'
	inputs[3].CharLimit = 100
	inputs[3].Width = 30
	if pass, ok := credential.GetPassword(hostName); ok {
		inputs[3].SetValue(pass)
	}

	// 4: Identity input
	inputs[4] = textinput.New()
	inputs[4].Placeholder = "~/.ssh/id_rsa"
	inputs[4].CharLimit = 200
	inputs[4].Width = 50
	inputs[4].SetValue(host.Identity)

	// 5: ProxyJump input
	inputs[5] = textinput.New()
	inputs[5].Placeholder = "jump-server"
	inputs[5].CharLimit = 100
	inputs[5].Width = 30
	inputs[5].SetValue(host.ProxyJump)

	// 6: ProxyCommand input
	inputs[6] = textinput.New()
	inputs[6].Placeholder = "ssh -W %h:%p Jumphost"
	inputs[6].CharLimit = 200
	inputs[6].Width = 50
	inputs[6].SetValue(host.ProxyCommand)

	// 7: Tags input
	inputs[7] = textinput.New()
	inputs[7].Placeholder = "production, web, database"
	inputs[7].CharLimit = 200
	inputs[7].Width = 50
	if len(host.Tags) > 0 {
		inputs[7].SetValue(strings.Join(host.Tags, ", "))
	}

	// 8: Options input
	inputs[8] = textinput.New()
	inputs[8].Placeholder = "-o StrictHostKeyChecking=no"
	inputs[8].CharLimit = 200
	inputs[8].Width = 50
	if host.Options != "" {
		inputs[8].SetValue(config.FormatSSHOptionsForCommand(host.Options))
	}

	// 9: Remote Command input
	inputs[9] = textinput.New()
	inputs[9].Placeholder = "ls -la, htop, bash"
	inputs[9].CharLimit = 300
	inputs[9].Width = 70
	inputs[9].SetValue(host.RemoteCommand)

	// 10: RequestTTY input
	inputs[10] = textinput.New()
	inputs[10].Placeholder = "yes, no, force, auto"
	inputs[10].CharLimit = 10
	inputs[10].Width = 30
	inputs[10].SetValue(host.RequestTTY)

	return &editFormModel{
		hostInputs:       hostInputs,
		inputs:           inputs,
		focusArea:        focusAreaHosts, // Start with hosts focused for multi-host editing
		focused:          0,
		currentTab:       0, // Start on General tab
		originalName:     hostName,
		originalHosts:    hostNames,
		host:             host,
		configFile:       configFile,
		actualConfigFile: actualConfigFile,
		styles:           styles,
		width:            width,
		height:           height,
	}, nil
}

func (m *editFormModel) Init() tea.Cmd {
	return textinput.Blink
}

// addHostInput adds a new empty host input
func (m *editFormModel) addHostInput() tea.Cmd {
	newInput := textinput.New()
	newInput.Placeholder = "host-name"
	newInput.Focus()

	// Unfocus current input regardless of which area we're in
	if m.focusArea == focusAreaHosts && m.focused < len(m.hostInputs) {
		m.hostInputs[m.focused].Blur()
	} else if m.focusArea == focusAreaProperties && m.focused < len(m.inputs) {
		m.inputs[m.focused].Blur()
	}

	m.hostInputs = append(m.hostInputs, newInput)

	// Move focus to the new host input
	m.focusArea = focusAreaHosts
	m.focused = len(m.hostInputs) - 1

	return textinput.Blink
}

// deleteHostInput removes the currently focused host input
func (m *editFormModel) deleteHostInput() tea.Cmd {
	if len(m.hostInputs) <= 1 || m.focusArea != focusAreaHosts {
		return nil // Can't delete if only one host or not in host area
	}

	// Remove the focused host input
	m.hostInputs = append(m.hostInputs[:m.focused], m.hostInputs[m.focused+1:]...)

	// Adjust focus
	if m.focused >= len(m.hostInputs) {
		m.focused = len(m.hostInputs) - 1
	}

	// Focus the new current input
	if len(m.hostInputs) > 0 {
		m.hostInputs[m.focused].Focus()
	}

	return nil
}

// updateFocus updates the focus state based on current area and index
func (m *editFormModel) updateFocus() tea.Cmd {
	// Blur all inputs first
	for i := range m.hostInputs {
		m.hostInputs[i].Blur()
	}
	for i := range m.inputs {
		m.inputs[i].Blur()
	}

	// Focus the appropriate input
	if m.focusArea == focusAreaHosts {
		if m.focused < len(m.hostInputs) {
			m.hostInputs[m.focused].Focus()
		}
	} else {
		if m.focused < len(m.inputs) {
			m.inputs[m.focused].Focus()
		}
	}

	return textinput.Blink
}

// getPropertiesForCurrentTab returns the property input indices for the current tab
func (m *editFormModel) getPropertiesForCurrentTab() []int {
	switch m.currentTab {
	case 0: // General
		return []int{0, 1, 2, 3, 4, 5, 6, 7} // hostname, user, port, password, identity, proxyjump, proxycommand, tags
	case 1: // Advanced
		return []int{8, 9, 10} // options, remotecommand, requesttty
	default:
		return []int{0, 1, 2, 3, 4, 5, 6, 7}
	}
}

// getFirstPropertyForTab returns the first property index for a given tab
func (m *editFormModel) getFirstPropertyForTab(tab int) int {
	properties := []int{0, 1, 2, 3, 4, 5, 6, 7} // General tab
	if tab == 1 {
		properties = []int{8, 9, 10} // Advanced tab
	}
	if len(properties) > 0 {
		return properties[0]
	}
	return 0
}

// handleEditNavigation handles navigation in the edit form with tab support
func (m *editFormModel) handleEditNavigation(key string) tea.Cmd {
	if m.focusArea == focusAreaHosts {
		// Navigate in hosts area
		if key == "up" || key == "shift+tab" {
			m.focused--
		} else {
			m.focused++
		}

		if m.focused >= len(m.hostInputs) {
			// Move to properties area, keep current tab
			m.focusArea = focusAreaProperties
			// Keep the current tab instead of forcing it to 0
			m.focused = m.getFirstPropertyForTab(m.currentTab)
		} else if m.focused < 0 {
			m.focused = len(m.hostInputs) - 1
		}
	} else {
		// Navigate in properties area within current tab
		currentTabProperties := m.getPropertiesForCurrentTab()

		// Find current position within the tab
		currentPos := 0
		for i, prop := range currentTabProperties {
			if prop == m.focused {
				currentPos = i
				break
			}
		}

		// Handle form submission on last field of Advanced tab
		if key == "enter" && m.currentTab == 1 && currentPos == len(currentTabProperties)-1 {
			return m.submitEditForm()
		}

		// Navigate within current tab
		if key == "up" || key == "shift+tab" {
			currentPos--
		} else {
			currentPos++
		}

		// Handle transitions between areas and tabs
		if currentPos >= len(currentTabProperties) {
			// Move to next area/tab
			if m.currentTab == 0 {
				// Move to advanced tab
				m.currentTab = 1
				m.focused = m.getFirstPropertyForTab(1)
			} else {
				// Move back to hosts area
				m.focusArea = focusAreaHosts
				m.focused = 0
			}
		} else if currentPos < 0 {
			// Move to previous area/tab
			if m.currentTab == 1 {
				// Move to general tab
				m.currentTab = 0
				properties := m.getPropertiesForCurrentTab()
				m.focused = properties[len(properties)-1]
			} else {
				// Move to hosts area
				m.focusArea = focusAreaHosts
				m.focused = len(m.hostInputs) - 1
			}
		} else {
			m.focused = currentTabProperties[currentPos]
		}
	}

	return m.updateFocus()
}

// getMinimumHeight calculates the minimum height needed to display the edit form
func (m *editFormModel) getMinimumHeight() int {
	// Title: 1 line + 2 newlines = 3
	titleLines := 3
	// Config file info: 1 line + 2 newlines = 3
	configLines := 3
	// Host Names section: title (1) + spacing (2) = 3
	hostSectionLines := 3
	// Host inputs: number of hosts * 3 lines each (reduced from 4)
	hostLines := len(m.hostInputs) * 3
	// Properties section: title (1) + spacing (2) = 3
	propertiesSectionLines := 3
	// Tabs: 1 line + 2 newlines = 3
	tabLines := 3
	// Fields in current tab
	var fieldsCount int
	if m.currentTab == 0 {
		fieldsCount = 6 // 6 fields in general tab
	} else {
		fieldsCount = 3 // 3 fields in advanced tab
	}
	// Each field: reduced from 4 to 3 lines per field
	fieldsLines := fieldsCount * 3
	// Help text: 3 lines
	helpLines := 3
	// Error message space when needed: 2 lines
	errorLines := 0 // Only count when there's actually an error
	if m.err != "" {
		errorLines = 2
	}

	return titleLines + configLines + hostSectionLines + hostLines + propertiesSectionLines + tabLines + fieldsLines + helpLines + errorLines + 1 // +1 minimal safety margin
}

// isHeightSufficient checks if the current terminal height is sufficient
func (m *editFormModel) isHeightSufficient() bool {
	return m.height >= m.getMinimumHeight()
}

// renderHeightWarning renders a warning message when height is insufficient
func (m *editFormModel) renderHeightWarning() string {
	required := m.getMinimumHeight()
	current := m.height

	warning := m.styles.ErrorText.Render(i18n.T("form.height_warning_title"))
	details := m.styles.FormField.Render(fmt.Sprintf(i18n.T("form.height_warning_details"), current, required))
	instruction := m.styles.FormHelp.Render(i18n.T("form.height_warning_resize"))
	instruction2 := m.styles.FormHelp.Render(i18n.T("form.height_warning_cancel"))

	return warning + "\n\n" + details + "\n\n" + instruction + "\n" + instruction2
}

func (m *editFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.err = ""
			return m, func() tea.Msg { return editFormCancelMsg{} }

		case "ctrl+s":
			// Allow submission from any field with Ctrl+S (Save)
			return m, m.submitEditForm()

		case "ctrl+j":
			// Switch to next tab
			m.currentTab = (m.currentTab + 1) % 2
			// If we're in hosts area, stay there. If in properties, go to the first field of the new tab
			if m.focusArea == focusAreaProperties {
				m.focused = m.getFirstPropertyForTab(m.currentTab)
			}
			return m, m.updateFocus()

		case "ctrl+k":
			// Switch to previous tab
			m.currentTab = (m.currentTab - 1 + 2) % 2
			// If we're in hosts area, stay there. If in properties, go to the first field of the new tab
			if m.focusArea == focusAreaProperties {
				m.focused = m.getFirstPropertyForTab(m.currentTab)
			}
			return m, m.updateFocus()

		case "tab", "shift+tab", "enter", "up", "down":
			return m, m.handleEditNavigation(msg.String())

		case "ctrl+a":
			// Add a new host input
			return m, m.addHostInput()

		case "ctrl+d":
			// Delete the currently focused host (if more than one exists)
			if m.focusArea == focusAreaHosts && len(m.hostInputs) > 1 {
				return m, m.deleteHostInput()
			}
		}

	case editFormSubmitMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			// Success: let the wrapper handle this
			// In TUI mode, this will be handled by the parent
			// In standalone mode, the wrapper will quit
		}
		return m, nil
	}

	// Update host inputs
	hostCmd := make([]tea.Cmd, len(m.hostInputs))
	for i := range m.hostInputs {
		m.hostInputs[i], hostCmd[i] = m.hostInputs[i].Update(msg)
	}
	cmds = append(cmds, hostCmd...)

	// Update property inputs
	propCmd := make([]tea.Cmd, len(m.inputs))
	for i := range m.inputs {
		m.inputs[i], propCmd[i] = m.inputs[i].Update(msg)
	}
	cmds = append(cmds, propCmd...)

	return m, tea.Batch(cmds...)
}

func (m *editFormModel) View() string {
	// Check if terminal height is sufficient
	if !m.isHeightSufficient() {
		return m.renderHeightWarning()
	}

	var b strings.Builder

	if m.err != "" {
		b.WriteString(m.styles.Error.Render("Error: " + m.err))
		b.WriteString("\n\n")
	}

	b.WriteString(m.styles.Header.Render(i18n.T("form.edit_title")))
	b.WriteString("\n\n")

	if m.host != nil && m.host.SourceFile != "" {
		labelStyle := m.styles.FormField
		pathStyle := m.styles.FormField
		configInfo := labelStyle.Render(i18n.T("form.config_file")) + pathStyle.Render(formatConfigFile(m.host.SourceFile))
		b.WriteString(configInfo)
	}

	b.WriteString("\n\n")

	// Host Names Section
	b.WriteString(m.styles.FormTitle.Render(i18n.T("form.host_names_section")))
	b.WriteString("\n\n")

	for i, hostInput := range m.hostInputs {
		hostStyle := m.styles.FormField
		if m.focusArea == focusAreaHosts && m.focused == i {
			hostStyle = m.styles.FocusedLabel
		}
		b.WriteString(hostStyle.Render(fmt.Sprintf(i18n.T("form.host_name_num"), i+1)))
		b.WriteString("\n")
		b.WriteString(hostInput.View())
		b.WriteString("\n\n")
	}

	// Properties Section
	b.WriteString(m.styles.FormTitle.Render(i18n.T("form.common_properties_section")))
	b.WriteString("\n\n")

	// Render tabs for properties
	b.WriteString(m.renderEditTabs())
	b.WriteString("\n\n")

	// Render current tab content
	switch m.currentTab {
	case 0: // General
		b.WriteString(m.renderEditGeneralTab())
	case 1: // Advanced
		b.WriteString(m.renderEditAdvancedTab())
	}

	if m.err != "" {
		b.WriteString(m.styles.Error.Render("Error: " + m.err))
		b.WriteString("\n\n")
	}

	// Show different help based on number of hosts
	if len(m.hostInputs) > 1 {
		b.WriteString(m.styles.FormHelp.Render(i18n.T("form.help_multi_host")))
		b.WriteString("\n")
	} else {
		b.WriteString(m.styles.FormHelp.Render(i18n.T("form.help_single_host")))
		b.WriteString("\n")
	}
	b.WriteString(m.styles.FormHelp.Render(i18n.T("form.help_save_cancel")))

	return b.String()
}

// renderEditTabs renders the tab headers for properties
func (m *editFormModel) renderEditTabs() string {
	var generalTab, advancedTab string
	generalLabel := i18n.T("form.tab_general")
	advancedLabel := i18n.T("form.tab_advanced")

	if m.currentTab == 0 {
		generalTab = m.styles.FocusedLabel.Render(fmt.Sprintf("[ %s ]", generalLabel))
		advancedTab = m.styles.FormField.Render(fmt.Sprintf("  %s  ", advancedLabel))
	} else {
		generalTab = m.styles.FormField.Render(fmt.Sprintf("  %s  ", generalLabel))
		advancedTab = m.styles.FocusedLabel.Render(fmt.Sprintf("[ %s ]", advancedLabel))
	}

	return generalTab + "  " + advancedTab
}

// renderEditGeneralTab renders the general tab content for properties
func (m *editFormModel) renderEditGeneralTab() string {
	var b strings.Builder

	fields := []struct {
		index int
		label string
	}{
		{0, i18n.T("form.hostname_ip")},
		{1, i18n.T("form.user")},
		{2, i18n.T("form.port")},
		{3, i18n.T("form.password")},
		{4, i18n.T("form.identity_file")},
		{5, i18n.T("form.proxy_jump")},
		{6, i18n.T("form.proxy_command")},
		{7, i18n.T("form.tags")},
	}

	for _, field := range fields {
		fieldStyle := m.styles.FormField
		if m.focusArea == focusAreaProperties && m.focused == field.index {
			fieldStyle = m.styles.FocusedLabel
		}
		b.WriteString(fieldStyle.Render(field.label))
		b.WriteString("\n")
		b.WriteString(m.inputs[field.index].View())
		b.WriteString("\n")
		if field.index == 7 && m.focusArea == focusAreaProperties && m.focused == 7 {
			b.WriteString(m.styles.FormHelp.Render(i18n.T("form.tip_hidden")))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	return b.String()
}

// renderEditAdvancedTab renders the advanced tab content for properties
func (m *editFormModel) renderEditAdvancedTab() string {
	var b strings.Builder

	fields := []struct {
		index int
		label string
	}{
		{8, i18n.T("form.ssh_options")},
		{9, i18n.T("form.remote_command")},
		{10, i18n.T("form.request_tty")},
	}

	for _, field := range fields {
		fieldStyle := m.styles.FormField
		if m.focusArea == focusAreaProperties && m.focused == field.index {
			fieldStyle = m.styles.FocusedLabel
		}
		b.WriteString(fieldStyle.Render(field.label))
		b.WriteString("\n")
		b.WriteString(m.inputs[field.index].View())
		b.WriteString("\n\n")
	}

	return b.String()
}

// Standalone wrapper for edit form
type standaloneEditForm struct {
	*editFormModel
}

func (m standaloneEditForm) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case editFormSubmitMsg:
		if msg.err != nil {
			m.editFormModel.err = msg.err.Error()
			return m, nil
		} else {
			// Success: quit the program
			return m, tea.Quit
		}
	case editFormCancelMsg:
		return m, tea.Quit
	}

	newForm, cmd := m.editFormModel.Update(msg)
	m.editFormModel = newForm.(*editFormModel)
	return m, cmd
}

// RunEditForm runs the edit form as a standalone program
func RunEditForm(hostName string, configFile string) error {
	styles := NewStyles(80) // Default width
	editForm, err := NewEditForm(hostName, styles, 80, 24, configFile)
	if err != nil {
		return err
	}

	m := standaloneEditForm{editForm}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func (m *editFormModel) submitEditForm() tea.Cmd {
	return func() tea.Msg {
		// Collect host names
		var hostNames []string
		for _, input := range m.hostInputs {
			name := strings.TrimSpace(input.Value())
			if name != "" {
				hostNames = append(hostNames, name)
			}
		}

		if len(hostNames) == 0 {
			return editFormSubmitMsg{err: fmt.Errorf("%s", i18n.T("form.err_host_name_req"))}
		}

		// Get property values using direct indices
		hostname := strings.TrimSpace(m.inputs[0].Value())
		user := strings.TrimSpace(m.inputs[1].Value())
		port := strings.TrimSpace(m.inputs[2].Value())
		password := strings.TrimSpace(m.inputs[3].Value())
		identity := strings.TrimSpace(m.inputs[4].Value())
		proxyJump := strings.TrimSpace(m.inputs[5].Value())
		proxyCommand := strings.TrimSpace(m.inputs[6].Value())
		tagsStr := strings.TrimSpace(m.inputs[7].Value())
		options := config.ParseSSHOptionsFromCommand(strings.TrimSpace(m.inputs[8].Value()))
		remoteCommand := strings.TrimSpace(m.inputs[9].Value())
		requestTTY := strings.TrimSpace(m.inputs[10].Value())

		// Set defaults
		if port == "" {
			port = "22"
		}

		// Validate hostname
		if hostname == "" {
			return editFormSubmitMsg{err: fmt.Errorf("%s", i18n.T("form.err_hostname_req"))}
		}

		// Validate all host names
		for _, hostName := range hostNames {
			if err := validation.ValidateHost(hostName, hostname, port, identity); err != nil {
				return editFormSubmitMsg{err: err}
			}
		}

		// Parse tags
		var tags []string
		if tagsStr != "" {
			for _, tag := range strings.Split(tagsStr, ",") {
				tag = strings.TrimSpace(tag)
				tag = strings.TrimPrefix(tag, "#")
				if tag != "" {
					tags = append(tags, tag)
				}
			}
		}

		// Create the common host configuration
		commonHost := config.SSHHost{
			Hostname:      hostname,
			User:          user,
			Port:          port,
			Identity:      identity,
			ProxyJump:     proxyJump,
			ProxyCommand:  proxyCommand,
			Options:       options,
			RemoteCommand: remoteCommand,
			RequestTTY:    requestTTY,
			Tags:          tags,
		}

		var err error
		if len(hostNames) == 1 && len(m.originalHosts) == 1 {
			// Single host editing
			commonHost.Name = hostNames[0]
			if m.actualConfigFile != "" {
				err = config.UpdateSSHHostInFile(m.originalName, commonHost, m.actualConfigFile)
			} else {
				err = config.UpdateSSHHost(m.originalName, commonHost)
			}
		} else {
			// Multi-host editing or conversion from single to multi
			err = config.UpdateMultiHostBlock(m.originalHosts, hostNames, commonHost, m.actualConfigFile)
		}

		if err == nil {
			for _, name := range hostNames {
				if m.originalName != name {
					_ = credential.RenameHost(m.originalName, name)
				}
				if password != "" {
					_ = credential.SetPassword(name, password)
				} else {
					_ = credential.DeletePassword(name)
				}
			}
		}

		return editFormSubmitMsg{hostname: hostNames[0], err: err}
	}
}
