package serial

import (
	"context"
	"sync"
	"sync/atomic"
)

// BufferPool manages reusable byte buffers for I/O operations
type BufferPool struct {
	pool sync.Pool
	size int
	// Metrics for monitoring pool efficiency
	gets    atomic.Int64
	puts    atomic.Int64
	creates atomic.Int64
}

// NewBufferPool creates a buffer pool with fixed-size buffers
func NewBufferPool(bufferSize int) *BufferPool {
	bp := &BufferPool{
		size: bufferSize,
	}
	bp.pool = sync.Pool{
		New: func() interface{} {
			bp.creates.Add(1)
			return make([]byte, bufferSize)
		},
	}
	return bp
}

// Get retrieves a buffer from the pool
func (bp *BufferPool) Get() []byte {
	bp.gets.Add(1)
	return bp.pool.Get().([]byte)
}

// Put returns a buffer to the pool (clears it first for security)
func (bp *BufferPool) Put(buf []byte) {
	if len(buf) != bp.size {
		return // Don't pool incorrectly sized buffers
	}
	bp.puts.Add(1)

	clear(buf)
	bp.pool.Put(buf)
}

// Stats returns pool usage statistics
func (bp *BufferPool) Stats() PoolStats {
	return PoolStats{
		Size:    bp.size,
		Gets:    bp.gets.Load(),
		Puts:    bp.puts.Load(),
		Creates: bp.creates.Load(),
	}
}

// PoolStats contains buffer pool usage statistics
type PoolStats struct {
	Size    int   // Buffer size managed by this pool
	Gets    int64 // Number of Get() calls
	Puts    int64 // Number of Put() calls
	Creates int64 // Number of new buffers created
}

// HitRatio returns the cache hit ratio (0.0 to 1.0)
func (ps PoolStats) HitRatio() float64 {
	if ps.Gets == 0 {
		return 0.0
	}
	return 1.0 - (float64(ps.Creates) / float64(ps.Gets))
}

// BufferPoolManager manages multiple buffer pools for a Service instance
type BufferPoolManager struct {
	smallPool  *BufferPool // 256 bytes
	mediumPool *BufferPool // 1024 bytes
	largePool  *BufferPool // 4096 bytes
	service    *Service    // Reference to parent service for metrics
}

// NewBufferPoolManager creates a new buffer pool manager
func NewBufferPoolManager(service *Service) *BufferPoolManager {
	return &BufferPoolManager{
		smallPool:  NewBufferPool(256),
		mediumPool: NewBufferPool(1024),
		largePool:  NewBufferPool(4096),
		service:    service,
	}
}

// GetPooledBuffer returns an appropriately sized buffer from pools
func (bpm *BufferPoolManager) GetPooledBuffer(size int) ([]byte, func()) {
	recordMiss := func() {
		if bpm.service != nil && bpm.service.metrics != nil {
			bpm.service.metrics.BufferPoolMisses.Add(1)
		}
	}

	recordHit := func() {
		if bpm.service != nil && bpm.service.metrics != nil {
			bpm.service.metrics.BufferPoolHits.Add(1)
		}
	}

	if size <= 0 {
		// Return minimal buffer for zero/negative sizes
		recordHit()
		buf := bpm.smallPool.Get()[:1]
		return buf, func() { bpm.smallPool.Put(buf[:cap(buf)]) }
	}

	// SECURITY FIX: Prevent memory exhaustion attacks by enforcing absolute maximum
	if size > AbsoluteMaxBufferSize {
		// Reject extremely large allocations that could cause memory exhaustion
		recordMiss()
		return nil, func() {} // Return nil buffer to indicate failure
	}

	if size > MaxBufferSize {
		// Don't pool oversized buffers, but allow direct allocation up to absolute limit
		recordMiss()
		buf := make([]byte, size)
		return buf, func() {} // No-op cleanup
	}

	var buf []byte
	var cleanup func()

	switch {
	case size <= 256:
		recordHit()
		buf = bpm.smallPool.Get()[:size]
		cleanup = func() { bpm.smallPool.Put(buf[:cap(buf)]) }
	case size <= 1024:
		recordHit()
		buf = bpm.mediumPool.Get()[:size]
		cleanup = func() { bpm.mediumPool.Put(buf[:cap(buf)]) }
	case size <= 4096:
		recordHit()
		buf = bpm.largePool.Get()[:size]
		cleanup = func() { bpm.largePool.Put(buf[:cap(buf)]) }
	default:
		// For sizes between 4KB and MaxBufferSize, use direct allocation
		recordMiss()
		buf = make([]byte, size)
		cleanup = func() {} // No-op cleanup
	}

	return buf, cleanup
}

