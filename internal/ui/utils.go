package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/zsuroy/ctty/internal/connectivity"
	"github.com/zsuroy/ctty/internal/i18n"
)

// padDisplay right-pads s with spaces to the given terminal display width.
func padDisplay(s string, width int) string {
	w := ansi.StringWidth(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// searchInputWidth calculates a responsive search input width based on the
// terminal width and the search prompt text.
//
// The search bar content is: prompt + textinput.View().
// textinput.View() always renders Width+1 display columns (the +1 is the cursor char).
// The search bar is wrapped in RoundedBorder (2 cols) + Padding(0,1) (2 cols),
// plus the App container's Padding(0,1) (2 cols).
// Total overhead: border(2) + search padding(2) + app padding(2) + cursor(1) = 7
func searchInputWidth(terminalWidth int, prompt string) int {
	promptWidth := ansi.StringWidth(prompt)
	overhead := 7
	w := terminalWidth - promptWidth - overhead
	if w < 5 {
		w = 5
	}
	return w
}

// formatTimeAgo formats a time into a readable "X time ago" string
func formatTimeAgo(t time.Time) string {
	now := time.Now()
	duration := now.Sub(t)

	switch {
	case duration < time.Minute:
		seconds := int(duration.Seconds())
		if seconds <= 1 {
			return i18n.T("time.second_ago", 1)
		}
		return i18n.T("time.seconds_ago", seconds)
	case duration < time.Hour:
		minutes := int(duration.Minutes())
		if minutes == 1 {
			return i18n.T("time.minute_ago", 1)
		}
		return i18n.T("time.minutes_ago", minutes)
	case duration < 24*time.Hour:
		hours := int(duration.Hours())
		if hours == 1 {
			return i18n.T("time.hour_ago", 1)
		}
		return i18n.T("time.hours_ago", hours)
	case duration < 7*24*time.Hour:
		days := int(duration.Hours() / 24)
		if days == 1 {
			return i18n.T("time.day_ago", 1)
		}
		return i18n.T("time.days_ago", days)
	case duration < 30*24*time.Hour:
		weeks := int(duration.Hours() / (24 * 7))
		if weeks == 1 {
			return i18n.T("time.week_ago", 1)
		}
		return i18n.T("time.weeks_ago", weeks)
	case duration < 365*24*time.Hour:
		months := int(duration.Hours() / (24 * 30))
		if months == 1 {
			return i18n.T("time.month_ago", 1)
		}
		return i18n.T("time.months_ago", months)
	default:
		years := int(duration.Hours() / (24 * 365))
		if years == 1 {
			return i18n.T("time.year_ago", 1)
		}
		return i18n.T("time.years_ago", years)
	}
}

// formatConfigFile formats a config file path for display
func formatConfigFile(filePath string) string {
	if filePath == "" {
		return "Unknown"
	}
	// Show just the filename and parent directory for readability
	parts := strings.Split(filePath, "/")
	if len(parts) >= 2 {
		return fmt.Sprintf(".../%s/%s", parts[len(parts)-2], parts[len(parts)-1])
	}
	return filePath
}

// getPingStatusIndicator returns a colored circle indicator based on ping status
func (m *Model) getPingStatusIndicator(hostName string) string {
	if m.pingManager == nil {
		return "⚫" // Gray circle for unknown
	}

	status := m.pingManager.GetStatus(hostName)
	switch status {
	case connectivity.StatusOnline:
		return "🟢" // Green circle for online
	case connectivity.StatusOffline:
		return "🔴" // Red circle for offline
	case connectivity.StatusConnecting:
		return "🟡" // Yellow circle for connecting
	default:
		return "⚫" // Gray circle for unknown
	}
}

// extractHostNameFromTableRow extracts the host name from the first column,
// removing the ping status indicator
func extractHostNameFromTableRow(firstColumn string) string {
	// The first first column format is: "🟢 hostname" or "⚫ hostname" etc.
	// We need to remove the emoji and space to get just the hostname
	parts := strings.Fields(firstColumn)
	if len(parts) >= 2 {
		// Return everything after the first part (the emoji)
		return strings.Join(parts[1:], " ")
	}
	// Fallback: if there's no space, return the whole string
	return firstColumn
}
