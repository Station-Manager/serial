
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
