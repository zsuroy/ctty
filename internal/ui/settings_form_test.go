package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/i18n"
)

func TestSettingsFormInitialization(t *testing.T) {
	enabled := false
	cfg := &config.AppConfig{
		Language:        "zh_CN",
		Theme:           "catppuccin",
		CheckForUpdates: &enabled,
		KeyBindings: config.KeyBindings{
			DisableEscQuit: true,
		},
		FTPLayout:  config.FTPLayoutSingle,
		SFTPLayout: config.SFTPLayoutSingle,
	}

	f := NewSettingsForm(NewStyles(80), 80, 24, cfg)
	if f.langVal != "zh_CN" {
		t.Errorf("expected langVal zh_CN, got %q", f.langVal)
	}
	if f.themeVal != "catppuccin" {
		t.Errorf("expected themeVal catppuccin, got %q", f.themeVal)
	}
	if f.updateVal != false {
		t.Errorf("expected updateVal false, got %v", f.updateVal)
	}
	if f.disableEscVal != true {
		t.Errorf("expected disableEscVal true, got %v", f.disableEscVal)
	}
	if f.ftpLayoutVal != "single" {
		t.Errorf("expected ftpLayoutVal single, got %q", f.ftpLayoutVal)
	}
	if f.sftpLayoutVal != "single" {
		t.Errorf("expected sftpLayoutVal single, got %q", f.sftpLayoutVal)
	}
}

func TestSettingsFormRendering(t *testing.T) {
	for _, lang := range []string{i18n.LangZHCN, i18n.LangEN} {
		t.Run(lang, func(t *testing.T) {
			i18n.Init(lang)
			f := NewSettingsForm(NewStyles(80), 80, 24, &config.AppConfig{})
			f.Init()
			view := f.View()
			if view == "" {
				t.Fatal("expected non-empty settings view")
			}
			t.Logf("RENDERED VIEW:\n%s", view)

			// Verify key settings titles appear in rendered output
			for _, key := range []string{
				"settings.title",
				"settings.lang_label",
				"settings.theme_label",
				"settings.update_label",
			} {
				expected := i18n.T(key)
				if !strings.Contains(view, expected) {
					t.Errorf("view missing expected label %q", expected)
				}
			}
		})
	}
}

func TestSettingsFormCancel(t *testing.T) {
	f := NewSettingsForm(NewStyles(80), 80, 24, &config.AppConfig{})
	updated, cmd := f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !updated.cancelled {
		t.Error("expected form to be cancelled on Esc")
	}
	if cmd == nil {
		t.Fatal("expected cancel cmd")
	}
	msg := cmd()
	closeMsg, ok := msg.(settingsCloseMsg)
	if !ok || closeMsg.Saved {
		t.Errorf("expected closeMsg with Saved=false, got %+v", msg)
	}
}

func TestSettingsFormSave(t *testing.T) {
	cfg := &config.AppConfig{}
	f := NewSettingsForm(NewStyles(80), 80, 24, cfg)
	f.langVal = "en"
	f.themeVal = "dracula"
	f.updateVal = true

	cmd := f.saveSettings()
	if cmd == nil {
		t.Fatal("expected save cmd")
	}
	msg := cmd()
	closeMsg, ok := msg.(settingsCloseMsg)
	if !ok || !closeMsg.Saved {
		t.Fatalf("expected closeMsg with Saved=true, got %+v", msg)
	}
	if closeMsg.AppConfig.Language != "en" {
		t.Errorf("expected saved language en, got %q", closeMsg.AppConfig.Language)
	}
	if closeMsg.AppConfig.Theme != "dracula" {
		t.Errorf("expected saved theme dracula, got %q", closeMsg.AppConfig.Theme)
	}
}

func TestSettingsFormWindowResize(t *testing.T) {
	f := NewSettingsForm(NewStyles(80), 80, 24, &config.AppConfig{})
	for _, size := range []struct{ w, h int }{
		{40, 15},
		{80, 24},
		{120, 40},
	} {
		f.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		view := f.View()
		if view == "" {
			t.Errorf("empty view on size %dx%d", size.w, size.h)
		}
	}
}

