package smoke_tests

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestRegistry_AddAndGetTest(t *testing.T) {
	registry := NewTestRegistry()

	config := &TestConfig{
		Name:     "test1",
		Type:     "dns",
		Enabled:  true,
		Severity: "high",
		Timeout:  5 * time.Second,
		Domain:   "localhost",
	}

	test := NewDNSTest(config)

	err := registry.AddTest("test1", test)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	retrieved, exists := registry.GetTest("test1")
	if !exists {
		t.Error("test should exist in registry")
	}

	if retrieved.GetName() != test.GetName() {
		t.Error("retrieved test should match added test")
	}
}

func TestRegistry_RemoveTest(t *testing.T) {
	registry := NewTestRegistry()

	config := &TestConfig{
		Name:     "test1",
		Type:     "dns",
		Enabled:  true,
		Severity: "high",
		Timeout:  5 * time.Second,
		Domain:   "localhost",
	}

	test := NewDNSTest(config)
	registry.AddTest("test1", test)

	err := registry.RemoveTest("test1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, exists := registry.GetTest("test1")
	if exists {
		t.Error("test should not exist after removal")
	}
}

func TestRegistry_ListTests(t *testing.T) {
	registry := NewTestRegistry()

	for i := 1; i <= 3; i++ {
		config := &TestConfig{
			Name:     "test" + string(rune(i)),
			Type:     "dns",
			Enabled:  true,
			Severity: "high",
			Timeout:  5 * time.Second,
			Domain:   "localhost",
		}
		test := NewDNSTest(config)
		registry.AddTest("test"+string(rune(i)), test)
	}

	tests := registry.ListTests()
	if len(tests) != 3 {
		t.Errorf("expected 3 tests, got %d", len(tests))
	}
}

func TestRegistry_Count(t *testing.T) {
	registry := NewTestRegistry()

	if registry.Count() != 0 {
		t.Error("initial count should be 0")
	}

	config := &TestConfig{
		Name:     "test1",
		Type:     "dns",
		Enabled:  true,
		Severity: "high",
		Timeout:  5 * time.Second,
		Domain:   "localhost",
	}
	test := NewDNSTest(config)
	registry.AddTest("test1", test)

	if registry.Count() != 1 {
		t.Error("count should be 1 after adding test")
	}
}

func TestRegistry_Clear(t *testing.T) {
	registry := NewTestRegistry()

	// Add multiple tests
	for i := 1; i <= 3; i++ {
		config := &TestConfig{
			Name:     "test",
			Type:     "dns",
			Enabled:  true,
			Severity: "high",
			Timeout:  5 * time.Second,
			Domain:   "localhost",
		}
		test := NewDNSTest(config)
		registry.AddTest("test"+string(rune(i)), test)
	}

	if registry.Count() != 3 {
		t.Error("should have 3 tests before clear")
	}

	registry.Clear()

	if registry.Count() != 0 {
		t.Error("should have 0 tests after clear")
	}
}

func TestRegistry_RunAllTests(t *testing.T) {
	registry := NewTestRegistry()

	// Add DNS test
	dnsConfig := &TestConfig{
		Name:     "dns-test",
		Type:     "dns",
		Enabled:  true,
		Severity: "high",
		Timeout:  5 * time.Second,
		Domain:   "localhost",
	}
	registry.AddTest("dns-test", NewDNSTest(dnsConfig))

	ctx := context.Background()
	results := registry.RunAllTests(ctx)

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
		return
	}

	if results[0].Name != "dns-test" {
		t.Errorf("expected name dns-test, got %s", results[0].Name)
	}

	if results[0].Type != "dns" {
		t.Errorf("expected type dns, got %s", results[0].Type)
	}
}

func TestRegistry_RunAllTests_Multiple(t *testing.T) {
	registry := NewTestRegistry()

	// Add multiple tests
	for i := 1; i <= 3; i++ {
		config := &TestConfig{
			Name:     "test",
			Type:     "dns",
			Enabled:  true,
			Severity: "high",
			Timeout:  5 * time.Second,
			Domain:   "localhost",
		}
		test := NewDNSTest(config)
		registry.AddTest("test"+string(rune(i)), test)
	}

	ctx := context.Background()
	results := registry.RunAllTests(ctx)

	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

func TestRegistry_ThreadSafety(t *testing.T) {
	registry := NewTestRegistry()

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Concurrently add tests
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			config := &TestConfig{
				Name:     "test",
				Type:     "dns",
				Enabled:  true,
				Severity: "high",
				Timeout:  5 * time.Second,
				Domain:   "localhost",
			}
			test := NewDNSTest(config)
			if err := registry.AddTest("test"+string(rune(idx)), test); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("unexpected error: %v", err)
	}

	if registry.Count() != 50 {
		t.Errorf("expected 50 tests, got %d", registry.Count())
	}
}

func TestRegistry_EmptyRunAllTests(t *testing.T) {
	registry := NewTestRegistry()

	ctx := context.Background()
	results := registry.RunAllTests(ctx)

	if len(results) != 0 {
		t.Errorf("expected 0 results from empty registry, got %d", len(results))
	}
}
