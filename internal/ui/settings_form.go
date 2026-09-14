package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/ui/theme"
)

type settingsCloseMsg struct {
	Saved     bool
	AppConfig *config.AppConfig
}

type settingsFormModel struct {
	styles    Styles
	width     int
	height    int
	appConfig config.AppConfig
	form      *huh.Form
	viewport  viewport.Model

	langVal       string
	themeVal      string
	updateVal     bool
	disableEscVal bool
	ftpLayoutVal  string
	sftpLayoutVal string
	confirmSave   bool

	prevTheme string
	saved     bool
	cancelled bool
}

// NewSettingsForm creates a new modern settings form based on charmbracelet/huh
func NewSettingsForm(styles Styles, width, height int, appConfig *config.AppConfig) *settingsFormModel {
	cfg := config.GetDefaultAppConfig()
	if appConfig != nil {
		cfg = *appConfig
	}

	langVal := cfg.Language
	if langVal == "" {
		langVal = "auto"
	}

	themeVal := cfg.Theme
	if themeVal == "" {
		themeVal = "default"
	}

	updateVal := cfg.IsUpdateCheckEnabled()
	disableEscVal := cfg.KeyBindings.DisableEscQuit

	ftpLayoutVal := string(config.NormalizeFTPLayout(cfg.FTPLayout))
	sftpLayoutVal := string(config.NormalizeSFTPLayout(cfg.SFTPLayout))

	m := &settingsFormModel{
		styles:        styles,
		width:         width,
		height:        height,
		appConfig:     cfg,
		langVal:       langVal,
		themeVal:      themeVal,
		updateVal:     updateVal,
		disableEscVal: disableEscVal,
		ftpLayoutVal:  ftpLayoutVal,
		sftpLayoutVal: sftpLayoutVal,
		confirmSave:   true,
		prevTheme:     themeVal,
	}

	m.buildForm()
	return m
}

func (m *settingsFormModel) buildForm() {
	allThemes := theme.AllThemes()
	themeOptions := make([]huh.Option[string], len(allThemes))
	for i, t := range allThemes {
		themeOptions[i] = huh.NewOption(t.Name, t.ID)
	}

	currentHuhTheme := theme.GetTheme(m.themeVal).HuhTheme()
	innerW := formPageInnerWidth(m.width)
	if innerW < 20 {
		innerW = 20
	}

	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Key("lang").
				Title(i18n.T("settings.lang_label")).
				Inline(true).
				Options(
					huh.NewOption(i18n.T("settings.lang_auto"), "auto"),
					huh.NewOption(i18n.T("settings.lang_zh"), "zh_CN"),
					huh.NewOption(i18n.T("settings.lang_en"), "en"),
				).
				Value(&m.langVal),

			huh.NewSelect[string]().
				Key("theme").
				Title(i18n.T("settings.theme_label")).
				Inline(true).
				Options(themeOptions...).
				Value(&m.themeVal),

			huh.NewSelect[bool]().
				Key("update").
				Title(i18n.T("settings.update_label")).
				Inline(true).
				Options(
					huh.NewOption(i18n.T("settings.update_on"), true),
					huh.NewOption(i18n.T("settings.update_off"), false),
				).
				Value(&m.updateVal),

			huh.NewSelect[bool]().
				Key("esc").
				Title(i18n.T("settings.esc_quit_label")).
				Inline(true).
				Options(
					huh.NewOption(i18n.T("settings.esc_on"), false),
					huh.NewOption(i18n.T("settings.esc_off"), true),
				).
				Value(&m.disableEscVal),

			huh.NewSelect[string]().
				Key("ftp").
				Title(i18n.T("settings.ftp_layout_label")).
				Inline(true).
				Options(
					huh.NewOption(i18n.T("ftp.layout_dual"), "dual"),
					huh.NewOption(i18n.T("ftp.layout_single"), "single"),
				).
				Value(&m.ftpLayoutVal),

			huh.NewSelect[string]().
				Key("sftp").
				Title(i18n.T("settings.sftp_layout_label")).
				Inline(true).
				Options(
					huh.NewOption(i18n.T("sftp.layout_dual"), "dual"),
					huh.NewOption(i18n.T("sftp.layout_single"), "single"),
				).
				Value(&m.sftpLayoutVal),

			huh.NewConfirm().
				Key("save").
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

func (m *settingsFormModel) Init() tea.Cmd {
	if m.form != nil {
		return m.form.Init()
	}
	return nil
}

func (m *settingsFormModel) Update(msg tea.Msg) (*settingsFormModel, tea.Cmd) {
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
			m.cancelled = true
			return m, func() tea.Msg { return settingsCloseMsg{Saved: false} }
		case msg.String() == "ctrl+s":
			return m, m.saveSettings()
		case msg.String() == "down" || msg.String() == "j":
			if m.form != nil {
				focused := m.form.GetFocusedField()
				if focused != nil && focused.GetKey() != "save" {
					cmd := m.form.NextField()
					return m, cmd
				}
			}
			return m, nil
		case msg.String() == "up" || msg.String() == "k":
			if m.form != nil {
				focused := m.form.GetFocusedField()
				if focused != nil && focused.GetKey() != "lang" {
					cmd := m.form.PrevField()
					return m, cmd
				}
			}
			return m, nil
		case msg.Type == tea.KeyTab || msg.String() == "tab":
			if m.form != nil {
				focused := m.form.GetFocusedField()
				if focused != nil {
					if focused.GetKey() == "save" {
						for m.form.GetFocusedField().GetKey() != "lang" {
							m.form.PrevField()
						}
						return m, nil
					}
					cmd := m.form.NextField()
					return m, cmd
				}
			}
			return m, nil
		case msg.Type == tea.KeyShiftTab || msg.String() == "shift+tab" || msg.String() == "backtab":
			if m.form != nil {
				focused := m.form.GetFocusedField()
				if focused != nil {
					if focused.GetKey() == "lang" {
						for m.form.GetFocusedField().GetKey() != "save" {
							m.form.NextField()
						}
						return m, nil
					}
					cmd := m.form.PrevField()
					return m, cmd
				}
			}
			return m, nil
		case msg.Type == tea.KeyEnter || msg.String() == "enter":
			if m.form != nil {
				focused := m.form.GetFocusedField()
				if focused != nil && focused.GetKey() != "save" {
					cmd := m.form.NextField()
					return m, cmd
				}
			}
			// When focused on "save", pass Enter through to m.form.Update to submit!
		}
	}

	if m.form == nil {
		return m, nil
	}

	formModel, cmd := m.form.Update(msg)
	if f, ok := formModel.(*huh.Form); ok {
		m.form = f
	}

	// Live preview theme if user changed theme value
	if m.themeVal != m.prevTheme {
		m.prevTheme = m.themeVal
		th := theme.GetTheme(m.themeVal)
		m.styles = ApplyTheme(th)
		m.form.WithTheme(th.HuhTheme())
	}

	if m.form.State == huh.StateCompleted {
		if m.confirmSave {
			return m, m.saveSettings()
		}
		m.cancelled = true
		return m, func() tea.Msg { return settingsCloseMsg{Saved: false} }
	}

	if m.form.State == huh.StateAborted {
		m.cancelled = true
		return m, func() tea.Msg { return settingsCloseMsg{Saved: false} }
	}

	return m, cmd
}

