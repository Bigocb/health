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
func (l *LLMClient) GenerateDataAnalysisPrompt(metrics string, trends string) string {
	return fmt.Sprintf(`You are a Kubernetes cluster health analyst. Your task is to classify metrics and identify issues.

## Thresholds for Health Classification
- CPU Usage: Good <70%%, Elevated 70-85%%, Critical >85%%
- Memory Usage: Good <75%%, Elevated 75-90%%, Critical >90%%
- Disk Usage: Good <80%%, Elevated 80-95%%, Critical >95%%
- Unschedulable Nodes: Any count >0 is elevated
- Failed Pods: 0=good, 1-5=elevated, 6+=critical

## Provided Cluster Metrics
%s

## Recent Trends
%s

## Your Analysis Task: Go Deep, Connect the Data

IMPORTANT: Use ALL provided data to build comprehensive context:
1. **Per-Node Analysis**: For each node in "Per-Node Metrics", extract:
   - Node name, Ready status, Schedulable status
   - CPU usage %%, Memory usage %%, Available memory
   - Pod count on that node
   - Apply thresholds to each node's metrics individually
   - Flag correlations (e.g., unschedulable node with high CPU/memory)

2. **Failed Pods Analysis**: Examine each failed pod:
   - Which namespace and node is it on?
   - What's the failure reason?
   - Pattern analysis: Are failed pods concentrated on specific nodes?

3. **Cluster Context**: Build a picture of overall health from:
   - Aggregate metrics (cluster-wide CPU, Memory, Disk)
   - Per-node distribution (are resources imbalanced?)
   - Failed pod patterns (random? concentrated on one node?)
   - Unschedulable nodes (why are they unschedulable?)

4. **Correlations**: Identify relationships:
   - Unschedulable node + High CPU/Memory = Resource pressure
   - Failed pods on same node = Node health issue
   - Imbalanced resource distribution = Scheduling problem

## Your Task: Classify Each Metric Using EXACT THRESHOLDS
CRITICAL: Apply these EXACT thresholds. Do not deviate. Do not interpret trends.

**Memory Usage Classification:**
- If value < 75: status = good
- If value >= 75 and < 90: status = elevated
- If value >= 90: status = critical
- Example: 27%% = good, 75%% = elevated, 90%% = critical

**CPU Usage Classification:**
- If value < 70: status = good
- If value >= 70 and < 85: status = elevated
- If value >= 85: status = critical
- Example: 50%% = good, 70%% = elevated, 85%% = critical

**Disk Usage Classification:**
- If value < 80: status = good
- If value >= 80 and < 95: status = elevated
- If value >= 95: status = critical
- Example: 45%% = good, 80%% = elevated, 95%% = critical

## CRITICAL: RESPONSE FORMAT
DO NOT OUTPUT JSON.
DO NOT OUTPUT CURLY BRACES.
DO NOT OUTPUT SQUARE BRACKETS WITH KEY-VALUE PAIRS.

Output ONLY plain markdown text using this EXACT format:

### Overall Health
degraded

### Metrics Summary
- CPU Usage: 19.0%% → **good**
- Memory Usage: 25.0%% → **good**
- Disk Usage: 45.0%% → **good**
- Available Memory: 121 GB
- Available Storage: 637 GB
- Nodes Total: 2 (Ready: 2, Unschedulable: 1)
- Pods Total: 147 (Running: 147, Failed: 7, Pending: 0)

### Per-Node Health
Include ALL nodes with their detailed metrics and status:
- nodename: CPU=XX.X%% → **status**, Memory=XX.X%% → **status**, Available=XXX.XGB, Pods=XX (Ready/Unschedulable/NotReady)
- Example: app01: CPU=65.0%% → **elevated**, Memory=27.0%% → **good**, Available=3.0GB, Pods=7 (Unschedulable)

### Flagged Issues
- pods_failed: 7 → **critical** (Reason: failures concentrated on specific nodes or random distribution)
- nodes_unschedulable: 1 → **elevated** (Node app01 - reason: high CPU/memory pressure or maintenance)

Rules:
- Only output markdown text
- Start with ### Overall Health
- Include ### Metrics Summary with each metric value
- Only include ### Node Health if there are nodes
- Only include ### Flagged Issues if there are elevated or critical items
- Use this exact line format for metrics: "- Name: {number} → **{status}**"
- NO JSON, NO BRACES, NO BRACKETS - ONLY MARKDOWN TEXT`, metrics, trends)
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
			// Pattern: "- Metric Name: {value}{suffix} → **{status}**"
			idx := strings.Index(line, metricLabel)
			if idx == -1 {
				continue
			}
			afterLabel := line[idx+len(metricLabel):]

			// Find the number in the afterLabel (e.g., " 16.0% →")
			// Look for digits (possibly with decimal point)
			re := regexp.MustCompile(`\s*([\d.]+)` + regexp.QuoteMeta(valueSuffix) + `\s*→`)
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
			// Find pattern "→ **old_status**" and replace with "→ **correct_status**"
			statusRe := regexp.MustCompile(`→\s*\*\*([a-z]+)\*\*`)
			oldMatches := statusRe.FindStringSubmatchIndex(line)
			if oldMatches != nil {
				oldStatus := line[oldMatches[2]:oldMatches[3]]
				if oldStatus != correctStatus {
					line = statusRe.ReplaceAllString(line, fmt.Sprintf("→ **%s**", correctStatus))
					lines[i] = line
					log.Printf("[VALIDATOR] %s: %.1f%s - corrected from '%s' to '%s'", metricLabel, value, valueSuffix, oldStatus, correctStatus)
					return strings.Join(lines, "\n"), true
				}
			}
		}
	}
	return text, false
}
