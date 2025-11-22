package serial

import (
	"context"
	"errors"
	"fmt"
	"github.com/Station-Manager/config"
	"github.com/Station-Manager/logging"
	"github.com/Station-Manager/types"
	gobug "go.bug.st/serial"
	"go.uber.org/atomic"
	"sync"
	"time"
)

// allow tests to override external dependencies
var (
	openPort     = func(name string, mode *gobug.Mode) (portHandle, error) { return gobug.Open(name, mode) }
	getPortsList = gobug.GetPortsList
)

const (
	ServiceName = types.SerialServiceName

	// MaxBufferSize defines the maximum allowed buffer size for Read/Write operations
	// This prevents excessive memory allocation and potential DoS attacks.
	// 64KB is sufficient for most serial protocols (Modbus, CAT control, etc.)
	// and aligns with typical OS serial buffer sizes.
	MaxBufferSize = 64 * 1024 // 64KB

	// AbsoluteMaxBufferSize defines the absolute maximum buffer size that can be allocated
	// This prevents memory exhaustion attacks even for non-pooled allocations.
	// 1MB hard limit prevents OOM conditions on resource-constrained systems.
	// SECURITY: Exceeding this limit returns ErrBufferExceedsAbsoluteLimit
	AbsoluteMaxBufferSize = 1024 * 1024 // 1MB

	// MinBufferSize defines the minimum sensible buffer size
	// Buffers smaller than this are likely configuration errors
	//	MinBufferSize = 1 // Allow single-byte reads for specialized protocols
)

type portHandle interface {
	SetReadTimeout(timeout time.Duration) error
	SetDTR(bool) error
	SetRTS(bool) error
	Write([]byte) (int, error)
	Read([]byte) (int, error)
	Close() error
}

// writeOperation represents a queued write operation
type writeOperation struct {
	data     []byte
	ctx      context.Context
	resultCh chan writeResult
}

// writeResult holds the result of a write operation
type writeResult struct {
	n   int
	err error
}

type Service struct {
	LoggerService *logging.Service `di.inject:"logger"`
	ConfigService *config.Service  `di.inject:"config"`
	Config        *types.SerialConfig
	initialized   atomic.Bool
	isOpen        atomic.Bool
	mode          *gobug.Mode
	handle        portHandle
	mu            sync.RWMutex

	// Write queue for serial write operations
	writeQueue chan *writeOperation
	queueOnce  sync.Once
	queueDone  chan struct{}

	// Queue management mutex - protects writeQueue access during initialization/cleanup
	queueMu sync.RWMutex

	// Channel closing protection
	closeOnce      sync.Once
	queueCloseOnce sync.Once
	queueClosed    atomic.Bool // Track if queue is closed

	// Goroutine coordination
	writeGoroutineDone chan struct{} // Signals when write goroutine has exited
	writeGoroutineOnce sync.Once     // Ensures goroutine is started only once

	// Instance-specific buffer pool manager
	bufferPoolManager *BufferPoolManager

	// Metrics
	metrics            *Metrics
	metricsEnabled     atomic.Bool
	metricsBroadcaster *MetricsBroadcaster

	// Config synchronization - protects Config pointer and related fields
	configMu sync.RWMutex

	// Initialization synchronization - ensures Initialize() is called only once
	initOnce sync.Once
	initErr  error
}

func (p *Service) Initialize() (err error) {
	// Use sync.Once to ensure initialization happens only once, even with concurrent calls
	p.initOnce.Do(func() {
		p.initErr = p.doInitialize()
	})

	// Return the stored initialization result
	return p.initErr
}

