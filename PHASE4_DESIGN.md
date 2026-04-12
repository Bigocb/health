# Phase 4 Design: Agent Skill + LLM Integration with Historical Analysis

## Overview

**Objective**: Implement intelligent cluster health analysis using an OpenCode agent skill that:
1. Collects current metrics and historical data (last N hourly reports)
2. Detects trends and patterns in cluster behavior
3. Provides AI-powered analysis and recommendations
4. Executes on the hourly CronJob schedule
5. Enriches Discord reports with actionable insights

**Architecture**: Decoupled components allowing:
- Health Reporter collects metrics and saves historical data
- Agent Skill runs separately (can be called by different systems)
- LLM analysis is optional/async (can fail without breaking health reporting)
- Historical data accessible for offline analysis

---

## Phase 4 Components

### 1. Historical Data Storage

**What**: Save hourly reports to persistent storage for trend analysis

**Format**: JSON-serialized reports stored with timestamp
```
health-reporter/
├── reports/
│   └── history/
│       ├── 2026-04-12T00-00-00Z.json   # Hourly snapshots
│       ├── 2026-04-12T01-00-00Z.json
│       ├── 2026-04-12T02-00-00Z.json
│       └── ...
```

**Storage Options**:
1. **Kubernetes ConfigMap/Secret** (current deployment)
   - Pros: Native K8s, no external storage needed
   - Cons: Limited to ~1MB, ConfigMaps not ideal for time-series
   
2. **Kubernetes PersistentVolume** (recommended for Phase 4)
   - Pros: Unlimited capacity, designed for this use case
   - Cons: Requires PV provisioning
   
3. **PostgreSQL/TimescaleDB** (future enhancement)
   - Pros: Proper time-series DB, queryable
   - Cons: Adds external dependency
   
4. **S3/MinIO** (alternative)
   - Pros: Object storage, scalable
   - Cons: External dependency

**Recommendation for Phase 4**: Use Kubernetes PersistentVolume initially
- Mount at `/var/lib/health-reporter/reports/`
- Store last 24-168 hours (1-7 days) of reports
- JSON files named by ISO8601 timestamp
- Auto-cleanup of reports older than retention period

**Data Structure (stored JSON)**:
```json
{
  "timestamp": "2026-04-12T00:00:00Z",
  "status": "degraded",
  "summary": "⚠️ DEGRADED: Cluster has concerns - 17 pod restarts (1h), 0 pending pods, CPU: 17%, Memory: 23%",
  "metrics": {
    "nodes": {
      "total": 2,
      "ready": 2,
      "not_ready": 0
    },
    "pods": {
      "total": 166,
      "running": 166,
      "pending": 0,
      "failed": 10,
      "restarts": 17
    },
    "resources": {
      "cpu_usage_percent": 17.0,
      "memory_usage_percent": 23.0,
      "disk_usage_percent": 40.0,
      "available_memory_gb": 123.0
    }
  },
  "concerns": [
    {
      "title": "Failed Pods",
      "severity": "high",
      "details": "10 pod(s) in failed state"
    },
    {
      "title": "Pod Restarts",
      "severity": "medium",
      "details": "17 pod restarts in last hour"
    }
  ]
}
```

---

### 2. Agent Skill for Cluster Analysis

**What**: An OpenCode agent skill that performs trend analysis and LLM-based insights

**Location**: `.agents/skills/health-analysis/`

**Capabilities**:
- Load historical reports from file system
- Calculate trends (CPU trending up/down, pod restarts increasing, etc.)
- Generate anomaly detection alerts
- Call LLM for intelligent analysis
- Format output for Discord or other channels

**Skill Invocation**:
```bash
opencode skill run health-analysis --context-file /path/to/context.json
```

**Input Context Format** (`context.json`):
```json
{
  "cluster_name": "microk8s-prod",
  "current_report": {
    "timestamp": "2026-04-12T01:00:00Z",
    "status": "degraded",
    "metrics": { /* current metrics */ },
    "concerns": [ /* current concerns */ ]
  },
  "historical_reports": [
    {
      "timestamp": "2026-04-12T00:00:00Z",
      "metrics": { /* previous metrics */ }
    },
    // ... more historical reports (last 24-48 hours)
  ],
  "analysis_config": {
    "trend_window_hours": 24,
    "anomaly_threshold": 1.5,
    "include_recommendations": true
  }
}
```

