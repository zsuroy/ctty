package ui

import (
	"fmt"
	"time"

	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/connectivity"
	"github.com/zsuroy/ctty/internal/history"
	"github.com/zsuroy/ctty/internal/i18n"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

	// Create initial styles (will be updated on first WindowSizeMsg)
	styles := NewStyles(80) // Default width

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

	// Initialize table styles based on initial focus state
	m.updateTableStyles()

	// The table height will be properly set on the first WindowSizeMsg
	// when m.ready becomes true and actual terminal dimensions are known

	return m
}

// RunInteractiveMode starts the interactive TUI interface
func RunInteractiveMode(hosts []config.SSHHost, configFile string, searchMode bool, currentVersion string, noUpdateCheck bool) error {
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

// RunSFTPMode starts the TUI directly in the SFTP file browser for a host.
func RunSFTPMode(hostName, configFile, currentVersion string, noUpdateCheck bool) error {
	m := NewModel(nil, configFile, false, currentVersion, noUpdateCheck)
	m.sftpForm = NewSFTPForm(m.styles, m.width, m.height, hostName, configFile)
	m.viewMode = ViewSFTP

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	if err != nil {
		return fmt.Errorf("error running SFTP: %w", err)
	}
	return nil
}
