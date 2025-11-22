package serial

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/7Q-Station-Manager/types"
)

// mockPortHandleWithDelay simulates a port handle that can introduce delays
// to increase the chance of hitting race conditions
type mockPortHandleWithDelay struct {
	writeDelay time.Duration
	readDelay  time.Duration
	closeDelay time.Duration
	closed     atomic.Bool
	writeCount atomic.Int64
	readCount  atomic.Int64
	closeCount atomic.Int64
}

func newMockPortHandleWithDelay(writeDelay, readDelay, closeDelay time.Duration) *mockPortHandleWithDelay {
	return &mockPortHandleWithDelay{
		writeDelay: writeDelay,
		readDelay:  readDelay,
		closeDelay: closeDelay,
	}
}

func (m *mockPortHandleWithDelay) Write(b []byte) (int, error) {
	if m.closed.Load() {
		return 0, ErrPortNotOpen
	}

	m.writeCount.Add(1)

	// Simulate delay to increase race condition window
	if m.writeDelay > 0 {
		time.Sleep(m.writeDelay)
	}

	// Check again after delay to simulate real-world timing
	if m.closed.Load() {
		return 0, ErrPortNotOpen
	}

	return len(b), nil
}

func (m *mockPortHandleWithDelay) Read(b []byte) (int, error) {
	if m.closed.Load() {
		return 0, ErrPortNotOpen
	}

	m.readCount.Add(1)

	// Simulate delay to increase race condition window
	if m.readDelay > 0 {
		time.Sleep(m.readDelay)
	}

	// Check again after delay to simulate real-world timing
	if m.closed.Load() {
		return 0, ErrPortNotOpen
	}

	// Return some test data
	copy(b, []byte("test data"))
	return min(len(b), 9), nil
}

func (m *mockPortHandleWithDelay) Close() error {
	m.closeCount.Add(1)

	// Simulate delay during close operation
	if m.closeDelay > 0 {
		time.Sleep(m.closeDelay)
	}

	m.closed.Store(true)
	return nil
}

func (m *mockPortHandleWithDelay) SetReadTimeout(_ time.Duration) error { return nil }
func (m *mockPortHandleWithDelay) SetDTR(_ bool) error                  { return nil }
func (m *mockPortHandleWithDelay) SetRTS(_ bool) error                  { return nil }

// TestTOCTOUWriteRaceCondition tests the TOCTOU race condition fix for write operations
func TestTOCTOUWriteRaceCondition(t *testing.T) {
	for i := 0; i < 100; i++ {
		t.Run("iteration", func(t *testing.T) {
			testWriteCloseRace(t)
		})
	}
}

func testWriteCloseRace(t *testing.T) {
	// Create a service with controlled timing
	service := createTestService(t)

	// Use a mock handle with delays to increase race condition window
	mockHandle := newMockPortHandleWithDelay(
		time.Microsecond*100, // write delay
		time.Microsecond*50,  // read delay
		time.Microsecond*10,  // close delay
	)

	// Set up the service state
	service.handle = mockHandle
	service.isOpen.Store(true)
	service.queueDone = make(chan struct{})
	service.writeGoroutineDone = make(chan struct{}) // Add this line to fix the panic

	// Initialize the write queue
	service.queueOnce.Do(service.initWriteQueue)

	// Wait for queue to be ready
	time.Sleep(time.Millisecond * 10)

	var wg sync.WaitGroup
	var writeErrors atomic.Int64
	var closeErrors atomic.Int64

	// Start multiple concurrent write operations
	numWrites := 50
	for i := 0; i < numWrites; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*500)
			defer cancel()

			// This should not panic or cause undefined behavior
			// even if Close() is called concurrently
			_, err := service.WriteWithContext(ctx, []byte("test data"))
			if err != nil && err != ErrPortNotOpen && err != context.DeadlineExceeded {
				writeErrors.Add(1)
				t.Errorf("Unexpected write error: %v", err)
			}
		}(i)
	}

	// Start close operation after a small delay
	wg.Add(1)
	go func() {
		defer wg.Done()

		// Give writes time to start
		time.Sleep(time.Microsecond * 50)

		err := service.Close()
		if err != nil {
			closeErrors.Add(1)
			t.Errorf("Close failed: %v", err)
		}
	}()

	// Wait for all operations to complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Test completed
	case <-time.After(time.Second * 5):
		t.Fatal("Test timed out - possible deadlock or race condition not fixed")
	}

	// Verify no unexpected errors occurred
	if writeErrors.Load() > 0 {
		t.Errorf("Unexpected write errors: %d", writeErrors.Load())
	}
	if closeErrors.Load() > 0 {
		t.Errorf("Close errors: %d", closeErrors.Load())
	}

	// Verify the mock handle was used (operations actually happened)
	if mockHandle.writeCount.Load() == 0 && mockHandle.closeCount.Load() == 0 {
		t.Error("No operations were performed on the mock handle")
	}
}

