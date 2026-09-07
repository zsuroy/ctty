// sftp_hostkey_test.go covers trust-on-first-use host key persistence for SFTP.
package sftpconfig

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func testPublicKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestAcceptNewHostKeyCallbackRecordsAndVerifies(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".ssh", "known_hosts")
	callback, err := acceptNewHostKeyCallback(path)
	if err != nil {
		t.Fatal(err)
	}
	hostname := "example.test:22"
	remote := &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 22}
	key := testPublicKey(t)

	if err := callback(hostname, remote, key); err != nil {
		t.Fatalf("first connection should record the key: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), knownhosts.Normalize(hostname)) {
		t.Fatalf("known_hosts entry %q does not contain %q", data, knownhosts.Normalize(hostname))
	}
	if !bytes.Contains(data, ssh.MarshalAuthorizedKey(key)) {
		t.Fatalf("known_hosts entry does not contain the recorded key")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("known_hosts permissions = %o, want 600", got)
	}

	reloaded, err := acceptNewHostKeyCallback(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := reloaded(hostname, remote, key); err != nil {
		t.Fatalf("recorded key should be accepted: %v", err)
	}
}

func TestAcceptNewHostKeyCallbackRejectsChangedKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	callback, err := acceptNewHostKeyCallback(path)
	if err != nil {
		t.Fatal(err)
	}
	hostname := "[example.test]:2222"
	remote := &net.TCPAddr{IP: net.ParseIP("192.0.2.11"), Port: 2222}
	if err := callback(hostname, remote, testPublicKey(t)); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	reloaded, err := acceptNewHostKeyCallback(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := reloaded(hostname, remote, testPublicKey(t)); err == nil {
		t.Fatal("changed host key should be rejected")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("changed host key must not be appended")
	}
}

func TestAcceptNewHostKeyCallbackRejectsMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte("not a known_hosts entry\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := acceptNewHostKeyCallback(path); err == nil {
		t.Fatal("malformed known_hosts file should fail closed")
	}
}
