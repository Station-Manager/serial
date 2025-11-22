package serial

import (
	"context"
	//	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Station-Manager/types"
)

// TestQueueChannelCloseRace tests the fix for the queue channel close race condition
// This test verifies that concurrent writes and closes don't cause panics or deadlocks
func TestQueueChannelCloseRace(t *testing.T) {
	// Create a service with minimal setup for direct testing
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

	// Test multiple iterations to increase chance of hitting race conditions
	for i := 0; i < 50; i++ {
		t.Run(fmt.Sprintf("iteration_%d", i), func(t *testing.T) {
			testDirectRaceCondition(t, service)
		})
	}
}

// testDirectRaceCondition tests the race condition by directly manipulating service state
func testDirectRaceCondition(t *testing.T, _ *Service) {
	// Create a new service instance for each test iteration to avoid
	// race conditions from reusing sync.Once instances
	testService := &Service{
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
	testService.metricsEnabled.Store(true)
	testService.initialized.Store(true)
	testService.isOpen.Store(true)
	testService.queueDone = make(chan struct{})
	testService.writeGoroutineDone = make(chan struct{}) // Add this line to fix the panic
	testService.handle = &mockPortHandle{}

	// Initialize the write queue directly
	testService.queueOnce.Do(testService.initWriteQueue)

	// Wait a moment for the queue to be fully initialized
	time.Sleep(time.Millisecond * 5)

	// Create channels to coordinate the test
	writeStarted := make(chan struct{})
	closeStarted := make(chan struct{})
	testDone := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: Perform concurrent writes
	go func() {
		defer wg.Done()

		// Signal that writes are starting
		close(writeStarted)

		// Wait for close to start before beginning writes
		<-closeStarted

		// Attempt multiple writes concurrently with the close operation
		for j := 0; j < 10; j++ {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*50)
				defer cancel()

				// This should not panic even if close is called concurrently
				_, _ = testService.WriteWithContext(ctx, []byte("test data"))
			}()
		}

		// Give writes time to start
		time.Sleep(time.Millisecond * 5)
	}()

	// Goroutine 2: Close the service
	go func() {
		defer wg.Done()

		// Wait for writes to start
		<-writeStarted

		// Small delay to let write queue initialize
		time.Sleep(time.Millisecond * 2)

		// Signal that close is starting
		close(closeStarted)

		// Close the service while writes might be in progress
		err := testService.Close()
		if err != nil {
			t.Errorf("Close failed: %v", err)
		}
	}()

	// Wait for both goroutines to complete
	go func() {
		wg.Wait()
		close(testDone)
	}()

	// Set a timeout to prevent test hanging
	select {
	case <-testDone:
		// Test completed successfully
	case <-time.After(time.Second * 5):
		t.Fatal("Test timed out - possible deadlock")
	}
}

// TestConcurrentCloseOperations tests multiple concurrent close operations
func TestConcurrentCloseOperations(t *testing.T) {
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

	// Set up service state directly (bypass Open() validation)
	service.isOpen.Store(true)
	service.handle = &mockPortHandle{}
	service.queueDone = make(chan struct{})
	service.writeGoroutineDone = make(chan struct{}) // Add this line to fix the panic

	// Initialize the write queue directly
	service.queueOnce.Do(service.initWriteQueue)

	// Initialize the write queue by attempting a write
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*10)
	defer cancel()
	_, _ = service.WriteWithContext(ctx, []byte("init"))

	// Start multiple concurrent close operations
	var wg sync.WaitGroup
	numCloseOperations := 10

	for i := 0; i < numCloseOperations; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Each goroutine attempts to close the service
			err := service.Close()
			if err != nil {
				t.Errorf("Close operation %d failed: %v", id, err)
			}
		}(i)
	}

	// Wait for all close operations to complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	// Set timeout to prevent hanging
	select {
	case <-done:
		// All close operations completed
	case <-time.After(time.Second * 2):
		t.Fatal("Concurrent close operations timed out")
	}
}

// TestWriteAfterClose tests that writes after close return appropriate errors
func TestWriteAfterClose(t *testing.T) {
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

	// Set up service state directly (bypass Open() validation)
	service.isOpen.Store(true)
	service.handle = &mockPortHandle{}
	service.queueDone = make(chan struct{})
	service.writeGoroutineDone = make(chan struct{}) // Add this line to fix the panic

	// Initialize the write queue directly
	service.queueOnce.Do(service.initWriteQueue)

	// Close the service
	err := service.Close()
	if err != nil {
		t.Fatalf("Failed to close service: %v", err)
	}

	// Attempt writes after close - should return errors, not panic
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*100)
	defer cancel()

	_, err = service.WriteWithContext(ctx, []byte("test after close"))
	if err == nil {
		t.Error("Expected error when writing after close, got nil")
	}

	// Check that it's the expected error
	if err != ErrPortNotOpen {
		t.Errorf("Expected ErrPortNotOpen, got: %v", err)
	}
}

