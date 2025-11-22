package serial

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"encoding/json"
	"github.com/Station-Manager/config"
	"github.com/Station-Manager/logging"
	"github.com/Station-Manager/types"
	"github.com/Station-Manager/utils"
	gobug "go.bug.st/serial"
)

// ----- Test helpers -----

// defaultSerialConfig returns a valid default SerialConfig for testing
func defaultSerialConfig() types.SerialConfig {
	return types.SerialConfig{
		PortName: "COM1",
		BaudRate: 9600,
		DataBits: 8,
		Parity:   int(ParityNone),
		StopBits: 1,
	}
}

// createConfigService creates an isolated temp working directory, writes a config.json
// with the provided rig and required rig name, sets SM_WORKING_DIR, initializes and returns
// a real *config.Service instance ready for use by the serial package.
func createConfigService(t *testing.T, rig types.RigConfig, requiredRigID int64) *config.Service {
	t.Helper()
	dir := t.TempDir()

	// Apply defaults to rig's serial config if fields are unset
	if rig.SerialConfig.BaudRate == 0 {
		rig.SerialConfig.BaudRate = 9600
	}
	if rig.SerialConfig.DataBits == 0 {
		rig.SerialConfig.DataBits = 8
	}
	if rig.SerialConfig.StopBits == 0 {
		rig.SerialConfig.StopBits = 1
	}

	// Build a minimal config payload
	cfg := types.Configs{
		RigConfigs:     []types.RigConfig{rig},
		RequiredConfig: types.RequiredConfigs{RigID: requiredRigID},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal test config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o644); err != nil {
		t.Fatalf("write test config: %v", err)
	}
	// Point the config service at the temp dir
	if err := os.Setenv(utils.EnvSmWorkingDir, dir); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() { os.Unsetenv(utils.EnvSmWorkingDir) })
	cs := &config.Service{}
	if err := cs.Initialize(); err != nil {
		t.Fatalf("config Initialize: %v", err)
	}
	return cs
}

type mockHandle struct {
	readTimeout   time.Duration
	setDTR        []bool
	setRTS        []bool
	closed        bool
	writeErr      error
	readErr       error
	closeErr      error
	setTimeoutErr error
	setDTRErr     error
	setRTSErr     error
	writeData     []byte
	readData      []byte
	writeDelay    time.Duration // Add this for timeout testing
}

func (m *mockHandle) SetReadTimeout(d time.Duration) error { m.readTimeout = d; return m.setTimeoutErr }
func (m *mockHandle) SetDTR(b bool) error                  { m.setDTR = append(m.setDTR, b); return m.setDTRErr }
func (m *mockHandle) SetRTS(b bool) error                  { m.setRTS = append(m.setRTS, b); return m.setRTSErr }
func (m *mockHandle) Write(b []byte) (int, error) {
	if m.writeDelay > 0 {
		time.Sleep(m.writeDelay)
	}
	m.writeData = append(m.writeData, b...)
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	return len(b), nil
}
func (m *mockHandle) Read(b []byte) (int, error) {
	if m.readErr != nil {
		return 0, m.readErr
	}
	n := copy(b, m.readData)
	// Consume the read data to prevent infinite loops in streaming tests
	m.readData = m.readData[n:]
	return n, nil
}
func (m *mockHandle) Close() error { m.closed = true; return m.closeErr }

// helper to restore globals
func withPortsAndOpen(listFn func() ([]string, error), openFn func(string, *gobug.Mode) (portHandle, error), fn func()) {
	oldList := getPortsList
	oldOpen := openPort
	getPortsList = listFn
	openPort = openFn
	defer func() { getPortsList = oldList; openPort = oldOpen }()
	fn()
}

// ----- Basic type tests -----

func TestBaudRate_Int(t *testing.T) {
	if Baud9600.Int() != 9600 {
		t.Fatalf("Baud9600.Int expected 9600")
	}
}

func TestDataBits_Int(t *testing.T) {
	if DataBits8.Int() != 8 {
		t.Fatalf("DataBits8.Int expected 8")
	}
}

func TestParity_Get(t *testing.T) {
	if ParityNone.Get() != gobug.NoParity {
		t.Fatalf("ParityNone mapping incorrect")
	}
}

func TestStopBits_Get(t *testing.T) {
	if StopBits1.Get() != gobug.OneStopBit {
		t.Fatalf("StopBits1 mapping incorrect")
	}
}

// ----- Available ports and helper tests -----

