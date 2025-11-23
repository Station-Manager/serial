package serial

import (
	"fmt"
	"github.com/Station-Manager/types"

	"go.bug.st/serial"
)

// validate checks the configuration for obvious issues.
func validateConfig(cfg types.SerialConfig) error {
	if cfg.PortName == "" {
		return fmt.Errorf("serial: missing port name")
	}
	if cfg.BaudRate <= 0 {
		return fmt.Errorf("serial: invalid baud rate %d", cfg.BaudRate)
	}
	if cfg.DataBits == 0 {
		cfg.DataBits = 8
	}
	if cfg.StopBits == 0 {
		cfg.StopBits = serial.OneStopBit
	}
	// parity is left as-is; zero value is ParityNone.
	if cfg.Parity == 0 {
		cfg.Parity = serial.NoParity
	}
	return nil
}
