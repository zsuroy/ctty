//go:build windows

package config

import (
	"os"
)

// SetSecureFilePermissions configures secure permissions on Windows
func SetSecureFilePermissions(filepath string) error {
	// On Windows, file permissions work differently
	// We ensure the file is not read-only and has basic permissions
	info, err := os.Stat(filepath)
	if err != nil {
		return err
	}

	// Ensure the file is not read-only
	if info.Mode()&os.ModeType == 0 {
		return os.Chmod(filepath, 0600)
	}

	return nil
}
