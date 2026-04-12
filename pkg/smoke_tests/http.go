package smoke_tests

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// HTTPTest implements TestRunner for HTTP health check tests
type HTTPTest struct {
	config *TestConfig
	client *http.Client
}

// NewHTTPTest creates a new HTTP test
func NewHTTPTest(config *TestConfig) *HTTPTest {
	// Create HTTP client with custom TLS configuration if needed
	tlsConfig := &tls.Config{
		InsecureSkipVerify: config.TLSInsecure,
	}

	client := &http.Client{
		Timeout: config.Timeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	return &HTTPTest{
		config: config,
		client: client,
	}
}

// Run executes the HTTP health check test
func (h *HTTPTest) Run(ctx context.Context) (*TestResult, error) {
	start := time.Now()
	result := &TestResult{
		Name:      h.config.Name,
		Type:      h.config.Type,
		Timestamp: start,
		Severity:  h.config.Severity,
	}

	// Default to GET if method not specified
	method := h.config.Method
	if method == "" {
		method = "GET"
	}

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, method, h.config.URL, nil)
	if err != nil {
		result.Status = "fail"
		result.Message = fmt.Sprintf("Failed to create HTTP request: %v", err)
		result.Duration = time.Since(start)
		return result, nil
	}

	// Add custom headers if provided
	if h.config.Headers != nil {
		for key, value := range h.config.Headers {
			req.Header.Set(key, value)
		}
	}

	// Add service account token if configured
	if h.config.UseServiceAccountToken {
		token, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+string(token))
		}
	}

	// Execute the request
	resp, err := h.client.Do(req)
	duration := time.Since(start)
	result.Duration = duration

	if err != nil {
		result.Status = "fail"
		result.Message = fmt.Sprintf("HTTP request failed: %v", err)
		return result, nil
	}

	defer resp.Body.Close()

	// Read response body (limited to 1MB to avoid memory issues)
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
	if err != nil {
		result.Status = "fail"
		result.Message = fmt.Sprintf("Failed to read response body: %v", err)
		return result, nil
	}

	// Check status code
	expectedStatus := h.config.ExpectedStatus
	if expectedStatus == 0 {
		// Default to 200 OK if not specified
		expectedStatus = 200
	}

	if resp.StatusCode != expectedStatus {
		result.Status = "fail"
		result.Message = fmt.Sprintf("HTTP status code mismatch: expected %d, got %d", expectedStatus, resp.StatusCode)
		return result, nil
	}

	result.Status = "pass"
	result.Message = fmt.Sprintf("HTTP %s %s returned %d (expected %d) in %dms", method, h.config.URL, resp.StatusCode, expectedStatus, resp.ContentLength)

	// Log response size for debugging
	result.Message = fmt.Sprintf("HTTP %s returned %d with %d bytes", method, resp.StatusCode, len(body))

	return result, nil
}

// GetConfig returns the test configuration
func (h *HTTPTest) GetConfig() *TestConfig {
	return h.config
}

// GetName returns the test name
func (h *HTTPTest) GetName() string {
	return h.config.Name
}

// GetType returns the test type
func (h *HTTPTest) GetType() string {
	return "http"
}
