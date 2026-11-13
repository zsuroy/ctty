//go:build !windows

package sftpconfig

import "net"

// defaultSSHAgentAddr returns the platform default agent address when
// IdentityAgent and SSH_AUTH_SOCK are both unset. On Unix there is no
// implicit socket; the caller already consulted SSH_AUTH_SOCK.
func defaultSSHAgentAddr() string {
	return ""
}

func dialSSHAgent(addr string) (net.Conn, error) {
	return net.Dial("unix", addr)
}
