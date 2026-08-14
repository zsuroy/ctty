package ui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/history"
	"github.com/zsuroy/ctty/internal/i18n"
)

// Input field indices for port forward form
const (
	pfTypeInput = iota
	pfLocalPortInput
	pfRemoteHostInput
	pfRemotePortInput
	pfBindAddressInput
)

type portForwardModel struct {
	inputs         []textinput.Model
	focused        int
	forwardType    PortForwardType
	hostName       string
	err            string
	styles         Styles
	width          int
	height         int
	configFile     string
	historyManager *history.HistoryManager
	scrollOffset   int
}

// portForwardSubmitMsg is sent when the port forward form is submitted
type portForwardSubmitMsg struct {
	err     error
	sshArgs []string
}

// portForwardCancelMsg is sent when the port forward form is cancelled
type portForwardCancelMsg struct{}

// NewPortForwardForm creates a new port forward form model
func NewPortForwardForm(hostName string, styles Styles, width, height int, configFile string, historyManager *history.HistoryManager) *portForwardModel {
	inputs := make([]textinput.Model, 5)

	// Forward type input (display only, controlled by arrow keys)
	inputs[pfTypeInput] = textinput.New()
	inputs[pfTypeInput].Placeholder = "Use ←/→ to change forward type"
	inputs[pfTypeInput].Focus()
	inputs[pfTypeInput].Width = 40

	// Local port input
	inputs[pfLocalPortInput] = textinput.New()
	inputs[pfLocalPortInput].Placeholder = "8080"
	inputs[pfLocalPortInput].CharLimit = 5
	inputs[pfLocalPortInput].Width = 20

	// Remote host input
	inputs[pfRemoteHostInput] = textinput.New()
	inputs[pfRemoteHostInput].Placeholder = "localhost"
	inputs[pfRemoteHostInput].CharLimit = 100
	inputs[pfRemoteHostInput].Width = 30
	inputs[pfRemoteHostInput].SetValue("localhost")

	// Remote port input
	inputs[pfRemotePortInput] = textinput.New()
	inputs[pfRemotePortInput].Placeholder = "80"
	inputs[pfRemotePortInput].CharLimit = 5
	inputs[pfRemotePortInput].Width = 20

	// Bind address input (optional)
	inputs[pfBindAddressInput] = textinput.New()
	inputs[pfBindAddressInput].Placeholder = "127.0.0.1 (optional)"
	inputs[pfBindAddressInput].CharLimit = 50
	inputs[pfBindAddressInput].Width = 30

	pf := &portForwardModel{
		inputs:         inputs,
		focused:        0,
		forwardType:    LocalForward,
		hostName:       hostName,
		styles:         styles,
		width:          width,
		height:         height,
		configFile:     configFile,
		historyManager: historyManager,
	}

	// Load previous port forwarding configuration if available
	pf.loadPreviousConfig()

	// Initialize input visibility
	pf.updateInputVisibility()

	return pf
}

func (m *portForwardModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *portForwardModel) Update(msg tea.Msg) (*portForwardModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			return m, func() tea.Msg { return portForwardCancelMsg{} }

		case "enter":
			nextField := m.getNextValidField(m.focused)
			if nextField != -1 {
				// Move to next valid input
				m.inputs[m.focused].Blur()
				m.focused = nextField
				m.inputs[m.focused].Focus()
				return m, textinput.Blink
			} else {
				// Submit form
				return m, m.submitForm()
			}

		case "shift+tab", "up":
			prevField := m.getPrevValidField(m.focused)
			if prevField != -1 {
				m.inputs[m.focused].Blur()
				m.focused = prevField
				m.inputs[m.focused].Focus()
				return m, textinput.Blink
			}

		case "tab", "down":
			nextField := m.getNextValidField(m.focused)
			if nextField != -1 {
				m.inputs[m.focused].Blur()
				m.focused = nextField
				m.inputs[m.focused].Focus()
				return m, textinput.Blink
			}

		case "left", "right":
			if m.focused == pfTypeInput {
				// Change forward type
				if msg.String() == "left" {
					if m.forwardType > 0 {
						m.forwardType--
					} else {
						m.forwardType = DynamicForward
					}
				} else {
					if m.forwardType < DynamicForward {
						m.forwardType++
					} else {
						m.forwardType = LocalForward
					}
				}
				m.inputs[pfTypeInput].SetValue(m.forwardType.String())
				m.updateInputVisibility()

				// Ensure focused field is valid for the new type
				validFields := m.getValidFields()
				validFocus := false
				for _, field := range validFields {
					if field == m.focused {
						validFocus = true
						break
					}
				}
				if !validFocus && len(validFields) > 0 {
					m.inputs[m.focused].Blur()
					m.focused = validFields[0]
					m.inputs[m.focused].Focus()
				}

				return m, nil
			}
		}
	}

	// Update the focused input
	m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
	return m, cmd
}

