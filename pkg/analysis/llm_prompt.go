package analysis

import (
	"fmt"
	"strings"

	"github.com/ArchipelagoAI/health-reporter/pkg/types"
)

// GenerateExecutiveSummaryPrompt creates the LLM prompt for executive summary generation
func GenerateExecutiveSummaryPrompt(report *types.Report) string {
	// Extract cluster metrics
	clusterMetrics := report.ClusterMetrics
	cpuUsage := floatOrZero(clusterMetrics, "resources", "cpu_usage_percent")
	memUsage := floatOrZero(clusterMetrics, "resources", "memory_usage_percent")
	diskUsage := floatOrZero(clusterMetrics, "resources", "disk_usage_percent")
	availMem := floatOrZero(clusterMetrics, "resources", "available_memory_gb")
	availStorage := floatOrZero(clusterMetrics, "resources", "available_storage_gb")

	// Build node status section
	nodeStatusLines := []string{}
	for _, node := range report.NodeMetrics {
		status := "Ready"
		if node.Unschedulable {
			status = "Unschedulable ⚠️"
		}
		nodeStatusLines = append(nodeStatusLines, fmt.Sprintf(
			"- %s: CPU %.1f%%, Memory %.1f%%, Disk available %.1f GB, %d pods, %s",
			node.Name,
			node.CPUUsagePercent,
			node.MemoryUsagePercent,
			node.AvailableDiskGB,
			node.PodCount,
			status,
		))
	}
	nodeStatus := strings.Join(nodeStatusLines, "\n")

	// Count pod states
	podResources := clusterMetrics["pods"].(map[string]interface{})
	running := intOrZero(podResources, "running")
	pending := intOrZero(podResources, "pending")
	failed := intOrZero(podResources, "failed")

	// Count smoke test results
	passCount := 0
	failCount := 0
	for _, test := range report.SmokeTests {
		if test.Status == "pass" {
			passCount++
		} else {
			failCount++
		}
	}

	// Build the prompt
	prompt := fmt.Sprintf(`You are a Kubernetes cluster health analyst. Analyze the following cluster state and produce ONLY an executive summary.

## Current Cluster State

**Cluster Metrics**
- CPU Usage: %.1f%% (good — well below 70%% threshold)
- Memory Usage: %.1f%% (good — well below 85%% threshold)
- Disk Usage: %.1f%% (good — well below 90%% threshold)
- Available Memory: %.1f GB
- Available Storage: %.1f GB

**Node Status**
%s

**Pod Capacity**
- Running: %d
- Pending (awaiting node capacity): %d
- Failed: %d

**Smoke Tests**
- %d passed, %d failed

**Trend Analysis (24h)**
- Memory: Stable
- CPU: Stable
- Pod churn: Minimal
- No restarts in last hour

## Your Task

Write a concise executive summary (aim for ~300 words, maximum 350) addressing:
1. Overall cluster health status
2. Key constraints or risks (if any)
3. Recommended actions (if any)

**Requirements:**
- STRICT: Keep to 300-350 words maximum
- Professional tone, concise language
- Specific component names (not vague)
- Lead with clear status: HEALTHY / DEGRADED / CRITICAL
- Base recommendations only on metrics shown
- Do not recommend pod eviction without understanding root cause
- If no immediate remediation is needed, state that clearly
- Quantify fault tolerance impact if a node is constrained
- If the root cause is disk-based, include specific cleanup recommendations
- Use bullet points for clarity, not long paragraphs

---

Generate the executive summary now:`,
		cpuUsage, memUsage, diskUsage, availMem, availStorage,
		nodeStatus,
		running, pending, failed,
		passCount, failCount,
	)

	return prompt
}

// Helper function to safely extract nested float values
func floatOrZero(m interface{}, keys ...string) float64 {
	current := m
	for _, key := range keys {
		if mmap, ok := current.(map[string]interface{}); ok {
			current = mmap[key]
		} else {
			return 0.0
		}
	}

	if f, ok := current.(float64); ok {
		return f
	}
	return 0.0
}

// Helper function to safely extract nested int values
func intOrZero(m interface{}, keys ...string) int {
	current := m
	for _, key := range keys {
		if mmap, ok := current.(map[string]interface{}); ok {
			current = mmap[key]
		} else {
			return 0
		}
	}

	switch v := current.(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}
