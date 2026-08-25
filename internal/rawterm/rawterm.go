// Package rawterm provides stdin raw-mode toggling shared by the
// serial and telnet interactive bridges. golang.org/x/term implements
// the correct per-platform ioctls (POSIX termios, Windows console API),
// replacing hand-rolled constants.
package rawterm

import (
	"os"

	"golang.org/x/term"
)

// State aliases x/term's terminal state handle.
type State = term.State

// MakeRaw switches stdin into raw mode and returns the prior state.
func MakeRaw() (*term.State, error) {
	return term.MakeRaw(int(os.Stdin.Fd()))
}

// Restore puts stdin back into the saved state. Nil is a no-op so the
// caller can defer unconditionally.
func Restore(old *term.State) {
	if old != nil {
		_ = term.Restore(int(os.Stdin.Fd()), old)
	}
}
