package serial

import (
	"strings"
	"testing"
	"time"

	"github.com/7Q-Station-Manager/types"
)

func TestValidateConfig_ValidConfig(t *testing.T) {
	cfg := types.SerialConfig{
		PortName:           "COM1",
		BaudRate:           9600,
		DataBits:           8,
		Parity:             int(ParityNone),
		StopBits:           1,
		ReadTimeout:        time.Second,
		WriteTimeout:       time.Second,
		MetricsChannelSize: 50,
	}

	if err := ValidateConfig(&cfg); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestValidateConfig_EmptyPortName(t *testing.T) {
	cfg := types.SerialConfig{
		PortName: "",
		BaudRate: 9600,
		DataBits: 8,
		Parity:   int(ParityNone),
		StopBits: 1,
	}

	err := ValidateConfig(&cfg)
	if err == nil {
		t.Fatal("expected error for empty port name")
	}
	if !strings.Contains(err.Error(), "port name cannot be empty") {
		t.Fatalf("expected 'port name cannot be empty' error, got: %v", err)
	}
}

func TestValidateConfig_InvalidBaudRate(t *testing.T) {
	tests := []struct {
		baudRate int
		wantErr  bool
	}{
		{1200, false},   // Valid
		{9600, false},   // Valid
		{115200, false}, // Valid
		{12345, true},   // Invalid
		{0, true},       // Invalid
		{-9600, true},   // Invalid
		{1000000, true}, // Invalid (too high)
	}

	for _, tt := range tests {
		cfg := types.SerialConfig{
			PortName: "COM1",
			BaudRate: tt.baudRate,
			DataBits: 8,
			Parity:   int(ParityNone),
			StopBits: 1,
		}

		err := ValidateConfig(&cfg)
		if (err != nil) != tt.wantErr {
			t.Fatalf("baudRate=%d: wantErr=%v, got=%v", tt.baudRate, tt.wantErr, err)
		}
		if tt.wantErr && !strings.Contains(err.Error(), "invalid baud rate") {
			t.Fatalf("baudRate=%d: expected 'invalid baud rate' error, got: %v", tt.baudRate, err)
		}
	}
}

func TestValidateConfig_InvalidDataBits(t *testing.T) {
	tests := []struct {
		dataBits int
		wantErr  bool
	}{
		{4, true},  // Too small
		{5, false}, // Valid
		{6, false}, // Valid
		{7, false}, // Valid
		{8, false}, // Valid
		{9, true},  // Too large
		{0, true},  // Invalid
		{-1, true}, // Invalid
	}

	for _, tt := range tests {
		cfg := types.SerialConfig{
			PortName: "COM1",
			BaudRate: 9600,
			DataBits: tt.dataBits,
			Parity:   int(ParityNone),
			StopBits: 1,
		}

		err := ValidateConfig(&cfg)
		if (err != nil) != tt.wantErr {
			t.Fatalf("dataBits=%d: wantErr=%v, got=%v", tt.dataBits, tt.wantErr, err)
		}
		if tt.wantErr && !strings.Contains(err.Error(), "data bits must be 5-8") {
			t.Fatalf("dataBits=%d: expected 'data bits' error, got: %v", tt.dataBits, err)
		}
	}
}

func TestValidateConfig_InvalidParity(t *testing.T) {
	tests := []struct {
		parity  int
		wantErr bool
	}{
		{int(ParityNone), false},  // Valid
		{int(ParityOdd), false},   // Valid
		{int(ParityEven), false},  // Valid
		{int(ParityMark), false},  // Valid
		{int(ParitySpace), false}, // Valid
		{99, true},                // Invalid
		{-1, true},                // Invalid
	}

	for _, tt := range tests {
		cfg := types.SerialConfig{
			PortName: "COM1",
			BaudRate: 9600,
			DataBits: 8,
			Parity:   tt.parity,
			StopBits: 1,
		}

		err := ValidateConfig(&cfg)
		if (err != nil) != tt.wantErr {
			t.Fatalf("parity=%d: wantErr=%v, got=%v", tt.parity, tt.wantErr, err)
		}
		if tt.wantErr && !strings.Contains(err.Error(), "invalid parity") {
			t.Fatalf("parity=%d: expected 'invalid parity' error, got: %v", tt.parity, err)
		}
	}
}

func TestValidateConfig_InvalidStopBits(t *testing.T) {
	tests := []struct {
		stopBits float32
		wantErr  bool
	}{
		{1, false},   // Valid
		{1.5, false}, // Valid
		{2, false},   // Valid
		{0.5, true},  // Invalid
		{3, true},    // Invalid
		{0, false},   // Invalid
		{-1, true},   // Invalid
	}

	for _, tt := range tests {
		cfg := types.SerialConfig{
			PortName: "COM1",
			BaudRate: 9600,
			DataBits: 8,
			Parity:   int(ParityNone),
			StopBits: tt.stopBits,
		}

		err := ValidateConfig(&cfg)
		if (err != nil) != tt.wantErr {
			t.Fatalf("stopBits=%.1f: wantErr=%v, got=%v", tt.stopBits, tt.wantErr, err)
		}
		if tt.wantErr && !strings.Contains(err.Error(), "stop bits must be") {
			t.Fatalf("stopBits=%.1f: expected 'stop bits' error, got: %v", tt.stopBits, err)
		}
	}
}

func TestValidateConfig_NegativeTimeouts(t *testing.T) {
	// Test negative read timeout
	cfg := types.SerialConfig{
		PortName:    "COM1",
		BaudRate:    9600,
		DataBits:    8,
		Parity:      int(ParityNone),
		StopBits:    1,
		ReadTimeout: -1 * time.Second,
	}

	err := ValidateConfig(&cfg)
	if err == nil {
		t.Fatal("expected error for negative read timeout")
	}
	if !strings.Contains(err.Error(), "read timeout cannot be negative") {
		t.Fatalf("expected 'read timeout' error, got: %v", err)
	}

	// Test negative write timeout
	cfg = types.SerialConfig{
		PortName:     "COM1",
		BaudRate:     9600,
		DataBits:     8,
		Parity:       int(ParityNone),
		StopBits:     1,
		WriteTimeout: -1 * time.Second,
	}

	err = ValidateConfig(&cfg)
	if err == nil {
		t.Fatal("expected error for negative write timeout")
	}
	if !strings.Contains(err.Error(), "write timeout cannot be negative") {
		t.Fatalf("expected 'write timeout' error, got: %v", err)
	}
}

func TestValidateConfig_InvalidMetricsChannelSize(t *testing.T) {
	tests := []struct {
		size    int64
		wantErr bool
	}{
		{0, false},     // Valid (will use default)
		{50, false},    // Valid
		{10000, false}, // Valid (at limit)
		{10001, true},  // Invalid (exceeds limit)
		{-1, true},     // Invalid (negative)
		{-100, true},   // Invalid (negative)
	}

	for _, tt := range tests {
		cfg := types.SerialConfig{
			PortName:           "COM1",
			BaudRate:           9600,
			DataBits:           8,
			Parity:             int(ParityNone),
			StopBits:           1,
			MetricsChannelSize: tt.size,
		}

		err := ValidateConfig(&cfg)
		if (err != nil) != tt.wantErr {
			t.Fatalf("metricsChannelSize=%d: wantErr=%v, got=%v", tt.size, tt.wantErr, err)
		}
		if tt.wantErr && !strings.Contains(err.Error(), "metrics channel size") {
			t.Fatalf("metricsChannelSize=%d: expected 'metrics channel size' error, got: %v", tt.size, err)
		}
	}
}

func TestIsPortAvailable_PathTraversal(t *testing.T) {
	ok, err := isPortAvailable("../../../etc/passwd")
	if err == nil || ok {
		t.Fatal("expected error for path traversal attempt")
	}
	if !strings.Contains(err.Error(), "path traversal") {
		t.Fatalf("expected 'path traversal' error, got: %v", err)
	}
}

func TestIsPortAvailable_InvalidPattern(t *testing.T) {
	tests := []struct {
		portName string
		wantErr  bool
	}{
		{"/tmp/malicious", true},     // Invalid path
		{"/etc/passwd", true},        // Invalid path
		{"/home/user/fake", true},    // Invalid path
		{"INVALID", true},            // Invalid pattern
		{"/dev/ttyUSB0", false},      // Valid Unix pattern (but may not exist)
		{"/dev/ttyS0", false},        // Valid Unix pattern (but may not exist)
		{"/dev/cu.usbserial", false}, // Valid macOS pattern (but may not exist)
		{"COM1", false},              // Valid Windows pattern (but may not exist)
		{"COM99", false},             // Valid Windows pattern (but may not exist)
		{"COMPORT", true},            // Invalid Windows pattern (too long)
	}

	for _, tt := range tests {
		_, err := isPortAvailable(tt.portName)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("portName=%s: expected error, got nil", tt.portName)
			}
		} else {
			// For valid patterns, we expect either success or a "not found" error
			// The pattern is valid even if the port doesn't exist
			if err != nil && strings.Contains(err.Error(), "doesn't match expected pattern") {
				t.Fatalf("portName=%s: pattern should be valid, got error: %v", tt.portName, err)
			}
			// If we get here and the port doesn't exist, it just means the port isn't available
			// which is fine for pattern validation test
		}
	}
}

func TestIsValidPortPattern(t *testing.T) {
	tests := []struct {
		portName string
		want     bool
	}{
		// Valid Windows ports
		{"COM1", true},
		{"COM2", true},
		{"COM99", true},
		{"COM999", true},
		// Invalid Windows ports
		{"COMPORT", false}, // Too long
		{"COM", false},     // Too short (no number)
		// Valid Unix/Linux ports
		{"/dev/ttyUSB0", true},
		{"/dev/ttyS0", true},
		{"/dev/ttyACM0", true},
		{"/dev/ttyAMA0", true},
		// Valid macOS ports
		{"/dev/cu.usbserial", true},
		{"/dev/cu.usbmodem", true},
		// Invalid patterns
		{"/tmp/fake", false},
		{"/etc/passwd", false},
		{"INVALID", false},
		{"", false},
		{"/dev/null", false},
		{"/dev/zero", false},
	}

	for _, tt := range tests {
		got := isValidPortPattern(tt.portName)
		if got != tt.want {
			t.Fatalf("isValidPortPattern(%q) = %v, want %v", tt.portName, got, tt.want)
		}
	}
}
