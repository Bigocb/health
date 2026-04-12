package analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

type LLMClient struct {
	endpoint    string
	model       string
	timeout     time.Duration
	maxRetries  int
	httpClient  *http.Client
	promptCache map[string]string
}

type LLMRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Stream      bool      `json:"stream"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatResponse struct {
	Choices []Choice `json:"choices"`
}

type Choice struct {
	Message Message `json:"message"`
}

func NewLLMClient(endpoint, model string, timeoutSeconds, maxRetries int) *LLMClient {
	return &LLMClient{
		endpoint:    endpoint,
		model:       model,
		timeout:     time.Duration(timeoutSeconds) * time.Second,
		maxRetries:  maxRetries,
		httpClient:  &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second},
		promptCache: make(map[string]string),
	}
}

func (l *LLMClient) Analyze(ctx context.Context, prompt string) (string, error) {
	log.Printf("[LLM] Analyzing prompt, calling API (no caching)")

	var lastErr error
	for i := 0; i < l.maxRetries; i++ {
		resp, err := l.callAPI(ctx, prompt)
		if err == nil && len(resp) > 10 {
			log.Printf("[LLM] Got response, length: %d chars", len(resp))
			return resp, nil
		}
		lastErr = err
		log.Printf("[LLM] Attempt %d failed: %v", i+1, err)
		time.Sleep(time.Duration(i+1) * time.Second)
	}

	return "", fmt.Errorf("LLM request failed after %d retries: %w", l.maxRetries, lastErr)
}

func (l *LLMClient) callAPI(ctx context.Context, prompt string) (string, error) {
	url := fmt.Sprintf("%s/api/generate", l.endpoint)

	systemPrompt := "You are a precise Kubernetes cluster health analyst. You ONLY reference data that is explicitly provided. You do NOT invent metrics or services. When uncertain, you say DATA_NOT_PROVIDED. Your goal is accuracy, not length."

	fullPrompt := fmt.Sprintf("System: %s\n\nUser: %s", systemPrompt, prompt)

	reqBody := map[string]interface{}{
		"model":      l.model,
		"prompt":     fullPrompt,
		"stream":     false,
		"max_tokens": 2048,
		"options": map[string]interface{}{
			"temperature": 0.2,
		},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	log.Printf("[LLM] Request to %s with model %s, prompt length %d", url, l.model, len(prompt))

	resp, err := l.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode == 404 {
		log.Printf("[LLM] Model %s not found, retrying with llama3.2:1b", l.model)
		reqBody["model"] = "llama3.2:1b"
		data, _ = json.Marshal(reqBody)
		req, _ = http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		resp, err = l.httpClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("fallback request failed: %w", err)
		}
		defer resp.Body.Close()
		bodyBytes, _ = io.ReadAll(resp.Body)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LLM returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var genResp struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(bodyBytes, &genResp); err != nil {
		log.Printf("[LLM] Raw response: %.500s", string(bodyBytes))
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	log.Printf("[LLM] Response received, length: %d chars, first 200 chars: %.200s", len(genResp.Response), genResp.Response)

	return genResp.Response, nil
}

func (l *LLMClient) GenerateAnalysisPrompt(currentReport string, trends string, anomalies string) string {
	return fmt.Sprintf(`You are a Kubernetes cluster health analyst and DevOps expert.

## Current Cluster Status
%s

## Recent Trends
%s

## Identified Anomalies
%s

Generate a brief health summary.`, currentReport, trends, anomalies)
}

func (l *LLMClient) GenerateEnhancedPrompt(metrics, trends, anomalies, smokeTests, status, logContext, podDetails string) string {
	_ = logContext // Included in metrics
	_ = podDetails // Included in metrics

	return fmt.Sprintf(`You are a Kubernetes cluster health analyst. Analyze ONLY the provided data.

CRITICAL RULES:
- ONLY reference metrics explicitly provided below
- Do NOT invent, assume, or hallucinate any data
- Do NOT mention services/components not in the provided metrics
- If data is missing or unclear, say "DATA_NOT_PROVIDED" instead of guessing
- Use EXACT numbers from the data - do not round or estimate

## Provided Cluster Metrics
%s

## Recent Trends (if available)
%s

## Smoke Tests (if available)
%s

## Current Status: %s

## Your Task:
Provide analysis in exactly these 3 sections:

### 1. Executive Summary (2-3 sentences MAX)
Describe the current cluster health status using ONLY the metrics above. Reference specific numbers.

