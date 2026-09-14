package ui

import (
	"errors"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/ftpconfig"
	"github.com/zsuroy/ctty/internal/ftpcred"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/ui/theme"
)

type ftpAddFormModel struct {
	form     *huh.Form
	viewport viewport.Model
	styles   Styles
	width    int
	height   int
	editing  *ftpconfig.FTPSite
	err      string

	nameVal     string
	hostVal     string
	portVal     string
	userVal     string
	passwordVal string
	tagsVal     string
	confirmVal  bool

	done      bool
	cancelled bool
}

func newFTPAddForm(styles Styles, width, height int, initial *ftpconfig.FTPSite) *ftpAddFormModel {
	m := &ftpAddFormModel{
		styles:     styles,
		width:      width,
		height:     height,
		editing:    initial,
		portVal:    strconv.Itoa(ftpconfig.DefaultPort),
		userVal:    "anonymous",
		confirmVal: true,
	}
	if initial != nil {
		m.nameVal = initial.Name
		m.hostVal = initial.Host
		if initial.Port > 0 {
			m.portVal = strconv.Itoa(initial.Port)
		}
		if initial.User != "" {
			m.userVal = initial.User
		}
		if pass, ok := ftpcred.GetPassword(initial.Name); ok {
			m.passwordVal = pass
		}
		m.tagsVal = strings.Join(initial.Tags, ", ")
	}
	m.buildForm()
	return m
}

func (m *ftpAddFormModel) buildForm() {
	innerW := formPageInnerWidth(m.width)
	if innerW < 20 {
		innerW = 20
	}

	validatePort := func(s string) error {
		val := strings.TrimSpace(s)
		if val == "" {
			return nil
		}
		p, err := strconv.Atoi(val)
		if err != nil || p < 1 || p > 65535 {
			return errors.New(i18n.T("pf.err_invalid_port"))
		}
		return nil
	}

	validateName := func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New(i18n.T("form.err_host_name_req"))
		}
		return nil
	}
	validateHost := func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New(i18n.T("form.err_hostname_req"))
		}
		return nil
	}

	currentHuhTheme := theme.GetTheme(m.styles.Theme.ID).HuhTheme()

	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Key("name").
				Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("ftp.field_name"), ":"))).
				Prompt("> ").
				Placeholder("e.g. lab-nas").
				Validate(validateName).
				Value(&m.nameVal),

			huh.NewInput().
				Key("host").
				Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("ftp.field_host"), ":"))).
				Prompt("> ").
				Placeholder("e.g. 192.168.1.1 or nas.local").
				Validate(validateHost).
				Value(&m.hostVal),

			huh.NewInput().
				Key("port").
				Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("ftp.field_port"), ":"))).
				Prompt("> ").
				Placeholder("21").
				Validate(validatePort).
				Value(&m.portVal),

			huh.NewInput().
				Key("user").
				Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("ftp.field_user"), ":"))).
				Prompt("> ").
				Placeholder("anonymous").
				Value(&m.userVal),

			huh.NewInput().
				Key("password").
				Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("ftp.field_password"), ":"))).
				Prompt("> ").
				Placeholder("leave empty to keep").
				EchoMode(huh.EchoModePassword).
				Value(&m.passwordVal),

			huh.NewInput().
				Key("tags").
				Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("ftp.field_tags"), ":"))).
				Prompt("> ").
				Placeholder("lab,backup").
				Value(&m.tagsVal),

			huh.NewConfirm().
				Key("confirm").
				Title("").
				Affirmative(i18n.T("form.btn_save")).
				Negative(i18n.T("form.btn_cancel")).
				Value(&m.confirmVal),
		),
	).WithLayout(huh.LayoutStack).
		WithTheme(currentHuhTheme).
		WithWidth(innerW).
		WithShowHelp(false)
}

func (m *ftpAddFormModel) Init() tea.Cmd {
	if m.form != nil {
		return m.form.Init()
	}
	return nil
}

func (m *ftpAddFormModel) nextField() tea.Cmd {
	if m.form == nil {
		return nil
	}
	focused := m.form.GetFocusedField()
	if focused != nil && focused.GetKey() == "confirm" {
		for m.form.GetFocusedField().GetKey() != "name" {
			m.form.PrevField()
		}
		return nil
	}
	return m.form.NextField()
}

