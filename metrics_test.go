package serial

import (
	"errors"
	"testing"
	"time"

	"github.com/7Q-Station-Manager/logging"
	"github.com/7Q-Station-Manager/types"
	gobug "go.bug.st/serial"
)

// Test helper for creating a service with metrics enabled
func createServiceWithMetrics(t *testing.T, cfg types.SerialConfig) *Service {
	t.Helper()

	// Apply defaults for unset values to ensure valid configuration
	if cfg.BaudRate == 0 {
		cfg.BaudRate = 9600
	}
	if cfg.DataBits == 0 {
		cfg.DataBits = 8
	}
	if cfg.StopBits == 0 {
		cfg.StopBits = 1
	}
	// Parity defaults to 0 which is ParityNone, so it's valid

	rig := types.RigConfig{Name: "TestRig", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	service := &Service{
		LoggerService: &logging.Service{},
		ConfigService: cs,
	}
	if err := service.Initialize(); err != nil {
		t.Fatalf("Failed to initialize service: %v", err)
	}
	return service
}

// ----- Core Metrics Tests -----

func TestMetrics_Initialization(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	service := createServiceWithMetrics(t, cfg)

	// Check that metrics are initialized
	if service.metrics == nil {
		t.Fatal("Metrics not initialized")
	}

	// Check that metrics are enabled by default
	if !service.metricsEnabled.Load() {
		t.Fatal("Metrics should be enabled by default")
	}

	// Check that buffer pool manager is initialized
	if service.bufferPoolManager == nil {
		t.Fatal("Buffer pool manager not initialized")
	}
}

func TestMetrics_EnableDisable(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	service := createServiceWithMetrics(t, cfg)

	// Test disable
	service.DisableMetrics()
	if service.IsMetricsEnabled() {
		t.Fatal("Metrics should be disabled")
	}

	// Test enable
	service.EnableMetrics()
	if !service.IsMetricsEnabled() {
		t.Fatal("Metrics should be enabled")
	}
}

func TestMetrics_ResetMetrics(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	service := createServiceWithMetrics(t, cfg)

	// Add some metrics
	service.metrics.ReadOperations.Add(10)
	service.metrics.WriteOperations.Add(5)
	service.metrics.BytesRead.Add(1000)

	// Reset metrics
	service.ResetMetrics()

	// Check that metrics are reset
	if service.metrics.ReadOperations.Load() != 0 {
		t.Fatal("Read operations should be reset to 0")
	}
	if service.metrics.WriteOperations.Load() != 0 {
		t.Fatal("Write operations should be reset to 0")
	}
	if service.metrics.BytesRead.Load() != 0 {
		t.Fatal("Bytes read should be reset to 0")
	}
}

// ----- Connection Metrics Tests -----

func TestMetrics_ConnectionSuccess(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	service := createServiceWithMetrics(t, cfg)

	// Reset metrics to ensure clean test
	service.ResetMetrics()

	mh := &mockHandle{}
	withPortsAndOpen(
		func() ([]string, error) { return []string{"COM1"}, nil },
		func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil },
		func() {
			err := service.Open()
			if err != nil {
				t.Fatalf("Open failed: %v", err)
			}

			// Check connection metrics
			if service.metrics.ConnectionAttempts.Load() != 1 {
				t.Fatalf("Expected 1 connection attempt, got %d", service.metrics.ConnectionAttempts.Load())
			}
			if service.metrics.SuccessfulConnects.Load() != 1 {
				t.Fatalf("Expected 1 successful connect, got %d", service.metrics.SuccessfulConnects.Load())
			}
			if service.metrics.ConnectionFailures.Load() != 0 {
				t.Fatalf("Expected 0 connection failures, got %d", service.metrics.ConnectionFailures.Load())
			}
			if service.metrics.CurrentConnections.Load() != 1 {
				t.Fatalf("Expected 1 current connection, got %d", service.metrics.CurrentConnections.Load())
			}
		})
}

