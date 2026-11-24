package ui

import (
	"io"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zsuroy/ctty/internal/winterm"
)

// osExecCommand wraps an *exec.Cmd for tea.Exec while preserving Bubble Tea's
// "only fill empty stdio" behavior (the same contract as tea.ExecProcess's
// internal wrapper).
type osExecCommand struct{ *exec.Cmd }

func (c *osExecCommand) SetStdin(r io.Reader) {
	if c.Stdin == nil {
		c.Stdin = r
	}
}

func (c *osExecCommand) SetStdout(w io.Writer) {
	if c.Stdout == nil {
		c.Stdout = w
	}
}

func (c *osExecCommand) SetStderr(w io.Writer) {
	if c.Stderr == nil {
		c.Stderr = w
	}
}

// consoleExecCommand restores the TUI console mode after a child process owns
// the terminal. Windows OpenSSH in particular can leave the console without VT
// processing on its failed-auth path; without this Bubble Tea's resume/quit
// escape sequences print literally.
type consoleExecCommand struct {
	tea.ExecCommand
}

func (c consoleExecCommand) Run() error {
	restore := winterm.PrepareForTUIChild()
	defer restore()
	return c.ExecCommand.Run()
}

func execInTerminal(command tea.ExecCommand, fn tea.ExecCallback) tea.Cmd {
	return tea.Exec(consoleExecCommand{command}, fn)
}

func execProcess(cmd *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
	return execInTerminal(&osExecCommand{Cmd: cmd}, fn)
}

// monitorMouseCmd switches terminal mouse reporting on for the serial
// assistant and off again on the way out. The assistant needs wheel events to
// scroll its receive pane; other views keep native drag-select, so this is
// scoped to that one page instead of a global tea.WithMouseCellMotion.
func monitorMouseCmd(on bool) tea.Cmd {
	return func() tea.Msg {
		if on {
			return tea.EnableMouseCellMotion()
		}
		return tea.DisableMouse()
	}
}
