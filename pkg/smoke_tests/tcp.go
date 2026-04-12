package smoke_tests

import (
	"context"
	"fmt"
	"net"
	"time"
)

// TCPTest implements TestRunner for TCP connectivity tests
type TCPTest struct {
	config *TestConfig
}

// NewTCPTest creates a new TCP test
func NewTCPTest(config *TestConfig) *TCPTest {
	return &TCPTest{config: config}
}

// Run executes the TCP connectivity test
func (t *TCPTest) Run(ctx context.Context) (*TestResult, error) {
	start := time.Now()
	result := &TestResult{
		Name:      t.config.Name,
		Type:      t.config.Type,
		Timestamp: start,
		Severity:  t.config.Severity,
	}

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(ctx, t.config.Timeout)
	defer cancel()

	// Format the address
	address := fmt.Sprintf("%s:%d", t.config.Host, t.config.Port)

	// Attempt TCP connection
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", address)

	duration := time.Since(start)
	result.Duration = duration

	if err != nil {
		result.Status = "fail"
		result.Message = fmt.Sprintf("TCP connection to %s failed: %v", address, err)
		return result, nil
	}

	// Close connection
	conn.Close()

	result.Status = "pass"
	result.Message = fmt.Sprintf("Successfully connected to TCP %s", address)

	return result, nil
}

// GetConfig returns the test configuration
func (t *TCPTest) GetConfig() *TestConfig {
	return t.config
}

// GetName returns the test name
func (t *TCPTest) GetName() string {
	return t.config.Name
}

// GetType returns the test type
func (t *TCPTest) GetType() string {
	return "tcp"
}
