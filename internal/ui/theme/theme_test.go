package theme

import (
	"testing"
)

func TestThemeRegistration(t *testing.T) {
	themes := AllThemes()
	if len(themes) < 6 {
		t.Fatalf("expected at least 6 themes, got %d", len(themes))
	}

	ids := ThemeIDs()
	names := ThemeNames()
	if len(ids) != len(themes) || len(names) != len(themes) {
		t.Fatalf("mismatched ids (%d) or names (%d) vs themes (%d)", len(ids), len(names), len(themes))
	}

	for _, th := range themes {
		if th.ID == "" || th.Name == "" {
			t.Fatalf("theme missing ID or Name: %+v", th)
		}
		if th.HuhTheme() == nil {
			t.Fatalf("theme %s missing HuhTheme", th.ID)
		}
		got := GetTheme(th.ID)
		if got.ID != th.ID {
			t.Fatalf("GetTheme(%q) = %q, want %q", th.ID, got.ID, th.ID)
		}
	}

	// Default fallback
	fallback := GetTheme("non-existent-theme-xyz")
	if fallback.ID != "default" {
		t.Fatalf("expected default fallback, got %q", fallback.ID)
	}

	// Index check
	idx := ThemeIndex("catppuccin")
	if idx != 1 {
		t.Fatalf("expected catppuccin index 1, got %d", idx)
	}
}
