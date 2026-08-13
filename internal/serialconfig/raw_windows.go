//go:build windows

package serialconfig

import "os"

// On Windows, raw mode for stdin is handled differently.
// For now, use a no-op stub — Windows serial support can be added later.
type termios struct{}

func setRawStdin() (*termios, error) {
	return nil, nil
}

func restoreStdin(_ *termios) {}

// Suppress unused import on Windows
var _ = os.Stdin
