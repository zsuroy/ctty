package sftpconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zsuroy/ctty/internal/config"
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

	host := &config.SSHHost{
		Name:          "test-host",
		IdentityAgent: "none",
	}

	_, err := getAuthMethods(host)
	if err != nil {
		t.Fatalf("getAuthMethods() error: %v", err)
	}
}
