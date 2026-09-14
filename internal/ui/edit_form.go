package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/credential"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/ui/theme"
	"github.com/zsuroy/ctty/internal/validation"
)

type editFormSubmitMsg struct {
	hostname string
	err      error
}

type editFormCancelMsg struct{}

type editFormModel struct {
	form             *huh.Form
	viewport         viewport.Model
	styles           Styles
	err              string
	success          bool
	originalName     string
	originalHosts    []string
	host             *config.SSHHost
	configFile       string
	actualConfigFile string
	width            int
	height           int

	// Form field values
	hostNamesVal     string
	hostnameVal      string
	userVal          string
	portVal          string
	passwordVal      string
	identityVal      string
	tagsVal          string
	proxyJumpVal     string
	proxyCommandVal  string
	optionsVal       string
	remoteCommandVal string
	requestTTYVal    string
	confirmSave      bool
}

// NewEditForm creates a new modern edit form model powered by charmbracelet/huh
func NewEditForm(hostName string, styles Styles, width, height int, configFile string) (*editFormModel, error) {
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

	var actualConfigFile string
	var hostNames []string

	if configFile != "" {
		actualConfigFile = configFile
	} else {
		actualConfigFile = host.SourceFile
	}

	if actualConfigFile != "" {
		_, hostNames, err = config.IsPartOfMultiHostDeclaration(hostName, actualConfigFile)
		if err != nil || len(hostNames) == 0 {
			hostNames = []string{hostName}
		}
	} else {
		hostNames = []string{hostName}
	}

	password := ""
	if pass, ok := credential.GetPassword(hostName); ok {
		password = pass
	}

	optionsStr := ""
	if host.Options != "" {
		optionsStr = config.FormatSSHOptionsForCommand(host.Options)
	}

	tagsStr := ""
	if len(host.Tags) > 0 {
		tagsStr = strings.Join(host.Tags, ", ")
	}

	m := &editFormModel{
		styles:           styles,
		width:            width,
		height:           height,
		originalName:     hostName,
		originalHosts:    hostNames,
		host:             host,
		configFile:       configFile,
		actualConfigFile: actualConfigFile,
		hostNamesVal:     strings.Join(hostNames, " "),
		hostnameVal:      host.Hostname,
		userVal:          host.User,
		portVal:          host.Port,
		passwordVal:      password,
		identityVal:      host.Identity,
		tagsVal:          tagsStr,
		proxyJumpVal:     host.ProxyJump,
		proxyCommandVal:  host.ProxyCommand,
		optionsVal:       optionsStr,
		remoteCommandVal: host.RemoteCommand,
		requestTTYVal:    host.RequestTTY,
		confirmSave:      true,
	}

	m.buildForm()
	return m, nil
}

func (m *editFormModel) buildForm() {
	innerW := formPageInnerWidth(m.width)
	if innerW < 20 {
		innerW = 20
	}

	currentHuhTheme := theme.GetTheme(m.styles.Theme.ID).HuhTheme()

	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Key("hosts").
				Title(i18n.T("form.host_name")).
				Prompt("> ").
				Placeholder("server-name (or multiple aliases separated by space)").
				Value(&m.hostNamesVal).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("%s", i18n.T("form.err_host_name_req"))
					}
					return nil
				}),

			huh.NewInput().
				Key("hostname").
				Title(i18n.T("form.hostname_ip")).
				Prompt("> ").
				Placeholder("192.168.1.100 or example.com").
				Value(&m.hostnameVal).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("%s", i18n.T("form.err_hostname_req"))
					}
					return nil
				}),

			huh.NewInput().
				Key("user").
				Title(i18n.T("form.user")).
				Prompt("> ").
				Placeholder("root").
				Value(&m.userVal),

			huh.NewInput().
				Key("port").
				Title(i18n.T("form.port")).
				Prompt("> ").
				Placeholder("22").
				Value(&m.portVal),

			huh.NewInput().
				Key("password").
				Title(i18n.T("form.password")).
				Prompt("> ").
				Placeholder(i18n.T("form.password_placeholder")).
				EchoMode(huh.EchoModePassword).
				Value(&m.passwordVal),

			huh.NewInput().
				Key("identity").
				Title(i18n.T("form.identity_file")).
				Prompt("> ").
				Placeholder("~/.ssh/id_rsa").
				Value(&m.identityVal),

			huh.NewInput().
				Key("tags").
				Title(i18n.T("form.tags")).
				Prompt("> ").
				Placeholder("production, web").
				Value(&m.tagsVal),

			huh.NewInput().
				Key("proxy_jump").
				Title(i18n.T("form.proxy_jump")).
				Prompt("> ").
				Placeholder("user@jump-host:port").
				Value(&m.proxyJumpVal),

			huh.NewInput().
				Key("proxy_command").
				Title(i18n.T("form.proxy_command")).
				Prompt("> ").
				Placeholder("ssh -W %h:%p Jumphost").
				Value(&m.proxyCommandVal),

			huh.NewInput().
				Key("options").
				Title(i18n.T("form.ssh_options")).
				Prompt("> ").
				Placeholder("-o Compression=yes").
				Value(&m.optionsVal),

			huh.NewInput().
				Key("remote_command").
				Title(i18n.T("form.remote_command")).
				Prompt("> ").
				Placeholder("ls -la, htop, bash").
				Value(&m.remoteCommandVal),

			huh.NewSelect[string]().
				Key("request_tty").
				Title(i18n.T("form.request_tty")).
				Inline(true).
				Options(
					huh.NewOption("Default", ""),
					huh.NewOption("yes", "yes"),
					huh.NewOption("no", "no"),
					huh.NewOption("force", "force"),
					huh.NewOption("auto", "auto"),
				).
				Value(&m.requestTTYVal),

			huh.NewConfirm().
				Key("confirm").
				Title(i18n.T("settings.save_prompt")).
				Affirmative(i18n.T("settings.save_confirm")).
				Negative(i18n.T("settings.cancel")).
				Value(&m.confirmSave),
		),
	).WithLayout(huh.LayoutStack).
		WithTheme(currentHuhTheme).
		WithWidth(innerW).
		WithShowHelp(false)
}

