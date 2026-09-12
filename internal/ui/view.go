package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zsuroy/ctty/internal/i18n"
)

// View renders the complete user interface
func (m Model) View() string {
	if !m.ready {
		return i18n.T("table.loading")
	}

	// Handle different view modes
	switch m.viewMode {
	case ViewAdd:
		if m.addForm != nil {
			return m.addForm.View()
		}
	case ViewEdit:
		if m.editForm != nil {
			return m.editForm.View()
		}
	case ViewMove:
		if m.moveForm != nil {
			return m.moveForm.View()
		}
	case ViewInfo:
		if m.infoForm != nil {
			return m.infoForm.View()
		}
	case ViewPortForward:
		if m.portForwardForm != nil {
			return m.portForwardForm.View()
		}
	case ViewHelp:
		if m.helpForm != nil {
			return m.helpForm.View()
		}
	case ViewFileSelector:
		if m.fileSelectorForm != nil {
			return m.fileSelectorForm.View()
		}
	case ViewSerial:
		if m.serialForm != nil {
			return m.serialForm.View()
		}
	case ViewTelnet:
		if m.telnetForm != nil {
			return m.telnetForm.View()
		}
	case ViewSFTP:
		if m.sftpForm != nil {
			return m.sftpForm.View()
		}
	case ViewFTP:
		if m.ftpSitesForm != nil {
			return m.ftpSitesForm.View()
		}
	case ViewFTPBrowse:
		if m.ftpForm != nil {
			return m.ftpForm.View()
		}
	case ViewSettings:
		if m.settingsForm != nil {
			return m.settingsForm.View()
		}
	case ViewSnippet:
		if m.snippetForm != nil {
			return m.snippetForm.View()
		}
	case ViewUpdate:
		if m.updateForm != nil {
			return m.updateForm.View()
		}
	}
	return m.renderListView()
}

// listBoxInnerWidth is the content width inside the host-list table ASCII box
// (before left/right border columns). App pad(2) + border(2) = 4.
func listBoxInnerWidth(terminalWidth int) int {
	w := terminalWidth - 4
	if w < 12 {
		return 12
	}
	return w
}

// searchMaxWidth returns the maximum display width the search bar content
// (prompt + textinput.View) should occupy, accounting for the App container
// padding (2) and search bar border+padding (4). Used by host-list search
// (renderListView) and by renderSearchBar (serial/telnet/SFTP).
func searchMaxWidth(terminalWidth int) int {
	w := terminalWidth - 6 // app padding(2) + search border(2) + search padding(2)
	if w < 5 {
		w = 5
	}
	return w
}

// fillTerminal pads content to the full terminal so a smaller frame
// overwrites leftover cells from the previous size.
func fillTerminal(width, height int, content string) string {
	if width <= 0 {
		return content
	}
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		lines = append(lines, ansi.Truncate(line, width, ""))
	}
	content = strings.Join(lines, "\n")
	if height <= 0 {
		return content
	}
	return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top, content)
}

