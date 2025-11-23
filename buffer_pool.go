package serial

import "sync"

const (
	defaultBufSize = 512
	maxLineSize    = 4096
)

var readBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, defaultBufSize)
		return b
	},
}

func getReadBuf() []byte {
	return readBufPool.Get().([]byte)
}

func putReadBuf(b []byte) {
	if cap(b) != defaultBufSize {
		return
	}
	readBufPool.Put(b[:defaultBufSize])
}
