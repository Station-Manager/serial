# Call site, combine Erros with a normal control loop

```go

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

go func() {
    if err, ok := <-port.Errors(); ok && err != nil {
        // log and trigger reconnect
        // e.g., notify a supervisor goroutine that will Close and reopen
    }
}()

for {
    resp, err := port.ReadResponse(ctx)
    if err != nil {
        // distinguish between context cancellation vs ErrClosed
        if errors.Is(err, serial.ErrClosed) {
            break
        }
        // handle other errors
        break
    }
    // process resp ...
}



```

## Concurrency notes

The `serial` package is built around a "one reader, many writers"
concurrency model:

- Multiple goroutines may safely call `WriteCommand` concurrently on the
  same client; writes are serialized internally on the underlying
  `SerialPort`.
- Responses are produced by a single background reader goroutine inside
  the client and must be consumed by **at most one goroutine** at a time
  via `ReadResponse` or `Exec`.

In practice this means:

- Run exactly one goroutine that calls `ReadResponse` in a loop and
  forwards lines to the rest of your application (e.g. over channels).
- Allow any number of other goroutines to call `WriteCommand`/`Exec`
  against the same client for outgoing CAT commands.

If you need to fan out responses to multiple consumers, do so in your
own code by reading from `ReadResponse` in a single place and then
multiplexing the resulting lines, rather than calling `ReadResponse`
concurrently from multiple goroutines.
