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
	nodeReady := 0
	nodeUnschedulable := 0
	for _, node := range report.NodeMetrics {
		status := "Ready"
		if node.Unschedulable {
			status = "Unschedulable ⚠️"
			nodeUnschedulable++
		} else {
			nodeReady++
		}
		nodeStatusLines = append(nodeStatusLines, fmt.Sprintf(
			"- %s: CPU %.1f%%, Memory %.1f%%, Disk %.1f%% (%.1f GB available), %d pods, %s",
			node.Name,
			node.CPUUsagePercent,
			node.MemoryUsagePercent,
			node.DiskUsagePercent,
			node.AvailableDiskGB,
			node.PodCount,
			status,
		))
	}
	nodeStatus := strings.Join(nodeStatusLines, "\n")
	nodeTotal := len(report.NodeMetrics)

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
	prompt := fmt.Sprintf(`You are a Kubernetes cluster health analyst. Analyze the following cluster state and produce ONLY a concise executive summary.

## Current Cluster State

**Cluster Metrics**
- CPU Usage: %.1f%% (good — well below 70%% threshold)
- Memory Usage: %.1f%% (good — well below 85%% threshold)
- Disk Usage: %.1f%% (good — well below 90%% threshold)
- Available Memory: %.1f GB
- Available Storage: %.1f GB

**Node Status** (Total: %d, Ready: %d, Unschedulable: %d)
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

## Output Format

**CRITICAL RULES - Follow exactly:**

1. START with ONE sentence stating overall status and key metrics: "The cluster is HEALTHY with X% CPU, Y% memory, Z% disk usage."

2. Briefly describe cluster state (2-3 sentences max):
   - Overall capacity/readiness
   - Available resources (memory GB, storage GB)
   - Trend summary (stable, no restarts, minimal churn)

3. **CALL OUT CONSTRAINTS** - Use the provided node data:
   - List any UNSCHEDULABLE nodes with reason (e.g., "app01: Unschedulable")
   - List any nodes with HIGH resource usage (CPU >70%, Memory >75%, Disk >75%)
   - Format: "- node_name: status (specific metric)")
   - If NO constraints, state: "No resource constraints detected."

4. **FORBIDDEN (NEVER include):**
   - Generic statements like "monitor the cluster closely"
   - Redundant observations (e.g., "Memory is stable and CPU is stable")
   - Mentions of "no restarts" as problem statements
   - Vague phrases without specifics
   - Filler words or padding
   - Contradictory statements

5. **REQUIRED:**
   - Target: 450-550 words/chars
   - Reference actual node names (vps01, app01, internal, etc.)
   - Use exact numbers from provided metrics
   - If all metrics healthy: brief summary + constraint list
   - If any constraints: identify them clearly
   - No recommendations unless actually needed

6. Output ONLY the summary text. No markdown headers, no "Executive Summary:" prefix.

---

Generate the summary now:`,
		cpuUsage, memUsage, diskUsage, availMem, availStorage,
		nodeTotal, nodeReady, nodeUnschedulable,
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
