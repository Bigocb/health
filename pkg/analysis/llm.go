package analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
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
// Now includes log context for deep correlation analysis
func (l *LLMClient) GenerateDataAnalysisPrompt(metrics string, trends string, logContext string) string {
	return fmt.Sprintf(`You are a Kubernetes cluster health analyst. Analyze logs to EXPLAIN the cluster metrics below.

## Metric Classifications (Already Done - Server-Side)
These metrics are already classified. DO NOT change them. Analyze logs to explain WHY they are at these levels.

%s

## Recent Trends
%s

## Log Context
%s

## Your Task: Explain Metrics Using Logs

Focus ONLY on analyzing logs:
1. For EACH elevated or critical metric: identify which logs explain it
2. For EACH issue in the Flagged Issues section: cite specific log entries that support it
3. Look for error patterns, pod crashes, and resource constraints
4. Explain the correlation between metrics and log evidence

Example:
- If metrics show "CPU: elevated", explain: "Logs show OOMKilled errors on pod-X, causing CPU throttling"
- If metrics show "Memory: good despite OOMKilled in logs", explain: "OOMKilled error is isolated to 1 pod; overall memory is healthy"

## Output Format
Plain markdown text only.

### Log Analysis & Root Causes
For each flagged/elevated issue, provide:
- The specific metric/issue
- Which log entries support this classification
- Any error patterns or trends
- No log evidence if none found

### Additional Insights
- Any emerging problems visible in logs
- Patterns that could lead to future issues`, metrics, trends, logContext)
}

