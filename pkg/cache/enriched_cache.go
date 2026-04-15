package cache

import (
	"log"
	"sort"
	"sync"
	"time"

	"github.com/ArchipelagoAI/health-reporter/pkg/smoke_tests"
)

// EnrichedCache stores enriched metrics and log data with automatic eviction
type EnrichedCache struct {
	mu sync.RWMutex

	// Storage
	failedPods           map[string]*EnrichedFailedPod    // key: namespace/pod-name
	metrics              []EnrichedMetrics                // time-series metrics
	nodeMetrics          map[string][]NodeMetricsSnapshot // key: node-name, time-series
	lastSmokeTestResults []*smoke_tests.TestResult        // cached smoke test results
	lastSmokeTestTime    time.Time                        // timestamp of last smoke test run

	// Configuration
	maxLogEntries  int           // max error entries across all pods
	maxCacheAge    time.Duration // evict data older than this
	maxMemoryBytes int64         // hard limit on cache size (soft enforcement)
	collectionTime time.Time     // last time data was collected

	// Stats
	stats CacheStats
}

// NewEnrichedCache creates a new cache with eviction policies
func NewEnrichedCache(maxLogEntries int, maxCacheAge time.Duration, maxMemoryBytes int64) *EnrichedCache {
	return &EnrichedCache{
		failedPods:     make(map[string]*EnrichedFailedPod),
		metrics:        make([]EnrichedMetrics, 0),
		nodeMetrics:    make(map[string][]NodeMetricsSnapshot),
		maxLogEntries:  maxLogEntries,
		maxCacheAge:    maxCacheAge,
		maxMemoryBytes: maxMemoryBytes,
		stats:          CacheStats{},
	}
}

// UpdateFailedPods replaces the failed pods cache with enriched data
func (c *EnrichedCache) UpdateFailedPods(pods []*EnrichedFailedPod) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.failedPods = make(map[string]*EnrichedFailedPod)
	for _, pod := range pods {
		key := pod.Namespace + "/" + pod.PodName
		c.failedPods[key] = pod
	}

	c.updateStats()
	log.Printf("[Cache] Updated failed pods: %d pods, %d total errors", len(pods), c.stats.TotalErrorEntries)
}

// UpdateMetrics appends new metrics and applies eviction
func (c *EnrichedCache) UpdateMetrics(metrics EnrichedMetrics) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.metrics = append(c.metrics, metrics)
	c.collectionTime = metrics.Timestamp

	// Store node metrics separately
	for _, nm := range metrics.NodeMetrics {
		c.nodeMetrics[nm.NodeName] = append(c.nodeMetrics[nm.NodeName], nm)
	}

	c.evict()
	c.updateStats()
	log.Printf("[Cache] Updated metrics: %d total metrics, %d node metric series", len(c.metrics), len(c.nodeMetrics))
}

// GetFailedPods returns all enriched failed pods
func (c *EnrichedCache) GetFailedPods() []*EnrichedFailedPod {
	c.mu.RLock()
	defer c.mu.RUnlock()

	pods := make([]*EnrichedFailedPod, 0, len(c.failedPods))
	for _, pod := range c.failedPods {
		pods = append(pods, pod)
	}
	return pods
}

// GetLatestMetrics returns the most recent metrics
func (c *EnrichedCache) GetLatestMetrics() *EnrichedMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.metrics) == 0 {
		return nil
	}
	return &c.metrics[len(c.metrics)-1]
}

// GetNodeMetrics returns metrics for a specific node
func (c *EnrichedCache) GetNodeMetrics(nodeName string) []NodeMetricsSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if metrics, ok := c.nodeMetrics[nodeName]; ok {
		return metrics
	}
	return nil
}

// GetMetricsTimeRange returns metrics within a time window
func (c *EnrichedCache) GetMetricsTimeRange(since time.Time) []EnrichedMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []EnrichedMetrics
	for _, m := range c.metrics {
		if m.Timestamp.After(since) {
			result = append(result, m)
		}
	}
	return result
}

// GetStats returns cache statistics
func (c *EnrichedCache) GetStats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stats
}