// doInitialize performs the actual initialization logic
func (p *Service) doInitialize() (err error) {
	// Check if already initialized (should not happen with sync.Once, but defensive programming)
	if p.initialized.Load() {
		return nil
	}

	defer func() {
		if err != nil {
			// If initialization failed, record metrics if possible
			if p.metricsEnabled.Load() && p.metrics != nil {
				p.metrics.InitializationErrors.Add(1)
			}
		} else {
			// Set initialized flag only on successful completion
			p.initialized.Store(true)
		}
	}()

	// Initialize metrics first
	p.metrics = &Metrics{}
	p.metricsEnabled.Store(true) // Enable by default

	// Initialize instance-specific buffer pool manager
	p.bufferPoolManager = NewBufferPoolManager(p)

	if p.ConfigService == nil {
		if p.metrics != nil {
			p.metrics.InitializationErrors.Add(1)
		}
		return errors.New("application config has not been set/injected")
	}
	if p.LoggerService == nil {
		if p.metrics != nil {
			p.metrics.InitializationErrors.Add(1)
		}
		return errors.New("logger has not been set/injected")
	}

	// Get config with proper synchronization
	cfg, err := p.serialPortConfig()
	if err != nil {
		if p.metrics != nil {
			p.metrics.ConfigurationErrors.Add(1)
		}
		return fmt.Errorf("getting serial port config: %w", err)
	}

	// Set config safely
	p.setConfigSafe(cfg)

	// Validate configuration parameters
	if err = ValidateConfig(cfg); err != nil {
		if p.metrics != nil {
			p.metrics.ConfigurationErrors.Add(1)
		}
		return fmt.Errorf("invalid serial port configuration: %w", err)
	}

	p.mode = &gobug.Mode{
		BaudRate: BaudRate(cfg.BaudRate).Int(),
		DataBits: DataBits(cfg.DataBits).Int(),
		Parity:   Parity(cfg.Parity).Get(),
		StopBits: StopBits(cfg.StopBits).Get(),
	}

	// Initialize write queue done channel
	p.queueDone = make(chan struct{})
	p.writeGoroutineDone = make(chan struct{})

	return nil
}

// initWriteQueue initializes the write queue and starts the processor goroutine
func (p *Service) initWriteQueue() {
	p.queueMu.Lock()
	defer p.queueMu.Unlock()

	// Only initialize writeQueue if not already done
	if p.writeQueue == nil {
		p.writeQueue = make(chan *writeOperation, 50) // Buffered channel for 50 operations
	}

	p.writeGoroutineOnce.Do(func() {
		go p.processWrites()
	})
}

// processWrites handles all write operations from the queue in a single goroutine
func (p *Service) processWrites() {
	defer func() {
		// Signal that this goroutine has exited
		close(p.writeGoroutineDone)

		// Ensure we clean up any remaining operations when exiting
		p.queueMu.RLock()
		queue := p.writeQueue
		p.queueMu.RUnlock()

		if queue != nil {
			// Drain any remaining operations and close their result channels
			for {
				select {
				case op := <-queue:
					if op == nil {
						return
					}
					// Send error to any waiting operations
					select {
					case op.resultCh <- writeResult{0, ErrPortNotOpen}:
					default:
					}
					close(op.resultCh)
				default:
					return
				}
			}
		}
	}()

	for {
		p.queueMu.RLock()
		queue := p.writeQueue
		p.queueMu.RUnlock()

		if queue == nil {
			// Queue was closed, exit
			return
		}

		select {
		case op := <-queue:
			if op == nil {
				// Channel closed, exit
				return
			}
			// Check if service is closing before processing
			if p.queueClosed.Load() {
				// Service is closing, send error and close result channel
				select {
				case op.resultCh <- writeResult{0, ErrPortNotOpen}:
				default:
				}
				close(op.resultCh)
				continue
			}
			p.executeWrite(op)
		case <-p.queueDone:
			// Service is shutting down, drain remaining operations
			p.drainPendingOperations()
			return
		}
	}
}

// drainPendingOperations drains all pending write operations and sends errors
func (p *Service) drainPendingOperations() {
	p.queueMu.RLock()
	queue := p.writeQueue
	p.queueMu.RUnlock()

	if queue == nil {
		return
	}

	// Drain all remaining operations
	for {
		select {
		case op := <-queue:
			if op == nil {
				return
			}
			// Send error to waiting operations
			select {
			case op.resultCh <- writeResult{0, ErrPortNotOpen}:
			default:
			}
			close(op.resultCh)
		default:
			return
		}
	}
}

