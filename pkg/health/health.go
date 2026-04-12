package health

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/ArchipelagoAI/health-reporter/pkg/analysis"
	"github.com/ArchipelagoAI/health-reporter/pkg/loki"
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
	lokiClient   *loki.Client
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

func (r *Reporter) SetLokiClient(client *loki.Client) {
	r.lokiClient = client
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

	// Convert per-node metrics from mimir to types.NodeMetrics
	if len(metrics.NodeDetails) > 0 {
		for _, detail := range metrics.NodeDetails {
			report.NodeMetrics = append(report.NodeMetrics, types.NodeMetrics{
				Name:               detail.Name,
				Ready:              detail.Ready,
				Unschedulable:      detail.Unschedulable,
				CPUUsagePercent:    detail.CPUUsagePercent,
				MemoryUsagePercent: detail.MemoryUsagePercent,
				AvailableMemoryGB:  detail.AvailableMemoryGB,
				PodCount:           detail.PodCount,
			})
		}
	}

	// Gather failed pods details
	if metrics.Pods.Failed > 0 && r.lokiClient != nil {
		report.FailedPods = r.getFailedPodsList(ctx)
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

	// Get Loki logs for additional context
	var logContext string
	if r.lokiClient != nil && r.lokiClient.IsAvailable(ctx) {
		logContext = r.getLogContext(ctx, report)
	}

	if r.llmClient != nil && r.llmClient.IsAvailable(ctx) {
		metricsText := r.formatMetricsAsText(report.ClusterMetrics)

		// Add per-node metrics to analysis
		if len(report.NodeMetrics) > 0 {
			metricsText += "\n## Per-Node Metrics\n"
			for _, node := range report.NodeMetrics {
				readyStr := "Ready"
				if !node.Ready {
					readyStr = "NotReady"
				}
				schedStr := "Schedulable"
				if node.Unschedulable {
					schedStr = "Unschedulable"
				}
				metricsText += fmt.Sprintf("- %s: %s, %s, CPU: %.1f%%, Memory: %.1f%%, Available: %.1fGB, Pods: %d\n",
					node.Name, readyStr, schedStr, node.CPUUsagePercent, node.MemoryUsagePercent, node.AvailableMemoryGB, node.PodCount)
			}
		}

		// Add failed pods to analysis
		if len(report.FailedPods) > 0 {
			metricsText += "\n## Failed Pods\n"
			for _, pod := range report.FailedPods {
				metricsText += fmt.Sprintf("- %s/%s: Phase=%s, Reason=%s, LastError=%s\n",
					pod.Namespace, pod.Name, pod.Phase, pod.Reason, pod.LastError)
			}
		}

		smokeTestsJSON, _ := json.Marshal(report.SmokeTests)

		// Phase 1: Data Analysis - Classify metrics with thresholds
		dataAnalysisPrompt := r.llmClient.GenerateDataAnalysisPrompt(
			metricsText,
			fmt.Sprintf("%+v", result.Trends),
		)

		log.Printf("LLM Phase 1 - Data Analysis prompt length: %d chars", len(dataAnalysisPrompt))

		dataAnalysisJSON, err := r.llmClient.Analyze(ctx, dataAnalysisPrompt)
		if err != nil {
			log.Printf("LLM Phase 1 failed: %v", err)
			return result
		}

		// Validate and correct thresholds server-side to fix LLM misclassifications
		dataAnalysisJSON = analysis.ValidatePhase1Response(dataAnalysisJSON)

		log.Printf("LLM Phase 1 analysis: %s", dataAnalysisJSON)

		// Phase 2: Narrative Generation - Create report based on analysis
		narrativePrompt := r.llmClient.GenerateNarrativePrompt(
			dataAnalysisJSON,
			string(smokeTestsJSON),
			logContext,
		)

		log.Printf("LLM Phase 2 - Narrative prompt length: %d chars", len(narrativePrompt))

		if llmAnalysis, err := r.llmClient.Analyze(ctx, narrativePrompt); err == nil {
			result.HealthSummary = llmAnalysis
		} else {
			log.Printf("LLM Phase 2 failed: %v", err)
		}
	}

	return result
}

func (r *Reporter) getLogContext(ctx context.Context, report *types.Report) string {
	var context string

	// Get failed pod errors first (most important)
	podsFailed := 0
	if pods, ok := report.ClusterMetrics["pods"].(map[string]interface{}); ok {
		if failed, ok := pods["failed"].(float64); ok {
			podsFailed = int(failed)
		}
	}

	if podsFailed > 0 {
		failedPodErrors, err := r.lokiClient.GetFailedPodsErrors(ctx)
		if err == nil && len(failedPodErrors) > 0 {
			context += fmt.Sprintf("\n## Failed Pod Logs (%d pods)\n", podsFailed)
			for pod, errors := range failedPodErrors {
				if len(errors) > 0 {
					context += fmt.Sprintf("\n### %s\n", pod)
					for _, err := range errors {
						context += fmt.Sprintf("- %s\n", err)
					}
				}
			}
		}
	}

	// Get recent errors from Loki
	errors, err := r.lokiClient.GetRecentErrors(ctx, 1*time.Hour)
	if err == nil && errors != nil {
		if errors.TotalErrors > 0 {
			context += fmt.Sprintf("\n## Recent Log Errors (1h)\n")
			context += fmt.Sprintf("Total errors in last hour: %d\n", errors.TotalErrors)
			if len(errors.TopErrors) > 0 {
				context += "Sample errors:\n"
				for i, err := range errors.TopErrors {
					if i < 3 {
						context += fmt.Sprintf("- %s\n", err)
					}
				}
			}
		}
	}

	return context
}

func (r *Reporter) getPodDetails(ctx context.Context, report *types.Report) string {
	var details string

	// Get failed pods
	podsFailed := 0
	if pods, ok := report.ClusterMetrics["pods"].(map[string]interface{}); ok {
		if failed, ok := pods["failed"].(float64); ok {
			podsFailed = int(failed)
		}
	}

	if podsFailed > 0 && r.lokiClient != nil {
		failedPodErrors, err := r.lokiClient.GetFailedPodsErrors(ctx)
		if err == nil && len(failedPodErrors) > 0 {
			details += "## Failed Pods\n"
			for pod, errors := range failedPodErrors {
				details += fmt.Sprintf("- **%s**: ", pod)
				if len(errors) > 0 {
					details += errors[0]
				} else {
					details += "No recent errors in logs"
				}
				details += "\n"
			}
		}
	}

	// Get pending pods info
	podsPending := 0
	if pods, ok := report.ClusterMetrics["pods"].(map[string]interface{}); ok {
		if pending, ok := pods["pending"].(float64); ok {
			podsPending = int(pending)
		}
	}

	if podsPending > 0 {
		details += "\n## Pending Pods\n"
		details += fmt.Sprintf("- %d pods in Pending state - likely due to resource constraints or scheduling issues\n", podsPending)
		details += "- Run `kubectl get pods -A --field-selector=status.phase=Pending` to see details\n"
	}

	if details == "" {
		details = "No failed or pending pods detected."
	}

	return details
}

// getFailedPodsList queries for failed pods with error context
func (r *Reporter) getFailedPodsList(ctx context.Context) []types.FailedPod {
	if r.lokiClient == nil {
		return nil
	}

	var failedPods []types.FailedPod

	// Get failed pod errors from Loki with pod/namespace info
	failedPodErrors, err := r.lokiClient.GetFailedPodsErrors(ctx)
	if err != nil || len(failedPodErrors) == 0 {
		return failedPods
	}

	// Convert error map to FailedPod array
	// Map key format is typically "namespace/pod-name"
	for podKey, errors := range failedPodErrors {
		lastError := ""
		if len(errors) > 0 {
			lastError = errors[0]
		}

		// Parse namespace/name from key
		namespace := "unknown"
		name := podKey
		if idx := len(podKey) - 1; idx > 0 {
			// Try to extract namespace from the pod key if it contains /
			for i := 0; i < len(podKey); i++ {
				if podKey[i] == '/' {
					namespace = podKey[:i]
					name = podKey[i+1:]
					break
				}
			}
		}

		failedPods = append(failedPods, types.FailedPod{
			Namespace:   namespace,
			Name:        name,
			Phase:       "Failed",
			Reason:      "Unknown",
			LastError:   lastError,
			RestartCount: 0,
		})
	}

	return failedPods
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

func (r *Reporter) formatMetricsAsText(metrics map[string]interface{}) string {
	var buf bytes.Buffer
	buf.WriteString("cluster_metrics:\n")

	// Nodes
	if nodes, ok := metrics["nodes"].(map[string]interface{}); ok {
		buf.WriteString("  nodes:\n")
		if v := getIntValue(nodes["total"]); v >= 0 {
			buf.WriteString(fmt.Sprintf("    total: %d\n", v))
		}
		if v := getIntValue(nodes["ready"]); v >= 0 {
			buf.WriteString(fmt.Sprintf("    ready: %d\n", v))
		}
		if v := getIntValue(nodes["not_ready"]); v >= 0 {
			buf.WriteString(fmt.Sprintf("    not_ready: %d\n", v))
		}
		if v := getIntValue(nodes["unschedulable"]); v >= 0 {
			buf.WriteString(fmt.Sprintf("    unschedulable: %d\n", v))
		}
	}

	// Pods
	if pods, ok := metrics["pods"].(map[string]interface{}); ok {
		buf.WriteString("  pods:\n")
		if v := getIntValue(pods["total"]); v >= 0 {
			buf.WriteString(fmt.Sprintf("    total: %d\n", v))
		}
		if v := getIntValue(pods["running"]); v >= 0 {
			buf.WriteString(fmt.Sprintf("    running: %d\n", v))
		}
		if v := getIntValue(pods["pending"]); v >= 0 {
			buf.WriteString(fmt.Sprintf("    pending: %d\n", v))
		}
		if v := getIntValue(pods["failed"]); v >= 0 {
			buf.WriteString(fmt.Sprintf("    failed: %d\n", v))
		}
		if v := getIntValue(pods["succeeded"]); v >= 0 {
			buf.WriteString(fmt.Sprintf("    succeeded: %d\n", v))
		}
		if v := getIntValue(pods["restarts"]); v >= 0 {
			buf.WriteString(fmt.Sprintf("    restarts_1h: %d\n", v))
		}
	}

	// Resources
	if resources, ok := metrics["resources"].(map[string]interface{}); ok {
		buf.WriteString("  resources:\n")
		if v := getFloatValue(resources["cpu_usage_percent"]); v >= 0 {
			buf.WriteString(fmt.Sprintf("    cpu_usage_percent: %.1f\n", v))
		}
		if v := getFloatValue(resources["memory_usage_percent"]); v >= 0 {
			buf.WriteString(fmt.Sprintf("    memory_usage_percent: %.1f\n", v))
		}
		if v := getFloatValue(resources["disk_usage_percent"]); v >= 0 {
			buf.WriteString(fmt.Sprintf("    disk_usage_percent: %.1f\n", v))
		}
		if v := getFloatValue(resources["available_memory_gb"]); v >= 0 {
			buf.WriteString(fmt.Sprintf("    available_memory_gb: %.0f\n", v))
		}
		if v := getFloatValue(resources["available_storage_gb"]); v >= 0 {
			buf.WriteString(fmt.Sprintf("    available_storage_gb: %.0f\n", v))
		}
	}

	return buf.String()
}

// getMapKeys returns the keys of a map for debugging
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// getIntValue converts a value to int, handling both int and float64 types
// Returns -1 if the value cannot be converted
func getIntValue(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case float32:
		return int(val)
	default:
		return -1
	}
}

// getFloatValue converts a value to float64, handling int, float64, and float32 types
// Returns -1 if the value cannot be converted
func getFloatValue(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	default:
		return -1
	}
}
