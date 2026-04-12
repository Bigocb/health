package types

import (
	"encoding/json"
	"time"
)

// HealthStatus represents cluster health level
type HealthStatus string

const (
	StatusHealthy  HealthStatus = "healthy"
	StatusDegraded HealthStatus = "degraded"
	StatusCritical HealthStatus = "critical"
)

// NodeMetrics represents per-node resource metrics
type NodeMetrics struct {
	Name              string  `json:"name"`
	Ready             bool    `json:"ready"`
	Unschedulable     bool    `json:"unschedulable"`
	CPUUsagePercent   float64 `json:"cpu_usage_percent"`
	MemoryUsagePercent float64 `json:"memory_usage_percent"`
	AvailableMemoryGB float64 `json:"available_memory_gb"`
	PodCount          int     `json:"pod_count"`
}

// FailedPod represents a failed pod with its details
type FailedPod struct {
	Namespace    string `json:"namespace"`
	Name         string `json:"name"`
	Phase        string `json:"phase"`
	Reason       string `json:"reason"`
	LastError    string `json:"last_error"`
	RestartCount int    `json:"restart_count"`
}

// Report represents a health report
type Report struct {
	Timestamp       time.Time              `json:"timestamp"`
	Status          HealthStatus           `json:"status"`
	Summary         string                 `json:"summary"`
	ClusterMetrics  map[string]interface{} `json:"cluster_metrics"`
	Concerns        []Concern              `json:"concerns,omitempty"`
	Recommendations []string               `json:"recommendations,omitempty"`
	SmokeTests      []SmokeTestResult      `json:"smoke_tests,omitempty"`
	Analysis        map[string]interface{} `json:"analysis,omitempty"`
	NodeMetrics     []NodeMetrics          `json:"node_metrics,omitempty"`
	FailedPods      []FailedPod            `json:"failed_pods,omitempty"`
}

// SmokeTestResult represents the result of a smoke test
type SmokeTestResult struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Status   string `json:"status"` // pass, fail, timeout
	Message  string `json:"message"`
	Duration int    `json:"duration_ms"`
	Severity string `json:"severity"` // critical, high, medium, low
}

// Concern represents an identified issue
type Concern struct {
	Title    string `json:"title"`
	Severity string `json:"severity"` // "low", "medium", "high"
	Details  string `json:"details"`
}

// Recommendation represents an actionable recommendation
type Recommendation struct {
	Priority  string   `json:"priority"`
	Category  string   `json:"category"`
	Action    string   `json:"action"`
	Rationale string   `json:"rationale"`
	Steps     []string `json:"steps"`
}

// ToJSON serializes report to JSON
func (r *Report) ToJSON() (string, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
