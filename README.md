# 7Q-Station-Manager: Serial Module

[![ci](https://github.com/7Q-Station-Manager/serial/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/7Q-Station-Manager/serial/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/7Q-Station-Manager/dev/branch/main/graph/badge.svg)](https://codecov.io/gh/7Q-Station-Manager/dev)

A comprehensive serial port communication module for the 7Q Station Manager ham radio station management system. This module provides robust serial port communication with advanced monitoring, error handling, and performance optimization features.

## Features

### Core Functionality
- **Cross-platform serial port communication** using [go.bug.st/serial](https://pkg.go.dev/go.bug.st/serial)
- **Configurable timeouts** for read and write operations with context support
- **Thread-safe operations** with proper state management
- **Buffer validation and size limits** for security and stability
- **Comprehensive error handling** with categorized error types

### Advanced Monitoring & Metrics
- **Real-time health assessment** with 4-tier status system (healthy/degraded/unhealthy/down)
- **Comprehensive metrics collection** including success rates, latency, throughput
- **Channel-based metrics broadcasting** for frontend integration
- **Buffer pool monitoring** for memory efficiency tracking
- **Automatic error categorization** and consecutive failure tracking

### Performance Optimization
- **Buffer pooling system** for reduced memory allocations
- **Atomic operations** for thread-safe metrics without locks
- **Configurable channel buffers** with optimized defaults
- **Minimal overhead design** suitable for production use

## Quick Start

### Basic Usage

```go
package main

import (
    "log"
    "github.com/7Q-Station-Manager/serial"
    "github.com/7Q-Station-Manager/types"
)

func main() {
    // Initialize service
    service := &serial.Service{
        LoggerService: &logging.Service{},
        ConfigService: &config.Service{},
    }

    err := service.Initialize()
    if err != nil {
        log.Fatalf("Failed to initialize: %v", err)
    }

    // Open connection
    err = service.Open()
    if err != nil {
        log.Fatalf("Failed to open port: %v", err)
    }
    defer service.Close()

    // Perform I/O operations
    n, err := service.Write([]byte("AT\r\n"))
    if err != nil {
        log.Printf("Write error: %v", err)
    }

    buffer := make([]byte, 256)
    n, err = service.Read(buffer)
    if err != nil {
        log.Printf("Read error: %v", err)
    }

    log.Printf("Read %d bytes: %s", n, string(buffer[:n]))
}
```

### With Metrics Monitoring

```go
// Start metrics broadcasting
err := service.StartMetricsBroadcasting(2 * time.Second)
if err != nil {
    log.Printf("Failed to start metrics: %v", err)
}

// Get metrics channel for monitoring
metricsChannel, err := service.MetricsChannel()
if err != nil {
    log.Printf("Failed to get metrics channel: %v", err)
}

// Monitor in background
go func() {
    for snapshot := range metricsChannel {
        log.Printf("Health: %s (%.1f), Errors: %.2f%%, Throughput: %.1f B/s",
            snapshot.HealthStatus, snapshot.HealthScore,
            snapshot.ErrorRate, snapshot.BytesPerSecond)
    }
}()
```

## Configuration

The module supports comprehensive configuration through the `types.SerialConfig` structure:

```go
type SerialConfig struct {
    PortName           string        `koanf:"port_name"`
    BaudRate           int           `koanf:"baud_rate"`
    DataBits           int           `koanf:"data_bits"`
    Parity             int           `koanf:"parity"`
    StopBits           float32       `koanf:"stop_bits"`
    ReadTimeout        time.Duration `koanf:"read_timeout"`
    WriteTimeout       time.Duration `koanf:"write_timeout"`  // Context-based write timeouts
    RTS                bool          `koanf:"rts"`
    DTR                bool          `koanf:"dtr"`
    MetricsChannelSize int64         `koanf:"metrics_channel_size"` // Default: 50
}
```

### Example Configuration (JSON)

```json
{
  "serial_port": {
    "port_name": "/dev/ttyUSB0",
    "baud_rate": 9600,
    "data_bits": 8,
    "parity": 0,
    "stop_bits": 1,
    "read_timeout": "1500ms",
    "write_timeout": "3000ms",
    "rts": false,
    "dtr": false,
    "metrics_channel_size": 50
  }
}
```

## Advanced Features

### Write Timeout Methods

```go
// Method 1: Use configured timeout (recommended)
n, err := service.Write(data)

// Method 2: Explicit timeout
n, err := service.WriteWithTimeout(data, 5*time.Second)

// Method 3: Context-based (most flexible)
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
n, err := service.WriteWithContext(ctx, data)
```

### Buffer Pool Usage

```go
// Get optimally-sized buffer from pool
buffer, cleanup := serial.GetPooledBuffer(256)
defer cleanup()

// Use buffer for read operations
n, err := service.Read(buffer)
```

### Health Monitoring

```go
snapshot := service.GetMetricsSnapshot()

switch snapshot.HealthStatus {
case "healthy":
    // All systems normal
case "degraded":
    // Minor issues detected
case "unhealthy":
    // Significant problems
case "down":
    // Connection failed
}
```

## Documentation

- **[METRICS_USAGE.md](METRICS_USAGE.md)** - Comprehensive metrics system guide
- **API Documentation** - Available via `go doc`
- **Examples** - See `examples/` directory

## Testing

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -race -covermode=atomic -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run benchmarks
go test -bench=. -benchmem ./...
```

## Architecture

### Key Components

- **`Service`** - Main serial port service with dependency injection
- **`Metrics`** - Comprehensive monitoring system with atomic counters
- **`MetricsBroadcaster`** - Channel-based metrics distribution
- **`BufferPool`** - Memory-efficient buffer management
- **Error Types** - Categorized error handling

### Design Principles

- **Thread Safety** - All operations are thread-safe using atomic operations
- **Performance** - Optimized for low overhead and high throughput
- **Reliability** - Comprehensive error handling and health monitoring
- **Extensibility** - Modular design with clean interfaces
- **Observability** - Rich metrics and monitoring capabilities

## Requirements

- **Go 1.21+**
- **Supported Platforms**: Linux, macOS, Windows
- **Dependencies**: See `go.mod` for current versions

## License

[License information would go here]

## Contributing

[Contributing guidelines would go here]
