//go:build windows

package ui

import (
	"golang.org/x/sys/windows"

	"github.com/zsuroy/ctty/internal/winterm"
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

func setupWindowsConsoleOutput() func() {
	return winterm.PrepareForTUI()
}
