package main

import (
	"fmt"
	"github.com/7Q-Station-Manager/config"
	"github.com/7Q-Station-Manager/iocdi"
	"github.com/7Q-Station-Manager/logging"
	"github.com/7Q-Station-Manager/serial"
	"github.com/7Q-Station-Manager/utils"
	"os"
	"reflect"
	"time"
)

func main() {
	wd, _ := utils.WorkingDir()
	err := os.Setenv(utils.EnvSmWorkingDir, wd)
	if err != nil {
		panic(err)
	}
	fmt.Println(os.Getenv(utils.EnvSmWorkingDir))

	container := iocdi.New()
	err = container.RegisterInstance("WorkingDir", wd)
	if err != nil {
		panic(err)
	}
	err = container.Register(config.ServiceName, reflect.TypeOf((*config.Service)(nil)))
	if err != nil {
		panic(err)
	}
	err = container.Register(logging.ServiceName, reflect.TypeOf((*logging.Service)(nil)))
	if err != nil {
		panic(err)
	}
	err = container.Register(serial.ServiceName, reflect.TypeOf((*serial.Service)(nil)))
	if err != nil {
		panic(err)
	}

	err = container.Build()
	if err != nil {
		panic(err)
	}

	service, err := container.ResolveSafe(serial.ServiceName)
	if err != nil {
		panic(err)
	}

	port, ok := service.(*serial.Service)
	if !ok {
		panic("service is not a serial.Service")
	}

	if err = port.Open(); err != nil {
		panic(err)
	}
	defer func(port *serial.Service) {
		e := port.Close()
		if e != nil {
			panic(e)
		}
	}(port)

	sent, err := port.Write([]byte("ID;"))
	if sent == 0 {
		panic(err)
	}

	// Give the rig time to respond
	time.Sleep(100 * time.Millisecond)

	// Use efficient buffer pooling for response reading
	response, err := readResponseWithPooling(port)
	if err != nil {
		panic(err)
	}

	fmt.Println("This should be 'ID0761;' for an FTdx10, and it is:", response)
}

// readResponseWithPooling demonstrates efficient response reading using buffer pooling
func readResponseWithPooling(port *serial.Service) (string, error) {
	const maxResponseSize = 1024

	// Use the service's ReadWithPooling method instead of accessing bufferPoolManager directly
	data, err := port.ReadWithPooling(128)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// min returns the smaller of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
