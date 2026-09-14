package ui

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/ftpconfig"
	"github.com/zsuroy/ctty/internal/serialconfig"
	"github.com/zsuroy/ctty/internal/telnetconfig"
)

// copyToClipboard copies text to the system clipboard using the standard OSC 52 escape sequence.
// This works seamlessly across local terminals, SSH jump hosts, tmux sessions, and remote servers.
func copyToClipboard(text string) {
	b64 := base64.StdEncoding.EncodeToString([]byte(text))
	var seq string
	if os.Getenv("TMUX") != "" {
		// Wrap in tmux DCS pass-through escape sequence
		seq = fmt.Sprintf("\x1bPtmux;\x1b\x1b]52;c;%s\x07\x1b\\", b64)
	} else {
		seq = fmt.Sprintf("\x1b]52;c;%s\x07", b64)
	}
	_, _ = os.Stdout.WriteString(seq)
}

// FormatSSHCommand formats a complete, runnable SSH command for the given host.
func FormatSSHCommand(host config.SSHHost, configFile string) string {
	var parts []string
	parts = append(parts, "ssh")
	if configFile != "" {
		parts = append(parts, "-F", configFile)
	}
	if host.Port != "" && host.Port != "22" {
		parts = append(parts, "-p", host.Port)
	}
	if host.Identity != "" {
		parts = append(parts, "-i", host.Identity)
	}
	if host.ProxyJump != "" {
		parts = append(parts, "-J", host.ProxyJump)
	}

	target := host.Name
	if host.Hostname != "" && host.Hostname != host.Name {
		if host.User != "" {
			target = fmt.Sprintf("%s@%s", host.User, host.Hostname)
		} else {
			target = host.Hostname
		}
	} else if host.User != "" {
		target = fmt.Sprintf("%s@%s", host.User, host.Name)
	}
	parts = append(parts, target)
	return strings.Join(parts, " ")
}

// FormatTelnetCommand formats a runnable telnet command for the given host.
func FormatTelnetCommand(h telnetconfig.TelnetHost) string {
	port := h.Port
	if port == 0 {
		port = 23
	}
	return fmt.Sprintf("telnet %s %d", h.Host, port)
}

// FormatSerialCommand formats a runnable serial command (using picocom) for the given device.
func FormatSerialCommand(dev serialconfig.SerialDevice) string {
	baud := dev.BaudRate
	if baud == 0 {
		baud = 115200
	}
	return fmt.Sprintf("picocom -b %d %s", baud, dev.Device)
}

// FormatFTPCommand formats a runnable ftp command for the given site.
func FormatFTPCommand(site ftpconfig.FTPSite) string {
	port := site.Port
	if port == 0 {
		port = 21
	}
	if site.User != "" {
		return fmt.Sprintf("ftp ftp://%s@%s:%d", site.User, site.Host, port)
	}
	return fmt.Sprintf("ftp ftp://%s:%d", site.Host, port)
}
