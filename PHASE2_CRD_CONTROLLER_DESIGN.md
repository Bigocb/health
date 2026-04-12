# Phase 2 Controller Design: Kubernetes-Native Smoke Tests via CRDs

## Overview

**New Architecture**: Instead of YAML config file, use Kubernetes Custom Resource Definitions (CRDs)

**Benefits**:
- Update tests without redeploying the app
- GitOps-friendly (tests in git, apply via `kubectl apply`)
- Dynamic test discovery via controller
- Natural K8s resource management
- Watch-based updates (tests update immediately)
- Audit trail via etcd (Kubernetes API)
- Multi-namespace support
- RBAC control over test definitions

---

## CRD Design

### 1. SmokeTest CRD Definition

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: smoketests.health.archipelago.ai
spec:
  group: health.archipelago.ai
  names:
    kind: SmokeTest
    plural: smoketests
    singular: smoketest
    shortNames:
      - st
  scope: Namespaced
  versions:
    - name: v1alpha1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              required:
                - type
                - interval
              properties:
                type:
                  type: string
                  enum: ["dns", "http", "tcp"]
                  description: "Test type"
                
                enabled:
                  type: boolean
                  default: true
                  description: "Enable/disable this test"
                
                interval:
                  type: string
                  default: "0h"
                  description: "Optional: Override global interval for this test"
                
                severity:
                  type: string
                  enum: ["critical", "high", "medium", "low"]
                  default: "high"
                  description: "Failure severity level"
                
                timeout:
                  type: string
                  default: "5s"
                  description: "Test timeout"
                
                # DNS Test fields
                domain:
                  type: string
                  description: "Domain to resolve (DNS tests)"
                
                # HTTP Test fields
                url:
                  type: string
                  description: "Endpoint URL (HTTP tests)"
                
                method:
                  type: string
                  default: "GET"
                  enum: ["GET", "POST", "PUT", "DELETE", "HEAD"]
                  description: "HTTP method"
                
                expectedStatus:
                  type: integer
                  default: 200
                  description: "Expected HTTP status code"
                
                tlsInsecure:
                  type: boolean
                  default: false
                  description: "Skip TLS cert verification"
                
                headers:
                  type: object
                  additionalProperties:
                    type: string
                  description: "Custom HTTP headers"
                
                # TCP Test fields
                host:
                  type: string
                  description: "Hostname or IP (TCP tests)"
                
                port:
                  type: integer
                  minimum: 1
                  maximum: 65535
                  description: "Port number (TCP tests)"
            
            status:
              type: object
              properties:
                lastRun:
                  type: string
                  format: date-time
                
                lastStatus:
                  type: string
                  enum: ["pass", "fail", "timeout", "error"]
                
                lastMessage:
                  type: string
                
                passCount:
                  type: integer
                
                failCount:
                  type: integer
```

### 2. Example CRD Resources

**DNS Test**:
```yaml
apiVersion: health.archipelago.ai/v1alpha1
kind: SmokeTest
metadata:
  name: kubernetes-api-dns
  namespace: monitoring
spec:
  type: dns
  enabled: true
  domain: kubernetes.default.svc.cluster.local
  timeout: 5s
  severity: critical
```

**HTTP Test**:
```yaml
apiVersion: health.archipelago.ai/v1alpha1
kind: SmokeTest
metadata:
  name: prometheus-health
  namespace: monitoring
spec:
  type: http
  enabled: true
  url: http://prometheus.monitoring.svc.cluster.local:9090/-/healthy
  method: GET
  expectedStatus: 200
  timeout: 5s
  severity: high
```

**TCP Test**:
```yaml
apiVersion: health.archipelago.ai/v1alpha1
kind: SmokeTest
metadata:
  name: etcd-port
  namespace: monitoring
spec:
  type: tcp
  enabled: true
  host: localhost
  port: 2379
  timeout: 5s
  severity: critical