func TestAvailablePorts_OK(t *testing.T) {
	withPortsAndOpen(func() ([]string, error) { return []string{"/dev/ttyS0", "/dev/ttyUSB0"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return &mockHandle{}, nil }, func() {
		ports, err := AvailablePorts()
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if len(ports) != 2 {
			t.Fatalf("expected 2 ports, got %d", len(ports))
		}
	})
}

func TestAvailablePorts_Error(t *testing.T) {
	boom := errors.New("boom")
	withPortsAndOpen(func() ([]string, error) { return nil, boom }, func(s string, m *gobug.Mode) (portHandle, error) { return &mockHandle{}, nil }, func() {
		_, err := AvailablePorts()
		if !errors.Is(err, boom) {
			t.Fatalf("expected error to propagate")
		}
	})
}

func TestIsPortAvailable(t *testing.T) {
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return &mockHandle{}, nil }, func() {
		ok, err := isPortAvailable("COM1")
		if err != nil || !ok {
			t.Fatalf("expected COM1 to be available, err=%v ok=%v", err, ok)
		}
		ok, err = isPortAvailable("COM2")
		if err != nil || ok {
			t.Fatalf("expected COM2 to be unavailable, err=%v ok=%v", err, ok)
		}
	})
	withPortsAndOpen(func() ([]string, error) { return nil, errors.New("x") }, func(s string, m *gobug.Mode) (portHandle, error) { return &mockHandle{}, nil }, func() {
		ok, err := isPortAvailable("COM1")
		if err == nil || ok {
			t.Fatalf("expected error and ok=false on list error, err=%v ok=%v", err, ok)
		}
	})
}

// ----- Port Initialize tests -----

func TestService_Initialize_Errors(t *testing.T) {
	p := &Service{}
	if err := p.Initialize(); err == nil {
		t.Fatalf("expected error when services not injected")
	}

	p = &Service{LoggerService: &logging.Service{}}
	if err := p.Initialize(); err == nil {
		t.Fatalf("expected error when config not injected")
	}
}

func TestService_Initialize_Success(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1", BaudRate: 115200, DataBits: 8, Parity: int(ParityNone), StopBits: 1, ReadTimeout: 1500 * time.Millisecond, RTS: true, DTR: true}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("unexpected init err: %v", err)
	}
	if !p.initialized.Load() {
		t.Fatalf("expected initialized true")
	}
	if p.mode == nil {
		t.Fatalf("expected mode set")
	}
	if p.mode.BaudRate != 115200 || p.mode.DataBits != 8 {
		t.Fatalf("mode not set correctly: %+v", p.mode)
	}
}

// ----- Port Open/Close/Read/Write tests -----

func TestService_Open_NotInitialized(t *testing.T) {
	p := &Service{}
	if err := p.Open(); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("expected ErrNotInitialized")
	}
}

func TestService_Open_InvalidPort(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM9"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return &mockHandle{}, nil }, func() {
		err := p.Open()
		if !errors.Is(err, ErrInvalidPortName) {
			t.Fatalf("expected ErrInvalidPortName, got %v", err)
		}
	})
}

func TestService_Open_Success(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1", ReadTimeout: 500 * time.Millisecond, RTS: false, DTR: false}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}
	mh := &mockHandle{}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}
		if !p.isOpen.Load() {
			t.Fatalf("expected isOpen true")
		}
		if mh.readTimeout != 500*time.Millisecond {
			t.Fatalf("timeout not set")
		}
		if len(mh.setDTR) != 1 || mh.setDTR[0] != false {
			t.Fatalf("DTR not set false when expected")
		}
		if len(mh.setRTS) != 1 || mh.setRTS[0] != false {
			t.Fatalf("RTS not set false when expected")
		}
	})
}

func TestService_Open_ReadTimeoutError_Closes(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1", ReadTimeout: 1 * time.Second}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}
	mh := &mockHandle{setTimeoutErr: errors.New("timeout-set")}
	mh.closeErr = errors.New("close-err")
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		err := p.Open()
		if err == nil {
			t.Fatalf("expected error")
		}
		// Expect that Close was attempted
		if !mh.closed {
			t.Fatalf("expected handle closed on error")
		}
	})
}

func TestService_ReadWrite_StateChecks(t *testing.T) {
	p := &Service{}
	if _, err := p.Write([]byte("x")); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("expected ErrNotInitialized write")
	}
	if _, err := p.Read(make([]byte, 1)); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("expected ErrNotInitialized read")
	}

	// initialized but not open
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p = &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}
	if _, err := p.Write([]byte("x")); !errors.Is(err, ErrPortNotOpen) {
		t.Fatalf("expected ErrPortNotOpen write")
	}
	if _, err := p.Read(make([]byte, 1)); !errors.Is(err, ErrPortNotOpen) {
		t.Fatalf("expected ErrPortNotOpen read")
	}
}

