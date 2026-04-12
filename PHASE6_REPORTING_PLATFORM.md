# Phase 6 Design: Multi-Type Reporting Platform

## Vision

Transform the Health Reporter from a single infra health check into a **comprehensive reporting platform** that supports:

- **Multiple metric sources**: Infrastructure, application, business KPIs, security, compliance
- **Multiple report types**: Health, capacity, security, cost, custom
- **Multiple schedules**: Hourly, daily, weekly, monthly, cron-based
- **Multiple destinations**: Discord, email, Slack, webhooks, dashboards
- **Web UI**: Configuration, history, visualizations, management

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Reporting Platform                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌─────────────┐   ┌─────────────┐   ┌─────────────┐   ┌─────────────┐    │
│  │  Scheduler  │   │   Report    │   │   Output    │   │    Web UI   │    │
│  │   Engine    │──▶│   Engine    │──▶│   Router    │   │   Server    │    │
│  └─────────────┘   └─────────────┘   └─────────────┘   └─────────────┘    │
│         │                  │                  │                             │
│         ▼                  ▼                  ▼                             │
│  ┌─────────────┐   ┌─────────────┐   ┌─────────────┐                        │
│  │   Metric    │   │   Report    │   │  Delivery   │                        │
│  │  Collectors │   │  Templates  │   │  Adapters   │                        │
│  └─────────────┘   └─────────────┘   └─────────────┘                        │
│         │                                                         │         │
│         ▼                                                         ▼         │
│  ┌─────────────┐   ┌─────────────┐   ┌─────────────┐   ┌─────────────┐    │
│  │ Prometheus  │   │  Mimir     │   │  K8s API    │   │  Custom     │    │
│  │ (infra)     │   │  (metrics) │   │  (cluster)  │   │  Sources    │    │
│  └─────────────┘   └─────────────┘   └─────────────┘   └─────────────┘    │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Core Concepts

### 1. Report Definition (CRD)

```yaml
apiVersion: health.archipelago.ai/v1alpha1
kind: ReportDefinition
metadata:
  name: weekly-capacity-report
  namespace: monitoring
spec:
  type: capacity           # health, capacity, security, cost, custom
  schedule: "0 9 * * 1"    # Cron: Every Monday at 9am
  
  # Data sources
  sources:
    - type: prometheus
      query: "sum(kube_pod_container_resource_requests)"
    - type: mimir
      query: "up"
    - type: k8s
      resource: pods
      namespace: "{{ .Namespaces }}"
  
  # Processing pipeline
  pipeline:
    - step: aggregate
      group_by: ["namespace"]
    - step: calculate
      metrics:
        - name: total_requests
          formula: "sum(container_requests)"
        - name: utilization
          formula: "total_requests / capacity * 100"
    - step: threshold
      rules:
        - metric: utilization
          warn: 80
          critical: 90
  
  # Output configuration
  output:
    format: markdown        # markdown, json, html, pdf
    destination:
      - type: discord
        channel: "#capacity"
      - type: email
        recipients: ["team@example.com"]
      - type: webhook
        url: "https://api.example.com/reports"
  
  # Thresholds and alerts
  alerts:
    - condition: "utilization > 90"
      severity: critical
      action: notify
```

### 2. Report Type System

| Type | Description | Example Metrics | Schedule |
|------|-------------|-----------------|----------|
| **health** | Cluster/component health | CPU, memory, pods, nodes | Hourly |
| **capacity** | Resource capacity & usage | Requests, limits, PV usage | Daily/Weekly |
| **security** | Security posture | RBAC, secrets, vulnerabilities | Daily |
| **cost** | Cost tracking | Compute hours, storage GB | Monthly |
| **compliance** | Policy compliance | Pod security, network policies | Weekly |
| **application** | App-specific metrics | Latency, errors, throughput | Custom |
| **business** | Business KPIs | User signups, transactions | Custom |
| **custom** | User-defined | Any prometheus query | Custom |

### 3. Metric Collectors