```

---

## Controller Implementation Architecture

### 1. Controller Flow

```
┌──────────────────────────────────────────────────────┐
│         Health Reporter App (with Controller)         │
└──────────────────────────────────────────────────────┘
                         │
         ┌───────────────┼───────────────┐
         │               │               │
    ┌────▼────┐    ┌─────▼────┐    ┌───▼────┐
    │ Watch    │    │  Mimir   │    │ Discord│
    │ CRDs     │    │ Metrics  │    │ Webhook│
    └────┬────┘    └─────┬────┘    └───┬────┘
         │               │              │
         └───────────────┼──────────────┘
                         │
         ┌───────────────▼──────────────┐
         │   Smoke Test Runner          │
         │   (dynamic from CRDs)        │
         └───────────────┬──────────────┘
                         │
         ┌───────────────▼──────────────┐
         │   Health Report Generator    │
         └──────────────────────────────┘
```

### 2. Controller Components

```go
// SmokeTestController watches CRD changes
type SmokeTestController struct {
    clientset       kubernetes.Interface
    informer        cache.SharedIndexInformer
    workqueue       workqueue.RateLimitingInterface
    recorder        record.EventRecorder
    testRunner      *smoke_tests.SmokeTestRunner
}

// Reconciler reconciles SmokeTest resources
func (c *SmokeTestController) Reconcile(ctx context.Context, key string) error {
    // 1. Get the SmokeTest resource from cache
    obj, exists, err := c.informer.GetIndexer().GetByKey(key)
    
    // 2. Parse into SmokeTest struct
    smokeTest := obj.(*SmokeTest)
    
    // 3. If enabled: add/update in runner
    // 4. If disabled: remove from runner
    // 5. Update status subresource with last run info
}

// Watch watches for SmokeTest CRD changes
func (c *SmokeTestController) Start(ctx context.Context) {
    go c.informer.Run(ctx.Done())
    go c.runWorker(ctx)
}
```

### 3. Runtime Behavior

**Startup**:
1. Controller starts
2. Lists all SmokeTest resources in cluster
3. Dynamically adds them to test runner
4. Begins periodic test execution

**Update (kubectl apply)**:
1. User creates/updates SmokeTest CRD
2. Informer detects change
3. Adds to workqueue for reconciliation
4. Controller updates test runner
5. Updates CRD status with last run info

**Delete**:
1. User deletes SmokeTest CRD
2. Informer detects deletion
3. Controller removes from test runner
4. Test no longer runs

---

## Go Implementation Plan

### Phase 2.1: Core Types (Day 1)
```go
// pkg/apis/health/v1alpha1/smoketest_types.go
type SmokeTest struct {
    metav1.TypeMeta
    metav1.ObjectMeta
    Spec   SmokeTestSpec
    Status SmokeTestStatus
}

type SmokeTestSpec struct {
    Type            string
    Enabled         bool
    Domain          string  // DNS
    URL             string  // HTTP
    Host            string  // TCP
    Port            int     // TCP
    ExpectedStatus  int     // HTTP
    TLSInsecure     bool    // HTTP
    Timeout         string
    Severity        string
    // ... other fields
}

type SmokeTestStatus struct {
    LastRun       metav1.Time
    LastStatus    string
    LastMessage   string
    PassCount     int
    FailCount     int
}
```

### Phase 2.2: CRD Generator (Day 1)
```bash
# Generate DeepCopy, client, informer, lister
code-generator
  --go-header-file=hack/boilerplate.go.txt
  --input-dirs=./pkg/apis/health/v1alpha1
  --output-base=./
```

### Phase 2.3: Controller (Day 2)
```go
// pkg/controller/smoketest_controller.go
func NewSmokeTestController(
    clientset kubernetes.Interface,
    informer cache.SharedIndexInformer,
) *SmokeTestController {
    // Initialize controller
    // Setup event recorder
    // Setup workqueue
}

func (c *SmokeTestController) Reconcile(ctx context.Context, key string) error {
    // Reconciliation logic
}
```

### Phase 2.4: Integration (Day 2-3)
- Embed controller in main app
- Start controller alongside CronJob logic
- Update health reporter to use controller's test runner

### Phase 2.5: Testing & CRDs (Day 3-4)
- Unit tests for controller
- Integration tests with fake clientset
- Sample CRD manifests
- Helm chart integration

---

## Helm Chart Updates

### Current Structure
```yaml
# values.yaml
smokeTests:
  enabled: false
  tests: []  # List of test configs