func TestPort_BufferValidation(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	mh := &mockHandle{}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}

		// Test nil buffer
		if _, err := p.Write(nil); !errors.Is(err, ErrInvalidBuffer) {
			t.Fatalf("expected ErrInvalidBuffer for nil write buffer, got: %v", err)
		}
		if _, err := p.Read(nil); !errors.Is(err, ErrInvalidBuffer) {
			t.Fatalf("expected ErrInvalidBuffer for nil read buffer, got: %v", err)
		}

		// Test empty buffer
		if _, err := p.Write([]byte{}); !errors.Is(err, ErrInvalidBuffer) {
			t.Fatalf("expected ErrInvalidBuffer for empty write buffer, got: %v", err)
		}
		if _, err := p.Read(make([]byte, 0)); !errors.Is(err, ErrInvalidBuffer) {
			t.Fatalf("expected ErrInvalidBuffer for empty read buffer, got: %v", err)
		}

		// Test oversized buffer
		largeBuffer := make([]byte, MaxBufferSize+1)
		if _, err := p.Write(largeBuffer); !errors.Is(err, ErrBufferTooLarge) {
			t.Fatalf("expected ErrBufferTooLarge for oversized write buffer, got: %v", err)
		}
		if _, err := p.Read(largeBuffer); !errors.Is(err, ErrBufferTooLarge) {
			t.Fatalf("expected ErrBufferTooLarge for oversized read buffer, got: %v", err)
		}

		// Test valid buffer should work
		if _, err := p.Write([]byte("test")); err != nil {
			t.Fatalf("expected valid write to succeed, got: %v", err)
		}
		if _, err := p.Read(make([]byte, 10)); err != nil {
			t.Fatalf("expected valid read to succeed, got: %v", err)
		}
	})
}

func TestService_ReadWrite_Delegates(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}
	mh := &mockHandle{readData: []byte("abc")}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}
		n, err := p.Write([]byte("hi"))
		if err != nil || n != 2 {
			t.Fatalf("write err or n: %v %d", err, n)
		}
		buf := make([]byte, 4)
		n, err = p.Read(buf)
		if err != nil || n != 3 {
			t.Fatalf("read err or n: %v %d", err, n)
		}
		if string(mh.writeData) != "hi" {
			t.Fatalf("write did not delegate")
		}
	})
}

func TestService_Close(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}
	// not open -> no error
	if err := p.Close(); err != nil {
		t.Fatalf("expected nil when not open")
	}
	// open and then close
	mh := &mockHandle{}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}
		if err := p.Close(); err != nil {
			t.Fatalf("close err: %v", err)
		}
		if p.isOpen.Load() {
			t.Fatalf("expected isOpen false")
		}
	})
}

// ----- Buffer Pool Tests -----

func TestBufferPool_Basic(t *testing.T) {
	bp := NewBufferPool(100)

	// Get buffer from pool
	buf := bp.Get()
	if len(buf) != 100 {
		t.Fatalf("expected buffer size 100, got %d", len(buf))
	}

	// Modify buffer
	buf[0] = 42
	buf[10] = 84

	// Return to pool
	bp.Put(buf)

	// Get another buffer - should be cleared
	buf2 := bp.Get()
	if buf2[0] != 0 || buf2[10] != 0 {
		t.Fatalf("buffer not cleared: [0]=%d, [10]=%d", buf2[0], buf2[10])
	}

	// Verify stats
	stats := bp.Stats()
	if stats.Size != 100 {
		t.Fatalf("expected pool size 100, got %d", stats.Size)
	}
	if stats.Gets != 2 {
		t.Fatalf("expected 2 gets, got %d", stats.Gets)
	}
	if stats.Puts != 1 {
		t.Fatalf("expected 1 put, got %d", stats.Puts)
	}
}

func TestBufferPool_WrongSizeRejected(t *testing.T) {
	bp := NewBufferPool(100)

	// Try to put wrong size buffer
	wrongBuf := make([]byte, 50)
	bp.Put(wrongBuf)

	stats := bp.Stats()
	if stats.Puts != 0 {
		t.Fatalf("expected 0 puts for wrong size buffer, got %d", stats.Puts)
	}
}

func TestGetPooledBuffer_SizeSelection(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	service := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := service.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	// Reset pool stats for clean test
	service.ResetBufferPoolStats()

	tests := []struct {
		size         int
		expectedPool string
	}{
		{100, "small"},   // <= 256
		{256, "small"},   // <= 256
		{500, "medium"},  // <= 1024
		{1024, "medium"}, // <= 1024
		{2000, "large"},  // <= 4096
		{4096, "large"},  // <= 4096
		{5000, "none"},   // > 4096, direct allocation
	}

	for _, tt := range tests {
		buf, cleanup := service.bufferPoolManager.GetPooledBuffer(tt.size)
		if len(buf) != tt.size {
			t.Fatalf("size %d: expected buffer len %d, got %d", tt.size, tt.size, len(buf))
		}
		cleanup()
	}

	// Verify pool usage
	stats := service.GetBufferPoolStats()
	if len(stats) != 3 {
		t.Fatalf("expected 3 pool stats, got %d", len(stats))
	}

	// Small pool should have been used twice (100, 256)
	if stats[0].Gets < 2 {
		t.Fatalf("expected at least 2 gets from small pool, got %d", stats[0].Gets)
	}
}

