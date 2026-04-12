package health

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/ArchipelagoAI/health-reporter/pkg/analysis"
	"github.com/ArchipelagoAI/health-reporter/pkg/mimir"
	"github.com/ArchipelagoAI/health-reporter/pkg/smoke_tests"
	"github.com/ArchipelagoAI/health-reporter/pkg/storage"
	"github.com/ArchipelagoAI/health-reporter/pkg/types"
	"github.com/ArchipelagoAI/health-reporter/pkg/webhook"
)

type Concern struct {
	Title    string `json:"title"`
	Severity string `json:"severity"`
	Details  string `json:"details"`
}

type Reporter struct {
	mimirClient  *mimir.Client
	sender       webhook.Sender
	testRegistry smoke_tests.TestRegistry
	historyMgr   *storage.HistoryManager
	analyzer     *analysis.TrendDetector
	llmClient    *analysis.LLMClient
	analysisCfg  analysis.Config
}

type Config struct {
	StorageDir      string
	RetentionHours  int
	AnalysisEnabled bool
	LLMEnabled      bool
	LLMEndpoint     string
	LLMModel        string
	LLMTimeout      int
}

func NewReporter(mimirClient *mimir.Client, sender webhook.Sender) *Reporter {
	return &Reporter{
		mimirClient:  mimirClient,
		sender:       sender,
		testRegistry: nil,
	}
}

func (r *Reporter) SetTestRegistry(registry smoke_tests.TestRegistry) {
	r.testRegistry = registry
}

func (r *Reporter) SetHistoryManager(mgr *storage.HistoryManager) {
	r.historyMgr = mgr
}

func (r *Reporter) SetAnalyzer(analyzer *analysis.TrendDetector) {
	r.analyzer = analyzer
}

func (r *Reporter) SetLLMClient(client *analysis.LLMClient) {
	r.llmClient = client
}

func (r *Reporter) SetAnalysisConfig(cfg analysis.Config) {
	r.analysisCfg = cfg
}

func (r *Reporter) Generate(ctx context.Context) (*types.Report, error) {
	metrics, err := r.mimirClient.GetMetrics(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get metrics: %w", err)
	}

	report := &types.Report{
		Timestamp: time.Now().UTC(),
		ClusterMetrics: map[string]interface{}{
			"nodes": map[string]interface{}{
				"total":         metrics.Nodes.Total,
				"ready":         metrics.Nodes.Ready,
				"not_ready":     metrics.Nodes.NotReady,
				"unschedulable": metrics.Nodes.Unschedulable,
				"cpu_cores":     metrics.Nodes.CPUCores,
				"memory_gb":     metrics.Nodes.MemoryGB,
			},
			"pods": map[string]interface{}{
				"total":         metrics.Pods.Total,
				"running":       metrics.Pods.Running,
				"pending":       metrics.Pods.Pending,
				"failed":        metrics.Pods.Failed,
				"succeeded":     metrics.Pods.Succeeded,
				"restarts":      metrics.Pods.Restarts,
				"unschedulable": metrics.Pods.Unschedulable,
			},
			"resources": map[string]interface{}{
				"cpu_usage_percent":     metrics.Resources.CPUUsagePercent,
				"memory_usage_percent":  metrics.Resources.MemoryUsagePercent,
				"disk_usage_percent":    metrics.Resources.DiskUsagePercent,
				"available_memory_gb":   metrics.Resources.AvailableMemoryGB,
				"available_storage_gb":  metrics.Resources.AvailableStorageGB,
				"cpu_cores_allocatable": metrics.Resources.CPUCoresAllocatable,
				"memory_gb_allocatable": metrics.Resources.MemoryGBAllocatable,
			},
			"deployments": map[string]interface{}{
				"total":       metrics.Deployments.Total,
				"ready":       metrics.Deployments.Ready,
				"unready":     metrics.Deployments.Unready,
				"unavailable": metrics.Deployments.Unavailable,
			},
			"jobs": map[string]interface{}{
				"total":     metrics.Jobs.Total,
				"active":    metrics.Jobs.Active,
				"failed":    metrics.Jobs.Failed,
				"succeeded": metrics.Jobs.Succeeded,
			},
			"services": map[string]interface{}{
				"total":        metrics.Services.Total,
				"cluster_ip":   metrics.Services.ClusterIP,
				"headless":     metrics.Services.Headless,
				"loadbalancer": metrics.Services.TypeLoadBalancer,
			},
			"storage": map[string]interface{}{
				"total_pvcs":   metrics.Storage.TotalPVCs,
				"bound_pvcs":   metrics.Storage.BoundPVCs,
				"pending_pvcs": metrics.Storage.PendingPVCs,
				"lost_pvcs":    metrics.Storage.LostPVCs,
			},
		},
	}

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

	report.Status = r.calculateStatus(metrics)
	report.Summary = r.generateSummary(metrics, report.Status)
	report.Concerns = r.identifyConcerns(metrics)
	report.Recommendations = r.generateRecommendations(report)

	if r.historyMgr != nil {
		if err := r.historyMgr.SaveReport(ctx, report); err != nil {
			_ = fmt.Errorf("failed to save report: %v", err)
		}

		if err := r.historyMgr.CleanupOldReports(ctx); err != nil {
			_ = fmt.Errorf("failed to cleanup old reports: %v", err)
		}
	}

	return report, nil
}

