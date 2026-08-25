//go:build !windows

// Raw terminal mode for stdin via the shared rawterm package
// (golang.org/x/term). Replaces the hand-rolled termios ioctl
// constants (Linux TCGETS/TCSETS, macOS TIOCGETA/TIOCSETA) which
// duplicated platform structs and risked drift on other unix targets.

package serialconfig

import "github.com/zsuroy/ctty/internal/rawterm"

// termState aliases the x/term state handle used by both bridges.
type termState = rawterm.State

func setRawStdin() (*termState, error) {
	return rawterm.MakeRaw()
}

func restoreStdin(old *termState) {
	rawterm.Restore(old)
}
