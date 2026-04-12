package smoke_tests

import (
	"context"
	"fmt"
	"net"
	"time"
)

// DNSTest implements TestRunner for DNS resolution tests
type DNSTest struct {
	config *TestConfig
}

// NewDNSTest creates a new DNS test
func NewDNSTest(config *TestConfig) *DNSTest {
	return &DNSTest{config: config}
}

// Run executes the DNS resolution test
func (d *DNSTest) Run(ctx context.Context) (*TestResult, error) {
	start := time.Now()
	result := &TestResult{
		Name:      d.config.Name,
		Type:      d.config.Type,
		Timestamp: start,
		Severity:  d.config.Severity,
	}

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(ctx, d.config.Timeout)
	defer cancel()

	// Perform DNS resolution
	resolver := &net.Resolver{}
	ips, err := resolver.LookupIPAddr(ctx, d.config.Domain)

	duration := time.Since(start)
	result.Duration = duration

	if err != nil {
		result.Status = "fail"
		result.Message = fmt.Sprintf("DNS lookup failed for %s: %v", d.config.Domain, err)
		return result, nil
	}

	if len(ips) == 0 {
		result.Status = "fail"
		result.Message = fmt.Sprintf("DNS lookup returned no results for %s", d.config.Domain)
		return result, nil
	}

	result.Status = "pass"
	result.Message = fmt.Sprintf("Successfully resolved %s to %d address(es)", d.config.Domain, len(ips))

	// Log first resolved IP for debugging
	if len(ips) > 0 {
		result.Message = fmt.Sprintf("Successfully resolved %s to %s", d.config.Domain, ips[0].String())
	}

	return result, nil
}

// GetConfig returns the test configuration
func (d *DNSTest) GetConfig() *TestConfig {
	return d.config
}

// GetName returns the test name
func (d *DNSTest) GetName() string {
	return d.config.Name
}

// GetType returns the test type
func (d *DNSTest) GetType() string {
	return "dns"
}