func TestMetrics_ConnectionFailure(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	service := createServiceWithMetrics(t, cfg)

	// Reset metrics to ensure clean test
	service.ResetMetrics()

	withPortsAndOpen(
		func() ([]string, error) { return []string{"COM2"}, nil }, // Different port
		func(s string, m *gobug.Mode) (portHandle, error) { return &mockHandle{}, nil },
		func() {
			err := service.Open()
			if err == nil {
				t.Fatal("Expected open to fail")
			}

			// Check connection metrics
			if service.metrics.ConnectionAttempts.Load() != 1 {
				t.Fatalf("Expected 1 connection attempt, got %d", service.metrics.ConnectionAttempts.Load())
			}
			if service.metrics.SuccessfulConnects.Load() != 0 {
				t.Fatalf("Expected 0 successful connects, got %d", service.metrics.SuccessfulConnects.Load())
			}
			if service.metrics.ConnectionFailures.Load() != 1 {
				t.Fatalf("Expected 1 connection failure, got %d", service.metrics.ConnectionFailures.Load())
			}
		})
}

func TestMetrics_Disconnection(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	service := createServiceWithMetrics(t, cfg)

	mh := &mockHandle{}
	withPortsAndOpen(
		func() ([]string, error) { return []string{"COM1"}, nil },
		func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil },
		func() {
			// Open and then close
			if err := service.Open(); err != nil {
				t.Fatalf("Open failed: %v", err)
			}

			// Small delay to ensure time passage
			time.Sleep(10 * time.Millisecond)

			if err := service.Close(); err != nil {
				t.Fatalf("Close failed: %v", err)
			}

			// Check disconnection metrics
			if service.metrics.Disconnections.Load() != 1 {
				t.Fatalf("Expected 1 disconnection, got %d", service.metrics.Disconnections.Load())
			}
			if service.metrics.CurrentConnections.Load() != 0 {
				t.Fatalf("Expected 0 current connections, got %d", service.metrics.CurrentConnections.Load())
			}
			if service.metrics.TotalUptime.Load() == 0 {
				t.Fatal("Expected non-zero total uptime")
			}
		})
}

// ----- Read/Write Metrics Tests -----

func TestMetrics_SuccessfulWrite(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	service := createServiceWithMetrics(t, cfg)

	mh := &mockHandle{}
	withPortsAndOpen(
		func() ([]string, error) { return []string{"COM1"}, nil },
		func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil },
		func() {
			if err := service.Open(); err != nil {
				t.Fatalf("Open failed: %v", err)
			}

			data := []byte("test data")
			n, err := service.Write(data)
			if err != nil {
				t.Fatalf("Write failed: %v", err)
			}
			if n != len(data) {
				t.Fatalf("Expected %d bytes written, got %d", len(data), n)
			}

			// Check write metrics
			if service.metrics.WriteOperations.Load() != 1 {
				t.Fatalf("Expected 1 write operation, got %d", service.metrics.WriteOperations.Load())
			}
			if service.metrics.SuccessfulWrites.Load() != 1 {
				t.Fatalf("Expected 1 successful write, got %d", service.metrics.SuccessfulWrites.Load())
			}
			if service.metrics.BytesWritten.Load() != int64(len(data)) {
				t.Fatalf("Expected %d bytes written, got %d", len(data), service.metrics.BytesWritten.Load())
			}
			if service.metrics.WriteErrors.Load() != 0 {
				t.Fatalf("Expected 0 write errors, got %d", service.metrics.WriteErrors.Load())
			}
			if service.metrics.ConsecutiveFailures.Load() != 0 {
				t.Fatalf("Expected 0 consecutive failures, got %d", service.metrics.ConsecutiveFailures.Load())
			}
		})
}

func TestMetrics_FailedWrite(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	service := createServiceWithMetrics(t, cfg)

	writeErr := errors.New("write failed")
	mh := &mockHandle{writeErr: writeErr}
	withPortsAndOpen(
		func() ([]string, error) { return []string{"COM1"}, nil },
		func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil },
		func() {
			if err := service.Open(); err != nil {
				t.Fatalf("Open failed: %v", err)
			}

			_, err := service.Write([]byte("test"))
			if err == nil {
				t.Fatal("Expected write to fail")
			}

			// Check write metrics
			if service.metrics.WriteOperations.Load() != 1 {
				t.Fatalf("Expected 1 write operation, got %d", service.metrics.WriteOperations.Load())
			}
			if service.metrics.SuccessfulWrites.Load() != 0 {
				t.Fatalf("Expected 0 successful writes, got %d", service.metrics.SuccessfulWrites.Load())
			}
			if service.metrics.WriteErrors.Load() != 1 {
				t.Fatalf("Expected 1 write error, got %d", service.metrics.WriteErrors.Load())
			}
			if service.metrics.ConsecutiveFailures.Load() != 1 {
				t.Fatalf("Expected 1 consecutive failure, got %d", service.metrics.ConsecutiveFailures.Load())
			}
		})
}

