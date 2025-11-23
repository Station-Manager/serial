package serial

import (
	"context"
	"github.com/Station-Manager/types"
	"go.bug.st/serial"
	"sync"
)

// Config holds configuration for opening a serial port.
//type Config struct {
//	// Name is the path to the serial device, e.g. /dev/ttyUSB0.
//	Name string
//
//	BaudRate int
//	DataBits int
//	StopBits serial.StopBits
//	Parity   serial.Parity
//
//	// ReadTimeout is the underlying port read timeout.
//	ReadTimeout time.Duration
//
//	// LineDelimiter is the byte used to frame responses.
//	// If zero, '\r' is used. For many CAT implementations this is ';'.
//	LineDelimiter byte
//}

// Client is the high-level interface for sending CAT commands and
// receiving responses over a serial port.
type Client interface {
	// WriteCommand writes a single CAT command string to the port.
	// Implementations will append the configured line delimiter if missing.
	WriteCommand(ctx context.Context, cmd string) error

	// ReadResponse reads a single response line terminated by the
	// configured delimiter.
	ReadResponse(ctx context.Context) (string, error)

	// Exec is a convenience that writes a command then reads one response.
	Exec(ctx context.Context, cmd string) (string, error)

	// Close closes the underlying port. It is safe to call multiple times.
	Close() error
}

// Port is the concrete implementation of Client backed by go.bug.st/serial.
type Port struct {
	port SerialPort

	cfg types.SerialConfig

	writeMu sync.Mutex

	responses chan string
	closeCh   chan struct{}
	doneCh    chan struct{}

	closed bool
	mu     sync.RWMutex
}

// Open opens a serial port with the given configuration.
func Open(cfg types.SerialConfig) (*Port, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	mode := &serial.Mode{
		BaudRate: cfg.BaudRate,
		DataBits: cfg.DataBits,
		StopBits: cfg.StopBits,
		Parity:   cfg.Parity,
	}

	p, err := serial.Open(cfg.PortName, mode)
	if err != nil {
		return nil, err
	}

	if cfg.ReadTimeout > 0 {
		_ = p.SetReadTimeout(cfg.ReadTimeout)
	}

	sp := &bugstPort{Port: p}
	cl := newPort(sp, cfg)
	return cl, nil
}

// newPort constructs a Port around an existing SerialPort.
func newPort(sp SerialPort, cfg types.SerialConfig) *Port {
	if cfg.LineDelimiter == 0 {
		cfg.LineDelimiter = '\r'
	}

	po := &Port{
		port:      sp,
		cfg:       cfg,
		responses: make(chan string, 64),
		closeCh:   make(chan struct{}),
		doneCh:    make(chan struct{}),
	}

	go po.readerLoop()

	return po
}

// WriteCommand implements Client.
func (p *Port) WriteCommand(ctx context.Context, cmd string) error {
	p.mu.RLock()
	closed := p.closed
	p.mu.RUnlock()
	if closed {
		return ErrClosed
	}

	if len(cmd) == 0 {
		return nil
	}

	// ensure delimiter
	if cmd[len(cmd)-1] != p.cfg.LineDelimiter {
		cmd = cmd + string(p.cfg.LineDelimiter)
	}

	data := []byte(cmd)

	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	written := 0
	for written < len(data) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := p.port.Write(data[written:])
		if err != nil {
			return err
		}
		written += n
	}

	return nil
}

// ReadResponse implements Client.
func (p *Port) ReadResponse(ctx context.Context) (string, error) {
	p.mu.RLock()
	closed := p.closed
	p.mu.RUnlock()
	if closed {
		return "", ErrClosed
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case line, ok := <-p.responses:
		if !ok {
			return "", ErrClosed
		}
		return line, nil
	}
}

// Exec implements Client.
func (p *Port) Exec(ctx context.Context, cmd string) (string, error) {
	if err := p.WriteCommand(ctx, cmd); err != nil {
		return "", err
	}
	return p.ReadResponse(ctx)
}

// Close implements Client.
func (p *Port) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.closeCh)
	p.mu.Unlock()

	// Close the underlying port first to unblock any in-flight Read calls.
	if err := p.port.Close(); err != nil {
		return err
	}

	// Wait for the reader loop to finish cleanup.
	<-p.doneCh
	return nil
}

// readerLoop continuously reads from the serial port and emits
// complete lines onto the response channel.
func (p *Port) readerLoop() {
	defer close(p.doneCh)
	defer close(p.responses)

	buf := getReadBuf()
	defer putReadBuf(buf)

	var lineBuf []byte

	for {
		select {
		case <-p.closeCh:
			return
		default:
		}

		n, err := p.port.Read(buf)
		if err != nil {
			return
		}
		if n == 0 {
			continue
		}

		chunk := buf[:n]
		for len(chunk) > 0 {
			idx := indexByte(chunk, p.cfg.LineDelimiter)
			if idx == -1 {
				lineBuf = append(lineBuf, chunk...)
				if len(lineBuf) > maxLineSize {
					// drop overly long lines
					lineBuf = lineBuf[:0]
				}
				break
			}

			lineBuf = append(lineBuf, chunk[:idx]...)
			// emit line
			select {
			case p.responses <- string(lineBuf):
			case <-p.closeCh:
				return
			}
			lineBuf = lineBuf[:0]

			chunk = chunk[idx+1:]
		}
	}
}

// indexByte is a small helper to avoid importing bytes for single-byte search.
func indexByte(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}
