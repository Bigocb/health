package health

import (
	"testing"

	"github.com/ArchipelagoAI/health-reporter/pkg/mimir"
)

func TestCalculateStatus_Healthy(t *testing.T) {
	reporter := &Reporter{}
	metrics := &mimir.Metrics{
		Nodes: mimir.NodeMetrics{
			Total:    3,
			Ready:    3,
			NotReady: 0,
		},
		Pods: mimir.PodMetrics{
			Total:    100,
			Running:  98,
			Pending:  1,
			Failed:   0,
			Restarts: 2,
		},
		Resources: mimir.ResourceMetrics{
			CPUUsagePercent:    45.5,
			MemoryUsagePercent: 62.3,
		},
	}

	status := reporter.calculateStatus(metrics)
	if status != StatusHealthy {
		t.Errorf("expected healthy, got %s", status)
	}
}

func TestCalculateStatus_Degraded_HighCPU(t *testing.T) {
	reporter := &Reporter{}
	metrics := &mimir.Metrics{
		Nodes: mimir.NodeMetrics{
			Total:    3,
			Ready:    3,
			NotReady: 0,
		},
		Pods: mimir.PodMetrics{
			Total:   100,
			Running: 100,
			Failed:  0,
		},
		Resources: mimir.ResourceMetrics{
			CPUUsagePercent:    85.0,
			MemoryUsagePercent: 60.0,
		},
	}

	status := reporter.calculateStatus(metrics)
	if status != StatusDegraded {
		t.Errorf("expected degraded, got %s", status)
	}
}

func TestCalculateStatus_Degraded_HighRestarts(t *testing.T) {
	reporter := &Reporter{}
	metrics := &mimir.Metrics{
		Nodes: mimir.NodeMetrics{
			Total:    3,
			Ready:    3,
			NotReady: 0,
		},
		Pods: mimir.PodMetrics{
			Total:    100,
			Running:  95,
			Pending:  0,
			Failed:   0,
			Restarts: 8,
		},
		Resources: mimir.ResourceMetrics{
			CPUUsagePercent:    50.0,
			MemoryUsagePercent: 60.0,
		},
	}

	status := reporter.calculateStatus(metrics)
	if status != StatusDegraded {
		t.Errorf("expected degraded, got %s", status)
	}
}

func TestCalculateStatus_Critical_FailedPods(t *testing.T) {
	reporter := &Reporter{}
	metrics := &mimir.Metrics{
		Nodes: mimir.NodeMetrics{
			Total:    3,
			Ready:    3,
			NotReady: 0,
		},
		Pods: mimir.PodMetrics{
			Total:    100,
			Running:  95,
			Pending:  0,
			Failed:   5,
			Restarts: 2,
		},
		Resources: mimir.ResourceMetrics{
			CPUUsagePercent:    50.0,
			MemoryUsagePercent: 60.0,
		},
	}

	status := reporter.calculateStatus(metrics)
	if status != StatusCritical {
		t.Errorf("expected critical, got %s", status)
	}
}

func TestCalculateStatus_Critical_NodeDown(t *testing.T) {
	reporter := &Reporter{}
	metrics := &mimir.Metrics{
		Nodes: mimir.NodeMetrics{
			Total:    3,
			Ready:    2,
			NotReady: 1,
		},
		Pods: mimir.PodMetrics{
			Total:    100,
			Running:  98,
			Pending:  0,
			Failed:   0,
			Restarts: 1,
		},
		Resources: mimir.ResourceMetrics{
			CPUUsagePercent:    50.0,
			MemoryUsagePercent: 60.0,
		},
	}

	status := reporter.calculateStatus(metrics)
	if status != StatusCritical {
		t.Errorf("expected critical, got %s", status)
	}
}

func TestCalculateStatus_Critical_HighMemory(t *testing.T) {
	reporter := &Reporter{}
	metrics := &mimir.Metrics{
		Nodes: mimir.NodeMetrics{
			Total:    3,
			Ready:    3,
			NotReady: 0,
		},
		Pods: mimir.PodMetrics{
			Total:   100,
			Running: 100,
			Failed:  0,
		},
		Resources: mimir.ResourceMetrics{
			CPUUsagePercent:    50.0,
			MemoryUsagePercent: 92.0,
		},
	}

	status := reporter.calculateStatus(metrics)
	if status != StatusCritical {
		t.Errorf("expected critical, got %s", status)
	}
}

