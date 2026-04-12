# Health Reporter: Kubernetes-Native Architecture (Master Plan)

## Vision

Build Health Reporter as a **Kubernetes Operator** following industry best practices:
- CRD-based test definitions
- Controller pattern for dynamic reconciliation
- Minimal external dependencies
- GitOps-friendly configuration
- Production-grade error handling

---

## Architecture Overview

### Components

```
┌─────────────────────────────────────────────────────────┐
│           Kubernetes Cluster                             │
├─────────────────────────────────────────────────────────┤
│                                                          │
│  ┌──────────────────────────────────────────────────┐   │
│  │  CRDs (Tests stored in etcd)                     │   │
│  │  - SmokeTest resources                           │   │
│  │  - HealthReport resources (optional)             │   │
│  └──────────────────────────────────────────────────┘   │
│                                                          │
│  ┌──────────────────────────────────────────────────┐   │
│  │  Health Reporter Operator Deployment             │   │
│  │  ┌──────────────────────────────────────────┐    │   │
│  │  │  Controller:                             │    │   │
│  │  │  - Watch SmokeTest CRDs                  │    │   │
│  │  │  - Reconcile test definitions            │    │   │
│  │  │  - Update status subresource             │    │   │
│  │  │  - Record events                         │    │   │
│  │  └──────────────────────────────────────────┘    │   │
│  │                                                   │    │
│  │  ┌──────────────────────────────────────────┐    │   │
│  │  │  Health Reporter:                        │    │   │
│  │  │  - Collect Mimir metrics                 │    │   │
│  │  │  - Run tests from controller             │    │   │
│  │  │  - Generate health report                │    │   │
│  │  │  - Send to Discord/webhooks              │    │   │
│  │  └──────────────────────────────────────────┘    │   │
│  └──────────────────────────────────────────────────┘   │
│                                                          │
│  ┌──────────────────────────────────────────────────┐   │
│  │  CronJob (scheduler)                             │   │
│  │  - Triggers hourly reports                       │   │
│  │  - Runs health-reporter in --once mode           │   │
│  └──────────────────────────────────────────────────┘   │
│                                                          │
└─────────────────────────────────────────────────────────┘
```

### Data Flow

```
1. DEPLOYMENT:
   - Install CRD via Helm
   - Deploy controller (Deployment)
   - Deploy CronJob
   - Create SmokeTest resources (via GitOps/kubectl)

2. RUNTIME (Continuous):
   - Controller watches SmokeTest resources
   - Controller builds dynamic test registry
   - Test definitions auto-update when CRDs change

3. HOURLY (Triggered by CronJob):
   - CronJob creates Job pod
   - Pod queries Mimir for metrics
   - Pod gets test list from controller
   - Pod runs all tests in parallel
   - Pod generates report with metrics + test results
   - Pod sends to Discord
   - Pod exits

4. MONITORING:
   - kubectl get smoketests
   - kubectl describe smoketest <name>
   - kubectl logs deployment/health-reporter-controller
```

---

## Phase 2: Kubernetes Operator Implementation

### Phase 2.0: Project Setup (1-2 days)

#### 2.0.1: Initialize Operator Structure
```
health-reporter/
├── api/
│   └── v1alpha1/
│       ├── smoketest_types.go      # CRD definition
│       ├── groupversion_info.go    # API group info
│       └── zz_generated.deepcopy.go # Auto-generated
├── config/
│   ├── crd/                        # CRD manifests
│   ├── manager/                    # Controller deployment
│   ├── rbac/                       # RBAC policies
│   └── samples/                    # Example CRDs
├── controllers/
│   └── smoketest_controller.go     # Main controller logic
├── hack/
│   └── boilerplate.go.txt          # Copyright header
├── Dockerfile                      # Multi-stage build
├── Makefile                        # Build automation
└── go.mod / go.sum
```

#### 2.0.2: Add Go Dependencies
```bash
go get k8s.io/api@v0.27.0
go get k8s.io/apimachinery@v0.27.0
go get k8s.io/client-go@v0.27.0
go get sigs.k8s.io/controller-runtime@v0.15.0
```

### Phase 2.1: Define CRD API Types (1-2 days)

#### 2.1.1: Create SmokeTest CRD Type
File: `api/v1alpha1/smoketest_types.go`

