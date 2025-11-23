package serial_test

import (
	"context"
	"fmt"
	"github.com/Station-Manager/types"
	"time"

	"github.com/Station-Manager/serial"
)

func Example() {
	cfg := types.SerialConfig{
		PortName: "/dev/ttyUSB0",
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