func TestIdentifyConcerns_NoConcerns(t *testing.T) {
	reporter := &Reporter{}
	metrics := &mimir.Metrics{
		Nodes: mimir.NodeMetrics{
			Total:    3,
			Ready:    3,
			NotReady: 0,
		},
		Pods: mimir.PodMetrics{
			Total:    100,
			Running:  100,
			Pending:  0,
			Failed:   0,
			Restarts: 1,
		},
		Resources: mimir.ResourceMetrics{
			CPUUsagePercent:    40.0,
			MemoryUsagePercent: 50.0,
		},
	}

	concerns := reporter.identifyConcerns(metrics)
	if len(concerns) != 0 {
		t.Errorf("expected no concerns, got %d", len(concerns))
	}
}

func TestIdentifyConcerns_Multiple(t *testing.T) {
	reporter := &Reporter{}
	metrics := &mimir.Metrics{
		Nodes: mimir.NodeMetrics{
			Total:    3,
			Ready:    2,
			NotReady: 1,
		},
		Pods: mimir.PodMetrics{
			Total:    100,
			Running:  90,
			Pending:  5,
			Failed:   2,
			Restarts: 12,
		},
		Resources: mimir.ResourceMetrics{
			CPUUsagePercent:    88.0,
			MemoryUsagePercent: 92.0,
		},
	}

	concerns := reporter.identifyConcerns(metrics)
	if len(concerns) == 0 {
		t.Errorf("expected concerns, got none")
	}

	// Verify high-severity concerns
	highSeverity := 0
	for _, concern := range concerns {
		if concern.Severity == "high" {
			highSeverity++
		}
	}

	if highSeverity < 2 {
		t.Errorf("expected at least 2 high-severity concerns, got %d", highSeverity)
	}
}

func TestGenerateSummary_Healthy(t *testing.T) {
	reporter := &Reporter{}
	report := &Report{
		Status: StatusHealthy,
		ClusterMetrics: mimir.Metrics{
			Nodes: mimir.NodeMetrics{
				Total: 3,
				Ready: 3,
			},
			Pods: mimir.PodMetrics{
				Running: 98,
			},
			Resources: mimir.ResourceMetrics{
				CPUUsagePercent:    45.0,
				MemoryUsagePercent: 62.0,
			},
		},
	}

	summary := reporter.generateSummary(report)
	if summary == "" {
		t.Errorf("expected non-empty summary")
	}

	if !contains(summary, "HEALTHY") {
		t.Errorf("expected 'HEALTHY' in summary, got: %s", summary)
	}
}

func TestGenerateSummary_Degraded(t *testing.T) {
	reporter := &Reporter{}
	report := &Report{
		Status: StatusDegraded,
		ClusterMetrics: mimir.Metrics{
			Nodes: mimir.NodeMetrics{
				Total: 3,
				Ready: 3,
			},
			Pods: mimir.PodMetrics{
				Running:  95,
				Pending:  3,
				Restarts: 8,
			},
			Resources: mimir.ResourceMetrics{
				CPUUsagePercent:    85.0,
				MemoryUsagePercent: 80.0,
			},
		},
	}

	summary := reporter.generateSummary(report)
	if summary == "" {
		t.Errorf("expected non-empty summary")
	}

	if !contains(summary, "DEGRADED") {
		t.Errorf("expected 'DEGRADED' in summary, got: %s", summary)
	}
}

func TestGenerateRecommendations_Critical(t *testing.T) {
	reporter := &Reporter{}
	report := &Report{
		Status:   StatusCritical,
		Concerns: []Concern{{Title: "Test", Severity: "high"}},
	}

	recs := reporter.generateRecommendations(report)
	if len(recs) == 0 {
		t.Errorf("expected recommendations for critical status")
	}

	hasDescribeNodes := false
	for _, rec := range recs {
		if contains(rec, "kubectl describe nodes") {
			hasDescribeNodes = true
			break
		}
	}

	if !hasDescribeNodes {
		t.Errorf("expected 'kubectl describe nodes' recommendation")
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && contains(s[1:], substr) || contains(s[:len(s)-1], substr))
}
