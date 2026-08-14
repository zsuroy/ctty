package i18n

import (
	"fmt"
	"strings"
	"sync"
)

const (
	LangEN   = "en"
	LangZHCN = "zh_CN"
)

var (
	mu          sync.RWMutex
	currentLang = LangEN
)

// SetLang explicitly sets the active language
func SetLang(lang string) {
	mu.Lock()
	defer mu.Unlock()

	normalized := normalizeLang(lang)
	if normalized != "" {
		currentLang = normalized
	} else {
		currentLang = LangEN
	}
}

// CurrentLang returns the current active language code
func CurrentLang() string {
	mu.RLock()
	defer mu.RUnlock()
	return currentLang
}

// normalizeLang maps various language strings to supported language constants
func normalizeLang(lang string) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	switch {
	case strings.HasPrefix(l, "zh"):
		return LangZHCN
	case strings.HasPrefix(l, "en"):
		return LangEN
	default:
		return ""
	}
}

// DetectLanguage detects the system language using platform-specific mechanisms
func DetectLanguage() string {
	return detectPlatformLanguage()
}

// Init initializes the i18n subsystem based on configured language, CLI flag, or system auto-detection
func Init(configuredLang string) {
	if configuredLang != "" && strings.ToLower(configuredLang) != "auto" {
		SetLang(configuredLang)
		return
	}
	SetLang(DetectLanguage())
}

// T translates a message key to the current language, optionally formatting with args
func T(key string, args ...any) string {
	mu.RLock()
	lang := currentLang
	mu.RUnlock()

	// 1. Look up in current language
	msgDict, ok := messages[lang]
	if !ok {
		msgDict = messages[LangEN]
	}

	msg, found := msgDict[key]
	if !found {
		// 2. Fallback to English
		if lang != LangEN {
			if enDict, ok := messages[LangEN]; ok {
				msg, found = enDict[key]
			}
		}
		if !found {
			msg = key
		}
	}

	if len(args) > 0 {
		return fmt.Sprintf(msg, args...)
	}
	return msg
}