**Output Format** (`analysis_output.json`):
```json
{
  "timestamp": "2026-04-12T01:00:00Z",
  "analysis": {
    "trends": {
      "cpu_trend": {
        "direction": "increasing",
        "change_percent": 15.2,
        "description": "CPU usage increased 15.2% over last 6 hours"
      },
      "pod_restarts_trend": {
        "direction": "stable",
        "change_percent": 0.0,
        "description": "Pod restarts remain consistent (17 per hour)"
      },
      "memory_trend": {
        "direction": "stable",
        "change_percent": 2.1,
        "description": "Memory usage stable around 23%"
      }
    },
    "anomalies": [
      {
        "type": "pod_restart_spike",
        "severity": "medium",
        "description": "Pod restarts elevated compared to baseline (17 vs avg 8)",
        "confidence": 0.87
      }
    ],
    "predictions": [
      {
        "type": "resource_saturation",
        "risk_level": "low",
        "estimated_hours_to_saturation": 72,
        "description": "At current CPU trend, cluster will reach 90% CPU in ~3 days"
      }
    ],
    "recommendations": [
      {
        "priority": "high",
        "category": "pod_health",
        "action": "Investigate pod restart causes",
        "rationale": "Pod restarts above baseline; investigate app errors or resource limits",
        "steps": [
          "Check pod logs: kubectl logs <failing-pod>",
          "Review resource limits vs actual usage",
          "Check for CrashLoopBackOff patterns"
        ]
      },
      {
        "priority": "medium",
        "category": "capacity_planning",
        "action": "Monitor CPU trend",
        "rationale": "CPU increasing; ensure adequate headroom for peak loads",
        "steps": [
          "Enable cluster autoscaling if not already enabled",
          "Review workload scheduling policies",
          "Consider pod resource requests optimization"
        ]
      }
    ],
    "health_summary": "Cluster is degraded due to elevated pod restarts. Monitor CPU trend (15% increase over 6h). No immediate saturation risk, but pod health requires investigation.",
    "agent_version": "1.0.0",
    "llm_model": "ollama:llama2-7b",
    "confidence_score": 0.92
  }
}
```

---

### 3. Trend Detection Engine

**Metrics to Track** (per hourly report):

1. **Resource Trends**:
   - CPU usage (direction, velocity, acceleration)
   - Memory usage (direction, velocity, acceleration)
   - Disk usage (direction, velocity, acceleration)
   - Available memory (decreasing = concerning)

2. **Pod Health Trends**:
   - Running pods (should be stable)
   - Failed pods (should be 0 or low)
   - Pod restarts/hour (baseline vs current)
   - Restart spike detection

3. **Node Health Trends**:
   - Ready node count (unexpected changes)
   - Node status changes (nodes failing/recovering)

4. **Anomaly Detection**:
   - Deviation from rolling 24h average
   - Spike detection (>1.5x rolling average)
   - Unexpected drops (capacity loss)
   - Status changes without explanation

**Trend Calculations**:
```
trend_direction = sign(current - average_24h)

trend_severity:
  if |change| < 5%:     "stable"
  if 5% <= |change| < 15%:  "moderate"
  if 15% <= |change| < 30%:  "elevated"
  if |change| >= 30%:   "critical"

prediction_urgency = (current_value - threshold) / rate_of_change
```

---

### 4. LLM Integration Architecture

**LLM Provider Options**:
1. **Ollama** (local, recommended for Phase 4)
   - Model: Llama 3.2 1B or Llama 2 7B
   - Pros: Local, no API costs, fast
   - Cons: Needs host with GPU/CPU capacity

2. **OpenAI API** (cloud, alternative)
   - Model: GPT-4 or GPT-3.5-turbo
   - Pros: More capable, hosted
   - Cons: API costs, external dependency

3. **Anthropic Claude** (alternative)
   - Pros: High quality analysis
   - Cons: API costs, external dependency

**For Phase 4**: Use Ollama locally
- Already familiar with setup (Grafana Alloy uses same cluster)
- No API costs
- Full control over model and response