func (m *editFormModel) Init() tea.Cmd {
	if m.form != nil {
		return m.form.Init()
	}
	return nil
}

func (m *editFormModel) nextField() tea.Cmd {
	if m.form == nil {
		return nil
	}
	focused := m.form.GetFocusedField()
	if focused != nil && focused.GetKey() == "confirm" {
		for m.form.GetFocusedField().GetKey() != "hosts" {
			m.form.PrevField()
		}
		return nil
	}
	return m.form.NextField()
}

func (m *editFormModel) prevField() tea.Cmd {
	if m.form == nil {
		return nil
	}
	focused := m.form.GetFocusedField()
	if focused != nil && focused.GetKey() == "hosts" {
		for m.form.GetFocusedField().GetKey() != "confirm" {
			m.form.NextField()
		}
		return nil
	}
	return m.form.PrevField()
}

func (m *editFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			return m, func() tea.Msg { return editFormCancelMsg{} }
		case msg.String() == "ctrl+s":
			return m, m.submitEditForm()
		case msg.Type == tea.KeyTab || msg.String() == "tab":
			return m, m.nextField()
		case msg.Type == tea.KeyShiftTab || msg.String() == "shift+tab" || msg.String() == "backtab":
			return m, m.prevField()
		case msg.Type == tea.KeyDown || msg.String() == "down":
			return m, m.nextField()
		case msg.Type == tea.KeyUp || msg.String() == "up":
			return m, m.prevField()
		case msg.Type == tea.KeyEnter || msg.String() == "enter":
			if m.form != nil {
				focused := m.form.GetFocusedField()
				if focused != nil && focused.GetKey() == "confirm" {
					if m.confirmSave {
						return m, m.submitEditForm()
					}
					return m, func() tea.Msg { return editFormCancelMsg{} }
				}
			}
			return m, m.nextField()
		}

	case editFormSubmitMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.success = true
			m.err = ""
		}
		return m, nil
	}

	if m.form == nil {
		return m, nil
	}

	formModel, cmd := m.form.Update(msg)
	if f, ok := formModel.(*huh.Form); ok {
		m.form = f
	}

	if m.form.State == huh.StateCompleted {
		if m.confirmSave {
			return m, m.submitEditForm()
		}
		return m, func() tea.Msg { return editFormCancelMsg{} }
	}

	if m.form.State == huh.StateAborted {
		return m, func() tea.Msg { return editFormCancelMsg{} }
	}

	return m, cmd
}

