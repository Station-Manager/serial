package serial_test

import (
	"context"
	"fmt"
	"time"

	"serial"
)

func Example() {
	cfg := serial.Config{
		Name:     "/dev/ttyUSB0",
		BaudRate: 9600,
		DataBits: 8,
	}

	client, err := serial.Open(cfg)
	if err != nil {
		fmt.Println("open error:", err)
		return
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	resp, err := client.Exec(ctx, "FA")
	if err != nil {
		fmt.Println("exec error:", err)
		return
	}

	fmt.Println("response:", resp)
}
