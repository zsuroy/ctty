package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/zsuroy/ctty/internal/ui/theme"
)

// Active theme state
var currentTheme = theme.DefaultTheme

// Theme colors (maintained for backward compatibility with external/legacy callers)
var (
	// Primary interface color - easily modifiable
	PrimaryColor = "#00ADD8" // Official Go logo blue color

	// Secondary colors
	SecondaryColor = "240" // Gray
	ErrorColor     = "1"   // Red
	SuccessColor   = "36"  // Green (for reference if needed)
)

// CurrentTheme returns the currently active theme
func CurrentTheme() theme.Theme {
	return currentTheme
}

// ApplyTheme sets the active theme and updates legacy color variables
func ApplyTheme(t theme.Theme) Styles {
	currentTheme = t
	if c, ok := t.Primary.(lipgloss.Color); ok {
		PrimaryColor = string(c)
	}
	if c, ok := t.Secondary.(lipgloss.Color); ok {
		SecondaryColor = string(c)
	}
	if c, ok := t.Error.(lipgloss.Color); ok {
		ErrorColor = string(c)
	}
	if c, ok := t.Success.(lipgloss.Color); ok {
		SuccessColor = string(c)
	}
	return NewStylesWithTheme(t)
}

// Styles struct centralizes all lipgloss styles
type Styles struct {
	Theme theme.Theme

	// Layout
	App    lipgloss.Style
	Header lipgloss.Style

	// Search styles
	SearchFocused   lipgloss.Style
	SearchUnfocused lipgloss.Style

	// Table styles
	TableFocused   lipgloss.Style
	TableUnfocused lipgloss.Style
	Selected       lipgloss.Style

	// Info and help styles
	SortInfo lipgloss.Style
	HelpText lipgloss.Style

	// Error and confirmation styles
	Error     lipgloss.Style
	ErrorText lipgloss.Style

	// Form styles (for add/edit forms)
	FormTitle     lipgloss.Style
	FormField     lipgloss.Style
	FormHelp      lipgloss.Style
	FormContainer lipgloss.Style
	Label         lipgloss.Style
	FocusedLabel  lipgloss.Style
	HelpSection   lipgloss.Style
}

// NewStyles creates a new Styles struct using the active theme
func NewStyles(width int) Styles {
	return NewStylesWithTheme(currentTheme)
}

// NewStylesWithTheme creates a new Styles struct using the specified theme
func NewStylesWithTheme(t theme.Theme) Styles {
	return Styles{
		Theme: t,

		// Main app container
		App: lipgloss.NewStyle().
			Padding(0, 1),

		// Header style
		Header: lipgloss.NewStyle().
			Foreground(t.Primary).
			Bold(true).
			Align(lipgloss.Center),

		// Search styles
		SearchFocused: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(t.Primary).
			Padding(0, 1),

		SearchUnfocused: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(t.Secondary).
			Padding(0, 1),

		// Table styles
		TableFocused: lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(t.Primary),

		TableUnfocused: lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(t.Secondary),

		// Style for selected items
		Selected: lipgloss.NewStyle().
			Foreground(t.SelectedFg).
			Background(t.SelectedBg).
			Bold(false),

		// Info styles
		SortInfo: lipgloss.NewStyle().
			Foreground(t.Secondary),

		HelpText: lipgloss.NewStyle().
			Foreground(t.HelpText),

		// Error style
		Error: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(t.Error).
			Padding(1, 2),

		// Error text style (no border, just red text)
		ErrorText: lipgloss.NewStyle().
			Foreground(t.Error).
			Bold(true),

		// Form styles
		FormTitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFDF5")).
			Background(t.Primary).
			Padding(0, 1),

		FormField: lipgloss.NewStyle().
			Foreground(t.Primary),

		FormHelp: lipgloss.NewStyle().
			Foreground(t.FormHelp),

		FormContainer: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(t.Primary).
			Padding(1, 2),

		Label: lipgloss.NewStyle().
			Foreground(t.Secondary),

		FocusedLabel: lipgloss.NewStyle().
			Foreground(t.Primary),

		HelpSection: lipgloss.NewStyle().
			Padding(0, 2),
	}
}

// formPageInnerWidth is the content width inside FormContainer.
// App padding (2) + FormContainer border (2) + padding (4) = 8.
func formPageInnerWidth(termWidth int) int {
	w := termWidth - 8
	if w < 10 {
		return 10
	}
	return w
}

func renderFormPage(styles Styles, termWidth int, body string) string {
	// FormContainer.Width is the padded block before borders.
	// App pad(2) + border(2) = 4, so the rounded box fills the terminal.
	box := termWidth - 4
	if box < 12 {
		box = 12
	}
	return styles.App.Render(styles.FormContainer.Width(box).Render(body))
}

// Application ASCII title
const asciiTitle = "    _   _        \n" +
	" __| |_| |_ _  _ \n" +
	"/ _|  _|  _| || |\n" +
	"\\__|\\__|\\__|\\_, |\n" +
	"            |__/ "
