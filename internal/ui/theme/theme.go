package theme

import (
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// Theme encapsulates design tokens for the entire UI
type Theme struct {
	ID         string                 // unique ID: "default", "catppuccin", "dracula", "nord", "charm", "ansi"
	Name       string                 // human-readable name
	Primary    lipgloss.TerminalColor // Primary accent color (active border, highlights)
	Secondary  lipgloss.TerminalColor // Secondary color (inactive border, subtitles)
	Error      lipgloss.TerminalColor // Error / Danger color (red)
	Success    lipgloss.TerminalColor // Success color (green)
	Warning    lipgloss.TerminalColor // Warning color (yellow)
	SelectedFg lipgloss.TerminalColor // Selected text foreground
	SelectedBg lipgloss.TerminalColor // Selected text background
	HelpText   lipgloss.TerminalColor // Help & hints
	FormHelp   lipgloss.TerminalColor // Form helper text
}

func (t Theme) HuhTheme() *huh.Theme {
	return makeHuhTheme(t)
}

func makeHuhTheme(t Theme) *huh.Theme {
	theme := huh.ThemeCharm()
	theme.Focused.Title = lipgloss.NewStyle().Bold(true).Foreground(t.Primary)
	theme.Focused.TextInput.Prompt = lipgloss.NewStyle().Bold(true).Foreground(t.Primary)
	theme.Focused.TextInput.Text = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	theme.Focused.SelectSelector = lipgloss.NewStyle().Bold(true).Foreground(t.Primary)
	theme.Focused.FocusedButton = lipgloss.NewStyle().Bold(true).Foreground(t.SelectedFg).Background(t.SelectedBg).Padding(0, 1)
	theme.Focused.BlurredButton = lipgloss.NewStyle().Foreground(t.Secondary).Padding(0, 1)

	// In Blurred state: ensure titles and filled-in text are CRISP and clearly visible (NOT faint!)
	theme.Blurred.Title = lipgloss.NewStyle().Foreground(t.Primary)
	theme.Blurred.TextInput.Prompt = lipgloss.NewStyle().Foreground(t.Secondary)
	theme.Blurred.TextInput.Text = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	theme.Blurred.TextInput.Placeholder = lipgloss.NewStyle().Foreground(t.HelpText)
	theme.Blurred.FocusedButton = theme.Focused.FocusedButton
	theme.Blurred.BlurredButton = theme.Focused.BlurredButton

	theme.Help.ShortKey = lipgloss.NewStyle().Foreground(t.Primary)
	theme.Help.ShortDesc = lipgloss.NewStyle().Foreground(t.HelpText)
	return theme
}

var (
	DefaultTheme = Theme{
		ID:         "default",
		Name:       "Classic Go Blue",
		Primary:    lipgloss.Color("#00ADD8"),
		Secondary:  lipgloss.Color("240"),
		Error:      lipgloss.Color("1"),
		Success:    lipgloss.Color("36"),
		Warning:    lipgloss.Color("11"),
		SelectedFg: lipgloss.Color("229"),
		SelectedBg: lipgloss.Color("#00ADD8"),
		HelpText:   lipgloss.Color("240"),
		FormHelp:   lipgloss.Color("#626262"),
	}

	CatppuccinTheme = Theme{
		ID:         "catppuccin",
		Name:       "Catppuccin Mocha",
		Primary:    lipgloss.Color("#cba6f7"),
		Secondary:  lipgloss.Color("#6c7086"),
		Error:      lipgloss.Color("#f38ba8"),
		Success:    lipgloss.Color("#a6e3a1"),
		Warning:    lipgloss.Color("#f9e2af"),
		SelectedFg: lipgloss.Color("#11111b"),
		SelectedBg: lipgloss.Color("#cba6f7"),
		HelpText:   lipgloss.Color("#9399b2"),
		FormHelp:   lipgloss.Color("#6c7086"),
	}

	DraculaTheme = Theme{
		ID:         "dracula",
		Name:       "Dracula",
		Primary:    lipgloss.Color("#bd93f9"),
		Secondary:  lipgloss.Color("#6272a4"),
		Error:      lipgloss.Color("#ff5555"),
		Success:    lipgloss.Color("#50fa7b"),
		Warning:    lipgloss.Color("#f1fa8c"),
		SelectedFg: lipgloss.Color("#282a36"),
		SelectedBg: lipgloss.Color("#bd93f9"),
		HelpText:   lipgloss.Color("#6272a4"),
		FormHelp:   lipgloss.Color("#6272a4"),
	}

	NordTheme = Theme{
		ID:         "nord",
		Name:       "Nord",
		Primary:    lipgloss.Color("#88c0d0"),
		Secondary:  lipgloss.Color("#4c566a"),
		Error:      lipgloss.Color("#bf616a"),
		Success:    lipgloss.Color("#a3be8c"),
		Warning:    lipgloss.Color("#ebcb8b"),
		SelectedFg: lipgloss.Color("#2e3440"),
		SelectedBg: lipgloss.Color("#88c0d0"),
		HelpText:   lipgloss.Color("#4c566a"),
		FormHelp:   lipgloss.Color("#4c566a"),
	}

	CharmTheme = Theme{
		ID:         "charm",
		Name:       "Charm",
		Primary:    lipgloss.Color("#FF5F87"),
		Secondary:  lipgloss.Color("240"),
		Error:      lipgloss.Color("1"),
		Success:    lipgloss.Color("36"),
		Warning:    lipgloss.Color("11"),
		SelectedFg: lipgloss.Color("#FFFFFF"),
		SelectedBg: lipgloss.Color("#7D56F4"),
		HelpText:   lipgloss.Color("240"),
		FormHelp:   lipgloss.Color("#626262"),
	}

	ANSITheme = Theme{
		ID:         "ansi",
		Name:       "ANSI 16-Color",
		Primary:    lipgloss.Color("6"),
		Secondary:  lipgloss.Color("8"),
		Error:      lipgloss.Color("1"),
		Success:    lipgloss.Color("2"),
		Warning:    lipgloss.Color("3"),
		SelectedFg: lipgloss.Color("0"),
		SelectedBg: lipgloss.Color("6"),
		HelpText:   lipgloss.Color("8"),
		FormHelp:   lipgloss.Color("8"),
	}
)

var allThemes = []Theme{
	DefaultTheme,
	CatppuccinTheme,
	DraculaTheme,
	NordTheme,
	CharmTheme,
	ANSITheme,
}

// AllThemes returns a slice of all registered themes
func AllThemes() []Theme {
	return allThemes
}

// ThemeIDs returns the IDs of all available themes
func ThemeIDs() []string {
	ids := make([]string, len(allThemes))
	for i, t := range allThemes {
		ids[i] = t.ID
	}
	return ids
}

// ThemeNames returns the display names of all available themes
func ThemeNames() []string {
	names := make([]string, len(allThemes))
	for i, t := range allThemes {
		names[i] = t.Name
	}
	return names
}

// GetTheme returns the theme by ID (falls back to DefaultTheme if not found)
func GetTheme(id string) Theme {
	clean := strings.ToLower(strings.TrimSpace(id))
	for _, t := range allThemes {
		if t.ID == clean {
			return t
		}
	}
	return DefaultTheme
}

// ThemeIndex returns the index of the theme by ID
func ThemeIndex(id string) int {
	clean := strings.ToLower(strings.TrimSpace(id))
	for i, t := range allThemes {
		if t.ID == clean {
			return i
		}
	}
	return 0
}