### 2. Critical Issues (if any)
List ONLY issues that are explicitly shown in the metrics. Format: "Issue Name: specific number/detail"
If no issues, write: "No issues identified in provided metrics."

### 3. Recommendations (3 specific actions)
Based ONLY on the issues identified above, suggest 3 concrete actions.
Example format: "Monitor [specific metric] as it is at [number]"

## Validation:
Before responding, verify:
- Every number you mention exists in the provided metrics ✓
- You have not mentioned any service not in the data ✓
- Your statements are supported by the data ✓
- You said "DATA_NOT_PROVIDED" for any missing data ✓`, metrics, trends, smokeTests, status)
}

// GenerateDataAnalysisPrompt creates a prompt for Phase 1: structured data analysis with thresholds
func (l *LLMClient) GenerateDataAnalysisPrompt(metrics string, trends string) string {
	return fmt.Sprintf(`You are a Kubernetes cluster health analyst. Your task is to classify metrics and identify issues.

## Thresholds for Health Classification
- CPU Usage: Good <70%%, Elevated 70-85%%, Critical >85%%
- Memory Usage: Good <75%%, Elevated 75-90%%, Critical >90%%
- Disk Usage: Good <80%%, Elevated 80-95%%, Critical >95%%
- Unschedulable Nodes: Any count >0 is elevated
- Failed Pods: Any count >0 is elevated

## Provided Cluster Metrics
%s

## Recent Trends
%s

## Your Task: Classify Each Metric - USE EXACT THRESHOLDS ONLY
CRITICAL: Apply these EXACT thresholds. Do not deviate. Do not interpret trends.

**MEMORY_USAGE_PERCENT**
- If value is LESS THAN 75: status = "good"
- If value is 75 OR MORE and LESS THAN 90: status = "elevated"
- If value is 90 OR MORE: status = "critical"
- Examples: 25% = GOOD, 75% = elevated, 90% = critical

**CPU_USAGE_PERCENT**
- If value is LESS THAN 70: status = "good"
- If value is 70 OR MORE and LESS THAN 85: status = "elevated"
- If value is 85 OR MORE: status = "critical"
- Examples: 50% = GOOD, 70% = elevated, 85% = critical

**DISK_USAGE_PERCENT**
- If value is LESS THAN 80: status = "good"
- If value is 80 OR MORE and LESS THAN 95: status = "elevated"
- If value is 95 OR MORE: status = "critical"
- Examples: 45% = GOOD, 80% = elevated, 95% = critical

**FAILED_PODS**
- If count is 0: status = "good"
- If count is 1-5: status = "elevated"
- If count is 6+: status = "critical"

Only include metrics in flagged_issues if status is "elevated" or "critical".

Respond with ONLY valid JSON (no markdown, no commentary):
{
  "overall_health": "healthy|degraded|critical",
  "metrics_summary": {
    "cpu_usage_percent": {"value": <number>, "status": "good|elevated|critical"},
    "memory_usage_percent": {"value": <number>, "status": "good|elevated|critical"},
    "disk_usage_percent": {"value": <number>, "status": "good|elevated|critical"},
    "available_memory_gb": {"value": <number>},
    "available_storage_gb": {"value": <number>},
    "nodes_total": {"value": <number>},
    "nodes_ready": {"value": <number>},
    "nodes_unschedulable": {"value": <number>, "status": "good|elevated|critical"},
    "pods_total": {"value": <number>},
    "pods_running": {"value": <number>},
    "pods_failed": {"value": <number>, "status": "good|elevated|critical"},
    "pods_pending": {"value": <number>, "status": "good|elevated|critical"}
  },
  "node_health": [
    {
      "name": "node-name",
      "status": "good|elevated|critical",
      "reason": "brief reason if not good"
    }
  ],
  "flagged_issues": [
    {
      "metric": "metric_name",
      "value": <number>,
      "severity": "elevated|critical",
      "description": "brief description"
    }
  ]
}

Only include flagged_issues if status is elevated or critical. Only include node_health if there are nodes. Return valid JSON only.`, metrics, trends)
}

// GenerateNarrativePrompt creates a prompt for Phase 2: narrative generation based on structured analysis
func (l *LLMClient) GenerateNarrativePrompt(dataAnalysisJSON string, smokeTests string, logContext string) string {
	return fmt.Sprintf(`You are a Kubernetes cluster health analyst. Based on structured analysis, generate a narrative report.

## IMPORTANT INSTRUCTIONS
- Reference ONLY the flagged_issues from the Phase 1 analysis
- If no issues are flagged, cluster is healthy
- Do NOT invent metrics or thresholds
- Do NOT modify severity levels from Phase 1
- If Phase 1 says something is critical, you say it is critical
- If Phase 1 says something is good, do NOT mention it as a problem

