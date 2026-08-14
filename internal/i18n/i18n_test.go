package i18n

import (
	"os"
	"testing"
)

func TestI18nBasic(t *testing.T) {
	// Test English
	SetLang("en")
	if CurrentLang() != LangEN {
		t.Errorf("Expected currentLang to be en, got %s", CurrentLang())
	}
	if T("table.col.name") != "Name" {
		t.Errorf("Expected 'Name', got '%s'", T("table.col.name"))
	}
	if T("cli.found_hosts", 5) != "Found 5 host(s)" {
		t.Errorf("Expected 'Found 5 host(s)', got '%s'", T("cli.found_hosts", 5))
	}

	// Test Chinese
	SetLang("zh_CN")
	if CurrentLang() != LangZHCN {
		t.Errorf("Expected currentLang to be zh_CN, got %s", CurrentLang())
	}
	if T("table.col.name") != "名称" {
		t.Errorf("Expected '名称', got '%s'", T("table.col.name"))
	}
	if T("cli.found_hosts", 5) != "共找到 5 台主机" {
		t.Errorf("Expected '共找到 5 台主机', got '%s'", T("cli.found_hosts", 5))
	}

	// Test Fallback for unknown key
	if T("non.existent.key") != "non.existent.key" {
		t.Errorf("Expected 'non.existent.key', got '%s'", T("non.existent.key"))
	}
}

func TestDetectLanguage(t *testing.T) {
	// Test explicit CTTY_LANG overrides
	oldCTTYLang := os.Getenv("CTTY_LANG")
	defer func() {
		if oldCTTYLang != "" {
			os.Setenv("CTTY_LANG", oldCTTYLang)
		} else {
			os.Unsetenv("CTTY_LANG")
		}
	}()

	os.Setenv("CTTY_LANG", "zh")
	if DetectLanguage() != LangZHCN {
		t.Errorf("Expected zh_CN for CTTY_LANG=zh, got %s", DetectLanguage())
	}

	os.Setenv("CTTY_LANG", "en")
	if DetectLanguage() != LangEN {
		t.Errorf("Expected en for CTTY_LANG=en, got %s", DetectLanguage())
	}
}
