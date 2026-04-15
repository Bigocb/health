package health

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ArchipelagoAI/health-reporter/pkg/analysis"
	"github.com/ArchipelagoAI/health-reporter/pkg/cache"
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
	llmClient    *analysis.LLMClient  // Phase 1: Structured analysis
	llmClient2   *analysis.LLMClient  // Phase 2: Narrative generation (different model)
	analysisCfg  analysis.Config
	cache        *cache.EnrichedCache  // NEW: enriched data cache
	collector    *cache.CacheCollector // NEW: background collector
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

func (r *Reporter) SetLLMClient2(client *analysis.LLMClient) {
	r.llmClient2 = client
}

func (r *Reporter) SetAnalysisConfig(cfg analysis.Config) {
	r.analysisCfg = cfg
}

func (r *Reporter) SetCache(c *cache.EnrichedCache) {
	r.cache = c
}

func (r *Reporter) SetCacheCollector(collector *cache.CacheCollector) {
	r.collector = collector
}

// convertCacheToMetrics converts enriched cache format back to mimir.Metrics
func convertCacheToMetrics(cached *cache.EnrichedMetrics) *mimir.Metrics {
	metrics := &mimir.Metrics{
		Nodes:        mimir.NodeMetrics{},
		Pods:         mimir.PodMetrics{},
		Resources:    mimir.ResourceMetrics{},
		Deployments:  mimir.DeploymentMetrics{},
		Jobs:         mimir.JobMetrics{},
		Services:     mimir.ServiceMetrics{},
		Storage:      mimir.StorageMetrics{},
		NodeDetails:  make([]mimir.NodeDetail, 0),
	}

	// Extract cluster-level metrics from cache ClusterMetrics map
	if clusterMetrics, ok := cached.ClusterMetrics["nodes"].(map[string]interface{}); ok {
		metrics.Nodes.Total = intOrZero(clusterMetrics["total"])
		metrics.Nodes.Ready = intOrZero(clusterMetrics["ready"])
		metrics.Nodes.NotReady = intOrZero(clusterMetrics["not_ready"])
		metrics.Nodes.Unschedulable = intOrZero(clusterMetrics["unschedulable"])
		metrics.Nodes.CPUCores = intOrZero(clusterMetrics["cpu_cores"])
		metrics.Nodes.MemoryGB = floatOrZero(clusterMetrics["memory_gb"])
	}

	if clusterMetrics, ok := cached.ClusterMetrics["pods"].(map[string]interface{}); ok {
		metrics.Pods.Total = intOrZero(clusterMetrics["total"])
		metrics.Pods.Running = intOrZero(clusterMetrics["running"])
		metrics.Pods.Pending = intOrZero(clusterMetrics["pending"])
		metrics.Pods.Failed = intOrZero(clusterMetrics["failed"])
		metrics.Pods.Succeeded = intOrZero(clusterMetrics["succeeded"])
		metrics.Pods.Restarts = intOrZero(clusterMetrics["restarts"])
		metrics.Pods.Unschedulable = intOrZero(clusterMetrics["unschedulable"])
	}

	if resourceMetrics, ok := cached.ClusterMetrics["resources"].(map[string]interface{}); ok {
		metrics.Resources.CPUUsagePercent = floatOrZero(resourceMetrics["cpu_usage_percent"])
		metrics.Resources.MemoryUsagePercent = floatOrZero(resourceMetrics["memory_usage_percent"])
		metrics.Resources.DiskUsagePercent = floatOrZero(resourceMetrics["disk_usage_percent"])
		metrics.Resources.AvailableMemoryGB = floatOrZero(resourceMetrics["available_memory_gb"])
		metrics.Resources.AvailableStorageGB = floatOrZero(resourceMetrics["available_storage_gb"])
		metrics.Resources.CPUCoresAllocatable = floatOrZero(resourceMetrics["cpu_cores_allocatable"])
		metrics.Resources.MemoryGBAllocatable = floatOrZero(resourceMetrics["memory_gb_allocatable"])
	}

	if deployMetrics, ok := cached.ClusterMetrics["deployments"].(map[string]interface{}); ok {
		metrics.Deployments.Total = intOrZero(deployMetrics["total"])
		metrics.Deployments.Ready = intOrZero(deployMetrics["ready"])
		metrics.Deployments.Unready = intOrZero(deployMetrics["unready"])
		metrics.Deployments.Unavailable = intOrZero(deployMetrics["unavailable"])
	}

	if jobMetrics, ok := cached.ClusterMetrics["jobs"].(map[string]interface{}); ok {
		metrics.Jobs.Total = intOrZero(jobMetrics["total"])
		metrics.Jobs.Active = intOrZero(jobMetrics["active"])
		metrics.Jobs.Failed = intOrZero(jobMetrics["failed"])
		metrics.Jobs.Succeeded = intOrZero(jobMetrics["succeeded"])
	}

	if serviceMetrics, ok := cached.ClusterMetrics["services"].(map[string]interface{}); ok {
		metrics.Services.Total = intOrZero(serviceMetrics["total"])
		metrics.Services.ClusterIP = intOrZero(serviceMetrics["cluster_ip"])
		metrics.Services.Headless = intOrZero(serviceMetrics["headless"])
		metrics.Services.TypeLoadBalancer = intOrZero(serviceMetrics["loadbalancer"])
	}

	if storageMetrics, ok := cached.ClusterMetrics["storage"].(map[string]interface{}); ok {
		metrics.Storage.TotalPVCs = intOrZero(storageMetrics["total_pvcs"])
		metrics.Storage.BoundPVCs = intOrZero(storageMetrics["bound_pvcs"])
		metrics.Storage.PendingPVCs = intOrZero(storageMetrics["pending_pvcs"])
		metrics.Storage.LostPVCs = intOrZero(storageMetrics["lost_pvcs"])
	}

	// Convert per-node metrics from cache snapshots
	for _, nodeSnapshot := range cached.NodeMetrics {
		metrics.NodeDetails = append(metrics.NodeDetails, mimir.NodeDetail{
			Name:                nodeSnapshot.NodeName,
			Ready:               nodeSnapshot.Ready,
			Unschedulable:       nodeSnapshot.Unschedulable,
			CPUUsagePercent:     nodeSnapshot.CPUUsagePercent,
			MemoryUsagePercent:  nodeSnapshot.MemoryUsagePercent,
			AvailableMemoryGB:   nodeSnapshot.AvailableMemoryGB,
			PodCount:            nodeSnapshot.PodCount,
		})
	}

	return metrics
}

