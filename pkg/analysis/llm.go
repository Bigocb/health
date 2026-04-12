package analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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

## Your Task: Classify Each Metric
STRICTLY use these thresholds (do not interpret, do not estimate):
1. Memory: If value < 75 THEN good, IF 75 <= value < 90 THEN elevated, IF value >= 90 THEN critical
2. CPU: If value < 70 THEN good, IF 70 <= value < 85 THEN elevated, IF value >= 85 THEN critical
3. Disk: If value < 80 THEN good, IF 80 <= value < 95 THEN elevated, IF value >= 95 THEN critical
4. Failed Pods: If value > 0 THEN elevated, If value > 5 THEN critical
5. Pending Pods: If value > 0 THEN elevated

Only include metrics in flagged_issues if they are elevated or critical. Include the exact numeric threshold comparison.

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
Using ONLY the issues flagged in the structured analysis, provide exactly 3 sections:

### 1. Executive Summary (2-3 sentences)
Summarize cluster health. If issues were flagged, mention them with specific values. Otherwise, state the cluster is operating normally.

### 2. Critical Issues
List ONLY flagged issues with:
- Issue Name
- Current Value (exact number)
- Severity

If none flagged, write: "No critical issues identified."

### 3. Recommendations
Provide 3 specific actions:
- If issues flagged: Focus on resolving them
- If no issues: Proactive maintenance suggestions

Format: "Monitor [specific metric] at [value] because [reason]"`, dataAnalysisJSON, smokeTests, logContext)
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
