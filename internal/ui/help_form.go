package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/version"
)

type helpModel struct {
	styles       Styles
	width        int
	height       int
	scrollOffset int
	version      string
}

// helpCloseMsg is sent when the help window is closed
type helpCloseMsg struct{}

// NewHelpForm creates a new help form model
func NewHelpForm(styles Styles, width, height int, ver string) *helpModel {
	return &helpModel{
		styles:  styles,
		width:   width,
		height:  height,
		version: ver,
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
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("T   "), m.styles.HelpText.Render(i18n.T("help.telnet_manager"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("o   "), m.styles.HelpText.Render(i18n.T("help.sftp_browser"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("x   "), m.styles.HelpText.Render(i18n.T("help.exec_cmd"))),
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
		m.styles.FocusedLabel.Render(i18n.T("help.cat_snippet")),
		"",
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("n   "), m.styles.HelpText.Render(i18n.T("help.snippet_add"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("d   "), m.styles.HelpText.Render(i18n.T("help.snippet_delete"))),
		"",
		m.styles.FocusedLabel.Render(i18n.T("help.cat_system")),
		"",
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("U   "), m.styles.HelpText.Render(i18n.T("help.update"))),
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
		m.styles.FocusedLabel.Render(i18n.T("help.cat_telnet")),
		"",
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("⏎   "), m.styles.HelpText.Render(i18n.T("help.telnet_connect"))),
		lipgloss.JoinHorizontal(lipgloss.Left, m.styles.FocusedLabel.Render("p   "), m.styles.HelpText.Render(i18n.T("help.telnet_probe"))),
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
	totalWidth := m.width
	if totalWidth <= 0 {
		totalWidth = 80
	}

	boxStyle := m.styles.FormContainer
	if totalHeight < 22 {
		boxStyle = boxStyle.Padding(0, 1)
	}

	compact := totalHeight < 16
	headerLines := 2 // title + repo
	footerLines := 1 // close prompt
	if !compact {
		headerLines++
		footerLines++
	}
	reserved := boxStyle.GetVerticalFrameSize() + headerLines + footerLines
	viewportHeight := totalHeight - reserved
	if viewportHeight < 1 {
		viewportHeight = 1
	}

	contentMaxW := totalWidth - boxStyle.GetHorizontalFrameSize()
	if contentMaxW < 10 {
		contentMaxW = 10
	}

	versionStr := m.styles.HelpText.Faint(true).Render("v" + m.version)
	titleTrunc := ansi.Truncate(title+" "+versionStr, contentMaxW, "")
	repoTrunc := ansi.Truncate(m.styles.HelpText.Faint(true).Render(version.RepoURL()), contentMaxW, "")

	var box string
	for {
		m.scrollOffset = clampHelpScroll(m.scrollOffset, len(contentLines), viewportHeight)
		endIdx := m.scrollOffset + viewportHeight
		if endIdx > len(contentLines) {
			endIdx = len(contentLines)
		}
		var truncatedLines []string
		for _, line := range contentLines[m.scrollOffset:endIdx] {
			truncatedLines = append(truncatedLines, ansi.Truncate(line, contentMaxW, ""))
		}
		visibleContent := strings.Join(truncatedLines, "\n")

		promptText := i18n.T("help.close_prompt")
		if len(contentLines) > viewportHeight {
			promptText = "↑/↓/PgUp/PgDn: scroll • " + promptText
		}
		promptTrunc := ansi.Truncate(m.styles.HelpText.Render(promptText), contentMaxW, "")

		parts := []string{titleTrunc, repoTrunc}
		if !compact {
			parts = append(parts, "")
		}
		parts = append(parts, visibleContent)
		if !compact {
			parts = append(parts, "")
		}
		parts = append(parts, promptTrunc)

		inner := lipgloss.JoinVertical(lipgloss.Center, parts...)
		box = boxStyle.MaxWidth(totalWidth).Render(inner)
		if lipgloss.Height(box) <= totalHeight || viewportHeight <= 1 {
			break
		}
		viewportHeight--
	}

	vAlign := lipgloss.Center
	if lipgloss.Height(box) >= totalHeight {
		vAlign = lipgloss.Top
	}
	placed := lipgloss.Place(totalWidth, totalHeight, lipgloss.Center, vAlign, box)
	return lipgloss.NewStyle().MaxHeight(totalHeight).MaxWidth(totalWidth).Render(placed)
}

func clampHelpScroll(offset, n, viewport int) int {
	if n <= viewport {
		return 0
	}
	maxOff := n - viewport
	if offset > maxOff {
		return maxOff
	}
	if offset < 0 {
		return 0
	}
	return offset
}
