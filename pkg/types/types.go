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

// Report represents a health report
type Report struct {
	Timestamp       time.Time              `json:"timestamp"`
	Status          HealthStatus           `json:"status"`
	Summary         string                 `json:"summary"`
	ClusterMetrics  map[string]interface{} `json:"cluster_metrics"`
	Concerns        []Concern              `json:"concerns,omitempty"`
	Recommendations []string               `json:"recommendations,omitempty"`
}

// Concern represents an identified issue
type Concern struct {
	Title    string `json:"title"`
	Severity string `json:"severity"` // "low", "medium", "high"
	Details  string `json:"details"`
}

// ToJSON serializes report to JSON
func (r *Report) ToJSON() (string, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