func (m *portForwardModel) updateInputVisibility() {
	// Reset all inputs visibility
	for i := range m.inputs {
		if i != pfTypeInput {
			m.inputs[i].Placeholder = ""
		}
	}

	switch m.forwardType {
	case LocalForward:
		m.inputs[pfLocalPortInput].Placeholder = i18n.T("pf.local_port_placeholder")
		m.inputs[pfRemoteHostInput].Placeholder = i18n.T("pf.remote_host_placeholder")
		m.inputs[pfRemotePortInput].Placeholder = i18n.T("pf.remote_port_placeholder")
		m.inputs[pfBindAddressInput].Placeholder = i18n.T("pf.bind_address_placeholder_local")
	case RemoteForward:
		m.inputs[pfLocalPortInput].Placeholder = i18n.T("pf.remote_port_placeholder")
		m.inputs[pfRemoteHostInput].Placeholder = i18n.T("pf.local_host_placeholder")
		m.inputs[pfRemotePortInput].Placeholder = i18n.T("pf.local_port_placeholder")
		m.inputs[pfBindAddressInput].Placeholder = i18n.T("pf.bind_address_placeholder")
	case DynamicForward:
		m.inputs[pfLocalPortInput].Placeholder = i18n.T("pf.socks_port_placeholder")
		m.inputs[pfRemoteHostInput].Placeholder = ""
		m.inputs[pfRemotePortInput].Placeholder = ""
		m.inputs[pfBindAddressInput].Placeholder = i18n.T("pf.bind_address_placeholder_dynamic")
	}
}

