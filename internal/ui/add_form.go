package ui

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/credential"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/validation"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type addFormModel struct {
	inputs       []textinput.Model
	focused      int
	currentTab   int // 0 = General, 1 = Advanced
	err          string
	styles       Styles
	success      bool
	width        int
	height       int
	configFile   string
	scrollOffset int
}

// NewAddForm creates a new add form model
func NewAddForm(hostname string, styles Styles, width, height int, configFile string) *addFormModel {
	// Get current user for default
	currentUser, _ := user.Current()
	defaultUser := "root"
	if currentUser != nil {
		defaultUser = currentUser.Username
	}

	// Find default identity file
	homeDir, _ := os.UserHomeDir()
	defaultIdentity := filepath.Join(homeDir, ".ssh", "id_rsa")

	// Check for other common key types
	keyTypes := []string{"id_ed25519", "id_ecdsa", "id_rsa"}
	for _, keyType := range keyTypes {
		keyPath := filepath.Join(homeDir, ".ssh", keyType)
		if _, err := os.Stat(keyPath); err == nil {
			defaultIdentity = keyPath
			break
		}
	}

	inputs := make([]textinput.Model, numAddInputs)

	// Name input
	inputs[nameInput] = textinput.New()
	inputs[nameInput].Placeholder = "server-name"
	inputs[nameInput].Focus()
	inputs[nameInput].CharLimit = 50
	inputs[nameInput].Width = 30
	if hostname != "" {
		inputs[nameInput].SetValue(hostname)
	}

	// Hostname input
	inputs[hostnameInput] = textinput.New()
	inputs[hostnameInput].Placeholder = "192.168.1.100 or example.com"
	inputs[hostnameInput].CharLimit = 100
	inputs[hostnameInput].Width = 30

	// User input
	inputs[userInput] = textinput.New()
	inputs[userInput].Placeholder = defaultUser
	inputs[userInput].CharLimit = 50
	inputs[userInput].Width = 30

	// Port input
	inputs[portInput] = textinput.New()
	inputs[portInput].Placeholder = "22"
	inputs[portInput].CharLimit = 5
	inputs[portInput].Width = 30

	// Password input
	inputs[passwordInput] = textinput.New()
	inputs[passwordInput].Placeholder = i18n.T("form.password_placeholder")
	inputs[passwordInput].EchoMode = textinput.EchoPassword
	inputs[passwordInput].EchoCharacter = '•'
	inputs[passwordInput].CharLimit = 100
	inputs[passwordInput].Width = 30

	// Identity input
	inputs[identityInput] = textinput.New()
	inputs[identityInput].Placeholder = defaultIdentity
	inputs[identityInput].CharLimit = 200
	inputs[identityInput].Width = 50

	// ProxyJump input
	inputs[proxyJumpInput] = textinput.New()
	inputs[proxyJumpInput].Placeholder = "user@jump-host:port or existing-host-name"
	inputs[proxyJumpInput].CharLimit = 200
	inputs[proxyJumpInput].Width = 50

	// ProxyCommand input
	inputs[proxyCommandInput] = textinput.New()
	inputs[proxyCommandInput].Placeholder = "ssh -W %h:%p Jumphost"
	inputs[proxyCommandInput].CharLimit = 200
	inputs[proxyCommandInput].Width = 50

	// SSH Options input
	inputs[optionsInput] = textinput.New()
	inputs[optionsInput].Placeholder = "-o Compression=yes -o ServerAliveInterval=60"
	inputs[optionsInput].CharLimit = 500
	inputs[optionsInput].Width = 70

	// Tags input
	inputs[tagsInput] = textinput.New()
	inputs[tagsInput].Placeholder = "production, web, database"
	inputs[tagsInput].CharLimit = 200
	inputs[tagsInput].Width = 50

	// Remote Command input
	inputs[remoteCommandInput] = textinput.New()
	inputs[remoteCommandInput].Placeholder = "ls -la, htop, bash"
	inputs[remoteCommandInput].CharLimit = 300
	inputs[remoteCommandInput].Width = 70

	// RequestTTY input
	inputs[requestTTYInput] = textinput.New()
	inputs[requestTTYInput].Placeholder = "yes, no, force, auto"
	inputs[requestTTYInput].CharLimit = 10
	inputs[requestTTYInput].Width = 30

	return &addFormModel{
		inputs:     inputs,
		focused:    nameInput,
		currentTab: tabGeneral, // Start on General tab
		styles:     styles,
		width:      width,
		height:     height,
		configFile: configFile,
	}
}