func TestSettingsFormNavigation(t *testing.T) {
	f := NewSettingsForm(NewStyles(80), 80, 24, &config.AppConfig{})
	f.Init()

	expectedKeys := []string{"lang", "theme", "update", "esc", "ftp", "sftp", "save"}

	// Check initial field
	if f.form.GetFocusedField().GetKey() != expectedKeys[0] {
		t.Fatalf("expected initial field %q, got %q", expectedKeys[0], f.form.GetFocusedField().GetKey())
	}

	// Navigate down with KeyDown
	for i := 1; i < len(expectedKeys); i++ {
		f.Update(tea.KeyMsg{Type: tea.KeyDown})
		got := f.form.GetFocusedField().GetKey()
		if got != expectedKeys[i] {
			t.Fatalf("step %d: expected field %q after KeyDown, got %q", i, expectedKeys[i], got)
		}
	}

	// Pressing down on the last field ("save") should stay on "save"
	f.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := f.form.GetFocusedField().GetKey(); got != "save" {
		t.Fatalf("expected to stay on 'save', got %q", got)
	}

	// Navigate back up with KeyUp
	for i := len(expectedKeys) - 2; i >= 0; i-- {
		f.Update(tea.KeyMsg{Type: tea.KeyUp})
		got := f.form.GetFocusedField().GetKey()
		if got != expectedKeys[i] {
			t.Fatalf("step back %d: expected field %q after KeyUp, got %q", i, expectedKeys[i], got)
		}
	}

	// Pressing up on the first field ("lang") should stay on "lang"
	f.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := f.form.GetFocusedField().GetKey(); got != "lang" {
		t.Fatalf("expected to stay on 'lang', got %q", got)
	}

	// Test 'j' and 'k' navigation
	f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if got := f.form.GetFocusedField().GetKey(); got != "theme" {
		t.Fatalf("expected 'theme' after 'j', got %q", got)
	}
	f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if got := f.form.GetFocusedField().GetKey(); got != "lang" {
		t.Fatalf("expected 'lang' after 'k', got %q", got)
	}

	// Test 'enter' advances on select
	f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := f.form.GetFocusedField().GetKey(); got != "theme" {
		t.Fatalf("expected 'theme' after 'enter', got %q", got)
	}

	// Test 'tab' navigation
	f.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := f.form.GetFocusedField().GetKey(); got != "update" {
		t.Fatalf("expected 'update' after 'tab', got %q", got)
	}

	// Advance with Tab until "save"
	f.Update(tea.KeyMsg{Type: tea.KeyTab}) // esc
	f.Update(tea.KeyMsg{Type: tea.KeyTab}) // ftp
	f.Update(tea.KeyMsg{Type: tea.KeyTab}) // sftp
	f.Update(tea.KeyMsg{Type: tea.KeyTab}) // save
	if got := f.form.GetFocusedField().GetKey(); got != "save" {
		t.Fatalf("expected 'save' after tabs, got %q", got)
	}

	// Tab on "save" should loop back to "lang"
	f.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := f.form.GetFocusedField().GetKey(); got != "lang" {
		t.Fatalf("expected 'lang' after Tab on 'save' (loop), got %q", got)
	}

	// Shift+Tab on "lang" should loop to "save"
	f.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if got := f.form.GetFocusedField().GetKey(); got != "save" {
		t.Fatalf("expected 'save' after Shift+Tab on 'lang' (loop), got %q", got)
	}

	// Shift+Tab on "save" should go to "sftp"
	f.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if got := f.form.GetFocusedField().GetKey(); got != "sftp" {
		t.Fatalf("expected 'sftp' after Shift+Tab on 'save', got %q", got)
	}
}

func TestSettingsFormHeaderAlwaysVisible(t *testing.T) {
	headerText := i18n.T("settings.title")

	for _, size := range []struct{ w, h int }{
		{80, 24},
		{60, 16},
		{50, 12},
		{100, 35},
	} {
		f := NewSettingsForm(NewStyles(size.w), size.w, size.h, &config.AppConfig{})
		f.Init()
		view := f.View()

		// View height must NOT exceed terminal height
		viewLines := strings.Split(view, "\n")
		if len(viewLines) > size.h {
			t.Errorf("size %dx%d: view height %d exceeds terminal height %d", size.w, size.h, len(viewLines), size.h)
		}

		// Top border corner (╭) and header must always be present in view
		if !strings.Contains(view, "╭") {
			t.Errorf("size %dx%d: top border corner (╭) not found in view", size.w, size.h)
		}
		if !strings.Contains(view, headerText) {
			t.Errorf("size %dx%d: header %q not found in view", size.w, size.h, headerText)
		}

		// Bottom border corner (╰) must always be present in view
		if !strings.Contains(view, "╰") {
			t.Errorf("size %dx%d: bottom border corner (╰) not found in view", size.w, size.h)
		}

		// Help keywords must always be present
		if !strings.Contains(view, "Esc") || !strings.Contains(view, "save") {
			t.Errorf("size %dx%d: help not found in view", size.w, size.h)
		}
	}
}