func (m *editFormModel) submitEditForm() tea.Cmd {
	return func() tea.Msg {
		rawNames := strings.FieldsFunc(m.hostNamesVal, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t'
		})
		var hostNames []string
		for _, name := range rawNames {
			name = strings.TrimSpace(name)
			if name != "" {
				hostNames = append(hostNames, name)
			}
		}

		if len(hostNames) == 0 {
			return editFormSubmitMsg{err: fmt.Errorf("%s", i18n.T("form.err_host_name_req"))}
		}

		hostname := strings.TrimSpace(m.hostnameVal)
		user := strings.TrimSpace(m.userVal)
		port := strings.TrimSpace(m.portVal)
		password := strings.TrimSpace(m.passwordVal)
		identity := strings.TrimSpace(m.identityVal)
		proxyJump := strings.TrimSpace(m.proxyJumpVal)
		proxyCommand := strings.TrimSpace(m.proxyCommandVal)
		tagsStr := strings.TrimSpace(m.tagsVal)
		options := config.ParseSSHOptionsFromCommand(strings.TrimSpace(m.optionsVal))
		remoteCommand := strings.TrimSpace(m.remoteCommandVal)
		requestTTY := strings.TrimSpace(m.requestTTYVal)

		if port == "" {
			port = "22"
		}

		if hostname == "" {
			return editFormSubmitMsg{err: fmt.Errorf("%s", i18n.T("form.err_hostname_req"))}
		}

		for _, h := range hostNames {
			if err := validation.ValidateHost(h, hostname, port, identity); err != nil {
				return editFormSubmitMsg{err: err}
			}
		}

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
			commonHost.Name = hostNames[0]
			if m.actualConfigFile != "" {
				err = config.UpdateSSHHostInFile(m.originalName, commonHost, m.actualConfigFile)
			} else {
				err = config.UpdateSSHHost(m.originalName, commonHost)
			}
		} else {
			if m.actualConfigFile != "" {
				err = config.UpdateMultiHostBlock(m.originalHosts, hostNames, commonHost, m.actualConfigFile)
			} else {
				err = config.UpdateMultiHostBlock(m.originalHosts, hostNames, commonHost, "")
			}
		}

		if err == nil {
			if password != "" {
				for _, name := range hostNames {
					_ = credential.SetPassword(name, password)
				}
			}
		}

		return editFormSubmitMsg{hostname: m.originalName, err: err}
	}
}

func (m *editFormModel) getFieldTitle(key string) string {
	switch key {
	case "hosts":
		return i18n.T("form.host_name")
	case "hostname":
		return i18n.T("form.hostname_ip")
	case "user":
		return i18n.T("form.user")
	case "port":
		return i18n.T("form.port")
	case "password":
		return i18n.T("form.password")
	case "identity":
		return i18n.T("form.identity_file")
	case "tags":
		return i18n.T("form.tags")
	case "proxy_jump":
		return i18n.T("form.proxy_jump")
	case "proxy_command":
		return i18n.T("form.proxy_command")
	case "options":
		return i18n.T("form.ssh_options")
	case "remote_command":
		return i18n.T("form.remote_command")
	case "request_tty":
		return i18n.T("form.request_tty")
	case "confirm":
		return i18n.T("settings.save_prompt")
	default:
		return ""
	}
}

func (m *editFormModel) View() string {
	if m.success {
		return ""
	}

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
	titleText := m.styles.Header.Width(innerW).Render(i18n.T("form.edit_title"))

	helpText := m.styles.HelpText.Width(innerW).Render(i18n.T("form.help_save_cancel"))

	formView := ""
	if m.form != nil {
		formView = m.form.View()
	}

	frameH := container.GetVerticalFrameSize()
	headerH := lipgloss.Height(titleText)
	helpH := lipgloss.Height(helpText)

	targetBoxH := m.height
	if m.height >= 14 {
		targetBoxH = m.height - 1
	}

	overhead := frameH + headerH + helpH + 2
	if m.err != "" {
		overhead += 2
	}
	availableH := targetBoxH - overhead
	if availableH < 2 {
		availableH = 2
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

	contentParts := []string{titleText, ""}
	if m.err != "" {
		contentParts = append(contentParts, m.styles.ErrorText.Width(innerW).MaxHeight(2).Render("❌ "+m.err), "")
	}
	contentParts = append(contentParts, bodyView, "", helpText)

	content := lipgloss.JoinVertical(lipgloss.Left, contentParts...)
	box := container.Width(boxWidth).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top, box)
}

type standaloneEditForm struct {
	*editFormModel
}

func (m standaloneEditForm) Init() tea.Cmd {
	return m.editFormModel.Init()
}

func (m standaloneEditForm) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case editFormSubmitMsg:
		if msg.err != nil {
			m.editFormModel.err = msg.err.Error()
			return m, nil
		}
		return m, tea.Quit
	case editFormCancelMsg:
		return m, tea.Quit
	}

	newForm, cmd := m.editFormModel.Update(msg)
	m.editFormModel = newForm.(*editFormModel)
	return m, cmd
}

func (m standaloneEditForm) View() string {
	return m.editFormModel.View()
}

// RunEditForm runs the edit form as a standalone program
func RunEditForm(hostName string, configFile string) error {
	styles := NewStyles(80)
	editForm, err := NewEditForm(hostName, styles, 80, 24, configFile)
	if err != nil {
		return err
	}

	m := standaloneEditForm{editForm}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