// renderListView renders the main list interface
func (m Model) renderListView() string {
	// Build the interface components
	components := []string{}

	// Add title (compact on small terminal heights < 20 lines)
	if m.height < 20 {
		components = append(components, m.styles.Header.Render("🚀 ctty"))
	} else {
		components = append(components, m.styles.Header.Render(asciiTitle))
	}

	// Add update notification if available (between title and search)
	if m.updateInfo != nil && m.updateInfo.Available {
		updateText := i18n.T("update.available",
			m.updateInfo.CurrentVer,
			m.updateInfo.LatestVer)

		updateStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("10")). // Green color
			Bold(true).
			Align(lipgloss.Center) // Center the notification

		components = append(components, updateStyle.Render(updateText))
	}

	// Add error message if there's one to show
	if m.showingError && m.errorMessage != "" {
		errorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")). // Red color
			Background(lipgloss.Color("1")). // Dark red background
			Bold(true).
			Padding(0, 1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("9")).
			Align(lipgloss.Center)

		components = append(components, errorStyle.Render("❌ "+m.errorMessage))
	}

	// Add status toast notification if active
	if m.statusActive() {
		statusStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("10")). // Green color
			Bold(true).
			Align(lipgloss.Center)

		components = append(components, statusStyle.Render("✓ "+m.statusMessage))
	}

	// Add indicator when hidden hosts are shown
	if m.showHidden {
		hiddenBannerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("11")).
			Bold(true)
		components = append(components, hiddenBannerStyle.Render(i18n.T("main.show_hidden")))
	}

	// Search keeps master-style rounded SearchFocused/Unfocused chrome.
	searchPrompt := i18n.T("search.prompt")
	searchContent := searchPrompt + m.searchInput.View()
	searchMaxW := searchMaxWidth(m.width)
	searchContent = ansi.Truncate(searchContent, searchMaxW, "")
	if m.searchMode {
		components = append(components, m.styles.SearchFocused.Render(searchContent))
	} else {
		components = append(components, m.styles.SearchUnfocused.Render(searchContent))
	}

	// Table only: manual ASCII box (not lipgloss Border+Width). JetBrains/JediTerm
	// advances gray status circle by 1 while ansi.StringWidth counts 2; padToTerminalWidth
	// + renderAsciiBox keep right borders aligned with corners.
	boxInner := listBoxInnerWidth(m.width)
	tableFg := PrimaryColor
	if m.searchMode {
		tableFg = SecondaryColor
	}
	components = append(components, renderAsciiBox(boxInner, m.renderTableView(), tableFg))

	// Add the help text, truncated to terminal width
	var helpText string
	if !m.searchMode {
		helpText = i18n.T("main.help")
	} else {
		helpText = i18n.T("search.help")
	}
	helpMaxW := m.width - 2 // App padding
	if helpMaxW < 5 {
		helpMaxW = 5
	}
	components = append(components, m.styles.HelpText.MaxWidth(helpMaxW).Render(helpText))

	// Join all components vertically with appropriate spacing
	mainView := m.styles.App.Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			components...,
		),
	)

	// If in delete mode, overlay the confirmation dialog
	if m.deleteMode {
		// Combine the main view with the confirmation dialog overlay
		confirmation := m.renderDeleteConfirmation()

		// Center the confirmation dialog on the screen
		centeredConfirmation := lipgloss.Place(
			m.width,
			m.height,
			lipgloss.Center,
			lipgloss.Center,
			confirmation,
		)

		return centeredConfirmation
	}

	return mainView
}

// renderDeleteConfirmation renders a clean delete confirmation dialog
func (m Model) renderDeleteConfirmation() string {
	if m.deleteHost == nil {
		return ""
	}

	host := m.deleteHost
	msg := i18n.T("delete.confirm", host.Name)

	confirmation := m.styles.Error.Render(msg)

	// Wrap in a centered style
	return lipgloss.NewStyle().
		Align(lipgloss.Center).
		Render(confirmation)
}

// renderUpdateNotification renders the update notification banner
func (m Model) renderUpdateNotification() string {
	if m.updateInfo == nil || !m.updateInfo.Available {
		return ""
	}

	text := i18n.T("update.available",
		m.updateInfo.CurrentVer,
		m.updateInfo.LatestVer)

	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")).
		Bold(true).
		Align(lipgloss.Center).
		Render(text)
}

// renderSearchBar is a shared helper for rendering the search bar with proper
// width constraints. Used by serial, telnet, and SFTP views (rounded Search
// styles). Host list search uses the same SearchFocused/Unfocused styles inline
// in renderListView.
func renderSearchBar(styles Styles, searchMode bool, prompt string, searchView string, terminalWidth int) string {
	content := prompt + searchView
	maxW := searchMaxWidth(terminalWidth)
	// Truncate to fit: ansi.Truncate handles ANSI escape codes and CJK display widths.
	// Without this, CJK placeholder text in textinput.View() can exceed the expected
	// Width+1 display cols, pushing the search border off-screen on narrow terminals.
	content = ansi.Truncate(content, maxW, "")
	if searchMode {
		return styles.SearchFocused.Render(content)
	}
	return styles.SearchUnfocused.Render(content)
}

// dedupeStrings drops empty and consecutive duplicate strings (stable order).
func dedupeStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if len(out) > 0 && out[len(out)-1] == s {
			continue
		}
		out = append(out, s)
	}
	return out
}

// renderHelpText is a shared helper for rendering help text truncated to terminal width.
func renderHelpText(styles Styles, text string, terminalWidth int) string {
	maxW := terminalWidth - 2 // App padding
	if maxW < 5 {
		maxW = 5
	}
	return styles.HelpText.MaxWidth(maxW).Render(text)
}

// lineDisplayWidth returns the display width of a string, used for debugging.
func lineDisplayWidth(s string) int {
	return ansi.StringWidth(s)
}
