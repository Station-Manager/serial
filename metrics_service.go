package serial

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/7Q-Station-Manager/types"
)

// Metrics accessor and management methods for Service

// GetMetrics returns the current metrics instance
func (p *Service) GetMetrics() *Metrics {
	if p.metrics == nil {
		return &Metrics{} // Return empty metrics if not initialized
	}
	return p.metrics
}

// GetMetricsSnapshot creates a comprehensive snapshot for frontend consumption
func (p *Service) GetMetricsSnapshot() *types.SerialMetricsSnapshot {
	if p.metrics == nil {
		return &types.SerialMetricsSnapshot{
			Timestamp:    time.Now(),
			HealthStatus: string(HealthStatusDown),
			HealthScore:  0,
		}
	}

	now := time.Now()
	isConnected := p.isOpen.Load()
	connectionStartTime := p.metrics.ConnectionStartTime.Load()

	snapshot := &types.SerialMetricsSnapshot{
		Timestamp:   now,
		IsConnected: isConnected,
	}

	// Calculate rates and averages
	snapshot.ConnectionSuccess = p.metrics.calculateConnectionSuccessRate()
	snapshot.ReadSuccessRate = p.metrics.calculateReadSuccessRate()
	snapshot.WriteSuccessRate = p.metrics.calculateWriteSuccessRate()
	snapshot.AverageReadLatency = p.metrics.calculateAverageReadLatency()
	snapshot.AverageWriteLatency = p.metrics.calculateAverageWriteLatency()
	snapshot.BytesPerSecond = p.metrics.calculateThroughput(isConnected, connectionStartTime)
	snapshot.OperationsPerSecond = p.metrics.calculateOperationsPerSecond(isConnected, connectionStartTime)
	snapshot.TimeoutRate = p.metrics.calculateTimeoutRate()
	snapshot.ErrorRate = float64(p.metrics.ErrorRate.Load()) / 10.0 // Convert from per-1000 to percentage
	snapshot.ConsecutiveFailures = p.metrics.ConsecutiveFailures.Load()
	snapshot.BufferPoolHitRatio = p.metrics.calculateBufferPoolHitRatio()
	snapshot.UptimeSeconds = p.metrics.calculateUptime(isConnected, connectionStartTime)

	// Detailed counts for debugging
	snapshot.TotalReads = p.metrics.ReadOperations.Load()
	snapshot.TotalWrites = p.metrics.WriteOperations.Load()
	snapshot.TotalBytesRead = p.metrics.BytesRead.Load()
	snapshot.TotalBytesWritten = p.metrics.BytesWritten.Load()
	snapshot.TotalErrors = p.metrics.ReadErrors.Load() + p.metrics.WriteErrors.Load()
	snapshot.TotalTimeouts = p.metrics.ReadTimeouts.Load() + p.metrics.WriteTimeouts.Load()
	snapshot.MaxReadLatency = time.Duration(p.metrics.MaxReadTime.Load())
	snapshot.MaxWriteLatency = time.Duration(p.metrics.MaxWriteTime.Load())

	// Health assessment
	health := p.metrics.assessHealthStatus(snapshot)
	snapshot.HealthStatus = string(health)
	snapshot.HealthScore = p.metrics.calculateHealthScore(snapshot)

	return snapshot
}

// EnableMetrics turns on metrics collection
func (p *Service) EnableMetrics() {
	p.metricsEnabled.Store(true)
}

// DisableMetrics turns off metrics collection
func (p *Service) DisableMetrics() {
	p.metricsEnabled.Store(false)
}

// IsMetricsEnabled returns whether metrics collection is enabled
func (p *Service) IsMetricsEnabled() bool {
	return p.metricsEnabled.Load()
}

// ResetMetrics clears all metrics (useful for testing)
func (p *Service) ResetMetrics() {
	if p.metrics != nil {
		p.metrics = &Metrics{}
	}
}

// StartMetricsBroadcasting begins broadcasting metrics to the channel
func (p *Service) StartMetricsBroadcasting(interval time.Duration) error {
	if !p.initialized.Load() {
		return ErrNotInitialized
	}

	if p.metricsBroadcaster != nil {
		p.metricsBroadcaster.Stop()
	}

	channelSize := p.Config.MetricsChannelSize
	if channelSize <= 0 {
		channelSize = 50 // Default channel size - allows buffering ~50-250 seconds depending on interval
	} else if channelSize > 10000 {
		// Prevent excessive memory allocation for metrics channel
		return fmt.Errorf("metrics channel size too large: %d (max 10000)", channelSize)
	}

	p.metricsBroadcaster = NewMetricsBroadcaster(channelSize, interval)
	p.metricsBroadcaster.Start(p)
	return nil
}

