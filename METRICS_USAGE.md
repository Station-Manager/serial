# Serial Port Metrics Usage Guide

This guide demonstrates how to use the serial port metrics functionality for monitoring and debugging serial communication health in the 7Q Station Manager.

## Overview

The serial port metrics system provides comprehensive monitoring of:
- Connection health and stability
- Read/write operation success rates and performance
- Error categorization and trending
- Buffer pool efficiency
- Real-time health assessment
- Channel-based metrics streaming for frontend integration

## Basic Usage

### Initialize Service with Metrics

```go
import (
    "github.com/7Q-Station-Manager/serial"
    "github.com/7Q-Station-Manager/logging"
    "github.com/7Q-Station-Manager/config"
)

// Create and initialize the service
service := &serial.Service{
    LoggerService: &logging.Service{},
    ConfigService: &config.Service{},
}

err := service.Initialize()
if err != nil {
    log.Fatalf("Failed to initialize serial service: %v", err)
}

// Metrics are automatically enabled by default
// service.EnableMetrics()  // Explicitly enable if needed
```

### Open Connection and Monitor

```go
// Open the serial port
err = service.Open()
if err != nil {
    log.Fatalf("Failed to open serial port: %v", err)
}

// Perform operations (metrics are recorded automatically)
data := []byte("AT\r\n")
n, err := service.Write(data)
if err != nil {
    log.Printf("Write error: %v", err)
}

response := make([]byte, 256)
n, err = service.Read(response)
if err != nil {
    log.Printf("Read error: %v", err)
}
```

### Get Metrics Snapshot

```go
// Get comprehensive metrics snapshot
snapshot := service.GetMetricsSnapshot()

fmt.Printf("Serial Port Health: %s (Score: %.1f/100)\n", 
    snapshot.HealthStatus, snapshot.HealthScore)
    
fmt.Printf("Connection Status: %t, Uptime: %.1fs\n",
    snapshot.IsConnected, snapshot.UptimeSeconds)
    
fmt.Printf("Success Rates - Read: %.1f%%, Write: %.1f%%\n",
    snapshot.ReadSuccessRate, snapshot.WriteSuccessRate)
    
fmt.Printf("Performance - Throughput: %.1f bytes/sec, Avg Read: %v, Avg Write: %v\n",
    snapshot.BytesPerSecond, snapshot.AverageReadLatency, snapshot.AverageWriteLatency)
    
fmt.Printf("Errors - Rate: %.2f%%, Timeouts: %.2f%%, Consecutive Failures: %d\n",
    snapshot.ErrorRate, snapshot.TimeoutRate, snapshot.ConsecutiveFailures)
```

## Channel-Based Frontend Integration

### Setup Metrics Broadcasting

```go
import (
    "time"
    "log"
)

// Start metrics broadcasting (typically in main application setup)
func setupSerialMetrics(serialService *serial.Service) {
    // Start metrics broadcasting for frontend updates
    err := serialService.StartMetricsBroadcasting(2 * time.Second)
    if err != nil {
        log.Printf("Failed to start metrics broadcasting: %v", err)
        return
    }

    // Get metrics channel for consumption
    metricsChannel, err := serialService.MetricsChannel()
    if err != nil {
        log.Printf("Failed to get metrics channel: %v", err)
        return
    }

    // Start goroutine to consume metrics
    go func() {
        for snapshot := range metricsChannel {
            // Forward to your frontend system (WebSocket, SSE, etc.)
            sendToFrontend(snapshot)
        }
    }()
}

// Clean shutdown
func shutdown(serialService *serial.Service) {
    serialService.StopMetricsBroadcasting()
}
```

### Frontend Integration Example

