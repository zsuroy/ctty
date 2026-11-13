//go:build windows

package sftpconfig

import (
	"net"
	"strings"

	"github.com/Microsoft/go-winio"
)

// Default OpenSSH for Windows agent pipe. WinCrypt and other providers
// that inject into this agent are reached through the same path.
const windowsOpenSSHAgentPipe = `\\.\pipe\openssh-ssh-agent`

func defaultSSHAgentAddr() string {
	return windowsOpenSSHAgentPipe
}

func dialSSHAgent(addr string) (net.Conn, error) {
	if isWindowsNamedPipe(addr) {
		return winio.DialPipe(normalizeWindowsNamedPipe(addr), nil)
	}
	// Some environments (Cygwin, MSYS, WSL interop) still expose a Unix socket.
	if conn, err := net.Dial("unix", addr); err == nil {
		return conn, nil
	}
	return winio.DialPipe(normalizeWindowsNamedPipe(addr), nil)
}

func isWindowsNamedPipe(addr string) bool {
	s := strings.ReplaceAll(addr, "/", `\`)
	return strings.Contains(strings.ToLower(s), `\pipe\`)
}

func normalizeWindowsNamedPipe(addr string) string {
	s := strings.ReplaceAll(addr, "/", `\`)
	if strings.HasPrefix(s, `\.\pipe\`) && !strings.HasPrefix(s, `\\.\pipe\`) {
		// leading backslash may have been lost by path cleaning
		s = `\` + s
	}
	return s
}
