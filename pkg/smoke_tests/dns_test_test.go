package smoke_tests

import (
	"context"
	"testing"
	"time"
)

func TestDNSTest_Pass(t *testing.T) {
	config := &TestConfig{
		Name:     "test-dns-pass",
		Type:     "dns",
		Enabled:  true,
		Severity: "high",
		Timeout:  5 * time.Second,
		Domain:   "localhost",
	}

	test := NewDNSTest(config)
	ctx := context.Background()
	result, err := test.Run(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Name != config.Name {
		t.Errorf("expected name %s, got %s", config.Name, result.Name)
	}

	if result.Type != "dns" {
		t.Errorf("expected type dns, got %s", result.Type)
	}

	if result.Status != "pass" {
		t.Errorf("expected status pass, got %s: %s", result.Status, result.Message)
	}

	if result.Duration == 0 {
		t.Error("expected non-zero duration")
	}
}

func TestDNSTest_InvalidDomain(t *testing.T) {
	config := &TestConfig{
		Name:     "test-dns-invalid",
		Type:     "dns",
		Enabled:  true,
		Severity: "high",
		Timeout:  5 * time.Second,
		Domain:   "invalid-domain-that-does-not-exist-12345.local",
	}

	test := NewDNSTest(config)
	ctx := context.Background()
	result, err := test.Run(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "fail" {
		t.Errorf("expected status fail, got %s", result.Status)
	}
}

func TestDNSTest_Timeout(t *testing.T) {
	config := &TestConfig{
		Name:     "test-dns-timeout",
		Type:     "dns",
		Enabled:  true,
		Severity: "high",
		Timeout:  1 * time.Millisecond, // Very short timeout
		Domain:   "example.com",
	}

	test := NewDNSTest(config)
	ctx := context.Background()
	result, err := test.Run(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Either timeout or fail is acceptable
	if result.Status != "fail" && result.Status != "timeout" {
		t.Errorf("expected status fail or timeout, got %s", result.Status)
	}
}

func TestDNSTest_GetConfig(t *testing.T) {
	config := &TestConfig{
		Name:     "test",
		Type:     "dns",
		Enabled:  true,
		Severity: "high",
		Timeout:  5 * time.Second,
		Domain:   "test.local",
	}

	test := NewDNSTest(config)

	if test.GetConfig() != config {
		t.Error("GetConfig should return the same config")
	}

	if test.GetName() != config.Name {
		t.Error("GetName should return config name")
	}

	if test.GetType() != "dns" {
		t.Error("GetType should return dns")
	}
}
