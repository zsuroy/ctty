package ui

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/connectivity"
	"github.com/zsuroy/ctty/internal/history"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/ui/theme"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// NewModel creates a new TUI model with the given SSH hosts
func NewModel(hosts []config.SSHHost, configFile string, searchMode bool, currentVersion string, noUpdateCheck bool) Model {
	// Load application configuration
	appConfig, err := config.LoadAppConfig()
	if err != nil {
		// Log the error but continue with default configuration
		fmt.Printf("Warning: Could not load application config: %v, using defaults\n", err)
		defaultConfig := config.GetDefaultAppConfig()
		appConfig = &defaultConfig
	}

	// Initialize i18n from config if not yet set
	if appConfig != nil && appConfig.Language != "" {
		i18n.Init(appConfig.Language)
	}

	// CLI flag overrides config file setting
	if noUpdateCheck {
		f := false
		appConfig.CheckForUpdates = &f
	}

	// Register custom tag colors from configuration if available
	if appConfig != nil && len(appConfig.TagColors) > 0 {
		SetCustomTagColors(appConfig.TagColors)
	}

	// Initialize the history manager
	historyManager, err := history.NewHistoryManager()
	if err != nil {
		// Log the error but continue without the history functionality
		fmt.Printf("Warning: Could not initialize history manager: %v\n", err)
		historyManager = nil
	}

	// Initialize theme from app config
	th := theme.DefaultTheme
	if appConfig != nil && appConfig.Theme != "" {
		th = theme.GetTheme(appConfig.Theme)
	}
	styles := ApplyTheme(th)

	// Initialize ping manager with 5 second timeout
	pingManager := connectivity.NewPingManager(5*time.Second, configFile)

	// Create the model with default sorting by name
	m := Model{
		allHosts:       hosts,
		historyManager: historyManager,
		pingManager:    pingManager,
		sortMode:       SortByName,
		configFile:     configFile,
		currentVersion: currentVersion,
		appConfig:      appConfig,
		styles:         styles,
		width:          80,
		height:         24,
		ready:          false,
		viewMode:       ViewList,
		searchMode:     searchMode,
	}

	// Apply visibility filter (showHidden is false by default)
	visibleHosts := m.applyVisibilityFilter(hosts)
	m.hosts = visibleHosts

	// Sort hosts according to the default sort mode
	sortedHosts := m.sortHosts(visibleHosts)

	// Create the search input
	ti := textinput.New()
	ti.Placeholder = i18n.T("search.placeholder")
	ti.CharLimit = 50
	ti.Width = searchInputWidth(80, i18n.T("search.prompt"))
	if searchMode {
		ti.Focus()
	}

	// Use dynamic column width calculation (will fallback to static if width not available)
	nameWidth, hostnameWidth, tagsWidth, lastLoginWidth := m.calculateDynamicColumnWidths(sortedHosts)

	// Create table columns
	columns := []table.Column{
		{Title: i18n.T("table.col.name") + " ↓", Width: nameWidth},
		{Title: i18n.T("table.col.hostname"), Width: hostnameWidth},
		// {Title: i18n.T("table.col.user"), Width: 12},                  // Commented to save space
		// {Title: i18n.T("table.col.port"), Width: 6},                   // Commented to save space
		{Title: i18n.T("table.col.tags"), Width: tagsWidth},
		{Title: i18n.T("table.col.last_login"), Width: lastLoginWidth},
	}

	// Convert hosts to table rows
	var rows []table.Row
	for _, host := range sortedHosts {
		// Get ping status indicator
		statusIndicator := m.getPingStatusIndicator(host.Name)

		// Format plain tags for table model
		tagsStr := FormatPlainTags(host.Tags)

		// Format last login information
		var lastLoginStr string
		if historyManager != nil {
			if lastConnect, exists := historyManager.GetLastConnectionTime(host.Name); exists {
				lastLoginStr = formatTimeAgo(lastConnect)
			}
		}

		rows = append(rows, table.Row{
			statusIndicator + " " + host.Name,
			host.Hostname,
			// host.User,        // Commented to save space
			// host.Port,        // Commented to save space
			tagsStr,
			lastLoginStr,
		})
	}

	// Create the table with initial height (will be updated on first WindowSizeMsg)
	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(10), // Initial height, will be recalculated dynamically
	)

	// Style the table
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(SecondaryColor)).
		BorderBottom(true).
		Bold(false)
	s.Selected = m.styles.Selected

	t.SetStyles(s)

	// Update the model with the table and other properties
	m.table = t
	m.searchInput = ti
	m.filteredHosts = sortedHosts
	m.selectedHosts = make(map[string]bool)

	// Initialize table styles based on initial focus state
	m.updateTableStyles()

	// The table height will be properly set on the first WindowSizeMsg
	// when m.ready becomes true and actual terminal dimensions are known

	return m
}

// calibrateTerminalGlyphs measures the real column advance of the glyphs the
// layout depends on, so the alignment tables are measured facts instead of
// per-terminal guesses. It runs before the Bubble Tea program takes over the
// terminal and silently keeps the static fallback tables when the terminal
// cannot be probed (no /dev/tty, no cursor-position reports).
func calibrateTerminalGlyphs() {
	if !needsWidthCompensation() {
		return
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return
	}
	defer tty.Close()
	oldState, err := term.MakeRaw(int(tty.Fd()))
	if err != nil {
		return
	}
	defer term.Restore(int(tty.Fd()), oldState)
	CalibrateGlyphAdvances(tty)
	// The probe wrote to the home position; clear it before the TUI starts.
	_, _ = io.WriteString(tty, "\x1b[2K\x1b[1;1H")
}

