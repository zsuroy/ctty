package ui

import (
	"errors"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/s3config"
	"github.com/zsuroy/ctty/internal/s3cred"
	"github.com/zsuroy/ctty/internal/ui/theme"
)

type s3AddFormModel struct {
	form     *huh.Form
	viewport viewport.Model
	styles   Styles
	width    int
	height   int
	editing  *s3config.S3Site
	err      string

	nameVal        string
	endpointVal    string
	bucketVal      string
	regionVal      string
	accessKeyVal   string
	secretKeyVal   string
	useSSLVal      bool
	insecureTLSVal bool
	pathStyleVal   bool
	tagsVal        string
	confirmVal     bool

	done      bool
	cancelled bool
}

func newS3AddForm(styles Styles, width, height int, initial *s3config.S3Site) *s3AddFormModel {
	m := &s3AddFormModel{
		styles:      styles,
		width:       width,
		height:      height,
		editing:     initial,
		endpointVal: "s3.amazonaws.com",
		regionVal:   "us-east-1",
		useSSLVal:   true,
		confirmVal:  true,
	}
	if initial != nil {
		m.nameVal = initial.Name
		m.endpointVal = initial.Endpoint
		m.bucketVal = initial.Bucket
		m.regionVal = initial.Region
		m.accessKeyVal = initial.AccessKey
		m.useSSLVal = initial.UseSSL
		m.insecureTLSVal = initial.InsecureTLS
		m.pathStyleVal = initial.PathStyle
		if secret, ok := s3cred.GetSecretKey(initial.Name); ok {
			m.secretKeyVal = secret
		}
		m.tagsVal = strings.Join(initial.Tags, ", ")
	}
	m.buildForm()
	return m
}

func (m *s3AddFormModel) buildForm() {
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
	validateEndpoint := func(s string) error {
		val := strings.TrimSpace(s)
		if val == "" {
			return errors.New(i18n.T("s3.err_endpoint_req"))
		}
		return nil
	}

	currentHuhTheme := theme.GetTheme(m.styles.Theme.ID).HuhTheme()

	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Key("name").
				Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("s3.field_name"), ":"))).
				Prompt("> ").
				Placeholder(i18n.T("s3.placeholder_name")).
				Validate(validateName).
				Value(&m.nameVal),

			huh.NewInput().
				Key("endpoint").
				Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("s3.field_endpoint"), ":"))).
				Prompt("> ").
				Placeholder("s3.amazonaws.com / 127.0.0.1:9000").
				Validate(validateEndpoint).
				Value(&m.endpointVal),

			huh.NewInput().
				Key("bucket").
				Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("s3.field_bucket"), ":"))).
				Prompt("> ").
				Placeholder(i18n.T("s3.placeholder_bucket")).
				Value(&m.bucketVal),

			huh.NewInput().
				Key("region").
				Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("s3.field_region"), ":"))).
				Prompt("> ").
				Placeholder("us-east-1").
				Value(&m.regionVal),

			huh.NewInput().
				Key("access_key").
				Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("s3.field_access_key"), ":"))).
				Prompt("> ").
				Placeholder(i18n.T("s3.placeholder_access_key")).
				Value(&m.accessKeyVal),

			huh.NewInput().
				Key("secret_key").
				Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("s3.field_secret_key"), ":"))).
				Prompt("> ").
				Placeholder(i18n.T("s3.placeholder_secret_key")).
				EchoMode(huh.EchoModePassword).
				Value(&m.secretKeyVal),

			huh.NewConfirm().
				Key("use_ssl").
				Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("s3.field_use_ssl"), ":"))).
				Affirmative(i18n.T("common.yes")).
				Negative(i18n.T("common.no")).
				WithButtonAlignment(lipgloss.Left).
				Value(&m.useSSLVal),

			huh.NewConfirm().
				Key("path_style").
				Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("s3.field_path_style"), ":"))).
				Affirmative(i18n.T("common.yes")).
				Negative(i18n.T("common.no")).
				WithButtonAlignment(lipgloss.Left).
				Value(&m.pathStyleVal),

			huh.NewConfirm().
				Key("insecure").
				Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("s3.field_insecure_tls"), ":"))).
				Affirmative(i18n.T("common.yes")).
				Negative(i18n.T("common.no")).
				WithButtonAlignment(lipgloss.Left).
				Value(&m.insecureTLSVal),

			huh.NewInput().
				Key("tags").
				Title(strings.TrimSpace(strings.TrimSuffix(i18n.T("s3.field_tags"), ":"))).
				Prompt("> ").
				Placeholder(i18n.T("s3.placeholder_tags")).
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

	m.viewport = viewport.New(m.width, m.height)
}

