package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/history"
	"github.com/zsuroy/ctty/internal/i18n"
)

// calculateDynamicColumnWidths calculates optimal column widths based on terminal width
// and content length, ensuring all content fits when possible
func (m *Model) calculateDynamicColumnWidths(hosts []config.SSHHost) (int, int, int, int) {
	if m.width <= 0 {
		// Fallback to static widths if terminal width is not available
		return calculateNameColumnWidth(hosts), 25, calculateTagsColumnWidth(hosts), calculateLastLoginColumnWidth(hosts, m.historyManager)
	}

	// Calculate content lengths
	maxNameLength := ansi.StringWidth(i18n.T("table.col.name")) + 4
	maxHostnameLength := ansi.StringWidth(i18n.T("table.col.hostname")) + 4
	maxTagsLength := ansi.StringWidth(i18n.T("table.col.tags")) + 4
	maxLastLoginLength := ansi.StringWidth(i18n.T("table.col.last_login")) + 4

	for _, host := range hosts {
		// Name column includes status indicator (2 chars) + space (1 char) + name
		nameLength := 3 + len(host.Name)
		if nameLength > maxNameLength {
			maxNameLength = nameLength
		}

		if len(host.Hostname) > maxHostnameLength {
			maxHostnameLength = len(host.Hostname)
		}

		// Calculate tags string length
		tagsLen := CalculatePlainTagsWidth(host.Tags)
		if tagsLen > maxTagsLength {
			maxTagsLength = tagsLen
		}

		// Calculate last login length
		if m.historyManager != nil {
			if lastConnect, exists := m.historyManager.GetLastConnectionTime(host.Name); exists {
				timeStr := formatTimeAgo(lastConnect)
				if len(timeStr) > maxLastLoginLength {
					maxLastLoginLength = len(timeStr)
				}
			}
		}
	}

	// Add padding to each column
	maxNameLength += 2
	maxHostnameLength += 2
	maxTagsLength += 2
	maxLastLoginLength += 2

	// Calculate available width (minus borders, padding, and separators)
	// TableFocused border (2) + internal separators (3) + App horizontal padding (2) + margin (2) = 9
	availableWidth := m.width - 9
	if availableWidth < 30 {
		availableWidth = 30
	}

	totalNeededWidth := maxNameLength + maxHostnameLength + maxTagsLength + maxLastLoginLength

	if totalNeededWidth <= availableWidth {
		// Everything fits perfectly
		return maxNameLength, maxHostnameLength, maxTagsLength, maxLastLoginLength
	}

	// Need to adjust widths - prioritize columns by importance
	// Priority: Name > Hostname > Last Login > Tags

	// Calculate minimum widths
	minNameWidth := 15 // Enough for status + short name
	minHostnameWidth := 15
	minLastLoginWidth := 12
	minTagsWidth := 10

	remainingWidth := availableWidth

	// Allocate minimum widths first
	nameWidth := minNameWidth
	hostnameWidth := minHostnameWidth
	lastLoginWidth := minLastLoginWidth
	tagsWidth := minTagsWidth

	remainingWidth -= (nameWidth + hostnameWidth + lastLoginWidth + tagsWidth)

	// Distribute remaining space proportionally
	if remainingWidth > 0 {
		// Calculate how much each column wants beyond minimum
		nameWant := maxNameLength - minNameWidth
		hostnameWant := maxHostnameLength - minHostnameWidth
		lastLoginWant := maxLastLoginLength - minLastLoginWidth
		tagsWant := maxTagsLength - minTagsWidth

		totalWant := nameWant + hostnameWant + lastLoginWant + tagsWant

		if totalWant > 0 {
			// Distribute proportionally
			nameExtra := (nameWant * remainingWidth) / totalWant
			hostnameExtra := (hostnameWant * remainingWidth) / totalWant
			lastLoginExtra := (lastLoginWant * remainingWidth) / totalWant
			tagsExtra := remainingWidth - nameExtra - hostnameExtra - lastLoginExtra

			nameWidth += nameExtra
			hostnameWidth += hostnameExtra
			lastLoginWidth += lastLoginExtra
			tagsWidth += tagsExtra
		}
	}

	return nameWidth, hostnameWidth, tagsWidth, lastLoginWidth
}