func (r *Reporter) Generate(ctx context.Context) (*types.Report, error) {
	var metrics *mimir.Metrics

	// Prefer cache (if enabled and available), fallback to direct queries
	if r.cache != nil {
		cachedEnriched := r.cache.GetLatestMetrics()
		if cachedEnriched != nil {
			// Use cached metrics
			metrics = convertCacheToMetrics(cachedEnriched)
			log.Printf("[Report] Generating report from cached metrics (timestamp: %v, cache age: %v)",
				cachedEnriched.Timestamp, time.Since(cachedEnriched.Timestamp))
		} else {
			// Cache not ready yet, fallback to direct query
			log.Printf("[Report] Cache available but empty, falling back to direct metrics query")
			var err error
			metrics, err = r.mimirClient.GetMetrics(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to get metrics: %w", err)
			}
		}
	} else {
		// Cache not initialized, fallback to direct query
		log.Printf("[Report] Cache not initialized, using direct metrics query")
		var err error
		metrics, err = r.mimirClient.GetMetrics(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get metrics: %w", err)
		}
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
	// If mimir queries failed, try to use cached metrics instead
	nodeMetricsToUse := metrics.NodeDetails
	if len(nodeMetricsToUse) == 0 && r.cache != nil {
		// Fallback to cache if live metrics failed
		cachedMetrics := r.cache.GetLatestMetrics()
		if cachedMetrics != nil && len(cachedMetrics.NodeMetrics) > 0 {
			// Convert cache node metrics to mimir NodeDetail format
			for _, cached := range cachedMetrics.NodeMetrics {
				nodeMetricsToUse = append(nodeMetricsToUse, mimir.NodeDetail{
					Name:               cached.NodeName,
					Ready:              cached.Ready,
					Unschedulable:      cached.Unschedulable,
					CPUUsagePercent:    cached.CPUUsagePercent,
					MemoryUsagePercent: cached.MemoryUsagePercent,
					AvailableMemoryGB:  cached.AvailableMemoryGB,
					PodCount:           cached.PodCount,
				})
			}
		}
	}

	if len(nodeMetricsToUse) > 0 {
		for _, detail := range nodeMetricsToUse {
			report.NodeMetrics = append(report.NodeMetrics, types.NodeMetrics{
				Name:               detail.Name,
				Ready:              detail.Ready,
				Unschedulable:      detail.Unschedulable,
				CPUUsagePercent:    detail.CPUUsagePercent,
				MemoryUsagePercent: detail.MemoryUsagePercent,
				DiskUsagePercent:   detail.DiskUsagePercent,
				AvailableMemoryGB:  detail.AvailableMemoryGB,
				AvailableDiskGB:    detail.AvailableDiskGB,
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

	// Always invoke LLM for executive summary (different prompts based on status)
	if r.llmClient != nil {
		var llmPrompt string
		var llmTimeout time.Duration

		if report.Status == types.StatusHealthy {
			// Happy Path: Brief executive summary
			llmPrompt = analysis.GenerateExecutiveSummaryPrompt(report)
			llmTimeout = time.Duration(120) * time.Second // 2 minutes for simple summary
			log.Printf("[Report] Happy Path: Invoking LLM for executive summary (brief)")
		} else {
			// Smart Path: Detailed root cause analysis
			llmPrompt = analysis.GenerateExecutiveSummaryPrompt(report) // TODO: Create GenerateRootCauseAnalysisPrompt() later
			llmTimeout = time.Duration(360) * time.Second              // 6 minutes for detailed analysis
			log.Printf("[Report] Smart Path: Invoking LLM for root cause analysis (status: %s)", report.Status)
		}

		// Create a context with timeout for LLM calls
		llmCtx, cancel := context.WithTimeout(ctx, llmTimeout)
		defer cancel()

		llmResponse, err := r.llmClient.Analyze(llmCtx, llmPrompt)
		if err != nil {
			log.Printf("[Report] LLM analysis failed (non-blocking): %v", err)
			// Don't fail the entire report generation, just skip LLM analysis
		} else if llmResponse != "" {
			// Append LLM analysis to report as additional context
			report.Summary = fmt.Sprintf("%s\n\n## LLM Analysis:\n%s", report.Summary, llmResponse)
			log.Printf("[Report] Added LLM analysis to report (length: %d chars)", len(llmResponse))
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

	// TODO: Log context for future smart path (LLM root cause analysis)
	// For now, running deterministic-only path
	// var logContext string
	// if r.cache != nil {
	// 	logContext = r.getLogContextFromCache()
	// 	log.Printf("[Analyze] Using enriched cache data: %d failed pods, %d error entries",
	// 		r.cache.GetStats().FailedPodsCount, r.cache.GetStats().TotalErrorEntries)
	// } else if r.lokiClient != nil && r.lokiClient.IsAvailable(ctx) {
	// 	logContext = r.getLogContext(ctx, report)
	// 	log.Printf("[Analyze] Fetching live log data (cache not available)")
	// }

	// DETERMINISTIC HAPPY PATH - No LLM, no dependencies on log context
	if true {
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

		// Add application-level metrics to analysis
		if appMetrics, ok := report.ClusterMetrics["applications"].(map[string]interface{}); ok && len(appMetrics) > 0 {
			metricsText += "\n## Application Metrics\n"
			for appName, metrics := range appMetrics {
				if m, ok := metrics.(map[string]interface{}); ok {
					metricsText += fmt.Sprintf("- %s:\n", appName)
					metricsText += fmt.Sprintf("  Request Rate: %.2f req/s\n", floatOrZero(m["request_rate_rps"]))
					metricsText += fmt.Sprintf("  Error Rate: %.2f err/s (%.1f%% of requests)\n",
						floatOrZero(m["error_rate_rps"]), floatOrZero(m["error_percent"]))
					metricsText += fmt.Sprintf("  Latency: P50=%.0fms, P95=%.0fms, P99=%.0fms\n",
						floatOrZero(m["p50_latency_ms"]), floatOrZero(m["p95_latency_ms"]), floatOrZero(m["p99_latency_ms"]))
					metricsText += fmt.Sprintf("  Replicas: %d available / %d desired\n\n",
						intOrZero(m["available_replicas"]), intOrZero(m["desired_replicas"]))
				}
			}
		}

		// Format smoke tests nicely for summary (not as JSON)
		smokeTestsSummary := ""
		if len(report.SmokeTests) > 0 {
			passed := 0
			failed := 0
			for _, st := range report.SmokeTests {
				if st.Status == "pass" {
					passed++
				} else {
					failed++
				}
			}
			smokeTestsSummary = fmt.Sprintf("%d passed", passed)
			if failed > 0 {
				smokeTestsSummary += fmt.Sprintf(", %d failed", failed)
			}
		}

		// PHASE 1 STEP 1: Classify metrics server-side (deterministic, not LLM-based)
		var cpuPercent, memPercent, diskPercent float64
		var resourcesMap, nodesMap, podsMap map[string]interface{}

		if resources, ok := report.ClusterMetrics["resources"].(map[string]interface{}); ok {
			resourcesMap = resources
			cpuPercent = floatOrZero(resources["cpu_usage_percent"])
			memPercent = floatOrZero(resources["memory_usage_percent"])
			diskPercent = floatOrZero(resources["disk_usage_percent"])
		}
		if nodes, ok := report.ClusterMetrics["nodes"].(map[string]interface{}); ok {
			nodesMap = nodes
		}
		if pods, ok := report.ClusterMetrics["pods"].(map[string]interface{}); ok {
			podsMap = pods
		}

		classifiedMetrics := analysis.ClassifyMetrics(cpuPercent, memPercent, diskPercent)
		healthStatus := analysis.DetermineHealthStatus(classifiedMetrics)

		// Classify per-node metrics
		var perNodeClassifications []analysis.PerNodeClassification
		for _, node := range report.NodeMetrics {
			nodeClass := analysis.ClassifyPerNodeMetrics(node.Name, node.CPUUsagePercent, node.MemoryUsagePercent)
			perNodeClassifications = append(perNodeClassifications, nodeClass)
		}

		// Build per-node classifications text
		perNodeText := "### Per-Node Classifications\n"
		for _, nodeClass := range perNodeClassifications {
			unschedulableStr := ""
			for _, n := range report.NodeMetrics {
				if n.Name == nodeClass.NodeName && n.Unschedulable {
					unschedulableStr = " (Unschedulable)"
					break
				}
			}
			perNodeText += fmt.Sprintf("- %s: CPU %.1f%% [%s], Memory %.1f%% [%s]%s\n",
				nodeClass.NodeName,
				nodeClass.CPU.Value, nodeClass.CPU.Status,
				nodeClass.Memory.Value, nodeClass.Memory.Status,
				unschedulableStr)
		}

		// DETERMINISTIC ANALYSIS ONLY - No LLM
		// Build clean health summary from server-side classifications

		// Per-node status
		var perNodeSummary string
		for _, nodeClass := range perNodeClassifications {
			unschedulableStr := ""
			for _, n := range report.NodeMetrics {
				if n.Name == nodeClass.NodeName && n.Unschedulable {
					unschedulableStr = " ⚠️ Unschedulable"
					break
				}
			}
			perNodeSummary += fmt.Sprintf("- **%s**: CPU %s (%s), Memory %s (%s)%s\n",
				nodeClass.NodeName,
				fmt.Sprintf("%.1f%%", nodeClass.CPU.Value),
				nodeClass.CPU.Status,
				fmt.Sprintf("%.1f%%", nodeClass.Memory.Value),
				nodeClass.Memory.Status,
				unschedulableStr)
		}

		// Build deterministic report
		reportSummary := fmt.Sprintf(`**Cluster Health Report** — %s

**Executive Summary**
Cluster is currently **%s**. All metrics within threshold ranges.

**Cluster Metrics**
- CPU Usage: %.1f%% [%s]
- Memory Usage: %.1f%% [%s]
- Disk Usage: %.1f%% [%s]
- Available Memory: %.0f GB
- Available Storage: %.0f GB

**Node Status**
%s
**Cluster Resources**
- Nodes: %d total (%d ready, %d unschedulable)
- Pods: %d total (%d running, %d failed, %d pending)

**Smoke Tests**
%s`,
			time.Now().Format("2006-01-02 15:04:05 MST"),
			strings.ToUpper(healthStatus),
			classifiedMetrics["cpu"].Value, classifiedMetrics["cpu"].Status,
			classifiedMetrics["memory"].Value, classifiedMetrics["memory"].Status,
			classifiedMetrics["disk"].Value, classifiedMetrics["disk"].Status,
			floatOrZero(resourcesMap["available_memory_gb"]),
			floatOrZero(resourcesMap["available_storage_gb"]),
			perNodeSummary,
			getIntValue(nodesMap["total"]),
			getIntValue(nodesMap["ready"]),
			getIntValue(nodesMap["unschedulable"]),
			getIntValue(podsMap["total"]),
			getIntValue(podsMap["running"]),
			getIntValue(podsMap["failed"]),
			getIntValue(podsMap["pending"]),
			smokeTestsSummary)

		result.HealthSummary = reportSummary

		// Replace executive summary with LLM analysis if available
		if strings.Contains(report.Summary, "## LLM Analysis:") {
			parts := strings.SplitN(report.Summary, "## LLM Analysis:", 2)
			if len(parts) == 2 {
				llmAnalysis := strings.TrimSpace(parts[1])
				// Replace the executive summary paragraph with LLM analysis
				// Keep the header format: "**Executive Summary**\n"
				result.HealthSummary = strings.Replace(
					reportSummary,
					"**Executive Summary**\nCluster is currently **"+strings.ToUpper(healthStatus)+"**. All metrics within threshold ranges.",
					"**Executive Summary**\n"+llmAnalysis,
					1,
				)
				log.Printf("[DETERMINISTIC ANALYSIS + LLM] Executive summary replaced with LLM analysis")
			}
		} else {
			log.Printf("[DETERMINISTIC ANALYSIS] Report generated (no LLM):\n%s", reportSummary)
		}

		// SMART PATH: If status is degraded/critical, invoke LLM for root cause analysis
		if healthStatus != "healthy" && r.llmClient != nil && r.llmClient.IsAvailable(ctx) {
			log.Printf("[SMART PATH] Status is %s, analyzing root causes with LLM", healthStatus)

			// Get log context for LLM analysis
			var logContext string
			if r.cache != nil {
				logContext = r.getLogContextFromCache()
				log.Printf("[SMART PATH] Using enriched cache data")
			} else if r.lokiClient != nil && r.lokiClient.IsAvailable(ctx) {
				logContext = r.getLogContext(ctx, report)
				log.Printf("[SMART PATH] Fetching live log data")
			}

			// Build root cause analysis prompt
			rootCausePrompt := fmt.Sprintf(`You are a Kubernetes cluster health analyst. The cluster is currently %s.

## Current Metrics & Status
%s

## Unschedulable Nodes
%d node(s) marked unschedulable

## Pod State
- Running: %d
- Failed: %d
- Pending: %d

## Log Context
%s

## Your Task
Analyze the logs and metrics to determine:
1. Why is the cluster degraded? (specific root causes)
2. Which nodes/pods are affected and why?
3. What immediate actions should be taken?

Be precise and reference specific logs and metrics.`,
				strings.ToUpper(healthStatus),
				reportSummary,
				getIntValue(nodesMap["unschedulable"]),
				getIntValue(podsMap["running"]),
				getIntValue(podsMap["failed"]),
				getIntValue(podsMap["pending"]),
				logContext)

			if rootCauseAnalysis, err := r.llmClient.Analyze(ctx, rootCausePrompt); err == nil {
				// Append root cause analysis to report
				result.HealthSummary = reportSummary + "\n\n## Root Cause Analysis\n" + rootCauseAnalysis
				log.Printf("[SMART PATH] Root cause analysis complete")
			} else {
				log.Printf("[SMART PATH] LLM root cause analysis failed: %v (using deterministic report only)", err)
			}
		}
	}

	return result
}

// getLogContextFromCache builds log context from enriched cache data
func (r *Reporter) getLogContextFromCache() string {
	var context string

	// Get enriched failed pods from cache
	failedPods := r.cache.GetFailedPods()
	if len(failedPods) > 0 {
		context += fmt.Sprintf("\n## Failed Pod Logs (%d pods with detailed errors)\n", len(failedPods))
		for _, pod := range failedPods {
			context += fmt.Sprintf("\n### %s/%s\n", pod.Namespace, pod.PodName)
			context += fmt.Sprintf("**Error Category:** %s\n", pod.ErrorCategory)
			if len(pod.Errors) > 0 {
				context += "**Error Details:**\n"
				for i, err := range pod.Errors {
					if i < 10 {
						context += fmt.Sprintf("  %d. %s\n", i+1, err.Message)
					}
				}
				if len(pod.Errors) > 10 {
					context += fmt.Sprintf("  ... and %d more errors\n", len(pod.Errors)-10)
				}
			}
			// Add node context if available
			if pod.NodeMetricsAtTime.NodeName != "" {
				context += fmt.Sprintf("**Node at failure time:** %s (CPU: %.1f%%, Memory: %.1f%%)\n",
					pod.NodeMetricsAtTime.NodeName,
					pod.NodeMetricsAtTime.CPUUsagePercent,
					pod.NodeMetricsAtTime.MemoryUsagePercent)
			}
		}
	}

	// Get recent metrics trends
	latestMetrics := r.cache.GetLatestMetrics()
	if latestMetrics != nil {
		context += fmt.Sprintf("\n## Recent Metrics Trends\n")
		context += fmt.Sprintf("- CPU Trend: %s\n", latestMetrics.CPUTrend)
		context += fmt.Sprintf("- Memory Trend: %s\n", latestMetrics.MemoryTrend)
		context += fmt.Sprintf("- Last updated: %v\n", latestMetrics.Timestamp)
	}

	// Add cache stats
	stats := r.cache.GetStats()
	if stats.TotalErrorEntries > 0 {
		context += fmt.Sprintf("\n## Cache Statistics\n")
		context += fmt.Sprintf("- Failed pods tracked: %d\n", stats.FailedPodsCount)
		context += fmt.Sprintf("- Total error entries: %d\n", stats.TotalErrorEntries)
		context += fmt.Sprintf("- Error time window: %v to %v\n", stats.OldestErrorTime, stats.NewestErrorTime)
		context += fmt.Sprintf("- Cache size: %.1f MB\n", float64(stats.CacheSizeBytes)/1024/1024)
	}

	return context
}

func (r *Reporter) getLogContext(ctx context.Context, report *types.Report) string {
	var context string

	// Get failed pod errors first (most important) - GET ALL ERRORS
	podsFailed := 0
	if pods, ok := report.ClusterMetrics["pods"].(map[string]interface{}); ok {
		if failed, ok := pods["failed"].(float64); ok {
			podsFailed = int(failed)
		}
	}

	if podsFailed > 0 {
		failedPodErrors, err := r.lokiClient.GetFailedPodsErrors(ctx)
		if err == nil && len(failedPodErrors) > 0 {
			context += fmt.Sprintf("\n## Failed Pod Logs (%d pods with detailed errors)\n", podsFailed)
			for pod, errors := range failedPodErrors {
				if len(errors) > 0 {
					context += fmt.Sprintf("\n### %s\n", pod)
					// Include ALL available errors, not just the first
					context += "**Error Details:**\n"
					for i, err := range errors {
						// Include up to 10 error entries per pod for deep analysis
						if i < 10 {
							context += fmt.Sprintf("  %d. %s\n", i+1, err)
						}
					}
					// Add error count context
					if len(errors) > 10 {
						context += fmt.Sprintf("  ... and %d more errors\n", len(errors)-10)
					}
				}
			}
		}
	}

	// Get recent errors from Loki - INCREASE SAMPLE SIZE
	errors, err := r.lokiClient.GetRecentErrors(ctx, 1*time.Hour)
	if err == nil && errors != nil {
		if errors.TotalErrors > 0 {
			context += fmt.Sprintf("\n## Recent Log Errors (1h window)\n")
			context += fmt.Sprintf("**Total errors detected: %d**\n", errors.TotalErrors)
			if len(errors.TopErrors) > 0 {
				context += "\n**Top Error Patterns (for analysis):**\n"
				// Increased from 3 to 10 samples for deeper analysis
				maxSamples := 10
				if len(errors.TopErrors) < maxSamples {
					maxSamples = len(errors.TopErrors)
				}
				for i := 0; i < maxSamples; i++ {
					context += fmt.Sprintf("  %d. %s\n", i+1, errors.TopErrors[i])
				}
				if len(errors.TopErrors) > 10 {
					context += fmt.Sprintf("\n  ... %d additional error patterns detected\n", len(errors.TopErrors)-10)
				}
			}
		}
	}

	// Get pod restart patterns if restarts > 5
	podsRestarts := 0
	if pods, ok := report.ClusterMetrics["pods"].(map[string]interface{}); ok {
		if restarts, ok := pods["restarts"].(float64); ok {
			podsRestarts = int(restarts)
		}
	}

	if podsRestarts > 5 {
		context += fmt.Sprintf("\n## Pod Restart Alert (%d restarts in 1h)\n", podsRestarts)
		context += "This indicates potential pod crashes or resource issues. Check failed pod logs above for root causes.\n"
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
			Namespace:    namespace,
			Name:         name,
			Phase:        "Failed",
			Reason:       "Unknown",
			LastError:    lastError,
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

// floatOrZero converts a value to float64, returning 0 if conversion fails
func floatOrZero(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	default:
		return 0
	}
}

// intOrZero converts a value to int, returning 0 if conversion fails
func intOrZero(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case float32:
		return int(val)
	default:
		return 0
	}
}

// truncateString truncates a string to maxLen and adds "..." if truncated
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
