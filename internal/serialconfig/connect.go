package serialconfig

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"go.bug.st/serial"
)

// serialExecCommand implements tea.ExecCommand so that Bubble Tea's
// tea.Exec() properly suspends the TUI (releases the terminal to
// normal mode) before we take over stdin/stdout for the serial bridge,
// and restores the TUI when we return.
type serialExecCommand struct {
	dev   SerialDevice
	stdin io.Reader
	stdout io.Writer
	stderr io.Writer
}

// NewExecCommand creates an ExecCommand for the given serial device.
// Pass the result to tea.Exec() — Bubble Tea will call SetStdin/SetStdout
// with the real terminal handles, then call Run().
func NewExecCommand(dev SerialDevice) *serialExecCommand {
	return &serialExecCommand{dev: dev}
}

func (c *serialExecCommand) SetStdin(r io.Reader)  { c.stdin = r }
func (c *serialExecCommand) SetStdout(w io.Writer) { c.stdout = w }
func (c *serialExecCommand) SetStderr(w io.Writer) { c.stderr = w }

// Run opens the serial port and bridges terminal stdin/stdout to it
// until the user presses Ctrl+C or Ctrl+].
func (c *serialExecCommand) Run() error {
	mode := &serial.Mode{
		BaudRate: c.dev.BaudRate,
		DataBits: c.dev.DataBits,
		Parity:   ParityFromString(c.dev.Parity),
		StopBits: StopBitsFromString(c.dev.StopBits),
	}

	port, err := serial.Open(c.dev.Device, mode)
	if err != nil {
		return fmt.Errorf("opening serial port %s: %w", c.dev.Device, err)
	}
	defer port.Close()

	out := c.stdout
	if out == nil {
		out = os.Stdout
	}
	in := c.stdin
	if in == nil {
		in = os.Stdin
	}
	errOut := c.stderr
	if errOut == nil {
		errOut = os.Stderr
	}

	fmt.Fprintf(errOut, "Connected to %s (%s @ %d baud). Press Ctrl+] or Ctrl+C to disconnect.\n",
		c.dev.Name, c.dev.Device, c.dev.BaudRate)

	// Set stdin to raw mode so keystrokes go directly to the serial port.
	oldState, err := setRawStdin()
	if err != nil {
		return fmt.Errorf("setting raw mode: %w", err)
	}
	defer restoreStdin(oldState)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	doneCh := make(chan struct{}, 2)

	// stdin -> serial port
	go func() {
		_, _ = io.Copy(port, in)
		doneCh <- struct{}{}
	}()

	// serial port -> stdout
	go func() {
		_, _ = io.Copy(out, port)
		doneCh <- struct{}{}
	}()

	select {
	case <-doneCh:
	case <-sigCh:
	}

	fmt.Fprintln(errOut, "\nDisconnected.")
	return nil
}