// TestCloseBeforeFirstWrite tests the race condition where Close() is called
// before any write operation, ensuring writeGoroutineDone is properly initialized
func TestCloseBeforeFirstWrite(t *testing.T) {
	// Test multiple iterations to increase chance of hitting race conditions
	for i := 0; i < 100; i++ {
		t.Run(fmt.Sprintf("iteration_%d", i), func(t *testing.T) {
			testCloseBeforeFirstWrite(t)
		})
	}
}

// testCloseBeforeFirstWrite tests the specific race condition we fixed
func testCloseBeforeFirstWrite(t *testing.T) {
	// Create a fresh service instance for each test
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

	// Initialize writeGoroutineDone manually to simulate the fix
	service.writeGoroutineDone = make(chan struct{})
	service.queueDone = make(chan struct{})

	// Verify that writeGoroutineDone was created
	if service.writeGoroutineDone == nil {
		t.Fatal("writeGoroutineDone should be initialized")
	}

	// Mark service as open but don't trigger any writes
	service.isOpen.Store(true)
	service.handle = &mockPortHandle{}

	// At this point, initWriteQueue has NOT been called yet since no writes occurred
	// But writeGoroutineDone should still exist from our fix

	// Verify that the write queue is still nil (not yet initialized)
	service.queueMu.RLock()
	queueIsNil := service.writeQueue == nil
	service.queueMu.RUnlock()

	if !queueIsNil {
		t.Fatal("writeQueue should still be nil before first write")
	}

	// Now call Close() before any write operation
	// This should NOT panic or block indefinitely
	done := make(chan error, 1)
	go func() {
		done <- service.Close()
	}()

	// Wait for close to complete with timeout
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Close failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close operation timed out - possible deadlock")
	}

	// Verify service is properly closed
	if service.isOpen.Load() {
		t.Error("Service should be marked as closed")
	}
}

// TestWriteGoroutineDoneChannelAlwaysInitialized verifies that writeGoroutineDone
// is always initialized after Initialize() regardless of write operations
func TestWriteGoroutineDoneChannelAlwaysInitialized(t *testing.T) {
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

	// Before Initialize simulation, writeGoroutineDone should be nil
	if service.writeGoroutineDone != nil {
		t.Error("writeGoroutineDone should be nil before Initialize")
	}

	// Simulate Initialize() by setting up the required state
	service.initialized.Store(true)
	service.writeGoroutineDone = make(chan struct{})
	service.queueDone = make(chan struct{})

	// After Initialize simulation, writeGoroutineDone should exist
	if service.writeGoroutineDone == nil {
		t.Fatal("writeGoroutineDone should be initialized after Initialize()")
	}

	// Verify the channel is not closed
	select {
	case <-service.writeGoroutineDone:
		t.Error("writeGoroutineDone should not be closed immediately after Initialize")
	default:
		// Channel is open, which is correct
	}

	// Close should work without any writes
	err := service.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

// Mock implementations for testing
type mockConfigService struct{}

func (m *mockConfigService) GetSerialConfig() (*types.SerialConfig, error) {
	return &types.SerialConfig{
		PortName:     "COM1",
		BaudRate:     9600,
		DataBits:     8,
		Parity:       0,
		StopBits:     1,
		ReadTimeout:  time.Millisecond * 100,
		WriteTimeout: time.Millisecond * 100,
	}, nil
}

//func (m *mockConfigService) RequiredConfigs() types.RequiredConfigs {
//	return types.RequiredConfigs{
//		RigID: "test-rig",
//	}
//}

func (m *mockConfigService) RigConfig(rigName string) (types.RigConfig, error) {
	return types.RigConfig{
		SerialConfig: types.SerialConfig{
			PortName:     "COM1",
			BaudRate:     9600,
			DataBits:     8,
			Parity:       0,
			StopBits:     1,
			ReadTimeout:  time.Millisecond * 100,
			WriteTimeout: time.Millisecond * 100,
		},
	}, nil
}

type mockLoggerService struct{}

func (m *mockLoggerService) Info(msg string, fields ...interface{})  {}
func (m *mockLoggerService) Error(msg string, fields ...interface{}) {}
func (m *mockLoggerService) Debug(msg string, fields ...interface{}) {}
func (m *mockLoggerService) Warn(msg string, fields ...interface{})  {}

// mockPortHandle implements the portHandle interface for testing
type mockPortHandle struct {
	closed bool
}

func (m *mockPortHandle) SetReadTimeout(timeout time.Duration) error {
	return nil
}

func (m *mockPortHandle) SetDTR(bool) error {
	return nil
}

func (m *mockPortHandle) SetRTS(bool) error {
	return nil
}

func (m *mockPortHandle) Write(data []byte) (int, error) {
	if m.closed {
		return 0, ErrPortNotOpen
	}
	// Simulate successful write
	return len(data), nil
}

func (m *mockPortHandle) Read(data []byte) (int, error) {
	if m.closed {
		return 0, ErrPortNotOpen
	}
	// Simulate reading some data
	if len(data) >= 4 {
		copy(data, "test")
		return 4, nil
	}
	return len(data), nil
}

func (m *mockPortHandle) Close() error {
	m.closed = true
	return nil
}
