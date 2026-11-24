package ui

import (
	"testing"

	"github.com/zsuroy/ctty/internal/serialconfig"
)

func TestSerialAddFormPersistsConnectMode(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	f := newSerialAddForm(NewStyles(80), 80, 24, []string{"COM7"})
	f.nameVal = "Board"
	f.modeVal = serialconfig.ModeRaw
	f.submit()
	if f.err != "" {
		t.Fatalf("submit failed: %s", f.err)
	}
	dev, ok := serialconfig.Find("Board")
	if !ok {
		t.Fatal("device was not saved")
	}
	if dev.Mode != serialconfig.ModeRaw {
		t.Fatalf("saved mode = %q, want %q", dev.Mode, serialconfig.ModeRaw)
	}
}

func TestSerialAddFormDefaultsToInherit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	f := newSerialAddForm(NewStyles(80), 80, 24, []string{"COM7"})
	if f.modeVal != serialconfig.ModeInherit {
		t.Fatalf("default mode = %q, want inherit", f.modeVal)
	}
	f.nameVal = "Plain"
	f.submit()
	dev, ok := serialconfig.Find("Plain")
	if !ok || dev.Mode != serialconfig.ModeInherit {
		t.Fatalf("mode = %q, want inherit (empty)", dev.Mode)
	}
}