```go
package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// SmokeTestSpec defines the desired state
type SmokeTestSpec struct {
    Type           string            `json:"type"` // dns, http, tcp
    Enabled        bool              `json:"enabled"`
    Severity       string            `json:"severity"` // critical, high, medium, low
    Timeout        string            `json:"timeout"`
    
    // DNS fields
    Domain         string            `json:"domain,omitempty"`
    
    // HTTP fields
    URL            string            `json:"url,omitempty"`
    Method         string            `json:"method,omitempty"`
    ExpectedStatus int               `json:"expectedStatus,omitempty"`
    TLSInsecure    bool              `json:"tlsInsecure,omitempty"`
    Headers        map[string]string `json:"headers,omitempty"`
    
    // TCP fields
    Host           string            `json:"host,omitempty"`
    Port           int               `json:"port,omitempty"`
}

// SmokeTestStatus defines the observed state
type SmokeTestStatus struct {
    LastRun       *metav1.Time `json:"lastRun,omitempty"`
    LastStatus    string       `json:"lastStatus,omitempty"` // pass, fail, timeout
    LastMessage   string       `json:"lastMessage,omitempty"`
    PassCount     int          `json:"passCount"`
    FailCount     int          `json:"failCount"`
}

// SmokeTest is the Schema for the smoketests API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type SmokeTest struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    
    Spec   SmokeTestSpec   `json:"spec,omitempty"`
    Status SmokeTestStatus `json:"status,omitempty"`
}

// SmokeTestList contains a list of SmokeTest
// +kubebuilder:object:root=true
type SmokeTestList struct {
    metav1.TypeMeta `json:",inline"`
    metav1.ListMeta `json:"metadata,omitempty"`
    Items           []SmokeTest `json:"items"`
}
```

#### 2.1.2: Generate Code
```bash
# Install code-generator
go install k8s.io/code-generator/cmd/deepcopy-gen@latest

# Generate deepcopy
deepcopy-gen \
  --go-header-file=hack/boilerplate.go.txt \
  --input-dirs=./api/v1alpha1
```

### Phase 2.2: Implement Controller (2-3 days)

#### 2.2.1: Core Controller Logic
File: `controllers/smoketest_controller.go`

```go
package controllers

import (
    "context"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/predicate"
    
    healthv1alpha1 "github.com/ArchipelagoAI/health-reporter/api/v1alpha1"
    "github.com/ArchipelagoAI/health-reporter/pkg/smoke_tests"
)

// SmokeTestReconciler reconciles a SmokeTest object
type SmokeTestReconciler struct {
    client.Client
    testRegistry *smoke_tests.TestRegistry
}

// +kubebuilder:rbac:groups=health.archipelago.ai,resources=smoketests,verbs=get;list;watch
// +kubebuilder:rbac:groups=health.archipelago.ai,resources=smoketests/status,verbs=get;patch;update
// +kubebuilder:rbac:groups=health.archipelago.ai,resources=smoketests/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *SmokeTestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // Get the SmokeTest resource
    var smokeTest healthv1alpha1.SmokeTest
    if err := r.Get(ctx, req.NamespacedName, &smokeTest); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    
    // Convert CRD to test config
    testConfig := r.crToTestConfig(&smokeTest)
    
    // Create test runner
    if smokeTest.Spec.Enabled {
        // Add/update in registry
        testRunner, err := r.testRegistry.CreateTest(testConfig)
        if err != nil {
            return ctrl.Result{}, err
        }
        // Store in registry...
    } else {
        // Remove from registry...
    }
    
    // Update status
    smokeTest.Status.LastRun = metav1.Now()
    if err := r.Status().Update(ctx, &smokeTest); err != nil {
        return ctrl.Result{}, err
    }
    
    return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller
func (r *SmokeTestReconciler) SetupWithManager(mgr ctrl.Manager) error {
    // Filter for spec changes only
    predicate := predicate.GenerationChangedPredicate{}
    
    return ctrl.NewControllerManagedBy(mgr).
        For(&healthv1alpha1.SmokeTest{}).
        WithEventFilter(predicate).
        Complete(r)
}
```

#### 2.2.2: Test Registry Integration
```go
// TestRegistry becomes a shared resource
type SharedTestRegistry struct {
    mu    sync.RWMutex
    tests map[string]smoke_tests.TestRunner
}

func (r *SharedTestRegistry) GetAllTests() []smoke_tests.TestRunner {
    r.mu.RLock()
    defer r.mu.RUnlock()
    
    var result []smoke_tests.TestRunner
    for _, test := range r.tests {
        result = append(result, test)
    }
    return result
}

func (r *SharedTestRegistry) UpdateTest(key string, test smoke_tests.TestRunner) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.tests[key] = test
}

func (r *SharedTestRegistry) RemoveTest(key string) {
    r.mu.Lock()
    defer r.mu.Unlock()
    delete(r.tests, key)
}
```

### Phase 2.3: Main App Integration (1 day)

#### 2.3.1: Run Controller & Reporter in Same Pod
File: `cmd/health-reporter/main.go`

```go
func main() {
    mgr, err := ctrl.NewManager(config, ctrl.Options{...})
    if err != nil {
        log.Fatal(err)
    }
    
    // Shared test registry
    testRegistry := &SharedTestRegistry{tests: make(map[string]smoke_tests.TestRunner)}
    
    // Setup controller
    if err = (&SmokeTestReconciler{
        Client:       mgr.GetClient(),
        testRegistry: testRegistry,
    }).SetupWithManager(mgr); err != nil {
        log.Fatal(err)
    }
    
    // Start controller in background
    go func() {
        if err := mgr.Start(ctx); err != nil {
            log.Fatal(err)
        }
    }()
    
    // Wait for controller to cache
    if !mgr.GetCache().WaitForCacheSync(ctx) {
        log.Fatal("timeout waiting for cache sync")
    }
    
    // Run health reporter
    runHealthReporter(ctx, testRegistry)
}
```