func TestMetrics_SuccessfulRead(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	service := createServiceWithMetrics(t, cfg)

	testData := []byte("response data")
	mh := &mockHandle{readData: testData}
	withPortsAndOpen(
		func() ([]string, error) { return []string{"COM1"}, nil },
		func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil },
		func() {
			if err := service.Open(); err != nil {
				t.Fatalf("Open failed: %v", err)
			}

			buf := make([]byte, 100)
			n, err := service.Read(buf)
			if err != nil {
				t.Fatalf("Read failed: %v", err)
			}
			if n != len(testData) {
				t.Fatalf("Expected %d bytes read, got %d", len(testData), n)
			}

			// Check read metrics
			if service.metrics.ReadOperations.Load() != 1 {
				t.Fatalf("Expected 1 read operation, got %d", service.metrics.ReadOperations.Load())
			}
			if service.metrics.SuccessfulReads.Load() != 1 {
				t.Fatalf("Expected 1 successful read, got %d", service.metrics.SuccessfulReads.Load())
			}
			if service.metrics.BytesRead.Load() != int64(len(testData)) {
				t.Fatalf("Expected %d bytes read, got %d", len(testData), service.metrics.BytesRead.Load())
			}
			if service.metrics.ReadErrors.Load() != 0 {
				t.Fatalf("Expected 0 read errors, got %d", service.metrics.ReadErrors.Load())
			}
		})
}

func TestMetrics_WriteTimeout(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1", WriteTimeout: 50 * time.Millisecond}
	service := createServiceWithMetrics(t, cfg)

	mh := &mockHandle{writeDelay: 100 * time.Millisecond} // Longer than timeout
	withPortsAndOpen(
		func() ([]string, error) { return []string{"COM1"}, nil },
		func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil },
		func() {
			if err := service.Open(); err != nil {
				t.Fatalf("Open failed: %v", err)
			}

			_, err := service.Write([]byte("test"))
			if !errors.Is(err, ErrWriteTimeout) {
				t.Fatalf("Expected ErrWriteTimeout, got: %v", err)
			}

			// Check timeout metrics
			if service.metrics.WriteTimeouts.Load() != 1 {
				t.Fatalf("Expected 1 write timeout, got %d", service.metrics.WriteTimeouts.Load())
			}
			if service.metrics.TimeoutErrors.Load() != 1 {
				t.Fatalf("Expected 1 timeout error, got %d", service.metrics.TimeoutErrors.Load())
			}
		})
}

// ----- Buffer Validation Metrics Tests -----

func TestMetrics_BufferValidationErrors(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	service := createServiceWithMetrics(t, cfg)

	mh := &mockHandle{}
	withPortsAndOpen(
		func() ([]string, error) { return []string{"COM1"}, nil },
		func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil },
		func() {
			if err := service.Open(); err != nil {
				t.Fatalf("Open failed: %v", err)
			}

			// Test nil buffer
			_, err := service.Write(nil)
			if !errors.Is(err, ErrInvalidBuffer) {
				t.Fatalf("Expected ErrInvalidBuffer, got: %v", err)
			}

			// Test empty buffer
			_, err = service.Write([]byte{})
			if !errors.Is(err, ErrInvalidBuffer) {
				t.Fatalf("Expected ErrInvalidBuffer, got: %v", err)
			}

			// Test oversized buffer
			largeBuffer := make([]byte, MaxBufferSize+1)
			_, err = service.Write(largeBuffer)
			if !errors.Is(err, ErrBufferTooLarge) {
				t.Fatalf("Expected ErrBufferTooLarge, got: %v", err)
			}

			// Check buffer error metrics
			if service.metrics.BufferErrors.Load() != 3 {
				t.Fatalf("Expected 3 buffer errors, got %d", service.metrics.BufferErrors.Load())
			}
		})
}

