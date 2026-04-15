# LLM Integration Summary

## What Was Integrated

The health-reporter now has **Smart Path LLM inference** integrated into the report generation pipeline. This follows the hybrid architecture:

- **Happy Path (Always Active)**: Pure deterministic classification, no LLM calls
- **Smart Path (Conditional)**: LLM analysis only when cluster is degraded/critical

### Key Changes

#### 1. **health.go - Report Generation** (Lines 340-362)
- Added conditional LLM invocation after report status calculation
- **Trigger**: `if report.Status != types.StatusHealthy && r.llmClient != nil`
- **Behavior**:
  - Calls `GenerateExecutiveSummaryPrompt()` to build structured prompt from report metrics
  - Invokes `llmClient.Analyze()` with 6-minute timeout
  - Appends LLM response to report summary section
  - Gracefully handles LLM failures (non-blocking)
  - Logs all activity for debugging

#### 2. **llm_prompt.go - Prompt Generation** (Already Implemented)
- `GenerateExecutiveSummaryPrompt()` creates detailed structured prompt including:
  - Cluster metrics (CPU, Memory, Disk usage with thresholds)
  - Per-node status (CPU %, Memory %, Available Disk)
  - Pod capacity breakdown (running, pending, failed)
  - Smoke test results (pass/fail counts)
  - Trend analysis placeholder
  - Specific instructions for LLM output format

#### 3. **Configuration** (values.yaml)
```yaml
analysis:
  llm:
    enabled: true
    provider: "ollama"
    model: "orca-mini"  # ~2.0 GB, optimized for summaries
    endpoint: "http://ollama.ollama.svc.cluster.local:11434"
    timeoutSeconds: 360  # 6 minutes
    maxRetries: 1
    temperature: 0.2
    maxTokens: 2048
```

#### 4. **LLMClient** (llm.go - Already Implemented)
- `NewLLMClient()` initializes with endpoint, model, timeout
- `Analyze()` method:
  - Makes HTTP POST to `{endpoint}/api/generate`
  - Formats request as Ollama API compatible
  - Returns response text or error
  - Includes retry logic with exponential backoff
  - Fallback to `llama3.2:1b` if model not found

### Execution Flow

```
Report Generation
├─ Gather Metrics (Mimir queries)
├─ Build Report Structure
├─ Calculate Status (deterministic)
│
└─ If Status == HEALTHY
│  └─ Done (return report)
│
└─ If Status == DEGRADED or CRITICAL
   ├─ Generate Prompt (structured data)
   ├─ Call Ollama API (orca-mini model)
   ├─ Get LLM Analysis (root cause insights)
   ├─ Append to Report Summary
   └─ Return Enhanced Report
```

### What Happens When Cluster is HEALTHY
- No LLM calls (saves time + resources)
- Report returned in ~100ms
- Log: `[Report] Happy Path: Cluster is healthy, skipping LLM inference`

### What Happens When Cluster is DEGRADED or CRITICAL
- LLM invoked with full prompt (~30-60 seconds)
- Analysis appended to report summary
- Log: `[Report] Cluster status is degraded/critical, invoking LLM for root cause analysis`
- Failure handling: If LLM times out or fails, report still sent without LLM section

## Testing the Integration

### 1. Verify Configuration
```bash
kubectl get configmap health-reporter -n monitoring -o yaml | grep -A 20 "analysis:"
```

Should show `llm.enabled: true` and `llm.model: orca-mini`

### 2. Verify Ollama is Running
```bash
kubectl get pod -n ollama -o wide
kubectl exec -it <ollama-pod> -n ollama -- ollama list
```

Should show `orca-mini:latest 2.0 GB`

### 3. Test Network Connectivity
```bash
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl http://ollama.ollama.svc.cluster.local:11434/api/tags
```

Should return JSON with available models

### 4. Trigger Degraded Cluster State (to test LLM)
Create a failing pod or reduce available resources to force `degraded` status, then generate a report

### 5. Check Logs
```bash
kubectl logs -f deployment/health-reporter -n monitoring | grep -E "\[Report\]|\[LLM\]"
```

Look for:
- `[Report] Happy Path: Cluster is healthy...` (when healthy)
- `[Report] Cluster status is degraded, invoking LLM...` (when degraded)
- `[LLM] Analyzing prompt...` (LLM calls)
- `[LLM] Response received...` (successful responses)

## Performance Expectations

| Scenario | Latency | LLM Calls |
|----------|---------|-----------|
| Healthy Cluster | ~100ms | 0 |
| Degraded Cluster | ~30-60s | 1 (orca-mini analysis) |
| Critical Cluster | ~30-60s | 1 (orca-mini analysis) |

The 30-60s is dominated by Ollama inference time, not network latency.

## Failure Modes & Resilience

| Failure | Behavior |
|---------|----------|
| Ollama endpoint unreachable | Report sent without LLM section; logged as non-blocking error |
| Ollama timeout (360s exceeded) | Report sent without LLM section; context cancelled |
| Invalid model name | Fallback to `llama3.2:1b` (hardcoded fallback) |
| LLM returns empty response | Report sent without LLM section |
| Mimir metrics failed | LLM still invoked if status != healthy (uses cache or defaults) |

## Next Steps

1. **Rebuild & Deploy**
   ```bash
   git add -A && git commit -m "Integrate LLM inference for degraded/critical cluster analysis"
   git push  # Triggers GitHub Actions to rebuild image
   kubectl rollout restart deployment/health-reporter -n monitoring
   ```

2. **Verify Reports Include LLM Analysis**
   - Check Discord webhook for reports with "## LLM Analysis:" section
   - Look for specific root cause insights in the summary

3. **Monitor Performance**
   - Use Grafana dashboard to track report generation times
   - Check logs for LLM call success rates
   - Monitor Ollama resource usage

4. **Optimize as Needed**
   - If orca-mini output quality is poor, consider larger model (Phase 2)
   - If LLM calls are too slow, consider async analysis
   - Fine-tune temperature (currently 0.2) for more/less creative output

## Files Modified

- `pkg/health/health.go` - Added LLM invocation logic
- No changes needed to:
  - `pkg/analysis/llm.go` - Already complete
  - `pkg/analysis/llm_prompt.go` - Already complete
  - `helm/health-reporter/values.yaml` - Already configured
  - `pkg/types/types.go` - Already has needed structures

## Architecture Decision Rationale

**Why Smart Path Only?**
- Happy path (healthy clusters) doesn't need LLM analysis
- Saves 30-60s per report cycle for healthy deployments
- Reduces load on Ollama for stable clusters
- LLM is expensive; only use when there's a problem to diagnose

**Why Orca-Mini?**
- 2.0 GB (fits on internal node with room for growth)
- Fast inference (30-60s for structured analysis)
- Good enough for executive summaries
- Phase 2: Can add larger model (Mistral 7B) later for deeper root cause analysis
