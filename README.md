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
cd /home/mveary/Development/serial
# inside this module, just use the local package
```

If you publish this module, consumers can import it as:

```go
import "serial"
```

## Usage

### Opening a port

```go
cfg := serial.Config{
    Name:          "/dev/ttyUSB0",
    BaudRate:      9600,
    DataBits:      8,
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
        break
    }
    fmt.Println("CAT:", line)
}
```

## CLI: `cmd/catcli`

A small command-line tool is provided for manual CAT interaction.

Build it:

```sh
cd /home/mveary/Development/serial
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
cd /home/mveary/Development/serial
go test ./...

go test -run='^$' -bench=. -benchmem ./...
```