```typescript
// Frontend interface matching the SerialMetricsSnapshot type
interface SerialMetricsSnapshot {
    timestamp: string;
    is_connected: boolean;
    health_status: 'healthy' | 'degraded' | 'unhealthy' | 'down';
    health_score: number;
    connection_success_rate: number;
    uptime_seconds: number;
    read_success_rate: number;
    write_success_rate: number;
    average_read_latency: number;
    average_write_latency: number;
    bytes_per_second: number;
    operations_per_second: number;
    timeout_rate: number;
    error_rate: number;
    consecutive_failures: number;
    buffer_pool_hit_ratio: number;
    // ... additional fields
}

// Example WebSocket-based metrics consumption
class SerialMetricsConsumer {
    private ws: WebSocket;

    constructor() {
        this.ws = new WebSocket('ws://localhost:8080/metrics');
        this.setupEventHandlers();
    }

    private setupEventHandlers() {
        this.ws.onmessage = (event) => {
            const metrics: SerialMetricsSnapshot = JSON.parse(event.data);
            this.updateHealthIndicator(metrics.health_status, metrics.health_score);
            this.updatePerformanceCharts(metrics);
            this.updateErrorAlerts(metrics);
        };
    }

    private updateHealthIndicator(status: string, score: number) {
        const indicator = document.getElementById('serial-health');
        if (indicator) {
            indicator.className = `health-indicator ${status}`;
            indicator.textContent = `${status.toUpperCase()} (${score.toFixed(1)})`;
        }
    }
}

// Alternative: Server-Sent Events approach
const eventSource = new EventSource('/api/serial/metrics');
eventSource.onmessage = function(event) {
    const metrics: SerialMetricsSnapshot = JSON.parse(event.data);
    // Handle metrics update
};
```

### Advanced Channel Consumption Patterns

```go
// Pattern 1: Non-blocking consumer with select
func consumeMetricsNonBlocking(service *serial.Service) {
    metricsChannel, err := service.MetricsChannel()
    if err != nil {
        log.Printf("Failed to get metrics channel: %v", err)
        return
    }

    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case snapshot := <-metricsChannel:
            processMetrics(snapshot)
        case <-ticker.C:
            // Periodic health checks or other work
        }
    }
}

// Pattern 2: Multiple consumers with fan-out
func fanOutMetrics(service *serial.Service, consumers int) {
    metricsChannel, err := service.MetricsChannel()
    if err != nil {
        log.Printf("Failed to get metrics channel: %v", err)
        return
    }

    // Create fan-out channels
    fanOut := make([]chan types.SerialMetricsSnapshot, consumers)
    for i := range fanOut {
        fanOut[i] = make(chan types.SerialMetricsSnapshot, 10)
    }

    // Fan-out goroutine
    go func() {
        for snapshot := range metricsChannel {
            for _, ch := range fanOut {
                select {
                case ch <- snapshot:
                default:
                    // Consumer is slow, skip
                }
            }
        }
        // Close all fan-out channels
        for _, ch := range fanOut {
            close(ch)
        }
    }()

    // Start consumers
    for i, ch := range fanOut {
        go func(id int, ch <-chan types.SerialMetricsSnapshot) {
            for snapshot := range ch {
                log.Printf("Consumer %d processing metrics at %v", id, snapshot.Timestamp)
            }
        }(i, ch)
    }
}

// Pattern 3: Filtering and aggregation
func consumeAndAggregate(service *serial.Service) {
    metricsChannel, err := service.MetricsChannel()
    if err != nil {
        log.Printf("Failed to get metrics channel: %v", err)
        return
    }

    var healthScores []float64
    var lastAlert time.Time

    for snapshot := range metricsChannel {
        // Collect health scores for trending
        healthScores = append(healthScores, snapshot.HealthScore)
        if len(healthScores) > 10 {
            healthScores = healthScores[1:] // Keep last 10 scores
        }

        // Only alert on significant health degradation
        if snapshot.HealthScore < 50 && time.Since(lastAlert) > 5*time.Minute {
            avgScore := calculateAverage(healthScores)
            if avgScore < 60 { // Confirm trend
                sendAlert("Health trend declining", snapshot)
                lastAlert = time.Now()
            }
        }
    }
}
```

## Health Monitoring & Alerting

### Continuous Health Monitoring

