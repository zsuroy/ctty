package ui

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/connectivity"
	"github.com/zsuroy/ctty/internal/credential"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/serialconfig"
	"github.com/zsuroy/ctty/internal/version"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Messages for SSH ping functionality and version checking
type (
	pingResultMsg   *connectivity.HostPingResult
	versionCheckMsg *version.UpdateInfo
	versionErrorMsg error
	errorMsg        string
)

// startPingAllCmd creates a command to ping all hosts concurrently
func (m Model) startPingAllCmd() tea.Cmd {
	if m.pingManager == nil {
		return nil
	}

	return tea.Batch(
		// Create individual ping commands for each host
		func() tea.Cmd {
			var cmds []tea.Cmd
			for _, host := range m.hosts {
				cmds = append(cmds, pingSingleHostCmd(m.pingManager, host))
			}
			if len(cmds) == 0 {
				return nil
			}
			return tea.Batch(cmds...)
		}(),
	)
}

// listenForPingResultsCmd is no longer needed since we use individual ping commands

// pingSingleHostCmd creates a command to ping a single host
func pingSingleHostCmd(pingManager *connectivity.PingManager, host config.SSHHost) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result := pingManager.PingHost(ctx, host)
		return pingResultMsg(result)
	}
}

// checkVersionCmd creates a command to check for version updates
func checkVersionCmd(currentVersion string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		updateInfo, err := version.CheckForUpdates(ctx, currentVersion)
		if err != nil {
			return versionErrorMsg(err)
		}
		return versionCheckMsg(updateInfo)
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd

	// Basic initialization commands
	cmds = append(cmds, textinput.Blink)

	// Check for version updates if we have a current version and updates are enabled
	if m.currentVersion != "" && m.appConfig.IsUpdateCheckEnabled() {
		cmds = append(cmds, checkVersionCmd(m.currentVersion))
	}

	return tea.Batch(cmds...)
}

