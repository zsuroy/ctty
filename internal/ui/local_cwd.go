package ui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zsuroy/ctty/internal/i18n"
)

// defaultLocalUploadDir is the starting folder for SFTP upload browse:
// the process working directory (where ctty was launched), falling back to home.
func defaultLocalUploadDir() string {
	if wd, err := os.Getwd(); err == nil && wd != "" {
		return wd
	}
	home, _ := os.UserHomeDir()
	return home
}

// isLocalFilesystemRoot reports whether p is the top of a local volume
// (Unix "/", or a Windows drive root like `C:\`).
func isLocalFilesystemRoot(p string) bool {
	if p == "" {
		return false
	}
	if runtime.GOOS == "windows" {
		return isWindowsVolumeRoot(p)
	}
	return filepath.Clean(p) == string(filepath.Separator)
}

// isWindowsVolumeRoot reports whether p is a Windows drive root (`C:`, `C:\`, `C:/`).
// Pure string logic so tests can cover it on any GOOS.
func isWindowsVolumeRoot(p string) bool {
	n := strings.ReplaceAll(p, "/", `\`)
	if len(n) < 2 || n[1] != ':' {
		return false
	}
	if n[0] < 'A' || (n[0] > 'Z' && n[0] < 'a') || n[0] > 'z' {
		return false
	}
	rest := n[2:]
	return rest == "" || rest == `\`
}

// listLocalDrives returns available Windows drive roots (`C:\`, `D:\`, …).
// On non-Windows platforms it returns nil.
func listLocalDrives() []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	var drives []string
	for c := 'A'; c <= 'Z'; c++ {
		root := string(c) + `:\`
		f, err := os.Open(root)
		if err != nil {
			continue
		}
		_ = f.Close()
		drives = append(drives, root)
	}
	return drives
}

// localBrowseLabel is the path (or drives header) shown in the upload UI.
func localBrowseLabel(cwd string, showingDrives bool) string {
	if showingDrives {
		return i18n.T("sftp.local_drives")
	}
	return cwd
}
