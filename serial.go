package serial

import (
	"context"
	stderr "errors"
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
	"go.bug.st/serial"
	"sync"
)

const (
	// responsesBufSize controls the capacity of the responses channel used to
	// deliver framed lines from the background reader loop to callers.
	responsesBufSize = 64
)

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

	// Errors returns a receive-only channel that will yield at most one
	// terminal error from the reader loop, if any, and is closed when the
	// reader loop exits. Callers should not assume it will always produce
	// a value; a graceful close may result in the channel closing without
	// an error.
	Errors() <-chan error

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

	// errCh carries a single terminal error from the reader loop, if any.
	// It is closed when readerLoop exits.
	errCh chan error

	closed bool
	mu     sync.RWMutex
}

// Open initializes and opens a serial port based on the given SerialConfig. It returns a Port or an error if unsuccessful.
func Open(cfg types.SerialConfig) (*Port, error) {
	const op errors.Op = "serial.Open"

	ncfg, err := validateConfig(cfg)
	if err != nil {
		return nil, errors.New(op).Err(err)
	}

	mode := &serial.Mode{
		BaudRate: ncfg.BaudRate,
		DataBits: ncfg.DataBits,
		StopBits: ncfg.StopBits,
		Parity:   ncfg.Parity,
	}

	p, err := serial.Open(ncfg.PortName, mode)
	if err != nil {
		return nil, errors.New(op).Err(err)
	}

	if ncfg.ReadTimeout > 0 {
		if err := p.SetReadTimeout(ncfg.ReadTimeout); err != nil {
			return nil, errors.New(op).Err(err)
		}
	}

	sp := &bugstPort{Port: p}
	cl := newPort(sp, ncfg)
	return cl, nil
}

// newPort constructs a Port around an existing SerialPort.
func newPort(sp SerialPort, cfg types.SerialConfig) *Port {
	if cfg.LineDelimiter == 0 {
		cfg.LineDelimiter = '\r' // Default line delimiter, if not provided
	}

	po := &Port{
		port:      sp,
		cfg:       cfg,
		responses: make(chan string, responsesBufSize),
		closeCh:   make(chan struct{}),
		doneCh:    make(chan struct{}),
		// errCh is buffered by one so the reader loop can report a terminal
		// error without blocking; it is closed when readerLoop exits.
		errCh: make(chan error, 1),
	}

	go po.readerLoop()

	return po
}

// WriteCommand implements Client.
func (p *Port) WriteCommand(ctx context.Context, cmd string) error {
	const op errors.Op = "serial.WriteCommand"

	p.mu.RLock()
	closed := p.closed
	p.mu.RUnlock()
	if closed {
		return errors.New(op).Err(ErrClosed)
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
			return errors.New(op).Err(ctx.Err())
		default:
		}

		n, err := p.port.Write(data[written:])
		if err != nil {
			return errors.New(op).Err(err)
		}
		if n == 0 {
			// Protect against misbehaving SerialPort implementations that
			// report success but do not advance the write offset, which
			// would otherwise cause this loop to spin indefinitely.
			return errors.New(op).Msg("serial: write returned 0 bytes without error")
		}
		written += n
	}

	return nil
}

// ReadResponse implements Client.
func (p *Port) ReadResponse(ctx context.Context) (string, error) {
	const op errors.Op = "serial.ReadResponse"

	p.mu.RLock()
	closed := p.closed
	p.mu.RUnlock()
	if closed {
		return "", errors.New(op).Err(ErrClosed)
	}

	select {
	case <-ctx.Done():
		return "", errors.New(op).Err(ctx.Err())
	case line, ok := <-p.responses:
		if !ok {
			return "", errors.New(op).Err(ErrClosed)
		}
		return line, nil
	}
}

// Exec implements Client.
func (p *Port) Exec(ctx context.Context, cmd string) (string, error) {
	const op errors.Op = "serial.Exec"

	if err := p.WriteCommand(ctx, cmd); err != nil {
		return "", errors.New(op).Err(err)
	}
	return p.ReadResponse(ctx)
}

// Errors implements Client.
func (p *Port) Errors() <-chan error {
	return p.errCh
}

// Close implements Client.
func (p *Port) Close() error {
	const op errors.Op = "serial.Close"

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
		return errors.New(op).Err(err)
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
	defer close(p.errCh)

	buf := getReadBuf()
	defer putReadBuf(buf)

	var lineBuf []byte

	for {
		select {
		case <-p.closeCh:
			return
		default:
			// No-op
		}

		n, err := p.port.Read(buf)
		if err != nil {
			// Treat timeout-like errors as recoverable and keep looping.
			var to interface{ Timeout() bool }
			if stderr.As(err, &to) && to.Timeout() {
				continue
			}

			// Non-timeout error: surface it to callers, then exit.
			select {
			case p.errCh <- errors.New(errors.Op("serial.readerLoop")).Err(err):
			default:
			}
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
					// drop overly long lines and notify via Errors() on a
					// best-effort basis without terminating the loop.
					lineBuf = lineBuf[:0]
					select {
					case p.errCh <- errors.New(errors.Op("serial.readerLoop")).Msg("serial: dropped line exceeding maxLineSize (4096 bytes)"):
					default:
					}
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
