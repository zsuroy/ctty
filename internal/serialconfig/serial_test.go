package serialconfig

import "testing"

func TestResolveModePrecedence(t *testing.T) {
	// CLI flag > device mode > global setting > built-in per-entry default.
	cases := []struct {
		name                     string
		override, device, global string
		standalone               bool
		want                     string
	}{
		{"flag beats everything", ModeAssistant, ModeRaw, ModeRaw, true, ModeAssistant},
		{"device beats global", "", ModeAssistant, ModeRaw, false, ModeAssistant},
		{"global beats default", "", ModeInherit, ModeRaw, false, ModeRaw},
		{"standalone default is raw", "", ModeInherit, "auto", true, ModeRaw},
		{"tui default is assistant", "", ModeInherit, "", false, ModeAssistant},
		{"legacy alias normalises", "passthrough", "", "", false, ModeRaw},
		{"garbage inherits", "nonsense", "", "", false, ModeAssistant},
	}
	for _, c := range cases {
		if got := ResolveMode(c.override, c.device, c.global, c.standalone); got != c.want {
			t.Errorf("%s: ResolveMode(%q,%q,%q,%v) = %q, want %q",
				c.name, c.override, c.device, c.global, c.standalone, got, c.want)
		}
	}
}

func TestSetModePersistsPerDevice(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	dev := DefaultDevice()
	dev.Name = "Board"
	dev.Device = "COM7"
	if err := Add(dev); err != nil {
		t.Fatal(err)
	}
	if err := SetMode("Board", "raw"); err != nil {
		t.Fatal(err)
	}
	got, ok := Find("Board")
	if !ok || got.Mode != ModeRaw {
		t.Fatalf("stored mode = %q, want %q", got.Mode, ModeRaw)
	}
	// Unknown names are a no-op, not an error, so a stale form cannot fail.
	if err := SetMode("Ghost", ModeAssistant); err != nil {
		t.Fatalf("unknown device should be a no-op, got %v", err)
	}
}