// GetAllPoolStats returns statistics for all pools
func (bpm *BufferPoolManager) GetAllPoolStats() []PoolStats {
	return []PoolStats{
		bpm.smallPool.Stats(),
		bpm.mediumPool.Stats(),
		bpm.largePool.Stats(),
	}
}

// ResetPoolStats resets all pool statistics (useful for testing)
func (bpm *BufferPoolManager) ResetPoolStats() {
	bpm.smallPool.gets.Store(0)
	bpm.smallPool.puts.Store(0)
	bpm.smallPool.creates.Store(0)

	bpm.mediumPool.gets.Store(0)
	bpm.mediumPool.puts.Store(0)
	bpm.mediumPool.creates.Store(0)

	bpm.largePool.gets.Store(0)
	bpm.largePool.puts.Store(0)
	bpm.largePool.creates.Store(0)
}

// PooledReadResult contains the result of a pooled read operation
type PooledReadResult struct {
	Data []byte
	N    int
	Err  error
}

// ReadWithPooling performs a read operation using a pooled buffer
func (p *Service) ReadWithPooling(size int) ([]byte, error) {
	if !p.initialized.Load() {
		return nil, ErrNotInitialized
	}
	if !p.isOpen.Load() {
		return nil, ErrPortNotOpen
	}

	// Validate size
	if size <= 0 {
		return nil, ErrInvalidBuffer
	}
	if size > AbsoluteMaxBufferSize {
		return nil, ErrBufferExceedsAbsoluteLimit
	}
	if size > MaxBufferSize {
		return nil, ErrBufferTooLarge
	}

	// Get pooled buffer from instance-specific pool manager
	buf, cleanup := p.bufferPoolManager.GetPooledBuffer(size)
	if buf == nil {
		return nil, ErrBufferExceedsAbsoluteLimit
	}
	defer cleanup()

	// Perform read
	n, err := p.Read(buf)
	if err != nil {
		return nil, err
	}

	// Return only the data that was actually read
	result := make([]byte, n)
	copy(result, buf[:n])
	return result, nil
}

// ReadStreamWithPooling creates a channel-based streaming reader using pooled buffers
func (p *Service) ReadStreamWithPooling(ctx context.Context, bufferSize int) (<-chan PooledReadResult, error) {
	if !p.initialized.Load() {
		return nil, ErrNotInitialized
	}
	if !p.isOpen.Load() {
		return nil, ErrPortNotOpen
	}

	// Validate buffer size
	if bufferSize <= 0 {
		return nil, ErrInvalidBuffer
	}
	if bufferSize > MaxBufferSize {
		return nil, ErrBufferTooLarge
	}

	resultChan := make(chan PooledReadResult, 10) // Buffered channel

	go func() {
		defer close(resultChan)

		for {
			// Check context cancellation first
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Get pooled buffer from instance-specific pool manager
			buf, cleanup := p.bufferPoolManager.GetPooledBuffer(bufferSize)

			// Perform read
			n, err := p.Read(buf)

			// Exit immediately on error or when no more data is available
			if err != nil || n == 0 {
				cleanup() // Return buffer to pool
				return
			}

			// Create result with copy of data
			data := make([]byte, n)
			copy(data, buf[:n])

			cleanup() // Return buffer to pool

			result := PooledReadResult{
				Data: data,
				N:    n,
				Err:  err,
			}

			// Try to send result, but exit immediately if context is cancelled
			select {
			case resultChan <- result:
				// Successfully sent
			case <-ctx.Done():
				return
			}
		}
	}()

	return resultChan, nil
}
