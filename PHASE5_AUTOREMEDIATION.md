# Phase 5 Design: Auto-Remediation Agent

## Overview

**Objective**: Implement an intelligent auto-remediation system that can:
1. Detect specific issues from health reports and analysis
2. Investigate root causes using Kubernetes API
3. Attempt automated fixes for known patterns (limited scope)
4. Report actions taken back to Discord

**Guiding Principles**:
- **Safety first**: Only attempt safe, reversible actions
- **Human in the loop**: Option to enable/disable auto-remediation
- **Audit trail**: Log all actions for review
- **Scope limited**: Start with simple, safe fixes only

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    Health Reporter Daemon                       │
└─────────────────────────────────────────────────────────────────┘
                               │
         ┌─────────────────────┼─────────────────────┐
         │                     │                     │
    ┌────▼────┐          ┌────▼────┐          ┌────▼────┐
    │ Metrics │          │  Smoke  │          │Analysis │
    │Collector│          │  Tests  │          │  (LLM)  │
    └─────────┘          └─────────┘          └─────────┘
         │                     │                     │
         └─────────────────────┼─────────────────────┘
                               │
                    ┌──────────▼──────────┐
                    │  Report Generator   │
                    └──────────┬──────────┘
                               │
                    ┌──────────▼──────────┐
                    │ Auto-Remediation    │
                    │      Engine         │
                    └──────────┬──────────┘
                               │
         ┌─────────────────────┼─────────────────────┐
         │                     │                     │
    ┌────▼────┐          ┌────▼────┐          ┌────▼────┐
    │Ruleset │          │K8s API │          │ Action  │
    │ Engine │          │ Client │          │ Executor│
    └─────────┘          └─────────┘          └─────────┘
```

---

## Components

### 1. Ruleset Engine (`pkg/remediation/ruleset.go`)

**Purpose**: Match report issues to remediation rules

**Rule Structure**:
```go
type RemediationRule struct {
    Name        string   `json:"name"`
    Description string   `json:"description"`
    
    // Trigger conditions
    Conditions  RuleConditions `json:"conditions"`
    
    // Investigation steps (before acting)
    Investigation []InvestigationStep `json:"investigation"`
    
    // Action to take
    Action       Action `json:"action"`
    
    // Safety controls
    Enabled      bool   `json:"enabled"`
    DryRun       bool   `json:"dry_run"`        // Always log, never execute
    MaxRetries   int    `json:"max_retries"`
    Timeout      int    `json:"timeout_seconds"`
}

type RuleConditions struct {
    Status          []string `json:"status"`           // ["degraded", "critical"]
    ConcernTitles   []string `json:"concern_titles"`   // ["Pod Restarts", "Failed Pods"]
    SmokeTestFailed []string `json:"smoke_test_failed"` // test names that failed
    MetricThresholds map[string]Threshold `json:"metric_thresholds"`
}

type Threshold struct {
    Metric   string  `json:"metric"`    // "cpu_usage_percent", "pod_restarts"
    Operator string  `json:"operator"`  // "gt", "lt", "gte", "lte"
    Value    float64 `json:"value"`
}
```

**Example Rules** (Phase 5.1 - Safe actions only):

```yaml
rules:
  # Rule 1: Restart pods in CrashLoopBackOff
  - name: restart_crashing_pods
    description: Restart pods stuck in CrashLoopBackOff
    enabled: true
    dry_run: false
    conditions:
      concern_titles:
        - "Pod Restarts"
        - "Failed Pods"
    investigation:
      - step: get_pod_events
        resource: pods
        field: events
      - step: check_crash_loop_status
        check: "reason=CrashLoopBackOff"
    action:
      type: delete_pod
      target: "{{ .FailedPodName }}"
      namespace: "{{ .Namespace }}"
  
  # Rule 2: Clear completed jobs
  - name: cleanup_completed_jobs
    description: Remove old completed jobs to free resources
    enabled: true
    conditions:
      metric_thresholds:
        memory_usage_percent:
          operator: "gt"
          value: 85
    investigation:
      - step: list_completed_jobs
        resource: jobs
        status: "completed"
    action:
      type: delete_resources
      resource: jobs
      selector: "status.phase=Completed"
      older_than: "24h"
  
  # Rule 3: Scale down idle deployments (future)
  - name: scale_idle_deployments
    description: Scale down deployments with 0 replicas for 24h
    enabled: false  # Disabled by default - risky
    conditions: {}
    action:
      type: scale_deployment
      replicas: 0
