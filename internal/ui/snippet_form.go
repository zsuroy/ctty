package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zsuroy/ctty/internal/i18n"
)

// Snippet represents a saved remote command snippet.
type Snippet struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

// snippetMode controls the sub-state within the snippet view.
type snippetMode int

const (
	snippetBrowse        snippetMode = iota // browsing snippet list / typing command
	snippetAdd                              // adding a new custom snippet
	snippetDeleteConfirm                    // confirming deletion of a custom snippet
)

// snippetFormModel is the view for entering/selecting a remote command to
// execute on the selected host.
type snippetFormModel struct {
	hostName   string
	configFile string
	input      textinput.Model
	styles     Styles
	width      int
	height     int
	cursor     int
	snippets   []Snippet // all snippets (builtin + user)
	userIdx    int       // index in m.snippets where user snippets begin
	mode       snippetMode
	err        string

	// Add mode fields
	addName    textinput.Model
	addCommand textinput.Model
	addFocus   int // 0=name, 1=command
}

// snippetSubmitMsg is sent when the user submits a command.
type snippetSubmitMsg struct {
	hostName string
	command  string
}

// snippetCloseMsg is sent when the user cancels.
type snippetCloseMsg struct{}

// Built-in common snippets shown at the top of the list.
var builtinSnippets = []Snippet{
	{Name: "docker ps", Command: "docker ps"},
	{Name: "df -h", Command: "df -h"},
	{Name: "free -m", Command: "free -m"},
	{Name: "uptime", Command: "uptime"},
	{Name: "top (batch)", Command: "top -bn1 | head -20"},
	{Name: "last logins", Command: "last -10"},
	{Name: "processes", Command: "ps aux --sort=-%mem | head -15"},
	{Name: "disk usage", Command: "du -sh /* 2>/dev/null | sort -rh | head -10"},
}

// snippetsFilePath returns the path to the user's saved snippets file.
func snippetsFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "ctty", "snippets.json")
}

// loadSnippets loads user-saved snippets from disk.
func loadSnippets() []Snippet {
	path := snippetsFilePath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var snippets []Snippet
	if err := json.Unmarshal(data, &snippets); err != nil {
		return nil
	}
	return snippets
}

// saveSnippets persists user snippets to disk.
func saveSnippets(snippets []Snippet) error {
	path := snippetsFilePath()
	if path == "" {
		return fmt.Errorf("cannot determine home directory")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snippets, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// NewSnippetForm creates a new snippet execution form for the given host.
func NewSnippetForm(styles Styles, width, height int, hostName, configFile string) *snippetFormModel {
	ti := textinput.New()
	ti.Placeholder = i18n.T("snippet.placeholder")
	ti.CharLimit = 500
	ti.Width = max(searchMaxWidth(width)-2, 10)
	ti.Focus()

	userSnippets := loadSnippets()
	all := append([]Snippet{}, builtinSnippets...)
	all = append(all, userSnippets...)

	return &snippetFormModel{
		hostName:   hostName,
		configFile: configFile,
		input:      ti,
		styles:     styles,
		width:      width,
		height:     height,
		snippets:   all,
		userIdx:    len(builtinSnippets),
		mode:       snippetBrowse,
	}
}

func (m *snippetFormModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *snippetFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.styles = NewStyles(m.width)
		m.input.Width = max(searchMaxWidth(m.width)-2, 10)
		if m.mode == snippetAdd {
			m.addName.Width = max(searchMaxWidth(m.width)-2, 10)
			m.addCommand.Width = max(searchMaxWidth(m.width)-2, 10)
		}
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case snippetAdd:
			return m.handleAddKeys(msg)
		case snippetDeleteConfirm:
			return m.handleDeleteConfirmKeys(msg)
		default:
			return m.handleBrowseKeys(msg)
		}
	}

	return m, nil
}

