package serial

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/7Q-Station-Manager/types"
)

// Metrics tracks comprehensive serial communication health statistics
type Metrics struct {
	// Connection Statistics
	ConnectionAttempts  atomic.Int64 // Total connection attempts
	SuccessfulConnects  atomic.Int64 // Successful connections
	ConnectionFailures  atomic.Int64 // Failed connections
	Disconnections      atomic.Int64 // Total disconnects
	CurrentConnections  atomic.Int64 // Currently active connections
	LastConnectTime     atomic.Int64 // Unix timestamp of last connect
	LastDisconnectTime  atomic.Int64 // Unix timestamp of last disconnect
	TotalUptime         atomic.Int64 // Total connected time in nanoseconds
	ConnectionStartTime atomic.Int64 // When current connection started

	// Read Operations
	ReadOperations  atomic.Int64 // Total read attempts
	SuccessfulReads atomic.Int64 // Successful reads
	ReadTimeouts    atomic.Int64 // Read timeout failures
	ReadErrors      atomic.Int64 // Other read errors
	BytesRead       atomic.Int64 // Total bytes read
	TotalReadTime   atomic.Int64 // Total time spent reading (ns)
	MaxReadTime     atomic.Int64 // Slowest read operation (ns)
	LastReadTime    atomic.Int64 // Timestamp of last read

	// Write Operations
	WriteOperations  atomic.Int64 // Total write attempts
	SuccessfulWrites atomic.Int64 // Successful writes
	WriteTimeouts    atomic.Int64 // Write timeout failures
	WriteErrors      atomic.Int64 // Other write errors
	BytesWritten     atomic.Int64 // Total bytes written
	TotalWriteTime   atomic.Int64 // Total time spent writing (ns)
	MaxWriteTime     atomic.Int64 // Slowest write operation (ns)
	LastWriteTime    atomic.Int64 // Timestamp of last write

	// Buffer Pool Metrics
	BufferPoolHits   atomic.Int64 // Buffer pool cache hits
	BufferPoolMisses atomic.Int64 // Buffer pool cache misses

	// Error Categories
	InitializationErrors atomic.Int64 // Service init failures
	ConfigurationErrors  atomic.Int64 // Config-related errors
	PortValidationErrors atomic.Int64 // Invalid port errors
	BufferErrors         atomic.Int64 // Buffer validation errors
	TimeoutErrors        atomic.Int64 // All timeout errors
	HardwareErrors       atomic.Int64 // Hardware/driver errors

	// Health Indicators
	ConsecutiveFailures atomic.Int64 // Consecutive operation failures
	LastErrorTime       atomic.Int64 // Timestamp of last error
	ErrorRate           atomic.Int64 // Errors per thousand operations
}

// HealthStatus represents the overall health of serial communication
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusDown      HealthStatus = "down"
)

// MetricsBroadcaster handles channel-based metrics broadcasting
type MetricsBroadcaster struct {
	metricsChannel   chan types.SerialMetricsSnapshot
	broadcastTicker  *time.Ticker
	enabled          atomic.Bool
	stopCh           chan struct{}
	emissionInterval time.Duration
	stopOnce         sync.Once // Prevent double-close race
}

// NewMetricsBroadcaster creates a new metrics broadcaster with channel-based distribution
func NewMetricsBroadcaster(channelSize int64, interval time.Duration) *MetricsBroadcaster {
	return &MetricsBroadcaster{
		metricsChannel:   make(chan types.SerialMetricsSnapshot, channelSize),
		stopCh:           make(chan struct{}),
		emissionInterval: interval,
	}
}

// Start begins broadcasting metrics to the channel
func (mb *MetricsBroadcaster) Start(service *Service) {
	if !mb.enabled.CompareAndSwap(false, true) {
		return // Already running
	}

	mb.broadcastTicker = time.NewTicker(mb.emissionInterval)

	go func() {
		defer mb.broadcastTicker.Stop()

		for {
			select {
			case <-mb.stopCh:
				return
			case <-mb.broadcastTicker.C:
				mb.broadcastMetrics(service)
			}
		}
	}()
}

// Stop stops broadcasting metrics
func (mb *MetricsBroadcaster) Stop() {
	if mb.enabled.CompareAndSwap(true, false) {
		mb.stopOnce.Do(func() {
			close(mb.stopCh)
			close(mb.metricsChannel)
		})
	}
}

// BroadcastImmediate sends metrics immediately (for critical events)
func (mb *MetricsBroadcaster) BroadcastImmediate(service *Service) {
	mb.broadcastMetrics(service)
}

// GetMetricsChannel returns the read-only metrics channel for consumers
func (mb *MetricsBroadcaster) GetMetricsChannel() <-chan types.SerialMetricsSnapshot {
	return mb.metricsChannel
}

