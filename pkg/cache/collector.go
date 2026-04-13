package cache

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/ArchipelagoAI/health-reporter/pkg/loki"
	"github.com/ArchipelagoAI/health-reporter/pkg/mimir"
)

// CacheCollector continuously collects and enriches data from Mimir and Loki
type CacheCollector struct {
	cache       *EnrichedCache
	mimirClient *mimir.Client
	lokiClient  *loki.Client
	interval    time.Duration
	done        chan struct{}
	running     bool
}

// NewCacheCollector creates a new background collector
func NewCacheCollector(
	cache *EnrichedCache,
	mimirClient *mimir.Client,
	lokiClient *loki.Client,
	intervalSeconds int,
) *CacheCollector {
	return &CacheCollector{
		cache:       cache,
		mimirClient: mimirClient,
		lokiClient:  lokiClient,
		interval:    time.Duration(intervalSeconds) * time.Second,
		done:        make(chan struct{}),
		running:     false,
	}
}

// Start begins the background collection process
func (cc *CacheCollector) Start(ctx context.Context) {
	if cc.running {
		log.Printf("[Collector] Already running")
		return
	}

	cc.running = true
	go cc.runCollection(ctx)
	log.Printf("[Collector] Started with interval: %v", cc.interval)
}

// Stop halts the background collection
func (cc *CacheCollector) Stop() {
	if !cc.running {
		return
	}
	cc.running = false
	select {
	case cc.done <- struct{}{}:
	default:
	}
	log.Printf("[Collector] Stopped")
}

// runCollection is the main collection loop
func (cc *CacheCollector) runCollection(ctx context.Context) {
	ticker := time.NewTicker(cc.interval)
	defer ticker.Stop()

	// Initial collection
	cc.collectOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			cc.running = false
			return
		case <-cc.done:
			return
		case <-ticker.C:
			cc.collectOnce(ctx)
		}
	}
}

// collectOnce performs a single collection cycle
func (cc *CacheCollector) collectOnce(ctx context.Context) {
	// Set timeout for collection
	collectionCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	log.Printf("[Collector] Starting collection cycle...")

	// Collect metrics
	metrics, err := cc.collectMetrics(collectionCtx)
	if err != nil {
		log.Printf("[Collector] Failed to collect metrics: %v", err)
		cc.cache.stats.CollectionErrors++
	} else if metrics != nil {
		cc.cache.UpdateMetrics(*metrics)
	}

	// Collect enriched failed pods
	failedPods, err := cc.collectEnrichedFailedPods(collectionCtx, metrics)
	if err != nil {
		log.Printf("[Collector] Failed to collect enriched failed pods: %v", err)
		cc.cache.stats.CollectionErrors++
	} else if len(failedPods) > 0 {
		cc.cache.UpdateFailedPods(failedPods)
	}

	log.Printf("[Collector] Collection cycle complete")
}

// collectMetrics gathers metrics from Mimir and enriches with trends
func (cc *CacheCollector) collectMetrics(ctx context.Context) (*EnrichedMetrics, error) {
	metrics, err := cc.mimirClient.GetMetrics(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get metrics from mimir: %w", err)
	}

	enriched := &EnrichedMetrics{
		Timestamp:      time.Now().UTC(),
		ClusterMetrics: make(map[string]interface{}),
		NodeMetrics:    make([]NodeMetricsSnapshot, 0),
	}

	// Copy cluster metrics
	enriched.ClusterMetrics = metrics.ToMap()

	// Convert per-node metrics
	for _, detail := range metrics.NodeDetails {
		enriched.NodeMetrics = append(enriched.NodeMetrics, NodeMetricsSnapshot{
			NodeName:           detail.Name,
			CPUUsagePercent:    detail.CPUUsagePercent,
			MemoryUsagePercent: detail.MemoryUsagePercent,
			AvailableMemoryGB:  detail.AvailableMemoryGB,
			Ready:              detail.Ready,
			Unschedulable:      detail.Unschedulable,
			PodCount:           detail.PodCount,
			Timestamp:          time.Now().UTC(),
		})
	}

	// Calculate trends from previous metrics
	latestMetrics := cc.cache.GetLatestMetrics()
	if latestMetrics != nil {
		enriched.PreviousTimestamp = latestMetrics.Timestamp
		enriched.PreviousCPU = metrics.Resources.CPUUsagePercent
		enriched.PreviousMemory = metrics.Resources.MemoryUsagePercent

		// Determine trends
		cpuDiff := metrics.Resources.CPUUsagePercent - latestMetrics.PreviousCPU
		if cpuDiff > 2.0 {
			enriched.CPUTrend = TrendUp
		} else if cpuDiff < -2.0 {
			enriched.CPUTrend = TrendDown
		} else {
			enriched.CPUTrend = TrendStable
		}

		memDiff := metrics.Resources.MemoryUsagePercent - latestMetrics.PreviousMemory
		if memDiff > 2.0 {
			enriched.MemoryTrend = TrendUp
		} else if memDiff < -2.0 {
			enriched.MemoryTrend = TrendDown
		} else {
			enriched.MemoryTrend = TrendStable
		}
	}

	return enriched, nil
}

