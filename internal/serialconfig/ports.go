package serialconfig

import (
	"sort"
	"strings"

	"go.bug.st/serial"
)

// AvailablePorts returns a list of serial port paths currently available
// on the system (e.g. /dev/cu.usbserial-1420 on macOS, COM3 on Windows).
// Returns an empty list if none are found.
func AvailablePorts() []string {
	ports, err := serial.GetPortsList()
	if err != nil {
		return nil
	}
	// Filter out empty names
	var result []string
	for _, p := range ports {
		if p != "" {
			result = append(result, p)
		}
	}
	sort.Strings(result)
	return result
}

// ParityFromString converts a string like "none"/"even"/"odd" to the
// serial.Parity constant.
func ParityFromString(s string) serial.Parity {
	switch strings.ToLower(s) {
	case "even":
		return serial.EvenParity
	case "odd":
		return serial.OddParity
	default:
		return serial.NoParity
	}
}

// StopBitsFromString converts 1 or 2 to the serial.StopBits constant.
func StopBitsFromString(n int) serial.StopBits {
	if n == 2 {
		return serial.TwoStopBits
	}
	return serial.OneStopBit
}