func (m *portForwardModel) View() string {
	// 1. Header (Title + Host Info)
	var headerLines []string
	headerLines = append(headerLines, m.styles.Header.Render(i18n.T("pf.title")))
	headerLines = append(headerLines, m.styles.HelpText.Render(fmt.Sprintf(i18n.T("pf.host_info"), m.hostName)))

	// 2. Footer (Errors + Help)
	var footerLines []string
	if m.err != "" {
		footerLines = append(footerLines, m.styles.Error.Render(i18n.T("pf.error_prefix")+m.err))
	}
	footerLines = append(footerLines, m.styles.HelpText.Render(i18n.T("pf.help_text")))

	// 3. Body lines & field line positions
	type fieldPos struct {
		startLine int
		endLine   int
	}
	fieldPositions := make(map[int]fieldPos)
	var bodyLines []string

	// Forward type field
	typeStart := len(bodyLines)
	typeLabel := i18n.T("pf.forward_type_label")
	if m.focused == pfTypeInput {
		typeLabel = m.styles.FocusedLabel.Render(typeLabel)
	} else {
		typeLabel = m.styles.Label.Render(typeLabel)
	}
	bodyLines = append(bodyLines, typeLabel)
	bodyLines = append(bodyLines, m.inputs[pfTypeInput].View())
	bodyLines = append(bodyLines, m.styles.HelpText.Render(i18n.T("pf.use_arrows")))
	bodyLines = append(bodyLines, "")
	fieldPositions[pfTypeInput] = fieldPos{startLine: typeStart, endLine: len(bodyLines) - 1}

	switch m.forwardType {
	case LocalForward:
		bodyLines = append(bodyLines, m.styles.HelpText.Render(i18n.T("pf.local_desc")))
		bodyLines = append(bodyLines, "")

		// Local port
		lpStart := len(bodyLines)
		lpLabel := i18n.T("pf.local_port_label")
		if m.focused == pfLocalPortInput {
			lpLabel = m.styles.FocusedLabel.Render(lpLabel)
		} else {
			lpLabel = m.styles.Label.Render(lpLabel)
		}
		bodyLines = append(bodyLines, lpLabel)
		bodyLines = append(bodyLines, m.inputs[pfLocalPortInput].View())
		bodyLines = append(bodyLines, "")
		fieldPositions[pfLocalPortInput] = fieldPos{startLine: lpStart, endLine: len(bodyLines) - 1}

		// Remote host
		rhStart := len(bodyLines)
		rhLabel := i18n.T("pf.remote_host_label")
		if m.focused == pfRemoteHostInput {
			rhLabel = m.styles.FocusedLabel.Render(rhLabel)
		} else {
			rhLabel = m.styles.Label.Render(rhLabel)
		}
		bodyLines = append(bodyLines, rhLabel)
		bodyLines = append(bodyLines, m.inputs[pfRemoteHostInput].View())
		bodyLines = append(bodyLines, "")
		fieldPositions[pfRemoteHostInput] = fieldPos{startLine: rhStart, endLine: len(bodyLines) - 1}

		// Remote port
		rpStart := len(bodyLines)
		rpLabel := i18n.T("pf.remote_port_label")
		if m.focused == pfRemotePortInput {
			rpLabel = m.styles.FocusedLabel.Render(rpLabel)
		} else {
			rpLabel = m.styles.Label.Render(rpLabel)
		}
		bodyLines = append(bodyLines, rpLabel)
		bodyLines = append(bodyLines, m.inputs[pfRemotePortInput].View())
		bodyLines = append(bodyLines, "")
		fieldPositions[pfRemotePortInput] = fieldPos{startLine: rpStart, endLine: len(bodyLines) - 1}

	case RemoteForward:
		bodyLines = append(bodyLines, m.styles.HelpText.Render(i18n.T("pf.remote_desc")))
		bodyLines = append(bodyLines, "")

		// Remote port (using pfLocalPortInput)
		rpStart := len(bodyLines)
		rpLabel := i18n.T("pf.remote_port_label")
		if m.focused == pfLocalPortInput {
			rpLabel = m.styles.FocusedLabel.Render(rpLabel)
		} else {
			rpLabel = m.styles.Label.Render(rpLabel)
		}
		bodyLines = append(bodyLines, rpLabel)
		bodyLines = append(bodyLines, m.inputs[pfLocalPortInput].View())
		bodyLines = append(bodyLines, "")
		fieldPositions[pfLocalPortInput] = fieldPos{startLine: rpStart, endLine: len(bodyLines) - 1}

		// Local host (using pfRemoteHostInput)
		lhStart := len(bodyLines)
		lhLabel := i18n.T("pf.local_host_label")
		if m.focused == pfRemoteHostInput {
			lhLabel = m.styles.FocusedLabel.Render(lhLabel)
		} else {
			lhLabel = m.styles.Label.Render(lhLabel)
		}
		bodyLines = append(bodyLines, lhLabel)
		bodyLines = append(bodyLines, m.inputs[pfRemoteHostInput].View())
		bodyLines = append(bodyLines, "")
		fieldPositions[pfRemoteHostInput] = fieldPos{startLine: lhStart, endLine: len(bodyLines) - 1}

		// Local port (using pfRemotePortInput)
		lpStart := len(bodyLines)
		lpLabel := i18n.T("pf.local_port_label")
		if m.focused == pfRemotePortInput {
			lpLabel = m.styles.FocusedLabel.Render(lpLabel)
		} else {
			lpLabel = m.styles.Label.Render(lpLabel)
		}
		bodyLines = append(bodyLines, lpLabel)
		bodyLines = append(bodyLines, m.inputs[pfRemotePortInput].View())
		bodyLines = append(bodyLines, "")
		fieldPositions[pfRemotePortInput] = fieldPos{startLine: lpStart, endLine: len(bodyLines) - 1}

	case DynamicForward:
		bodyLines = append(bodyLines, m.styles.HelpText.Render(i18n.T("pf.dynamic_desc")))
		bodyLines = append(bodyLines, "")

		// SOCKS port
		spStart := len(bodyLines)
		spLabel := i18n.T("pf.socks_port_label")
		if m.focused == pfLocalPortInput {
			spLabel = m.styles.FocusedLabel.Render(spLabel)
		} else {
			spLabel = m.styles.Label.Render(spLabel)
		}
		bodyLines = append(bodyLines, spLabel)
		bodyLines = append(bodyLines, m.inputs[pfLocalPortInput].View())
		bodyLines = append(bodyLines, "")
		fieldPositions[pfLocalPortInput] = fieldPos{startLine: spStart, endLine: len(bodyLines) - 1}
	}

	// Bind address (for all types)
	baStart := len(bodyLines)
	bindLabel := i18n.T("pf.bind_address_label")
	if m.focused == pfBindAddressInput {
		bindLabel = m.styles.FocusedLabel.Render(bindLabel)
	} else {
		bindLabel = m.styles.Label.Render(bindLabel)
	}
	bodyLines = append(bodyLines, bindLabel)
	bodyLines = append(bodyLines, m.inputs[pfBindAddressInput].View())
	fieldPositions[pfBindAddressInput] = fieldPos{startLine: baStart, endLine: len(bodyLines) - 1}

	// 4. Viewport calculation & auto-scroll
	totalHeight := m.height
	if totalHeight <= 0 {
		totalHeight = 24
	}

	reservedLines := len(headerLines) + len(footerLines) + 4
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
	b.WriteString("\n")

	for _, line := range footerLines {
		b.WriteString(line)
		b.WriteString("\n")
	}

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		m.styles.FormContainer.Render(strings.TrimRight(b.String(), "\n")),
	)
}

