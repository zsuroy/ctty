//go:build windows

package winterm

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

const tuiOutputMode = windows.ENABLE_PROCESSED_OUTPUT | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING

// IsLegacyConsole reports whether the process is attached to the classic
// Windows console host (ConHost), as opposed to Windows Terminal, mintty, or
// an IDE-embedded terminal. Those terminals either do not use ConHost or have
// their own glyph/width behavior.
func IsLegacyConsole() bool {
	if os.Getenv("WT_SESSION") != "" {
		return false
	}
	if os.Getenv("MSYSTEM") != "" || os.Getenv("TERM_PROGRAM") == "mintty" {
		return false
	}
	if os.Getenv("TERM_PROGRAM") == "vscode" {
		return false
	}
	if strings.Contains(os.Getenv("TERMINAL_EMULATOR"), "JetBrains") {
		return false
	}
	if os.Getenv("JETBRAINS_INTELLIJ_COMMAND_X") != "" || os.Getenv("IDEA_INITIAL_DIRECTORY") != "" {
		return false
	}
	return true
}

type consoleHandle struct {
	file   *os.File
	handle windows.Handle
	mode   uint32
	ok     bool
}

func openConsole(name string) consoleHandle {
	f, err := os.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		return consoleHandle{}
	}
	handle := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		_ = f.Close()
		return consoleHandle{}
	}
	return consoleHandle{file: f, handle: handle, mode: mode, ok: true}
}

func (c consoleHandle) close() {
	if c.ok {
		_ = c.file.Close()
	}
}

// PrepareForTUI prepares the console for Bubble Tea and returns a function
// that restores the mode the shell had before the TUI started.
//
// Bubble Tea's initInput only ORs ENABLE_VIRTUAL_TERMINAL_PROCESSING into the
// output mode. Without DISABLE_NEWLINE_AUTO_RETURN, ConHost performs an
// immediate "auto return" when a character is written into the last column of
// a row instead of deferring the wrap like a VT terminal: every full-width row
// consumes two physical rows, frames grow taller than the screen, and the
// renderer's line accounting desyncs from the physical cursor. The flag must
// be set before the program starts so Bubble Tea reads it back and preserves
// it.
func PrepareForTUI() func() {
	out := openConsole("CONOUT$")
	if !out.ok {
		return func() {}
	}

	if IsLegacyConsole() {
		mode := out.mode | tuiOutputMode | windows.DISABLE_NEWLINE_AUTO_RETURN
		if err := windows.SetConsoleMode(out.handle, mode); err != nil {
			out.close()
			return func() {}
		}
	}

	return func() {
		_ = windows.SetConsoleMode(out.handle, out.mode)
		out.close()
	}
}

// PrepareForTUIChild prepares the console for a TUI child process. After the
// child exits it restores the TUI output mode Bubble Tea needs to resume.
func PrepareForTUIChild() func() {
	return prepareForChild(true)
}

// PrepareForCLIChild prepares the console for a non-TUI child process and
// restores the exact shell modes that were active before it started.
func PrepareForCLIChild() func() {
	return prepareForChild(false)
}

// prepareForChild captures the console input/output modes, gives the child a
// usable VT output mode, and returns a function that restores the captured
// modes.
//
// Windows OpenSSH saves console modes at startup and can restore stale values
// on its failed-auth path, clearing ENABLE_PROCESSED_OUTPUT and
// ENABLE_VIRTUAL_TERMINAL_PROCESSING on the shared screen buffer. A TUI then
// paints its resume/quit escape sequences literally. forTUI keeps the
// processed/VT output bits and, on legacy ConHost, DISABLE_NEWLINE_AUTO_RETURN
// so Bubble Tea can resume its frame; direct CLI callers get their exact
// pre-child modes back instead.
func prepareForChild(forTUI bool) func() {
	out := openConsole("CONOUT$")
	in := openConsole("CONIN$")
	legacy := IsLegacyConsole()

	if out.ok {
		childMode := out.mode | tuiOutputMode
		if legacy {
			childMode &^= windows.DISABLE_NEWLINE_AUTO_RETURN
		}
		_ = windows.SetConsoleMode(out.handle, childMode)
	}

	restoreMode := out.mode
	if forTUI {
		restoreMode |= tuiOutputMode
		if legacy {
			restoreMode |= windows.DISABLE_NEWLINE_AUTO_RETURN
		}
	}

	return func() {
		if out.ok {
			_ = windows.SetConsoleMode(out.handle, restoreMode)
			out.close()
		}
		if in.ok {
			_ = windows.SetConsoleMode(in.handle, in.mode)
			in.close()
		}
	}
}
