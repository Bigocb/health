package smoke_tests

import (
	"context"
	"sync"
)

// SharedTestRegistry implements TestRegistry with thread-safe operations
type SharedTestRegistry struct {
	mu    sync.RWMutex
	tests map[string]TestRunner
}

// NewTestRegistry creates a new test registry
func NewTestRegistry() *SharedTestRegistry {
	return &SharedTestRegistry{
		tests: make(map[string]TestRunner),
	}
}

// AddTest adds or updates a test in the registry
func (r *SharedTestRegistry) AddTest(name string, runner TestRunner) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tests[name] = runner
	return nil
}

// RemoveTest removes a test from the registry
func (r *SharedTestRegistry) RemoveTest(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tests, name)
	return nil
}

// GetTest retrieves a specific test
func (r *SharedTestRegistry) GetTest(name string) (TestRunner, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	runner, exists := r.tests[name]
	return runner, exists
}

// ListTests returns all active tests
func (r *SharedTestRegistry) ListTests() []TestRunner {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []TestRunner
	for _, runner := range r.tests {
		result = append(result, runner)
	}
	return result
}

// RunAllTests executes all enabled tests in parallel
func (r *SharedTestRegistry) RunAllTests(ctx context.Context) []*TestResult {
	tests := r.ListTests()

	if len(tests) == 0 {
		return []*TestResult{}
	}

	// Run all tests concurrently
	results := make([]*TestResult, len(tests))
	var wg sync.WaitGroup

	for i, test := range tests {
		wg.Add(1)
		go func(idx int, t TestRunner) {
			defer wg.Done()
			result, err := t.Run(ctx)
			if err != nil {
				// If error occurs, create a failed result
				results[idx] = &TestResult{
					Name:    t.GetName(),
					Type:    t.GetType(),
					Status:  "fail",
					Message: err.Error(),
				}
				return
			}
			results[idx] = result
		}(i, test)
	}

	wg.Wait()
	return results
}

// Clear removes all tests from the registry
func (r *SharedTestRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tests = make(map[string]TestRunner)
}

// Count returns the number of tests in the registry
func (r *SharedTestRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tests)
}