```go
type MetricCollector interface {
    Collect(ctx context.Context, config CollectorConfig) (*Metrics, error)
    Type() string
}

// Built-in collectors
type PrometheusCollector struct{ /* ... */ }
type MimirCollector struct{ /* ... */ }
type K8sCollector struct{ /* ... */ }
type CustomQueryCollector struct{ /* ... */ }

// Third-party collectors (plugins)
type DatadogCollector struct{ /* ... */ }
type CloudWatchCollector struct{ /* ... */ }
type NewReliCollector struct{ /* ... */ }
type GrafanaCollector struct{ /* ... */ }
```

### 4. Delivery Adapters

```go
type DeliveryAdapter interface {
    Send(ctx context.Context, report *Report, config DestinationConfig) error
    Type() string
}

// Built-in adapters
type DiscordAdapter struct{ /* ... */ }
type EmailAdapter struct{ /* ... */ }
type SlackAdapter struct{ /* ... */ }
type WebhookAdapter struct{ /* ... */ }
type FileAdapter struct{ /* ... */ }        // Save to disk/S3
type PagerDutyAdapter struct{ /* ... */ }   // Incidents
type GrafanaAdapter struct{ /* ... */ }     // Dashboard annotations

// Future adapters
type TeamsAdapter struct{ /* ... */ }
type TelegramAdapter struct{ /* ... */ }
type SMSAdapter struct{ /* ... */ }
```

### 5. Scheduler Engine

```go
type Scheduler struct {
    // Cron-based schedules
    cronJobs map[string]*CronJob
    
    // One-time scheduled reports
    scheduledReports []ScheduledReport
    
    // Manual triggers
    manualTrigger chan ReportRequest
}

type Schedule struct {
    Type string `json:"type"` // "cron", "interval", "manual", "event"
    
    // Cron expression (e.g., "0 9 * * 1" = Monday 9am)
    Cron string `json:"cron,omitempty"`
    
    // Interval (e.g., "1h", "24h")
    Interval string `json:"interval,omitempty"`
    
    // Event-based (e.g., "on-pod-failure", "on-deployment")
    Event string `json:"event,omitempty"`
    
    // Timezone
    Timezone string `json:"timezone,omitempty"` // "UTC", "America/New_York"
}
```

---

## Web UI Design

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        Web UI Server                            │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │   API       │  │   Auth      │  │   Static   │              │
│  │   Server    │  │   (OAuth)   │  │   Files    │              │
│  └─────────────┘  └─────────────┘  └─────────────┘              │
│         │                  │                                    │
└─────────┼──────────────────┼────────────────────────────────────┘
          │                  │
          ▼                  ▼
   ┌─────────────┐    ┌─────────────┐
   │  SQLite/    │    │   K8s API   │
   │  PostgreSQL │    │   (config)  │
   └─────────────┘    └─────────────┘
```

### Features

| Feature | Description |
|---------|-------------|
| **Dashboard** | Overview of all reports, recent runs, alerts |
| **Report List** | View all report definitions, status, history |
| **Report Builder** | Visual editor for creating report definitions |
| **Schedule Manager** | View/edit cron schedules, pause/resume |
| **History Browser** | View past report outputs, trends |
| **Alert Configuration** | Set up alert rules and notifications |
| **Settings** | Configure destinations, collectors, users |
| **API Keys** | Manage API access for external integrations |

### API Endpoints

```yaml
/api/v1:
  /reports:
    GET    - List all reports
    POST   - Create new report
    GET    - Get report by ID
    PUT    - Update report
    DELETE - Delete report
  
  /reports/{id}/run:
    POST   - Trigger report immediately
  
  /reports/{id}/history:
    GET    - Get report execution history
  
  /reports/{id}/output:
    GET    - Get latest report output
  
  /destinations:
    GET    - List configured destinations
    POST   - Add destination
    PUT    - Update destination
    DELETE - Remove destination
  
  /schedules:
    GET    - List all schedules
    PUT    - Update schedule
  
  /metrics:
    GET    - Platform metrics (reports sent, errors, etc.)
  
  /health:
    GET    - UI health status
