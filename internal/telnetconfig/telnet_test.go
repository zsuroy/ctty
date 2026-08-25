package telnetconfig

import (
	"os"
	"path/filepath"
	"testing"
)

// isolateConfig points XDG_CONFIG_HOME at a temp dir for the duration of
// the test, mirroring the pattern in internal/config/ssh_test.go.
func isolateConfig(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()

	originalXDG := os.Getenv("XDG_CONFIG_HOME")
	originalHome := os.Getenv("HOME")
	os.Setenv("XDG_CONFIG_HOME", tempDir)
	os.Setenv("HOME", tempDir)
	t.Cleanup(func() {
		os.Setenv("XDG_CONFIG_HOME", originalXDG)
		os.Setenv("HOME", originalHome)
	})
	return tempDir
}

func TestLoadMissingFile(t *testing.T) {
	isolateConfig(t)
	hosts, err := Load()
	if err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if len(hosts) != 0 {
		t.Fatalf("expected empty list, got %d hosts", len(hosts))
	}
}

func TestAddUpdateDeleteRoundTrip(t *testing.T) {
	isolateConfig(t)

	// Add two hosts.
	h1 := TelnetHost{Name: "core-sw", Host: "192.168.1.1", Tags: []string{"lab"}}
	h2 := TelnetHost{Name: "console01", Host: "10.0.0.5:2001", Port: 0}
	if err := Add(h1); err != nil {
		t.Fatalf("Add h1: %v", err)
	}
	if err := Add(h2); err != nil {
		t.Fatalf("Add h2: %v", err)
	}

	hosts, err := Load()
	if err != nil {
		t.Fatalf("Load after add: %v", err)
	}
	if len(hosts) != 2 {
		t.Fatalf("want 2 hosts, got %d", len(hosts))
	}
	// Sorted by name.
	if hosts[0].Name != "console01" || hosts[1].Name != "core-sw" {
		t.Fatalf("sort by name broken: %v", hostNames(hosts))
	}
	// "host:port" suffix split by normalize.
	found := hosts[0]
	if found.Port != 2001 {
		t.Fatalf("port suffix not parsed, want 2001 got %d", found.Port)
	}
	if found.Host != "10.0.0.5" {
		t.Fatalf("port suffix not stripped from host, got %q", found.Host)
	}

	// Duplicate name rejected.
	if err := Add(TelnetHost{Name: "core-sw", Host: "1.2.3.4"}); err == nil {
		t.Fatal("duplicate name must be rejected")
	}

	// Update by old name; renamed entry keeps data.
	upd := TelnetHost{Name: "core-sw-console", Host: "192.168.1.1", Port: 2323, Tags: []string{"lab", "switch"}}
	if err := Update("core-sw", upd); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, ok := Find("core-sw-console")
	if !ok || got.Port != 2323 || len(got.Tags) != 2 {
		t.Fatalf("updated host wrong: %+v ok=%v", got, ok)
	}
	if _, ok := Find("core-sw"); ok {
		t.Fatal("old name still resolvable after rename")
	}

	// Delete.
	if err := Delete("core-sw-console"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := Find("core-sw-console"); ok {
		t.Fatal("host still present after delete")
	}
	hosts, _ = Load()
	if len(hosts) != 1 {
		t.Fatalf("want 1 remaining host, got %d", len(hosts))
	}
}

func TestUpdateUnknownHost(t *testing.T) {
	isolateConfig(t)
	if err := Update("ghost", TelnetHost{Name: "x", Host: "h"}); err == nil {
		t.Fatal("updating unknown host must fail")
	}
}

func TestSaveFilePermissions(t *testing.T) {
	dir := isolateConfig(t)
	if err := Add(TelnetHost{Name: "a", Host: "b"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "ctty", "telnet.json"))
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("config perms = %o, want 600", perm)
	}
}

func TestAddrIPv6Bracketed(t *testing.T) {
	isolateConfig(t)
	if err := Add(TelnetHost{Name: "v6", Host: "::1"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	h, ok := Find("v6")
	if !ok {
		t.Fatal("saved host not found")
	}
	if got := h.Addr(); got != "[::1]:23" {
		t.Fatalf("Addr() = %q, want [::1]:23", got)
	}
}

func hostNames(hosts []TelnetHost) []string {
	names := make([]string, 0, len(hosts))
	for i := range hosts {
		names = append(names, hosts[i].Name)
	}
	return names
}