```go
import (
    "time"
    "log"
)

func monitorSerialHealth(service *serial.Service) {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        snapshot := service.GetMetricsSnapshot()
        
            // Check for critical conditions
        if snapshot.HealthStatus == "unhealthy" {
            log.Printf("⚠️ CRITICAL: Serial port unhealthy - Score: %.1f", snapshot.HealthScore)
            
            if snapshot.ConsecutiveFailures >= 5 {
                log.Printf("   - High consecutive failures: %d", snapshot.ConsecutiveFailures)
            }
            if snapshot.ErrorRate > 10 {
                log.Printf("   - High error rate: %.2f%%", snapshot.ErrorRate)
            }
            if snapshot.TimeoutRate > 20 {
                log.Printf("   - High timeout rate: %.2f%%", snapshot.TimeoutRate)
            }
            
            // Trigger alerts/notifications here
            sendAlert("SERIAL_CRITICAL", snapshot)
        }
        
        // Performance warnings
        if snapshot.AverageWriteLatency > 100*time.Millisecond {
            log.Printf("🐌 Performance: High write latency %v", snapshot.AverageWriteLatency)
        }
        
        if snapshot.BufferPoolHitRatio < 80 {
            log.Printf("📊 Buffer pool efficiency low: %.1f%%", snapshot.BufferPoolHitRatio)
        }
    }
}
```

### Automated Diagnostics

```go
func diagnoseSerialIssues(service *serial.Service) {
    snapshot := service.GetMetricsSnapshot()
    
    fmt.Println("=== SERIAL PORT DIAGNOSTICS ===")
    
    if !snapshot.IsConnected {
        fmt.Println("❌ Port not connected")
        fmt.Println("   Recommendations:")
        fmt.Println("   - Check physical connection")
        fmt.Println("   - Verify port permissions")
        fmt.Println("   - Ensure port not in use by another process")
        return
    }
    
    // Connection stability
    if snapshot.ConnectionSuccess < 90 {
        fmt.Printf("⚠️ Low connection success rate: %.1f%%\n", snapshot.ConnectionSuccess)
        fmt.Println("   - Check USB cable integrity")
        fmt.Println("   - Verify power supply stability")
    }
    
    // Communication reliability
    if snapshot.ErrorRate > 5 {
        fmt.Printf("⚠️ High error rate: %.2f%%\n", snapshot.ErrorRate)
        fmt.Println("   - Check baud rate configuration")
        fmt.Println("   - Verify data/parity/stop bit settings")
        fmt.Println("   - Test with different cable")
    }
    
    if snapshot.TimeoutRate > 10 {
        fmt.Printf("⚠️ High timeout rate: %.2f%%\n", snapshot.TimeoutRate)
        fmt.Println("   - Consider increasing timeout values")
        fmt.Println("   - Check device responsiveness")
        fmt.Println("   - Verify flow control settings")
    }
    
    // Performance analysis
    if snapshot.AverageReadLatency > 50*time.Millisecond {
        fmt.Printf("📊 Slow read performance: %v average\n", snapshot.AverageReadLatency)
        fmt.Println("   - Consider USB vs native serial performance")
        fmt.Println("   - Check system load")
    }
    
    if snapshot.BytesPerSecond < 100 && snapshot.OperationsPerSecond > 10 {
        fmt.Println("📊 Many small operations detected")
        fmt.Println("   - Consider batching small requests")
        fmt.Println("   - Review communication protocol efficiency")
    }
    
    // Buffer pool optimization
    if snapshot.BufferPoolHitRatio < 85 {
        fmt.Printf("🔧 Buffer pool could be optimized: %.1f%% hit rate\n", snapshot.BufferPoolHitRatio)
        fmt.Println("   - Buffer pool sizes may need tuning")
    }
    
    fmt.Printf("✅ Overall Health Score: %.1f/100\n", snapshot.HealthScore)
}
```

## Write Timeout Configuration

### Configure Write Timeouts and Metrics Channel

```go
// In your configuration (config.json)
{
  "serial_port": {
    "port_name": "/dev/ttyUSB0",
    "baud_rate": 9600,
    "read_timeout": "1500ms",
    "write_timeout": "3000ms",  // Configure write timeout
    "metrics_channel_size": 50,  // Channel buffer size (default: 50)
    // ... other settings
  }
}
```