// updateTableRows updates the table with filtered hosts
func (m *Model) updateTableRows() {
	var rows []table.Row
	hostsToShow := m.filteredHosts
	if hostsToShow == nil {
		hostsToShow = m.hosts
	}

	for _, host := range hostsToShow {
		// Get ping status indicator
		statusIndicator := m.getPingStatusIndicator(host.Name)

		// Format plain tags for table model
		tagsStr := FormatPlainTags(host.Tags)

		// Format last login information
		var lastLoginStr string
		if m.historyManager != nil {
			if lastConnect, exists := m.historyManager.GetLastConnectionTime(host.Name); exists {
				lastLoginStr = formatTimeAgo(lastConnect)
			}
		}

		rows = append(rows, table.Row{
			statusIndicator + " " + host.Name,
			host.Hostname,
			tagsStr,
			lastLoginStr,
		})
	}

	m.table.SetRows(rows)

	// Update table height and columns based on current terminal size
	m.updateTableHeight()
	m.updateTableColumns()
}

// updateTableHeight dynamically adjusts table height based on terminal size
func (m *Model) updateTableHeight() {
	if !m.ready {
		return
	}

	// Calculate dynamic table height based on terminal size
	// Layout breakdown:
	// - ASCII title: 5 lines (1 empty + 4 text lines)
	// - Update banner : 1 line (if present)
	// - Search bar: 1 line
	// - Help text: 1 line
	// - App margins/spacing: 3 lines
	// - Safety margin: 3 lines (to ensure UI elements are always visible)
	reservedHeight := 14
	if m.height < 20 {
		reservedHeight = 9 // Compact single-line title saves lines
	}
	availableHeight := m.height - reservedHeight
	hostCount := len(m.table.Rows())

	minTableHeight := 2 // 1 header + 1 data row minimum
	maxTableHeight := availableHeight
	if maxTableHeight < minTableHeight {
		maxTableHeight = minTableHeight
	}

	tableHeight := 1 // header
	dataRowsNeeded := hostCount
	maxDataRows := maxTableHeight - 1 // subtract 1 for header

	if dataRowsNeeded <= maxDataRows {
		// We have enough space for all hosts
		tableHeight += dataRowsNeeded
	} else {
		// We need to limit to available space
		tableHeight += maxDataRows
	}

	// Update table height
	m.table.SetHeight(tableHeight)
}

// updateTableColumns dynamically adjusts table column widths based on terminal size
func (m *Model) updateTableColumns() {
	if !m.ready {
		return
	}

	hostsToShow := m.filteredHosts
	if hostsToShow == nil {
		hostsToShow = m.hosts
	}

	// Use dynamic column width calculation
	nameWidth, hostnameWidth, tagsWidth, lastLoginWidth := m.calculateDynamicColumnWidths(hostsToShow)

	// Create new columns with updated widths and sort indicators
	nameTitle := i18n.T("table.col.name")
	hostnameTitle := i18n.T("table.col.hostname")
	tagsTitle := i18n.T("table.col.tags")
	lastLoginTitle := i18n.T("table.col.last_login")

	// Add sort indicators based on current sort mode
	switch m.sortMode {
	case SortByName:
		nameTitle += " ↓"
	case SortByHostname:
		hostnameTitle += " ↓"
	case SortByTags:
		tagsTitle += " ↓"
	case SortByLastUsed:
		lastLoginTitle += " ↓"
	}

	columns := []table.Column{
		{Title: nameTitle, Width: nameWidth},
		{Title: hostnameTitle, Width: hostnameWidth},
		{Title: tagsTitle, Width: tagsWidth},
		{Title: lastLoginTitle, Width: lastLoginWidth},
	}

	m.table.SetColumns(columns)
}

// renderCell formats a cell value with ANSI-aware truncation and space padding
func renderCell(value string, width int) string {
	if width <= 0 {
		return ""
	}
	truncated := ansi.Truncate(value, width, "…")
	sw := ansi.StringWidth(truncated)
	if sw < width {
		truncated += strings.Repeat(" ", width-sw)
	}
	return truncated
}