// Update handles model updates
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	// Handle different message types
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Update terminal size and recalculate styles
		m.width = msg.Width
		m.height = msg.Height
		m.styles = NewStyles(m.width)
		m.ready = true

		// Update table height and columns based on new window size
		m.updateTableHeight()
		m.updateTableColumns()
		m.searchInput.Width = searchInputWidth(m.width, i18n.T("search.prompt"))

		// Update sub-forms if they exist
		if m.addForm != nil {
			m.addForm.width = m.width
			m.addForm.height = m.height
			m.addForm.styles = m.styles
		}
		if m.editForm != nil {
			m.editForm.width = m.width
			m.editForm.height = m.height
			m.editForm.styles = m.styles
		}
		if m.moveForm != nil {
			m.moveForm.width = m.width
			m.moveForm.height = m.height
			m.moveForm.styles = m.styles
		}
		if m.infoForm != nil {
			m.infoForm.width = m.width
			m.infoForm.height = m.height
			m.infoForm.styles = m.styles
		}
		if m.portForwardForm != nil {
			m.portForwardForm.width = m.width
			m.portForwardForm.height = m.height
			m.portForwardForm.styles = m.styles
		}
		if m.helpForm != nil {
			m.helpForm.width = m.width
			m.helpForm.height = m.height
			m.helpForm.styles = m.styles
		}
		if m.fileSelectorForm != nil {
			m.fileSelectorForm.width = m.width
			m.fileSelectorForm.height = m.height
			m.fileSelectorForm.styles = m.styles
		}
		if m.serialForm != nil {
			m.serialForm.Update(msg)
		}
		if m.sftpForm != nil {
			m.sftpForm.Update(msg)
		}
		if m.settingsForm != nil {
			m.settingsForm.Update(msg)
		}
		if m.snippetForm != nil {
			m.snippetForm.Update(msg)
		}
		return m, nil

	case pingResultMsg:
		// Handle ping result - update table display
		if msg != nil {
			// Update the table to reflect the new ping status
			m.updateTableRows()
		}
		return m, nil

	case versionCheckMsg:
		// Handle version check result
		if msg != nil {
			m.updateInfo = msg
		}
		return m, nil

	case versionErrorMsg:
		// Handle version check error (silently - not critical)
		// We don't want to show error messages for version checks
		// as it might disrupt the user experience
		return m, nil

	case errorMsg:
		// Handle general error messages
		if string(msg) == "clear" {
			m.showingError = false
			m.errorMessage = ""
		}
		return m, nil

	case addFormSubmitMsg:
		if msg.err != nil {
			// Show error in form
			if m.addForm != nil {
				m.addForm.err = msg.err.Error()
			}
			return m, nil
		} else {
			// Success: refresh hosts and return to list view
			var hosts []config.SSHHost
			var err error

			if m.configFile != "" {
				hosts, err = config.ParseSSHConfigFile(m.configFile)
			} else {
				hosts, err = config.ParseSSHConfig()
			}

			if err != nil {
				return m, tea.Quit
			}
			m.allHosts = hosts
			m.hosts = m.sortHosts(m.applyVisibilityFilter(hosts))

			// Reapply search filter if there is one active
			if m.searchInput.Value() != "" {
				m.filteredHosts = m.filterHosts(m.searchInput.Value())
			} else {
				m.filteredHosts = m.hosts
			}

			m.updateTableRows()
			m.viewMode = ViewList
			m.addForm = nil
			m.table.Focus()
			return m, nil
		}

	case addFormCancelMsg:
		// Cancel: return to list view
		m.viewMode = ViewList
		m.addForm = nil
		m.table.Focus()
		return m, nil

	case editFormSubmitMsg:
		if msg.err != nil {
			// Show error in form
			if m.editForm != nil {
				m.editForm.err = msg.err.Error()
			}
			return m, nil
		} else {
			// Success: refresh hosts and return to list view
			var hosts []config.SSHHost
			var err error

			if m.configFile != "" {
				hosts, err = config.ParseSSHConfigFile(m.configFile)
			} else {
				hosts, err = config.ParseSSHConfig()
			}

			if err != nil {
				return m, tea.Quit
			}
			m.allHosts = hosts
			m.hosts = m.sortHosts(m.applyVisibilityFilter(hosts))

			// Reapply search filter if there is one active
			if m.searchInput.Value() != "" {
				m.filteredHosts = m.filterHosts(m.searchInput.Value())
			} else {
				m.filteredHosts = m.hosts
			}

			m.updateTableRows()
			m.viewMode = ViewList
			m.editForm = nil
			m.table.Focus()
			return m, nil
		}

	case editFormCancelMsg:
		// Cancel: return to list view
		m.viewMode = ViewList
		m.editForm = nil
		m.table.Focus()
		return m, nil

	case moveFormSubmitMsg:
		if msg.err != nil {
			// En cas d'erreur, on pourrait afficher une notification ou retourner à la liste
			// Pour l'instant, on retourne simplement à la liste
			m.viewMode = ViewList
			m.moveForm = nil
			m.table.Focus()
			return m, nil
		} else {
			// Success: refresh hosts and return to list view
			var hosts []config.SSHHost
			var err error

			if m.configFile != "" {
				hosts, err = config.ParseSSHConfigFile(m.configFile)
			} else {
				hosts, err = config.ParseSSHConfig()
			}

			if err != nil {
				return m, tea.Quit
			}
			m.allHosts = hosts
			m.hosts = m.sortHosts(m.applyVisibilityFilter(hosts))

			// Reapply search filter if there is one active
			if m.searchInput.Value() != "" {
				m.filteredHosts = m.filterHosts(m.searchInput.Value())
			} else {
				m.filteredHosts = m.hosts
			}

			m.updateTableRows()
			m.viewMode = ViewList
			m.moveForm = nil
			m.table.Focus()
			return m, nil
		}

	case moveFormCancelMsg:
		// Cancel: return to list view
		m.viewMode = ViewList
		m.moveForm = nil
		m.table.Focus()
		return m, nil

	case infoFormCancelMsg:
		// Cancel: return to list view
		m.viewMode = ViewList
		m.infoForm = nil
		m.table.Focus()
		return m, nil

	case fileSelectorMsg:
		if msg.cancelled {
			// Cancel: return to list view
			m.viewMode = ViewList
			m.fileSelectorForm = nil
			m.table.Focus()
			return m, nil
		} else {
			// File selected: proceed to add form with selected file
			m.addForm = NewAddForm("", m.styles, m.width, m.height, msg.selectedFile)
			m.viewMode = ViewAdd
			m.fileSelectorForm = nil
			return m, textinput.Blink
		}

	case infoFormEditMsg:
		// Switch from info to edit mode
		editForm, err := NewEditForm(msg.hostName, m.styles, m.width, m.height, m.configFile)
		if err != nil {
			// Handle error - could show in UI, for now just go back to list
			m.viewMode = ViewList
			m.infoForm = nil
			m.table.Focus()
			return m, nil
		}
		m.editForm = editForm
		m.infoForm = nil
		m.viewMode = ViewEdit
		return m, textinput.Blink

	case portForwardSubmitMsg:
		if msg.err != nil {
			// Show error in form
			if m.portForwardForm != nil {
				m.portForwardForm.err = msg.err.Error()
			}
			return m, nil
		} else {
			// Success: execute SSH command with port forwarding
			if len(msg.sshArgs) > 0 {
				// Insert StrictHostKeyChecking at the beginning of sshArgs
				sshArgs := make([]string, 0, len(msg.sshArgs)+2)
				sshArgs = append(sshArgs, "-o", "StrictHostKeyChecking=accept-new")
				sshArgs = append(sshArgs, msg.sshArgs...)
				sshCmd := exec.Command("ssh", sshArgs...)

				// Record the connection in history
				if m.historyManager != nil && m.portForwardForm != nil {
					err := m.historyManager.RecordConnection(m.portForwardForm.hostName)
					if err != nil {
						fmt.Printf("Warning: Could not record connection history: %v\n", err)
					}
				}

				return m, tea.ExecProcess(sshCmd, func(err error) tea.Msg {
					return tea.Quit()
				})
			}

			// If no SSH args, just return to list view
			m.viewMode = ViewList
			m.portForwardForm = nil
			m.table.Focus()
			return m, nil
		}

	case portForwardCancelMsg:
		// Cancel: return to list view
		m.viewMode = ViewList
		m.portForwardForm = nil
		m.table.Focus()
		return m, nil

	case helpCloseMsg:
		// Close help: return to list view
		m.viewMode = ViewList
		m.helpForm = nil
		m.table.Focus()
		return m, nil

	case serialConnectMsg:
		// Suspend TUI via tea.Exec — Bubble Tea releases the terminal
		// to normal mode, calls our ExecCommand.Run() (which opens the
		// serial port and bridges stdin/stdout), then restores the TUI.
		m.serialForm = nil
		m.viewMode = ViewList
		m.table.Focus()
		return m, tea.Exec(serialconfig.NewExecCommand(msg.device), func(err error) tea.Msg {
			return tea.Quit()
		})

	case serialDoneMsg:
		m.serialForm = nil
		if m.serialOnly {
			// Launched via 'ctty serial' — exit entirely
			return m, tea.Quit
		}
		// Return to SSH host list
		m.viewMode = ViewList
		m.table.Focus()
		return m, nil

	case sftpDoneMsg:
		m.sftpForm = nil
		m.viewMode = ViewList
		m.table.Focus()
		return m, nil

	case settingsCloseMsg:
		m.settingsForm = nil
		m.viewMode = ViewList
		m.table.Focus()
		if msg.Saved && msg.AppConfig != nil {
			m.appConfig = msg.AppConfig
			m.searchInput.Placeholder = i18n.T("search.placeholder")
			m.updateTableRows()
			m.setStatus(i18n.T("settings.saved_toast"))
		}
		return m, nil

	case snippetCloseMsg:
		m.snippetForm = nil
		m.viewMode = ViewList
		m.table.Focus()
		return m, nil

	case snippetSubmitMsg:
		m.snippetForm = nil
		m.viewMode = ViewList
		m.table.Focus()
		// Execute the command via ssh and suspend TUI
		var sshCmd *exec.Cmd
		if m.configFile != "" {
			sshCmd = exec.Command("ssh", "-F", m.configFile, "-o", "StrictHostKeyChecking=accept-new", msg.hostName, msg.command)
		} else {
			sshCmd = exec.Command("ssh", "-o", "StrictHostKeyChecking=accept-new", msg.hostName, msg.command)
		}
		sshCmd.Env = buildSSHEnv(msg.hostName)
		return m, tea.ExecProcess(sshCmd, func(err error) tea.Msg {
			return tea.Quit()
		})

	case tea.KeyMsg:
		// Handle view-specific key presses
		switch m.viewMode {
		case ViewAdd:
			if m.addForm != nil {
				var newForm *addFormModel
				newForm, cmd = m.addForm.Update(msg)
				m.addForm = newForm
				return m, cmd
			}
		case ViewEdit:
			if m.editForm != nil {
				var updatedModel tea.Model
				updatedModel, cmd = m.editForm.Update(msg)
				m.editForm = updatedModel.(*editFormModel)
				return m, cmd
			}
		case ViewMove:
			if m.moveForm != nil {
				var newForm *moveFormModel
				newForm, cmd = m.moveForm.Update(msg)
				m.moveForm = newForm
				return m, cmd
			}
		case ViewInfo:
			if m.infoForm != nil {
				var newForm *infoFormModel
				newForm, cmd = m.infoForm.Update(msg)
				m.infoForm = newForm
				return m, cmd
			}
		case ViewPortForward:
			if m.portForwardForm != nil {
				var newForm *portForwardModel
				newForm, cmd = m.portForwardForm.Update(msg)
				m.portForwardForm = newForm
				return m, cmd
			}
		case ViewHelp:
			if m.helpForm != nil {
				var newForm *helpModel
				newForm, cmd = m.helpForm.Update(msg)
				m.helpForm = newForm
				return m, cmd
			}
		case ViewFileSelector:
			if m.fileSelectorForm != nil {
				var newForm *fileSelectorModel
				newForm, cmd = m.fileSelectorForm.Update(msg)
				m.fileSelectorForm = newForm
				return m, cmd
			}
		case ViewSerial:
			if m.serialForm != nil {
				updatedModel, cmd := m.serialForm.Update(msg)
				if sm, ok := updatedModel.(*serialFormModel); ok {
					m.serialForm = sm
				}
				return m, cmd
			}
		case ViewSFTP:
			if m.sftpForm != nil {
				updatedModel, cmd := m.sftpForm.Update(msg)
				if sm, ok := updatedModel.(*sftpFormModel); ok {
					m.sftpForm = sm
				}
				return m, cmd
			}
		case ViewSettings:
			if m.settingsForm != nil {
				var newForm *settingsFormModel
				newForm, cmd = m.settingsForm.Update(msg)
				m.settingsForm = newForm
				return m, cmd
			}
		case ViewSnippet:
			if m.snippetForm != nil {
				updatedModel, cmd := m.snippetForm.Update(msg)
				if sm, ok := updatedModel.(*snippetFormModel); ok {
					m.snippetForm = sm
				}
				return m, cmd
			}
		case ViewList:
			// Handle list view keys
			return m.handleListViewKeys(msg)
		}
	}

	// Forward SFTP-specific messages to the sftp form (these are not tea.KeyMsg)
	if m.sftpForm != nil && m.viewMode == ViewSFTP {
		updatedModel, sftpCmd := m.sftpForm.Update(msg)
		if sm, ok := updatedModel.(*sftpFormModel); ok {
			m.sftpForm = sm
		}
		return m, sftpCmd
	}

	return m, cmd
}

