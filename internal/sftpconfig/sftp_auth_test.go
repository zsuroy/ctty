package sftpconfig

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"~", home},
		{"~/", home},
		{"~/.ssh/id_rsa", filepath.Join(home, ".ssh", "id_rsa")},
		{`"~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock"`, filepath.Join(home, "Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock")},
		{`'/custom/path'`, "/custom/path"},
		{"/var/run/agent.sock", "/var/run/agent.sock"},
	}

	for _, tc := range tests {
		got := expandPath(tc.input)
		if got != tc.expected {
			t.Errorf("expandPath(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestGetAuthMethodsIdentityAgentNone(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/tmp/dummy-ssh-auth.sock")

	_, err := getAuthMethods(effectiveHost{
		name:          "alpha.example.test",
		identityAgent: "none",
	}, "")
	if err != nil {
		t.Fatalf("getAuthMethods() error: %v", err)
	}
}

func TestParseSSHGOutput(t *testing.T) {
	out := "hostname 10.0.0.5\nuser alice\nport 2222\nidentityagent none\nidentityfile ~/.ssh/id_ed25519\nidentityfile ~/.ssh/id_rsa\n"
	r := parseSSHGOutput(out)
	if r.hostname != "10.0.0.5" || r.user != "alice" || r.port != "2222" {
		t.Fatalf("basic fields: %+v", r)
	}
	if r.identityAgent != "none" || !r.identityAgentSet {
		t.Fatalf("identityagent: %+v", r)
	}
	if len(r.identityFiles) != 2 || r.identityFiles[0] != "~/.ssh/id_ed25519" || r.identityFiles[1] != "~/.ssh/id_rsa" {
		t.Fatalf("identityfiles: %+v", r.identityFiles)
	}
}

func TestResolveEffectiveHostUsesSSHG(t *testing.T) {
	orig := runSSHG
	t.Cleanup(func() { runSSHG = orig })
	runSSHG = func(hostName, configFile string) ([]byte, error) {
		return []byte("hostname 1.2.3.4\nuser bob\nport 2200\nidentityfile /keys/from-wildcard\nidentityfile ~/.ssh/id_ed25519\n"), nil
	}
	eff := resolveEffectiveHost("myhost", "")
	if eff.hostname != "1.2.3.4" || eff.user != "bob" || eff.port != "2200" {
		t.Fatalf("eff = %+v", eff)
	}
	if len(eff.identityFiles) != 2 || eff.identityFiles[0] != "/keys/from-wildcard" {
		t.Fatalf("identityFiles = %v", eff.identityFiles)
	}
}

func TestResolveEffectiveHostFallsBackOnSSHGError(t *testing.T) {
	orig := runSSHG
	t.Cleanup(func() { runSSHG = orig })
	runSSHG = func(hostName, configFile string) ([]byte, error) {
		return nil, os.ErrNotExist
	}
	// unknown host, no parsed config match -> still returns defaults (hostname=name)
	eff := resolveEffectiveHost("missing-host-xyz", "")
	if eff.hostname != "missing-host-xyz" || eff.port != "22" {
		t.Fatalf("fallback eff = %+v", eff)
	}
}

func TestGetAuthMethodsLoadsIdentityFile(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_test")
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatal(err)
	}

	methods, err := getAuthMethods(effectiveHost{identityFiles: []string{keyPath}}, "")
	if err != nil {
		t.Fatalf("getAuthMethods error: %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("expected 1 auth method for a loadable key, got %d", len(methods))
	}
}

func writeEd25519Key(t *testing.T, path, passphrase string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestGetAuthMethodsEncryptedKeyRequiresPassphrase(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_alpha")
	writeEd25519Key(t, keyPath, "correct-horse")

	_, err := getAuthMethods(effectiveHost{
		name:          "alpha.example.test",
		identityAgent: "none",
		identityFiles: []string{keyPath},
	}, "")
	if !IsPassphraseRequired(err) {
		t.Fatalf("expected PassphraseRequiredError, got %v", err)
	}
	var pe *PassphraseRequiredError
	if !errors.As(err, &pe) || pe.Path != keyPath {
		t.Fatalf("PassphraseRequiredError.Path = %#v, want %q", pe, keyPath)
	}
}

func TestGetAuthMethodsEncryptedKeyWithPassphrase(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_alpha")
	writeEd25519Key(t, keyPath, "correct-horse")

	methods, err := getAuthMethods(effectiveHost{
		name:          "alpha.example.test",
		identityAgent: "none",
		identityFiles: []string{keyPath},
	}, "correct-horse")
	if err != nil {
		t.Fatalf("getAuthMethods error: %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("expected 1 auth method, got %d", len(methods))
	}
}

func TestGetAuthMethodsWrongPassphrase(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_alpha")
	writeEd25519Key(t, keyPath, "correct-horse")

	_, err := getAuthMethods(effectiveHost{
		name:          "alpha.example.test",
		identityAgent: "none",
		identityFiles: []string{keyPath},
	}, "wrong-pass")
	if err == nil {
		t.Fatal("expected error for wrong passphrase")
	}
	if IsPassphraseRequired(err) {
		t.Fatalf("wrong passphrase should not be PassphraseRequiredError: %v", err)
	}
	if !strings.Contains(err.Error(), keyPath) {
		t.Fatalf("error should name the key file, got: %v", err)
	}
}

func TestGetAuthMethodsInvalidKeySurfacesError(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_alpha")
	if err := os.WriteFile(keyPath, []byte("not a key"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := getAuthMethods(effectiveHost{
		name:          "alpha.example.test",
		identityAgent: "none",
		identityFiles: []string{keyPath},
	}, "")
	if err == nil {
		t.Fatal("expected parse error for invalid key")
	}
	if IsPassphraseRequired(err) {
		t.Fatalf("invalid key should not be PassphraseRequiredError: %v", err)
	}
	if !strings.Contains(err.Error(), keyPath) {
		t.Fatalf("error should name the key file, got: %v", err)
	}
}

func TestGetAuthMethodsMissingIdentityFileSkipped(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist")
	good := filepath.Join(dir, "id_alpha")
	writeEd25519Key(t, good, "")

	methods, err := getAuthMethods(effectiveHost{
		name:          "alpha.example.test",
		identityAgent: "none",
		identityFiles: []string{missing, good},
	}, "")
	if err != nil {
		t.Fatalf("missing files should be skipped: %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("expected 1 auth method, got %d", len(methods))
	}
}

func TestAgentAddr(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/tmp/env-agent.sock")
	if got := agentAddr("none"); got != "" {
		t.Fatalf("none: got %q", got)
	}
	if got := agentAddr(""); got != "/tmp/env-agent.sock" {
		t.Fatalf("empty: got %q", got)
	}
	if got := agentAddr("SSH_AUTH_SOCK"); got != "/tmp/env-agent.sock" {
		t.Fatalf("SSH_AUTH_SOCK: got %q", got)
	}
	if got := agentAddr("/custom/agent.sock"); got != "/custom/agent.sock" {
		t.Fatalf("explicit: got %q", got)
	}
}

func TestIsPassphraseRequired(t *testing.T) {
	if IsPassphraseRequired(nil) || IsPassphraseRequired(os.ErrNotExist) {
		t.Fatal("nil / unrelated errors must not match")
	}
	err := &PassphraseRequiredError{Path: "/keys/id_alpha"}
	if !IsPassphraseRequired(err) || !IsPassphraseRequired(errors.Join(err)) {
		t.Fatal("expected match for PassphraseRequiredError")
	}
}