// ----- Metrics Snapshot Tests -----

func TestMetrics_Snapshot(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	service := createServiceWithMetrics(t, cfg)

	mh := &mockHandle{readData: []byte("test")}
	withPortsAndOpen(
		func() ([]string, error) { return []string{"COM1"}, nil },
		func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil },
		func() {
			if err := service.Open(); err != nil {
				t.Fatalf("Open failed: %v", err)
			}

			// Perform some operations
			service.Write([]byte("hello"))
			buf := make([]byte, 10)
			service.Read(buf)

			// Get snapshot
			snapshot := service.GetMetricsSnapshot()

			// Check basic fields
			if !snapshot.IsConnected {
				t.Fatal("Expected IsConnected to be true")
			}
			if snapshot.HealthStatus != string(HealthStatusHealthy) {
				t.Fatalf("Expected healthy status, got %s", snapshot.HealthStatus)
			}
			if snapshot.HealthScore < 90 {
				t.Fatalf("Expected high health score, got %.1f", snapshot.HealthScore)
			}
			if snapshot.TotalReads != 1 {
				t.Fatalf("Expected 1 read operation, got %d", snapshot.TotalReads)
			}
			if snapshot.TotalWrites != 1 {
				t.Fatalf("Expected 1 write operation, got %d", snapshot.TotalWrites)
			}
			if snapshot.ReadSuccessRate != 100.0 {
				t.Fatalf("Expected 100%% read success rate, got %.1f", snapshot.ReadSuccessRate)
			}
			if snapshot.WriteSuccessRate != 100.0 {
				t.Fatalf("Expected 100%% write success rate, got %.1f", snapshot.WriteSuccessRate)
			}
		})
}

func TestMetrics_SnapshotUnhealthyConditions(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	service := createServiceWithMetrics(t, cfg)

	mh := &mockHandle{}
	withPortsAndOpen(
		func() ([]string, error) { return []string{"COM1"}, nil },
		func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil },
		func() {
			if err := service.Open(); err != nil {
				t.Fatalf("Open failed: %v", err)
			}

			// Simulate consecutive failures
			service.metrics.ConsecutiveFailures.Store(6) // Above unhealthy threshold
			service.metrics.ReadOperations.Store(1)
			service.metrics.WriteOperations.Store(1)

			snapshot := service.GetMetricsSnapshot()
			if snapshot.HealthStatus != string(HealthStatusUnhealthy) {
				t.Fatalf("Expected unhealthy status, got %s", snapshot.HealthStatus)
			}
			if snapshot.HealthScore > 50 {
				t.Fatalf("Expected low health score, got %.1f", snapshot.HealthScore)
			}
		})
}

// ----- Metrics Emitter Tests -----

func TestMetricsBroadcaster_StartStop(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1", MetricsChannelSize: 10}
	service := createServiceWithMetrics(t, cfg)

	// Start broadcaster
	err := service.StartMetricsBroadcasting(100 * time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to start metrics broadcasting: %v", err)
	}
	if service.metricsBroadcaster == nil {
		t.Fatal("Metrics broadcaster should be started")
	}
	if !service.metricsBroadcaster.enabled.Load() {
		t.Fatal("Metrics broadcaster should be enabled")
	}

	// Stop broadcaster
	service.StopMetricsBroadcasting()
	if service.metricsBroadcaster != nil {
		t.Fatal("Metrics broadcaster should be stopped")
	}
}

