package serial

import (
	"fmt"
	"github.com/Station-Manager/types"
)

// ValidateConfig validates serial port configuration parameters
func ValidateConfig(cfg *types.SerialConfig) error {
	// Validate port name
	if cfg.PortName == "" {
		return fmt.Errorf("port name cannot be empty")
	}

	// Validate baud rate
	validBaudRates := []int{1200, 2400, 4800, 9600, 19200, 38400, 57600, 115200, 230400, 460800, 921600}
	if !isValidBaudRate(cfg.BaudRate, validBaudRates) {
		return fmt.Errorf("invalid baud rate %d, must be one of: %v", cfg.BaudRate, validBaudRates)
	}

	// Validate data bits
	if cfg.DataBits < 5 || cfg.DataBits > 8 {
		return fmt.Errorf("data bits must be 5-8, got: %d", cfg.DataBits)
	}

	// Validate parity
	validParity := []int{int(ParityNone), int(ParityOdd), int(ParityEven), int(ParityMark), int(ParitySpace)}
	if !containsInt(validParity, cfg.Parity) {
		return fmt.Errorf("invalid parity value: %d", cfg.Parity)
	}

	// Validate stop bits
	if cfg.StopBits != 0 && cfg.StopBits != 1 && cfg.StopBits != 1.5 && cfg.StopBits != 2 {
		return fmt.Errorf("stop bits must be 0, 1, 1.5, or 2, got: %.1f", cfg.StopBits)
	}

	// Validate timeouts
	if cfg.ReadTimeout < 0 {
		return fmt.Errorf("read timeout cannot be negative: %v", cfg.ReadTimeout)
	}
	if cfg.WriteTimeout < 0 {
		return fmt.Errorf("write timeout cannot be negative: %v", cfg.WriteTimeout)
	}

	// Validate metrics channel size
	if cfg.MetricsChannelSize < 0 {
		return fmt.Errorf("metrics channel size cannot be negative: %d", cfg.MetricsChannelSize)
	}
	if cfg.MetricsChannelSize > 10000 {
		return fmt.Errorf("metrics channel size too large (max 10000): %d", cfg.MetricsChannelSize)
	}

	return nil
}

func isValidBaudRate(rate int, valid []int) bool {
	for _, v := range valid {
		if rate == v {
			return true
		}
	}
	return false
}

func containsInt(slice []int, val int) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}
