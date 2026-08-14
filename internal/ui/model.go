package ui

import (
	"time"

	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/connectivity"
	"github.com/zsuroy/ctty/internal/history"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/version"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// SortMode defines the available sorting modes
type SortMode int

const (
	SortByName SortMode = iota
	SortByHostname
	SortByTags
	SortByLastUsed
	NumSortModes
)

func (s SortMode) String() string {
	switch s {
	case SortByName:
		return i18n.T("sort.name")
	case SortByHostname:
		return i18n.T("sort.hostname")
	case SortByTags:
		return i18n.T("sort.tags")
	case SortByLastUsed:
		return i18n.T("sort.last_login")
	default:
		return i18n.T("sort.name")
	}
}

// ViewMode defines the current view state
type ViewMode int

const (
	ViewList ViewMode = iota
	ViewAdd
	ViewEdit
	ViewMove
	ViewInfo
	ViewPortForward
	ViewHelp
	ViewFileSelector
	ViewSerial
	ViewSFTP
	ViewSettings
)

// PortForwardType defines the type of port forwarding
type PortForwardType int

const (
	LocalForward PortForwardType = iota
	RemoteForward
	DynamicForward
)

func (p PortForwardType) String() string {
	switch p {
	case LocalForward:
		return "Local (-L)"
	case RemoteForward:
		return "Remote (-R)"
	case DynamicForward:
		return "Dynamic (-D)"
	default:
		return "Local (-L)"
	}
}

// Model represents the state of the user interface
type Model struct {
	table          table.Model
	searchInput    textinput.Model
	allHosts       []config.SSHHost // all parsed hosts, including hidden ones
	hosts          []config.SSHHost // visible hosts (filtered by showHidden)
	filteredHosts  []config.SSHHost
	showHidden     bool // when true, hidden-tagged hosts are shown
	searchMode     bool
	deleteMode     bool
	deleteHost     *config.SSHHost // Host to be deleted (with line number for precise targeting)
	historyManager *history.HistoryManager
	pingManager    *connectivity.PingManager
	sortMode       SortMode
	configFile     string // Path to the SSH config file

	// Application configuration
	appConfig *config.AppConfig

	// Version update information
	updateInfo     *version.UpdateInfo
	currentVersion string

	// View management
	viewMode         ViewMode
	addForm          *addFormModel
	editForm         *editFormModel
	moveForm         *moveFormModel
	infoForm         *infoFormModel
	portForwardForm  *portForwardModel
	helpForm         *helpModel
	fileSelectorForm *fileSelectorModel
	serialForm       *serialFormModel
	sftpForm         *sftpFormModel
	settingsForm     *settingsFormModel

	// Terminal size and styles
	width  int
	height int
	styles Styles
	ready  bool
	serialOnly bool // true when launched via 'ctty serial' — Esc exits instead of returning to host list

	// Error handling
	errorMessage string
	showingError bool

	// Status notification toast
	statusMessage string
	statusExpiry  time.Time
}

// setStatus sets a temporary status toast notification that expires after 3 seconds.
func (m *Model) setStatus(msg string) {
	m.statusMessage = msg
	m.statusExpiry = time.Now().Add(3 * time.Second)
}

// statusActive returns true if the status notification is currently active.
func (m Model) statusActive() bool {
	return m.statusMessage != "" && time.Now().Before(m.statusExpiry)
}

// applyVisibilityFilter returns hosts filtered according to the showHidden flag.
func (m Model) applyVisibilityFilter(hosts []config.SSHHost) []config.SSHHost {
	if m.showHidden {
		return hosts
	}
	return config.FilterVisibleHosts(hosts)
}

// updateTableStyles updates the table header border color based on focus state
func (m *Model) updateTableStyles() {
	s := table.DefaultStyles()
	s.Selected = m.styles.Selected

	if m.searchMode {
		// When in search mode, use secondary color for table header
		s.Header = s.Header.
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(SecondaryColor)).
			BorderBottom(true).
			Bold(false)
	} else {
		// When table is focused, use primary color for table header
		s.Header = s.Header.
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(PrimaryColor)).
			BorderBottom(true).
			Bold(false)
	}

	m.table.SetStyles(s)
}
