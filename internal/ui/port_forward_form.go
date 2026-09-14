package ui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/history"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/ui/theme"
)

type portForwardModel struct {
	form           *huh.Form
	viewport       viewport.Model
	styles         Styles
	width          int
	height         int
	hostName       string
	configFile     string
	historyManager *history.HistoryManager
	err            string

	// Form values
	typeVal        string // "local", "remote", "dynamic"
	localPortVal   string
	remoteHostVal  string
	remotePortVal  string
	bindAddressVal string
	confirmConnect bool
	hasHistory     bool
}

// portForwardSubmitMsg is sent when the port forward form is submitted
type portForwardSubmitMsg struct {
	err     error
	sshArgs []string
}

// portForwardCancelMsg is sent when the port forward form is cancelled
type portForwardCancelMsg struct{}

// NewPortForwardForm creates a new modern port forward form powered by charmbracelet/huh
func NewPortForwardForm(hostName string, styles Styles, width, height int, configFile string, historyManager *history.HistoryManager) *portForwardModel {
	pf := &portForwardModel{
		styles:         styles,
		width:          width,
		height:         height,
		hostName:       hostName,
		configFile:     configFile,
		historyManager: historyManager,
		typeVal:        "local",
		localPortVal:   "8080",
		remoteHostVal:  "localhost",
		remotePortVal:  "80",
		confirmConnect: true,
	}

	pf.loadPreviousConfig()
	pf.buildForm()
	return pf
}

func (m *portForwardModel) loadPreviousConfig() {
	if m.historyManager == nil {
		return
	}
	cfg := m.historyManager.GetPortForwardingConfig(m.hostName)
	if cfg == nil {
		return
	}

	m.hasHistory = true
	if cfg.Type != "" {
		m.typeVal = cfg.Type
	}
	if cfg.LocalPort != "" {
		m.localPortVal = cfg.LocalPort
	}
	if cfg.RemoteHost != "" {
		m.remoteHostVal = cfg.RemoteHost
	}
	if cfg.RemotePort != "" {
		m.remotePortVal = cfg.RemotePort
	}
	if cfg.BindAddress != "" {
		m.bindAddressVal = cfg.BindAddress
	}
}