func (m *settingsFormModel) saveSettings() tea.Cmd {
	return func() tea.Msg {
		m.appConfig.Language = m.langVal
		m.appConfig.Theme = m.themeVal
		enableUpdates := m.updateVal
		m.appConfig.CheckForUpdates = &enableUpdates
		m.appConfig.KeyBindings.DisableEscQuit = m.disableEscVal
		m.appConfig.FTPLayout = config.FTPLayout(m.ftpLayoutVal)
		m.appConfig.SFTPLayout = config.SFTPLayout(m.sftpLayoutVal)

		_ = config.SaveAppConfig(&m.appConfig)
		i18n.Init(m.appConfig.Language)
		m.saved = true

		return settingsCloseMsg{
			Saved:     true,
			AppConfig: &m.appConfig,
		}
	}
}

func (m *settingsFormModel) View() string {
	formView := ""
	if m.form != nil {
		formView = m.form.View()
	}

	boxWidth := m.width - 4
	if boxWidth < 20 {
		boxWidth = 20
	}

	container := m.styles.FormContainer
	// On compact heights (<24), use 0 vertical padding so content & borders fit comfortably
	if m.height < 24 {
		container = container.Padding(0, 1)
	}

	innerW := boxWidth - container.GetHorizontalFrameSize()
	if innerW < 10 {
		innerW = 10
	}
	header := m.styles.Header.Width(innerW).Render(i18n.T("settings.title"))

	help := m.styles.HelpText.Width(innerW).Render(i18n.T("settings.help"))

	frameH := container.GetVerticalFrameSize()
	headerH := lipgloss.Height(header)
	helpH := lipgloss.Height(help)

	// Leave 1 line breathing room at bottom so bottom border is never on terminal edge
	targetBoxH := m.height
	if m.height >= 14 {
		targetBoxH = m.height - 1
	}

	// Overhead: container border/padding + header + help + 2 empty separator lines
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

		// Auto-scroll viewport to keep focused field in view
		if m.form != nil {
			focused := m.form.GetFocusedField()
			if focused != nil {
				key := focused.GetKey()
				var titleKey string
				switch key {
				case "lang":
					titleKey = i18n.T("settings.lang_label")
				case "theme":
					titleKey = i18n.T("settings.theme_label")
				case "update":
					titleKey = i18n.T("settings.update_label")
				case "esc":
					titleKey = i18n.T("settings.esc_quit_label")
				case "ftp":
					titleKey = i18n.T("settings.ftp_layout_label")
				case "sftp":
					titleKey = i18n.T("settings.sftp_layout_label")
				case "save":
					titleKey = i18n.T("settings.save_prompt")
				}

				if titleKey != "" {
					lines := strings.Split(formView, "\n")
					for idx, line := range lines {
						if strings.Contains(line, titleKey) {
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

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		bodyView,
		"",
		help,
	)

	box := container.Width(boxWidth).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top, box)
}
