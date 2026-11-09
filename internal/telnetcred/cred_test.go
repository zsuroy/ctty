package telnetcred

import (
	"os"
	"testing"

	"github.com/zsuroy/ctty/internal/credential"
)

// isolateVault points the shared credential vault at a temp dir for the
// duration of the test, mirroring ftpcred's tests.
func isolateVault(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	originalXDG := os.Getenv("XDG_CONFIG_HOME")
	originalHome := os.Getenv("HOME")
	os.Setenv("XDG_CONFIG_HOME", dir)
	os.Setenv("HOME", dir)
	credential.ResetStoreForTest()
	t.Cleanup(func() {
		os.Setenv("XDG_CONFIG_HOME", originalXDG)
		os.Setenv("HOME", originalHome)
		credential.ResetStoreForTest()
	})
}

func TestPasswordRoundTrip(t *testing.T) {
	isolateVault(t)

	if _, ok := GetPassword("rpi"); ok {
		t.Fatal("expected no stored password for a fresh vault")
	}
	if err := SetPassword("rpi", "s3cret"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	pass, ok := GetPassword("rpi")
	if !ok || pass != "s3cret" {
		t.Fatalf("GetPassword = %q,%v want %q,true", pass, ok, "s3cret")
	}

	// An empty password deletes the entry (the form's "clear" path).
	if err := SetPassword("rpi", ""); err != nil {
		t.Fatalf("SetPassword(\"\"): %v", err)
	}
	if _, ok := GetPassword("rpi"); ok {
		t.Fatal("expected the entry to be deleted after an empty password")
	}
}

// TestNamespaceIsolation guards the "telnet:" prefix: an SSH host entry of
// the same name must never satisfy a telnet lookup (and vice versa).
func TestNamespaceIsolation(t *testing.T) {
	isolateVault(t)

	if err := credential.SetPassword("rpi", "ssh-pass"); err != nil {
		t.Fatalf("seed ssh entry: %v", err)
	}
	if _, ok := GetPassword("rpi"); ok {
		t.Fatal("telnet lookup leaked an SSH vault entry")
	}
	if err := SetPassword("rpi", "telnet-pass"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	ssh, ok := credential.GetPassword("rpi")
	if !ok || ssh != "ssh-pass" {
		t.Fatalf("ssh entry = %q,%v want %q,true", ssh, ok, "ssh-pass")
	}
	telnet, ok := GetPassword("rpi")
	if !ok || telnet != "telnet-pass" {
		t.Fatalf("telnet entry = %q,%v want %q,true", telnet, ok, "telnet-pass")
	}
}

func TestDeletePasswordMissing(t *testing.T) {
	isolateVault(t)
	if err := DeletePassword("never-saved"); err != nil {
		t.Fatalf("DeletePassword on a missing entry: %v", err)
	}
}