func (m *portForwardModel) buildForm() {
	innerW := formPageInnerWidth(m.width)
	if innerW < 20 {
		innerW = 20
	}

	validatePort := func(s string) error {
		val := strings.TrimSpace(s)
		if val == "" {
			return errors.New(i18n.T("pf.err_port_required"))
		}
		p, err := strconv.Atoi(val)
		if err != nil || p < 1 || p > 65535 {
			return errors.New(i18n.T("pf.err_invalid_port"))
		}
		return nil
	}

	currentHuhTheme := theme.GetTheme(m.styles.Theme.ID).HuhTheme()

	var port1Title, port1Placeholder string
	var hostTitle, hostPlaceholder string
	var port2Title, port2Placeholder string

	switch m.typeVal {
	case "remote":
		port1Title = i18n.T("pf.remote_listen_port_label")
		port1Placeholder = i18n.T("pf.remote_listen_port_placeholder")
		hostTitle = i18n.T("pf.local_target_host_label")
		hostPlaceholder = i18n.T("pf.local_target_host_placeholder")
		port2Title = i18n.T("pf.local_target_port_label")
		port2Placeholder = i18n.T("pf.local_target_port_placeholder")
	case "dynamic":
		port1Title = i18n.T("pf.socks_listen_port_label")
		port1Placeholder = i18n.T("pf.socks_listen_port_placeholder")
	default: // "local"
		port1Title = i18n.T("pf.local_listen_port_label")
		port1Placeholder = i18n.T("pf.local_listen_port_placeholder")
		hostTitle = i18n.T("pf.remote_target_host_label")
		hostPlaceholder = i18n.T("pf.remote_target_host_placeholder")
		port2Title = i18n.T("pf.remote_target_port_label")
		port2Placeholder = i18n.T("pf.remote_target_port_placeholder")
	}

	fields := []huh.Field{
		huh.NewSelect[string]().
			Key("type").
			Title(i18n.T("pf.forward_type_label")).
			Inline(true).
			Options(
				huh.NewOption("Local (-L)", "local"),
				huh.NewOption("Remote (-R)", "remote"),
				huh.NewOption("Dynamic SOCKS (-D)", "dynamic"),
			).
			Value(&m.typeVal),

		huh.NewInput().
			Key("local_port").
			Title(port1Title).
			Prompt("> ").
			Placeholder(port1Placeholder).
			Validate(validatePort).
			Value(&m.localPortVal),
	}

	if m.typeVal != "dynamic" {
		fields = append(fields,
			huh.NewInput().
				Key("remote_host").
				Title(hostTitle).
				Prompt("> ").
				Placeholder(hostPlaceholder).
				Value(&m.remoteHostVal),

			huh.NewInput().
				Key("remote_port").
				Title(port2Title).
				Prompt("> ").
				Placeholder(port2Placeholder).
				Validate(validatePort).
				Value(&m.remotePortVal),
		)
	}

	fields = append(fields,
		huh.NewInput().
			Key("bind_address").
			Title(i18n.T("pf.bind_address_label")).
			Prompt("> ").
			Placeholder("127.0.0.1 (optional)").
			Value(&m.bindAddressVal),

		huh.NewConfirm().
			Key("confirm").
			Title(i18n.T("pf.confirm_prompt")).
			Affirmative(i18n.T("pf.action_start")).
			Negative(i18n.T("pf.action_cancel")).
			Value(&m.confirmConnect),
	)

	m.form = huh.NewForm(
		huh.NewGroup(fields...),
	).WithLayout(huh.LayoutStack).
		WithTheme(currentHuhTheme).
		WithWidth(innerW).
		WithShowHelp(false)
}

func (m *portForwardModel) Init() tea.Cmd {
	if m.form != nil {
		return m.form.Init()
	}
	return nil
}

func (m *portForwardModel) getFieldTitle(key string) string {
	switch key {
	case "type":
		return i18n.T("pf.forward_type_label")
	case "local_port":
		switch m.typeVal {
		case "remote":
			return i18n.T("pf.remote_listen_port_label")
		case "dynamic":
			return i18n.T("pf.socks_listen_port_label")
		default:
			return i18n.T("pf.local_listen_port_label")
		}
	case "remote_host":
		if m.typeVal == "remote" {
			return i18n.T("pf.local_target_host_label")
		}
		return i18n.T("pf.remote_target_host_label")
	case "remote_port":
		if m.typeVal == "remote" {
			return i18n.T("pf.local_target_port_label")
		}
		return i18n.T("pf.remote_target_port_label")
	case "bind_address":
		return i18n.T("pf.bind_address_label")
	case "confirm":
		return i18n.T("pf.confirm_prompt")
	default:
		return ""
	}
}

func (m *portForwardModel) nextField() tea.Cmd {
	if m.form == nil {
		return nil
	}
	focused := m.form.GetFocusedField()
	if focused != nil && focused.GetKey() == "confirm" {
		for m.form.GetFocusedField().GetKey() != "type" {
			m.form.PrevField()
		}
		return nil
	}
	return m.form.NextField()
}

func (m *portForwardModel) prevField() tea.Cmd {
	if m.form == nil {
		return nil
	}
	focused := m.form.GetFocusedField()
	if focused != nil && focused.GetKey() == "type" {
		for m.form.GetFocusedField().GetKey() != "confirm" {
			m.form.NextField()
		}
		return nil
	}
	return m.form.PrevField()
}

