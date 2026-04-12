package smoke_tests

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestTCPTest_Success(t *testing.T) {
	// Create a listener to bind on a random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	// Extract the port
	addr := listener.Addr().(*net.TCPAddr)
	port := addr.Port

	config := &TestConfig{
		Name:     "test-tcp-success",
		Type:     "tcp",
		Enabled:  true,
		Severity: "high",
		Timeout:  5 * time.Second,
		Host:     "127.0.0.1",
		Port:     port,
	}

	test := NewTCPTest(config)
	ctx := context.Background()
	result, err := test.Run(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "pass" {
		t.Errorf("expected status pass, got %s: %s", result.Status, result.Message)
	}

	if result.Duration == 0 {
		t.Error("expected non-zero duration")
	}
}

func TestTCPTest_ConnectionRefused(t *testing.T) {
	// Use a port that's unlikely to be open
	config := &TestConfig{
		Name:     "test-tcp-refused",
		Type:     "tcp",
		Enabled:  true,
		Severity: "high",
		Timeout:  2 * time.Second,
		Host:     "127.0.0.1",
		Port:     54321, // Random unused port
	}

	test := NewTCPTest(config)
	ctx := context.Background()
	result, err := test.Run(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "fail" {
		t.Errorf("expected status fail, got %s", result.Status)
	}
}

func TestTCPTest_InvalidHost(t *testing.T) {
	config := &TestConfig{
		Name:     "test-tcp-invalid-host",
		Type:     "tcp",
		Enabled:  true,
		Severity: "high",
		Timeout:  2 * time.Second,
		Host:     "invalid-host-that-does-not-exist-12345.local",
		Port:     80,
	}

	test := NewTCPTest(config)
	ctx := context.Background()
	result, err := test.Run(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "fail" {
		t.Errorf("expected status fail, got %s", result.Status)
	}
}

func TestTCPTest_Timeout(t *testing.T) {
	// Use a port that will timeout (non-routable IP)
	config := &TestConfig{
		Name:     "test-tcp-timeout",
		Type:     "tcp",
		Enabled:  true,
		Severity: "high",
		Timeout:  100 * time.Millisecond,
		Host:     "192.0.2.1", // Non-routable address
		Port:     80,
	}

	test := NewTCPTest(config)
	ctx := context.Background()
	result, err := test.Run(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be fail due to timeout or connection refused
	if result.Status != "fail" {
		t.Errorf("expected status fail, got %s: %s", result.Status, result.Message)
	}
}

func TestTCPTest_GetConfig(t *testing.T) {
	config := &TestConfig{
		Name:     "test",
		Type:     "tcp",
		Enabled:  true,
		Severity: "high",
		Timeout:  5 * time.Second,
		Host:     "localhost",
		Port:     8080,
	}

	test := NewTCPTest(config)

	if test.GetConfig() != config {
		t.Error("GetConfig should return the same config")
	}

	if test.GetName() != config.Name {
		t.Error("GetName should return config name")
	}

	if test.GetType() != "tcp" {
		t.Error("GetType should return tcp")
	}
}