## Structured Data Analysis (Phase 1 Results)
%s

## Additional Context

### Smoke Tests
%s

### Recent Logs
%s

## Your Task: Generate Executive Report
Using ONLY the issues flagged in the structured analysis, provide exactly 3 sections.
INCORPORATE smoke test results and log samples when relevant to each section.

### 1. Executive Summary (2-3 sentences)
Summarize cluster health. If issues were flagged, mention them with specific values. Reference smoke test status (passed/failed). Otherwise, state the cluster is operating normally.

### 2. Critical Issues
List ONLY flagged issues with:
- Issue Name
- Current Value (exact number)
- Severity
- Relevant log samples or error context from the logs provided above (if applicable)

If none flagged, write: "No critical issues identified. Smoke tests: [status]"

### 3. Recommendations
Provide 3 specific actions:
- If issues flagged: Focus on resolving them using log context and smoke test failures as guidance
- If no issues: Proactive maintenance suggestions considering smoke test results

Format each recommendation with: "[Action] because [reason from metrics/logs/tests]"`, dataAnalysisJSON, smokeTests, logContext)
}

func (l *LLMClient) IsAvailable(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", l.endpoint+"/api/tags", nil)
	if err != nil {
		return false
	}

	resp, err := l.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// ValidatePhase1Response applies server-side threshold enforcement to Phase 1 LLM output
// This corrects any misclassifications by the LLM and ensures strict threshold rules are applied
func ValidatePhase1Response(jsonStr string) string {
	log.Printf("[VALIDATOR] Starting validation of Phase 1 response (length: %d chars)", len(jsonStr))
	thresholds := DefaultThresholds()

	// Extract first valid JSON object from the response (LLM may return multiple blocks)
	cleanedStr := extractFirstJSON(jsonStr)
	if cleanedStr == "" {
		log.Printf("[VALIDATOR] Could not extract valid JSON from response")
		return jsonStr
	}

	var response struct {
		OverallHealth   string `json:"overall_health"`
		MetricsSummary  map[string]interface{} `json:"metrics_summary"`
		NodeHealth      []map[string]interface{} `json:"node_health"`
		FlaggedIssues   []map[string]interface{} `json:"flagged_issues"`
	}

	if err := json.Unmarshal([]byte(cleanedStr), &response); err != nil {
		log.Printf("[VALIDATOR] Failed to parse Phase 1 response: %v", err)
		return jsonStr // Return unchanged if parse fails
	}

	// Apply threshold corrections to metrics_summary
	if response.MetricsSummary != nil {
		metricsToCorrect := []struct {
			key       string
			valueKey  string
			threshold ResourceThreshold
		}{
			{"cpu_usage_percent", "cpu_usage_percent", thresholds.CPU},
			{"memory_usage_percent", "memory_usage_percent", thresholds.Memory},
			{"disk_usage_percent", "disk_usage_percent", thresholds.Disk},
		}

		for _, m := range metricsToCorrect {
			if metricData, ok := response.MetricsSummary[m.key].(map[string]interface{}); ok {
				if value, ok := metricData["value"].(float64); ok {
					correctStatus := m.threshold.EvaluateStatus(value)
					oldStatus := ""
					if status, ok := metricData["status"].(string); ok {
						oldStatus = status
					}
					metricData["status"] = correctStatus
					if oldStatus != correctStatus {
						log.Printf("[VALIDATOR] %s: %.1f%% - corrected status from '%s' to '%s'", m.key, value, oldStatus, correctStatus)
					}
				}
			}
		}

		// Correct failed_pods status (0=good, 1-5=elevated, 6+=critical)
		if podsFailedData, ok := response.MetricsSummary["pods_failed"].(map[string]interface{}); ok {
			if value, ok := podsFailedData["value"].(float64); ok {
				var correctStatus string
				if value == 0 {
					correctStatus = "good"
				} else if value <= 5 {
					correctStatus = "elevated"
				} else {
					correctStatus = "critical"
				}
				oldStatus := ""
				if status, ok := podsFailedData["status"].(string); ok {
					oldStatus = status
				}
				podsFailedData["status"] = correctStatus
				if oldStatus != correctStatus {
					log.Printf("[VALIDATOR] pods_failed: %.0f - corrected status from '%s' to '%s'", value, oldStatus, correctStatus)
				}
			}
		}

		// Correct pending_pods status (same as failed_pods)
		if podsPendingData, ok := response.MetricsSummary["pods_pending"].(map[string]interface{}); ok {
			if value, ok := podsPendingData["value"].(float64); ok {
				var correctStatus string
				if value == 0 {
					correctStatus = "good"
				} else if value <= 5 {
					correctStatus = "elevated"
				} else {
					correctStatus = "critical"
				}
				oldStatus := ""
				if status, ok := podsPendingData["status"].(string); ok {
					oldStatus = status
				}
				podsPendingData["status"] = correctStatus
				if oldStatus != correctStatus {
					log.Printf("[VALIDATOR] pods_pending: %.0f - corrected status from '%s' to '%s'", value, oldStatus, correctStatus)
				}
			}
		}

		// Correct unschedulable_nodes status (0=good, >0=elevated)
		if nodesUnschedulableData, ok := response.MetricsSummary["nodes_unschedulable"].(map[string]interface{}); ok {
			if value, ok := nodesUnschedulableData["value"].(float64); ok {
				var correctStatus string
				if value == 0 {
					correctStatus = "good"
				} else {
					correctStatus = "elevated"
				}
				oldStatus := ""
				if status, ok := nodesUnschedulableData["status"].(string); ok {
					oldStatus = status
				}
				nodesUnschedulableData["status"] = correctStatus
				if oldStatus != correctStatus {
					log.Printf("[VALIDATOR] nodes_unschedulable: %.0f - corrected status from '%s' to '%s'", value, oldStatus, correctStatus)
				}
			}
		}
	}

	// Re-marshal to JSON
	correctedBytes, err := json.Marshal(response)
	if err != nil {
		log.Printf("[VALIDATOR] Failed to re-marshal corrected response: %v", err)
		return jsonStr
	}

	log.Printf("[VALIDATOR] Phase 1 response validated and corrected")
	return string(correctedBytes)
}

// extractFirstJSON finds and extracts the first valid JSON object from text (handles markdown and multiple blocks)
func extractFirstJSON(text string) string {
	// Find first markdown code fence if present
	text = strings.TrimSpace(text)

	// Try to find and extract markdown-wrapped JSON
	if idx := strings.Index(text, "```"); idx != -1 {
		// Found markdown fence, extract content between fences
		startIdx := idx + 3
		// Skip optional 'json' language marker
		if strings.HasPrefix(text[startIdx:], "json") {
			startIdx += 4
		}
		// Find closing fence
		if endIdx := strings.Index(text[startIdx:], "```"); endIdx != -1 {
			text = text[startIdx : startIdx+endIdx]
		}
	}

	text = strings.TrimSpace(text)

	// Find first '{' character
	braceIdx := strings.IndexByte(text, '{')
	if braceIdx == -1 {
		return ""
	}

	text = text[braceIdx:]

	// Find matching closing brace for the first object
	braceCount := 0
	inString := false
	escaped := false

	for i, ch := range text {
		if escaped {
			escaped = false
			continue
		}

		if ch == '\\' && inString {
			escaped = true
			continue
		}

		if ch == '"' {
			inString = !inString
			continue
		}

		if !inString {
			if ch == '{' {
				braceCount++
			} else if ch == '}' {
				braceCount--
				if braceCount == 0 {
					// Found matching closing brace
					candidate := text[:i+1]

					// Sanitize the JSON: fix unescaped newlines in strings
					candidate = sanitizeJSON(candidate)

					// Verify it's valid JSON
					var test interface{}
					if err := json.Unmarshal([]byte(candidate), &test); err == nil {
						return candidate
					}
				}
			}
		}
	}

	return ""
}

// sanitizeJSON fixes common JSON issues like unescaped newlines in string values
func sanitizeJSON(text string) string {
	// Replace actual newlines and tabs within quoted strings with escaped versions
	// This is a simple fix for LLM-generated JSON with literal whitespace in strings
	var result strings.Builder
	inString := false
	escaped := false

	for _, ch := range text {
		if escaped {
			result.WriteRune(ch)
			escaped = false
			continue
		}

		if ch == '\\' && inString {
			result.WriteRune(ch)
			escaped = true
			continue
		}

		if ch == '"' {
			result.WriteRune(ch)
			inString = !inString
			continue
		}

		if inString && (ch == '\n' || ch == '\r') {
			// Skip literal newlines/carriage returns in strings
			// (they're invalid JSON)
			continue
		}

		if inString && ch == '\t' {
			// Replace tabs with spaces in strings
			result.WriteRune(' ')
			continue
		}

		result.WriteRune(ch)
	}

	return result.String()
}