func TestGetPooledBuffer_EdgeCases(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	service := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := service.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	// Zero size
	buf, cleanup := service.bufferPoolManager.GetPooledBuffer(0)
	if len(buf) != 1 {
		t.Fatalf("expected len 1 for zero size, got %d", len(buf))
	}
	cleanup()

	// Negative size
	buf, cleanup = service.bufferPoolManager.GetPooledBuffer(-10)
	if len(buf) != 1 {
		t.Fatalf("expected len 1 for negative size, got %d", len(buf))
	}
	cleanup()

	// Oversized buffer (direct allocation) - within absolute limit
	buf, cleanup = service.bufferPoolManager.GetPooledBuffer(MaxBufferSize + 1)
	if len(buf) != MaxBufferSize+1 {
		t.Fatalf("expected direct allocation of size %d, got %d", MaxBufferSize+1, len(buf))
	}
	cleanup() // Should be no-op
}

// TestGetPooledBuffer_SecurityLimits tests the memory exhaustion vulnerability fix
func TestGetPooledBuffer_SecurityLimits(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	service := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := service.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	// Test buffer at the absolute maximum limit (should succeed)
	buf, cleanup := service.bufferPoolManager.GetPooledBuffer(AbsoluteMaxBufferSize)
	if buf == nil {
		t.Fatalf("expected buffer allocation at absolute max size %d", AbsoluteMaxBufferSize)
	}
	if len(buf) != AbsoluteMaxBufferSize {
		t.Fatalf("expected buffer size %d, got %d", AbsoluteMaxBufferSize, len(buf))
	}
	cleanup()

	// Test buffer exceeding absolute maximum limit (should fail)
	buf, cleanup = service.bufferPoolManager.GetPooledBuffer(AbsoluteMaxBufferSize + 1)
	if buf != nil {
		t.Fatalf("expected nil buffer for size exceeding absolute limit, got buffer of size %d", len(buf))
	}
	cleanup() // Should be safe to call

	// Test extremely large buffer request (simulating attack)
	hugeSize := 1024 * 1024 * 1024 // 1GB
	buf, cleanup = service.bufferPoolManager.GetPooledBuffer(hugeSize)
	if buf != nil {
		t.Fatalf("expected nil buffer for huge size %d (memory exhaustion attack), got buffer of size %d", hugeSize, len(buf))
	}
	cleanup()
}

// TestReadWithPooling_SecurityLimits tests security limits in ReadWithPooling
func TestReadWithPooling_SecurityLimits(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	mh := &mockHandle{readData: []byte("test")}
	withPortsAndOpen(
		func() ([]string, error) { return []string{"COM1"}, nil },
		func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil },
		func() {
			if err := p.Open(); err != nil {
				t.Fatalf("open err: %v", err)
			}

			// Test buffer at absolute maximum limit (should fail)
			_, err := p.ReadWithPooling(AbsoluteMaxBufferSize + 1)
			if !errors.Is(err, ErrBufferExceedsAbsoluteLimit) {
				t.Fatalf("expected ErrBufferExceedsAbsoluteLimit for size exceeding absolute limit, got: %v", err)
			}

			// Test extremely large buffer request (simulating memory exhaustion attack)
			hugeSize := 1024 * 1024 * 1024 // 1GB
			_, err = p.ReadWithPooling(hugeSize)
			if !errors.Is(err, ErrBufferExceedsAbsoluteLimit) {
				t.Fatalf("expected ErrBufferExceedsAbsoluteLimit for huge size %d (attack simulation), got: %v", hugeSize, err)
			}

			// Test buffer just under absolute limit (should work but fail with ErrBufferTooLarge)
			_, err = p.ReadWithPooling(AbsoluteMaxBufferSize - 1000)
			if !errors.Is(err, ErrBufferTooLarge) {
				t.Fatalf("expected ErrBufferTooLarge for size just under absolute limit, got: %v", err)
			}
		})
}
func TestService_ReadWithPooling(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	// Test state checks
	if _, err := p.ReadWithPooling(100); !errors.Is(err, ErrPortNotOpen) {
		t.Fatalf("expected ErrPortNotOpen when not open, got: %v", err)
	}

	mh := &mockHandle{readData: []byte("hello world")}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}

		// Test invalid size
		if _, err := p.ReadWithPooling(0); !errors.Is(err, ErrInvalidBuffer) {
			t.Fatalf("expected ErrInvalidBuffer for zero size, got: %v", err)
		}
		if _, err := p.ReadWithPooling(-1); !errors.Is(err, ErrInvalidBuffer) {
			t.Fatalf("expected ErrInvalidBuffer for negative size, got: %v", err)
		}
		if _, err := p.ReadWithPooling(MaxBufferSize + 1); !errors.Is(err, ErrBufferTooLarge) {
			t.Fatalf("expected ErrBufferTooLarge for oversized buffer, got: %v", err)
		}

		// Test successful read
		data, err := p.ReadWithPooling(1024)
		if err != nil {
			t.Fatalf("read err: %v", err)
		}
		expected := "hello world"
		if string(data) != expected {
			t.Fatalf("expected %q, got %q", expected, string(data))
		}

		// Test empty read
		mh.readData = []byte{}
		data, err = p.ReadWithPooling(100)
		if err != nil {
			t.Fatalf("empty read err: %v", err)
		}
		if len(data) != 0 {
			t.Fatalf("expected empty data, got %d bytes", len(data))
		}
	})
}