### Phase 2.4: Kubernetes Manifests (1 day)

#### 2.4.1: CRD Manifest
File: `config/crd/smoketest_crd.yaml`

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
  scope: Namespaced
  versions:
  - name: v1alpha1
    served: true
    storage: true
    subresources:
      status: {}
    schema:
      openAPIV3Schema:
        type: object
        properties:
          spec: ...
          status: ...
```

#### 2.4.2: Controller Deployment
File: `config/manager/manager.yaml`

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: health-reporter-controller
  namespace: monitoring
spec:
  replicas: 1
  selector:
    matchLabels:
      app: health-reporter-controller
  template:
    metadata:
      labels:
        app: health-reporter-controller
    spec:
      serviceAccountName: health-reporter
      containers:
      - name: controller
        image: health-reporter:latest
        args:
        - --mode=controller
        env:
        - name: WATCH_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
```

#### 2.4.3: RBAC
File: `config/rbac/role.yaml`

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: health-reporter-controller
rules:
- apiGroups: ["health.archipelago.ai"]
  resources: ["smoketests"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["health.archipelago.ai"]
  resources: ["smoketests/status"]
  verbs: ["get", "patch", "update"]
- apiGroups: [""]
  resources: ["events"]
  verbs: ["create", "patch"]
```

### Phase 2.5: Helm Chart Integration (1 day)

#### 2.5.1: Update values.yaml
```yaml
controller:
  enabled: true
  image: health-reporter:latest
  replicas: 1

crd:
  install: true

smokeTests: []  # No longer used - tests defined via CRDs

# Sample tests to install
samples:
  enabled: true
```

#### 2.5.2: Helm Templates
```
helm/health-reporter/
├── templates/
│   ├── crd.yaml              # CRD definition
│   ├── controller-deployment.yaml
│   ├── rbac.yaml
│   ├── samples/
│   │   ├── dns-test.yaml     # Example SmokeTest
│   │   ├── http-test.yaml
│   │   └── tcp-test.yaml
│   └── cronjob.yaml          # CronJob (unchanged)
└── values.yaml
```

---

## Implementation Timeline

| Phase | Task | Duration | Status |
|-------|------|----------|--------|
| 2.0 | Project setup, dependencies | 1-2d | Ready |
| 2.1 | CRD types, code-gen | 1-2d | Ready |
| 2.2 | Controller implementation | 2-3d | Ready |
| 2.3 | App integration | 1d | Ready |
| 2.4 | K8s manifests | 1d | Ready |
| 2.5 | Helm chart | 1d | Ready |
| 2.6 | Testing, documentation | 2-3d | Ready |

**Total: ~10-13 days** for full implementation

---

## Phase 2 Deliverables

✅ **CRD Definition**: SmokeTest resource
✅ **Controller**: Watches and reconciles tests
✅ **Integration**: Health reporter uses controller tests
✅ **Deployment**: Separate controller pod
✅ **Helm Chart**: Full operator deployment
✅ **Examples**: Sample test resources
✅ **Documentation**: How to add/update tests
✅ **RBAC**: Proper permissions

---

## Phase 3: Historical Data & Caching

After Phase 2, we can add:

```yaml
apiVersion: health.archipelago.ai/v1alpha1
kind: SmokeTestResult
metadata:
  name: test-result-2026-04-12-00-00
  namespace: monitoring
spec:
  testName: kubernetes-api-dns
  timestamp: 2026-04-12T00:00:00Z
  status: pass
  duration: 15ms
```

This enables:
- Full audit trail
- 24-48 hour history
- Trend analysis for Phase 4
- Survives pod crash

---

## What Makes This Kubernetes-Native

✅ **CRDs**: Test definitions stored in etcd
✅ **Controller Pattern**: Watches and reconciles
✅ **RBAC**: Fine-grained permissions
✅ **Status Subresource**: Live status tracking
✅ **Events**: Audit trail via K8s events
✅ **Labels/Annotations**: K8s metadata
✅ **Namespaced**: Multi-tenant support
✅ **GitOps Ready**: Apply via git
✅ **kubectl Native**: Full kubectl integration
✅ **No External DB**: All in K8s

---

## Quick Start (After Implementation)

```bash
# Install
helm install health-reporter ./helm/health-reporter \
  -n monitoring

# View CRD
kubectl get smoketests

# Add test
kubectl apply -f - <<EOF
apiVersion: health.archipelago.ai/v1alpha1
kind: SmokeTest
metadata:
  name: my-api
  namespace: monitoring
spec:
  type: http
  url: https://my-api.example.com/health
  severity: high
EOF

# Monitor
kubectl describe smoketest my-api

# Logs
kubectl logs deployment/health-reporter-controller
```

---

*Master Implementation Plan: Kubernetes-Native Health Reporter*
*Architecture: Operator Pattern with Controller-Runtime*
*Status: READY FOR IMPLEMENTATION*