func (m *portForwardModel) Update(msg tea.Msg) (*portForwardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		innerW := formPageInnerWidth(m.width)
		if innerW < 20 {
			innerW = 20
		}
		if m.form != nil {
			m.form.WithWidth(innerW)
		}
		return m, nil

	case tea.KeyMsg:
		switch {
		case msg.String() == "esc" || msg.String() == "ctrl+c":
			return m, func() tea.Msg { return portForwardCancelMsg{} }
		case msg.String() == "ctrl+s":
			return m, m.submitForm()
		case msg.Type == tea.KeyTab || msg.String() == "tab" || msg.Type == tea.KeyDown || msg.String() == "down":
			return m, m.nextField()
		case msg.Type == tea.KeyShiftTab || msg.String() == "shift+tab" || msg.String() == "backtab" || msg.Type == tea.KeyUp || msg.String() == "up":
			return m, m.prevField()
		case msg.Type == tea.KeyEnter || msg.String() == "enter":
			if m.form != nil {
				focused := m.form.GetFocusedField()
				if focused != nil && focused.GetKey() == "confirm" {
					if m.confirmConnect {
						return m, m.submitForm()
					}
					return m, func() tea.Msg { return portForwardCancelMsg{} }
				}
			}
			return m, m.nextField()
		}
	}

	if m.form == nil {
		return m, nil
	}

	prevType := m.typeVal
	formModel, cmd := m.form.Update(msg)
	if f, ok := formModel.(*huh.Form); ok {
		m.form = f
	}

	if m.typeVal != prevType {
		m.buildForm()
		_ = m.form.Init()
	}

	if m.form.State == huh.StateCompleted {
		if m.confirmConnect {
			return m, m.submitForm()
		}
		return m, func() tea.Msg { return portForwardCancelMsg{} }
	}

	if m.form.State == huh.StateAborted {
		return m, func() tea.Msg { return portForwardCancelMsg{} }
	}

	return m, cmd
}

func (m *portForwardModel) submitForm() tea.Cmd {
	return func() tea.Msg {
		localPort := strings.TrimSpace(m.localPortVal)
		remoteHost := strings.TrimSpace(m.remoteHostVal)
		remotePort := strings.TrimSpace(m.remotePortVal)
		bindAddress := strings.TrimSpace(m.bindAddressVal)

		var sshArgs []string
		if m.configFile != "" {
			sshArgs = append(sshArgs, "-F", m.configFile)
		}

		forwardTypeStr := m.typeVal
		switch m.typeVal {
		case "local":
			if remoteHost == "" {
				remoteHost = "localhost"
			}
			var forwardArg string
			if bindAddress != "" {
				forwardArg = fmt.Sprintf("%s:%s:%s:%s", bindAddress, localPort, remoteHost, remotePort)
			} else {
				forwardArg = fmt.Sprintf("%s:%s:%s", localPort, remoteHost, remotePort)
			}
			sshArgs = append(sshArgs, "-L", forwardArg)

		case "remote":
			if remoteHost == "" {
				remoteHost = "localhost"
			}
			var forwardArg string
			if bindAddress != "" {
				forwardArg = fmt.Sprintf("%s:%s:%s:%s", bindAddress, localPort, remoteHost, remotePort)
			} else {
				forwardArg = fmt.Sprintf("%s:%s:%s", localPort, remoteHost, remotePort)
			}
			sshArgs = append(sshArgs, "-R", forwardArg)

		case "dynamic":
			var forwardArg string
			if bindAddress != "" {
				forwardArg = fmt.Sprintf("%s:%s", bindAddress, localPort)
			} else {
				forwardArg = localPort
			}
			sshArgs = append(sshArgs, "-D", forwardArg)
		}

		// Save to history
		if m.historyManager != nil {
			_ = m.historyManager.RecordPortForwarding(
				m.hostName,
				forwardTypeStr,
				localPort,
				remoteHost,
				remotePort,
				bindAddress,
			)
		}

		sshArgs = append(sshArgs, m.hostName)
		return portForwardSubmitMsg{err: nil, sshArgs: sshArgs}
	}
}

