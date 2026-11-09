// Package telnetcred stores telnet device passwords in the shared encrypted
// credential vault (~/.config/ctty/credentials.json, AES-256-GCM, mode 0600).
//
// Keys are namespaced as "telnet:<device>" so telnet entries never collide
// with SSH host entries and never surface in SSH_ASKPASS lookups.
package telnetcred

import (
	"github.com/zsuroy/ctty/internal/credential"
)

// keyPrefix namespaces telnet entries inside the shared vault.
const keyPrefix = "telnet:"

// vaultKey maps a device name to its vault entry name.
func vaultKey(deviceName string) string {
	return keyPrefix + deviceName
}

// GetPassword returns the stored telnet password for a device name.
func GetPassword(deviceName string) (string, bool) {
	return credential.GetPassword(vaultKey(deviceName))
}

// SetPassword stores (or updates) the telnet password for a device.
// An empty password deletes the entry.
func SetPassword(deviceName, password string) error {
	return credential.SetPassword(vaultKey(deviceName), password)
}

// DeletePassword removes the stored password for a device.
func DeletePassword(deviceName string) error {
	return credential.DeletePassword(vaultKey(deviceName))
}
