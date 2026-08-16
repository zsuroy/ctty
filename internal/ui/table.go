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

// calculateDynamicColumnWidths calculates optimal column widths based on terminal
// width and content length, ensuring all content fits when possible.
// On narrow terminals (e.g. Termux on phones), it progressively hides optional
// columns (Tags, Last Login) and shrinks minimum widths to fit the screen.
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

	// Calculate available width (minus borders and padding).
	// Table border (2) + App horizontal padding (2) = 4.
	// renderTableView uses JoinHorizontal without internal separators, so no separator cost.
	availableWidth := m.width - 4
	if availableWidth < 12 {
		availableWidth = 12
	}

	totalNeededWidth := maxNameLength + maxHostnameLength + maxTagsLength + maxLastLoginLength

	if totalNeededWidth <= availableWidth {
		// Everything fits perfectly
		return maxNameLength, maxHostnameLength, maxTagsLength, maxLastLoginLength
	}

	// Progressive column hiding on narrow terminals (e.g. Termux on phones).
	// Try 4 columns → 3 columns (hide Tags) → 2 columns (hide Tags + Last Login).

	// Minimum widths when all 4 columns are shown
	minNameWidth4 := 15
	minHostnameWidth4 := 15
	minLastLoginWidth4 := 12
	minTagsWidth4 := 10

	// Minimum widths for 3-column mode (no Tags)
	minNameWidth3 := 16
	minHostnameWidth3 := 16
	minLastLoginWidth3 := 12

	// Minimum widths for 2-column mode (only Name + Hostname)
	minNameWidth2 := 8
	minHostnameWidth2 := 8

	// Try 4 columns first
	if availableWidth >= minNameWidth4+minHostnameWidth4+minTagsWidth4+minLastLoginWidth4 {
		return distributeWidths(
			availableWidth,
			[]int{maxNameLength, maxHostnameLength, maxTagsLength, maxLastLoginLength},
			[]int{minNameWidth4, minHostnameWidth4, minTagsWidth4, minLastLoginWidth4},
		)
	}

	// Try 3 columns: Name + Hostname + Last Login (hide Tags)
	if availableWidth >= minNameWidth3+minHostnameWidth3+minLastLoginWidth3 {
		nameW, hostnameW, _, lastLoginW := distributeWidths(
			availableWidth,
			[]int{maxNameLength, maxHostnameLength, maxLastLoginLength},
			[]int{minNameWidth3, minHostnameWidth3, minLastLoginWidth3},
		)
		// Tags column hidden (width 0)
		return nameW, hostnameW, 0, lastLoginW
	}

	// Fallback: 2 columns only (Name + Hostname), hide Tags and Last Login
	nameW, hostnameW := distributeWidths2(
		availableWidth, maxNameLength, maxHostnameLength,
		minNameWidth2, minHostnameWidth2,
	)
	return nameW, hostnameW, 0, 0
}

// distributeWidths distributes available width across up to 4 columns proportionally,
// respecting minimum widths. Returns 4 values (unused slots get 0).
func distributeWidths(availableWidth int, maxWidths, minWidths []int) (int, int, int, int) {
	n := len(minWidths)
	if n == 0 {
		return 0, 0, 0, 0
	}

	totalMin := 0
	for _, mw := range minWidths {
		totalMin += mw
	}
	remainingWidth := availableWidth - totalMin

	result := make([]int, 4)
	if remainingWidth <= 0 {
		for i := range min(n, 4) {
			result[i] = minWidths[i]
		}
		return result[0], result[1], result[2], result[3]
	}

	// Calculate how much each column wants beyond minimum
	wants := make([]int, n)
	totalWant := 0
	for i := range n {
		w := maxWidths[i] - minWidths[i]
		if w < 0 {
			w = 0
		}
		wants[i] = w
		totalWant += w
	}

	allocated := 0
	for i := range min(n, 4) {
		result[i] = minWidths[i]
		if totalWant > 0 {
			extra := (wants[i] * remainingWidth) / totalWant
			result[i] += extra
			allocated += extra
		}
	}
	// Assign leftover to last active column
	if n > 0 && n <= 4 {
		result[n-1] += remainingWidth - allocated
	}

	return result[0], result[1], result[2], result[3]
}

// distributeWidths2 distributes available width across 2 columns proportionally.
func distributeWidths2(availableWidth, maxName, maxHostname, minName, minHostname int) (int, int) {
	totalMin := minName + minHostname
	remainingWidth := availableWidth - totalMin

	if remainingWidth <= 0 {
		// Not enough even for minimums; split evenly
		half := availableWidth / 2
		if half < 4 {
			half = 4
		}
		return half, availableWidth - half
	}

	nameWant := maxName - minName
	hostnameWant := maxHostname - minHostname
	if nameWant < 0 {
		nameWant = 0
	}
	if hostnameWant < 0 {
		hostnameWant = 0
	}

	totalWant := nameWant + hostnameWant
	if totalWant <= 0 {
		return minName, minHostname
	}

	nameExtra := (nameWant * remainingWidth) / totalWant
	hostnameExtra := remainingWidth - nameExtra

	return minName + nameExtra, minHostname + hostnameExtra
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

	// Ensure we have at least 1 row for the header
	// When there are no hosts, we still need to show the header and an empty message row
	if hostCount == 0 {
		// Show at least 2 rows: 1 header + 1 empty message row
		maxDataRows := 1
		if maxTableHeight-1 >= 1 {
			maxDataRows = 1
		}
		tableHeight := 1 + maxDataRows
		// Update table height
		m.table.SetHeight(tableHeight)
		return
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