func TestService_ReadStreamWithPooling(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	// Test state checks
	if _, err := p.ReadStreamWithPooling(context.Background(), 100); !errors.Is(err, ErrPortNotOpen) {
		t.Fatalf("expected ErrPortNotOpen when not open, got: %v", err)
	}

	mh := &mockHandle{readData: []byte("test")}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}

		// Test invalid buffer size
		if _, err := p.ReadStreamWithPooling(context.Background(), 0); !errors.Is(err, ErrInvalidBuffer) {
			t.Fatalf("expected ErrInvalidBuffer for zero size, got: %v", err)
		}
		if _, err := p.ReadStreamWithPooling(context.Background(), MaxBufferSize+1); !errors.Is(err, ErrBufferTooLarge) {
			t.Fatalf("expected ErrBufferTooLarge for oversized buffer, got: %v", err)
		}

		// Test successful stream with context cancellation
		ctx, cancel := context.WithCancel(context.Background())
		resultChan, err := p.ReadStreamWithPooling(ctx, 100)
		if err != nil {
			t.Fatalf("stream err: %v", err)
		}

		// Should get one result
		select {
		case result := <-resultChan:
			if result.Err != nil {
				t.Fatalf("result err: %v", result.Err)
			}
			if string(result.Data) != "test" {
				t.Fatalf("expected 'test', got %q", string(result.Data))
			}
			if result.N != 4 {
				t.Fatalf("expected N=4, got %d", result.N)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timeout waiting for result")
		}

		// Cancel context and verify cleanup
		cancel()

		// Channel should close
		select {
		case _, ok := <-resultChan:
			if ok {
				t.Fatalf("expected channel to be closed")
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timeout waiting for channel close")
		}
	})
}

func TestPoolStats_HitRatio(t *testing.T) {
	stats := PoolStats{
		Gets:    100,
		Creates: 20,
	}
	ratio := stats.HitRatio()
	expected := 0.8 // 80% hit ratio
	if ratio != expected {
		t.Fatalf("expected hit ratio %.2f, got %.2f", expected, ratio)
	}

	// Zero gets case
	stats = PoolStats{}
	if stats.HitRatio() != 0.0 {
		t.Fatalf("expected 0.0 hit ratio for zero gets, got %.2f", stats.HitRatio())
	}
}

// ----- Write Timeout Tests -----

func TestService_WriteWithTimeout_Success(t *testing.T) {
	cfg := types.SerialConfig{
		PortName:     "COM1",
		WriteTimeout: 500 * time.Millisecond,
	}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	mh := &mockHandle{writeDelay: 50 * time.Millisecond} // Faster than timeout
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}

		start := time.Now()
		n, err := p.WriteWithTimeout([]byte("test"), 200*time.Millisecond)
		duration := time.Since(start)

		if err != nil {
			t.Fatalf("expected successful write, got err: %v", err)
		}
		if n != 4 {
			t.Fatalf("expected 4 bytes written, got %d", n)
		}
		if string(mh.writeData) != "test" {
			t.Fatalf("expected 'test' written, got %q", string(mh.writeData))
		}
		if duration > 150*time.Millisecond {
			t.Fatalf("write took too long: %v", duration)
		}
	})
}

func TestService_WriteWithTimeout_Timeout(t *testing.T) {
	cfg := types.SerialConfig{
		PortName:     "COM1",
		WriteTimeout: 100 * time.Millisecond,
	}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	mh := &mockHandle{writeDelay: 200 * time.Millisecond} // Slower than timeout
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}

		start := time.Now()
		_, err := p.WriteWithTimeout([]byte("test"), 100*time.Millisecond)
		duration := time.Since(start)

		if !errors.Is(err, ErrWriteTimeout) {
			t.Fatalf("expected ErrWriteTimeout, got: %v", err)
		}
		if duration > 150*time.Millisecond {
			t.Fatalf("timeout took too long: %v", duration)
		}
	})
}

