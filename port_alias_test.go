package serial

// This file provides a test-only type alias to keep existing tests compiling
// without altering the production implementation. The tests were written
// against a type named Port, while the implementation uses Service.
// By aliasing, tests can instantiate &Port{...} and exercise the same code.

type Port = Service