func (m *portForwardModel) View() string {
	boxWidth := m.width - 4
	if boxWidth < 20 {
		boxWidth = 20
	}
	container := m.styles.FormContainer
	if m.height < 24 {
		container = container.Padding(0, 1)
	}
	innerW := boxWidth - container.GetHorizontalFrameSize()
	if innerW < 10 {
		innerW = 10
	}

	// Header parts are width-constrained BEFORE lipgloss.Height measures
	// them: an unwrapped flow badge measured 1 row but wrapped to 3 at
	// narrow widths, and the uncounted rows pushed the bottom border past
	// the terminal height (clipped by RenderCanvas).
	titleText := m.styles.Header.Width(innerW).Render(i18n.T("pf.title"))
	hostInfoStr := fmt.Sprintf(i18n.T("pf.host_info"), m.hostName)
	if m.hasHistory {
		hostInfoStr = fmt.Sprintf(i18n.T("pf.host_info_history"), m.hostName)
	}
	hostInfo := m.styles.HelpText.Width(innerW).Render(hostInfoStr)

	// Traffic flow indicator
	var flowText string
	lp := strings.TrimSpace(m.localPortVal)
	if lp == "" {
		lp = "?"
	}
	rh := strings.TrimSpace(m.remoteHostVal)
	if rh == "" {
		rh = "localhost"
	}
	rp := strings.TrimSpace(m.remotePortVal)
	if rp == "" {
		rp = "?"
	}

	switch m.typeVal {
	case "remote":
		flowText = fmt.Sprintf(i18n.T("pf.flow_remote"), lp, rh, rp)
	case "dynamic":
		flowText = fmt.Sprintf(i18n.T("pf.flow_dynamic"), lp)
	default:
		flowText = fmt.Sprintf(i18n.T("pf.flow_local"), lp, rh, rp)
	}
	flowBadge := m.styles.FocusedLabel.Width(innerW).Render(flowText)
	header := lipgloss.JoinVertical(lipgloss.Left, titleText, hostInfo, flowBadge)

	helpText := m.styles.HelpText.Width(innerW).Render(i18n.T("pf.help_text"))

	formView := ""
	if m.form != nil {
		formView = m.form.View()
	}

	frameH := container.GetVerticalFrameSize()
	headerH := lipgloss.Height(header)
	helpH := lipgloss.Height(helpText)

	targetBoxH := m.height
	if m.height >= 14 {
		targetBoxH = m.height - 1
	}

	overhead := frameH + headerH + helpH + 2
	availableH := targetBoxH - overhead
	// Floor of 1: a two-row minimum made the box exceed the terminal on
	// very narrow frames (header+help wrap tall there), clipping the bottom
	// border; one body row keeps the focused field usable at any width.
	if availableH < 1 {
		availableH = 1
	}

	formH := lipgloss.Height(formView)
	var bodyView string
	if formH <= availableH {
		bodyView = formView
	} else {
		m.viewport.Width = innerW
		m.viewport.Height = availableH
		m.viewport.SetContent(formView)

		// Auto-scroll viewport to keep focused field in view
		if m.form != nil {
			focused := m.form.GetFocusedField()
			if focused != nil {
				key := focused.GetKey()
				title := m.getFieldTitle(key)
				if title != "" {
					lines := strings.Split(formView, "\n")
					for idx, line := range lines {
						if strings.Contains(line, title) {
							if idx < m.viewport.YOffset {
								m.viewport.SetYOffset(idx)
							} else if idx+2 >= m.viewport.YOffset+availableH {
								m.viewport.SetYOffset(idx + 3 - availableH)
							}
							break
						}
					}
				}
			}
		}
		bodyView = m.viewport.View()
	}

	contentParts := []string{header, ""}
	if m.err != "" {
		contentParts = append(contentParts, m.styles.ErrorText.Width(innerW).MaxHeight(2).Render("❌ "+m.err), "")
	}
	contentParts = append(contentParts, bodyView, "", helpText)

	content := lipgloss.JoinVertical(lipgloss.Left, contentParts...)
	box := container.Width(boxWidth).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top, box)
}