```

### 2. Investigation Engine (`pkg/remediation/investigate.go`)

**Purpose**: Gather evidence before taking action

```go
type InvestigationStep struct {
    Name   string `json:"name"`
    Query  string `json:"query"`  // Can use template syntax
    
    // Kubernetes queries
    Resource  string `json:"resource"`  // "pods", "events", "jobs", "deployments"
    Namespace string `json:"namespace"`
    LabelSelector string `json:"label_selector"`
    FieldSelector string `json:"field_selector"`
    
    // What to check
    Check    string `json:"check"`  // e.g., "status.phase=Failed"
}

type InvestigationResult struct {
    Step      string      `json:"step"`
    Data      interface{} `json:"data"`
    Findings  []Finding   `json:"findings"`
    Error     string      `json:"error,omitempty"`
}

type Finding struct {
    Resource   string `json:"resource"`
    Name       string `json:"name"`
    Reason     string `json:"reason"`
    Message    string `json:"message"`
    Actionable bool   `json:"actionable"`
}
```

### 3. Action Executor (`pkg/remediation/executor.go`)

**Purpose**: Execute remediation actions safely

```go
type Action struct {
    Type string `json:"type"` // "delete_pod", "delete_resources", "scale_deployment", "exec_command"
    
    // Target specification
    Target     string `json:"target,omitempty"`     // Template: "{{ .FailedPodName }}"
    Namespace  string `json:"namespace,omitempty"`
    Resource   string `json:"resource,omitempty"`   // "pods", "jobs", "deployments"
    Selector   string `json:"selector,omitempty"`
    
    // Action parameters
    Replicas   *int   `json:"replicas,omitempty"`
    Command    string `json:"command,omitempty"`
    Container  string `json:"container,omitempty"`
    
    // Filters
    OlderThan  string `json:"older_than,omitempty"`  // "24h", "1h"
}

type ActionResult struct {
    Action     string      `json:"action"`
    Success    bool        `json:"success"`
    Message    string      `json:"message"`
    Resources  []string    `json:"resources_affected"`
    Error      string      `json:"error,omitempty"`
    Timestamp  time.Time   `json:"timestamp"`
}
```

### 4. Kubernetes Client Wrapper (`pkg/remediation/k8s_client.go`)

**Purpose**: Provide safe K8s operations with RBAC

```go
type K8sClient struct {
    Client       kubernetes.Interface
    DynamicClient dynamic.Interface
    Namespace    string
}

func (k *K8sClient) GetPodsWithEvents(ctx context.Context, namespace string) ([]PodWithEvents, error)
func (k *K8sClient) GetFailedPods(ctx context.Context, namespace string) ([]corev1.Pod, error)
func (k *K8sClient) DeletePod(ctx context.Context, name, namespace string) error
func (k *K8sClient) ScaleDeployment(ctx context.Context, name, namespace string, replicas int) error
func (k *K8sClient) DeleteOldJobs(ctx context.Context, namespace, olderThan string) error
func (k *K8sClient) GetEvents(ctx context.Context, namespace, resourceName string) ([]corev1.Event, error)
```

### 5. Audit Logger (`pkg/remediation/audit.go`)

**Purpose**: Track all remediation attempts

```go
type AuditLog struct {
    Timestamp    time.Time `json:"timestamp"`
    ReportID     string    `json:"report_id"`
    RuleName    string    `json:"rule_name"`
    Triggered   bool      `json:"triggered"`
    
    Investigation []InvestigationResult `json:"investigation"`
    ActionResult *ActionResult          `json:"action_result"`
    
    Status       string    `json:"status"`  // "triggered", "investigated", "executed", "failed", "skipped"
    Error        string    `json:"error,omitempty"`
}
```

---

## Implementation Roadmap

### Phase 5.1: Foundation (Week 1)
- [ ] Create `pkg/remediation/` package
- [ ] Implement Ruleset engine with rule loading (YAML)
- [ ] Implement K8s client wrapper
- [ ] Add "dry run" mode (always log, never execute)
- [ ] Add audit logging

### Phase 5.2: Safe Actions (Week 2)
- [ ] Implement "restart crashing pod" rule
- [ ] Implement "cleanup completed jobs" rule
- [ ] Add Discord notification for actions taken
- [ ] Add config option to enable/disable auto-remediation

### Phase 5.3: Investigation (Week 2-3)
- [ ] Implement investigation steps
- [ ] Add pod event collection
- [ ] Add CrashLoopBackOff detection
- [ ] Add pre-action approval check

### Phase 5.4: Testing & Safety (Week 3)
- [ ] Add integration tests with mock K8s
- [ ] Add RBAC constraints (ServiceAccount with limited permissions)
- [ ] Add rate limiting (max 1 action per hour)
- [ ] Add rollback capability

---

## Configuration Schema

**Addition to `config.yaml`**:
```yaml
remediation:
  enabled: false  # Disabled by default - must opt-in
  dry_run: true   # Always start with dry run
  
  # Rate limiting
  max_actions_per_hour: 1
  cooldown_minutes: 60
  
  # Action categories (what to allow)
  allowed_actions:
    - delete_pod        # Restart crashing pods
    - delete_resources  # Cleanup old jobs
    - scale_deployment  # Scale idle workloads (disabled by default)
  
  # Per-rule overrides
  rule_overrides:
    restart_crashing_pods:
      enabled: true
      dry_run: false
    cleanup_completed_jobs:
      enabled: true
      dry_run: false
    scale_idle_deployments:
      enabled: false