func (m *ftpAddFormModel) prevField() tea.Cmd {
	if m.form == nil {
		return nil
	}
	focused := m.form.GetFocusedField()
	if focused != nil && focused.GetKey() == "name" {
		for m.form.GetFocusedField().GetKey() != "confirm" {
			m.form.NextField()
		}
		return nil
	}
	return m.form.PrevField()
}

func (m *ftpAddFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.styles = NewStyles(m.width)
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
			m.cancelled = true
			return m, nil
		case msg.String() == "ctrl+s":
			m.submit()
			return m, nil
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
					if m.confirmVal {
						m.submit()
					} else {
						m.cancelled = true
					}
					return m, nil
				}
			}
			return m, m.nextField()
		}
	}

	if m.form == nil {
		return m, nil
	}

	formModel, cmd := m.form.Update(msg)
	if f, ok := formModel.(*huh.Form); ok {
		m.form = f
	}

	if m.form.State == huh.StateCompleted {
		if m.confirmVal {
			m.submit()
			return m, nil
		}
		m.cancelled = true
		return m, nil
	}

	if m.form.State == huh.StateAborted {
		m.cancelled = true
		return m, nil
	}

	return m, cmd
}

func (m *ftpAddFormModel) submit() {
	name := strings.TrimSpace(m.nameVal)
	host := strings.TrimSpace(m.hostVal)
	if name == "" || host == "" {
		m.err = i18n.T("ftp.err_name_host_req")
		return
	}

	port := ftpconfig.DefaultPort
	if strings.TrimSpace(m.portVal) != "" {
		p, err := strconv.Atoi(strings.TrimSpace(m.portVal))
		if err == nil && p > 0 && p <= 65535 {
			port = p
		}
	}

	user := strings.TrimSpace(m.userVal)
	if user == "" {
		user = "anonymous"
	}

	var tags []string
	for _, t := range strings.Split(m.tagsVal, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}

	site := ftpconfig.FTPSite{Name: name, Host: host, Port: port, User: user, Tags: tags}

	var err error
	oldName := ""
	if m.editing != nil {
		oldName = m.editing.Name
		err = ftpconfig.Update(oldName, site)
		if err == nil && oldName != name {
			_ = ftpcred.DeletePassword(oldName)
		}
	} else {
		err = ftpconfig.Add(site)
	}
	if err != nil {
		m.err = err.Error()
		return
	}
	pass := m.passwordVal
	if pass != "" {
		_ = ftpcred.SetPassword(name, pass)
	} else if m.editing != nil {
		_ = ftpcred.DeletePassword(name)
	}
	m.done = true
}

func (m *ftpAddFormModel) getFieldTitle(key string) string {
	switch key {
	case "name":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("ftp.field_name"), ":"))
	case "host":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("ftp.field_host"), ":"))
	case "port":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("ftp.field_port"), ":"))
	case "user":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("ftp.field_user"), ":"))
	case "password":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("ftp.field_password"), ":"))
	case "tags":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("ftp.field_tags"), ":"))
	case "confirm":
		return i18n.T("form.btn_save")
	default:
		return ""
	}
}

func (m *ftpAddFormModel) View() string {
	title := i18n.T("ftp.sites_add_title")
	if m.editing != nil {
		title = i18n.T("ftp.sites_edit_title")
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
	titleText := m.styles.Header.Width(innerW).Render(title)

	helpText := m.styles.HelpText.Width(innerW).Render(i18n.T("ftp.sites_form_help"))
	credHint := m.styles.HelpText.Width(innerW).Render(i18n.T("ftp.cred_hint"))
	helpJoined := lipgloss.JoinVertical(lipgloss.Left, helpText, credHint)

	formView := ""
	if m.form != nil {
		formView = m.form.View()
	}

	frameH := container.GetVerticalFrameSize()
	headerH := lipgloss.Height(titleText)
	helpH := lipgloss.Height(helpJoined)

	targetBoxH := m.height
	if m.height >= 14 {
		targetBoxH = m.height - 1
	}

	overhead := frameH + headerH + helpH + 2
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
	contentParts = append(contentParts, bodyView, "", helpJoined)

	content := lipgloss.JoinVertical(lipgloss.Left, contentParts...)
	box := container.Width(boxWidth).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top, box)
}
