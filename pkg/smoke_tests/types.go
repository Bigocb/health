package smoke_tests

import (
	"context"
	"time"
)

// TestResult represents the outcome of a single test execution
type TestResult struct {
	Name      string        `json:"name"`
	Type      string        `json:"type"`   // dns, http, tcp
	Status    string        `json:"status"` // pass, fail, timeout
	Message   string        `json:"message"`
	Duration  time.Duration `json:"duration"`
	Timestamp time.Time     `json:"timestamp"`
	Severity  string        `json:"severity"` // critical, high, medium, low
}

// TestConfig defines a test to be executed
type TestConfig struct {
	Name                   string
	Type                   string // dns, http, tcp
	Enabled                bool
	Severity               string // critical, high, medium, low
	Timeout                time.Duration
	Domain                 string            // DNS
	URL                    string            // HTTP
	Method                 string            // HTTP (default: GET)
	ExpectedStatus         int               // HTTP
	TLSInsecure            bool              // HTTP
	Headers                map[string]string // HTTP
	UseServiceAccountToken bool              // HTTP (use pod's service account token)
	Host                   string            // TCP
	Port                   int               // TCP
}

// TestRunner defines the interface for all test types
type TestRunner interface {
	// Run executes the test and returns a TestResult
	Run(ctx context.Context) (*TestResult, error)

	// GetConfig returns the test configuration
	GetConfig() *TestConfig

	// GetName returns the test name
	GetName() string

	// GetType returns the test type (dns, http, tcp)
	GetType() string
}

// TestRegistry manages the collection of active tests
type TestRegistry interface {
	// AddTest adds or updates a test in the registry
	AddTest(name string, runner TestRunner) error

	// RemoveTest removes a test from the registry
	RemoveTest(name string) error

	// GetTest retrieves a specific test
	GetTest(name string) (TestRunner, bool)

	// ListTests returns all active tests
	ListTests() []TestRunner

	// RunAllTests executes all enabled tests in parallel
	RunAllTests(ctx context.Context) []*TestResult
}
