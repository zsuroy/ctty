//go:build windows

package ui

import (
	"os"

	"golang.org/x/sys/windows"
)

// init records whether the console output code page is a double-byte
// (East Asian) one. In a DBCS code page (GBK 936, Big5 950, Shift-JIS
// 932, Hangul 949) ConHost advances East-Asian-Ambiguous characters a
// full two cells, while ansi.StringWidth counts one; glyphDeltaFor
// applies the correction (see dbcsAmbiguousWide). mintty and Windows
// Terminal report the UTF-8 code page or are excluded by the
// isWindowsLegacyConsole gate at the use site, so they are unaffected.
func init() {
	cp, err := windows.GetConsoleOutputCP()
	if err != nil {
		return
	}
	switch cp {
	case 932, 936, 949, 950:
		consoleOutputCPDBCS = true
	}
}

// setupWindowsConsoleOutput prepares the legacy Windows console host
// (ConHost behind cmd.exe / Windows PowerShell) for full-width TUI frames
// and returns a function restoring the original output mode.
//
// Bubble Tea's initInput only ORs ENABLE_VIRTUAL_TERMINAL_PROCESSING into
// the output mode. Without DISABLE_NEWLINE_AUTO_RETURN, ConHost performs
// an immediate "auto return" when a character is written into the last
// column of a row instead of deferring the wrap like a VT terminal: every
// full-width row (box borders, padded table rows) consumes two physical
// rows, frames grow taller than the screen, the top rows scroll away, and
// the renderer's line accounting desyncs from the physical cursor (the
// Win10 cmd.exe screenshots: missing Telnet title, duplicated form labels,
// right borders torn off their corners). Setting the flag before the
// program starts survives Bubble Tea's mode update because it ORs its own
// flag into the mode it reads back.
func setupWindowsConsoleOutput() func() {
	if !isWindowsLegacyConsole() {
		return func() {}
	}
	f, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		return func() {}
	}
	handle := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		f.Close()
		return func() {}
	}
	if err := windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.DISABLE_NEWLINE_AUTO_RETURN); err != nil {
		f.Close()
		return func() {}
	}
	return func() {
		_ = windows.SetConsoleMode(handle, mode)
		f.Close()
	}
}
