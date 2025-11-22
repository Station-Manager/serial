package serial

import "errors"

var (
	ErrNotInitialized             = errors.New("serial port not initialized")
	ErrInvalidPortName            = errors.New("invalid port name")
	ErrPortNotOpen                = errors.New("port not open")
	ErrInvalidBuffer              = errors.New("invalid buffer: nil or empty")
	ErrBufferTooLarge             = errors.New("buffer exceeds maximum allowed size")
	ErrBufferExceedsAbsoluteLimit = errors.New("buffer size exceeds absolute maximum limit")
	ErrWriteTimeout               = errors.New("write operation timed out")
	ErrWriteQueueNotInitialized   = errors.New("write queue not initialized")
)
