//go:build !windows

package ui

// setupWindowsConsoleOutput is a no-op off Windows; see console_windows.go.
func setupWindowsConsoleOutput() func() {
	return func() {}
}