func (m *portForwardModel) submitForm() tea.Cmd {
	return func() tea.Msg {
		// Validate inputs
		localPort := strings.TrimSpace(m.inputs[pfLocalPortInput].Value())
		if localPort == "" {
			return portForwardSubmitMsg{err: errors.New(i18n.T("pf.err_port_required")), sshArgs: nil}
		}
		if _, err := strconv.Atoi(localPort); err != nil {
			return portForwardSubmitMsg{err: errors.New(i18n.T("pf.err_invalid_port")), sshArgs: nil}
		}

		remoteHost := strings.TrimSpace(m.inputs[pfRemoteHostInput].Value())
		remotePort := strings.TrimSpace(m.inputs[pfRemotePortInput].Value())
		bindAddress := strings.TrimSpace(m.inputs[pfBindAddressInput].Value())

		// Build SSH command with port forwarding
		var sshArgs []string
		// Add config file if specified
		if m.configFile != "" {
			sshArgs = append(sshArgs, "-F", m.configFile)
		}

		// Add forwarding arguments
		var forwardTypeStr string
		switch m.forwardType {
		case LocalForward:
			forwardTypeStr = "local"
			if remoteHost == "" {
				remoteHost = "localhost"
			}
			if remotePort == "" {
				return portForwardSubmitMsg{err: errors.New(i18n.T("pf.err_remote_port_req")), sshArgs: nil}
			}

			// Validate remote port
			if _, err := strconv.Atoi(remotePort); err != nil {
				return portForwardSubmitMsg{err: errors.New(i18n.T("pf.err_invalid_remote_port")), sshArgs: nil}
			}

			// Build -L argument
			var forwardArg string
			if bindAddress != "" {
				forwardArg = fmt.Sprintf("%s:%s:%s:%s", bindAddress, localPort, remoteHost, remotePort)
			} else {
				forwardArg = fmt.Sprintf("%s:%s:%s", localPort, remoteHost, remotePort)
			}
			sshArgs = append(sshArgs, "-L", forwardArg)

		case RemoteForward:
			forwardTypeStr = "remote"
			if remoteHost == "" {
				remoteHost = "localhost"
			}
			if remotePort == "" {
				return portForwardSubmitMsg{err: errors.New(i18n.T("pf.err_local_port_req")), sshArgs: nil}
			}

			// Validate local port
			if _, err := strconv.Atoi(remotePort); err != nil {
				return portForwardSubmitMsg{err: errors.New(i18n.T("pf.err_invalid_local_port")), sshArgs: nil}
			}

			// Build -R argument (note: localPort is actually the remote port in this context)
			var forwardArg string
			if bindAddress != "" {
				forwardArg = fmt.Sprintf("%s:%s:%s:%s", bindAddress, localPort, remoteHost, remotePort)
			} else {
				forwardArg = fmt.Sprintf("%s:%s:%s", localPort, remoteHost, remotePort)
			}
			sshArgs = append(sshArgs, "-R", forwardArg)

		case DynamicForward:
			forwardTypeStr = "dynamic"
			// Build -D argument
			var forwardArg string
			if bindAddress != "" {
				forwardArg = fmt.Sprintf("%s:%s", bindAddress, localPort)
			} else {
				forwardArg = localPort
			}
			sshArgs = append(sshArgs, "-D", forwardArg)
		}

		// Save port forwarding configuration to history
		if m.historyManager != nil {
			if err := m.historyManager.RecordPortForwarding(
				m.hostName,
				forwardTypeStr,
				localPort,
				remoteHost,
				remotePort,
				bindAddress,
			); err != nil {
				// Log the error but don't fail the connection
				// In a production environment, you might want to handle this differently
			}
		}

		// Add hostname
		sshArgs = append(sshArgs, m.hostName)

		// Return success with the SSH command to execute
		return portForwardSubmitMsg{err: nil, sshArgs: sshArgs}
	}
}

