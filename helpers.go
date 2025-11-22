package serial

import (
	"fmt"
	"strings"
)

func isPortAvailable(portName string) (bool, error) {
	// Security: Prevent path traversal attacks
	if strings.Contains(portName, "..") {
		return false, fmt.Errorf("invalid port name: contains path traversal")
	}

	// Security: Reject paths that don't look like serial ports
	// On Unix: /dev/ttyXXX or /dev/cuXXX
	// On Windows: COMX
	if !isValidPortPattern(portName) {
		return false, fmt.Errorf("port name doesn't match expected pattern: %s", portName)
	}

	ports, err := AvailablePorts()
	if err != nil {
		return false, err
	}
	for _, port := range ports {
		if port == portName {
			return true, nil
		}
	}
	return false, nil
}

func isValidPortPattern(portName string) bool {
	// Windows: COM1-COM999 (must have at least one digit after COM)
	if strings.HasPrefix(portName, "COM") && len(portName) >= 4 && len(portName) <= 6 {
		return true
	}
	// Unix/Linux: /dev/tty* or /dev/cu* (macOS)
	if strings.HasPrefix(portName, "/dev/tty") || strings.HasPrefix(portName, "/dev/cu") {
		return true
	}
	return false
}
