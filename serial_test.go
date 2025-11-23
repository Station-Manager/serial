package serial

import (
	"context"
	"github.com/Station-Manager/types"
	"sync"
	"testing"
	"time"
)

type mockPort struct {
	readCh  chan []byte
	writeMu sync.Mutex
	writes  [][]byte
	closed  bool
}

func newMockPort() *mockPort {
	return &mockPort{readCh: make(chan []byte, 16)}
}

func (m *mockPort) Read(p []byte) (int, error) {
	b, ok := <-m.readCh
	if !ok {
		return 0, context.Canceled
	}
	n := copy(p, b)
	return n, nil
}

func (m *mockPort) Write(p []byte) (int, error) {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	cp := make([]byte, len(p))
	copy(cp, p)
	m.writes = append(m.writes, cp)
	return len(p), nil
}

func (m *mockPort) Close() error {
	if !m.closed {
		close(m.readCh)
		m.closed = true
	}
	return nil
}

func (m *mockPort) SetReadTimeout(d time.Duration) error { return nil }

func TestExecSingleCommand(t *testing.T) {
	mp := newMockPort()
	cfg := types.SerialConfig{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: '\r',
	}

	c := newPort(mp, cfg)

	// simulate a response from the device
	mp.readCh <- []byte("OK\r")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := c.Exec(ctx, "FA")
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}
	if resp != "OK" {
		t.Fatalf("expected response OK, got %q", resp)
	}

	if len(mp.writes) != 1 {
		t.Fatalf("expected 1 write, got %d", len(mp.writes))
	}
	if string(mp.writes[0]) != "FA\r" {
		t.Fatalf("unexpected written data: %q", string(mp.writes[0]))
	}
}

func TestConcurrentWrites(t *testing.T) {
	mp := newMockPort()
	cfg := types.SerialConfig{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: '\r',
	}

	c := newPort(mp, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = c.WriteCommand(ctx, "CMD")
		}(i)
	}

	wg.Wait()

	if len(mp.writes) != 10 {
		t.Fatalf("expected 10 writes, got %d", len(mp.writes))
	}
}

func TestReadResponseTimeout(t *testing.T) {
	mp := newMockPort()
	cfg := types.SerialConfig{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		LineDelimiter: ';',
	}

	c := newPort(mp, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.ReadResponse(ctx)
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	if time.Since(start) < 10*time.Millisecond {
		t.Fatalf("ReadResponse returned too early for timeout")
	}
}

func TestCloseWhileReading(t *testing.T) {
	mp := newMockPort()
	cfg := types.SerialConfig{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		LineDelimiter: ';',
	}

	c := newPort(mp, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.ReadResponse(ctx)
	}()

	// Give the goroutine a moment to block in ReadResponse.
	time.Sleep(10 * time.Millisecond)

	if err := c.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	select {
	case <-done:
		// ok
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("ReadResponse goroutine did not exit after Close")
	}
}

func TestCloseUnblocksRead(t *testing.T) {
	mp := newMockPort()
	cfg := types.SerialConfig{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		LineDelimiter: ';',
	}

	c := newPort(mp, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.ReadResponse(ctx)
	}()

	// give goroutine time to block in ReadResponse
	time.Sleep(10 * time.Millisecond)

	if err := c.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	select {
	case <-done:
		// ok
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("ReadResponse did not unblock after Close")
	}
}