func (m *snippetFormModel) handleBrowseKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		return m, func() tea.Msg { return snippetCloseMsg{} }

	case "enter":
		cmd := m.input.Value()
		if cmd == "" && m.cursor < len(m.snippets) {
			cmd = m.snippets[m.cursor].Command
		}
		if cmd == "" {
			m.err = i18n.T("snippet.error_empty")
			return m, nil
		}
		return m, func() tea.Msg {
			return snippetSubmitMsg{hostName: m.hostName, command: cmd}
		}

	case "up", "k":
		m.err = ""
		if m.cursor > 0 {
			m.cursor--
		}

	case "down", "j":
		m.err = ""
		if m.cursor < len(m.snippets)-1 {
			m.cursor++
		}

	case "tab":
		m.err = ""
		if m.cursor < len(m.snippets) {
			m.input.SetValue(m.snippets[m.cursor].Command)
		}

	case "n":
		// Enter add mode
		m.err = ""
		m.mode = snippetAdd
		m.addName = textinput.New()
		m.addName.Placeholder = i18n.T("snippet.add_name_ph")
		m.addName.CharLimit = 50
		m.addName.Width = max(searchMaxWidth(m.width)-2, 10)
		m.addName.Focus()
		m.addCommand = textinput.New()
		m.addCommand.Placeholder = i18n.T("snippet.add_cmd_ph")
		m.addCommand.CharLimit = 500
		m.addCommand.Width = max(searchMaxWidth(m.width)-2, 10)
		m.addFocus = 0
		return m, textinput.Blink

	case "d":
		// Delete selected user snippet (can't delete builtins)
		m.err = ""
		if m.cursor >= m.userIdx && m.cursor < len(m.snippets) {
			m.mode = snippetDeleteConfirm
		}

	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *snippetFormModel) handleAddKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = snippetBrowse
		return m, nil

	case "tab":
		if m.addFocus == 0 {
			m.addFocus = 1
			m.addName.Blur()
			m.addCommand.Focus()
		} else {
			m.addFocus = 0
			m.addCommand.Blur()
			m.addName.Focus()
		}
		return m, textinput.Blink

	case "enter":
		name := strings.TrimSpace(m.addName.Value())
		cmd := strings.TrimSpace(m.addCommand.Value())
		if name == "" || cmd == "" {
			m.err = i18n.T("snippet.add_error_empty")
			return m, nil
		}

		// Save to user snippets
		userSnippets := m.snippets[m.userIdx:]
		userSnippets = append(userSnippets, Snippet{Name: name, Command: cmd})
		if err := saveSnippets(userSnippets); err != nil {
			m.err = err.Error()
			return m, nil
		}

		// Reload
		m.snippets = append([]Snippet{}, builtinSnippets...)
		m.snippets = append(m.snippets, loadSnippets()...)
		m.cursor = len(m.snippets) - 1
		m.mode = snippetBrowse
		m.err = ""
		return m, nil

	default:
		var cmd tea.Cmd
		if m.addFocus == 0 {
			m.addName, cmd = m.addName.Update(msg)
		} else {
			m.addCommand, cmd = m.addCommand.Update(msg)
		}
		return m, cmd
	}
}

func (m *snippetFormModel) handleDeleteConfirmKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "y", "Y":
		// Delete the selected user snippet
		idx := m.cursor - m.userIdx
		userSnippets := m.snippets[m.userIdx:]
		if idx >= 0 && idx < len(userSnippets) {
			userSnippets = append(userSnippets[:idx], userSnippets[idx+1:]...)
			_ = saveSnippets(userSnippets)
		}
		// Reload
		m.snippets = append([]Snippet{}, builtinSnippets...)
		m.snippets = append(m.snippets, loadSnippets()...)
		if m.cursor >= len(m.snippets) {
			m.cursor = len(m.snippets) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.mode = snippetBrowse
		return m, nil

	case "esc", "n", "N":
		m.mode = snippetBrowse
		return m, nil
	}

	return m, nil
}

func (m *snippetFormModel) View() string {
	switch m.mode {
	case snippetAdd:
		return m.viewAdd()
	case snippetDeleteConfirm:
		return m.viewDeleteConfirm()
	default:
		return m.viewBrowse()
	}
}

