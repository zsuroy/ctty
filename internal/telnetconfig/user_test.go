package telnetconfig

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestUserFieldRoundTrip covers the auto-login user name added for issue
// #46: it must survive save/load, be trimmed, and stay absent from JSON
// when unset (so existing configs keep their shape).
func TestUserFieldRoundTrip(t *testing.T) {
	isolateConfig(t)

	host := TelnetHost{Name: "rpi", Host: "10.0.0.5", Port: 22, User: "  der  ", Tags: []string{"lab"}}
	if err := Add(host); err != nil {
		t.Fatalf("Add: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("hosts=%d want 1", len(loaded))
	}
	if loaded[0].User != "der" {
		t.Fatalf("User=%q want %q (trimmed)", loaded[0].User, "der")
	}

	// Clearing the user (e.g. switching to password-only) must stick.
	host.User = ""
	if err := Update("rpi", host); err != nil {
		t.Fatalf("Update: %v", err)
	}
	loaded, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded[0].User != "" {
		t.Fatalf("User=%q want empty after clearing", loaded[0].User)
	}
}

// TestUserOmittedFromJSONWhenEmpty keeps hosts without credentials
// byte-identical on disk (the field is omitempty).
func TestUserOmittedFromJSONWhenEmpty(t *testing.T) {
	h := TelnetHost{Name: "core-sw", Host: "10.0.0.1", Port: 23}
	h.normalize()
	if DefaultHost().User != "" {
		t.Fatal("DefaultHost().User must be empty")
	}

	// Actually serialize: string-concatenating the fields does not prove
	// the omitempty tag is present (removing it kept this test green).
	raw, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("marshal host: %v", err)
	}
	if strings.Contains(string(raw), `"user"`) {
		t.Fatalf("empty User must be omitted from JSON, got %s", raw)
	}

	h.User = "pi"
	raw, err = json.Marshal(h)
	if err != nil {
		t.Fatalf("marshal host with user: %v", err)
	}
	if !strings.Contains(string(raw), `"user":"pi"`) {
		t.Fatalf("non-empty User must be serialized, got %s", raw)
	}
}
