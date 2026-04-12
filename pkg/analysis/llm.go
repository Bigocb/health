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

func (l *LLMClient) GenerateEnhancedPrompt(metrics, trends, anomalies, smokeTests, status string) string {
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

## Your Task
Generate a comprehensive cluster health report in markdown format. Include:

### 🎯 Executive Summary
2-3 sentences overview.

### 📊 Health Score
EXCELLENT / GOOD / DEGRADED / CRITICAL

### 📈 Key Metrics
- Nodes (ready/total/unschedulable)
- Pods (running/pending/failed/restarts)
- CPU, Memory, Disk usage
- Deployments (ready/unready)
- Jobs (active/failed/succeeded)
- Services (total/clusterIP/loadbalancer)
- Storage PVCs (bound/pending/lost)

### 🔍 Issues Found
List with severity badge.

### ✅ Smoke Tests
Pass/fail summary by type.

### 📉 Trends
Any significant changes.

### 💡 Recommendations
3-5 prioritized actions.

### ⚠️ Risk Outlook
24-48 hour prediction.

Format: Clean emoji-rich markdown for Discord.`, metrics, trends, anomalies, smokeTests, status)
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
