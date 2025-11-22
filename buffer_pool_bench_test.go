package serial

import (
	"fmt"
	"testing"

	"github.com/7Q-Station-Manager/logging"
	"github.com/7Q-Station-Manager/types"
)

// BenchmarkReadWithoutPooling tests traditional allocation approach
func BenchmarkReadWithoutPooling(b *testing.B) {
	service := setupBenchService(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := make([]byte, 1024) // Fresh allocation each time
		_, _ = service.Read(buf)
	}
}

// BenchmarkReadWithPooling tests buffer pooling approach
func BenchmarkReadWithPooling(b *testing.B) {
	service := setupBenchService(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = service.ReadWithPooling(1024) // Uses pooled buffers internally
	}
}

// BenchmarkGetPooledBuffer measures buffer pool allocation performance
func BenchmarkGetPooledBuffer(b *testing.B) {
	// Create a service instance for the benchmark
	service := setupBenchService(b)

	// Reset pool stats for clean benchmark
	service.ResetBufferPoolStats()

	sizes := []int{256, 1024, 4096}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				buf, cleanup := service.bufferPoolManager.GetPooledBuffer(size)
				_ = buf
				cleanup()
			}
		})
	}
}

// BenchmarkDirectAllocation measures direct allocation performance for comparison
func BenchmarkDirectAllocation(b *testing.B) {
	sizes := []int{256, 1024, 4096}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				buf := make([]byte, size)
				_ = buf
				// buf goes out of scope, eligible for GC
			}
		})
	}
}

// BenchmarkHighFrequencyReads simulates contest logging scenario
func BenchmarkHighFrequencyReads(b *testing.B) {
	service := setupBenchService(b)

	scenarios := []struct {
		name     string
		withPool bool
	}{
		{"WithoutPooling", false},
		{"WithPooling", true},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				// Simulate 10 rapid reads (typical for contest logging burst)
				for j := 0; j < 10; j++ {
					if scenario.withPool {
						_, _ = service.ReadWithPooling(128)
					} else {
						buf := make([]byte, 128)
						_, _ = service.Read(buf)
					}
				}
			}
		})
	}
}

// setupBenchService creates a service instance for benchmarking
func setupBenchService(b *testing.B) *Service {
	b.Helper()

	// Create a minimal service setup for benchmarking without using createConfigService
	cfg := types.SerialConfig{PortName: "COM1"}
	service := &Service{
		LoggerService: &logging.Service{},
		Config:        &cfg,
	}

	// Initialize the service
	service.metrics = &Metrics{}
	service.metricsEnabled.Store(true)
	service.bufferPoolManager = NewBufferPoolManager(service)
	service.initialized.Store(true)

	return service
}