const (
	tabGeneral = iota
	tabAdvanced
)

const (
	nameInput = iota
	hostnameInput
	userInput
	portInput
	passwordInput
	identityInput
	proxyJumpInput
	proxyCommandInput
	tagsInput
	// Advanced tab inputs
	optionsInput
	remoteCommandInput
	requestTTYInput
	numAddInputs
)

// Messages for communication with parent model
type addFormSubmitMsg struct {
	hostname string
	err      error
}

type addFormCancelMsg struct{}

func (m *addFormModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *addFormModel) Update(msg tea.Msg) (*addFormModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.styles = NewStyles(m.width)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, func() tea.Msg { return addFormCancelMsg{} }

		case "ctrl+s":
			// Allow submission from any field with Ctrl+S (Save)
			return m, m.submitForm()

		case "ctrl+j":
			// Switch to next tab
			m.currentTab = (m.currentTab + 1) % 2
			m.focused = m.getFirstInputForTab(m.currentTab)
			m.scrollOffset = 0
			return m, m.updateFocus()

		case "ctrl+k":
			// Switch to previous tab
			m.currentTab = (m.currentTab - 1 + 2) % 2
			m.focused = m.getFirstInputForTab(m.currentTab)
			m.scrollOffset = 0
			return m, m.updateFocus()

		case "tab", "shift+tab", "enter", "up", "down":
			return m, m.handleNavigation(msg.String())
		}

	case addFormSubmitMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.success = true
			m.err = ""
			// Don't quit here, let parent handle the success
		}
		return m, nil
	}

	// Update inputs
	cmd := make([]tea.Cmd, len(m.inputs))
	for i := range m.inputs {
		m.inputs[i], cmd[i] = m.inputs[i].Update(msg)
	}
	cmds = append(cmds, cmd...)

	return m, tea.Batch(cmds...)
}

// getFirstInputForTab returns the first input index for a given tab
func (m *addFormModel) getFirstInputForTab(tab int) int {
	switch tab {
	case tabGeneral:
		return nameInput
	case tabAdvanced:
		return optionsInput
	default:
		return nameInput
	}
}

// getInputsForCurrentTab returns the input indices for the current tab
func (m *addFormModel) getInputsForCurrentTab() []int {
	switch m.currentTab {
	case tabGeneral:
		return []int{nameInput, hostnameInput, userInput, portInput, passwordInput, identityInput, proxyJumpInput, proxyCommandInput, tagsInput}
	case tabAdvanced:
		return []int{optionsInput, remoteCommandInput, requestTTYInput}
	default:
		return []int{nameInput, hostnameInput, userInput, portInput, passwordInput, identityInput, proxyJumpInput, proxyCommandInput, tagsInput}
	}
}

// updateFocus updates focus for inputs
func (m *addFormModel) updateFocus() tea.Cmd {
	var cmds []tea.Cmd
	for i := range m.inputs {
		if i == m.focused {
			cmds = append(cmds, m.inputs[i].Focus())
		} else {
			m.inputs[i].Blur()
		}
	}
	return tea.Batch(cmds...)
}

