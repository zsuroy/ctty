package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/i18n"
)

type helpModel struct {
	styles       Styles
	width        int
	height       int
	scrollOffset int
}

// helpCloseMsg is sent when the help window is closed
type helpCloseMsg struct{}

// NewHelpForm creates a new help form model
func NewHelpForm(styles Styles, width, height int) *helpModel {
	return &helpModel{
		styles: styles,
		width:  width,
		height: height,
	}
}

func (m *helpModel) Init() tea.Cmd {
	return nil
}

func (m *helpModel) Update(msg tea.Msg) (*helpModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q", "h", "enter", "ctrl+c":
			return m, func() tea.Msg { return helpCloseMsg{} }
		case "up", "k":
			if m.scrollOffset > 0 {
				m.scrollOffset--
			}
			return m, nil
		case "down", "j":
			m.scrollOffset++
			return m, nil
		case "pgup":
			m.scrollOffset -= 5
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
			return m, nil
		case "pgdown":
			m.scrollOffset += 5
			return m, nil
		}
	}
	return m, nil
}

func (m *helpModel) View() string {
	title := m.styles.Header.Render(i18n.T("help.title"))

	col1 := lipgloss.JoinVertical(lipgloss.Left,
		m.styles.FocusedLabel.Render(i18n.T("help.cat_nav")),
		"",
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("⏎   "), m.styles.HelpText.Render(i18n.T("help.connect"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("i   "), m.styles.HelpText.Render(i18n.T("help.show_info"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("/   "), m.styles.HelpText.Render(i18n.T("help.search"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("t   "), m.styles.HelpText.Render(i18n.T("help.serial_manager"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("o   "), m.styles.HelpText.Render(i18n.T("help.sftp_browser"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("Tab "), m.styles.HelpText.Render(i18n.T("help.switch_focus"))),
		"",
		m.styles.FocusedLabel.Render(i18n.T("help.cat_host_mgmt")),
		"",
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("a   "), m.styles.HelpText.Render(i18n.T("help.add_host"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("e   "), m.styles.HelpText.Render(i18n.T("help.edit_host"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("m   "), m.styles.HelpText.Render(i18n.T("help.move_host"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("d   "), m.styles.HelpText.Render(i18n.T("help.delete_host"))),
	)

	col2 := lipgloss.JoinVertical(lipgloss.Left,
		m.styles.FocusedLabel.Render(i18n.T("help.cat_adv")),
		"",
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("p   "), m.styles.HelpText.Render(i18n.T("help.ping_all"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("H   "), m.styles.HelpText.Render(i18n.T("help.toggle_hidden"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("f   "), m.styles.HelpText.Render(i18n.T("help.port_forward"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("s   "), m.styles.HelpText.Render(i18n.T("help.cycle_sort"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("n   "), m.styles.HelpText.Render(i18n.T("help.sort_name"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("r   "), m.styles.HelpText.Render(i18n.T("help.sort_recent"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("S   "), m.styles.HelpText.Render(i18n.T("help.settings"))),
		"",
		m.styles.FocusedLabel.Render(i18n.T("help.cat_system")),
		"",
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("h   "), m.styles.HelpText.Render(i18n.T("help.show_help"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("q   "), m.styles.HelpText.Render(i18n.T("help.quit"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("ESC "), m.styles.HelpText.Render(i18n.T("help.exit_view"))),
	)

	col3 := lipgloss.JoinVertical(lipgloss.Left,
		m.styles.FocusedLabel.Render(i18n.T("help.cat_serial")),
		"",
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("⏎   "), m.styles.HelpText.Render(i18n.T("help.serial_connect"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("i   "), m.styles.HelpText.Render(i18n.T("help.serial_info"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("e   "), m.styles.HelpText.Render(i18n.T("help.serial_edit"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("←/→ "), m.styles.HelpText.Render(i18n.T("help.serial_baud"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("a   "), m.styles.HelpText.Render(i18n.T("help.serial_add"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("d   "), m.styles.HelpText.Render(i18n.T("help.serial_delete"))),
		"",
		m.styles.FocusedLabel.Render(i18n.T("help.cat_sftp")),
		"",
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("→/l "), m.styles.HelpText.Render(i18n.T("help.sftp_open_dir"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("←/h "), m.styles.HelpText.Render(i18n.T("help.sftp_parent_dir"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("⏎   "), m.styles.HelpText.Render(i18n.T("help.sftp_download"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("u   "), m.styles.HelpText.Render(i18n.T("help.sftp_upload"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("d   "), m.styles.HelpText.Render(i18n.T("help.sftp_delete"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("n   "), m.styles.HelpText.Render(i18n.T("help.sftp_mkdir"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("r   "), m.styles.HelpText.Render(i18n.T("help.sftp_refresh"))),
	)

	var columns string
	if m.width >= 80 {
		columns = lipgloss.JoinHorizontal(lipgloss.Top, col1, "    ", col2, "    ", col3)
	} else if m.width >= 55 {
		colLeft := lipgloss.JoinVertical(lipgloss.Left, col1, "", col2)
		columns = lipgloss.JoinHorizontal(lipgloss.Top, colLeft, "    ", col3)
	} else {
		columns = lipgloss.JoinVertical(lipgloss.Left, col1, "", col2, "", col3)
	}

	contentLines := strings.Split(columns, "\n")
	totalHeight := m.height
	if totalHeight <= 0 {
		totalHeight = 24
	}

	viewportHeight := totalHeight - 6 // Reserve 6 lines for title, footer, padding
	if viewportHeight < 5 {
		viewportHeight = 5
	}

	if len(contentLines) <= viewportHeight {
		m.scrollOffset = 0
	} else {
		if m.scrollOffset > len(contentLines)-viewportHeight {
			m.scrollOffset = len(contentLines) - viewportHeight
		}
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}
	}

	endIdx := m.scrollOffset + viewportHeight
	if endIdx > len(contentLines) {
		endIdx = len(contentLines)
	}
	visibleContent := strings.Join(contentLines[m.scrollOffset:endIdx], "\n")

	promptText := i18n.T("help.close_prompt")
	if len(contentLines) > viewportHeight {
		promptText = "↑/↓/PgUp/PgDn: scroll • " + promptText
	}

	content := lipgloss.JoinVertical(lipgloss.Center,
		title,
		"",
		visibleContent,
		"",
		m.styles.HelpText.Render(promptText),
	)

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		m.styles.FormContainer.Render(content),
	)
}