// executeWrite performs the actual write operation
func (p *Service) executeWrite(op *writeOperation) {
	defer close(op.resultCh)

	// Check if context is already cancelled
	select {
	case <-op.ctx.Done():
		select {
		case op.resultCh <- writeResult{0, op.ctx.Err()}:
		default:
		}
		return
	default:
	}

	// Use read lock to prevent Close() from proceeding while we're writing
	// This eliminates the TOCTOU race condition
	p.mu.RLock()
	defer p.mu.RUnlock()

	var result writeResult
	// Check if the service is still open and handle is available
	if !p.isOpen.Load() || p.handle == nil {
		result = writeResult{0, ErrPortNotOpen}
	} else {
		// Perform the write operation while holding the read lock
		// This prevents Close() from invalidating the handle during the write
		n, err := p.handle.Write(op.data)

		// Check again if the service was closed during the write operation
		// (though this is now redundant since we hold the read lock)
		if !p.isOpen.Load() {
			result = writeResult{0, ErrPortNotOpen}
		} else {
			result = writeResult{n, err}
		}
	}

	// Send result back, but don't block if context is cancelled
	select {
	case op.resultCh <- result:
	case <-op.ctx.Done():
		// Context cancelled while we were writing
	}
}

func AvailablePorts() ([]string, error) {
	ports, err := getPortsList()
	if err != nil {
		return nil, err
	}
	return ports, nil
}

