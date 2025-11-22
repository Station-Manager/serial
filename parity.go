package serial

import (
	gobug "go.bug.st/serial"
)

type Parity gobug.Parity

func (pa Parity) Get() gobug.Parity {
	return gobug.Parity(pa)
}

const (
	// ParityNone represents no parity bit
	ParityNone = Parity(gobug.NoParity)
	// ParityOdd represents odd parity bit
	ParityOdd = Parity(gobug.OddParity)
	// ParityEven represents even parity bit
	ParityEven = Parity(gobug.EvenParity)
	// ParityMark represents mark parity bit (always 1)
	ParityMark = Parity(gobug.MarkParity)
	// ParitySpace represents space parity bit (always 0)
	ParitySpace = Parity(gobug.SpaceParity)
)
