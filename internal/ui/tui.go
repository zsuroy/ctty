package ui

import (
	"fmt"
	"io"
	"os"
	"runtime"
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

// consoleReadWriter joins separate console input/output handles into one
// io.ReadWriter. On Unix /dev/tty is a single bidirectional device; on
// Windows the console is split: DSR (ESC[6n) answers are INPUT events that
// only arrive on CONIN$, while glyphs must be written to CONOUT$. The
// previous Windows path opened CONOUT$ for both directions, so reads never
// returned a cursor report (and term.MakeRaw, which is documented for
// console INPUT handles, was applied to an output handle): calibration
// silently failed on exactly the legacy consoles it was meant to measure,
// leaving glyphDeltaOverride empty and every compensated width wrong.
type consoleReadWriter struct {
	in  io.Reader
	out io.Writer
}

func (c consoleReadWriter) Read(p []byte) (int, error)  { return c.in.Read(p) }
func (c consoleReadWriter) Write(p []byte) (int, error) { return c.out.Write(p) }

// openTTYForCalibration returns the stream to probe plus the file whose
// descriptor term.MakeRaw must be applied to (the input side).
func openTTYForCalibration() (tty io.ReadWriter, rawFile *os.File, cleanup func(), err error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err == nil {
		return f, f, func() { f.Close() }, nil
	}
	if runtime.GOOS == "windows" {
		in, inErr := os.OpenFile("CONIN$", os.O_RDWR, 0)
		if inErr != nil {
			return nil, nil, func() {}, err
		}
		out, outErr := os.OpenFile("CONOUT$", os.O_RDWR, 0)
		if outErr != nil {
			in.Close()
			return nil, nil, func() {}, err
		}
		return consoleReadWriter{in: in, out: out}, in, func() { in.Close(); out.Close() }, nil
	}
	return nil, nil, func() {}, err
}

// calibrateTerminalGlyphs measures the real column advance of the glyphs the
// layout depends on, so the alignment tables are measured facts instead of
// per-terminal guesses. It runs before the Bubble Tea program takes over the
// terminal and silently keeps the static fallback tables when the terminal
// cannot be probed (no tty device, no cursor-position reports).
func calibrateTerminalGlyphs() {
	if !needsWidthCompensation() {
		return
	}
	if isWindowsLegacyConsole() {
		// Legacy Windows consoles (ConHost, mintty) must NOT be probed
		// at startup. The probe writes sample glyphs and ESC[6n queries
		// before the TUI owns the screen and before virtual-terminal
		// output processing is enabled, so ConHost paints the escape
		// sequences as garbage glyphs; worse, its cursor-position
		// replies are console input events that can block in cooked
		// mode until the user presses Enter, and any report that does
		// arrive is measured against that garbage rendering and
		// poisons glyphDeltaOverride for the whole session. The static
		// per-terminal fallback table (windowsLegacyGlyphDelta) covers
		// these consoles deterministically, with zero startup side
		// effects. Live probing stays for the IDE terminals it was
		// built for, where /dev/tty answers DSR properly.
		return
	}
	tty, rawFile, cleanup, err := openTTYForCalibration()
	if err != nil {
		return
	}
	defer cleanup()
	oldState, err := term.MakeRaw(int(rawFile.Fd()))
	if err != nil {
		return
	}
	defer term.Restore(int(rawFile.Fd()), oldState)
	CalibrateGlyphAdvances(tty)
	// The probe wrote to the home position; clear it before the TUI starts.
	_, _ = io.WriteString(tty, "\x1b[2K\x1b[1;1H")
}

// runTUIModel starts the Bubble Tea program for m in the alt screen. The
// Windows legacy console is prepared first (see setupWindowsConsoleOutput):
// Bubble Tea only adds ENABLE_VIRTUAL_TERMINAL_PROCESSING to the output
// mode, and without DISABLE_NEWLINE_AUTO_RETURN ConHost returns the cursor
// as soon as a character lands in the last column instead of deferring the
// wrap, which shifts every full-width frame.
func runTUIModel(m Model, errLabel string) error {
	restore := setupWindowsConsoleOutput()
	defer restore()
	// Start the application in alt screen mode for clean output
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("%s: %w", errLabel, err)
	}
	return nil
}

// RunInteractiveMode starts the interactive TUI interface
func RunInteractiveMode(hosts []config.SSHHost, configFile string, searchMode bool, currentVersion string, noUpdateCheck bool) error {
	calibrateTerminalGlyphs()
	m := NewModel(hosts, configFile, searchMode, currentVersion, noUpdateCheck)

	return runTUIModel(m, "error running TUI")
}