// getValidFields returns the list of valid field indices for the current forward type
func (m *portForwardModel) getValidFields() []int {
	switch m.forwardType {
	case LocalForward:
		return []int{pfTypeInput, pfLocalPortInput, pfRemoteHostInput, pfRemotePortInput, pfBindAddressInput}
	case RemoteForward:
		return []int{pfTypeInput, pfLocalPortInput, pfRemoteHostInput, pfRemotePortInput, pfBindAddressInput}
	case DynamicForward:
		return []int{pfTypeInput, pfLocalPortInput, pfBindAddressInput}
	default:
		return []int{pfTypeInput, pfLocalPortInput, pfRemoteHostInput, pfRemotePortInput, pfBindAddressInput}
	}
}

// getNextValidField returns the next valid field index, or -1 if none
func (m *portForwardModel) getNextValidField(currentField int) int {
	validFields := m.getValidFields()

	for i, field := range validFields {
		if field == currentField && i < len(validFields)-1 {
			return validFields[i+1]
		}
	}
	return -1
}

// getPrevValidField returns the previous valid field index, or -1 if none
func (m *portForwardModel) getPrevValidField(currentField int) int {
	validFields := m.getValidFields()

	for i, field := range validFields {
		if field == currentField && i > 0 {
			return validFields[i-1]
		}
	}
	return -1
}

// loadPreviousConfig loads the previous port forwarding configuration for this host
func (m *portForwardModel) loadPreviousConfig() {
	if m.historyManager == nil {
		m.inputs[pfTypeInput].SetValue("Local (-L)")
		return
	}

	config := m.historyManager.GetPortForwardingConfig(m.hostName)
	if config == nil {
		m.inputs[pfTypeInput].SetValue("Local (-L)")
		return
	}

	// Set forward type based on saved configuration
	switch config.Type {
	case "local":
		m.forwardType = LocalForward
	case "remote":
		m.forwardType = RemoteForward
	case "dynamic":
		m.forwardType = DynamicForward
	default:
		m.forwardType = LocalForward
	}
	m.inputs[pfTypeInput].SetValue(m.forwardType.String())

	// Set values from saved configuration
	if config.LocalPort != "" {
		m.inputs[pfLocalPortInput].SetValue(config.LocalPort)
	}
	if config.RemoteHost != "" {
		m.inputs[pfRemoteHostInput].SetValue(config.RemoteHost)
	} else if m.forwardType != DynamicForward {
		// Default to localhost for local and remote forwarding if not set
		m.inputs[pfRemoteHostInput].SetValue("localhost")
	}
	if config.RemotePort != "" {
		m.inputs[pfRemotePortInput].SetValue(config.RemotePort)
	}
	if config.BindAddress != "" {
		m.inputs[pfBindAddressInput].SetValue(config.BindAddress)
	}
}