// collectEnrichedFailedPods gathers failed pod data enriched with node info and logs
func (cc *CacheCollector) collectEnrichedFailedPods(ctx context.Context, metrics *EnrichedMetrics) ([]*EnrichedFailedPod, error) {
	if cc.lokiClient == nil {
		return nil, nil
	}

	failedPodErrors, err := cc.lokiClient.GetFailedPodsErrors(ctx)
	if err != nil || len(failedPodErrors) == 0 {
		return nil, err
	}

	// Create map of node metrics for quick lookup
	nodeMetricsMap := make(map[string]NodeMetricsSnapshot)
	if metrics != nil {
		for _, nm := range metrics.NodeMetrics {
			nodeMetricsMap[nm.NodeName] = nm
		}
	}

	var enrichedPods []*EnrichedFailedPod

	for podKey, errors := range failedPodErrors {
		pod := &EnrichedFailedPod{
			PodName:   podKey,
			Timestamp: time.Now().UTC(),
			Errors:    make([]ErrorEntry, 0),
		}

		// Parse namespace/pod from key
		parts := strings.Split(podKey, "/")
		if len(parts) == 2 {
			pod.Namespace = parts[0]
			pod.PodName = parts[1]
		}

		// Convert error strings to ErrorEntry
		for _, errStr := range errors {
			pod.Errors = append(pod.Errors, ErrorEntry{
				Message:   errStr,
				Timestamp: time.Now().UTC(),
			})
		}

		// Classify errors
		pod.ErrorCategory = classifyErrors(pod.Errors)

		// Enrich with node metrics (would need to fetch pod-to-node mapping in real scenario)
		// For now, we'll leave this for the health report generation to fill in
		// This could be enhanced to query pod details from Kubernetes

		enrichedPods = append(enrichedPods, pod)
	}

	return enrichedPods, nil
}

// classifyErrors categorizes errors based on their messages
func classifyErrors(errors []ErrorEntry) string {
	if len(errors) == 0 {
		return "unknown"
	}

	categories := make(map[string]int)

	for _, err := range errors {
		msg := strings.ToLower(err.Message)

		if isResourceError(msg) {
			categories["resource"]++
		} else if isCrashError(msg) {
			categories["crash"]++
		} else if isTimeoutError(msg) {
			categories["timeout"]++
		} else if isConfigError(msg) {
			categories["config"]++
		} else {
			categories["unknown"]++
		}
	}

	// Return the most common category
	maxCount := 0
	maxCategory := "unknown"
	for category, count := range categories {
		if count > maxCount {
			maxCount = count
			maxCategory = category
		}
	}

	return maxCategory
}

// isResourceError checks if error is resource-related
func isResourceError(msg string) bool {
	patterns := []string{
		"oom", "out of memory", "memory", "OOMKilled",
		"cpu limit", "cpu throttle",
		"disk", "storage", "space",
	}
	for _, p := range patterns {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

// isCrashError checks if error indicates a crash
func isCrashError(msg string) bool {
	patterns := []string{
		"crash", "panic", "exit", "segfault", "signal",
		"killed", "fatal", "error", "exception",
		"CrashLoop",
	}
	for _, p := range patterns {
		if matched, _ := regexp.MatchString("(?i)"+p, msg); matched {
			return true
		}
	}
	return false
}

// isTimeoutError checks if error is timeout-related
func isTimeoutError(msg string) bool {
	patterns := []string{
		"timeout", "deadline", "timed out", "time out",
		"connection refused", "connection timeout",
	}
	for _, p := range patterns {
		if strings.Contains(strings.ToLower(msg), p) {
			return true
		}
	}
	return false
}

// isConfigError checks if error is configuration-related
func isConfigError(msg string) bool {
	patterns := []string{
		"config", "configuration", "invalid", "bad argument",
		"environment", "variable", "port", "address",
	}
	for _, p := range patterns {
		if strings.Contains(strings.ToLower(msg), p) {
			return true
		}
	}
	return false
}
