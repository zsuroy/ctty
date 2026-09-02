//go:build windows

// Raw terminal mode for stdin via the shared rawterm package
// (golang.org/x/term). Windows previously shipped a no-op stub,
// leaving the console in cooked mode during serial/telnet sessions.

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