func (r *Reporter) Analyze(ctx context.Context, report *types.Report) *analysis.AnalysisResult {
	if r.analyzer == nil || r.historyMgr == nil {
		return nil
	}

	historicalReports, err := r.historyMgr.LoadReports(ctx, 24)
	if err != nil {
		log.Printf("failed to load historical reports: %v", err)
		return nil
	}

	log.Printf("trend analysis: loaded %d historical reports (need %d for trend analysis)",
		len(historicalReports), 6)

	result := r.analyzer.Analyze(ctx, report, historicalReports)

	if r.llmClient != nil && r.llmClient.IsAvailable(ctx) {
		metricsJSON, _ := json.Marshal(report.ClusterMetrics)
		smokeTestsJSON, _ := json.Marshal(report.SmokeTests)

		enhancedPrompt := r.llmClient.GenerateEnhancedPrompt(
			string(metricsJSON),
			fmt.Sprintf("%+v", result.Trends),
			fmt.Sprintf("%+v", result.Anomalies),
			string(smokeTestsJSON),
			string(report.Status),
		)

		if llmAnalysis, err := r.llmClient.Analyze(ctx, enhancedPrompt); err == nil {
			result.HealthSummary = llmAnalysis
		}
	}

	return result
}

func (r *Reporter) HasAnalyzer() bool {
	return r.analyzer != nil
}

func (r *Reporter) SendReportWithAnalysis(ctx context.Context, report *types.Report, analysis *analysis.AnalysisResult) error {
	if analysis != nil {
		report.Analysis = map[string]interface{}{
			"health_summary":   analysis.HealthSummary,
			"confidence_score": analysis.ConfidenceScore,
			"trends":           analysis.Trends,
			"anomalies":        analysis.Anomalies,
			"predictions":      analysis.Predictions,
		}
	}
	return r.sender.Send(ctx, report)
}

func (r *Reporter) SendReport(ctx context.Context, report *types.Report) error {
	return r.sender.Send(ctx, report)
}

func (r *Reporter) SaveReportWithAnalysis(ctx context.Context, report *types.Report) error {
	if r.historyMgr == nil {
		return nil
	}
	return r.historyMgr.SaveReport(ctx, report)
}

func (r *Reporter) calculateStatus(metrics *mimir.Metrics) types.HealthStatus {
	if metrics.Pods.Failed > 10 || metrics.Nodes.NotReady > 0 {
		return types.StatusCritical
	}

	if metrics.Resources.CPUUsagePercent > 90 || metrics.Resources.MemoryUsagePercent > 90 {
		return types.StatusCritical
	}

	if metrics.Pods.Failed > 0 || metrics.Pods.Restarts > 5 {
		return types.StatusDegraded
	}

	if metrics.Resources.CPUUsagePercent > 80 || metrics.Resources.MemoryUsagePercent > 80 {
		return types.StatusDegraded
	}

	return types.StatusHealthy
}

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

func (r *Reporter) identifyConcerns(metrics *mimir.Metrics) []types.Concern {
	var concerns []types.Concern

	if metrics.Nodes.NotReady > 0 {
		concerns = append(concerns, types.Concern{
			Title:    "Nodes Not Ready",
			Severity: "high",
			Details:  fmt.Sprintf("%d node(s) not in ready state", metrics.Nodes.NotReady),
		})
	}

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