func (mb *MetricsBroadcaster) broadcastMetrics(service *Service) {
	// Check if broadcaster is still enabled to prevent sending to closed channel
	if !mb.enabled.Load() {
		return
	}

	snapshot := service.GetMetricsSnapshot()

	// Non-blocking send to avoid goroutine blocking, with additional safety check
	select {
	case mb.metricsChannel <- *snapshot:
		// Successfully sent
	default:
		// Channel full or closed, skip this broadcast
	}
}

// Metrics calculation methods
func (m *Metrics) calculateConnectionSuccessRate() float64 {
	attempts := m.ConnectionAttempts.Load()
	if attempts == 0 {
		return 100.0
	}
	successes := m.SuccessfulConnects.Load()
	return float64(successes) / float64(attempts) * 100
}

func (m *Metrics) calculateReadSuccessRate() float64 {
	reads := m.ReadOperations.Load()
	if reads == 0 {
		return 100.0
	}
	successes := m.SuccessfulReads.Load()
	return float64(successes) / float64(reads) * 100
}

func (m *Metrics) calculateWriteSuccessRate() float64 {
	writes := m.WriteOperations.Load()
	if writes == 0 {
		return 100.0
	}
	successes := m.SuccessfulWrites.Load()
	return float64(successes) / float64(writes) * 100
}

func (m *Metrics) calculateAverageReadLatency() time.Duration {
	reads := m.ReadOperations.Load()
	if reads == 0 {
		return 0
	}
	totalTime := m.TotalReadTime.Load()
	return time.Duration(totalTime / reads)
}

func (m *Metrics) calculateAverageWriteLatency() time.Duration {
	writes := m.WriteOperations.Load()
	if writes == 0 {
		return 0
	}
	totalTime := m.TotalWriteTime.Load()
	return time.Duration(totalTime / writes)
}

func (m *Metrics) calculateThroughput(isConnected bool, connectionStartTime int64) float64 {
	if !isConnected || connectionStartTime == 0 {
		return 0.0
	}

	now := time.Now().UnixNano()
	duration := now - connectionStartTime
	if duration <= 0 {
		return 0.0
	}

	totalBytes := m.BytesRead.Load() + m.BytesWritten.Load()
	seconds := float64(duration) / float64(time.Second)
	return float64(totalBytes) / seconds
}

func (m *Metrics) calculateOperationsPerSecond(isConnected bool, connectionStartTime int64) float64 {
	if !isConnected || connectionStartTime == 0 {
		return 0.0
	}

	now := time.Now().UnixNano()
	duration := now - connectionStartTime
	if duration <= 0 {
		return 0.0
	}

	totalOps := m.ReadOperations.Load() + m.WriteOperations.Load()
	seconds := float64(duration) / float64(time.Second)
	return float64(totalOps) / seconds
}

func (m *Metrics) calculateTimeoutRate() float64 {
	totalOps := m.ReadOperations.Load() + m.WriteOperations.Load()
	if totalOps == 0 {
		return 0.0
	}
	totalTimeouts := m.ReadTimeouts.Load() + m.WriteTimeouts.Load()
	return float64(totalTimeouts) / float64(totalOps) * 100
}

func (m *Metrics) calculateBufferPoolHitRatio() float64 {
	total := m.BufferPoolHits.Load() + m.BufferPoolMisses.Load()
	if total == 0 {
		return 100.0
	}
	return float64(m.BufferPoolHits.Load()) / float64(total) * 100
}

func (m *Metrics) calculateUptime(isConnected bool, connectionStartTime int64) float64 {
	if !isConnected || connectionStartTime == 0 {
		return 0.0
	}

	now := time.Now().UnixNano()
	duration := now - connectionStartTime
	if duration <= 0 {
		return 0.0
	}

	return float64(duration) / float64(time.Second)
}

func (m *Metrics) assessHealthStatus(snapshot *types.SerialMetricsSnapshot) HealthStatus {
	if !snapshot.IsConnected {
		return HealthStatusDown
	}

	// Check for critical issues
	if snapshot.ErrorRate > 50.0 || snapshot.ConsecutiveFailures > 5 {
		return HealthStatusUnhealthy
	}

	// Check for performance degradation
	if snapshot.ErrorRate > 10.0 || snapshot.TimeoutRate > 20.0 || snapshot.ConsecutiveFailures > 3 {
		return HealthStatusDegraded
	}

	return HealthStatusHealthy
}

func (m *Metrics) calculateHealthScore(snapshot *types.SerialMetricsSnapshot) float64 {
	if !snapshot.IsConnected {
		return 0.0
	}

	score := 100.0

	// Deduct for errors
	score -= snapshot.ErrorRate * 2

	// Deduct for timeouts
	score -= snapshot.TimeoutRate

	// Deduct for consecutive failures (more severe penalty)
	score -= float64(snapshot.ConsecutiveFailures) * 10

	// Ensure score doesn't go below 0
	if score < 0 {
		score = 0
	}

	return score
}