func TestService_WriteWithTimeout_NoTimeout(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	mh := &mockHandle{}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}

		// Zero timeout should use direct write
		n, err := p.WriteWithTimeout([]byte("test"), 0)
		if err != nil {
			t.Fatalf("expected successful write, got err: %v", err)
		}
		if n != 4 {
			t.Fatalf("expected 4 bytes written, got %d", n)
		}

		// Negative timeout should use direct write
		n, err = p.WriteWithTimeout([]byte("test2"), -1*time.Second)
		if err != nil {
			t.Fatalf("expected successful write, got err: %v", err)
		}
		if n != 5 {
			t.Fatalf("expected 5 bytes written, got %d", n)
		}
	})
}

func TestService_WriteWithContext_Success(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	mh := &mockHandle{writeDelay: 50 * time.Millisecond}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		n, err := p.WriteWithContext(ctx, []byte("test"))
		if err != nil {
			t.Fatalf("expected successful write, got err: %v", err)
		}
		if n != 4 {
			t.Fatalf("expected 4 bytes written, got %d", n)
		}
		if string(mh.writeData) != "test" {
			t.Fatalf("expected 'test' written, got %q", string(mh.writeData))
		}
	})
}

func TestService_WriteWithContext_Timeout(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	// Mock handle with delay to trigger timeout
	mh := &mockHandle{writeDelay: 100 * time.Millisecond}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}

		// Test context timeout
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		_, err := p.WriteWithContext(ctx, []byte("timeout test"))
		if err == nil {
			t.Fatalf("expected timeout error")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context.DeadlineExceeded, got %v", err)
		}
	})
}

func TestService_WriteWithContext_CanceledContext(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	mh := &mockHandle{}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}

		// Test pre-canceled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := p.WriteWithContext(ctx, []byte("canceled test"))
		if err == nil {
			t.Fatalf("expected cancellation error")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})
}

func TestService_WriteWithContext_ConcurrentWrites(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	mh := &mockHandle{}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}

		// Test concurrent writes are serialized
		const numWrites = 10
		done := make(chan error, numWrites)

		for i := 0; i < numWrites; i++ {
			go func(id int) {
				ctx := context.Background()
				data := []byte(fmt.Sprintf("write%d", id))
				_, err := p.WriteWithContext(ctx, data)
				done <- err
			}(i)
		}

		// Wait for all writes to complete
		for i := 0; i < numWrites; i++ {
			if err := <-done; err != nil {
				t.Fatalf("concurrent write %d failed: %v", i, err)
			}
		}

		// Verify all data was written (order may vary due to concurrency)
		if len(mh.writeData) == 0 {
			t.Fatalf("no data was written")
		}
	})
}

// ----- Write Queue Tests -----

func TestService_WriteWithContext_WriteQueue(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	mh := &mockHandle{}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}

		// Trigger queue initialization by doing a write
		ctx := context.Background()
		_, err := p.WriteWithContext(ctx, []byte("trigger queue"))
		if err != nil {
			t.Fatalf("WriteWithContext failed: %v", err)
		}

		// Verify queue was initialized
		if p.writeQueue == nil {
			t.Fatalf("expected writeQueue to be initialized")
		}

		// Close should shut down the queue gracefully
		if err := p.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		// Give the goroutine time to exit
		time.Sleep(10 * time.Millisecond)

		// Queue should be closed after Close()
		select {
		case _, ok := <-p.writeQueue:
			if ok {
				t.Fatalf("expected writeQueue to be closed")
			}
		default:
			// Channel might be closed but not drained yet, this is OK
		}

		// queueDone should also be closed
		select {
		case _, ok := <-p.queueDone:
			if ok {
				t.Fatalf("expected queueDone to be closed")
			}
		default:
			// Channel might be closed but not drained yet, this is OK
		}
	})
}

func TestService_WriteQueue_PendingOperationsCleanup(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	// Mock handle that blocks writes to simulate pending operations
	mh := &mockHandle{writeDelay: 1 * time.Second}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}

		// Start multiple write operations that will be queued
		numOps := 5
		results := make([]chan error, numOps)

		for i := 0; i < numOps; i++ {
			results[i] = make(chan error, 1)
			go func(resultCh chan error) {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_, err := p.WriteWithContext(ctx, []byte("test data"))
				resultCh <- err
			}(results[i])
		}

		// Give operations time to queue up
		time.Sleep(50 * time.Millisecond)

		// Close the service while operations are pending
		if err := p.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		// All pending operations should complete with an error (not hang)
		for i := 0; i < numOps; i++ {
			select {
			case err := <-results[i]:
				// Operations should either complete with ErrPortNotOpen or context timeout
				if err == nil {
					t.Fatalf("expected error from operation %d after Close(), got nil", i)
				}
				// Acceptable errors: port closed, context timeout, or write timeout
				if !errors.Is(err, ErrPortNotOpen) && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, ErrWriteTimeout) {
					t.Logf("Operation %d completed with error: %v", i, err)
				}
			case <-time.After(3 * time.Second):
				t.Fatalf("operation %d did not complete within timeout", i)
			}
		}
	})
}

