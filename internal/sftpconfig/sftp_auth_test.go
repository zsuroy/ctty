package sftpconfig

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
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

	_, _, err := getAuthMethods(effectiveHost{
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

	methods, _, err := getAuthMethods(effectiveHost{identityFiles: []string{keyPath}}, "")
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

	_, _, err := getAuthMethods(effectiveHost{
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

	methods, _, err := getAuthMethods(effectiveHost{
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

	_, _, err := getAuthMethods(effectiveHost{
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

	_, _, err := getAuthMethods(effectiveHost{
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

	methods, _, err := getAuthMethods(effectiveHost{
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

type closeTrackingConn struct {
	net.Conn
	closes atomic.Int32
}

func (c *closeTrackingConn) Close() error {
	c.closes.Add(1)
	return c.Conn.Close()
}

func newTestAgentConn(t *testing.T, keyring agent.Agent) *closeTrackingConn {
	t.Helper()
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = agent.ServeAgent(keyring, server)
		_ = server.Close()
	}()
	conn := &closeTrackingConn{Conn: client}
	t.Cleanup(func() {
		_ = client.Close()
		<-done
	})
	return conn
}

func testAgentKeyring(t *testing.T) agent.Agent {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatalf("add test key to agent: %v", err)
	}
	return keyring
}

func TestGetAuthMethodsAgentSignersStayOpenUntilRelease(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/tmp/ctty-test-agent.sock")
	conn := newTestAgentConn(t, testAgentKeyring(t))
	origDial := dialAgent
	t.Cleanup(func() { dialAgent = origDial })
	dialAgent = func(string) (net.Conn, error) { return conn, nil }

	methods, releaseAgent, err := getAuthMethods(effectiveHost{
		identityAgent: "SSH_AUTH_SOCK",
		identityFiles: []string{filepath.Join(t.TempDir(), "absent")},
	}, "")
	if err != nil {
		t.Fatalf("getAuthMethods error: %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("expected one agent auth method, got %d", len(methods))
	}
	if releaseAgent == nil {
		t.Fatal("releaseAgent must not be nil while agent signers are in use")
	}
	if got := conn.closes.Load(); got != 0 {
		t.Fatalf("agent conn closed %d times before ssh.Dial; signers need it open", got)
	}
	releaseAgent()
	if got := conn.closes.Load(); got != 1 {
		t.Fatalf("releaseAgent closed agent conn %d times, want 1", got)
	}
	releaseAgent()
	if got := conn.closes.Load(); got != 1 {
		t.Fatalf("releaseAgent must be idempotent, closes = %d", got)
	}
}

func TestGetAuthMethodsClosesAgentWithoutUsableSigners(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/tmp/ctty-test-agent.sock")
	conn := newTestAgentConn(t, agent.NewKeyring())
	origDial := dialAgent
	t.Cleanup(func() { dialAgent = origDial })
	dialAgent = func(string) (net.Conn, error) { return conn, nil }

	methods, releaseAgent, err := getAuthMethods(effectiveHost{
		identityAgent: "SSH_AUTH_SOCK",
		identityFiles: []string{filepath.Join(t.TempDir(), "absent")},
	}, "")
	if err != nil {
		t.Fatalf("getAuthMethods error: %v", err)
	}
	if len(methods) != 0 {
		t.Fatalf("expected no auth methods, got %d", len(methods))
	}
	if got := conn.closes.Load(); got != 1 {
		t.Fatalf("agent conn without signers should be closed immediately, closes = %d", got)
	}
	if releaseAgent != nil {
		t.Fatal("no agent connection should remain to release")
	}
}

func stubConnectAuth(t *testing.T, conn *closeTrackingConn) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SSH_AUTH_SOCK", "/tmp/ctty-test-agent.sock")

	origAgentDial := dialAgent
	t.Cleanup(func() { dialAgent = origAgentDial })
	dialAgent = func(string) (net.Conn, error) { return conn, nil }

	origSSHG := runSSHG
	t.Cleanup(func() { runSSHG = origSSHG })
	runSSHG = func(string, string) ([]byte, error) {
		return []byte("hostname 127.0.0.1\nport 22\n"), nil
	}
}

func TestConnectWithAuthReleasesAgentAfterDial(t *testing.T) {
	conn := newTestAgentConn(t, testAgentKeyring(t))
	stubConnectAuth(t, conn)

	origDial := sshDial
	t.Cleanup(func() { sshDial = origDial })
	sshDial = func(string, string, *ssh.ClientConfig) (*ssh.Client, error) {
		return &ssh.Client{}, nil
	}

	client, err := ConnectWithAuth("ctty-test-host", "", "", "")
	if err != nil {
		t.Fatalf("ConnectWithAuth error: %v", err)
	}
	if client == nil {
		t.Fatal("expected a client")
	}
	if got := conn.closes.Load(); got != 1 {
		t.Fatalf("agent conn must be released once dial returns, closes = %d", got)
	}
}

func TestConnectWithAuthReleasesAgentWhenDialFails(t *testing.T) {
	conn := newTestAgentConn(t, testAgentKeyring(t))
	stubConnectAuth(t, conn)

	dialErr := errors.New("dial refused")
	origDial := sshDial
	t.Cleanup(func() { sshDial = origDial })
	sshDial = func(string, string, *ssh.ClientConfig) (*ssh.Client, error) {
		return nil, dialErr
	}

	if _, err := ConnectWithAuth("ctty-test-host", "", "", ""); !errors.Is(err, dialErr) {
		t.Fatalf("expected dial error, got %v", err)
	}
	if got := conn.closes.Load(); got != 1 {
		t.Fatalf("agent conn must be released when dial fails, closes = %d", got)
	}
}