```

### Frontend Stack

- **Framework**: React or Vue.js
- **UI Library**: Tailwind CSS + Headless UI or Radix
- **Charts**: Recharts or Chart.js
- **State**: React Query or Vue Use
- **Build**: Vite

---

## Data Model

### ReportDefinition (CRD)

```go
type ReportDefinition struct {
    TypeMeta   metav1.TypeMeta   `json:",inline"`
    ObjectMeta metav1.ObjectMeta `json:"metadata,omitempty"`
    
    Spec ReportSpec `json:"spec"`
    Status ReportStatus `json:"status,omitempty"`
}

type ReportSpec struct {
    Type      string      `json:"type"` // health, capacity, security, cost, custom
    Schedule  Schedule    `json:"schedule"`
    Sources   []Source    `json:"sources"`
    Pipeline  Pipeline    `json:"pipeline"`
    Output    Output      `json:"output"`
    Alerts    []Alert     `json:"alerts,omitempty"`
}

type Schedule struct {
    Type      string `json:"type"` // cron, interval, manual
    Cron      string `json:"cron,omitempty"`
    Interval  string `json:"interval,omitempty"`
    Timezone  string `json:"timezone,omitempty"`
    Paused    bool   `json:"paused,omitempty"`
}

type Source struct {
    Type    string `json:"type"` // prometheus, mimir, k8s, custom
    Name    string `json:"name"`
    Config  map[string]interface{} `json:"config"`
}

type Pipeline struct {
    Steps []PipelineStep `json:"steps"`
}

type PipelineStep struct {
    Name   string `json:"name"`
    Type   string `json:"type"` // aggregate, calculate, filter, transform
    Config map[string]interface{} `json:"config"`
}

type Output struct {
    Format     string `json:"format"` // markdown, json, html, pdf
    Template   string `json:"template,omitempty"`
    Destinations []Destination `json:"destinations"`
}

type Destination struct {
    Type   string `json:"type"` // discord, email, slack, webhook
    Config map[string]interface{} `json:"config"`
}

type Alert struct {
    Condition string `json:"condition"`
    Severity  string `json:"severity"` // info, warning, critical
    Action    string `json:"action"` // notify, escalate
}
```

### ReportExecution

```go
type ReportExecution struct {
    ID        string    `json:"id"`
    ReportID  string    `json:"report_id"`
    StartedAt time.Time `json:"started_at"`
    FinishedAt time.Time `json:"finished_at,omitempty"`
    Status    string    `json:"status"` // running, success, failed
    
    Metrics   map[string]interface{} `json:"metrics,omitempty"`
    Output    string `json:"output,omitempty"`
    Error     string `json:"error,omitempty"`
    
    Destinations []DeliveryResult `json:"delivery_results,omitempty"`
}

type DeliveryResult struct {
    Destination string `json:"destination"`
    Status      string `json:"status"`
    Error       string `json:"error,omitempty"`
}
```

---

## Implementation Phases

### Phase 6.1: Multi-Report Foundation
- [ ] Refactor to support multiple report types
- [ ] Add report definition CRD
- [ ] Implement scheduler with cron support
- [ ] Add destination routing

### Phase 6.2: Additional Report Types
- [ ] Add capacity report type
- [ ] Add security report type
- [ ] Add custom query support

### Phase 6.3: Multi-Destination
- [ ] Add email adapter
- [ ] Add Slack adapter
- [ ] Add webhook adapter

### Phase 6.4: Web UI
- [ ] Set up Go web server
- [ ] Build React frontend
- [ ] Implement API endpoints
- [ ] Add auth (OAuth)

### Phase 6.5: Advanced Features
- [ ] Report templates library
- [ ] Visualization builder
- [ ] Alert management
- [ ] API for external integrations

---

## Configuration Example

```yaml
# Platform-wide config
platform:
  # Default settings
  defaults:
    retention_days: 30
    timeout_seconds: 60
    max_retries: 3
  
  # Available collectors
  collectors:
    - name: prometheus
      enabled: true
      default_endpoint: "http://prometheus:9090"
    - name: mimir
      enabled: true
      default_endpoint: "http://mimir:9009"
    - name: k8s
      enabled: true
      default_namespace: "monitoring"
  
  # Available destinations
  destinations:
    - name: discord
      enabled: true
      default_webhook_env: "DISCORD_WEBHOOK"
    - name: email
      enabled: true
      smtp_host: "smtp.example.com"
      smtp_port: 587
    - name: slack
      enabled: false
    - name: webhook
      enabled: true

