package ui

import "github.com/charmbracelet/lipgloss"

// Theme colors
var (
	// Primary interface color - easily modifiable
	PrimaryColor = "#00ADD8" // Official Go logo blue color

	// Secondary colors
	SecondaryColor = "240" // Gray
	ErrorColor     = "1"   // Red
	SuccessColor   = "36"  // Green (for reference if needed)
)

// Styles struct centralizes all lipgloss styles
type Styles struct {
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

// NewStyles creates a new Styles struct with the given terminal width
func NewStyles(width int) Styles {
	return Styles{
		// Main app container
		App: lipgloss.NewStyle().
			Padding(0, 1),

		// Header style
		Header: lipgloss.NewStyle().
			Foreground(lipgloss.Color(PrimaryColor)).
			Bold(true).
			Align(lipgloss.Center),

		// Search styles
		SearchFocused: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(PrimaryColor)).
			Padding(0, 1),

		SearchUnfocused: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(SecondaryColor)).
			Padding(0, 1),

		// Table styles
		TableFocused: lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(PrimaryColor)),

		TableUnfocused: lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(SecondaryColor)),

		// Style for selected items
		Selected: lipgloss.NewStyle().
			Foreground(lipgloss.Color("229")).
			Background(lipgloss.Color(PrimaryColor)).
			Bold(false),

		// Info styles
		SortInfo: lipgloss.NewStyle().
			Foreground(lipgloss.Color(SecondaryColor)),

		HelpText: lipgloss.NewStyle().
			Foreground(lipgloss.Color(SecondaryColor)),

		// Error style
		Error: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(ErrorColor)).
			Padding(1, 2),

		// Error text style (no border, just red text)
		ErrorText: lipgloss.NewStyle().
			Foreground(lipgloss.Color(ErrorColor)).
			Bold(true),

		// Form styles
		FormTitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFDF5")).
			Background(lipgloss.Color(PrimaryColor)).
			Padding(0, 1),

		FormField: lipgloss.NewStyle().
			Foreground(lipgloss.Color(PrimaryColor)),

		FormHelp: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262")),

		FormContainer: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(PrimaryColor)).
			Padding(1, 2),

		Label: lipgloss.NewStyle().
			Foreground(lipgloss.Color(SecondaryColor)),

		FocusedLabel: lipgloss.NewStyle().
			Foreground(lipgloss.Color(PrimaryColor)),

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