// renderTableView renders the main host table with ANSI colored tags and proper viewport scrolling
func (m *Model) renderTableView() string {
	cols := m.table.Columns()
	if len(cols) == 0 {
		return ""
	}

	headerStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(SecondaryColor)).
		BorderBottom(true).
		Bold(false)

	// 1. Render Headers
	var headerCells []string
	for _, col := range cols {
		if col.Width <= 0 {
			continue
		}
		rendered := headerStyle.Render(renderCell(col.Title, col.Width))
		headerCells = append(headerCells, rendered)
	}
	headerRow := lipgloss.JoinHorizontal(lipgloss.Top, headerCells...)

	// 2. Determine visible rows
	hostsToShow := m.filteredHosts
	if hostsToShow == nil {
		hostsToShow = m.hosts
	}

	hostCount := len(hostsToShow)
	cursor := m.table.Cursor()
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= hostCount && hostCount > 0 {
		cursor = hostCount - 1
	}

	// Calculate maximum visible data rows from terminal height
	reservedHeight := 14
	availableHeight := m.height - reservedHeight
	if availableHeight < 4 {
		availableHeight = 4
	}
	maxDataRows := availableHeight - 1 // 1 line for header

	visibleRows := hostCount
	if visibleRows > maxDataRows {
		visibleRows = maxDataRows
	}
	if visibleRows < 1 && hostCount > 0 {
		visibleRows = 1
	}

	start := 0
	if hostCount > visibleRows {
		if cursor < start {
			start = cursor
		}
		if cursor >= start+visibleRows {
			start = cursor - visibleRows + 1
		}
		if start > hostCount-visibleRows {
			start = hostCount - visibleRows
		}
		if start < 0 {
			start = 0
		}
	}

	end := start + visibleRows
	if end > hostCount {
		end = hostCount
	}

	// 3. Render Data Rows
	var renderedRows []string
	for r := start; r < end; r++ {
		host := hostsToShow[r]
		statusIndicator := m.getPingStatusIndicator(host.Name)

		var lastLoginStr string
		if m.historyManager != nil {
			if lastConnect, exists := m.historyManager.GetLastConnectionTime(host.Name); exists {
				lastLoginStr = formatTimeAgo(lastConnect)
			}
		}

		rowValues := []string{
			statusIndicator + " " + host.Name,
			host.Hostname,
			FormatColoredTags(host.Tags),
			lastLoginStr,
		}

		var cells []string
		for i, col := range cols {
			if col.Width <= 0 {
				continue
			}
			val := ""
			if i < len(rowValues) {
				val = rowValues[i]
			}
			cells = append(cells, renderCell(val, col.Width))
		}

		rowContent := lipgloss.JoinHorizontal(lipgloss.Top, cells...)
		if r == cursor {
			rowContent = m.styles.Selected.Render(rowContent)
		}
		renderedRows = append(renderedRows, rowContent)
	}

	if hostCount == 0 {
		emptyMsg := i18n.T("table.no_matching")
		if len(m.hosts) == 0 && m.searchInput.Value() == "" {
			emptyMsg = i18n.T("table.empty_hosts")
		}
		emptyRow := lipgloss.NewStyle().
			Foreground(lipgloss.Color(SecondaryColor)).
			Italic(true).
			Render(emptyMsg)
		renderedRows = append(renderedRows, emptyRow)
	}

	return headerRow + "\n" + strings.Join(renderedRows, "\n")
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Legacy functions for compatibility

// calculateNameColumnWidth calculates the optimal width for the Name column
// based on the longest hostname, with a minimum of 8 and maximum of 40 characters
func calculateNameColumnWidth(hosts []config.SSHHost) int {
	maxLength := 8 // Minimum width to accommodate the "Name" header

	for _, host := range hosts {
		if len(host.Name) > maxLength {
			maxLength = len(host.Name)
		}
	}

	// Add some padding (2 characters) for better visual spacing
	maxLength += 2

	// Limit the maximum width to avoid extremely large columns
	if maxLength > 40 {
		maxLength = 40
	}

	return maxLength
}

// calculateTagsColumnWidth calculates the optimal width for the Tags column
// based on the longest tag string, with a minimum of 8 and maximum of 40 characters
func calculateTagsColumnWidth(hosts []config.SSHHost) int {
	maxLength := 8 // Minimum width to accommodate the "Tags" header

	for _, host := range hosts {
		tagsLen := CalculatePlainTagsWidth(host.Tags)
		if tagsLen > maxLength {
			maxLength = tagsLen
		}
	}

	// Add some padding (2 characters) for better visual spacing
	maxLength += 2

	// Limit the maximum width to avoid extremely large columns
	if maxLength > 40 {
		maxLength = 40
	}

	return maxLength
}

// calculateLastLoginColumnWidth calculates the optimal width for the Last Login column
// based on the longest time format, with a minimum of 12 and maximum of 20 characters
func calculateLastLoginColumnWidth(hosts []config.SSHHost, historyManager *history.HistoryManager) int {
	maxLength := 12 // Minimum width to accommodate the "Last Login" header

	if historyManager != nil {
		for _, host := range hosts {
			if lastConnect, exists := historyManager.GetLastConnectionTime(host.Name); exists {
				timeStr := formatTimeAgo(lastConnect)
				if len(timeStr) > maxLength {
					maxLength = len(timeStr)
				}
			}
		}
	}

	// Add some padding (2 characters) for better visual spacing
	maxLength += 2

	// Limit the maximum width to avoid extremely large columns
	if maxLength > 20 {
		maxLength = 20
	}

	return maxLength
}
