package cache

import (
	"context"
	"log"
	"time"

	"github.com/ArchipelagoAI/health-reporter/pkg/smoke_tests"
)

// SmokeTestCollector runs smoke tests asynchronously at a fixed interval
// and stores results in cache for reports to fetch
type SmokeTestCollector struct {
	testRegistry smoke_tests.TestRegistry
	cache        *EnrichedCache
	interval     time.Duration
	timeout      time.Duration
}

// NewSmokeTestCollector creates a new smoke test collector
func NewSmokeTestCollector(testRegistry smoke_tests.TestRegistry, cache *EnrichedCache) *SmokeTestCollector {
	return &SmokeTestCollector{
		testRegistry: testRegistry,
		cache:        cache,
		interval:     30 * time.Minute,    // Default: 30 minutes
		timeout:      90 * time.Second,    // Default: 90 seconds
	}
}

// Start begins the background smoke test collection loop
func (s *SmokeTestCollector) Start(ctx context.Context) {
	log.Printf("[SmokeTestCollector] Started (interval: %v, timeout: %v)", s.interval, s.timeout)

	// Run initial collection immediately on start
	s.runCollection(ctx)

	// Set up ticker for periodic collections
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.runCollection(ctx)
		case <-ctx.Done():
			log.Printf("[SmokeTestCollector] Stopped")
			return
		}
	}
}

// runCollection executes all tests with independent timeout and stores results
func (s *SmokeTestCollector) runCollection(ctx context.Context) {
	// Create a context with independent timeout for this collection
	collectionCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	// Run all tests
	results := s.testRegistry.RunAllTests(collectionCtx)

	// Store results in cache
	s.cache.UpdateSmokeTestResults(results)

	// Log summary
	passCount := 0
	failCount := 0
	for _, result := range results {
		if result.Status == "pass" {
			passCount++
		} else {
			failCount++
		}
	}
	log.Printf("[SmokeTestCollector] Collection complete: %d passed, %d failed", passCount, failCount)
}
