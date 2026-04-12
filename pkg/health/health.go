package health

import (
	"context"
	"fmt"
	"time"

	"github.com/ArchipelagoAI/health-reporter/pkg/mimir"
	"github.com/ArchipelagoAI/health-reporter/pkg/smoke_tests"
	"github.com/ArchipelagoAI/health-reporter/pkg/types"
	"github.com/ArchipelagoAI/health-reporter/pkg/webhook"
)

// Concern represents an identified issue
type Concern struct {
	Title    string `json:"title"`
	Severity string `json:"severity"` // "low", "medium", "high"
	Details  string `json:"details"`
}

// Reporter generates health reports
type Reporter struct {
	mimirClient  *mimir.Client
	sender       webhook.Sender
	testRegistry smoke_tests.TestRegistry
}

// NewReporter creates a new health reporter
func NewReporter(mimirClient *mimir.Client, sender webhook.Sender) *Reporter {
	return &Reporter{
		mimirClient:  mimirClient,
		sender:       sender,
		testRegistry: nil,
	}
}

// SetTestRegistry sets the smoke test registry
func (r *Reporter) SetTestRegistry(registry smoke_tests.TestRegistry) {
	r.testRegistry = registry
}

// Generate generates a health report from current metrics
func (r *Reporter) Generate(ctx context.Context) (*types.Report, error) {
	// Get metrics from Mimir
	metrics, err := r.mimirClient.GetMetrics(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get metrics: %w", err)
	}

	// Analyze metrics
	report := &types.Report{
		Timestamp: time.Now().UTC(),
		ClusterMetrics: map[string]interface{}{
			"nodes": map[string]interface{}{
				"total":     metrics.Nodes.Total,
				"ready":     metrics.Nodes.Ready,
				"not_ready": metrics.Nodes.NotReady,
			},
			"pods": map[string]interface{}{
				"total":    metrics.Pods.Total,
				"running":  metrics.Pods.Running,
				"pending":  metrics.Pods.Pending,
				"failed":   metrics.Pods.Failed,
				"restarts": metrics.Pods.Restarts,
			},
			"resources": map[string]interface{}{
				"cpu_usage_percent":    metrics.Resources.CPUUsagePercent,
				"memory_usage_percent": metrics.Resources.MemoryUsagePercent,
				"disk_usage_percent":   metrics.Resources.DiskUsagePercent,
				"available_memory_gb":  metrics.Resources.AvailableMemoryGB,
				"available_storage_gb": metrics.Resources.AvailableStorageGB,
			},
		},
	}

	// Run smoke tests if registry is available
	if r.testRegistry != nil {
		smokeTestResults := r.testRegistry.RunAllTests(ctx)
		for _, result := range smokeTestResults {
			report.SmokeTests = append(report.SmokeTests, types.SmokeTestResult{
				Name:     result.Name,
				Type:     result.Type,
				Status:   result.Status,
				Message:  result.Message,
				Duration: int(result.Duration.Milliseconds()),
				Severity: result.Severity,
			})
		}
	}

	// Calculate overall status
	report.Status = r.calculateStatus(metrics)
	report.Summary = r.generateSummary(metrics, report.Status)
	report.Concerns = r.identifyConcerns(metrics)
	report.Recommendations = r.generateRecommendations(report)

	return report, nil
}

// SendReport sends the report to webhook
func (r *Reporter) SendReport(ctx context.Context, report *types.Report) error {
	return r.sender.Send(ctx, report)
}

// calculateStatus determines overall cluster health
func (r *Reporter) calculateStatus(metrics *mimir.Metrics) types.HealthStatus {
	// Critical: multiple failed pods, node down, or high resource usage
	if metrics.Pods.Failed > 10 || metrics.Nodes.NotReady > 0 {
		return types.StatusCritical
	}

	if metrics.Resources.CPUUsagePercent > 90 || metrics.Resources.MemoryUsagePercent > 90 {
		return types.StatusCritical
	}

	// Degraded: some failed pods, restarts, or elevated resource usage
	if metrics.Pods.Failed > 0 || metrics.Pods.Restarts > 5 {
		return types.StatusDegraded
	}

	if metrics.Resources.CPUUsagePercent > 80 || metrics.Resources.MemoryUsagePercent > 80 {
		return types.StatusDegraded
	}

	return types.StatusHealthy
}

