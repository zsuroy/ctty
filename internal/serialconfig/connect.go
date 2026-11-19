package serialconfig

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/zsuroy/ctty/internal/sessionsnippet"
	"go.bug.st/serial"
)

// serialExecCommand implements tea.ExecCommand so that Bubble Tea's
// tea.Exec() properly suspends the TUI (releases the terminal to
// normal mode) before we take over stdin/stdout for the serial bridge,
// and restores the TUI when we return.
type serialExecCommand struct {
	dev    SerialDevice
	stdin  io.Reader
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
// until the user presses or Ctrl+].
func (c *serialExecCommand) Run() error {
	mode := &serial.Mode{
		BaudRate: c.dev.BaudRate,
		DataBits: c.dev.DataBits,
		Parity:   ParityFromString(c.dev.Parity),
		StopBits: StopBitsFromString(c.dev.StopBits),
	}

	port, err := serialOpen(c.dev.Device, mode)
	if err != nil {
		return fmt.Errorf("opening serial port %s: %w", c.dev.Device, err)
	}
	var closeOnce sync.Once
	closePort := func() {
		closeOnce.Do(func() { _ = port.Close() })
	}
	defer closePort()

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

	fmt.Fprintf(errOut, "Connected to %s (%s @ %d baud). Press Ctrl+] to disconnect, "+sessionsnippet.HotkeyLabel+" for saved commands.\n",
		c.dev.Name, c.dev.Device, c.dev.BaudRate)

	oldState, err := setRawStdinFn()
	if err != nil {
		return fmt.Errorf("setting raw mode: %w", err)
	}
	defer restoreStdinFn(oldState)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	errCh := make(chan error, 2)

	// Device output goes through the gate so the snippet list stays readable.
	gate := sessionsnippet.NewOutputGate(out)
	picker := sessionsnippet.NewPicker(gate)

	go func() {
		defer picker.Close()
		buf := make([]byte, 4096)
		for {
			n, rerr := in.Read(buf)
			if n > 0 {
				if containsDisconnect(buf[:n]) {
					closePort()
					errCh <- nil
					return
				}
				send := picker.Process(buf[:n])
				written := 0
				for written < len(send) {
					m, werr := port.Write(send[written:])
					if werr != nil {
						if isPortClosed(werr) {
							errCh <- nil
						} else {
							errCh <- fmt.Errorf("serial write: %w", werr)
						}
						return
					}
					if m == 0 {
						break
					}
					written += m
				}
			}
			if rerr != nil {
				if errors.Is(rerr, io.EOF) {
					errCh <- nil
				} else {
					errCh <- rerr
				}
				return
			}
		}
	}()

	go func() {
		_, err := io.Copy(gate, port)
		if err != nil && isPortClosed(err) {
			err = nil
		}
		errCh <- err
	}()

	select {
	case err := <-errCh:
		closePort()
		if err != nil {
			return err
		}
	case <-sigCh:
		closePort()
	}

	return nil
}

func isPortClosed(err error) bool {
	var pe *serial.PortError
	if errors.As(err, &pe) {
		if pe.Code() == serial.PortClosed {
			return true
		}
		if strings.Contains(pe.Error(), "closed") {
			return true
		}
	}
	var peVal serial.PortError
	if errors.As(err, &peVal) {
		if peVal.Code() == serial.PortClosed {
			return true
		}
		if strings.Contains(peVal.Error(), "closed") {
			return true
		}
	}
	if errors.Is(err, syscall.EBADF) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "Port has been closed") ||
		strings.Contains(msg, "bad file descriptor") ||
		strings.Contains(msg, "file descriptor")
}

var serialOpen = serial.Open

var setRawStdinFn = setRawStdin
var restoreStdinFn = restoreStdin

// containsDisconnect reports if chunk carries Ctrl-] (0x1d)
// advertised as disconnect keys for serial. Ctrl+C (0x03) is
// intentionally not treated as a disconnect and is forwarded to the
// serial device instead.
func containsDisconnect(chunk []byte) bool {
	for _, c := range chunk {
		if c == 0x1d {
			return true
		}
	}
	return false
}