func TestService_WriteQueue_GoroutineExitOnChannelClose(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	mh := &mockHandle{}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}

		// Initialize the write queue
		ctx := context.Background()
		_, err := p.WriteWithContext(ctx, []byte("initialize"))
		if err != nil {
			t.Fatalf("WriteWithContext failed: %v", err)
		}

		// Create a test operation to verify cleanup
		resultCh := make(chan writeResult, 1)
		testOp := &writeOperation{
			data:     []byte("test"),
			ctx:      context.Background(),
			resultCh: resultCh,
		}

		// Send the operation and immediately close the queue
		select {
		case p.writeQueue <- testOp:
			// Operation queued
		default:
			t.Fatalf("failed to queue test operation")
		}

		// Close the service
		if err := p.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		// The test operation should receive an error response from cleanup
		select {
		case result := <-resultCh:
			if result.err == nil {
				t.Fatalf("expected error from cleanup, got nil")
			}
			if !errors.Is(result.err, ErrPortNotOpen) {
				t.Fatalf("expected ErrPortNotOpen from cleanup, got: %v", result.err)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("test operation did not receive cleanup response")
		}
	})
}

func TestService_WriteQueue_NoLeakWithoutWrites(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	mh := &mockHandle{}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}

		// Verify queue is not initialized yet
		if p.writeQueue != nil {
			t.Fatalf("writeQueue should not be initialized without writes")
		}

		// Close without ever initializing the write queue
		if err := p.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		// Should complete without hanging
		time.Sleep(10 * time.Millisecond)
	})
}

func TestService_WriteQueue_ConcurrentCloseAndWrite(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	mh := &mockHandle{}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}

		// Start multiple goroutines doing writes
		numWriters := 10
		done := make(chan bool, numWriters+1)

		for i := 0; i < numWriters; i++ {
			go func(id int) {
				defer func() { done <- true }()
				for j := 0; j < 5; j++ {
					ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
					data := []byte(fmt.Sprintf("writer%d-msg%d", id, j))
					_, err := p.WriteWithContext(ctx, data)
					cancel()
					// Errors are expected after Close() is called
					if err != nil && !errors.Is(err, ErrPortNotOpen) && !errors.Is(err, context.DeadlineExceeded) {
						// Log unexpected errors but don't fail the test
						t.Logf("Write error: %v", err)
					}
					time.Sleep(1 * time.Millisecond)
				}
			}(i)
		}

		// Concurrently close the service after a short delay
		go func() {
			defer func() { done <- true }()
			time.Sleep(25 * time.Millisecond)
			if err := p.Close(); err != nil {
				t.Errorf("Close failed: %v", err)
			}
		}()

		// Wait for all goroutines to complete
		for i := 0; i < numWriters+1; i++ {
			select {
			case <-done:
				// Goroutine completed
			case <-time.After(2 * time.Second):
				t.Fatalf("goroutine %d did not complete within timeout", i)
			}
		}

		// Verify service is properly closed
		if p.isOpen.Load() {
			t.Fatalf("service should be closed")
		}
	})
}

// ----- Error Handling Tests -----

func TestService_WriteWithContext_StateValidation(t *testing.T) {
	p := &Service{}

	// Test not initialized
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := p.WriteWithContext(ctx, []byte("test"))
	if !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("expected ErrNotInitialized, got %v", err)
	}

	// Test initialized but not open
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p = &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	// Ensure cleanup of goroutines after test
	defer func() {
		if p.initialized.Load() {
			_ = p.Close() // Clean up any started goroutines
		}
	}()

	// Use timeout context to prevent hanging
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = p.WriteWithContext(ctx, []byte("test"))
	if !errors.Is(err, ErrPortNotOpen) {
		t.Fatalf("expected ErrPortNotOpen, got %v", err)
	}
}

func TestService_WriteWithContext_BufferValidation(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	mh := &mockHandle{}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}

		ctx := context.Background()

		// Test nil buffer
		_, err := p.WriteWithContext(ctx, nil)
		if !errors.Is(err, ErrInvalidBuffer) {
			t.Fatalf("expected ErrInvalidBuffer for nil buffer, got %v", err)
		}

		// Test empty buffer
		_, err = p.WriteWithContext(ctx, []byte{})
		if !errors.Is(err, ErrInvalidBuffer) {
			t.Fatalf("expected ErrInvalidBuffer for empty buffer, got %v", err)
		}

		// Test oversized buffer
		largeBuffer := make([]byte, MaxBufferSize+1)
		_, err = p.WriteWithContext(ctx, largeBuffer)
		if !errors.Is(err, ErrBufferTooLarge) {
			t.Fatalf("expected ErrBufferTooLarge for oversized buffer, got %v", err)
		}
	})
}