# Example report definitions (can also be CRDs)
reports:
  - name: daily-capacity
    type: capacity
    schedule: "0 8 * * *"  # 8am daily
    sources:
      - type: prometheus
        query: "sum(kube_pod_container_resource_requests) by (namespace)"
    output:
      format: markdown
      destinations:
        - type: discord
        - type: email

  - name: weekly-security
    type: security
    schedule: "0 9 * * 0"  # Sunday 9am
    sources:
      - type: k8s
        resource: pods
        check: "security_context"
    output:
      format: markdown
      destinations:
        - type: discord

  - name: hourly-health
    type: health
    schedule: "0 * * * *"  # Every hour
    # ... existing health report config
```

---

## File Structure

```
cmd/
├── health-reporter/     # Existing daemon
└── reporting-ui/       # NEW: Web UI server

pkg/
├── collector/
│   ├── prometheus.go
│   ├── mimir.go
│   ├── k8s.go
│   └── types.go
├── scheduler/
│   ├── engine.go
│   ├── cron.go
│   └── types.go
├── report/
│   ├── engine.go
│   ├── pipeline.go
│   ├── templates.go
│   └── types.go
├── output/
│   ├── router.go
│   ├── adapters/
│   │   ├── discord.go
│   │   ├── email.go
│   │   ├── slack.go
│   │   └── webhook.go
│   └── types.go
└── storage/
    ├── history.go
    └── migrations.go

helm/reporting-platform/
├── values.yaml
└── templates/
    ├── deployment-ui.yaml
    ├── service-ui.yaml
    ├── ingress.yaml
    └── configmap.yaml
```

---

## UI Screens

### 1. Dashboard
```
┌─────────────────────────────────────────────────────────────┐
│  📊 Reporting Platform                    [Settings] [?]   │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐         │
│  │ 15      │ │ 3       │ │ 1       │ │ 98%     │         │
│  │ Reports │ │ Running │ │ Failed  │ │ Delivered│         │
│  └─────────┘ └─────────┘ └─────────┘ └─────────┘         │
│                                                             │
│  Recent Reports                                            │
│  ┌─────────────────────────────────────────────┐          │
│  │ ✓ daily-capacity      2h ago    Success    │          │
│  │ ✓ hourly-health       1h ago    Success    │          │
│  │ ✗ weekly-security     5h ago    Failed     │          │
│  │ ✓ monthly-cost        1d ago    Success    │          │
│  └─────────────────────────────────────────────┘          │
│                                                             │
│  [ + New Report ]                                          │
└─────────────────────────────────────────────────────────────┘
```

### 2. Report Builder
```
┌─────────────────────────────────────────────────────────────┐
│  Create New Report                            [Save] [Cancel]│
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Name: [________________________]                          │
│  Type: [health ▼]  Schedule: [0 * * * * ▼]               │
│                                                             │
│  ── Data Sources ──                                        │
│  [+ Add Source]                                            │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ Source: [Prometheus ▼]  Query: [____________]      │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│  ── Pipeline ──                                            │
│  [Aggregate] [Calculate] [Filter] [Transform]              │
│                                                             │
│  ── Output ──                                              │
│  Format: [Markdown ▼]                                      │
│  Destinations:                                             │
│    ☑ Discord  ☑ Email  ☐ Slack  ☐ Webhook                │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

---

## Migration Path

1. **Keep existing** health reporter as "default" report type
2. **Add new CRDs** for ReportDefinition
3. **Extend scheduler** to handle multiple schedules
4. **Add output router** for multiple destinations
5. **Deploy UI** alongside existing deployment
6. **Migrate** existing config to new format incrementally

---

## Success Criteria

- [ ] Multiple report types supported (health, capacity, security, custom)
- [ ] Multiple schedules (cron, interval, manual)
- [ ] Multiple destinations (Discord, email, Slack, webhook)
- [ ] Web UI for managing reports
- [ ] Historical data storage and visualization
- [ ] API for programmatic access
- [ ] Backward compatible with existing health reporter

---

*Document created: 2026-04-12*
*Status: DESIGN COMPLETE*
*Phase: 6 (Future)*