### Using Different Write Methods

```go
// Method 1: Use configured timeout (recommended)
n, err := service.Write([]byte("AT\r\n"))

// Method 2: Explicit timeout
n, err := service.WriteWithTimeout([]byte("AT\r\n"), 5*time.Second)

// Method 3: Context-based (most flexible)
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
n, err := service.WriteWithContext(ctx, []byte("AT\r\n"))

// All methods automatically record metrics
```

## Testing Metrics

```go
func TestSerialMetrics(t *testing.T) {
    // Create service for testing
    service := createTestService(t)
    
    // Reset metrics for clean test
    service.ResetMetrics()
    
    // Perform operations
    service.Write([]byte("test"))
    
    // Verify metrics
    snapshot := service.GetMetricsSnapshot()
    assert.Equal(t, int64(1), snapshot.TotalWrites)
    assert.Equal(t, 100.0, snapshot.WriteSuccessRate)
    assert.Equal(t, "healthy", snapshot.HealthStatus)
}
```

## Performance Considerations

The metrics system is designed for minimal performance impact:

- **Overhead**: ~60-85% overhead in tight loops (actual I/O overhead much lower)
- **Memory**: Atomic operations with no allocations in hot path
- **Thread Safety**: All metrics operations are thread-safe
- **Buffer Pool**: Integrated metrics don't affect pool performance
- **Channel Buffer**: Default size of 50 provides ~50-250 seconds of buffering depending on broadcast interval

### Disable Metrics if Needed

```go
// Disable metrics collection for maximum performance
service.DisableMetrics()

// Re-enable when needed
service.EnableMetrics()

// Check status
if service.IsMetricsEnabled() {
    // Metrics are being collected
}
```

## Troubleshooting

### Common Issues

1. **Metrics Not Recording**
   ```go
   // Ensure service is properly initialized
   if !service.IsMetricsEnabled() {
       service.EnableMetrics()
   }
   ```

2. **Metrics Channel Not Working**
   ```go
   // Ensure metrics broadcasting is started
   err := service.StartMetricsBroadcasting(time.Second)
   if err != nil {
       log.Printf("Failed to start metrics broadcasting: %v", err)
   }

   // Get the metrics channel
   metricsChannel, err := service.MetricsChannel()
   if err != nil {
       log.Printf("Failed to get metrics channel: %v", err)
   }
   ```

3. **High Memory Usage**
   ```go
   // Reset metrics periodically if needed
   service.ResetMetrics()
   ```

### Debug Information

```go
// Get detailed metrics for debugging
snapshot := service.GetMetricsSnapshot()
fmt.Printf("Debug Info:\n")
fmt.Printf("  Total Operations: %d reads, %d writes\n", 
    snapshot.TotalReads, snapshot.TotalWrites)
fmt.Printf("  Total Errors: %d\n", snapshot.TotalErrors)
fmt.Printf("  Max Latencies: read %v, write %v\n", 
    snapshot.MaxReadLatency, snapshot.MaxWriteLatency)
fmt.Printf("  Buffer Pool: %.1f%% hit ratio\n", 
    snapshot.BufferPoolHitRatio)
```

## Summary

This comprehensive metrics system provides deep visibility into serial communication health, enabling proactive monitoring, performance optimization, and automated troubleshooting for reliable ham radio station operation.

### Key Features:
- **Channel-based Architecture**: Decoupled from specific frontend frameworks
- **Configurable Buffer Size**: Default 50 snapshots (adjustable via `metrics_channel_size`)
- **Multiple Consumption Patterns**: Non-blocking, fan-out, filtering, and aggregation
- **Thread-safe Operations**: All metrics use atomic operations
- **Minimal Performance Impact**: Optimized for production use
- **Comprehensive Health Assessment**: 4-tier health status with numeric scoring

The refactored system removes Wails dependencies while maintaining all monitoring capabilities, providing greater flexibility for integration with various frontend technologies and architectures.