//go:build windows

package i18n

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procGetUserDefaultLocaleName = kernel32.NewProc("GetUserDefaultLocaleName")
)

// detectPlatformLanguage detects the system language on Windows.
func detectPlatformLanguage() string {
	// 1. Explicit CTTY_LANG
	if val := os.Getenv("CTTY_LANG"); val != "" {
		if norm := normalizeLang(val); norm != "" {
			return norm
		}
	}

	// 2. Explicit POSIX environment variables (Git Bash, MSYS2, WSL, or user-set)
	for _, env := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if val := os.Getenv(env); val != "" {
			if norm := normalizeLang(val); norm != "" {
				return norm
			}
		}
	}

	// 3. Windows Native API: GetUserDefaultLocaleName
	const localeNameMaxLength = 85
	b := make([]uint16, localeNameMaxLength)
	r, _, _ := procGetUserDefaultLocaleName.Call(
		uintptr(unsafe.Pointer(&b[0])),
		uintptr(localeNameMaxLength),
	)
	if r != 0 {
		locale := syscall.UTF16ToString(b)
		if norm := normalizeLang(locale); norm != "" {
			return norm
		}
	}

	return LangEN
}