// GenerateNarrativePrompt creates a prompt for Phase 2: narrative generation based on structured analysis
func (l *LLMClient) GenerateNarrativePrompt(dataAnalysisJSON string, smokeTests string, logContext string) string {
	return fmt.Sprintf(`You are a Kubernetes cluster health analyst. Based on deep analysis including logs, generate a comprehensive narrative report.

## CRITICAL INSTRUCTIONS
- **FIRST: Extract Phase 1's health status from line: "### Overall Health: [healthy/degraded/critical]"**
- **USE that status directly in your Executive Summary - do NOT change it**
- Do NOT make your own health classification - use Phase 1's determination
- DEEPLY INCORPORATE provided logs and error context
- Research log patterns to explain WHY issues occur
- Do NOT invent metrics or thresholds
- Reference actual error messages and patterns from logs
- Connect metrics to log evidence for credibility

## Phase 1 Structured Analysis (Includes Log Context)
%s

## Smoke Tests Status
%s

## Log Context (For Root Cause Explanation)
%s

## Your Task: Generate Executive Report with Deep Analysis

**CRITICAL: Use Phase 1's overall health determination. Do NOT make your own health classification.**

You MUST reference logs and explain root causes based on error patterns.

### 1. Executive Summary (2-3 sentences, with log evidence)
Start with: "The cluster is currently in a **[HEALTH_STATUS]** state" where [HEALTH_STATUS] is the status from Phase 1 analysis.
Then add:
- If issues flagged: Mention specific values, reference log evidence, include smoke test impact
- If healthy: "All smoke tests passing. Cluster operating normally."
DO NOT override Phase 1's health determination.

### 2. Critical Issues (Deep Analysis Required)
FIRST: Check Phase 1's "### Flagged Issues" section.
- If Phase 1 flagged issues: For EACH flagged issue from Phase 1:
  - Issue Name and Value (from Phase 1)
  - Severity (from Phase 1)
  - **Root Cause Analysis** (MUST use logs):
    - What error messages appear in logs?
    - Are these application crashes, resource constraints, or infrastructure issues?
    - Do errors concentrate on specific pods/nodes?
    - What is the timing pattern (continuous, intermittent, spikes)?
  - Impact: How does this affect cluster/services?
- If Phase 1 flagged NO issues:
  "No critical issues identified. All smoke tests passing. Cluster operating normally."

### 3. Recommendations
Provide 3-4 specific, actionable recommendations:
- Based on log error patterns and root causes
- Reference specific log entries or error types
- If smoke tests failed, explain how to resolve
- If pod crashes: check application logs for panic/error
- If resource issues: provide capacity planning guidance

Format each recommendation:
"[Action] because [specific reason from logs/metrics/tests]"
Example: "Review application logs for OOMKilled pods because Memory Usage is 92% and logs show memory allocation failures"

## Output Format
Plain text, NOT JSON. Include all sections.`, dataAnalysisJSON, smokeTests, logContext)
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

// ValidatePhase1Response applies server-side threshold enforcement to Phase 1 LLM output (markdown format)
// This corrects any misclassifications by the LLM and ensures strict threshold rules are applied
func ValidatePhase1Response(markdownStr string) string {
	log.Printf("[VALIDATOR] Starting validation of Phase 1 response (length: %d chars)", len(markdownStr))
	thresholds := DefaultThresholds()

	return correctMarkdownStatuses(markdownStr, thresholds)
}

// correctMarkdownStatuses finds metric statuses in markdown and corrects them using thresholds
func correctMarkdownStatuses(markdown string, thresholds HealthThresholds) string {
	result := markdown
	correctedCount := 0

	// Pattern: "Metric Name: {value}% → **{status}**"
	// Find all metric lines and correct the status

	// CPU Usage line
	result, corrected := correctMetricStatus(result, "CPU Usage:", "%", func(value float64) string {
		return thresholds.CPU.EvaluateStatus(value)
	})
	if corrected {
		correctedCount++
	}

	// Memory Usage line
	result, corrected = correctMetricStatus(result, "Memory Usage:", "%", func(value float64) string {
		return thresholds.Memory.EvaluateStatus(value)
	})
	if corrected {
		correctedCount++
	}

	// Disk Usage line
	result, corrected = correctMetricStatus(result, "Disk Usage:", "%", func(value float64) string {
		return thresholds.Disk.EvaluateStatus(value)
	})
	if corrected {
		correctedCount++
	}

	// Failed Pods count
	result, corrected = correctMetricStatus(result, "Failed:", "", func(value float64) string {
		if value == 0 {
			return "good"
		} else if value <= 5 {
			return "elevated"
		}
		return "critical"
	})
	if corrected {
		correctedCount++
	}

	if correctedCount > 0 {
		log.Printf("[VALIDATOR] Corrected %d metric statuses in markdown", correctedCount)
	}
	return result
}

// correctMetricStatus finds a metric line, extracts value, applies threshold, and corrects status
func correctMetricStatus(text, metricLabel, valueSuffix string, evaluateFunc func(float64) string) (string, bool) {
	// Find the line containing the metric
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.Contains(line, metricLabel) {
			// Extract the numeric value from this line
			// Pattern: "- Metric Name: {value}{suffix} [{status}]"
			idx := strings.Index(line, metricLabel)
			if idx == -1 {
				continue
			}
			afterLabel := line[idx+len(metricLabel):]

			// Find the number in the afterLabel (e.g., " 16.0% [")
			// Look for digits (possibly with decimal point)
			re := regexp.MustCompile(`\s*([\d.]+)` + regexp.QuoteMeta(valueSuffix) + `\s*\[`)
			matches := re.FindStringSubmatch(afterLabel)
			if len(matches) < 2 {
				log.Printf("[VALIDATOR] DEBUG: Could not extract value from line: %s", line)
				continue
			}

			valueStr := matches[1]
			value, err := strconv.ParseFloat(valueStr, 64)
			if err != nil {
				log.Printf("[VALIDATOR] DEBUG: Could not parse value '%s': %v", valueStr, err)
				continue
			}

			// Get the correct status
			correctStatus := evaluateFunc(value)

			// Replace the old status with the correct one in this line
			// Find pattern "[old_status]" and replace with "[correct_status]"
			statusRe := regexp.MustCompile(`\[([a-z]+)\]`)
			oldMatches := statusRe.FindStringSubmatchIndex(line)
			if oldMatches != nil {
				oldStatus := line[oldMatches[2]:oldMatches[3]]
				if oldStatus != correctStatus {
					line = statusRe.ReplaceAllString(line, fmt.Sprintf("[%s]", correctStatus))
					lines[i] = line
					log.Printf("[VALIDATOR] %s: %.1f%s - corrected from '%s' to '%s'", metricLabel, value, valueSuffix, oldStatus, correctStatus)
					return strings.Join(lines, "\n"), true
				}
			}
		}
	}
	return text, false
}