**LLM Prompt Template**:
```
You are a Kubernetes cluster health analyst. Analyze the provided metrics and trends.

## Current Cluster Status
- Status: {status}
- Running Pods: {running}/{total} ({healthy}%)
- Failed Pods: {failed}
- CPU Usage: {cpu}%
- Memory Usage: {memory}%

## Recent Trends (Last 24 hours)
- CPU: {trend_cpu_direction} ({trend_cpu_change}%)
- Memory: {trend_memory_direction} ({trend_memory_change}%)
- Pod Restarts: {pod_restart_trend} (avg {restart_avg}/hr, current {restart_current}/hr)

## Identified Issues
{concerns_formatted}

## Your Analysis Should Include:
1. **Root Cause Analysis**: Why are these issues happening?
2. **Trend Implications**: What do the trends suggest?
3. **Risk Assessment**: What risks does this pose?
4. **Actionable Steps**: Specific kubectl commands or configuration changes
5. **Preventive Measures**: How to prevent recurrence?

Provide analysis in JSON format with fields: root_causes, risk_assessment, immediate_actions, preventive_measures
```

**LLM Response Parsing**:
- Extract JSON from LLM output
- Validate structure and required fields
- Fall back to template recommendations if LLM fails
- Cache LLM responses (don't call for identical inputs)

---

### 5. Integration into CronJob

**Current Flow**:
```
CronJob triggers → Get Metrics → Calculate Status → Send Discord → Exit
```

**Phase 4 Flow**:
```
CronJob triggers
  ├→ Get Metrics (existing)
  ├→ Calculate Status (existing)
  ├→ Save Report to History (NEW)
  ├→ Load Last 24h Reports (NEW)
  ├→ Prepare Context for Agent Skill (NEW)
  ├→ [ASYNC] Invoke Agent Skill (NEW)
  │   └→ Trend Analysis + LLM Analysis
  │   └→ Save analysis output
  ├→ Format Discord Message (enhanced with analysis)
  ├→ Send Discord (existing)
  └→ Exit
```

**Async Execution**: Agent skill runs in background
- Health report still sent immediately
- Analysis added if available before timeout
- Discord message includes "Analysis pending..." if not ready

**Timeout Handling**:
- Wait max 30 seconds for analysis
- If timeout, send report without analysis
- Log timeout for debugging
- Analysis continues in background

---

### 6. Data Flow Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                    Health Reporter CronJob                  │
└─────────────────────────────────────────────────────────────┘
                              │
                ┌─────────────┼─────────────┐
                │             │             │
        ┌───────▼────────┐    │      ┌──────▼────────┐
        │ Mimir Metrics  │    │      │ Report History│
        │ Collector      │    │      │ (PV Storage)  │
        └────────────────┘    │      └───────────────┘
                              │
                    ┌─────────▼──────────┐
                    │  Current Report    │
                    │  (JSON)            │
                    └────────────────────┘
                              │
                    ┌─────────▼──────────────────┐
                    │ Save to History PV         │
                    │ /reports/{timestamp}.json  │
                    └────────────────────────────┘
                              │
                    ┌─────────▼──────────────────┐
                    │ Prepare Context for Skill  │
                    │ - Current Report           │
                    │ - Last 24h Reports         │
                    │ - Analysis Config          │
                    └────────────────────────────┘
                              │
        ┌─────────────────────▼─────────────────────┐
        │                                           │
    ┌───▼──────────────────┐          ┌────────────▼────┐
    │ Send Discord Report  │          │ Agent Skill      │
    │ (without analysis)   │          │ (Async)          │
    │ Immediately          │          │                  │
    └──────────────────────┘          │ ┌──────────────┐ │
                                      │ │ Load History │ │
                                      │ └──────────────┘ │
                                      │ ┌──────────────┐ │
                                      │ │ Detect Trends│ │
                                      │ └──────────────┘ │
                                      │ ┌──────────────┐ │
                                      │ │ Call LLM     │ │
                                      │ │ (Ollama)     │ │
                                      │ └──────────────┘ │
                                      │ ┌──────────────┐ │
                                      │ │ Generate     │ │
                                      │ │ Analysis     │ │
                                      │ └──────────────┘ │
                                      └────────────────┘
                                              │
                                      ┌───────▼────────┐
                                      │ Analysis Output│
                                      │ (JSON)         │
                                      └────────────────┘
                                              │
                                      ┌───────▼──────────────┐
                                      │ [Optional] Send      │
                                      │ Updated Report to    │
                                      │ Discord via Thread   │
                                      └──────────────────────┘
```

---

## Implementation Roadmap for Phase 4

### Phase 4.1: Historical Data Storage (Week 1)
- [ ] Create PersistentVolume for reports
- [ ] Update Helm chart with PV mount
- [ ] Modify Health Reporter to save reports to `/reports/`
- [ ] Add report cleanup (retention policy: 168h = 7 days)
- [ ] Test report saving and loading

### Phase 4.2: Agent Skill Framework (Week 1-2)
- [ ] Create `.agents/skills/health-analysis/` directory
- [ ] Implement trend detection engine
- [ ] Create context preparation module
- [ ] Write unit tests for trend calculations
- [ ] Document skill usage

### Phase 4.3: LLM Integration (Week 2-3)
- [ ] Implement Ollama client wrapper
- [ ] Create LLM prompt templates
- [ ] Implement response parsing
- [ ] Add caching mechanism
- [ ] Handle LLM errors gracefully

### Phase 4.4: CronJob Integration (Week 3)
- [ ] Modify CronJob to invoke agent skill
- [ ] Implement async execution with timeout
- [ ] Enhance Discord formatter with analysis
- [ ] Add analysis to Discord embeds
- [ ] Test end-to-end flow

### Phase 4.5: Testing & Documentation (Week 3-4)
- [ ] Write integration tests
- [ ] Create example outputs
- [ ] Document LLM model selection
- [ ] Document trend detection algorithms
- [ ] Create troubleshooting guide

---

## Configuration Schema (Phase 4)

**Addition to `config.yaml`**:
```yaml
health_reporter:
  # ... existing config ...

storage:
  # Historical reports storage
  reports_directory: "/var/lib/health-reporter/reports"
  retention_hours: 168  # Keep 7 days
  backup_enabled: false  # Future: backup to S3/MinIO

analysis:
  enabled: true
  agent_skill_path: "/.agents/skills/health-analysis"
  timeout_seconds: 30
  
  trends:
    window_hours: 24
    anomaly_threshold: 1.5
    min_data_points: 6
  
  llm:
    enabled: true
    provider: "ollama"  # or "openai", "anthropic"
    model: "llama2:7b"
    endpoint: "http://ollama:11434"
    timeout_seconds: 15
    max_retries: 2
    cache_responses: true
    
  output:
    format: "json"  # Can be extended to "html", "markdown"
    include_trends: true
    include_predictions: true
    include_recommendations: true
```

---

## Success Criteria for Phase 4

✅ **Phase 4.1 Complete**: Reports saved and retrieved from history
✅ **Phase 4.2 Complete**: Trends detected accurately in test data
✅ **Phase 4.3 Complete**: LLM calls work, responses parsed correctly
✅ **Phase 4.4 Complete**: Agent skill invoked on schedule
✅ **Phase 4.5 Complete**: Discord reports include analysis insights

**Validation**:
- [ ] 168 hourly reports can be stored (7 days)
- [ ] Trends calculated without errors
- [ ] LLM analysis available within 30s timeout
- [ ] Discord reports enhanced with analysis
- [ ] No regression in health reporting (Phase 1 still works)

---

## Notes & Future Enhancements

### Phase 4 Extensions (Post-MVP)
1. **Dashboard**: Web UI to visualize trends over time
2. **Alerting**: Proactive alerts based on trend predictions
3. **Capacity Planning**: Predict when cluster needs scaling
4. **Anomaly Learning**: Teach LLM about known false positives
5. **Multi-Cluster**: Analyze multiple clusters simultaneously

### Phase 5 Integration
- Use historical data for better predictions
- Train anomaly detection models on historical patterns
- Implement capacity forecasting

### Alternative LLM Providers
- OpenAI: Better analysis, costs money (~$0.01 per report)
- Claude: Highest quality, most expensive
- Local ML models: Fast, no costs, less capable

---

## Next Steps

1. **Review & Feedback**: Review this design with stakeholder
2. **Approval**: Confirm Phase 4 scope
3. **Create Agent Skill Template**: Bootstrap `.agents/skills/health-analysis/`
4. **Start Phase 4.1**: Implement PersistentVolume storage
5. **Iterate**: Weekly reviews of each phase component

---

*Design Document created: 2026-04-12*
*Status: READY FOR IMPLEMENTATION*
