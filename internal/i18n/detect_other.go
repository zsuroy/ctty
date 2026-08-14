//go:build !darwin && !windows

package i18n

import (
	"os"
	"os/exec"
)

// detectPlatformLanguage detects the system language on Linux, Android/Termux, and other Unix platforms.
func detectPlatformLanguage() string {
	// 1. Explicit CTTY_LANG
	if val := os.Getenv("CTTY_LANG"); val != "" {
		if norm := normalizeLang(val); norm != "" {
			return norm
		}
	}

	// 2. Standard POSIX environment variables
	for _, env := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if val := os.Getenv(env); val != "" {
			if norm := normalizeLang(val); norm != "" {
				return norm
			}
		}
	}

	// 3. On Android / Termux: check getprop
	if out, err := exec.Command("getprop", "persist.sys.locale").Output(); err == nil {
		if norm := normalizeLang(string(out)); norm != "" {
			return norm
		}
	}
	if out, err := exec.Command("getprop", "ro.product.locale").Output(); err == nil {
		if norm := normalizeLang(string(out)); norm != "" {
			return norm
		}
	}

	return LangEN
}