// StopMetricsBroadcasting stops broadcasting metrics
func (p *Service) StopMetricsBroadcasting() {
	if p.metricsBroadcaster != nil {
		p.metricsBroadcaster.Stop()
		p.metricsBroadcaster = nil
	}
}

// BroadcastMetricsImmediate sends current metrics to channel immediately
func (p *Service) BroadcastMetricsImmediate() {
	if p.metricsBroadcaster != nil {
		p.metricsBroadcaster.BroadcastImmediate(p)
	}
}

// MetricsChannel returns the read-only metrics channel for consumers (similar to cat.StatusChannel)
func (p *Service) MetricsChannel() (<-chan types.SerialMetricsSnapshot, error) {
	if !p.initialized.Load() {
		return nil, ErrNotInitialized
	}
	if p.metricsBroadcaster == nil {
		return nil, errors.New("metrics broadcasting not started")
	}
	return p.metricsBroadcaster.GetMetricsChannel(), nil
}

// Internal metrics recording methods

func (p *Service) recordWriteMetrics(bytesWritten int, err error, duration time.Duration) {
	if p.metrics == nil {
		return
	}

	// Record that a write operation occurred
	p.metrics.WriteOperations.Add(1)
	p.metrics.LastWriteTime.Store(time.Now().Unix())
	p.metrics.TotalWriteTime.Add(duration.Nanoseconds())

	// Update max write time
	for {
		current := p.metrics.MaxWriteTime.Load()
		if duration.Nanoseconds() <= current {
			break
		}
		if p.metrics.MaxWriteTime.CompareAndSwap(current, duration.Nanoseconds()) {
			break
		}
	}

	if err != nil {
		p.metrics.WriteErrors.Add(1)
		p.incrementConsecutiveFailures()
		p.recordErrorMetrics(err)
	} else {
		p.metrics.SuccessfulWrites.Add(1)
		p.metrics.BytesWritten.Add(int64(bytesWritten))
		p.resetConsecutiveFailures()
	}
}

func (p *Service) recordReadMetrics(bytesRead int, err error, duration time.Duration) {
	if p.metrics == nil {
		return
	}

	// Record that a read operation occurred
	p.metrics.ReadOperations.Add(1)
	p.metrics.LastReadTime.Store(time.Now().Unix())
	p.metrics.TotalReadTime.Add(duration.Nanoseconds())

	// Update max read time
	for {
		current := p.metrics.MaxReadTime.Load()
		if duration.Nanoseconds() <= current {
			break
		}
		if p.metrics.MaxReadTime.CompareAndSwap(current, duration.Nanoseconds()) {
			break
		}
	}

	if err != nil {
		p.metrics.ReadErrors.Add(1)
		p.incrementConsecutiveFailures()
		p.recordErrorMetrics(err)
	} else {
		p.metrics.SuccessfulReads.Add(1)
		p.metrics.BytesRead.Add(int64(bytesRead))
		p.resetConsecutiveFailures()
	}
}

func (p *Service) recordErrorMetrics(err error) {
	if p.metrics == nil {
		return
	}

	p.metrics.LastErrorTime.Store(time.Now().Unix())

	// Categorize errors
	if errors.Is(err, ErrWriteTimeout) || errors.Is(err, context.DeadlineExceeded) {
		p.metrics.WriteTimeouts.Add(1)
		p.metrics.TimeoutErrors.Add(1)
	} else if errors.Is(err, context.Canceled) {
		// Context cancellation is not necessarily an error
	} else if errors.Is(err, ErrInvalidBuffer) || errors.Is(err, ErrBufferTooLarge) {
		p.metrics.BufferErrors.Add(1)
	} else if errors.Is(err, ErrInvalidPortName) {
		p.metrics.PortValidationErrors.Add(1)
	} else {
		p.metrics.HardwareErrors.Add(1)
	}

	// Update error rate (errors per 1000 operations)
	totalOps := p.metrics.ReadOperations.Load() + p.metrics.WriteOperations.Load()
	totalErrors := p.metrics.ReadErrors.Load() + p.metrics.WriteErrors.Load()
	if totalOps > 0 {
		errorRate := (totalErrors * 1000) / totalOps
		p.metrics.ErrorRate.Store(errorRate)
	}
}

func (p *Service) incrementConsecutiveFailures() {
	if p.metrics != nil {
		p.metrics.ConsecutiveFailures.Add(1)
	}
}

func (p *Service) resetConsecutiveFailures() {
	if p.metrics != nil {
		p.metrics.ConsecutiveFailures.Store(0)
	}
}
