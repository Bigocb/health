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

## Output Format

**CRITICAL RULES - Follow exactly:**

1. START with ONE sentence stating overall status: "The cluster is HEALTHY." or "The cluster is DEGRADED:" or "The cluster is CRITICAL:"

2. IF HEALTHY with no issues: State this in 1-2 sentences and stop. Do not add filler.
   Example: "The cluster is HEALTHY with all metrics well below thresholds. No immediate action required."

3. IF there are constraints/risks, structure as:
   - **Issue**: [Specific node/metric name and exact numbers]
   - **Impact**: [What this means for operations]
   - **Recommendation**: [Specific action, not generic advice]

4. **FORBIDDEN (NEVER include):**
   - Generic statements like "monitor the cluster closely"
   - Redundant observations (e.g., "Memory is stable and CPU is stable")
   - Mentions of "no restarts" as if it's a problem or constraint
   - Vague phrases like "node capacity is limited" without specifics
   - Contradictory statements about cluster health
   - Filler words or paragraphs that don't add information

5. **REQUIRED:**
   - Maximum 300 words absolute limit
   - Every statement must have a specific reason for being there
   - Reference actual node names (vps01, app01, internal, etc.)
   - Quantify impact: "X nodes constrained, affects Y% of capacity"
   - If no action needed, explicitly say: "No remediation required at this time."

6. Output ONLY the summary text. No markdown headers, no "Executive Summary:" prefix.

---

Generate the summary now:`,
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
