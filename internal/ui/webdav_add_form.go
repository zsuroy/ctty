package ui

import (
	"errors"
	"net/url"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/ui/theme"
	"github.com/zsuroy/ctty/internal/webdavconfig"
	"github.com/zsuroy/ctty/internal/webdavcred"
)

type webdavAddFormModel struct {
	form     *huh.Form
	viewport viewport.Model
	styles   Styles
	width    int
	height   int
	editing  *webdavconfig.WebDAVSite
	err      string

	nameVal        string
	urlVal         string
	userVal        string
	passwordVal    string
	insecureTLSVal bool
	tagsVal        string
	confirmVal     bool

	done      bool
	cancelled bool
}

func newWebDAVAddForm(styles Styles, width, height int, initial *webdavconfig.WebDAVSite) *webdavAddFormModel {
	m := &webdavAddFormModel{
		styles:     styles,
		width:      width,
		height:     height,
		editing:    initial,
		urlVal:     "https://",
		confirmVal: true,
	}
	if initial != nil {
		m.nameVal = initial.Name
		m.urlVal = initial.URL
		m.userVal = initial.User
		m.insecureTLSVal = initial.InsecureTLS
		if pass, ok := webdavcred.GetPassword(initial.Name); ok {
			m.passwordVal = pass
		}
		m.tagsVal = strings.Join(initial.Tags, ", ")
	}
	m.buildForm()
	return m
}

func (m *webdavAddFormModel) buildForm() {
	innerW := formPageInnerWidth(m.width)
	if innerW < 20 {
		innerW = 20
	}

	validateName := func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New(i18n.T("form.err_host_name_req"))
		}
		return nil
	}
	validateURL := func(s string) error {
		val := strings.TrimSpace(s)
		if val == "" {
			return errors.New(i18n.T("webdav.err_url_req"))
		}
		if !strings.Contains(val, "://") {
			val = "https://" + val
		}
		u, err := url.Parse(val)
		if err != nil || u.Host == "" {
			return errors.New(i18n.T("webdav.err_invalid_url"))
		}
		return nil
	}

	currentHuhTheme := theme.GetTheme(m.styles.Theme.ID).HuhTheme()

	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Key("name").
				Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("webdav.field_name"), ":"))).
				Prompt("> ").
				Placeholder(i18n.T("webdav.placeholder_name")).
				Validate(validateName).
				Value(&m.nameVal),

			huh.NewInput().
				Key("url").
				Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("webdav.field_url"), ":"))).
				Prompt("> ").
				Placeholder("https://dav.example.com/remote.php/dav/files/user/").
				Validate(validateURL).
				Value(&m.urlVal),

			huh.NewInput().
				Key("user").
				Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("webdav.field_user"), ":"))).
				Prompt("> ").
				Placeholder(i18n.T("webdav.placeholder_user")).
				Value(&m.userVal),

			huh.NewInput().
				Key("password").
				Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("webdav.field_password"), ":"))).
				Prompt("> ").
				Placeholder(i18n.T("webdav.placeholder_password")).
				EchoMode(huh.EchoModePassword).
				Value(&m.passwordVal),

			huh.NewConfirm().
				Key("insecure").
				Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("webdav.field_insecure_tls"), ":"))).
				Affirmative(i18n.T("common.yes")).
				Negative(i18n.T("common.no")).
				WithButtonAlignment(lipgloss.Left).
				Value(&m.insecureTLSVal),

			huh.NewInput().
				Key("tags").
				Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("webdav.field_tags"), ":"))).
				Prompt("> ").
				Placeholder(i18n.T("webdav.placeholder_tags")).
				Value(&m.tagsVal),

			huh.NewConfirm().
				Key("confirm").
				Title("").
				Affirmative(i18n.T("form.btn_save")).
				Negative(i18n.T("form.btn_cancel")).
				WithButtonAlignment(lipgloss.Left).
				Value(&m.confirmVal),
		),
	).WithLayout(huh.LayoutStack).
		WithTheme(currentHuhTheme).
		WithWidth(innerW).
		WithShowHelp(false)
}

func (m *webdavAddFormModel) Init() tea.Cmd {
	if m.form != nil {
		return m.form.Init()
	}
	return nil
}

func (m *webdavAddFormModel) nextField() tea.Cmd {
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

func (m *webdavAddFormModel) prevField() tea.Cmd {
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

func (m *webdavAddFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (m *webdavAddFormModel) submit() {
	name := strings.TrimSpace(m.nameVal)
	rawURL := strings.TrimSpace(m.urlVal)
	if name == "" || rawURL == "" {
		m.err = i18n.T("webdav.err_name_url_req")
		return
	}
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}

	user := strings.TrimSpace(m.userVal)

	var tags []string
	for _, t := range strings.Split(m.tagsVal, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}

	site := webdavconfig.WebDAVSite{
		Name:        name,
		URL:         rawURL,
		User:        user,
		InsecureTLS: m.insecureTLSVal,
		Tags:        tags,
	}

	var err error
	oldName := ""
	if m.editing != nil {
		oldName = m.editing.Name
		err = webdavconfig.Update(oldName, site)
		if err == nil && oldName != name {
			_ = webdavcred.DeletePassword(oldName)
		}
	} else {
		err = webdavconfig.Add(site)
	}
	if err != nil {
		m.err = err.Error()
		return
	}
	pass := m.passwordVal
	if pass != "" {
		_ = webdavcred.SetPassword(name, pass)
	} else if m.editing != nil {
		_ = webdavcred.DeletePassword(name)
	}
	m.done = true
}

func (m *webdavAddFormModel) getFieldTitle(key string) string {
	switch key {
	case "name":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("webdav.field_name"), ":"))
	case "url":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("webdav.field_url"), ":"))
	case "user":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("webdav.field_user"), ":"))
	case "password":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("webdav.field_password"), ":"))
	case "insecure":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("webdav.field_insecure_tls"), ":"))
	case "tags":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("webdav.field_tags"), ":"))
	case "confirm":
		return i18n.T("form.btn_save")
	default:
		return ""
	}
}

func (m *webdavAddFormModel) View() string {
	title := i18n.T("webdav.sites_add_title")
	if m.editing != nil {
		title = i18n.T("webdav.sites_edit_title")
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

	helpText := m.styles.HelpText.Width(innerW).Render(i18n.T("webdav.sites_form_help"))
	credHint := m.styles.HelpText.Width(innerW).Render(i18n.T("webdav.cred_hint"))
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
	box := alignJetBrainsBox(container.Width(boxWidth).Render(content))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top, box)
}
