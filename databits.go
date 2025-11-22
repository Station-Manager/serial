package serial

type DataBits int

func (d DataBits) Int() int {
	return int(d)
}

const (
	DataBits5 DataBits = 5
	DataBits6 DataBits = 6
	DataBits7 DataBits = 7
	DataBits8 DataBits = 8
)
