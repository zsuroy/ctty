package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/i18n"
)

type settingsField int

const (
	settingsFieldLang settingsField = iota
	settingsFieldUpdate
	settingsFieldEscQuit
	numSettingsFields
)

var langKeys = []string{"auto", "zh_CN", "en"}

type settingsFormModel struct {
	styles     Styles
	width      int
	height     int
	appConfig  config.AppConfig
	focusIndex settingsField

	langIndex       int // 0=auto, 1=zh_CN, 2=en
	updateIndex     int // 0=enabled, 1=disabled
	disableEscIndex int // 0=enabled (quit on esc), 1=disabled (vim mode)

	saved     bool
	cancelled bool
}

type settingsCloseMsg struct {
	Saved     bool
	AppConfig *config.AppConfig
}

// NewSettingsForm creates a new settings form
func NewSettingsForm(styles Styles, width, height int, appConfig *config.AppConfig) *settingsFormModel {
	cfg := config.GetDefaultAppConfig()
	if appConfig != nil {
		cfg = *appConfig
	}

	// Match current language index
	langIdx := 0 // default "auto"
	norm := strings.ToLower(strings.TrimSpace(cfg.Language))
	switch {
	case strings.HasPrefix(norm, "zh"):
		langIdx = 1
	case strings.HasPrefix(norm, "en"):
		langIdx = 2
	default:
		langIdx = 0
	}

	// Match update check index
	updateIdx := 0
	if cfg.CheckForUpdates != nil && !*cfg.CheckForUpdates {
		updateIdx = 1
	}

	// Match disableEscQuit index
	escIdx := 0
	if cfg.KeyBindings.DisableEscQuit {
		escIdx = 1
	}

	return &settingsFormModel{
		styles:          styles,
		width:           width,
		height:          height,
		appConfig:       cfg,
		focusIndex:      settingsFieldLang,
		langIndex:       langIdx,
		updateIndex:     updateIdx,
		disableEscIndex: escIdx,
	}
}

func (m *settingsFormModel) Init() tea.Cmd {
	return nil
}

func (m *settingsFormModel) Update(msg tea.Msg) (*settingsFormModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q", "ctrl+c":
			m.cancelled = true
			return m, func() tea.Msg { return settingsCloseMsg{Saved: false} }

		case "tab", "down", "j":
			m.focusIndex = (m.focusIndex + 1) % numSettingsFields
			return m, nil

		case "shift+tab", "up", "k":
			if m.focusIndex == 0 {
				m.focusIndex = numSettingsFields - 1
			} else {
				m.focusIndex--
			}
			return m, nil

		case "left", "h":
			m.adjustField(-1)
			return m, nil

		case "right", "l", " ":
			m.adjustField(1)
			return m, nil

		case "enter", "ctrl+s":
			return m, m.saveSettings()
		}
	}

	return m, nil
}

func (m *settingsFormModel) adjustField(dir int) {
	switch m.focusIndex {
	case settingsFieldLang:
		m.langIndex = (m.langIndex + dir + len(langKeys)) % len(langKeys)
	case settingsFieldUpdate:
		m.updateIndex = (m.updateIndex + dir + 2) % 2
	case settingsFieldEscQuit:
		m.disableEscIndex = (m.disableEscIndex + dir + 2) % 2
	}
}

func (m *settingsFormModel) saveSettings() tea.Cmd {
	return func() tea.Msg {
		// Update AppConfig values
		m.appConfig.Language = langKeys[m.langIndex]
		enableUpdates := (m.updateIndex == 0)
		m.appConfig.CheckForUpdates = &enableUpdates
		m.appConfig.KeyBindings.DisableEscQuit = (m.disableEscIndex == 1)

		// Persist to disk
		_ = config.SaveAppConfig(&m.appConfig)

		// Apply language immediately
		i18n.Init(m.appConfig.Language)

		m.saved = true
		return settingsCloseMsg{
			Saved:     true,
			AppConfig: &m.appConfig,
		}
	}
}

func (m *settingsFormModel) View() string {
	var b strings.Builder

	// Title
	b.WriteString(m.styles.Header.Render(i18n.T("settings.title")))
	b.WriteString("\n\n")

	// 1. Language options
	var langDisplay []string
	if i18n.CurrentLang() == i18n.LangZHCN {
		langDisplay = []string{"跟随系统 (Auto)", "简体中文 (zh_CN)", "English (en)"}
	} else {
		langDisplay = []string{"Follow System (Auto)", "Simplified Chinese (zh_CN)", "English (en)"}
	}
	m.renderRow(&b, settingsFieldLang, i18n.T("settings.lang_label"), langDisplay[m.langIndex])

	// 2. Check for updates
	var updateDisplay []string
	if i18n.CurrentLang() == i18n.LangZHCN {
		updateDisplay = []string{"开启 (Enabled)", "关闭 (Disabled)"}
	} else {
		updateDisplay = []string{"Enabled", "Disabled"}
	}
	m.renderRow(&b, settingsFieldUpdate, i18n.T("settings.update_label"), updateDisplay[m.updateIndex])

	// 3. ESC behavior
	var escDisplay []string
	if i18n.CurrentLang() == i18n.LangZHCN {
		escDisplay = []string{"允许直接退出 (默认)", "禁用 ESC 退出 (Vim 模式)"}
	} else {
		escDisplay = []string{"Quit on ESC (Default)", "Disable ESC Quit (Vim mode)"}
	}
	m.renderRow(&b, settingsFieldEscQuit, i18n.T("settings.esc_quit_label"), escDisplay[m.disableEscIndex])

	b.WriteString("\n")
	b.WriteString(m.styles.HelpText.Render(i18n.T("settings.help")))

	return m.styles.FormContainer.Render(b.String())
}

func (m *settingsFormModel) renderRow(b *strings.Builder, idx settingsField, label, value string) {
	labelStyle := m.styles.FormField
	arrowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(SecondaryColor))
	valStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)

	if m.focusIndex == idx {
		labelStyle = m.styles.FocusedLabel
		arrowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(PrimaryColor)).Bold(true)
		valStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(PrimaryColor)).Bold(true)
	}

	b.WriteString(labelStyle.Render(fmt.Sprintf("  %-28s", label)))
	b.WriteString(arrowStyle.Render("◂ "))
	b.WriteString(valStyle.Render(value))
	b.WriteString(arrowStyle.Render(" ▸"))
	b.WriteString("\n\n")
}
