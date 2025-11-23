package serial

import (
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"

	"go.bug.st/serial"
)

// validateConfig checks the configuration for obvious issues and returns a
// normalized copy with sensible defaults applied.
func validateConfig(cfg types.SerialConfig) (types.SerialConfig, error) {
	const op errors.Op = "serial.validateConfig"
	if cfg.PortName == "" {
		return cfg, errors.New(op).Msg("serial: missing port name")
	}
	if cfg.BaudRate <= 0 {
		return cfg, errors.New(op).Msgf("serial: invalid baud rate %d", cfg.BaudRate)
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
	return cfg, nil
}
