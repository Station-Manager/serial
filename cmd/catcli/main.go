package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/Station-Manager/types"
	"log"
	"os"
	"strings"
	"time"

	"github.com/Station-Manager/serial"
	bugst "go.bug.st/serial"
)

func main() {
	device := flag.String("device", "/dev/ttyUSB0", "serial device path")
	baud := flag.Int("baud", 9600, "baud rate")
	dataBits := flag.Int("databits", 8, "data bits")
	parity := flag.String("parity", "N", "parity (N,E,O)")
	stopBits := flag.Int("stopbits", 1, "stop bits (1 or 2)")
	delimiter := flag.String("delim", ";", "line delimiter used by CAT (default ';')")
	cmd := flag.String("cmd", "", "single CAT command to send; if empty, read commands from stdin")
	listen := flag.Bool("listen", false, "listen-only mode: print incoming CAT lines until interrupted")
	readTimeout := flag.Duration("read-timeout", 2*time.Second, "read timeout per response")

	flag.Parse()

	cfg := types.SerialConfig{
		PortName:      *device,
		BaudRate:      *baud,
		DataBits:      *dataBits,
		LineDelimiter: (*delimiter)[0],
	}

	// map parity
	switch strings.ToUpper(*parity) {
	case "N":
		cfg.Parity = bugst.NoParity
	case "E":
		cfg.Parity = bugst.EvenParity
	case "O":
		cfg.Parity = bugst.OddParity
	default:
		log.Fatalf("unsupported parity %q (use N,E,O)", *parity)
	}

	// map stop bits
	switch *stopBits {
	case 1:
		cfg.StopBits = bugst.OneStopBit
	case 2:
		cfg.StopBits = bugst.TwoStopBits
	default:
		log.Fatalf("unsupported stopbits %d (use 1 or 2)", *stopBits)
	}

	port, err := serial.Open(cfg)
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer port.Close()

	if *listen {
		// Listen-only mode: continuously read and print incoming lines.
		log.Printf("listening on %s (baud=%d, delim=%q)...", cfg.PortName, cfg.BaudRate, cfg.LineDelimiter)
		for {
			ctx, cancel := context.WithTimeout(context.Background(), *readTimeout)
			line, err := port.ReadResponse(ctx)
			cancel()
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
					// timeout or cancellation: just continue listening
					continue
				}
				log.Fatalf("listen error: %v", err)
			}
			fmt.Println(line)
		}
	}

	if *cmd != "" {
		// Single command mode
		ctx, cancel := context.WithTimeout(context.Background(), *readTimeout)
		defer cancel()

		resp, err := port.Exec(ctx, *cmd)
		if err != nil {
			log.Fatalf("exec: %v", err)
		}
		fmt.Println(resp)
		return
	}

	// Interactive mode: read commands from stdin line by line.
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Fprintln(os.Stderr, "Entering interactive mode. Type CAT commands, Ctrl+D to exit.")
	for {
		fmt.Fprint(os.Stderr, "> ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				log.Printf("stdin error: %v", err)
			}
			return
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), *readTimeout)
		resp, err := port.Exec(ctx, line)
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			continue
		}
		fmt.Println(resp)
	}
}
