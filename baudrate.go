package serial

type BaudRate int

func (b BaudRate) Int() int {
	return int(b)
}

const (
	Baud1200   BaudRate = 1200
	Baud2400   BaudRate = 2400
	Baud4800   BaudRate = 4800
	Baud9600   BaudRate = 9600
	Baud19200  BaudRate = 19200
	Baud38400  BaudRate = 38400
	Baud57600  BaudRate = 57600
	Baud115200 BaudRate = 115200
	Baud230400 BaudRate = 230400
	Baud460800 BaudRate = 460800
	Baud921600 BaudRate = 921600
)
