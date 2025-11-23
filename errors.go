package serial

import "errors"

var (
	ErrClosed = errors.New("serial: port closed")
	//	ErrLineTooLong = errors.New("serial: response too long")
)
