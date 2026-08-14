//go:build darwin

package i18n

import (
	"os"
	"os/exec"
	"strings"
)

// detectPlatformLanguage detects the system language on macOS.
// On macOS, terminal emulators commonly inject LANG=en_US.UTF-8 for UTF-8 byte encoding
// regardless of the user's actual UI language in macOS System Settings.
// Therefore, we prioritize AppleLocale / AppleLanguages before falling back to LANG.
func detectPlatformLanguage() string {
	// 1. Explicit CTTY_LANG overrides everything
	if val := os.Getenv("CTTY_LANG"); val != "" {
		if norm := normalizeLang(val); norm != "" {
			return norm
		}
	}

	// 2. Explicit LC_ALL / LC_MESSAGES
	for _, env := range []string{"LC_ALL", "LC_MESSAGES"} {
		if val := os.Getenv(env); val != "" {
			if norm := normalizeLang(val); norm != "" {
				return norm
			}
		}
	}

	// 3. macOS System Preferences: AppleLocale (e.g. "zh_CN", "zh_CN@currency=CNY")
	if out, err := exec.Command("defaults", "read", "-g", "AppleLocale").Output(); err == nil {
		if norm := normalizeLang(string(out)); norm != "" {
			return norm
		}
	}

	// 4. macOS System Preferences: AppleLanguages (e.g. ("zh-Hans-CN", "en-US"))
	if out, err := exec.Command("defaults", "read", "-g", "AppleLanguages").Output(); err == nil {
		content := string(out)
		// Extract first language from plist array output
		for _, line := range strings.Split(content, "\n") {
			line = strings.Trim(line, " \t\r\n,()\"")
			if line != "" {
				if norm := normalizeLang(line); norm != "" {
					return norm
				}
				break
			}
		}
	}

	// 5. Fallback to standard $LANG
	if val := os.Getenv("LANG"); val != "" {
		if norm := normalizeLang(val); norm != "" {
			return norm
		}
	}

	return LangEN
}
