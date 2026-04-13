package cache

import "time"

// EnrichedFailedPod represents a failed pod with contextual node information
type EnrichedFailedPod struct {
	PodName            string                 `json:"pod_name"`
	Namespace          string                 `json:"namespace"`
	NodeName           string                 `json:"node_name"`
	Phase              string                 `json:"phase"`
	Reason             string                 `json:"reason"`
	LastError          string                 `json:"last_error"`
	Timestamp          time.Time              `json:"timestamp"`
	Errors             []ErrorEntry           `json:"errors"` // All errors for this pod
	NodeMetricsAtTime  NodeMetricsSnapshot    `json:"node_metrics_at_time"`
	ErrorCategory      string                 `json:"error_category"` // crash, timeout, resource, config, unknown
}

// ErrorEntry represents a single error log from a pod
type ErrorEntry struct {
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"` // container name or source
}

// NodeMetricsSnapshot captures node state at a specific time
type NodeMetricsSnapshot struct {
	NodeName           string    `json:"node_name"`
	CPUUsagePercent    float64   `json:"cpu_usage_percent"`
	MemoryUsagePercent float64   `json:"memory_usage_percent"`
	AvailableMemoryGB  float64   `json:"available_memory_gb"`
	Ready              bool      `json:"ready"`
	Unschedulable      bool      `json:"unschedulable"`
	PodCount           int       `json:"pod_count"`
	Timestamp          time.Time `json:"timestamp"`
}

// EnrichedMetrics represents metrics with trend information
type EnrichedMetrics struct {
	Timestamp           time.Time     `json:"timestamp"`
	ClusterMetrics      map[string]interface{} `json:"cluster_metrics"`
	NodeMetrics         []NodeMetricsSnapshot  `json:"node_metrics"`
	CPUTrend            TrendDirection `json:"cpu_trend"`    // up, down, stable
	MemoryTrend         TrendDirection `json:"memory_trend"` // up, down, stable
	PreviousTimestamp   time.Time      `json:"previous_timestamp"`
	PreviousCPU         float64        `json:"previous_cpu"`
	PreviousMemory      float64        `json:"previous_memory"`
}

// TrendDirection indicates metric direction
type TrendDirection string

const (
	TrendUp    TrendDirection = "up"
	TrendDown  TrendDirection = "down"
	TrendStable TrendDirection = "stable"
)

// CacheStats for monitoring cache health
type CacheStats struct {
	FailedPodsCount      int           `json:"failed_pods_count"`
	TotalErrorEntries    int           `json:"total_error_entries"`
	OldestErrorTime      time.Time     `json:"oldest_error_time"`
	NewestErrorTime      time.Time     `json:"newest_error_time"`
	CacheSizeBytes       int64         `json:"cache_size_bytes"`
	LastCollectionTime   time.Time     `json:"last_collection_time"`
	CollectionErrors     int           `json:"collection_errors"`
}
