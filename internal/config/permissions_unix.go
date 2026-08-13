//go:build !windows

package config

import "os"

// SetSecureFilePermissions configures secure permissions on Unix systems
func SetSecureFilePermissions(filepath string) error {
	// Set file permissions to 0600 (owner read/write only)
	return os.Chmod(filepath, 0600)
}
