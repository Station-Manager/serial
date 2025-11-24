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

	mu sync.Mutex
	// errToReturn, if non-nil, will be returned on the next Read call
	// instead of data from readCh. This allows exercising error paths
	// from the reader loop.
	errToReturn error
}

func newMockPort() *mockPort {
	return &mockPort{readCh: make(chan []byte, 16)}
}

func (m *mockPort) Read(p []byte) (int, error) {
	m.mu.Lock()
	if m.errToReturn != nil {
		err := m.errToReturn
		m.errToReturn = nil
		m.mu.Unlock()
		return 0, err
	}
	m.mu.Unlock()

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

// TestErrorsStreamClosesOnCloseWithoutError verifies that the Errors
// channel is closed when the port is closed without a terminal read
// error, so callers can reliably range over it.
func TestErrorsStreamClosesOnCloseWithoutError(t *testing.T) {
	mp := newMockPort()
	cfg := types.SerialConfig{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: ';',
	}

	c := newPort(mp, cfg)

	if err := c.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	select {
	case _, ok := <-c.Errors():
		if ok {
			t.Fatalf("expected Errors() channel to be closed without value after Close")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for Errors() channel to close after Close")
	}
}

func TestValidateConfigDefaultsAndSuccess(t *testing.T) {
	in := types.SerialConfig{
		PortName: "ttyS0",
		BaudRate: 9600,
		// DataBits, StopBits, Parity left at zero to exercise defaults.
	}

	got, err := validateConfig(in)
	if err != nil {
		t.Fatalf("validateConfig unexpected error: %v", err)
	}

	if got.PortName != "ttyS0" {
		t.Fatalf("expected PortName to be %q, got %q", "ttyS0", got.PortName)
	}
	if got.BaudRate != 9600 {
		t.Fatalf("expected BaudRate to be 9600, got %d", got.BaudRate)
	}
	if got.DataBits != 8 {
		t.Fatalf("expected DataBits default of 8, got %d", got.DataBits)
	}
}

func TestValidateConfigFailures(t *testing.T) {
	tests := []struct {
		name string
		cfg  types.SerialConfig
	}{
		{"missing port name", types.SerialConfig{BaudRate: 9600}},
		{"invalid baud", types.SerialConfig{PortName: "ttyS0", BaudRate: 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateConfig(tt.cfg)
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tt.name)
			}
		})
	}
}

// zeroWritePort is a SerialPort implementation that always reports
// success but writes 0 bytes, which should be treated as an error by
// WriteCommand to avoid spinning indefinitely.
type zeroWritePort struct{}

func (z *zeroWritePort) Read(p []byte) (int, error)           { return 0, context.Canceled }
func (z *zeroWritePort) Write(p []byte) (int, error)          { return 0, nil }
func (z *zeroWritePort) Close() error                         { return nil }
func (z *zeroWritePort) SetReadTimeout(d time.Duration) error { return nil }

