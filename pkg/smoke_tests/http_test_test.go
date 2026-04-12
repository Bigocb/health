package smoke_tests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPTest_Success(t *testing.T) {
	// Create a test HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer server.Close()

	config := &TestConfig{
		Name:           "test-http-success",
		Type:           "http",
		Enabled:        true,
		Severity:       "high",
		Timeout:        5 * time.Second,
		URL:            server.URL,
		Method:         "GET",
		ExpectedStatus: 200,
		TLSInsecure:    false,
	}

	test := NewHTTPTest(config)
	ctx := context.Background()
	result, err := test.Run(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "pass" {
		t.Errorf("expected status pass, got %s: %s", result.Status, result.Message)
	}
}

func TestHTTPTest_BadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	config := &TestConfig{
		Name:           "test-http-bad-status",
		Type:           "http",
		Enabled:        true,
		Severity:       "high",
		Timeout:        5 * time.Second,
		URL:            server.URL,
		Method:         "GET",
		ExpectedStatus: 200,
		TLSInsecure:    false,
	}

	test := NewHTTPTest(config)
	ctx := context.Background()
	result, err := test.Run(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "fail" {
		t.Errorf("expected status fail, got %s", result.Status)
	}
}

func TestHTTPTest_InvalidURL(t *testing.T) {
	config := &TestConfig{
		Name:           "test-http-invalid",
		Type:           "http",
		Enabled:        true,
		Severity:       "high",
		Timeout:        5 * time.Second,
		URL:            "http://invalid-domain-that-does-not-exist-12345.local",
		Method:         "GET",
		ExpectedStatus: 200,
		TLSInsecure:    false,
	}

	test := NewHTTPTest(config)
	ctx := context.Background()
	result, err := test.Run(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "fail" {
		t.Errorf("expected status fail, got %s", result.Status)
	}
}

func TestHTTPTest_CustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom-Header") != "test-value" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &TestConfig{
		Name:           "test-http-headers",
		Type:           "http",
		Enabled:        true,
		Severity:       "high",
		Timeout:        5 * time.Second,
		URL:            server.URL,
		Method:         "GET",
		ExpectedStatus: 200,
		TLSInsecure:    false,
		Headers: map[string]string{
			"X-Custom-Header": "test-value",
		},
	}

	test := NewHTTPTest(config)
	ctx := context.Background()
	result, err := test.Run(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "pass" {
		t.Errorf("expected status pass, got %s: %s", result.Status, result.Message)
	}
}

func TestHTTPTest_DefaultMethod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &TestConfig{
		Name:           "test-http-default-method",
		Type:           "http",
		Enabled:        true,
		Severity:       "high",
		Timeout:        5 * time.Second,
		URL:            server.URL,
		Method:         "",
		ExpectedStatus: 200,
		TLSInsecure:    false,
	}

	test := NewHTTPTest(config)
	ctx := context.Background()
	result, err := test.Run(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "pass" {
		t.Errorf("expected status pass, got %s: %s", result.Status, result.Message)
	}
}

func TestHTTPTest_DefaultExpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &TestConfig{
		Name:           "test-http-default-status",
		Type:           "http",
		Enabled:        true,
		Severity:       "high",
		Timeout:        5 * time.Second,
		URL:            server.URL,
		Method:         "GET",
		ExpectedStatus: 0, // Should default to 200
		TLSInsecure:    false,
	}

	test := NewHTTPTest(config)
	ctx := context.Background()
	result, err := test.Run(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "pass" {
		t.Errorf("expected status pass, got %s: %s", result.Status, result.Message)
	}
}
