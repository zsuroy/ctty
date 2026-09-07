package ui

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestIsWindowsVolumeRoot(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{`C:\`, true},
		{`C:/`, true},
		{`C:`, true},
		{`c:\`, true},
		{`C:\Users`, false},
		{`C:\Users\`, false},
		{`\\server\share`, false},
		{`/`, false},
		{``, false},
	}
	for _, tc := range cases {
		if got := isWindowsVolumeRoot(tc.path); got != tc.want {
			t.Fatalf("isWindowsVolumeRoot(%q)=%v want %v", tc.path, got, tc.want)
		}
	}
}

func TestIsLocalFilesystemRootUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix root semantics")
	}
	if !isLocalFilesystemRoot("/") {
		t.Fatal("expected / to be root")
	}
	if isLocalFilesystemRoot("/home") {
		t.Fatal("/home should not be root")
	}
	if isLocalFilesystemRoot("") {
		t.Fatal("empty should not be root")
	}
}

func TestDefaultLocalUploadDirNonEmpty(t *testing.T) {
	got := defaultLocalUploadDir()
	if got == "" {
		t.Fatal("defaultLocalUploadDir empty")
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("expected absolute path, got %q", got)
	}
}

func TestListLocalDrivesNonWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows has drives")
	}
	if got := listLocalDrives(); got != nil {
		t.Fatalf("expected nil drives on non-windows, got %v", got)
	}
}
