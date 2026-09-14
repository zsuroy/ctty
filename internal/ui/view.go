package ui

import (
	"fmt"
	"sort"
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

	var content string
	switch m.viewMode {
	case ViewAdd:
		if m.addForm != nil {
			content = m.addForm.View()
		}
	case ViewEdit:
		if m.editForm != nil {
			content = m.editForm.View()
		}
	case ViewMove:
		if m.moveForm != nil {
			content = m.moveForm.View()
		}
	case ViewInfo:
		if m.infoForm != nil {
			content = m.infoForm.View()
		}
	case ViewPortForward:
		if m.portForwardForm != nil {
			content = m.portForwardForm.View()
		}
	case ViewHelp:
		if m.helpForm != nil {
			content = m.helpForm.View()
		}
	case ViewFileSelector:
		if m.fileSelectorForm != nil {
			content = m.fileSelectorForm.View()
		}
	case ViewSerial:
		if m.serialForm != nil {
			content = m.serialForm.View()
		}
	case ViewTelnet:
		if m.telnetForm != nil {
			content = m.telnetForm.View()
		}
	case ViewSFTP:
		if m.sftpForm != nil {
			content = m.sftpForm.View()
		}
	case ViewFTP:
		if m.ftpSitesForm != nil {
			content = m.ftpSitesForm.View()
		}
	case ViewFTPBrowse:
		if m.ftpForm != nil {
			content = m.ftpForm.View()
		}
	case ViewLocalBrowser:
		if m.localBrowserForm != nil {
			content = m.localBrowserForm.View()
		}
	case ViewSettings:
		if m.settingsForm != nil {
			content = m.settingsForm.View()
		}
	case ViewSnippet:
		if m.snippetForm != nil {
			content = m.snippetForm.View()
		}
	case ViewUpdate:
		if m.updateForm != nil {
			content = m.updateForm.View()
		}
	default:
		content = m.renderListView()
	}

	if content == "" {
		content = m.renderListView()
	}

	return RenderCanvas(m.width, m.height, content)
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

// renderConfirmBox builds a red bordered confirmation card from message
// lines (title, question, warning, help). See renderCardBox.
func renderConfirmBox(styles Styles, width int, lines ...string) string {
	return renderCardBox(styles.Error, width, lines...)
}

// renderCardBox builds a bordered card from message lines using the given
// container style (border + padding(1,2)). The card hugs the longest line
// up to maxConfirmBoxInner columns, so short prompts stay compact and long
// ones (e.g. device paths) use the room instead of wrapping mid-word. Text
// is left-aligned — per-line center combined with ANSI-styled runs shifts
// the glyphs off the visual middle; the whole card is centered on screen by
// renderConfirmModal. On narrow terminals the card yields to the frame:
// inner never exceeds width minus border plus breathing room.
func renderCardBox(container lipgloss.Style, width int, lines ...string) string {
	const maxConfirmBoxInner = 72

	inner := 0
	for _, ln := range lines {
		if w := lipgloss.Width(ln); w > inner {
			inner = w
		}
	}
	inner += 4               // container horizontal padding (2+2)
	terminalCap := width - 6 // border(2) + one column of margin
	if inner > terminalCap { // the terminal always wins…
		inner = terminalCap
	} else if inner > maxConfirmBoxInner { // …then the readability cap
		inner = maxConfirmBoxInner
	}
	if inner < 24 && terminalCap >= 24 { // compact floor, unless terminal is tiny
		inner = 24
	}
	if inner < 4 {
		inner = 4
	}
	return container.Width(inner).Align(lipgloss.Left).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// renderConfirmModal paints a confirmation card centered on a blank
// full-screen canvas. The underlying list is deliberately not shown: a
// destructive prompt should read as a true modal, and the full-size blank
// frame also erases leftover cells from the previous view. On degenerate
// terminals the wrapped card can still outgrow the canvas; degrade it by
// stripping its padding rows first, then clipping middle content, so the
// borders (and the actionable help line) never scroll off-screen.
func renderConfirmModal(width, height int, box string) string {
	lines := strings.Split(box, "\n")
	if len(lines) <= height {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
	}

	// Degenerate terminal: drop blank interior rows (the container's
	// vertical padding) until the card fits.
	if len(lines) > height {
		var kept []string
		for i, ln := range lines {
			if i > 0 && i < len(lines)-1 && strings.TrimSpace(ln) == "" {
				continue
			}
			kept = append(kept, ln)
		}
		lines = kept
	}

	// Still too tall: keep the top border, the first rows, and the final
	// content row (the actionable key hints) plus the bottom border.
	if len(lines) > height && height >= 4 {
		body := height - 3 // top border + kept content + bottom border
		trimmed := make([]string, 0, height)
		trimmed = append(trimmed, lines[0])
		trimmed = append(trimmed, lines[1:1+body-1]...)
		trimmed = append(trimmed, lines[len(lines)-2])
		trimmed = append(trimmed, lines[len(lines)-1])
		lines = trimmed
	}

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, strings.Join(lines, "\n"))
}

// wrapInfoLines expands logical body lines into physical rows at the given
// width. Info views measure their scroll viewport in LINES, but the
// container re-wraps long values (device paths, proxy jumps) at paint time;
// counting unwrapped rows under-budgets and clips the bottom border.
func wrapInfoLines(lines []string, width int) []string {
	if width < 10 {
		width = 10
	}
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		out = append(out, strings.Split(ansi.Wrap(ln, width, " "), "\n")...)
	}
	return out
}