func (m *s3AddFormModel) Init() tea.Cmd {
	if m.form != nil {
		return m.form.Init()
	}
	return nil
}

func (m *s3AddFormModel) nextField() tea.Cmd {
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

func (m *s3AddFormModel) prevField() tea.Cmd {
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

func (m *s3AddFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (m *s3AddFormModel) submit() {
	name := strings.TrimSpace(m.nameVal)
	endpoint := strings.TrimSpace(m.endpointVal)
	if name == "" || endpoint == "" {
		m.err = i18n.T("s3.err_name_endpoint_req")
		return
	}

	var tags []string
	if m.tagsVal != "" {
		for _, t := range strings.Split(m.tagsVal, ",") {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
	}

	site := s3config.S3Site{
		Name:        name,
		Endpoint:    endpoint,
		Bucket:      strings.TrimSpace(m.bucketVal),
		Region:      strings.TrimSpace(m.regionVal),
		AccessKey:   strings.TrimSpace(m.accessKeyVal),
		UseSSL:      m.useSSLVal,
		InsecureTLS: m.insecureTLSVal,
		PathStyle:   m.pathStyleVal,
		Tags:        tags,
	}

	var err error
	if m.editing == nil {
		err = s3config.Add(site)
	} else {
		oldName := m.editing.Name
		err = s3config.Update(oldName, site)
		if err == nil && oldName != name {
			_ = s3cred.DeleteSecretKey(oldName)
		}
	}
	if err != nil {
		m.err = err.Error()
		return
	}

	if m.secretKeyVal != "" {
		if err := s3cred.SetSecretKey(site.Name, m.secretKeyVal); err != nil {
			m.err = err.Error()
			return
		}
	} else if m.editing != nil {
		_ = s3cred.DeleteSecretKey(site.Name)
	}

	m.done = true
}

func (m *s3AddFormModel) getFieldTitle(key string) string {
	switch key {
	case "name":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("s3.field_name"), ":"))
	case "endpoint":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("s3.field_endpoint"), ":"))
	case "bucket":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("s3.field_bucket"), ":"))
	case "region":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("s3.field_region"), ":"))
	case "access_key":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("s3.field_access_key"), ":"))
	case "secret_key":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("s3.field_secret_key"), ":"))
	case "use_ssl":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("s3.field_use_ssl"), ":"))
	case "path_style":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("s3.field_path_style"), ":"))
	case "insecure":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("s3.field_insecure_tls"), ":"))
	case "tags":
		return strings.TrimSpace(strings.TrimSuffix(i18n.T("s3.field_tags"), ":"))
	case "confirm":
		return i18n.T("form.btn_save")
	default:
		return ""
	}
}

func (m *s3AddFormModel) View() string {
	title := i18n.T("s3.sites_add_title")
	if m.editing != nil {
		title = i18n.T("s3.sites_edit_title")
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

	helpText := m.styles.HelpText.Width(innerW).Render(i18n.T("s3.sites_form_help"))
	credHint := m.styles.HelpText.Width(innerW).Render(i18n.T("s3.cred_hint"))
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
