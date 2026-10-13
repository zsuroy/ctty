package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"
	"github.com/zsuroy/ctty/internal/ui"
	"golang.org/x/term"
)

// probeGlyphs are the glyphs whose real column advance the TUI layout
// depends on: the drawer title label, site status circles, picker marks
// and help-text symbols.
var probeGlyphs = []struct {
	name string
	s    string
}{
	{"label tag (drawer title)", "\U0001F3F7\ufe0f"},
	{"black circle (site status)", "\U000026AB"},
	{"green circle (online)", "\U0001F7E2"},
	{"filled circle (picker mark)", "\U000025CF"},
	{"hollow circle (picker mark)", "\U000025CB"},
	{"up arrow (help)", "\U00002191"},
	{"down arrow (help)", "\U00002193"},
	{"bullet (help)", "\U00002022"},
}

// runProbeTerminal measures the real column advance of layout-critical
// glyphs in the current terminal via DSR (ESC[6n) cursor-position reports,
// and checks whether the terminal wraps at the width it reports. It exists
// because per-terminal width tables are guesses: a glyph can advance wider
// or narrower than ansi.StringWidth counts, and only the terminal itself
// knows which. (The TUI runs this same measurement at startup.)
func runProbeTerminal(out io.Writer) error {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open /dev/tty: %w", err)
	}
	defer tty.Close()

	oldState, err := term.MakeRaw(int(tty.Fd()))
	if err != nil {
		return fmt.Errorf("set raw mode: %w", err)
	}
	defer term.Restore(int(tty.Fd()), oldState)

	w, h, err := term.GetSize(int(tty.Fd()))
	if err != nil {
		return fmt.Errorf("get terminal size: %w", err)
	}

	var report strings.Builder
	fmt.Fprintf(&report, "terminal reports %dx%d  TERMINAL_EMULATOR=%q TERM_PROGRAM=%q TERM=%q\n",
		w, h, os.Getenv("TERMINAL_EMULATOR"), os.Getenv("TERM_PROGRAM"), os.Getenv("TERM"))

	// DSR sanity check before anything else.
	if _, _, err := ui.ProbeCursorPosition(tty, "ABCD"); err != nil {
		return fmt.Errorf("cursor report failed (terminal does not support ESC[6n?): %w", err)
	}

	report.WriteString("glyph advances (real vs ansi.StringWidth):\n")
	for _, g := range probeGlyphs {
		advance, err := ui.MeasureGlyphAdvance(tty, g.s)
		if err != nil {
			fmt.Fprintf(&report, "  %-30s ERROR %v\n", g.name, err)
			continue
		}
		sw := ansi.StringWidth(g.s)
		flag := ""
		if advance != sw {
			flag = "   <-- MISMATCH"
		}
		fmt.Fprintf(&report, "  %-30s real=%d stringWidth=%d%s\n", g.name, advance, sw, flag)
	}

	// Wrap probe: fill a row with exactly the reported width of
	// single-column chars; a cursor row > 1 means the terminal wrapped,
	// i.e. it is narrower than the width it reports.
	row, _, err := ui.ProbeCursorPosition(tty, strings.Repeat("X", w))
	if err != nil {
		fmt.Fprintf(&report, "wrap probe: ERROR %v\n", err)
	} else if row == 1 {
		fmt.Fprintf(&report, "wrap probe: %d columns fit (no wrap)\n", w)
	} else {
		fmt.Fprintf(&report, "wrap probe: WRAPPED at %d reported columns (cursor row=%d) — terminal is narrower than it reports\n", w, row)
	}

	// Restore a clean screen before printing the report in cooked mode.
	if _, err := fmt.Fprintf(tty, "\x1b[2K\x1b[1;1H"); err != nil {
		return err
	}
	term.Restore(int(tty.Fd()), oldState)

	fmt.Fprint(out, report.String())
	return nil
}

var probeTerminalCmd = &cobra.Command{
	Use:           "probe-terminal",
	Short:         "Measure real glyph advances in this terminal (debugging)",
	Long: "Measures the real column advance of the glyphs the TUI layout depends on, using cursor-position reports (ESC[6n), and checks whether the terminal wraps at the width it reports.\n" +
		"Run it inside the terminal that renders incorrectly and paste the output when reporting a layout bug.",
	Hidden:        true,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runProbeTerminal(cmd.OutOrStdout())
	},
}

func init() {
	RootCmd.AddCommand(probeTerminalCmd)
}