// generateSummary creates a brief summary of cluster status
func (r *Reporter) generateSummary(metrics *mimir.Metrics, status types.HealthStatus) string {
	m := metrics

	switch status {
	case types.StatusCritical:
		return fmt.Sprintf(
			"⚠️ CRITICAL: Cluster has issues - %d/%d nodes ready, %d failed pods, CPU: %.0f%%, Memory: %.0f%%",
			m.Nodes.Ready, m.Nodes.Total, m.Pods.Failed,
			m.Resources.CPUUsagePercent, m.Resources.MemoryUsagePercent,
		)
	case types.StatusDegraded:
		return fmt.Sprintf(
			"⚠️ DEGRADED: Cluster has concerns - %d pod restarts (1h), %d pending pods, CPU: %.0f%%, Memory: %.0f%%",
			m.Pods.Restarts, m.Pods.Pending,
			m.Resources.CPUUsagePercent, m.Resources.MemoryUsagePercent,
		)
	default:
		return fmt.Sprintf(
			"✅ HEALTHY: All systems nominal - %d/%d nodes, %d running pods, CPU: %.0f%%, Memory: %.0f%%",
			m.Nodes.Ready, m.Nodes.Total, m.Pods.Running,
			m.Resources.CPUUsagePercent, m.Resources.MemoryUsagePercent,
		)
	}
}

// identifyConcerns identifies specific issues in the cluster
func (r *Reporter) identifyConcerns(metrics *mimir.Metrics) []types.Concern {
	var concerns []types.Concern

	// Check node health
	if metrics.Nodes.NotReady > 0 {
		concerns = append(concerns, types.Concern{
			Title:    "Nodes Not Ready",
			Severity: "high",
			Details:  fmt.Sprintf("%d node(s) not in ready state", metrics.Nodes.NotReady),
		})
	}

	// Check pod health
	if metrics.Pods.Failed > 0 {
		concerns = append(concerns, types.Concern{
			Title:    "Failed Pods",
			Severity: "high",
			Details:  fmt.Sprintf("%d pod(s) in failed state", metrics.Pods.Failed),
		})
	}

	if metrics.Pods.Pending > 2 {
		concerns = append(concerns, types.Concern{
			Title:    "Pending Pods",
			Severity: "medium",
			Details:  fmt.Sprintf("%d pod(s) pending for extended period", metrics.Pods.Pending),
		})
	}

	if metrics.Pods.Restarts > 5 {
		concerns = append(concerns, types.Concern{
			Title:    "Pod Restarts",
			Severity: "medium",
			Details:  fmt.Sprintf("%d pod restarts in last hour", metrics.Pods.Restarts),
		})
	}

	// Check resource pressure
	if metrics.Resources.CPUUsagePercent > 90 {
		concerns = append(concerns, types.Concern{
			Title:    "High CPU Usage",
			Severity: "high",
			Details:  fmt.Sprintf("CPU usage at %.0f%% - near saturation", metrics.Resources.CPUUsagePercent),
		})
	} else if metrics.Resources.CPUUsagePercent > 80 {
		concerns = append(concerns, types.Concern{
			Title:    "Elevated CPU Usage",
			Severity: "medium",
			Details:  fmt.Sprintf("CPU usage at %.0f%% - monitor for spikes", metrics.Resources.CPUUsagePercent),
		})
	}

	if metrics.Resources.MemoryUsagePercent > 90 {
		concerns = append(concerns, types.Concern{
			Title:    "High Memory Usage",
			Severity: "high",
			Details:  fmt.Sprintf("Memory usage at %.0f%% - risk of pod eviction", metrics.Resources.MemoryUsagePercent),
		})
	} else if metrics.Resources.MemoryUsagePercent > 80 {
		concerns = append(concerns, types.Concern{
			Title:    "Elevated Memory Usage",
			Severity: "medium",
			Details:  fmt.Sprintf("Memory usage at %.0f%% - monitor closely", metrics.Resources.MemoryUsagePercent),
		})
	}

	return concerns
}

// generateRecommendations suggests actions based on identified concerns
func (r *Reporter) generateRecommendations(report *types.Report) []string {
	var recommendations []string

	if report.Status == types.StatusCritical {
		recommendations = append(recommendations,
			"Investigate immediately using: kubectl describe nodes",
			"Check pod logs: kubectl logs <pod-name> -n <namespace>",
			"Review resource requests/limits and consider scaling",
		)
	}

	if report.Status == types.StatusDegraded {
		recommendations = append(recommendations,
			"Monitor pod restart patterns: kubectl get events",
			"Check if resource limits are appropriate",
			"Review application logs for errors or performance issues",
		)
	}

	if len(report.Concerns) > 0 {
		recommendations = append(recommendations,
			"Use 'kubectl top nodes' to see detailed resource usage",
			"Use 'kubectl top pods -A' to identify resource-heavy workloads",
		)
	}

	return recommendations
}
