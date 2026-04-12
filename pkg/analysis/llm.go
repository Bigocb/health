package analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type LLMResponse struct {
	Response string `json:"response"`
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
	if cached, ok := l.promptCache[prompt]; ok {
		return cached, nil
	}

	var lastErr error
	for i := 0; i < l.maxRetries; i++ {
		resp, err := l.callAPI(ctx, prompt)
		if err == nil {
			l.promptCache[prompt] = resp
			return resp, nil
		}
		lastErr = err
		time.Sleep(time.Duration(i+1) * time.Second)
	}

	return "", fmt.Errorf("LLM request failed after %d retries: %w", l.maxRetries, lastErr)
}

func (l *LLMClient) callAPI(ctx context.Context, prompt string) (string, error) {
	url := fmt.Sprintf("%s/api/generate", l.endpoint)

	reqBody := LLMRequest{
		Model:  l.model,
		Prompt: prompt,
		Stream: false,
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

	resp, err := l.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("LLM returned status %d: %s", resp.StatusCode, string(body))
	}

	var llmResp LLMResponse
	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return llmResp.Response, nil
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
	logSection := ""
	if logContext != "" {
		logSection = fmt.Sprintf("\n## Log Context\n%s", logContext)
	}

	podSection := ""
	if podDetails != "" {
		podSection = fmt.Sprintf("\n## Pod Details\n%s", podDetails)
	}

	return fmt.Sprintf(`You are a Kubernetes cluster health analyst and DevOps expert. Generate a comprehensive health report.

## Cluster Metrics (JSON)
%s

## Trends (JSON)
%s

## Anomalies (JSON)
%s

## Smoke Test Results (JSON)
%s

## Overall Status: %s
%s%s

## Your Task
Generate a detailed cluster health report. Write at least 3-4 sentences for EACH section. Use this exact format:

### 🎯 Executive Summary
[Detailed 3-4 sentence overview of cluster state]

### 📊 Health Score
[EXCELLENT/GOOD/DEGRADED/CRITICAL] - [one line justification]

### 📈 Key Metrics Breakdown
- Nodes: [details]
- Pods: [details with running/pending/failed counts]
- CPU: [usage and context]
- Memory: [usage and context]
- Disk: [usage]
- Deployments: [ready/total]
- Jobs: [status breakdown]
- Services: [counts]
- PVCs: [status]

### 🚨 Issues & Alerts
[List each issue with severity: high/medium/low and explanation - use log context to identify root causes]

### 🔴 Failed Pods
[If there are failed pods, list their names and namespaces, and suggest diagnostic commands]

### ⏳ Pending Pods
[If there are pending pods, list their names and namespaces, and suggest causes (resources, scheduling, etc.)]

### ✅ Smoke Tests Summary
[Pass/fail counts with any failures highlighted]

### 📉 Trend Analysis
[What the trends show - increasing/decreasing/stable for each metric]

### 🛠️ Diagnostic Commands
[Provide specific kubectl commands to investigate issues like:
- kubectl get pods -A --field-selector=status.phase=Failed
- kubectl describe pod <pod-name> -n <namespace>
- kubectl logs <pod-name> -n <namespace> --previous]

### 🔧 Recommendations
Provide numbered list of 5 specific actions to improve cluster health

### ⚠️ Risk Outlook
[24-48 hour prediction based on trends]

IMPORTANT: Write substantial content for each section. Do not use placeholder text.`, metrics, trends, anomalies, smokeTests, status, logSection, podSection)
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