// handleNavigation handles tab/arrow navigation within the current tab
func (m *addFormModel) handleNavigation(key string) tea.Cmd {
	currentTabInputs := m.getInputsForCurrentTab()

	// Find current position within the tab
	currentPos := 0
	for i, input := range currentTabInputs {
		if input == m.focused {
			currentPos = i
			break
		}
	}

	// Handle form submission on last field of Advanced tab
	if key == "enter" && m.currentTab == tabAdvanced && currentPos == len(currentTabInputs)-1 {
		return m.submitForm()
	}

	// Navigate within current tab
	if key == "up" || key == "shift+tab" {
		currentPos--
	} else {
		currentPos++
	}

	// Handle transitions between tabs
	if currentPos >= len(currentTabInputs) {
		// Move to next tab
		if m.currentTab == tabGeneral {
			// Move to advanced tab
			m.currentTab = tabAdvanced
			m.focused = m.getFirstInputForTab(tabAdvanced)
			return m.updateFocus()
		} else {
			// Wrap around to first field of current tab
			currentPos = 0
		}
	} else if currentPos < 0 {
		// Move to previous tab
		if m.currentTab == tabAdvanced {
			// Move to general tab
			m.currentTab = tabGeneral
			currentTabInputs = m.getInputsForCurrentTab()
			currentPos = len(currentTabInputs) - 1
		} else {
			// Wrap around to last field of current tab
			currentPos = len(currentTabInputs) - 1
		}
	}

	m.focused = currentTabInputs[currentPos]
	return m.updateFocus()
}

type formFieldDef struct {
	index int
	label string
}

func (m *addFormModel) getFieldsForTab(tab int) []formFieldDef {
	if tab == tabGeneral {
		return []formFieldDef{
			{nameInput, i18n.T("form.host_name")},
			{hostnameInput, i18n.T("form.hostname_ip")},
			{userInput, i18n.T("form.user")},
			{portInput, i18n.T("form.port")},
			{passwordInput, i18n.T("form.password")},
			{identityInput, i18n.T("form.identity_file")},
			{proxyJumpInput, i18n.T("form.proxy_jump")},
			{proxyCommandInput, i18n.T("form.proxy_command")},
			{tagsInput, i18n.T("form.tags")},
		}
	}
	return []formFieldDef{
		{optionsInput, i18n.T("form.ssh_options")},
		{remoteCommandInput, i18n.T("form.remote_command")},
		{requestTTYInput, i18n.T("form.request_tty")},
	}
}

// renderTabs renders the tab headers
func (m *addFormModel) renderTabs() string {
	var generalTab, advancedTab string
	generalLabel := i18n.T("form.tab_general")
	advancedLabel := i18n.T("form.tab_advanced")

	if m.currentTab == tabGeneral {
		generalTab = m.styles.FocusedLabel.Render(fmt.Sprintf("[ %s ]", generalLabel))
		advancedTab = m.styles.FormField.Render(fmt.Sprintf("  %s  ", advancedLabel))
	} else {
		generalTab = m.styles.FormField.Render(fmt.Sprintf("  %s  ", generalLabel))
		advancedTab = m.styles.FocusedLabel.Render(fmt.Sprintf("[ %s ]", advancedLabel))
	}

	return generalTab + "  " + advancedTab
}