// RunSerialMode starts the TUI directly in the serial device manager view.
func RunSerialMode(currentVersion string, noUpdateCheck bool) error {
	calibrateTerminalGlyphs()
	m := NewModel(nil, "", false, currentVersion, noUpdateCheck)
	m.serialForm = NewSerialForm(m.styles, m.width, m.height)
	m.viewMode = ViewSerial
	m.serialOnly = true

	return runTUIModel(m, "error running TUI")
}

// RunTelnetMode starts the TUI directly in the telnet device manager view.
func RunTelnetMode(currentVersion string, noUpdateCheck bool) error {
	calibrateTerminalGlyphs()
	m := NewModel(nil, "", false, currentVersion, noUpdateCheck)
	m.telnetForm = NewTelnetForm(m.styles, m.width, m.height)
	m.viewMode = ViewTelnet
	m.telnetOnly = true

	return runTUIModel(m, "error running TUI")
}

// RunSFTPMode starts the TUI directly in the SFTP file browser for a host.
func RunSFTPMode(hostName, configFile, currentVersion string, noUpdateCheck bool) error {
	calibrateTerminalGlyphs()
	m := NewModel(nil, configFile, false, currentVersion, noUpdateCheck)
	m.sftpForm = NewSFTPFormWithLayout(m.styles, m.width, m.height, hostName, configFile, m.appConfig.SFTPLayout)
	m.viewMode = ViewSFTP

	return runTUIModel(m, "error running SFTP")
}

// RunFTPMode starts the TUI directly in the FTP site manager view.
func RunFTPMode(currentVersion string, noUpdateCheck bool) error {
	calibrateTerminalGlyphs()
	m := NewModel(nil, "", false, currentVersion, noUpdateCheck)
	m.ftpSitesForm = NewFTPSitesForm(m.styles, m.width, m.height)
	m.viewMode = ViewFTP
	m.ftpOnly = true

	return runTUIModel(m, "error running FTP mode")
}

// RunFTPBrowserMode opens the FTP browser for a saved site.
func RunFTPBrowserMode(siteName, currentVersion string, noUpdateCheck bool) error {
	calibrateTerminalGlyphs()
	m := NewModel(nil, "", false, currentVersion, noUpdateCheck)
	m.ftpForm = NewFTPFormWithLayout(m.styles, m.width, m.height, siteName, m.appConfig.FTPLayout)
	m.viewMode = ViewFTPBrowse
	m.ftpOnly = true

	return runTUIModel(m, "error running FTP browser")
}

// RunWebDAVMode starts the TUI directly in the WebDAV site manager view.
func RunWebDAVMode(currentVersion string, noUpdateCheck bool) error {
	calibrateTerminalGlyphs()
	m := NewModel(nil, "", false, currentVersion, noUpdateCheck)
	m.webdavSitesForm = NewWebDAVSitesForm(m.styles, m.width, m.height)
	m.viewMode = ViewWebDAV
	m.webdavOnly = true

	return runTUIModel(m, "error running WebDAV mode")
}

// RunWebDAVBrowserMode opens the WebDAV browser for a saved site.
func RunWebDAVBrowserMode(siteName, currentVersion string, noUpdateCheck bool) error {
	calibrateTerminalGlyphs()
	m := NewModel(nil, "", false, currentVersion, noUpdateCheck)
	m.webdavForm = NewWebDAVFormWithLayout(m.styles, m.width, m.height, siteName, m.appConfig.WebDAVLayout)
	m.viewMode = ViewWebDAVBrowse
	m.webdavOnly = true

	return runTUIModel(m, "error running WebDAV browser")
}

// RunS3Mode starts the TUI directly in the S3 site manager view.
func RunS3Mode(currentVersion string, noUpdateCheck bool) error {
	calibrateTerminalGlyphs()
	m := NewModel(nil, "", false, currentVersion, noUpdateCheck)
	m.s3SitesForm = NewS3SitesForm(m.styles, m.width, m.height)
	m.viewMode = ViewS3
	m.s3Only = true

	return runTUIModel(m, "error running S3 mode")
}

// RunS3BrowserMode opens the S3 browser for a saved site.
func RunS3BrowserMode(siteName, currentVersion string, noUpdateCheck bool) error {
	calibrateTerminalGlyphs()
	m := NewModel(nil, "", false, currentVersion, noUpdateCheck)
	m.s3Form = NewS3FormWithLayout(m.styles, m.width, m.height, siteName, m.appConfig.S3Layout)
	m.viewMode = ViewS3Browse
	m.s3Only = true

	return runTUIModel(m, "error running S3 browser")
}