// scrollInfoKey advances an info-view scroll offset for up/down (vim keys
// included) and reports whether the key was consumed. Callers keep their
// own close/edit keys; everything else falls through untouched.
func scrollInfoKey(key string, offset *int) bool {
	switch key {
	case "up", "k":
		if *offset > 0 {
			*offset--
		}
		return true
	case "down", "j":
		*offset++
		return true
	}
	return false
}

// scrollInfoWindow clamps the offset to the last page and returns the
// visible slice of info body lines. Replaces the old silent truncation that
// dropped rows past the viewport with no way to reach them.
func scrollInfoWindow(bodyLines []string, viewportHeight int, offset *int) string {
	if len(bodyLines) <= viewportHeight {
		*offset = 0
		return strings.Join(bodyLines, "\n")
	}
	if *offset > len(bodyLines)-viewportHeight {
		*offset = len(bodyLines) - viewportHeight
	}
	if *offset < 0 {
		*offset = 0
	}
	return strings.Join(bodyLines[*offset:*offset+viewportHeight], "\n")
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

	// Add indicator when hidden hosts are shown
	if m.showHidden {
		hiddenBannerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("11")).
			Bold(true)
		components = append(components, hiddenBannerStyle.Render(i18n.T("main.show_hidden")))
	}

	// Add indicator when tag filter is active
	if m.selectedTag != "" {
		tagBannerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("14")).
			Bold(true)
		bannerText := fmt.Sprintf(i18n.T("tags.active_banner"), m.selectedTag, len(m.filteredHosts))
		components = append(components, tagBannerStyle.Render(bannerText))
	}

	// Protocol tabs navigation bar (when terminal height allows)
	if m.height >= 18 {
		if tabs := renderProtocolTabs(m.styles, "ssh", len(m.hosts), m.width); tabs != "" {
			components = append(components, tabs)
		}
	}

	// Search keeps master-style rounded SearchFocused/Unfocused chrome.
	searchPrompt := i18n.T("search.prompt")
	searchContent := searchPrompt + m.searchInput.View()
	searchMaxW := searchMaxWidth(m.width)

	// Position and match counter badge inside search bar
	if m.width >= 50 && len(m.hosts) > 0 {
		var badge string
		if len(m.selectedHosts) > 0 {
			badge = fmt.Sprintf("[%s]", fmt.Sprintf(i18n.T("main.selected_count"), len(m.selectedHosts)))
		} else if m.searchInput.Value() != "" {
			badge = fmt.Sprintf("[%d/%d %s]", len(m.filteredHosts), len(m.hosts), i18n.T("search.matched"))
		} else {
			cursor := m.table.Cursor() + 1
			if cursor > len(m.hosts) {
				cursor = len(m.hosts)
			}
			badge = fmt.Sprintf("[%d/%d]", cursor, len(m.hosts))
		}
		gap := searchMaxW - ansi.StringWidth(searchContent) - ansi.StringWidth(badge)
		if gap >= 2 {
			searchContent = searchContent + strings.Repeat(" ", gap) + badge
		}
	}

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

	// Status toast notification: rendered at the bottom directly below the table
	// so the header, tabs, search bar, and table never jitter/shift vertically.
	if m.statusActive() {
		components = append(components, renderStatusToast(m.statusMessage))
	}

	// Add the help text, truncated to terminal width
	var helpText string
	if !m.searchMode {
		helpText = renderMainHelp(m.styles, i18n.T("main.help"), m.width)
	} else {
		helpMaxW := m.width - 2 // App padding
		if helpMaxW < 5 {
			helpMaxW = 5
		}
		helpText = m.styles.HelpText.MaxWidth(helpMaxW).Render(i18n.T("search.help"))
	}
	components = append(components, helpText)

	// Join all components vertically with appropriate spacing
	mainView := m.styles.App.Render(
		lipgloss.JoinVertical(
			lipgloss.Left,
			components...,
		),
	)

	// If in delete mode, show the modal confirmation on a blank screen.
	if m.deleteMode {
		return renderConfirmModal(m.width, m.height, m.renderDeleteConfirmation())
	}

	// If in tag picker mode, show the tag filter drawer on a modal canvas.
	if m.tagPickerOpen {
		return renderConfirmModal(m.width, m.height, m.renderTagPicker())
	}

	// If in quick peek mode, show the health stats modal card.
	if m.peekOpen {
		return renderConfirmModal(m.width, m.height, m.renderPeekModal())
	}

	// If in batch exec mode, show the batch results modal card.
	if m.batchResultOpen {
		return renderConfirmModal(m.width, m.height, m.renderBatchModal())
	}

	return mainView
}