// RunInteractiveMode starts the interactive TUI interface
func RunInteractiveMode(hosts []config.SSHHost, configFile string, searchMode bool, currentVersion string, noUpdateCheck bool) error {
	calibrateTerminalGlyphs()
	m := NewModel(hosts, configFile, searchMode, currentVersion, noUpdateCheck)

	// Start the application in alt screen mode for clean output
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	if err != nil {
		return fmt.Errorf("error running TUI: %w", err)
	}

	return nil
}

// RunSerialMode starts the TUI directly in the serial device manager view.
func RunSerialMode(currentVersion string, noUpdateCheck bool) error {
	calibrateTerminalGlyphs()
	m := NewModel(nil, "", false, currentVersion, noUpdateCheck)
	m.serialForm = NewSerialForm(m.styles, m.width, m.height)
	m.viewMode = ViewSerial
	m.serialOnly = true

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	if err != nil {
		return fmt.Errorf("error running TUI: %w", err)
	}
	return nil
}

// RunTelnetMode starts the TUI directly in the telnet device manager view.
func RunTelnetMode(currentVersion string, noUpdateCheck bool) error {
	calibrateTerminalGlyphs()
	m := NewModel(nil, "", false, currentVersion, noUpdateCheck)
	m.telnetForm = NewTelnetForm(m.styles, m.width, m.height)
	m.viewMode = ViewTelnet
	m.telnetOnly = true

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	if err != nil {
		return fmt.Errorf("error running TUI: %w", err)
	}
	return nil
}

// RunSFTPMode starts the TUI directly in the SFTP file browser for a host.
func RunSFTPMode(hostName, configFile, currentVersion string, noUpdateCheck bool) error {
	calibrateTerminalGlyphs()
	m := NewModel(nil, configFile, false, currentVersion, noUpdateCheck)
	m.sftpForm = NewSFTPFormWithLayout(m.styles, m.width, m.height, hostName, configFile, m.appConfig.SFTPLayout)
	m.viewMode = ViewSFTP

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	if err != nil {
		return fmt.Errorf("error running SFTP: %w", err)
	}
	return nil
}

// RunFTPMode starts the TUI directly in the FTP site manager view.
func RunFTPMode(currentVersion string, noUpdateCheck bool) error {
	calibrateTerminalGlyphs()
	m := NewModel(nil, "", false, currentVersion, noUpdateCheck)
	m.ftpSitesForm = NewFTPSitesForm(m.styles, m.width, m.height)
	m.viewMode = ViewFTP
	m.ftpOnly = true

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	if err != nil {
		return fmt.Errorf("error running FTP mode: %w", err)
	}
	return nil
}

// RunFTPBrowserMode opens the FTP browser for a saved site.
func RunFTPBrowserMode(siteName, currentVersion string, noUpdateCheck bool) error {
	calibrateTerminalGlyphs()
	m := NewModel(nil, "", false, currentVersion, noUpdateCheck)
	m.ftpForm = NewFTPFormWithLayout(m.styles, m.width, m.height, siteName, m.appConfig.FTPLayout)
	m.viewMode = ViewFTPBrowse
	m.ftpOnly = true

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	if err != nil {
		return fmt.Errorf("error running FTP browser: %w", err)
	}
	return nil
}

// RunWebDAVMode starts the TUI directly in the WebDAV site manager view.
func RunWebDAVMode(currentVersion string, noUpdateCheck bool) error {
	calibrateTerminalGlyphs()
	m := NewModel(nil, "", false, currentVersion, noUpdateCheck)
	m.webdavSitesForm = NewWebDAVSitesForm(m.styles, m.width, m.height)
	m.viewMode = ViewWebDAV
	m.webdavOnly = true

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	if err != nil {
		return fmt.Errorf("error running WebDAV mode: %w", err)
	}
	return nil
}

// RunWebDAVBrowserMode opens the WebDAV browser for a saved site.
func RunWebDAVBrowserMode(siteName, currentVersion string, noUpdateCheck bool) error {
	calibrateTerminalGlyphs()
	m := NewModel(nil, "", false, currentVersion, noUpdateCheck)
	m.webdavForm = NewWebDAVFormWithLayout(m.styles, m.width, m.height, siteName, m.appConfig.WebDAVLayout)
	m.viewMode = ViewWebDAVBrowse
	m.webdavOnly = true

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	if err != nil {
		return fmt.Errorf("error running WebDAV browser: %w", err)
	}
	return nil
}

// RunS3Mode starts the TUI directly in the S3 site manager view.
func RunS3Mode(currentVersion string, noUpdateCheck bool) error {
	calibrateTerminalGlyphs()
	m := NewModel(nil, "", false, currentVersion, noUpdateCheck)
	m.s3SitesForm = NewS3SitesForm(m.styles, m.width, m.height)
	m.viewMode = ViewS3
	m.s3Only = true

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	if err != nil {
		return fmt.Errorf("error running S3 mode: %w", err)
	}
	return nil
}

// RunS3BrowserMode opens the S3 browser for a saved site.
func RunS3BrowserMode(siteName, currentVersion string, noUpdateCheck bool) error {
	calibrateTerminalGlyphs()
	m := NewModel(nil, "", false, currentVersion, noUpdateCheck)
	m.s3Form = NewS3FormWithLayout(m.styles, m.width, m.height, siteName, m.appConfig.S3Layout)
	m.viewMode = ViewS3Browse
	m.s3Only = true

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	if err != nil {
		return fmt.Errorf("error running S3 browser: %w", err)
	}
	return nil
}