func (m *addFormModel) View() string {
	if m.success {
		return ""
	}

	// 1. Header (Title + Tabs)
	var headerLines []string
	headerLines = append(headerLines, m.styles.FormTitle.Render(i18n.T("form.add_title")))
	headerLines = append(headerLines, m.renderTabs())

	// 2. Footer (Errors + Help)
	var footerLines []string
	if m.err != "" {
		footerLines = append(footerLines, m.styles.Error.Render("Error: "+m.err))
	}
	help1 := m.styles.FormHelp.Render(i18n.T("form.help_add_1"))
	help2 := m.styles.FormHelp.Render(i18n.T("form.help_add_2")) + " • " + m.styles.FormHelp.Render(i18n.T("form.help_required"))
	footerLines = append(footerLines, help1, help2)

	// 3. Body lines & field line positions
	type fieldPos struct {
		startLine int
		endLine   int
	}
	fieldPositions := make(map[int]fieldPos)
	var bodyLines []string

	fields := m.getFieldsForTab(m.currentTab)
	for _, field := range fields {
		startLine := len(bodyLines)

		fieldStyle := m.styles.FormField
		if m.focused == field.index {
			fieldStyle = m.styles.FocusedLabel
		}
		bodyLines = append(bodyLines, fieldStyle.Render(field.label))
		bodyLines = append(bodyLines, m.inputs[field.index].View())

		if field.index == tagsInput && m.focused == tagsInput {
			bodyLines = append(bodyLines, m.styles.FormHelp.Render(i18n.T("form.tip_hidden")))
		}
		bodyLines = append(bodyLines, "") // spacing
		endLine := len(bodyLines) - 1
		fieldPositions[field.index] = fieldPos{startLine: startLine, endLine: endLine}
	}

	// 4. Viewport calculation & auto-scroll
	totalHeight := m.height
	if totalHeight <= 0 {
		totalHeight = 24
	}

	reservedLines := len(headerLines) + len(footerLines) + 2
	viewportHeight := totalHeight - reservedLines
	if viewportHeight < 3 {
		viewportHeight = 3
	}

	if len(bodyLines) <= viewportHeight {
		m.scrollOffset = 0
	} else {
		if pos, ok := fieldPositions[m.focused]; ok {
			if pos.startLine < m.scrollOffset {
				m.scrollOffset = pos.startLine
			}
			if pos.endLine >= m.scrollOffset+viewportHeight {
				m.scrollOffset = pos.endLine - viewportHeight + 1
			}
		}

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
	visibleBody := bodyLines[m.scrollOffset:endIdx]

	var b strings.Builder
	for _, line := range headerLines {
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")

	for _, line := range visibleBody {
		b.WriteString(line)
		b.WriteString("\n")
	}

	for _, line := range footerLines {
		b.WriteString(line)
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

// Standalone wrapper for add form
type standaloneAddForm struct {
	*addFormModel
}

func (m standaloneAddForm) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case addFormSubmitMsg:
		if msg.err != nil {
			m.addFormModel.err = msg.err.Error()
		} else {
			m.addFormModel.success = true
			return m, tea.Quit
		}
		return m, nil
	case addFormCancelMsg:
		return m, tea.Quit
	}

	newForm, cmd := m.addFormModel.Update(msg)
	m.addFormModel = newForm
	return m, cmd
}

// RunAddForm provides backward compatibility for standalone add form
func RunAddForm(hostname string, configFile string) error {
	styles := NewStyles(80)
	addForm := NewAddForm(hostname, styles, 80, 24, configFile)
	m := standaloneAddForm{addForm}

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m *addFormModel) submitForm() tea.Cmd {
	return func() tea.Msg {
		// Get values
		name := strings.TrimSpace(m.inputs[nameInput].Value())
		hostname := strings.TrimSpace(m.inputs[hostnameInput].Value())
		user := strings.TrimSpace(m.inputs[userInput].Value())
		port := strings.TrimSpace(m.inputs[portInput].Value())
		identity := strings.TrimSpace(m.inputs[identityInput].Value())
		proxyJump := strings.TrimSpace(m.inputs[proxyJumpInput].Value())
		proxyCommand := strings.TrimSpace(m.inputs[proxyCommandInput].Value())
		options := strings.TrimSpace(m.inputs[optionsInput].Value())
		remoteCommand := strings.TrimSpace(m.inputs[remoteCommandInput].Value())
		requestTTY := strings.TrimSpace(m.inputs[requestTTYInput].Value())

		// Set defaults
		if user == "" {
			user = m.inputs[userInput].Placeholder
		}
		if port == "" {
			port = "22"
		}
		// Do not auto-fill identity with placeholder if left empty; keep it empty so it's optional

		// Validate all fields
		if err := validation.ValidateHost(name, hostname, port, identity); err != nil {
			return addFormSubmitMsg{err: err}
		}

		tagsStr := strings.TrimSpace(m.inputs[tagsInput].Value())
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

		// Create host configuration
		host := config.SSHHost{
			Name:          name,
			Hostname:      hostname,
			User:          user,
			Port:          port,
			Identity:      identity,
			ProxyJump:     proxyJump,
			ProxyCommand:  proxyCommand,
			Options:       config.ParseSSHOptionsFromCommand(options),
			RemoteCommand: remoteCommand,
			RequestTTY:    requestTTY,
			Tags:          tags,
		}

		// Add to config
		var err error
		if m.configFile != "" {
			err = config.AddSSHHostToFile(host, m.configFile)
		} else {
			err = config.AddSSHHost(host)
		}

		if err == nil {
			if pass := strings.TrimSpace(m.inputs[passwordInput].Value()); pass != "" {
				_ = credential.SetPassword(name, pass)
			}
		}

		return addFormSubmitMsg{hostname: name, err: err}
	}
}