type tagCountItem struct {
	tag   string
	count int
}

func (m Model) getTagCounts() []tagCountItem {
	counts := make(map[string]int)
	source := m.allHosts
	if len(source) == 0 {
		source = m.hosts
	}
	for _, h := range source {
		for _, t := range h.Tags {
			cleaned := strings.TrimSpace(strings.TrimPrefix(t, "#"))
			if cleaned != "" {
				counts[cleaned]++
			}
		}
	}
	items := make([]tagCountItem, 0, len(counts))
	for tag, count := range counts {
		items = append(items, tagCountItem{tag: tag, count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count != items[j].count {
			return items[i].count > items[j].count // highest frequency first
		}
		return items[i].tag < items[j].tag
	})
	return items
}

// renderTagPicker renders the centered tag selection drawer.
func (m Model) renderTagPicker() string {
	tagItems := m.getTagCounts()
	title := m.styles.FocusedLabel.Bold(true).Render("🏷️  " + i18n.T("tags.title"))

	var rows []string
	rows = append(rows, title, "")

	// Row 0: All Hosts
	allLabel := i18n.T("tags.all")
	totalCount := len(m.allHosts)
	if totalCount == 0 {
		totalCount = len(m.hosts)
	}
	allCount := fmt.Sprintf("(%d)", totalCount)
	cursor0 := "  "
	if m.tagPickerCursor == 0 {
		cursor0 = "> "
	}
	activeMark0 := "○"
	if m.selectedTag == "" {
		activeMark0 = "●"
	}
	line0 := fmt.Sprintf("%s%s [%s]  %s", cursor0, activeMark0, allLabel, allCount)
	if m.tagPickerCursor == 0 {
		line0 = m.styles.Selected.Render(line0)
	}
	rows = append(rows, line0)

	// Rows 1..N: Individual tags
	for i, item := range tagItems {
		idx := i + 1
		cursor := "  "
		if m.tagPickerCursor == idx {
			cursor = "> "
		}
		activeMark := "○"
		if m.selectedTag == item.tag {
			activeMark = "●"
		}
		shortcut := ""
		if idx <= 9 {
			shortcut = fmt.Sprintf("%d. ", idx)
		}
		tagFormatted := FormatColoredTags([]string{item.tag})
		line := fmt.Sprintf("%s%s %s%s (%d)", cursor, activeMark, shortcut, tagFormatted, item.count)
		if m.tagPickerCursor == idx {
			line = m.styles.Selected.Render(line)
		}
		rows = append(rows, line)
	}

	rows = append(rows, "", m.styles.HelpText.Render(i18n.T("tags.help")))
	return renderCardBox(m.styles.FormContainer, m.width, rows...)
}

// renderDeleteConfirmation renders the centered delete confirmation dialog.
func (m Model) renderDeleteConfirmation() string {
	if m.deleteHost == nil {
		return ""
	}
	return renderConfirmBox(m.styles, m.width,
		m.styles.ErrorText.Render(i18n.T("delete.title")),
		i18n.T("delete.confirm", m.deleteHost.Name),
		i18n.T("delete.warning"),
		m.styles.HelpText.Render(i18n.T("delete.help")),
	)
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

// renderProtocolTabs renders a clean top pill-style navigation bar indicating available
// connection protocols (SSH, Serial, Telnet, FTP, Local) with active highlight
// and shortcut key hints.
func renderProtocolTabs(styles Styles, activeTab string, count int, terminalWidth int) string {
	if terminalWidth < 64 {
		return ""
	}

	type tabItem struct {
		id    string
		key   string
		label string
	}

	tabs := []tabItem{
		{id: "ssh", key: "Esc", label: i18n.T("tab.ssh")},
		{id: "serial", key: "t", label: i18n.T("tab.serial")},
		{id: "telnet", key: "T", label: i18n.T("tab.telnet")},
		{id: "ftp", key: "F", label: i18n.T("tab.ftp")},
		{id: "browser", key: "b", label: i18n.T("tab.local")},
	}

	activeStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.Theme.SelectedFg).
		Background(styles.Theme.Primary).
		Padding(0, 1)

	inactiveKeyStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.Theme.Primary)

	inactiveLabelStyle := lipgloss.NewStyle().
		Foreground(styles.Theme.Secondary)

	var renderedTabs []string
	for _, tab := range tabs {
		if tab.id == activeTab {
			label := "● " + tab.label
			if count > 0 {
				label += fmt.Sprintf(" (%d)", count)
			}
			renderedTabs = append(renderedTabs, activeStyle.Render(label))
		} else {
			keyHint := tab.key
			if activeTab == "ssh" && tab.key == "Esc" {
				keyHint = ""
			}
			var item string
			if keyHint != "" {
				item = inactiveKeyStyle.Render(keyHint) + " " + inactiveLabelStyle.Render(tab.label)
			} else {
				item = inactiveLabelStyle.Render(tab.label)
			}
			renderedTabs = append(renderedTabs, " "+item+" ")
		}
	}

	row := "  " + strings.Join(renderedTabs, "  ")
	maxW := terminalWidth - 4
	if maxW < 10 {
		maxW = 10
	}
	return ansi.Truncate(row, maxW, "")
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

// helpItemSep is the canonical separator between key hints in footer help
// strings (the i18n locales are authored around it).
const helpItemSep = " • "

// fitHelpLine packs " • "-separated key hints into at most width display
// columns per line. It mirrors bubbles/help ShortHelpView semantics: hints
// are kept or dropped as whole items — never cut mid-word — and a trailing
// "…" marks dropped help when it fits. Never wrapping: each view's table
// height budget reserves exact help lines, and a taller frame would get
// clipped by RenderCanvas (losing the footer entirely on alt-screen).
func fitHelpLine(text string, width int) string {
	if width < 8 {
		width = 8
	}
	sepW := ansi.StringWidth(helpItemSep)

	var out []string
	for _, line := range strings.Split(text, "\n") {
		items := strings.Split(strings.TrimSpace(line), helpItemSep)
		total, kept := 0, 0
		for _, it := range items {
			gap := 0
			if kept > 0 {
				gap = sepW
			}
			w := ansi.StringWidth(it)
			if total+gap+w > width {
				break
			}
			total += gap + w
			kept++
		}
		if kept == 0 {
			// Not even the first hint fits: hard-truncate rather than blank.
			out = append(out, ansi.Truncate(items[0], width, ""))
			continue
		}
		joined := strings.Join(items[:kept], helpItemSep)
		if kept < len(items) && total+2 <= width {
			joined += " …" // " " + ellipsis, bubbles/help tail semantics
		}
		out = append(out, joined)
	}
	return strings.Join(out, "\n")
}

// renderHelpText renders footer help scaled to the terminal: whole key
// hints are dropped with an ellipsis when width shrinks, instead of the
// old mid-word hard truncation.
func renderHelpText(styles Styles, text string, terminalWidth int) string {
	maxW := terminalWidth - 2 // App padding
	if maxW < 5 {
		maxW = 5
	}
	return styles.HelpText.Render(fitHelpLine(text, maxW))
}

// renderMainHelp styles categorized help lines with highlighted [Tags],
// fitting each category's hints to the terminal width.
func renderMainHelp(styles Styles, helpText string, terminalWidth int) string {
	maxW := terminalWidth - 2
	if maxW < 5 {
		maxW = 5
	}
	var renderedLines []string
	for _, line := range strings.Split(helpText, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.Contains(trimmed, "]") {
			closeIdx := strings.Index(trimmed, "]")
			tag := trimmed[:closeIdx+1]
			prefixW := ansi.StringWidth(tag) + 2 // leading + trailing space
			rest := strings.TrimSpace(trimmed[closeIdx+1:])
			styledTag := styles.FocusedLabel.Bold(true).Render(" " + tag)
			styledRest := styles.HelpText.Render(fitHelpLine(rest, maxW-prefixW))
			renderedLines = append(renderedLines, styledTag+" "+styledRest)
		} else {
			renderedLines = append(renderedLines, styles.HelpText.Render(fitHelpLine(line, maxW)))
		}
	}
	return strings.Join(renderedLines, "\n")
}

// lineDisplayWidth returns the display width of a string, used for debugging.
func lineDisplayWidth(s string) int {
	return ansi.StringWidth(s)
}
