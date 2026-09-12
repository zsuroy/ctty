package ftpcred

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zsuroy/ctty/internal/credential"
)

func TestSetGetDeletePassword(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	credential.ResetStoreForTest()
	t.Cleanup(credential.ResetStoreForTest)

	if _, ok := GetPassword("lab-nas"); ok {
		t.Fatal("expected miss")
	}
	if err := SetPassword("lab-nas", "s3cret"); err != nil {
		t.Fatal(err)
	}
	got, ok := GetPassword("lab-nas")
	if !ok || got != "s3cret" {
		t.Fatalf("got %q ok=%v", got, ok)
	}

	vaultPath := filepath.Join(dir, "ctty", "credentials.json")
	info, err := os.Stat(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("perms %o", info.Mode().Perm())
	}
	raw, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "s3cret") {
		t.Fatal("vault must not contain cleartext password")
	}
	if _, err := os.Stat(filepath.Join(dir, "ctty", "ftp-credentials.json")); !os.IsNotExist(err) {
		t.Fatal("legacy cleartext store must not be created")
	}

	// SSH namespace stays untouched: same bare name must miss.
	if _, ok := credential.GetPassword("lab-nas"); ok {
		t.Fatal("FTP entry must not leak into SSH namespace")
	}

	if err := DeletePassword("lab-nas"); err != nil {
		t.Fatal(err)
	}
	if _, ok := GetPassword("lab-nas"); ok {
		t.Fatal("expected deleted")
	}
}