func (m *snippetFormModel) viewBrowse() string {
	// FormContainer adds border(2)+padding(4)=6 on top of App padding(2)=8 total.
	innerWidth := m.width - 8
	if innerWidth < 10 {
		innerWidth = 10
	}
	// FormTitle has Padding(0,1)=2, truncate to fit
	titleText := ansi.Truncate(i18n.T("snippet.title", m.hostName), innerWidth-2, "…")
	title := m.styles.FormTitle.Render(titleText)
	searchPrompt := i18n.T("snippet.prompt")
	inputLine := renderSearchBar(m.styles, true, searchPrompt, m.input.View(), innerWidth)

	var listLines []string
	listHeader := lipgloss.NewStyle().
		Foreground(lipgloss.Color(SecondaryColor)).
		Render(i18n.T("snippet.list_header"))
	listLines = append(listLines, listHeader)

	// List lines: tw - app(2) - form border(2) - form pad(4) = tw - 8
	listMaxW := m.width - 8
	if listMaxW < 10 {
		listMaxW = 10
	}

	for i, s := range m.snippets {
		marker := "  "
		if i >= m.userIdx {
			marker = "★ "
		}
		line := marker + s.Name + "  " + s.Command
		line = ansi.Truncate(line, listMaxW, "…")

		if i == m.cursor {
			line = m.styles.Selected.Render(line)
		}
		listLines = append(listLines, line)
	}

	helpLine := renderHelpText(m.styles, i18n.T("snippet.help"), innerWidth)

	var errLine string
	if m.err != "" {
		errLine = "\n" + lipgloss.NewStyle().
			Foreground(lipgloss.Color(ErrorColor)).
			Render("✗ "+m.err)
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		inputLine,
		"",
		strings.Join(listLines, "\n"),
		"",
		helpLine,
		errLine,
	)

	return m.styles.App.Render(
		m.styles.FormContainer.Render(content),
	)
}

func (m *snippetFormModel) viewAdd() string {
	innerWidth := m.width - 8
	if innerWidth < 10 {
		innerWidth = 10
	}
	titleText := ansi.Truncate(i18n.T("snippet.add_title"), innerWidth-2, "…")
	title := m.styles.FormTitle.Render(titleText)
	nameLabel := m.styles.Label.Render(i18n.T("snippet.add_name_label"))
	nameInput := renderSearchBar(m.styles, m.addFocus == 0, i18n.T("snippet.add_name_label")+" ", m.addName.View(), innerWidth)
	cmdLabel := m.styles.Label.Render(i18n.T("snippet.add_cmd_label"))
	cmdInput := renderSearchBar(m.styles, m.addFocus == 1, i18n.T("snippet.add_cmd_label")+" ", m.addCommand.View(), innerWidth)

	helpLine := renderHelpText(m.styles, i18n.T("snippet.add_help"), innerWidth)

	var errLine string
	if m.err != "" {
		errLine = "\n" + lipgloss.NewStyle().
			Foreground(lipgloss.Color(ErrorColor)).
			Render("✗ "+m.err)
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		nameLabel,
		nameInput,
		"",
		cmdLabel,
		cmdInput,
		"",
		helpLine,
		errLine,
	)

	return m.styles.App.Render(
		m.styles.FormContainer.Render(content),
	)
}

func (m *snippetFormModel) viewDeleteConfirm() string {
	if m.cursor < 0 || m.cursor >= len(m.snippets) {
		m.mode = snippetBrowse
		return m.viewBrowse()
	}
	s := m.snippets[m.cursor]
	msg := i18n.T("snippet.delete_confirm", s.Name)

	innerWidthDel := m.width - 8
	if innerWidthDel < 10 {
		innerWidthDel = 10
	}
	titleTextDel := ansi.Truncate(i18n.T("snippet.title", m.hostName), innerWidthDel-2, "…")

	content := lipgloss.JoinVertical(lipgloss.Left,
		m.styles.FormTitle.Render(titleTextDel),
		"",
		m.styles.Error.Render(msg),
		"",
		renderHelpText(m.styles, i18n.T("snippet.delete_help"), m.width-6),
	)

	return m.styles.App.Render(
		m.styles.FormContainer.Render(content),
	)
}