func (m Model) handleListViewKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	key := msg.String()

	switch key {
	case "esc", "ctrl+c":
		if m.deleteMode {
			// Exit delete mode
			m.deleteMode = false
			m.deleteHost = nil
			m.table.Focus()
			return m, nil
		}
		if m.searchMode {
			// Exit search mode back to table
			m.searchMode = false
			m.updateTableStyles()
			m.searchInput.Blur()
			m.table.Focus()
			return m, nil
		}
		// Use configurable key bindings for quit
		if m.appConfig != nil && m.appConfig.KeyBindings.ShouldQuitOnKey(key) {
			return m, tea.Quit
		}
	case "q":
		if !m.searchMode && !m.deleteMode {
			// Use configurable key bindings for quit
			if m.appConfig != nil && m.appConfig.KeyBindings.ShouldQuitOnKey(key) {
				return m, tea.Quit
			}
		}
	case "/", "ctrl+f":
		if !m.searchMode && !m.deleteMode {
			// Enter search mode
			m.searchMode = true
			m.updateTableStyles()
			m.table.Blur()
			m.searchInput.Focus()
			// Don't trigger filtering when entering search mode - wait for user input
			return m, textinput.Blink
		}
	case "tab":
		if !m.deleteMode {
			// Switch focus between search input and table
			if m.searchMode {
				// Switch from search to table
				m.searchMode = false
				m.updateTableStyles()
				m.searchInput.Blur()
				m.table.Focus()
			} else {
				// Switch from table to search
				m.searchMode = true
				m.updateTableStyles()
				m.table.Blur()
				m.searchInput.Focus()
				// Don't trigger filtering when switching to search mode
				return m, textinput.Blink
			}
			return m, nil
		}
	case "enter":
		if m.searchMode {
			// Validate search and return to table mode to allow commands
			m.searchMode = false
			m.updateTableStyles()
			m.searchInput.Blur()
			m.table.Focus()
			return m, nil
		} else if m.deleteMode {
			// Confirm deletion
			var err error
			if m.deleteHost != nil {
				_ = credential.DeletePassword(m.deleteHost.Name)
				err = config.DeleteSSHHostWithLine(*m.deleteHost)
			}
			if err != nil {
				// Could display an error message here
				m.deleteMode = false
				m.deleteHost = nil
				m.table.Focus()
				return m, nil
			}
			// Refresh the hosts list
			var hosts []config.SSHHost
			var parseErr error

			if m.configFile != "" {
				hosts, parseErr = config.ParseSSHConfigFile(m.configFile)
			} else {
				hosts, parseErr = config.ParseSSHConfig()
			}

			if parseErr != nil {
				// Could display an error message here
				m.deleteMode = false
				m.deleteHost = nil
				m.table.Focus()
				return m, nil
			}
			m.allHosts = hosts
			m.hosts = m.sortHosts(m.applyVisibilityFilter(hosts))

			// Reapply search filter if there is one active
			if m.searchInput.Value() != "" {
				m.filteredHosts = m.filterHosts(m.searchInput.Value())
			} else {
				m.filteredHosts = m.hosts
			}

			m.updateTableRows()
			m.deleteMode = false
			m.deleteHost = nil
			m.table.Focus()
			return m, nil
		} else {
			// Connect to the selected host
			selected := m.table.SelectedRow()
			if len(selected) > 0 {
				hostName := extractHostNameFromTableRow(selected[0]) // Extract hostname from first column

				// Record the connection in history
				if m.historyManager != nil {
					err := m.historyManager.RecordConnection(hostName)
					if err != nil {
						// Log the error but don't prevent the connection
						fmt.Printf("Warning: Could not record connection history: %v\n", err)
					}
				}

				// Build the SSH command with the appropriate config file
				var sshCmd *exec.Cmd
				if m.configFile != "" {
					sshCmd = exec.Command("ssh", "-F", m.configFile, "-o", "StrictHostKeyChecking=accept-new", hostName)
				} else {
					sshCmd = exec.Command("ssh", "-o", "StrictHostKeyChecking=accept-new", hostName)
				}

				// Set up SSH_ASKPASS for zero-touch auto-login with saved passwords
				sshCmd.Env = buildSSHEnv(hostName)

				return m, tea.ExecProcess(sshCmd, func(err error) tea.Msg {
					return tea.Quit()
				})
			}
		}
	case "e":
		if !m.searchMode && !m.deleteMode {
			// Edit the selected host
			selected := m.table.SelectedRow()
			if len(selected) > 0 {
				hostName := extractHostNameFromTableRow(selected[0]) // Extract hostname from first column
				editForm, err := NewEditForm(hostName, m.styles, m.width, m.height, m.configFile)
				if err != nil {
					// Handle error - could show in UI
					return m, nil
				}
				m.editForm = editForm
				m.viewMode = ViewEdit
				return m, textinput.Blink
			}
		}
	case "m":
		if !m.searchMode && !m.deleteMode {
			// Move the selected host to another config file
			selected := m.table.SelectedRow()
			if len(selected) > 0 {
				hostName := extractHostNameFromTableRow(selected[0]) // Extract hostname from first column
				moveForm, err := NewMoveForm(hostName, m.styles, m.width, m.height, m.configFile)
				if err != nil {
					// Show error message to user
					m.errorMessage = err.Error()
					m.showingError = true
					return m, func() tea.Msg {
						time.Sleep(3 * time.Second) // Show error for 3 seconds
						return errorMsg("clear")
					}
				}
				m.moveForm = moveForm
				m.viewMode = ViewMove
				return m, textinput.Blink
			}
		}
	case "i":
		if !m.searchMode && !m.deleteMode {
			// Show info for the selected host
			selected := m.table.SelectedRow()
			if len(selected) > 0 {
				hostName := extractHostNameFromTableRow(selected[0]) // Extract hostname from first column
				infoForm, err := NewInfoForm(hostName, m.styles, m.width, m.height, m.configFile)
				if err != nil {
					// Handle error - could show in UI
					return m, nil
				}
				m.infoForm = infoForm
				m.viewMode = ViewInfo
				return m, nil
			}
		}
	case "a":
		if !m.searchMode && !m.deleteMode {
			// Check if there are multiple config files starting from the current base config
			var configFiles []string
			var err error

			if m.configFile != "" {
				// Use the specified config file as base
				configFiles, err = config.GetAllConfigFilesFromBase(m.configFile)
			} else {
				// Use the default config file as base
				configFiles, err = config.GetAllConfigFiles()
			}

			if err != nil || len(configFiles) <= 1 {
				// Only one config file (or error), go directly to add form
				var configFile string
				if len(configFiles) == 1 {
					configFile = configFiles[0]
				} else {
					configFile = m.configFile
				}
				m.addForm = NewAddForm("", m.styles, m.width, m.height, configFile)
				m.viewMode = ViewAdd
			} else {
				// Multiple config files, show file selector
				fileSelectorForm, err := NewFileSelectorFromBase("Select config file to add host to:", m.styles, m.width, m.height, m.configFile)
				if err != nil {
					// Fallback to default behavior if file selector fails
					m.addForm = NewAddForm("", m.styles, m.width, m.height, m.configFile)
					m.viewMode = ViewAdd
				} else {
					m.fileSelectorForm = fileSelectorForm
					m.viewMode = ViewFileSelector
				}
			}
			return m, textinput.Blink
		}
	case "d":
		if !m.searchMode && !m.deleteMode {
			// Delete the selected host
			cursor := m.table.Cursor()
			if cursor >= 0 && cursor < len(m.filteredHosts) {
				// Get the host at the cursor position (which corresponds to filteredHosts index)
				targetHost := &m.filteredHosts[cursor]

				m.deleteMode = true
				m.deleteHost = targetHost
				m.table.Blur()
				return m, nil
			}
		}
	case "p":
		if !m.searchMode && !m.deleteMode {
			// Ping all hosts
			return m, m.startPingAllCmd()
		}
	case "f":
		if !m.searchMode && !m.deleteMode {
			// Port forwarding for the selected host
			selected := m.table.SelectedRow()
			if len(selected) > 0 {
				hostName := extractHostNameFromTableRow(selected[0]) // Extract hostname from first column
				m.portForwardForm = NewPortForwardForm(hostName, m.styles, m.width, m.height, m.configFile, m.historyManager)
				m.viewMode = ViewPortForward
				return m, textinput.Blink
			}
		}
	case "t":
		if !m.searchMode && !m.deleteMode {
			// Open serial device manager
			m.serialForm = NewSerialForm(m.styles, m.width, m.height)
			m.viewMode = ViewSerial
			return m, nil
		}
	case "o":
		if !m.searchMode && !m.deleteMode {
			// Open SFTP file browser for the selected host
			selected := m.table.SelectedRow()
			if len(selected) > 0 {
				hostName := extractHostNameFromTableRow(selected[0])
				m.sftpForm = NewSFTPForm(m.styles, m.width, m.height, hostName, m.configFile)
				m.viewMode = ViewSFTP
				return m, m.sftpForm.Init()
			}
		}
	case "h":
		if !m.searchMode && !m.deleteMode {
			// Show help
			m.helpForm = NewHelpForm(m.styles, m.width, m.height, m.currentVersion)
			m.viewMode = ViewHelp
			return m, nil
		}
	case "H":
		if !m.searchMode && !m.deleteMode {
			// Toggle visibility of hidden hosts
			m.showHidden = !m.showHidden
			m.hosts = m.sortHosts(m.applyVisibilityFilter(m.allHosts))
			if m.searchInput.Value() != "" {
				m.filteredHosts = m.filterHosts(m.searchInput.Value())
			} else {
				m.filteredHosts = m.hosts
			}
			m.updateTableRows()
			return m, nil
		}
	case "s":
		if !m.searchMode && !m.deleteMode {
			// Cycle through all sort modes (Name -> Hostname -> Tags -> Last Login)
			m.sortMode = (m.sortMode + 1) % NumSortModes
			// Re-apply the current filter with the new sort mode
			if m.searchInput.Value() != "" {
				m.filteredHosts = m.filterHosts(m.searchInput.Value())
			} else {
				m.filteredHosts = m.sortHosts(m.hosts)
			}
			m.updateTableRows()
			return m, nil
		}
	case "r":
		if !m.searchMode && !m.deleteMode {
			// Switch to sort by recent (last used)
			m.sortMode = SortByLastUsed
			// Re-apply the current filter with the new sort mode
			if m.searchInput.Value() != "" {
				m.filteredHosts = m.filterHosts(m.searchInput.Value())
			} else {
				m.filteredHosts = m.sortHosts(m.hosts)
			}
			m.updateTableRows()
			return m, nil
		}
	case "n":
		if !m.searchMode && !m.deleteMode {
			// Switch to sort by name
			m.sortMode = SortByName
			// Re-apply the current filter with the new sort mode
			if m.searchInput.Value() != "" {
				m.filteredHosts = m.filterHosts(m.searchInput.Value())
			} else {
				m.filteredHosts = m.sortHosts(m.hosts)
			}
			m.updateTableRows()
			return m, nil
		}
	case "S":
		if !m.searchMode && !m.deleteMode {
			m.settingsForm = NewSettingsForm(m.styles, m.width, m.height, m.appConfig)
			m.viewMode = ViewSettings
			return m, nil
		}
	case "x":
		if !m.searchMode && !m.deleteMode {
			// Execute a remote command on the selected host
			selected := m.table.SelectedRow()
			if len(selected) > 0 {
				hostName := extractHostNameFromTableRow(selected[0])
				m.snippetForm = NewSnippetForm(m.styles, m.width, m.height, hostName, m.configFile)
				m.viewMode = ViewSnippet
				return m, textinput.Blink
			}
		}
	}

	// Update the appropriate component based on mode
	if m.searchMode {
		oldValue := m.searchInput.Value()
		m.searchInput, cmd = m.searchInput.Update(msg)
		// Update filtered hosts only if the search value has changed
		if m.searchInput.Value() != oldValue {
			currentCursor := m.table.Cursor()
			if m.searchInput.Value() != "" {
				m.filteredHosts = m.filterHosts(m.searchInput.Value())
			} else {
				m.filteredHosts = m.sortHosts(m.hosts)
			}
			m.updateTableRows()
			// If the current cursor position is beyond the filtered results, reset to 0
			if currentCursor >= len(m.filteredHosts) && len(m.filteredHosts) > 0 {
				m.table.SetCursor(0)
			}
		}
	} else {
		// Only update table if there are hosts to display
		// This prevents panic when navigating with arrow keys on empty host list
		hostsToShow := m.filteredHosts
		if hostsToShow == nil {
			hostsToShow = m.hosts
		}
		if len(hostsToShow) > 0 {
			m.table, cmd = m.table.Update(msg)
		} else {
			// When host list is empty, ignore navigation keys to prevent panic
			// but still allow other key bindings to work
			cmd = nil
		}
	}

	return m, cmd
}

// buildSSHEnv constructs the environment for the SSH command, injecting
// SSH_ASKPASS variables when a saved password exists for the host.
// This mirrors the CLI logic in cmd/root.go connectToHost.
func buildSSHEnv(hostName string) []string {
	env := os.Environ()
	if pass, ok := credential.GetPassword(hostName); ok && pass != "" {
		selfPath, err := os.Executable()
		if err == nil {
			env = append(env,
				"SSH_ASKPASS="+selfPath,
				"SSH_ASKPASS_REQUIRE=force",
				"CTTY_ASKPASS_TOKEN="+base64.StdEncoding.EncodeToString([]byte(pass)),
				"DISPLAY=ctty:0",
			)
		}
	}
	return env
}