func (p *Service) Open() (err error) {
	// Metrics are recorded directly in Open() method to avoid double counting

	if !p.initialized.Load() {
		if p.metrics != nil {
			p.metrics.ConnectionFailures.Add(1)
		}
		return ErrNotInitialized
	}

	if p.metrics != nil {
		p.metrics.ConnectionAttempts.Add(1)
	}

	// Check if already marked as open and validate handle state
	if p.isOpen.Load() {
		p.mu.RLock()
		handleExists := p.handle != nil
		p.mu.RUnlock()

		if handleExists {
			// Port appears to be open and handle exists, assume it's valid
			return nil
		}
		// Handle is nil but flag says open - reset state and continue opening
		p.isOpen.Store(false)
	}

	// Check if config is nil without copying
	if p.isConfigNil() {
		if p.metrics != nil {
			p.metrics.ConfigurationErrors.Add(1)
		}
		return errors.New("serial config has not been set/injected")
	}

	// Validate config and check port availability using read-only access
	var portName string
	var readTimeout time.Duration
	var dtr, rts bool

	err = p.withConfigLock(func(config *types.SerialConfig) error {
		// Re-validate config in case it was modified after Initialize()
		if err := ValidateConfig(config); err != nil {
			return fmt.Errorf("configuration validation failed: %w", err)
		}

		// Store config values we'll need later
		portName = config.PortName
		readTimeout = config.ReadTimeout
		dtr = config.DTR
		rts = config.RTS

		return nil
	})

	if err != nil {
		if p.metrics != nil {
			p.metrics.ConfigurationErrors.Add(1)
		}
		return err
	}

	ok, listErr := isPortAvailable(portName)
	if listErr != nil {
		if p.metrics != nil {
			p.metrics.ConnectionFailures.Add(1)
		}
		return fmt.Errorf("listing ports: %w", listErr)
	}
	if !ok {
		if p.metrics != nil {
			p.metrics.PortValidationErrors.Add(1)
			p.metrics.ConnectionFailures.Add(1)
		}
		return ErrInvalidPortName
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check state under write lock in case another goroutine changed it
	if p.isOpen.Load() && p.handle != nil {
		return nil
	}

	if p.handle, err = openPort(portName, p.mode); err != nil {
		if p.metrics != nil {
			p.metrics.ConnectionFailures.Add(1)
			p.metrics.HardwareErrors.Add(1)
		}
		return fmt.Errorf("opening serial port: %w", err)
	}

	if err = p.handle.SetReadTimeout(readTimeout); err != nil {
		return p.handleOpenError(err)
	}

	// Explicitly set control lines to configured values
	if err = p.handle.SetDTR(dtr); err != nil {
		return p.handleOpenError(err)
	}
	if err = p.handle.SetRTS(rts); err != nil {
		return p.handleOpenError(err)
	}

	// Success metrics
	p.isOpen.Store(true)
	if p.metrics != nil {
		p.metrics.SuccessfulConnects.Add(1)
		p.metrics.CurrentConnections.Store(1)
		p.metrics.LastConnectTime.Store(time.Now().Unix())
		p.metrics.ConnectionStartTime.Store(time.Now().UnixNano())
		p.resetConsecutiveFailures()
	}

	return nil
}

func (p *Service) Close() error {
	if !p.initialized.Load() {
		return ErrNotInitialized
	}

	// Immediately mark as closed to prevent new operations and ensure
	// pending operations see the closed state
	p.isOpen.Store(false)
	p.queueClosed.Store(true) // Mark queue as closed atomically

	// Use sync.Once to ensure channels are closed only once, even with concurrent calls
	p.queueCloseOnce.Do(func() {
		// First, signal write queue to shutdown if it was initialized
		if p.queueDone != nil {
			close(p.queueDone)
		}

		// Wait for the write goroutine to exit gracefully if it was started
		p.queueMu.RLock()
		goroutineDone := p.writeGoroutineDone
		p.queueMu.RUnlock()

		if goroutineDone != nil {
			select {
			case <-goroutineDone:
				// Goroutine exited cleanly
			case <-time.After(100 * time.Millisecond):
				// Timeout waiting for goroutine, proceed anyway
			}
		}

		// Now safely close the write queue channel with proper synchronization
		p.queueMu.Lock()
		if p.writeQueue != nil {
			close(p.writeQueue)
			p.writeQueue = nil
		}
		p.queueMu.Unlock()
	})

	var closeErr error
	p.closeOnce.Do(func() {
		p.mu.Lock()
		defer p.mu.Unlock()

		// Record uptime before closing
		if p.metrics != nil {
			startTime := p.metrics.ConnectionStartTime.Load()
			if startTime > 0 {
				sessionUptime := time.Now().UnixNano() - startTime
				p.metrics.TotalUptime.Add(sessionUptime)
			}
			p.metrics.Disconnections.Add(1)
			p.metrics.LastDisconnectTime.Store(time.Now().Unix())
			p.metrics.CurrentConnections.Store(0)
		}

		closeErr = p.closeWithoutLock()
	})

	return closeErr
}

func (p *Service) Write(b []byte) (int, error) {
	if !p.initialized.Load() {
		return 0, ErrNotInitialized
	}

	// Use configured timeout, or direct write if no timeout configured
	timeout := p.getWriteTimeout()

	return p.WriteWithTimeout(b, timeout)
}

// WriteWithContext writes data to the serial port with context for cancellation/timeout
func (p *Service) WriteWithContext(ctx context.Context, b []byte) (int, error) {
	return p.writeWithContextInternal(ctx, b, true)
}

// writeWithContextInternal implements the actual context-based write with optional metrics recording
func (p *Service) writeWithContextInternal(ctx context.Context, b []byte, recordMetrics bool) (int, error) {
	start := time.Now()
	var n int
	var err error

	defer func() {
		if r := recover(); r != nil {
			// Recovered from panic (likely sending to closed channel)
			n, err = 0, ErrPortNotOpen
		}
		if recordMetrics && p.metricsEnabled.Load() && p.metrics != nil {
			p.recordWriteMetrics(n, err, time.Since(start))
		}
	}()

	if !p.initialized.Load() {
		return 0, ErrNotInitialized
	}

	// Validate buffer using helper function
	if err = p.validateBuffer(b, recordMetrics); err != nil {
		return 0, err
	}

	// Initialize write queue if not already done
	p.queueOnce.Do(p.initWriteQueue)

	// Create a result channel for this operation
	resultCh := make(chan writeResult, 1)
	op := &writeOperation{
		data:     b,
		ctx:      ctx,
		resultCh: resultCh,
	}

	// Perform ALL state checks and queue operations under a single lock
	// to eliminate TOCTOU race conditions completely
	sent := false

	// Use write lock briefly to ensure queue initialization is fully visible
	// before proceeding with read operations
	p.queueMu.Lock()
	queueInitialized := p.writeQueue != nil
	p.queueMu.Unlock()

	if !queueInitialized {
		// Queue initialization failed or service is closing
		return 0, ErrPortNotOpen
	}

	p.queueMu.RLock()
	// Consolidated state check: service open, queue exists, and not closing
	if p.isOpen.Load() && !p.queueClosed.Load() && p.writeQueue != nil {
		// Safely attempt to send to channel with panic recovery
		func() {
			defer func() {
				if r := recover(); r != nil {
					// Channel was closed during send attempt
					if p.LoggerService != nil {
						p.LoggerService.Debug("write queue closed during send", "panic", r)
					}
					sent = false
				}
			}()
			select {
			case p.writeQueue <- op:
				sent = true
			default:
				// Channel is full - we'll retry after unlock with timeout
			}
		}()
	}
	p.queueMu.RUnlock()

	// If initial send failed, try once more with timeout but only if service is still valid
	if !sent {
		// Use context timeout for retry attempt
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(50 * time.Millisecond):
			// Try one final time under lock
			p.queueMu.RLock()
			if p.isOpen.Load() && !p.queueClosed.Load() && p.writeQueue != nil {
				// Safely attempt to send to channel with panic recovery
				func() {
					defer func() {
						if recover() != nil {
							// Channel was closed during send attempt
							sent = false
						}
					}()
					select {
					case p.writeQueue <- op:
						sent = true
					default:
						// Still can't send - service likely closing or queue full
					}
				}()
			}
			p.queueMu.RUnlock()
		}

		// If still not sent after retry, service is likely closing
		if !sent {
			return 0, ErrPortNotOpen
		}
	}

	// Operation was successfully queued, wait for result
	if sent {
		select {
		case result := <-resultCh:
			n, err = result.n, result.err
			return n, err
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}

	return 0, ErrPortNotOpen
}

// WriteWithTimeout writes data to the serial port with explicit timeout
func (p *Service) WriteWithTimeout(b []byte, timeout time.Duration) (int, error) {
	if timeout <= 0 {
		// No timeout - use direct write
		return p.writeDirectly(b)
	}

	start := time.Now()
	var n int
	var err error

	defer func() {
		if p.metricsEnabled.Load() && p.metrics != nil {
			p.recordWriteMetrics(n, err, time.Since(start))
		}
	}()

	if !p.initialized.Load() {
		return 0, ErrNotInitialized
	}
	if !p.isOpen.Load() {
		return 0, ErrPortNotOpen
	}

	// Validate buffer using helper function
	if err = p.validateBuffer(b, true); err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	n, err = p.writeWithContextInternal(ctx, b, false) // Don't record metrics twice
	if errors.Is(err, context.DeadlineExceeded) {
		err = ErrWriteTimeout // Convert to our custom timeout error
	}
	return n, err
}

// writeDirectly performs immediate write without a timeout
func (p *Service) writeDirectly(b []byte) (int, error) {
	start := time.Now()
	var totalWritten int
	var err error

	defer func() {
		if p.metricsEnabled.Load() && p.metrics != nil {
			p.recordWriteMetrics(totalWritten, err, time.Since(start))
		}
	}()

	const maxRetries = 3

	if !p.initialized.Load() {
		return 0, ErrNotInitialized
	}
	if !p.isOpen.Load() {
		return 0, ErrPortNotOpen
	}

	// Validate buffer using helper function
	if err := p.validateBuffer(b, true); err != nil {
		return 0, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.handle == nil {
		return 0, ErrPortNotOpen
	}

	for retries := 0; totalWritten < len(b) && retries < maxRetries; retries++ {
		n, writeErr := p.handle.Write(b[totalWritten:])
		if writeErr != nil {
			err = writeErr
			break
		}
		totalWritten += n
		if n == 0 {
			// Prevent infinite loop if Write returns 0
			break
		}
	}
	if totalWritten < len(b) && err == nil {
		err = errors.New("partial write: not all bytes written")
	}
	return totalWritten, err
}

// Read is a blocking read operation that reads up to len(b) bytes from the serial port.
// It returns the number of bytes read and any error encountered.
//
// If the serial module has not been initialized, Read returns ErrNotInitialized.
//
// If the given buffer is too small, Read returns ErrInvalidBuffer.
//
// If the given buffer is too large, Read returns ErrBufferTooLarge.
//
// If the port is closed, Read returns ErrPortNotOpen.
func (p *Service) Read(b []byte) (int, error) {
	if !p.initialized.Load() {
		return 0, ErrNotInitialized
	}

	start := time.Now()
	var n int
	var err error

	defer func() {
		if p.metricsEnabled.Load() && p.metrics != nil {
			p.recordReadMetrics(n, err, time.Since(start))
		}
	}()

	// Validate buffer using helper function
	if err = p.validateBuffer(b, true); err != nil {
		return 0, err
	}

	// Use read lock to prevent Close() from proceeding while we're reading
	// This eliminates the TOCTOU race condition
	p.mu.RLock()
	defer p.mu.RUnlock()

	if !p.isOpen.Load() || p.handle == nil {
		return 0, ErrPortNotOpen
	}

	// Perform the read operation while holding the read lock
	// This prevents Close() from invalidating the handle during the read
	n, err = p.handle.Read(b)
	return n, err
}

// GetBufferPoolStats returns buffer pool statistics for this Service instance
func (p *Service) GetBufferPoolStats() []PoolStats {
	if p.bufferPoolManager == nil {
		return nil
	}
	return p.bufferPoolManager.GetAllPoolStats()
}

// ResetBufferPoolStats resets buffer pool statistics for this Service instance (useful for testing)
func (p *Service) ResetBufferPoolStats() {
	if p.bufferPoolManager != nil {
		p.bufferPoolManager.ResetPoolStats()
	}
}

// validateBuffer validates buffer parameters and records errors if needed
func (p *Service) validateBuffer(b []byte, recordErrors bool) error {
	if b == nil || len(b) == 0 {
		if recordErrors && p.metrics != nil {
			p.metrics.BufferErrors.Add(1)
		}
		return ErrInvalidBuffer
	}
	if len(b) > MaxBufferSize {
		if recordErrors && p.metrics != nil {
			p.metrics.BufferErrors.Add(1)
		}
		return ErrBufferTooLarge
	}
	return nil
}

// getConfigFieldSafe safely reads a specific config field to avoid full config copying
func (p *Service) getWriteTimeout() time.Duration {
	p.configMu.RLock()
	defer p.configMu.RUnlock()
	if p.Config != nil {
		return p.Config.WriteTimeout
	}
	return 0
}

// getPortName safely reads the port name without copying the entire config
func (p *Service) getPortName() string {
	p.configMu.RLock()
	defer p.configMu.RUnlock()
	if p.Config != nil {
		return p.Config.PortName
	}
	return ""
}

// getReadTimeout safely reads the read timeout without copying the entire config
func (p *Service) getReadTimeout() time.Duration {
	p.configMu.RLock()
	defer p.configMu.RUnlock()
	if p.Config != nil {
		return p.Config.ReadTimeout
	}
	return 0
}

// getDTR safely reads the DTR setting without copying the entire config
func (p *Service) getDTR() bool {
	p.configMu.RLock()
	defer p.configMu.RUnlock()
	if p.Config != nil {
		return p.Config.DTR
	}
	return false
}

// getRTS safely reads the RTS setting without copying the entire config
func (p *Service) getRTS() bool {
	p.configMu.RLock()
	defer p.configMu.RUnlock()
	if p.Config != nil {
		return p.Config.RTS
	}
	return false
}

// isConfigNil safely checks if config is nil without copying
func (p *Service) isConfigNil() bool {
	p.configMu.RLock()
	defer p.configMu.RUnlock()
	return p.Config == nil
}

// withConfigLock executes a function with read-only access to the config
// This allows complex operations without copying the entire config
func (p *Service) withConfigLock(fn func(*types.SerialConfig) error) error {
	p.configMu.RLock()
	defer p.configMu.RUnlock()
	if p.Config == nil {
		return errors.New("serial config has not been set/injected")
	}
	return fn(p.Config)
}

// getConfigSafeCopy safely returns a copy of the current config to prevent race conditions
// This should only be used when a full copy is actually needed (e.g., for external APIs)
func (p *Service) getConfigSafeCopy() *types.SerialConfig {
	p.configMu.RLock()
	defer p.configMu.RUnlock()

	if p.Config == nil {
		return nil
	}

	// Return a deep copy to prevent race conditions on config field access
	configCopy := &types.SerialConfig{
		PortName:     p.Config.PortName,
		BaudRate:     p.Config.BaudRate,
		DataBits:     p.Config.DataBits,
		Parity:       p.Config.Parity,
		StopBits:     p.Config.StopBits,
		ReadTimeout:  p.Config.ReadTimeout,
		WriteTimeout: p.Config.WriteTimeout,
		DTR:          p.Config.DTR,
		RTS:          p.Config.RTS,
	}

	return configCopy
}

// getConfigSafe safely returns a copy of the current config to prevent race conditions
// DEPRECATED: Use more specific methods like getPortName(), withConfigLock(), or getConfigSafeCopy()
func (p *Service) getConfigSafe() *types.SerialConfig {
	return p.getConfigSafeCopy()
}

// setConfigSafe safely sets the config with proper synchronization
func (p *Service) setConfigSafe(config *types.SerialConfig) {
	p.configMu.Lock()
	defer p.configMu.Unlock()
	p.Config = config
}
