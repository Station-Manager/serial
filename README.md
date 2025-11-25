# Station Manager: serial package

A thin, thread-safe wrapper around [`go.bug.st/serial`](https://pkg.go.dev/go.bug.st/serial) for working with HF transceivers that implement CAT (Computer Aided Transceiver)
commands.

It focuses on simple, line-oriented **string** I/O, with:

- A background reader goroutine that frames incoming data by a configurable delimiter (typically `;`).
- Safe concurrent writes, so multiple goroutines can send commands.
- A small buffer pool for read buffers to reduce allocations.
- A tiny CLI tool (`cmd/catcli`) for manual CAT interaction.

## Byte vs string APIs

The client exposes both string- and byte-oriented methods:

- String helpers:
  - `WriteCommand(ctx, cmd string) error`
  - `ReadResponse(ctx) (string, error)`
  - `Exec(ctx, cmd string) (string, error)`

- Byte primitives:
  - `WriteCommandBytes(ctx, cmd []byte) error`
  - `ReadResponseBytes(ctx) ([]byte, error)`
  - `ExecBytes(ctx, cmd []byte) ([]byte, error)`

The string APIs are thin convenience wrappers over the byte APIs:

- `WriteCommand` calls `WriteCommandBytes([]byte(cmd))`.
- `ReadResponse` calls `ReadResponseBytes` and converts the bytes to `string` without validating UTF-8.
- `Exec` calls `ExecBytes` and converts the returned bytes to `string`.

For new code—especially CAT handling where you want to parse prefixes and fields efficiently—prefer the **byte-oriented APIs**. They avoid unnecessary string allocations and make it straightforward to work with binary or non-UTF-8 payloads.

### Line delimiter behavior

All write methods share the same delimiter semantics:

- The configured `SerialConfig.LineDelimiter` (default: `\r`) is automatically appended if the last byte of the command is not already the delimiter.
- If the command already ends with the delimiter, it is not duplicated.

On reads:

- The background reader loop splits incoming data by the configured `LineDelimiter`.
- `ReadResponseBytes` returns the bytes **excluding** the delimiter.
- `ReadResponse` simply converts those bytes to a `string`.

This means each call to `ReadResponseBytes` corresponds to a single framed line from the device.

### Context and concurrency

All public methods accept a `context.Context`:

- **Write side**
  - `WriteCommandBytes` (and `WriteCommand`) check `ctx.Done()` while writing.
  - If the context is cancelled or times out, they return an error wrapping `ctx.Err()`.
  - Multiple goroutines may safely call write methods concurrently; writes are serialized internally.

- **Read side**
  - `ReadResponseBytes` (and `ReadResponse`) block until a framed line is available or the context is done.
  - If the context is cancelled or times out before a line arrives, they return an error wrapping `ctx.Err()`.
  - Reads must not be performed concurrently from multiple goroutines on the same client; use a single reader goroutine and fan out responses yourself if needed.

`ExecBytes` and `Exec` are simple compositions:

- `ExecBytes` = `WriteCommandBytes` then `ReadResponseBytes` under the same context.
- `Exec` does the same and converts the response to a `string`.

### Example: using byte APIs for CAT

```go
cfg := types.SerialConfig{
    PortName:      "/dev/ttyUSB0",
    BaudRate:      9600,
    DataBits:      8,
    StopBits:      1,
    LineDelimiter: '\r',
}

port, err := serial.Open(cfg)
if err != nil {
    log.Fatal(err)
}
defer port.Close()

ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
defer cancel()

// Send a CAT command as bytes (delimiter auto-appended if missing).
cmd := []byte("FA") // e.g. read VFO A frequency
if err := port.WriteCommandBytes(ctx, cmd); err != nil {
    log.Fatal(err)
}

// Read the response bytes (without delimiter) and hand them to a CAT parser.
resp, err := port.ReadResponseBytes(ctx)
if err != nil {
    log.Fatal(err)
}

handleCATLine(resp) // your code: parse prefix, update state, etc.
```

You can also use `ExecBytes` when you only need a single request/response pair:

```go
resp, err := port.ExecBytes(ctx, []byte("FA"))
if err != nil {
    log.Fatal(err)
}
handleCATLine(resp)
```

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

You can also combine `Errors()` with higher-level supervision logic. For
example, a long-running service might store the port in a struct and
restart it when a non-nil error arrives on the error stream.

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

## Concurrency model

The `serial` client is designed around a "one reader, many writers"
model:

- Multiple goroutines **may safely call** `WriteCommand` concurrently.
  Writes are serialized on the underlying serial port via an internal
  mutex.
- Responses are produced by a single background reader goroutine and
  must be consumed by **at most one goroutine** at a time via
  `ReadResponse` or `Exec` on a given client.

A typical pattern is:

```go
// one long-lived reader goroutine
responses := make(chan string)

go func() {
    defer close(responses)
    for {
        line, err := port.ReadResponse(ctx)
        if err != nil {
            // check for serial.ErrClosed or context errors
            break
        }
        responses <- line
    }
}()

// elsewhere, multiple goroutines are free to call WriteCommand/Exec
```

If you need to fan out responses to multiple consumers, do so from your
own reader goroutine or via an application-level dispatcher rather than
calling `ReadResponse` concurrently from multiple goroutines.
