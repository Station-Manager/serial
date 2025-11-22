package serial

import gobug "go.bug.st/serial"

type StopBits gobug.StopBits

func (sb StopBits) Get() gobug.StopBits {
	return gobug.StopBits(sb)
}

const (
	// StopBits1 represents 1 stop bit
	StopBits1 = StopBits(gobug.OneStopBit)
	// StopBits1Half represents 1.5 stop bits
	StopBits1Half = StopBits(gobug.OnePointFiveStopBits)
	// StopBits2 represents 2 stop bits
	StopBits2 = StopBits(gobug.TwoStopBits)
)