# templates/deployment.yaml
# App deployed as single-run CronJob
```

### New Structure
```yaml
# values.yaml
smokeTests:
  enabled: true
  # No tests here anymore!

crd:
  install: true  # Install SmokeTest CRD

# templates/crd.yaml (new)
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: smoketests.health.archipelago.ai
spec: ...

# templates/samples/ (new)
# Example SmokeTest resources

# templates/cronjob.yaml (modified)
# Now includes controller sidecar or init container
```

### Controller Deployment Options

**Option A: Sidecar in CronJob Pod**
```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: health-reporter
spec:
  schedule: "0 * * * *"
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: health-reporter
          containers:
          - name: controller
            image: health-reporter:latest
            args:
            - --mode=controller
            - --watch-only  # Just watch and update tests
          - name: reporter
            image: health-reporter:latest
            args:
            - --mode=reporter
            - --once  # Run once then exit
```

**Option B: Separate Deployment (Recommended)**
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: health-reporter-controller
spec:
  replicas: 1  # Single instance to avoid race conditions
  template:
    spec:
      serviceAccountName: health-reporter
      containers:
      - name: controller
        image: health-reporter:latest
        args:
        - --mode=controller
        - --continuous
```

**Option C: Operator Pattern (Future)**
Dedicated operator pod for test management.

---

## Implementation with Dynamic Test Discovery

### Main App Changes

```go
// cmd/health-reporter/main.go
func main() {
    mode := flag.String("mode", "reporter", "reporter|controller|both")
    
    if *mode == "controller" || *mode == "both" {
        // Start controller
        controller := NewSmokeTestController(...)
        go controller.Start(ctx)
        
        // Controller updates global test runner
        testRunner := controller.GetTestRunner()
    }
    
    if *mode == "reporter" || *mode == "both" {
        // Run health reporter with tests from controller
        // Tests are discovered dynamically
    }
}
```

### Dynamic Test Discovery Flow

```
1. Controller starts
2. Lists all SmokeTest CRDs
3. For each CRD:
   - Parse spec
   - Create TestRunner from spec
   - Add to registry
4. Watch for updates
5. When update detected:
   - If enabled: add/update
   - If disabled: remove
   - If deleted: remove
6. Health reporter periodically:
   - Gets current test list from controller
   - Runs tests
   - Reports results
```

---

## RBAC for Controller

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: health-reporter-controller
rules:
# Read SmokeTest CRDs
- apiGroups: ["health.archipelago.ai"]
  resources: ["smoketests"]
  verbs: ["get", "list", "watch"]

# Update SmokeTest status
- apiGroups: ["health.archipelago.ai"]
  resources: ["smoketests/status"]
  verbs: ["get", "patch", "update"]

# Create events for audit trail
- apiGroups: [""]
  resources: ["events"]
  verbs: ["create", "patch"]

# Existing permissions for metrics, etc.
- apiGroups: [""]
  resources: ["nodes", "pods"]
  verbs: ["get", "list"]
```

---

## GitOps Workflow with CRDs

### User Experience

**Step 1: Define Tests in Git**
```yaml
# infrastructure/health-reporter/tests/
# └── smoke-tests.yaml

apiVersion: health.archipelago.ai/v1alpha1
kind: SmokeTest
metadata:
  name: api-dns
  namespace: monitoring
spec:
  type: dns
  domain: kubernetes.default
  severity: critical
---
apiVersion: health.archipelago.ai/v1alpha1
kind: SmokeTest
metadata:
  name: prometheus
  namespace: monitoring
spec:
  type: http
  url: http://prometheus:9090/-/healthy
  severity: high
