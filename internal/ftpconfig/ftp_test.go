package ftpconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func isolateConfig(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	t.Setenv("HOME", tempDir)
	return tempDir
}

func TestLoadMissingFile(t *testing.T) {
	isolateConfig(t)
	sites, err := Load()
	if err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if len(sites) != 0 {
		t.Fatalf("expected empty list, got %d sites", len(sites))
	}
}

func TestAddUpdateDeleteRoundTrip(t *testing.T) {
	isolateConfig(t)

	s1 := FTPSite{Name: "lab-nas", Host: "nas.example.test", User: "ftpuser", Tags: []string{"lab"}}
	s2 := FTPSite{Name: "edge-ftp", Host: "10.0.0.5:2121", Port: 0, User: "anon"}
	if err := Add(s1); err != nil {
		t.Fatalf("Add s1: %v", err)
	}
	if err := Add(s2); err != nil {
		t.Fatalf("Add s2: %v", err)
	}

	sites, err := Load()
	if err != nil {
		t.Fatalf("Load after add: %v", err)
	}
	if len(sites) != 2 {
		t.Fatalf("want 2 sites, got %d", len(sites))
	}
	if sites[0].Name != "edge-ftp" || sites[1].Name != "lab-nas" {
		t.Fatalf("sort by name broken: %v %v", sites[0].Name, sites[1].Name)
	}
	if sites[0].Port != 2121 || sites[0].Host != "10.0.0.5" {
		t.Fatalf("port suffix not parsed: %+v", sites[0])
	}

	if err := Add(FTPSite{Name: "lab-nas", Host: "x.example.test"}); err == nil {
		t.Fatal("duplicate name must be rejected")
	}

	upd := FTPSite{Name: "lab-nas-v2", Host: "nas.example.test", Port: 21, User: "ftpuser", Tags: []string{"lab", "nas"}}
	if err := Update("lab-nas", upd); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, ok := Find("lab-nas-v2")
	if !ok || got.User != "ftpuser" || len(got.Tags) != 2 {
		t.Fatalf("updated site wrong: %+v ok=%v", got, ok)
	}
	if _, ok := Find("lab-nas"); ok {
		t.Fatal("old name still resolvable after rename")
	}

	if err := Delete("lab-nas-v2"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := Find("lab-nas-v2"); ok {
		t.Fatal("site still present after delete")
	}
}

func TestSaveFilePermissions(t *testing.T) {
	dir := isolateConfig(t)
	if err := Add(FTPSite{Name: "a", Host: "b.example.test"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "ctty", "ftp.json"))
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("config perms = %o, want 600", perm)
	}
}

func TestAddrIPv6Bracketed(t *testing.T) {
	isolateConfig(t)
	if err := Add(FTPSite{Name: "v6", Host: "::1", User: "u"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	h, ok := Find("v6")
	if !ok {
		t.Fatal("saved site not found")
	}
	if got := h.Addr(); got != "[::1]:21" {
		t.Fatalf("Addr() = %q, want [::1]:21", got)
	}
}

func TestDefaultPort(t *testing.T) {
	s := DefaultSite()
	if s.Port != DefaultPort {
		t.Fatalf("DefaultSite port = %d", s.Port)
	}
}