// evict removes old data based on retention policies
// Must be called with lock held
func (c *EnrichedCache) evict() {
	now := time.Now()
	cutoffTime := now.Add(-c.maxCacheAge)

	// Evict old metrics
	newMetrics := make([]EnrichedMetrics, 0)
	for _, m := range c.metrics {
		if m.Timestamp.After(cutoffTime) {
			newMetrics = append(newMetrics, m)
		}
	}
	c.metrics = newMetrics

	// Evict old node metrics
	for nodeName, nodeMetrics := range c.nodeMetrics {
		newNodeMetrics := make([]NodeMetricsSnapshot, 0)
		for _, nm := range nodeMetrics {
			if nm.Timestamp.After(cutoffTime) {
				newNodeMetrics = append(newNodeMetrics, nm)
			}
		}
		if len(newNodeMetrics) > 0 {
			c.nodeMetrics[nodeName] = newNodeMetrics
		} else {
			delete(c.nodeMetrics, nodeName)
		}
	}

	// Evict old error entries if exceeding maxLogEntries
	totalErrors := 0
	for _, pod := range c.failedPods {
		totalErrors += len(pod.Errors)
	}

	if totalErrors > c.maxLogEntries {
		c.evictOldestErrors(totalErrors - c.maxLogEntries)
	}

	// Evict failed pods if too old
	for key, pod := range c.failedPods {
		if pod.Timestamp.Before(cutoffTime) {
			delete(c.failedPods, key)
		}
	}
}

// evictOldestErrors removes the oldest error entries across all pods
// Must be called with lock held
func (c *EnrichedCache) evictOldestErrors(countToEvict int) {
	// Collect all errors with their pod context
	type errorWithPod struct {
		podKey string
		podIdx int
		errIdx int
		time   time.Time
	}

	var allErrors []errorWithPod
	for podKey, pod := range c.failedPods {
		for errIdx, err := range pod.Errors {
			allErrors = append(allErrors, errorWithPod{
				podKey: podKey,
				podIdx: 0,
				errIdx: errIdx,
				time:   err.Timestamp,
			})
		}
	}

	// Sort by timestamp (oldest first)
	sort.Slice(allErrors, func(i, j int) bool {
		return allErrors[i].time.Before(allErrors[j].time)
	})

	// Remove oldest entries
	removed := 0
	for _, errInfo := range allErrors {
		if removed >= countToEvict {
			break
		}
		pod := c.failedPods[errInfo.podKey]
		if errInfo.errIdx < len(pod.Errors) {
			// Mark for removal by swapping with last and shrinking
			pod.Errors = append(pod.Errors[:errInfo.errIdx], pod.Errors[errInfo.errIdx+1:]...)
			removed++
		}
	}

	log.Printf("[Cache] Evicted %d oldest errors (exceeded limit of %d)", removed, c.maxLogEntries)
}

// updateStats recalculates cache statistics
// Must be called with lock held
func (c *EnrichedCache) updateStats() {
	c.stats.FailedPodsCount = len(c.failedPods)
	c.stats.TotalErrorEntries = 0
	c.stats.OldestErrorTime = time.Now()
	c.stats.NewestErrorTime = time.Time{}

	for _, pod := range c.failedPods {
		c.stats.TotalErrorEntries += len(pod.Errors)
		for _, err := range pod.Errors {
			if err.Timestamp.Before(c.stats.OldestErrorTime) {
				c.stats.OldestErrorTime = err.Timestamp
			}
			if err.Timestamp.After(c.stats.NewestErrorTime) {
				c.stats.NewestErrorTime = err.Timestamp
			}
		}
	}

	c.stats.LastCollectionTime = c.collectionTime
	c.stats.CacheSizeBytes = c.estimateSize()
}

// estimateSize returns approximate cache size in bytes
// Must be called with lock held
func (c *EnrichedCache) estimateSize() int64 {
	size := int64(0)

	// Rough estimate: ~1KB per failed pod + 500B per error entry
	size += int64(len(c.failedPods) * 1024)
	for _, pod := range c.failedPods {
		size += int64(len(pod.Errors) * 500)
	}

	// Metrics: ~500B per metric entry
	size += int64(len(c.metrics) * 500)

	// Node metrics: ~300B per entry
	for _, nodeMetrics := range c.nodeMetrics {
		size += int64(len(nodeMetrics) * 300)
	}

	return size
}

// UpdateSmokeTestResults stores cached smoke test results with timestamp
func (c *EnrichedCache) UpdateSmokeTestResults(results []*smoke_tests.TestResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.lastSmokeTestResults = results
	c.lastSmokeTestTime = time.Now()

	log.Printf("[Cache] Updated smoke test results: %d tests", len(results))
}

// GetLatestSmokeTestResults returns the most recent cached smoke test results
func (c *EnrichedCache) GetLatestSmokeTestResults() []*smoke_tests.TestResult {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.lastSmokeTestResults
}

// Clear empties the cache
func (c *EnrichedCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.failedPods = make(map[string]*EnrichedFailedPod)
	c.metrics = make([]EnrichedMetrics, 0)
	c.nodeMetrics = make(map[string][]NodeMetricsSnapshot)
	c.lastSmokeTestResults = nil
	c.lastSmokeTestTime = time.Time{}
	c.updateStats()
	log.Printf("[Cache] Cleared")
}
