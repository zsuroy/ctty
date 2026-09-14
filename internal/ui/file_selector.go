package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/i18n"
)

type fileSelectorModel struct {
	files        []string // Chemins absolus des fichiers
	displayNames []string // Noms d'affichage conviviaux
	selected     int
	styles       Styles
	width        int
	height       int
	title        string
}

type fileSelectorMsg struct {
	selectedFile string
	cancelled    bool
}

// NewFileSelector creates a new file selector with all available config files
func NewFileSelector(title string, styles Styles, width, height int) (*fileSelectorModel, error) {
	return NewFileSelectorFromBase(title, styles, width, height, "")
}

// NewFileSelectorFromBase creates a new file selector using the specified base config file
func NewFileSelectorFromBase(title string, styles Styles, width, height int, baseConfigFile string) (*fileSelectorModel, error) {
	var files []string
	var err error

	if baseConfigFile != "" {
		files, err = config.GetAllConfigFilesFromBase(baseConfigFile)
	} else {
		files, err = config.GetAllConfigFiles()
	}

	if err != nil {
		return nil, fmt.Errorf("error finding config files: %w", err)
	}

	return newFileSelectorFromFiles(title, styles, width, height, files)
}

// newFileSelectorFromFiles creates a file selector with a pre-filtered list of files
func newFileSelectorFromFiles(title string, styles Styles, width, height int, files []string) (*fileSelectorModel, error) {
	// Create user-friendly display names
	var displayNames []string
	homeDir, _ := config.GetSSHDirectory()
	for _, file := range files {
		mainConfig, _ := config.GetDefaultSSHConfigPath()
		if file == mainConfig {
			displayNames = append(displayNames, "Main SSH Config (~/.ssh/config)")
		} else {
			if strings.HasPrefix(file, homeDir) {
				relPath, err := filepath.Rel(homeDir, file)
				if err == nil {
					displayNames = append(displayNames, fmt.Sprintf("~/.ssh/%s", relPath))
				} else {
					displayNames = append(displayNames, file)
				}
			} else {
				displayNames = append(displayNames, file)
			}
		}
	}

	return &fileSelectorModel{
		files:        files,
		displayNames: displayNames,
		selected:     0,
		styles:       styles,
		width:        width,
		height:       height,
		title:        title,
	}, nil
}

func (m *fileSelectorModel) Init() tea.Cmd {
	return nil
}

func (m *fileSelectorModel) Update(msg tea.Msg) (*fileSelectorModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.styles = NewStyles(m.width)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			return m, func() tea.Msg {
				return fileSelectorMsg{cancelled: true}
			}

		case "enter":
			selectedFile := ""
			if m.selected < len(m.files) {
				selectedFile = m.files[m.selected]
			}
			return m, func() tea.Msg {
				return fileSelectorMsg{selectedFile: selectedFile}
			}

		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}

		case "down", "j":
			if m.selected < len(m.files)-1 {
				m.selected++
			}
		}
	}

	return m, nil
}

func (m *fileSelectorModel) View() string {
	boxWidth := m.width - 4
	if boxWidth < 20 {
		boxWidth = 20
	}

	container := m.styles.FormContainer
	if m.height < 24 {
		container = container.Padding(0, 1)
	}

	innerW := boxWidth - container.GetHorizontalFrameSize()
	if innerW < 10 {
		innerW = 10
	}

	titleText := m.styles.Header.Width(innerW).Render(m.title)
	helpText := m.styles.HelpText.Width(innerW).Render(i18n.T("file_selector.help"))

	if len(m.files) == 0 {
		errMsg := m.styles.ErrorText.Width(innerW).Render(i18n.T("file_selector.empty"))
		content := lipgloss.JoinVertical(lipgloss.Left, titleText, "", errMsg, "", helpText)
		box := container.Width(boxWidth).Render(content)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top, box)
	}

	frameH := container.GetVerticalFrameSize()
	titleH := lipgloss.Height(titleText)
	helpH := lipgloss.Height(helpText)

	targetBoxH := m.height
	if m.height >= 14 {
		targetBoxH = m.height - 1
	}

	overhead := frameH + titleH + helpH + 2
	availableH := targetBoxH - overhead
	if availableH < 2 {
		availableH = 2
	}

	maxVisible := availableH
	start := 0
	if m.selected >= maxVisible {
		start = m.selected - maxVisible + 1
	}
	end := start + maxVisible
	if end > len(m.displayNames) {
		end = len(m.displayNames)
	}

	var items []string
	for i := start; i < end; i++ {
		displayName := m.displayNames[i]
		if i == m.selected {
			items = append(items, m.styles.Selected.Width(innerW).Render(fmt.Sprintf(" ▶ %s", displayName)))
		} else {
			items = append(items, fmt.Sprintf("   %s", displayName))
		}
	}

	listContent := strings.Join(items, "\n")
	content := lipgloss.JoinVertical(lipgloss.Left, titleText, "", listContent, "", helpText)
	box := container.Width(boxWidth).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top, box)
}
