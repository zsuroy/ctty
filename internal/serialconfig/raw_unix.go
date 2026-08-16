//go:build !windows

package serialconfig

import (
	"os"
	"syscall"
	"unsafe"
)

// termios constants for raw mode (POSIX)
const (
	// ioctl request numbers
	TCGETS = 0x5401 // Linux
	TCSETS = 0x5402 // Linux
	// macOS uses different numbers, handle via TIOCGETA/TIOCSETA below
)

// On macOS, the ioctl numbers are different; we use the TIOC variants.
const (
	IOC_NRMAS = 'T'
	TIOCGETA  = 0x40487413 // macOS: get termios
	TIOCSETA  = 0x80487414 // macOS: set termios
)

type termios struct {
	IFlag  uint32
	OFlag  uint32
	CFlag  uint32
	LFlag  uint32
	CC     [19]byte
	ISpeed uint32
	OSpeed uint32
}

func setRawStdin() (*termios, error) {
	fd := os.Stdin.Fd()

	// Get current termios
	var old termios
	if err := tcgetattr(int(fd), &old); err != nil {
		return nil, err
	}

	// Copy and modify to raw
	raw := old
	raw.IFlag &^= 0x00000002 // IGNPAR
	raw.IFlag &^= 0x00000080 // ISTRIP
	raw.IFlag &^= 0x00000400 // IXON
	raw.OFlag &^= 0x00000001 // OPOST
	raw.LFlag &^= 0x00000001 // ECHO
	raw.LFlag &^= 0x00000002 // ECHOE
	raw.LFlag &^= 0x00000004 // ECHOK
	raw.LFlag &^= 0x00000008 // ECHONL
	raw.LFlag &^= 0x00000010 // NOFLSH
	raw.LFlag &^= 0x00000040 // ICANON
	raw.LFlag &^= 0x00000080 // IEXTEN
	raw.LFlag &^= 0x00000200 // ISIG
	raw.CFlag &^= 0x00000040 // PARENB
	raw.CFlag &^= 0x00000100 // CSIZE
	raw.CFlag |= 0x00000300  // CS8
	raw.CFlag &^= 0x00000400 // CSTOPB (1 stop bit)

	if err := tcsetattr(int(fd), &raw); err != nil {
		return nil, err
	}
	return &old, nil
}

func restoreStdin(old *termios) {
	if old != nil {
		tcsetattr(int(os.Stdin.Fd()), old)
	}
}

func tcgetattr(fd int, t *termios) error {
	_, _, errno := syscall.Syscall6(
		syscall.SYS_IOCTL,
		uintptr(fd),
		uintptr(TIOCGETA),
		uintptr(unsafe.Pointer(t)),
		0, 0, 0,
	)
	if errno != 0 {
		return errno
	}
	return nil
}

func tcsetattr(fd int, t *termios) error {
	_, _, errno := syscall.Syscall6(
		syscall.SYS_IOCTL,
		uintptr(fd),
		uintptr(TIOCSETA),
		uintptr(unsafe.Pointer(t)),
		0, 0, 0,
	)
	if errno != 0 {
		return errno
	}
	return nil
}