```

**Step 2: Apply via GitOps**
```bash
# ArgoCD syncs automatically
kubectl apply -f smoke-tests.yaml
```

**Step 3: Controller Updates Tests**
- Controller detects new CRDs
- Adds tests to runner immediately
- No app restart needed

**Step 4: Monitor Test Status**
```bash
kubectl get smoketests -n monitoring
kubectl describe smoketest prometheus -n monitoring
kubectl logs -f deployment/health-reporter-controller
```

---

## Advantages Over Config File Approach

| Aspect | Config File | CRD-Based |
|--------|------------|----------|
| **Update Tests** | Restart app | kubectl apply |
| **GitOps** | Manual sync | Native k8s |
| **Audit Trail** | None | Etcd history |
| **Discovery** | Static list | Dynamic watch |
| **Multi-instance** | Share config | Coordinated |
| **Status Tracking** | In-memory | CRD status |
| **RBAC** | File system | K8s RBAC |
| **Templating** | Manual | Kustomize/Helm |

---

## Persistence Strategy (Addressing Crash Scenario)

Since CRDs are in etcd, they persist:

### What Survives a Pod Crash
- ✅ Test definitions (in etcd)
- ✅ Test status updates (in CRD status field)
- ✅ Last run timestamps
- ✅ Pass/fail counts

### What Gets Lost (Temporary)
- ❌ In-memory test history (cache)
- ❌ Current test runner state

### Lightweight Cache Strategy (Optional)

If we want to keep 24h of test results in K8s:

**Option 1: ConfigMap (Simple)**
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: health-reporter-cache
  namespace: monitoring
data:
  results.json: |
    [
      {"timestamp": "2026-04-12T00:00:00Z", "results": [...]},
      ...
    ]
```
- Controller updates after each run
- Survives pod crash
- Limited to ~1MB

**Option 2: Custom CRD (Better)**
```yaml
apiVersion: health.archipelago.ai/v1alpha1
kind: SmokeTestResult
metadata:
  name: test-result-2026-04-12-00-00
  namespace: monitoring
spec:
  testName: api-dns
  timestamp: 2026-04-12T00:00:00Z
  status: pass
  duration: 15ms
```
- One result per test per run
- Auto-cleanup via TTL
- Full audit trail

**Option 3: PersistentVolume (Future)**
```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: health-reporter-results
spec:
  accessModes: ["ReadWriteOnce"]
  resources:
    requests:
      storage: 1Gi
```
- For full historical analysis
- Needed for Phase 4 trend analysis
- Survives pod crash

---

## Recommended Implementation Path

### Phase 2.0: CRD Controller MVP (Week 1)
- [ ] Define SmokeTest CRD
- [ ] Implement controller with informer/workqueue
- [ ] Dynamic test discovery
- [ ] Update tests without restart
- [ ] Helm chart with CRD

### Phase 2.1: Status & Monitoring (Week 1-2)
- [ ] Update CRD status subresource
- [ ] Event recording for audit trail
- [ ] kubectl get/describe smoketests
- [ ] Test result tracking

### Phase 2.2: History & Trends (Week 2+)
- [ ] Optional: SmokeTestResult CRD for history
- [ ] Optional: ConfigMap for cache
- [ ] Feed into Phase 4 trend analysis

---

## Go Dependencies for Controller

```go
// go.mod additions
require (
    k8s.io/api v0.27.0
    k8s.io/apimachinery v0.27.0
    k8s.io/client-go v0.27.0
    k8s.io/code-generator v0.27.0  // For code-gen
    sigs.k8s.io/controller-runtime v0.15.0  // Alternative to raw client-go
)
```

---

## What This Enables

✅ **Dynamic Test Management**: Add/remove tests without restarting app
✅ **GitOps Native**: Tests in git, synced via ArgoCD
✅ **Persistent Definitions**: Test definitions survive pod crash
✅ **Status Tracking**: See last run status in CRD
✅ **Audit Trail**: All changes recorded in etcd
✅ **RBAC Control**: Fine-grained permissions per test
✅ **Multi-Cluster**: Works across multiple clusters
✅ **Extensible**: Easy to add new test types
✅ **Phase 4 Ready**: Historical data can feed into trend analysis

---

*Design Document: Kubernetes CRD-Based Smoke Test Controller*
*Status: READY FOR IMPLEMENTATION*
*Recommended: Start with Option B (Separate Deployment)*