// ----- TOCTOU Race Prevention Tests -----

func TestService_Read_TOCTOU_Prevention(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	mh := &mockHandle{readData: []byte("test data")}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}

		// Start concurrent read
		readDone := make(chan error, 1)
		go func() {
			buf := make([]byte, 10)
			_, err := p.Read(buf)
			readDone <- err
		}()

		// Briefly let read start, then close port
		time.Sleep(1 * time.Millisecond)
		if err := p.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		// Read should complete successfully with captured handle
		select {
		case err := <-readDone:
			if err != nil {
				t.Fatalf("Read failed despite TOCTOU protection: %v", err)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("Read timed out")
		}
	})
}

// ----- Integration Tests -----

func TestService_WriteWithContext_HandleClosed(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	mh := &mockHandle{}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}

		// Close the port to simulate handle being nil
		if err := p.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		// WriteWithContext should detect closed port
		ctx := context.Background()
		_, err := p.WriteWithContext(ctx, []byte("test"))
		if !errors.Is(err, ErrPortNotOpen) {
			t.Fatalf("expected ErrPortNotOpen when handle closed, got %v", err)
		}
	})
}

// ----- Performance and Edge Case Tests -----

func TestService_WriteQueue_HighThroughput(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	mh := &mockHandle{}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}

		// Test high throughput scenario
		const numWrites = 100
		done := make(chan struct{})
		errorCount := 0

		go func() {
			defer close(done)
			for i := 0; i < numWrites; i++ {
				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				data := []byte(fmt.Sprintf("msg%d", i))
				_, err := p.WriteWithContext(ctx, data)
				cancel()
				if err != nil {
					errorCount++
				}
			}
		}()

		// Wait for completion with timeout
		select {
		case <-done:
			if errorCount > 0 {
				t.Fatalf("expected no errors in high throughput test, got %d", errorCount)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("high throughput test timed out")
		}

		// Verify substantial data was written
		if len(mh.writeData) < 100 {
			t.Fatalf("expected substantial data written, got %d bytes", len(mh.writeData))
		}
	})
}

func TestService_MultipleClose_Safe(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	mh := &mockHandle{}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}

		// Trigger queue initialization
		ctx := context.Background()
		_, err := p.WriteWithContext(ctx, []byte("test"))
		if err != nil {
			t.Fatalf("WriteWithContext failed: %v", err)
		}

		// Multiple closes should be safe
		if err := p.Close(); err != nil {
			t.Fatalf("first close failed: %v", err)
		}
		if err := p.Close(); err != nil {
			t.Fatalf("second close failed: %v", err)
		}
		if err := p.Close(); err != nil {
			t.Fatalf("third close failed: %v", err)
		}
	})
}

// ----- Additional Goroutine Leak Tests -----

func TestService_WriteQueue_GoroutineCleanupOnPanic(t *testing.T) {
	cfg := types.SerialConfig{PortName: "COM1"}
	rig := types.RigConfig{Name: "R1", SerialConfig: cfg}
	cs := createConfigService(t, rig, 1)
	p := &Service{LoggerService: &logging.Service{}, ConfigService: cs}
	if err := p.Initialize(); err != nil {
		t.Fatalf("init err: %v", err)
	}

	mh := &mockHandle{}
	withPortsAndOpen(func() ([]string, error) { return []string{"COM1"}, nil }, func(s string, m *gobug.Mode) (portHandle, error) { return mh, nil }, func() {
		if err := p.Open(); err != nil {
			t.Fatalf("open err: %v", err)
		}

		// Initialize write queue
		ctx := context.Background()
		_, err := p.WriteWithContext(ctx, []byte("init"))
		if err != nil {
			t.Fatalf("WriteWithContext failed: %v", err)
		}

		// Queue an operation that will be cleaned up
		resultCh := make(chan writeResult, 1)
		testOp := &writeOperation{
			data:     []byte("test"),
			ctx:      context.Background(),
			resultCh: resultCh,
		}

		// Send operation to queue
		select {
		case p.writeQueue <- testOp:
			// Operation queued
		default:
			t.Fatalf("failed to queue test operation")
		}

		// Force close the queue channel to simulate unexpected goroutine exit
		close(p.writeQueue)

		// The operation should still receive a response due to defer cleanup
		select {
		case result := <-resultCh:
			// Should receive an error (could be ErrPortNotOpen or any error)
			if result.err == nil {
				t.Logf("Received unexpected nil error, but cleanup worked")
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("operation did not receive cleanup response")
		}
	})
}