```

---

## RBAC Requirements

**ServiceAccount**: `health-reporter-auto-remediation`

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: health-reporter-remediation
  namespace: monitoring
rules:
# Get/list/watch pods and events (for investigation)
- apiGroups: [""]
  resources: ["pods", "pods/log", "events"]
  verbs: ["get", "list", "watch"]

# Delete pods (restart action)
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["delete"]

# Get/list/watch jobs (for cleanup)
- apiGroups: ["batch"]
  resources: ["jobs"]
  verbs: ["get", "list", "watch", "delete"]

# Scale deployments (future)
- apiGroups: ["apps"]
  resources: ["deployments"]
  verbs: ["get", "patch"]
```

---

## Discord Integration

**Message Format** (when auto-remediation takes action):

```
🤖 Auto-Remediation Action Taken

Rule: restart_crashing_pods
Status: SUCCESS
Actions:
  • Deleted pod: pod-name in namespace monitoring
  • Reason: Pod in CrashLoopBackOff (restart count: 5)

Investigation:
  • Pod events: CrashLoopBackOff - Back-off restarting failed container
  
Audit ID: 2026-04-12T02-30-00Z
```

---

## Safety Mechanisms

1. **Dry run by default**: Always start with `dry_run: true`
2. **Limited scope**: Only restart pods, cleanup jobs (no config changes)
3. **Rate limiting**: Max 1 action per hour
4. **Audit trail**: All actions logged to file + Discord
5. **RBAC isolation**: Dedicated ServiceAccount with minimal permissions
6. **Timeout**: Actions timeout after 30 seconds
7. **Manual override**: Can disable via config

---

## Error Handling

| Scenario | Behavior |
|----------|----------|
| K8s API timeout | Log error, skip action, continue |
| Permission denied | Log error, skip action, alert via Discord |
| Action fails | Log error, rollback if possible, alert via Discord |
| Rule match fails | Log error, continue with next rule |
| No historical data | Skip remediation, continue |

---

## Testing Strategy

1. **Unit tests**: Rule matching, investigation logic
2. **Integration tests**: K8s client with mock server
3. **Dry run validation**: Run against real cluster, verify no changes
4. **Chaos testing**: Introduce failures, verify remediation works

---

## File Structure

```
pkg/remediation/
├── ruleset.go        # Rule loading and matching
├── investigate.go   # Investigation steps
├── executor.go       # Action execution
├── k8s_client.go     # K8s API wrapper
├── audit.go         # Audit logging
├── types.go         # Type definitions
└── test_utils.go    # Test helpers
```

---

## Example Rule File (`rules.d/pod-restarts.yaml`)

```yaml
- name: restart_crashing_pods
  description: Restart pods stuck in CrashLoopBackOff
  enabled: true
  dry_run: false
  max_retries: 3
  timeout_seconds: 30
  
  conditions:
    concern_titles:
      - "Pod Restarts"
    metric_thresholds:
      pod_restarts:
        operator: "gte"
        value: 5
  
  investigation:
    - name: get_pod_events
      resource: pods
      namespace: "{{ .Namespace }}"
      label_selector: "app={{ .AppLabel }}"
      
    - name: check_crash_loop
      check: "reason=CrashLoopBackOff"
  
  action:
    type: delete_pod
    namespace: "{{ .Namespace }}"
    selector: "app={{ .AppLabel }}"
```

---

## Future Enhancements (Post-Phase 5)

1. **AI-driven investigation**: Use LLM to analyze pod logs
2. **Predictive remediation**: Act before issues occur
3. **Multi-cluster support**: Apply rules across clusters
4. **Custom rules**: User-defined rules via CRD
5. **Approval workflow**: Require human approval for certain actions

---

## Success Criteria

- [ ] Auto-remediation can detect and restart crashed pods
- [ ] All actions logged with audit trail
- [ ] Discord notifications for actions taken
- [ ] Dry run mode works correctly
- [ ] RBAC properly restricts actions
- [ ] No accidental cluster damage in testing

---

*Document created: 2026-04-12*
*Status: READY FOR IMPLEMENTATION*
*Phase: 5 (Future)*