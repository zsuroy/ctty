//go:build windows

package sftpconfig

import "testing"

func TestDefaultSSHAgentAddrWindows(t *testing.T) {
	if got := defaultSSHAgentAddr(); got != windowsOpenSSHAgentPipe {
		t.Fatalf("defaultSSHAgentAddr() = %q, want %q", got, windowsOpenSSHAgentPipe)
	}
}

func TestIsWindowsNamedPipe(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{`\\.\pipe\openssh-ssh-agent`, true},
		{`//./pipe/openssh-ssh-agent`, true},
		{`\\.\Pipe\OpenSSH-SSH-Agent`, true},
		{`/tmp/ssh-agent.sock`, false},
		{"", false},
	}
	for _, tc := range tests {
		if got := isWindowsNamedPipe(tc.addr); got != tc.want {
			t.Errorf("isWindowsNamedPipe(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

func TestNormalizeWindowsNamedPipe(t *testing.T) {
	got := normalizeWindowsNamedPipe(`//./pipe/openssh-ssh-agent`)
	if got != `\\.\pipe\openssh-ssh-agent` {
		t.Fatalf("normalizeWindowsNamedPipe = %q", got)
	}
}

func TestAgentAddrFallsBackToNamedPipe(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	if got := agentAddr(""); got != windowsOpenSSHAgentPipe {
		t.Fatalf("agentAddr(\"\") = %q, want %q", got, windowsOpenSSHAgentPipe)
	}
	if got := agentAddr("none"); got != "" {
		t.Fatalf("agentAddr(\"none\") = %q, want empty", got)
	}
}
