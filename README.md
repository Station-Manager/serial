# serial

A thin, thread-safe wrapper around [`go.bug.st/serial`](https://pkg.go.dev/go.bug.st/serial) for working with HF transceivers that implement CAT (Computer Aided Transceiver)
commands.

It focuses on simple, line-oriented **string** I/O, with:

- A background reader goroutine that frames incoming data by a configurable delimiter (typically `;`).
- Safe concurrent writes, so multiple goroutines can send commands.
- A small buffer pool for read buffers to reduce allocations.
- A tiny CLI tool (`cmd/catcli`) for manual CAT interaction.

## Installation

```sh
# inside this module, just use the local package
cd /home/mveary/Development/Station-Manager/serial
```

If you publish this module, consumers can import it as:

```go
import (
    "github.com/Station-Manager/serial"
    "github.com/Station-Manager/types"
)
```

## Usage

### Opening a port

```go
cfg := types.SerialConfig{
    PortName:      "/dev/ttyUSB0",
    BaudRate:      9600,
    // DataBits, StopBits, and Parity may be left at zero to use defaults
    // (8 data bits, 1 stop bit, no parity).
    LineDelimiter: ';', // many CAT rigs use ';' as terminator
}

port, err := serial.Open(cfg)
if err != nil {
    // handle error
}
defer port.Close()
```

### Sending a command and reading a response

```go
ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
defer cancel()

resp, err := port.Exec(ctx, "FA") // e.g. read frequency
if err != nil {
    // handle error (timeout, closed, etc.)
}

fmt.Println("response:", resp)
```

### Listening for unsolicited CAT data

Some modern transceivers can be configured to stream CAT data automatically.

You can listen for these lines by repeatedly calling `ReadResponse`:

```go
ctx := context.Background()
for {
    line, err := port.ReadResponse(ctx)
    if err != nil {
        // handle error or break on serial.ErrClosed
        if errors.Is(err, serial.ErrClosed) {
            break
        }
        // handle other errors
        break
    }
    fmt.Println("CAT:", line)
}
```

### Observing terminal read errors via Errors()

The client also exposes an `Errors()` channel that reports a terminal
(non-timeout) error from the background reader loop, if any:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

// supervise terminal reader errors in a separate goroutine
go func() {
    if err, ok := <-port.Errors(); ok && err != nil {
        // log and trigger reconnect
        // e.g., notify a supervisor goroutine that will Close and reopen
    }
}()

for {
    resp, err := port.ReadResponse(ctx)
    if err != nil {
        if errors.Is(err, serial.ErrClosed) {
            break
        }
        // handle other errors
        break
    }
    // process resp ...
}
```

The `Errors()` channel yields at most one error and is closed when the
reader goroutine exits. A graceful close (via `Close()`) may cause the
channel to close without sending any error.

## Error structure

This package consistently wraps errors using the
[`github.com/Station-Manager/errors`](https://github.com/Station-Manager/errors)
module. Each public operation uses an `errors.Op` tag to annotate the
source of the failure, for example:

- `serial.Open`
- `serial.WriteCommand`
- `serial.ReadResponse`
- `serial.Exec`
- `serial.Close`
- `serial.readerLoop` (for terminal errors from the background reader)

This means callers will typically see errors created via
`errors.New(op).Err(err)` or `errors.New(op).Msg(...)`, preserving both
operation context and the underlying cause.

For control-flow, the package exposes a sentinel `ErrClosed` to indicate
that the port has been closed. Public methods wrap this sentinel via the
custom errors package, so you should use `errors.Is` (from the same
module) to detect it:

```go
resp, err := port.ReadResponse(ctx)
if err != nil {
    if errors.Is(err, serial.ErrClosed) {
        // handle closed port
    }
    // handle other errors
}
```

Context cancellations and timeouts are also wrapped, so the original
`context.Canceled` / `context.DeadlineExceeded` is preserved as the
cause and can be inspected via the custom errors helpers.

Finally, the `Errors()` channel surfaces at most one terminal
(non-timeout) error from the background reader loop, already wrapped
with an op of `serial.readerLoop`. The channel is always closed when the
reader goroutine exits, and may close without sending a value if the
port is closed cleanly.

If an incoming line grows beyond the internal `maxLineSize` (currently
4096 bytes) without a delimiter, that line is dropped. A best-effort
notification of this condition is emitted on `Errors()` using an op of
`serial.readerLoop`, but the reader continues to operate for subsequent
well-formed lines.

## CLI: `cmd/catcli`

A small command-line tool is provided for manual CAT interaction.

Build it:

```sh
cd /home/mveary/Development/Station-Manager/serial
go build ./cmd/catcli
```

### Single command mode

```sh
./catcli \
  -device /dev/ttyUSB0 \
  -baud 9600 \
  -databits 8 \
  -parity N \
  -stopbits 1 \
  -delim ';' \
  -read-timeout 2s \
  -cmd FA
```

### Interactive mode

```sh
./catcli -device /dev/ttyUSB0 -baud 9600 -delim ';'
```

Type commands (e.g. `FA`) and press Enter; responses will be printed until EOF (Ctrl+D).

### Listen-only mode

```sh
./catcli -device /dev/ttyUSB0 -baud 9600 -delim ';' -listen
```

In this mode, `catcli` does not send commands; it only prints incoming CAT lines as the transceiver streams them.

## Testing

Run unit tests and benchmarks:

```sh
cd /home/mveary/Development/Station-Manager/serial

go test ./...

go test -run='^$' -bench=. -benchmem ./...
```