// TestTOCTOUReadRaceCondition tests the TOCTOU race condition fix for read operations
func TestTOCTOUReadRaceCondition(t *testing.T) {
	for i := 0; i < 100; i++ {
		t.Run("iteration", func(t *testing.T) {
			testReadCloseRace(t)
		})
	}
}

func testReadCloseRace(t *testing.T) {
	service := createTestService(t)

	// Use a mock handle with delays to increase race condition window
	mockHandle := newMockPortHandleWithDelay(
		time.Microsecond*50,  // write delay
		time.Microsecond*100, // read delay
		time.Microsecond*10,  // close delay
	)

	service.handle = mockHandle
	service.isOpen.Store(true)

	var wg sync.WaitGroup
	var readErrors atomic.Int64
	var closeErrors atomic.Int64

	// Start multiple concurrent read operations
	numReads := 50
	for i := 0; i < numReads; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			buf := make([]byte, 100)

			// This should not panic or cause undefined behavior
			// even if Close() is called concurrently
			_, err := service.Read(buf)
			if err != nil && err != ErrPortNotOpen {
				readErrors.Add(1)
				t.Errorf("Unexpected read error: %v", err)
			}
		}(i)
	}

	// Start close operation after a small delay
	wg.Add(1)
	go func() {
		defer wg.Done()

		// Give reads time to start
		time.Sleep(time.Microsecond * 50)

		err := service.Close()
		if err != nil {
			closeErrors.Add(1)
			t.Errorf("Close failed: %v", err)
		}
	}()

	// Wait for all operations to complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Test completed
	case <-time.After(time.Second * 5):
		t.Fatal("Test timed out - possible deadlock or race condition not fixed")
	}

	// Verify no unexpected errors occurred
	if readErrors.Load() > 0 {
		t.Errorf("Unexpected read errors: %d", readErrors.Load())
	}
	if closeErrors.Load() > 0 {
		t.Errorf("Close errors: %d", closeErrors.Load())
	}

	// Verify the mock handle was used
	if mockHandle.readCount.Load() == 0 && mockHandle.closeCount.Load() == 0 {
		t.Error("No operations were performed on the mock handle")
	}
}

// TestTOCTOUMixedOperationsRaceCondition tests concurrent reads, writes, and close
func TestTOCTOUMixedOperationsRaceCondition(t *testing.T) {
	for i := 0; i < 50; i++ {
		t.Run("iteration", func(t *testing.T) {
			testMixedOperationsRace(t)
		})
	}
}

func testMixedOperationsRace(t *testing.T) {
	service := createTestService(t)

	mockHandle := newMockPortHandleWithDelay(
		time.Microsecond*80, // write delay
		time.Microsecond*80, // read delay
		time.Microsecond*20, // close delay
	)

	service.handle = mockHandle
	service.isOpen.Store(true)
	service.queueDone = make(chan struct{})
	service.writeGoroutineDone = make(chan struct{}) // Add this line to fix the panic

	// Initialize the write queue
	service.queueOnce.Do(service.initWriteQueue)
	time.Sleep(time.Millisecond * 10)

	var wg sync.WaitGroup
	var errors atomic.Int64

	// Start mixed read and write operations
	numOps := 30

	// Writes
	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*500)
			defer cancel()

			_, err := service.WriteWithContext(ctx, []byte("test"))
			if err != nil && err != ErrPortNotOpen && err != context.DeadlineExceeded {
				errors.Add(1)
				t.Errorf("Unexpected write error: %v", err)
			}
		}()
	}

	// Reads
	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			buf := make([]byte, 100)
			_, err := service.Read(buf)
			if err != nil && err != ErrPortNotOpen {
				errors.Add(1)
				t.Errorf("Unexpected read error: %v", err)
			}
		}()
	}

	// Close operation
	wg.Add(1)
	go func() {
		defer wg.Done()

		// Let some operations start
		time.Sleep(time.Microsecond * 100)

		err := service.Close()
		if err != nil {
			errors.Add(1)
			t.Errorf("Close failed: %v", err)
		}
	}()

	// Wait for completion
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Test completed
	case <-time.After(time.Second * 5):
		t.Fatal("Test timed out")
	}

	if errors.Load() > 0 {
		t.Errorf("Unexpected errors: %d", errors.Load())
	}
}

// createTestService creates a minimal service for testing
func createTestService(t *testing.T) *Service {
	service := &Service{
		Config: &types.SerialConfig{
			PortName:     "COM1",
			BaudRate:     9600,
			DataBits:     8,
			Parity:       0,
			StopBits:     1,
			ReadTimeout:  time.Millisecond * 100,
			WriteTimeout: time.Millisecond * 100,
		},
		metrics: &Metrics{},
	}
	service.metricsEnabled.Store(true)
	service.initialized.Store(true)

	return service
}

// min returns the minimum of two integers (for Go versions < 1.21)
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
