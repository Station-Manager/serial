package serial

import "errors"

var (
	ErrClosed = errors.New("serial: port closed")
)
