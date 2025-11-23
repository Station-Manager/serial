package serial

import (
	"context"
	"github.com/Station-Manager/types"
	"testing"
	"time"
)

// BenchmarkExec measures the cost of a typical Exec call against a mock port.
func BenchmarkExec(b *testing.B) {
	mp := newMockPort()
	cfg := types.SerialConfig{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		LineDelimiter: ';',
	}
	c := newPort(mp, cfg)

	// Pre-fill a response for each iteration.
	go func() {
		for i := 0; i < b.N; i++ {
			mp.readCh <- []byte("OK;")
		}
		close(mp.readCh)
	}()

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := c.Exec(ctx, "FA"); err != nil {
			b.Fatalf("Exec error: %v", err)
		}
	}
}

// BenchmarkReaderLoop focuses on the readerLoop by feeding many small chunks.
func BenchmarkReaderLoop(b *testing.B) {
	mp := newMockPort()
	cfg := types.SerialConfig{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		LineDelimiter: ';',
	}
	c := newPort(mp, cfg)

	// Feed data in another goroutine.
	go func() {
		for i := 0; i < b.N; i++ {
			mp.readCh <- []byte("RSP;")
		}
		close(mp.readCh)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.ReadResponse(ctx); err != nil {
			b.Fatalf("ReadResponse error: %v", err)
		}
	}
}
