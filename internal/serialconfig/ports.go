package serialconfig

import (
	"sort"
	"strconv"
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

// ParityFromString converts "none"/"even"/"odd"/"mark"/"space" to the
// serial.Parity constant.
func ParityFromString(s string) serial.Parity {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "odd":
		return serial.OddParity
	case "even":
		return serial.EvenParity
	case "mark":
		return serial.MarkParity
	case "space":
		return serial.SpaceParity
	default:
		return serial.NoParity
	}
}

// StopBitsFromString converts 1, 1.5 or 2 to the serial.StopBits constant.
func StopBitsFromString(n float64) serial.StopBits {
	if n >= 1.75 {
		return serial.TwoStopBits
	}
	if n >= 1.25 {
		return serial.OnePointFiveStopBits
	}
	return serial.OneStopBit
}

// FormatStopBits renders a stored stop-bit value without a trailing .0.
func FormatStopBits(n float64) string {
	return strconv.FormatFloat(n, 'g', -1, 64)
}