func TestMetricsBroadcaster_ImmediateBroadcast(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1", MetricsChannelSize: 10}
	service := createServiceWithMetrics(t, cfg)

	// Start broadcaster
	err := service.StartMetricsBroadcasting(1 * time.Second)
	if err != nil {
		t.Fatalf("Failed to start metrics broadcasting: %v", err)
	}
	defer service.StopMetricsBroadcasting()

	// Get metrics channel
	metricsChannel, err := service.MetricsChannel()
	if err != nil {
		t.Fatalf("Failed to get metrics channel: %v", err)
	}

	// Broadcast immediately and verify we get metrics
	service.BroadcastMetricsImmediate()

	// Should receive metrics within reasonable time
	select {
	case snapshot := <-metricsChannel:
		if snapshot.Timestamp.IsZero() {
			t.Fatal("Received invalid metrics snapshot")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Did not receive metrics within timeout")
	}
}

// ----- Buffer Pool Integration Tests -----

func TestMetrics_BufferPoolIntegration(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	service := createServiceWithMetrics(t, cfg)

	// Reset buffer pool stats for clean test
	service.ResetBufferPoolStats()

	// Use buffer pool through service instance
	buf1, cleanup1 := service.bufferPoolManager.GetPooledBuffer(256)
	if len(buf1) != 256 {
		t.Fatalf("Expected buffer size 256, got %d", len(buf1))
	}
	cleanup1()

	buf2, cleanup2 := service.bufferPoolManager.GetPooledBuffer(512)
	if len(buf2) != 512 {
		t.Fatalf("Expected buffer size 512, got %d", len(buf2))
	}
	cleanup2()

	// Check that metrics were recorded
	if service.metrics.BufferPoolHits.Load() != 2 {
		t.Fatalf("Expected 2 buffer pool hits, got %d", service.metrics.BufferPoolHits.Load())
	}

	// Test oversized buffer (should record miss)
	bigBuf, cleanup3 := service.bufferPoolManager.GetPooledBuffer(MaxBufferSize + 1)
	if len(bigBuf) != MaxBufferSize+1 {
		t.Fatalf("Expected buffer size %d, got %d", MaxBufferSize+1, len(bigBuf))
	}
	cleanup3()

	if service.metrics.BufferPoolMisses.Load() != 1 {
		t.Fatalf("Expected 1 buffer pool miss, got %d", service.metrics.BufferPoolMisses.Load())
	}
}

// ----- Concurrent Access Tests -----

func TestMetrics_ConcurrentAccess(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	service := createServiceWithMetrics(t, cfg)

	mh := &mockHandle{readData: []byte("test")}
	withPortsAndOpen(
		func() ([]string, error) { return []string{"COM1"}, nil },
		func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil },
		func() {
			if err := service.Open(); err != nil {
				t.Fatalf("Open failed: %v", err)
			}

			// Perform concurrent operations
			done := make(chan bool, 2)

			// Concurrent writes
			go func() {
				for i := 0; i < 10; i++ {
					service.Write([]byte("test"))
				}
				done <- true
			}()

			// Concurrent reads
			go func() {
				buf := make([]byte, 10)
				for i := 0; i < 10; i++ {
					service.Read(buf)
				}
				done <- true
			}()

			// Wait for completion
			<-done
			<-done

			// Check that metrics are consistent
			totalOps := service.metrics.ReadOperations.Load() + service.metrics.WriteOperations.Load()
			if totalOps != 20 {
				t.Fatalf("Expected 20 total operations, got %d", totalOps)
			}
		})
}

// ----- Performance Tests -----

func TestMetrics_PerformanceImpact(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	service := createServiceWithMetrics(t, cfg)

	mh := &mockHandle{}
	withPortsAndOpen(
		func() ([]string, error) { return []string{"COM1"}, nil },
		func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil },
		func() {
			if err := service.Open(); err != nil {
				t.Fatalf("Open failed: %v", err)
			}

			// Measure with metrics enabled
			start := time.Now()
			for i := 0; i < 1000; i++ {
				service.Write([]byte("test"))
			}
			withMetricsDuration := time.Since(start)

			// Disable metrics and measure again
			service.DisableMetrics()
			start = time.Now()
			for i := 0; i < 1000; i++ {
				service.Write([]byte("test"))
			}
			withoutMetricsDuration := time.Since(start)

			// Metrics should not add excessive overhead (< 1000% increase for tight loops)
			// In real-world usage, the overhead will be much lower relative to actual I/O
			// The higher threshold accounts for debug logging overhead in test environments
			overhead := float64(withMetricsDuration-withoutMetricsDuration) / float64(withoutMetricsDuration)
			if overhead > 10.0 {
				t.Fatalf("Metrics overhead too high: %.1f%%", overhead*100)
			}

			t.Logf("Metrics overhead: %.1f%% (%v vs %v)", overhead*100, withMetricsDuration, withoutMetricsDuration)
		})
}