func TestWriteCommandZeroWriteIsError(t *testing.T) {
	cfg := types.SerialConfig{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: '\r',
	}

	c := newPort(&zeroWritePort{}, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := c.WriteCommand(ctx, "CMD"); err == nil {
		t.Fatalf("expected error when underlying Write returns 0 bytes, got nil")
	}
}

// overflowPort feeds a single line that exceeds maxLineSize by sending
// data in chunks without any delimiter so that readerLoop overflows the
// internal line buffer and drops the line.
type overflowPort struct {
	mockPort
}

func newOverflowPort() *overflowPort {
	return &overflowPort{mockPort{readCh: make(chan []byte, 16)}}
}

func TestOversizedLineEmitsErrorAndIsDropped(t *testing.T) {
	o := newOverflowPort()
	cfg := types.SerialConfig{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: ';',
	}

	c := newPort(o, cfg)

	// Feed enough data to exceed maxLineSize (4096) without any delimiter.
	bigChunk := make([]byte, maxLineSize+10)
	for i := range bigChunk {
		bigChunk[i] = 'A'
	}
	o.readCh <- bigChunk

	// Close the underlying mock so readerLoop eventually stops reading.
	close(o.readCh)

	// We expect one best-effort error on Errors(), and no line delivered.
	select {
	case err, ok := <-c.Errors():
		if !ok {
			t.Fatalf("expected error value before channel close")
		}
		if err == nil {
			t.Fatalf("expected non-nil error from Errors() for oversized line")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for error from Errors() for oversized line")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// There should be no response line because the oversized line is dropped.
	if _, err := c.ReadResponse(ctx); err == nil {
		t.Fatalf("expected error or timeout when reading after oversized line; got nil")
	}
}

// TestOversizedLineEmitsErrorOnErrorsStream verifies that when the
// readerLoop encounters a line longer than maxLineSize, it emits a
// best-effort error on Errors() and does not panic or deadlock.
func TestOversizedLineEmitsErrorOnErrorsStream(t *testing.T) {
	o := newOverflowPort()
	cfg := types.SerialConfig{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: ';',
	}

	c := newPort(o, cfg)

	// Feed enough data to exceed maxLineSize (4096) without any delimiter.
	bigChunk := make([]byte, maxLineSize+10)
	for i := range bigChunk {
		bigChunk[i] = 'A'
	}
	o.readCh <- bigChunk

	// Close underlying channel so readerLoop eventually exits after
	// processing the oversized line.
	close(o.readCh)

	select {
	case err, ok := <-c.Errors():
		if !ok {
			t.Fatalf("expected error value before channel close for oversized line")
		}
		if err == nil {
			t.Fatalf("expected non-nil error from Errors() for oversized line")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for Errors() notification for oversized line")
	}
}

// TestConcurrentReadResponseCalls documents and exercises the current
// behavior when ReadResponse is called concurrently from multiple
// goroutines. The implementation does not guarantee safe concurrent
// reads, but this test ensures that, at minimum, the calls do not
// deadlock or panic under moderate contention.
func TestConcurrentReadResponseCalls(t *testing.T) {
	mp := newMockPort()
	cfg := types.SerialConfig{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: ';',
	}

	c := newPort(mp, cfg)

	// Feed a handful of well-formed lines.
	go func() {
		for i := 0; i < 10; i++ {
			mp.readCh <- []byte("RSP;")
		}
		close(mp.readCh)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				_, err := c.ReadResponse(ctx)
				if err != nil {
					return
				}
			}
		}()
	}

	wg.Wait()
}

func TestWriteCommandBytesAppendsDelimiter(t *testing.T) {
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

	if err := c.WriteCommandBytes(ctx, []byte("FA")); err != nil {
		t.Fatalf("WriteCommandBytes error: %v", err)
	}

	if len(mp.writes) != 1 {
		t.Fatalf("expected 1 write, got %d", len(mp.writes))
	}
	if string(mp.writes[0]) != "FA\r" {
		t.Fatalf("unexpected written data: %q", string(mp.writes[0]))
	}
}

func TestWriteCommandBytesPreservesExistingDelimiter(t *testing.T) {
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

	if err := c.WriteCommandBytes(ctx, []byte("FA\r")); err != nil {
		t.Fatalf("WriteCommandBytes error: %v", err)
	}

	if len(mp.writes) != 1 {
		t.Fatalf("expected 1 write, got %d", len(mp.writes))
	}
	if string(mp.writes[0]) != "FA\r" {
		t.Fatalf("unexpected written data: %q", string(mp.writes[0]))
	}
}

func TestWriteCommandBytesEmptyNoop(t *testing.T) {
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

	if err := c.WriteCommandBytes(ctx, nil); err != nil {
		t.Fatalf("WriteCommandBytes(nil) error: %v", err)
	}
	if err := c.WriteCommandBytes(ctx, []byte{}); err != nil {
		t.Fatalf("WriteCommandBytes(empty) error: %v", err)
	}

	if len(mp.writes) != 0 {
		t.Fatalf("expected no writes for empty commands, got %d", len(mp.writes))
	}
}

func TestReadResponseBytesReturnsBytesWithoutDelimiter(t *testing.T) {
	mp := newMockPort()
	cfg := types.SerialConfig{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: '\n',
	}

	c := newPort(mp, cfg)

	// simulate a single response line terminated by '\n'
	mp.readCh <- []byte("OK\n")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := c.ReadResponseBytes(ctx)
	if err != nil {
		t.Fatalf("ReadResponseBytes error: %v", err)
	}
	if string(resp) != "OK" {
		t.Fatalf("expected response 'OK', got %q", string(resp))
	}
}

func TestReadResponseBytesWithChunkedInput(t *testing.T) {
	mp := newMockPort()
	cfg := types.SerialConfig{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: ';',
	}

	c := newPort(mp, cfg)

	// Simulate fragmented input: "ABCD;EF;" split over multiple reads.
	mp.readCh <- []byte("AB")
	mp.readCh <- []byte("CD;EF;")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp1, err := c.ReadResponseBytes(ctx)
	if err != nil {
		t.Fatalf("first ReadResponseBytes error: %v", err)
	}
	if string(resp1) != "ABCD" {
		t.Fatalf("expected first response 'ABCD', got %q", string(resp1))
	}

	resp2, err := c.ReadResponseBytes(ctx)
	if err != nil {
		t.Fatalf("second ReadResponseBytes error: %v", err)
	}
	if string(resp2) != "EF" {
		t.Fatalf("expected second response 'EF', got %q", string(resp2))
	}
}

func TestExecBytesRoundTripBinaryPayload(t *testing.T) {
	mp := newMockPort()
	cfg := types.SerialConfig{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: '\r',
	}

	c := newPort(mp, cfg)

	cmd := []byte{0x00, 0x01, 0xFF}
	// Response includes delimiter, which should be stripped by ReadResponseBytes.
	mp.readCh <- []byte{0x10, 0x20, '\r'}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp, err := c.ExecBytes(ctx, cmd)
	if err != nil {
		t.Fatalf("ExecBytes error: %v", err)
	}

	if len(mp.writes) != 1 {
		t.Fatalf("expected 1 write, got %d", len(mp.writes))
	}
	expectedWritten := append(append([]byte{}, cmd...), '\r')
	if string(mp.writes[0]) != string(expectedWritten) {
		t.Fatalf("unexpected written data: %v, expected %v", mp.writes[0], expectedWritten)
	}

	expectedResp := []byte{0x10, 0x20}
	if string(resp) != string(expectedResp) {
		t.Fatalf("unexpected response data: %v, expected %v", resp, expectedResp)
	}
}

func TestWriteCommandBytesContextCancelledBeforeWrite(t *testing.T) {
	mp := newMockPort()
	cfg := types.SerialConfig{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: '\r',
	}

	c := newPort(mp, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := c.WriteCommandBytes(ctx, []byte("CMD")); err == nil {
		t.Fatalf("expected error when context is cancelled before write, got nil")
	}
	if len(mp.writes) != 0 {
		t.Fatalf("expected no writes when context is cancelled, got %d", len(mp.writes))
	}
}

func TestReadResponseBytesContextTimeoutWhileWaiting(t *testing.T) {
	mp := newMockPort()
	cfg := types.SerialConfig{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: ';',
	}

	c := newPort(mp, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.ReadResponseBytes(ctx)
	if err == nil {
		t.Fatalf("expected timeout error from ReadResponseBytes, got nil")
	}
	if time.Since(start) < 10*time.Millisecond {
		t.Fatalf("ReadResponseBytes returned too early for timeout")
	}
}

func TestExecBytesAfterCloseReturnsErrClosed(t *testing.T) {
	mp := newMockPort()
	cfg := types.SerialConfig{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: '\r',
	}

	c := newPort(mp, cfg)

	if err := c.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if _, err := c.ExecBytes(ctx, []byte("CMD")); err == nil {
		t.Fatalf("expected error from ExecBytes after Close, got nil")
	}
}

func TestReadResponseBytesAfterCloseReturnsErrClosed(t *testing.T) {
	mp := newMockPort()
	cfg := types.SerialConfig{
		PortName:      "mock",
		BaudRate:      9600,
		DataBits:      8,
		StopBits:      1,
		LineDelimiter: ';',
	}

	c := newPort(mp, cfg)

	if err := c.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if _, err := c.ReadResponseBytes(ctx); err == nil {
		t.Fatalf("expected error from ReadResponseBytes after Close, got nil")
	}
}

func TestWriteCommandBytesConcurrentWrites(t *testing.T) {
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
		go func() {
			defer wg.Done()
			_ = c.WriteCommandBytes(ctx, []byte("CMD"))
		}()
	}

	wg.Wait()

	if len(mp.writes) != 10 {
		t.Fatalf("expected 10 writes, got %d", len(mp.writes))
	}
}
