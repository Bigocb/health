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

	return fmt.Sprintf(`You are a Kubernetes cluster health analyst and DevOps expert. Your task is to generate a VERY DETAILED and COMPREHENSIVE health report.

## CRITICAL: Output Requirements
- Write EXTENSIVE content for every section (minimum 150 words per section)
- NEVER use placeholder text like "[details]" or "[provide...]"
- ALWAYS include specific numbers and details from the data provided
- Use bullet points and lists extensively
- Make each section actionable and informative

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

## Required Report Format

### 🎯 Executive Summary (MINIMUM 200 WORDS)
Write a thorough 3-4 paragraph overview that includes:
- Current cluster state summary
- Key metrics (CPU, Memory, Pods, Nodes)
- Primary concerns and their impact
- Overall health assessment

### 📊 Health Score (MINIMUM 100 WORDS)
Provide:
- Your determination: EXCELLENT / GOOD / DEGRADED / CRITICAL
- Detailed justification with specific metrics
- Comparison to previous trends if available

### 📈 Key Metrics Breakdown (MINIMUM 300 WORDS)
For EACH of the following, provide detailed numbers AND interpretation:
- Nodes: Total, Ready, NotReady, Unschedulable, CPU cores, Memory
- Pods: Running, Pending, Failed, Succeeded, Restarts (1h)
- CPU: Usage percentage and whether it's healthy
- Memory: Usage percentage and whether it's healthy
- Disk: Usage percentage
- Deployments: Ready vs Total, any unavailable
- Jobs: Active, Failed, Succeeded counts
- Services: ClusterIP, Headless, LoadBalancer counts
- PVCs: Bound, Pending, Lost

### 🚨 Issues & Alerts (MINIMUM 200 WORDS)
List EVERY issue found with:
- Specific severity (critical/high/medium/low)
- Exact count of affected resources
- Why this is a problem
- Potential root cause

### 🔴 Failed Pods (MINIMUM 150 WORDS)
- List each failed pod name and namespace if available
- Explain what "failed" means for this cluster
- Provide specific kubectl commands to investigate:
  - kubectl get pods -A --field-selector=status.phase=Failed
  - kubectl describe pod <pod-name> -n <namespace>
  - kubectl logs <pod-name> -n <namespace> --previous

### ⏳ Pending Pods (MINIMUM 100 WORDS)
- Count of pending pods
- Common causes (resource constraints, scheduling, PVC issues)
- Commands to investigate:
  - kubectl get pods -A --field-selector=status.phase=Pending

### ✅ Smoke Tests Summary (MINIMUM 150 WORDS)
- Total tests run, pass count, fail count
- List each test that failed with error message
- List each test that passed
- Implications of any failures

### 📉 Trend Analysis (MINIMUM 150 WORDS)
For EACH metric (CPU, Memory):
- Direction: increasing/decreasing/stable
- Percentage change
- What this trend means for cluster health

### 🛠️ Diagnostic Commands (MINIMUM 100 WORDS)
Provide these exact commands users can run:
- kubectl get pods -A --field-selector=status.phase=Failed
- kubectl get pods -A --field-selector=status.phase=Pending
- kubectl top nodes
- kubectl top pods -A
- kubectl describe node (node-name)
- kubectl get events --sort-by='.lastTimestamp' | tail -50

### 🔧 Recommendations (MINIMUM 200 WORDS)
Provide NUMBERED list of 5-10 specific actions with:
- What to do
- Why it matters
- How to do it (command)

### ⚠️ Risk Outlook (MINIMUM 100 WORDS)
- 24-48 hour prediction
- What could go wrong
- Early warning signs to watch for

Now generate your detailed report:`, metrics, trends, anomalies, smokeTests, status, logSection, podSection)
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
