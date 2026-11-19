package snippets

import (
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if got := Path(); got != filepath.Join(home, ".config", "ctty", "snippets.json") {
		t.Fatalf("Path() = %q", got)
	}
	if got := Load(); got != nil {
		t.Fatalf("missing file should load nil, got %v", got)
	}
	want := []Snippet{{Name: "vlan", Command: "show vlan brief"}}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("round trip = %v, want %v", got, want)
	}
}
