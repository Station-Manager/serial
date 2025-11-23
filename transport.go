package serial

import (
	"time"

	"go.bug.st/serial"
)

// SerialPort abstracts the subset of go.bug.st/serial.Port used by this package.
type SerialPort interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Close() error
	SetReadTimeout(d time.Duration) error
}

// bugstPort wraps the concrete serial.Port to satisfy SerialPort.
type bugstPort struct {
	serial.Port
}
