# Phase 2 Design: Smoke Test Framework

## Overview

**Objective**: Build a modular, expandable smoke test framework that:
1. Tests critical cluster services (DNS, HTTP, TCP)
2. Integrates test results into health reports
3. Easy to add new test types without modifying core logic
4. Configuration-driven (add tests via YAML)
5. Provides actionable failure insights

**Architecture**: Plugin-style test runners allowing:
- Service tests run independently
- Test results aggregated into health report
- Configurable test parameters (endpoints, ports, timeouts)
- Extensible for new test types

---

## Phase 2 Components

### 1. Test Framework Architecture

**Design Pattern**: Interface-based test runners

```go
// TestRunner interface - all tests implement this
type TestRunner interface {
    Name() string                           // Unique test identifier
    Category() string                       // "connectivity", "http", "dns"
    Run(ctx context.Context) *TestResult
}

// TestResult - standardized output
type TestResult struct {
    Name          string        // Test name
    Category      string        // Test category
    Status        string        // "pass", "fail", "timeout", "error"
    Duration      time.Duration // How long test took
    Message       string        // Human-readable result
    ErrorDetails  string        // Error info if failed
    Timestamp     time.Time     // When test ran
    Metadata      map[string]string  // Additional context
}
```

**Benefits**:
- New test types only need to implement interface
- Tests run independently (parallelizable)
- Results have consistent format
- Easy to filter/aggregate/report

---

### 2. Test Types (Initial)

#### Test Type 1: DNS Resolution
```go
// DNSTest checks if domain resolves correctly
type DNSTest struct {
    Name      string        // e.g., "DNS: kubernetes.default"
    Domain    string        // Domain to resolve
    Expected  []string      // Expected IPs (optional)
    Timeout   time.Duration // Query timeout
}

// Example configuration:
// {
//   "type": "dns",
//   "name": "DNS: Kubernetes API",
//   "domain": "kubernetes.default.svc.cluster.local",
//   "timeout": "5s"
// }
```

**What it tests**:
- DNS resolver working
- Service discovery functional
- Network connectivity to DNS

**Failure indicators**:
- NXDOMAIN (service doesn't exist)
- Timeout (DNS server not responding)
- Empty result (misconfigured DNS)

---

#### Test Type 2: HTTP/HTTPS Endpoint
```go
// HTTPTest checks if endpoint responds
type HTTPTest struct {
    Name           string        // e.g., "HTTP: Kubernetes API"
    URL            string        // Full URL
    Method         string        // GET, POST, HEAD
    ExpectedStatus int           // Expected HTTP status (e.g., 200)
    Timeout        time.Duration // Request timeout
    Headers        map[string]string // Optional headers
    TLSInsecure    bool          // Skip cert verification
}

// Example configuration:
// {
//   "type": "http",
//   "name": "HTTP: Kubernetes API",
//   "url": "https://kubernetes.default.svc.cluster.local/api",
//   "expected_status": 200,
//   "timeout": "10s",
//   "tls_insecure": true
// }
```

**What it tests**:
- Service endpoint reachable
- Service responding to requests
- HTTP layer functional
- TLS certificates valid (if enabled)

**Failure indicators**:
- Connection refused (service down)
- Timeout (service hung)
- Wrong status code (service unhealthy)
- TLS errors (cert issues)

---

#### Test Type 3: TCP Port Connectivity
```go
// TCPTest checks if port is open and responding
type TCPTest struct {
    Name    string        // e.g., "TCP: Etcd Port"
    Host    string        // Hostname or IP
    Port    int           // Port number
    Timeout time.Duration // Connection timeout
}

// Example configuration:
// {
//   "type": "tcp",
//   "name": "TCP: Etcd",
//   "host": "localhost",
//   "port": 2379,
//   "timeout": "5s"
// }
```

**What it tests**:
- Port open and listening
- Service bound to port
- Network connectivity at TCP layer

**Failure indicators**:
- Connection refused (port not open)
- Timeout (firewall blocking or slow)
- Host unreachable

---

### 3. Configuration Schema

**Location**: `config.yaml` (add new section)

```yaml
smoke_tests:
  enabled: true
  timeout_global: 30s  # Max time for all tests
  
  tests:
    # DNS Tests
    - type: dns
      name: "DNS: Kubernetes API"
      domain: "kubernetes.default.svc.cluster.local"
      timeout: 5s
      severity: critical  # critical, high, medium, low
    
    - type: dns
      name: "DNS: Coredns Service"
      domain: "kube-dns.kube-system.svc.cluster.local"
      timeout: 5s
      severity: high
    
    # HTTP Tests
    - type: http
      name: "HTTP: Kubernetes API"
      url: "https://kubernetes.default.svc.cluster.local/api"
      method: GET
      expected_status: 200
      timeout: 10s
      tls_insecure: true
      severity: critical
    
    - type: http
      name: "HTTP: Prometheus"
      url: "http://prometheus.monitoring.svc.cluster.local:9090/-/healthy"
      method: GET
      expected_status: 200
      timeout: 5s
      severity: high
    
    - type: http
      name: "HTTP: Grafana"
      url: "http://grafana.monitoring.svc.cluster.local:3000/api/health"
      method: GET
      expected_status: 200
      timeout: 5s
      severity: medium
    
    # TCP Tests
    - type: tcp
      name: "TCP: Etcd"
      host: "localhost"
      port: 2379
      timeout: 5s
      severity: critical
    
    - type: tcp
      name: "TCP: Kubelet API"
      host: "localhost"
      port: 10250
      timeout: 5s
      severity: high
```

**Configuration Features**:
- Type-specific parameters
- Per-test timeout and severity
- Global timeout for all tests
- Easy to enable/disable entire suite

---

### 4. Test Runner & Result Aggregation

```go
// SmokeTestSuite manages all tests
type SmokeTestSuite struct {
    Tests    []TestRunner
    Timeout  time.Duration
    Results  []*TestResult
}

// Run executes all tests (parallelized)
func (s *SmokeTestSuite) Run(ctx context.Context) (*SmokeTestReport, error) {
    // Run tests concurrently with timeout
    // Collect results
    // Calculate overall status
    // Return structured report
}

// SmokeTestReport - aggregated results
type SmokeTestReport struct {
    Timestamp      time.Time
    TotalTests     int
    PassedTests    int
    FailedTests    int
    Status         string  // "pass", "degraded", "critical"
    Results        []*TestResult
    Summary        string
    FailureSummary []string  // List of failed tests with details
}
```

---

### 5. Integration into Health Reporter

**Current Flow**:
```
Metrics → Health Status → Discord Report
```

**Phase 2 Flow**:
```
Metrics → Health Status ──┐
                         ├→ Run Smoke Tests (parallel) ──┐
                                                         ├→ Combine Results ──→ Discord Report
                         ─────────────────────────────────┘
```

**Health Report Enhancement**:

```json
{
  "timestamp": "2026-04-12T00:00:00Z",
  "status": "degraded",
  "metrics": { /* existing metrics */ },
  "smoke_tests": {
    "timestamp": "2026-04-12T00:00:00Z",
    "total_tests": 7,
    "passed_tests": 6,
    "failed_tests": 1,
    "status": "degraded",
    "failed_services": [
      {
        "name": "TCP: Etcd",
        "severity": "critical",
        "error": "connection refused",
        "details": "Port 2379 not responding"
      }
    ]
  }
}
```

**Discord Report Enhancement**:
- Add "Smoke Tests" field with pass/fail count
- If any failures, add them to "Concerns"
- Adjust overall status based on test failures:
  - Critical test failures → Critical status
  - High test failures → Degraded status
  - Medium/Low failures → Degraded status

---

### 6. Project Structure

```
health-reporter/
├── pkg/
│   ├── smoke_tests/
│   │   ├── smoke_tests.go         # Core framework (100 lines)
│   │   ├── types.go               # Shared types (80 lines)
│   │   ├── runner.go              # Test runner logic (150 lines)
│   │   ├── tests/
│   │   │   ├── dns_test.go        # DNS test impl (80 lines)
│   │   │   ├── http_test.go       # HTTP test impl (120 lines)
│   │   │   ├── tcp_test.go        # TCP test impl (80 lines)
│   │   │   └── smoke_tests_test.go # Unit tests (200 lines)
│   │   └── fixtures/
│   │       └── test_config.yaml   # Test data
│   ├── health/ (modified)
│   │   └── health.go              # Add smoke test integration
│   ├── config/ (modified)
│   │   └── config.go              # Add smoke test config parsing
│   └── ...
└── ...
```

---

### 7. Making it Expandable

#### Option A: Registry Pattern (Simple)
```go
// Test registry for easy discovery
type TestRegistry struct {
    factories map[string]func(config TestConfig) TestRunner
}

// Register new test type
func (r *TestRegistry) Register(testType string, factory func(config TestConfig) TestRunner) {
    r.factories[testType] = factory
}

// Create test from config
func (r *TestRegistry) CreateTest(config TestConfig) TestRunner {
    factory := r.factories[config.Type]
    return factory(config)
}

// In main.go:
registry := NewTestRegistry()
registry.Register("dns", func(c TestConfig) TestRunner { return NewDNSTest(c) })
registry.Register("http", func(c TestConfig) TestRunner { return NewHTTPTest(c) })
registry.Register("tcp", func(c TestConfig) TestRunner { return NewTCPTest(c) })
// Add new types here without modifying core logic!
```

**To add new test type**:
1. Create `tests/new_type_test.go`
2. Implement `TestRunner` interface
3. Register in `main.go`
4. Add test config to `config.yaml`

---

#### Option B: Plugin Loader (Advanced, future)
For Phase 2.5+:
- Load test implementations from external packages
- No recompilation needed for new tests
- Enables community contributions

---

## Implementation Plan

### Phase 2.1: Core Framework (Day 1-2)
- [ ] Create `pkg/smoke_tests/` package
- [ ] Define `TestRunner` interface
- [ ] Implement test result types
- [ ] Create test registry
- [ ] Add parallel test execution

### Phase 2.2: Initial Test Types (Day 2-3)
- [ ] Implement DNS test
- [ ] Implement HTTP test
- [ ] Implement TCP test
- [ ] Unit tests for each

### Phase 2.3: Configuration & Integration (Day 3-4)
- [ ] Parse smoke test config from YAML
- [ ] Integrate into health reporter flow
- [ ] Modify health status calculation with test results
- [ ] Update Discord formatter

### Phase 2.4: Testing & Documentation (Day 4-5)
- [ ] End-to-end tests
- [ ] Documentation for adding tests
- [ ] Example configurations
- [ ] Troubleshooting guide

---

## Success Criteria for Phase 2

✅ **Framework Complete**: All 3 test types working
✅ **Configuration Driven**: Tests added via YAML, not code
✅ **Expandable**: New test type takes <15 minutes to add
✅ **Integrated**: Smoke test results in health reports
✅ **Parallel**: All tests run concurrently
✅ **Reliable**: No flaky tests, consistent results
✅ **Discord Ready**: Test failures show in Discord reports

---

## Example Test Scenarios

### Scenario 1: Healthy Cluster
```
✅ DNS: Kubernetes API - PASS (2ms)
✅ DNS: Coredns Service - PASS (3ms)
✅ HTTP: Kubernetes API - PASS (15ms)
✅ HTTP: Prometheus - PASS (25ms)
✅ HTTP: Grafana - PASS (18ms)
✅ TCP: Etcd - PASS (5ms)
✅ TCP: Kubelet - PASS (8ms)

Overall: 7/7 PASS ✅
```

### Scenario 2: Degraded Cluster
```
✅ DNS: Kubernetes API - PASS (2ms)
✅ DNS: Coredns Service - PASS (3ms)
❌ HTTP: Kubernetes API - FAIL (timeout after 10s)
✅ HTTP: Prometheus - PASS (25ms)
⚠️  HTTP: Grafana - PASS (2500ms - slow)
✅ TCP: Etcd - PASS (5ms)
✅ TCP: Kubelet - PASS (8ms)

Overall: 6/7 PASS, 1 FAIL ⚠️
```

---

## Notes

### Testing Approach
- Use mock HTTP servers in tests
- Mock DNS resolution
- Real TCP tests against localhost
- CI/CD runs integration tests in container

### Error Handling
- Timeout: graceful error message
- Connection refused: clear "service down" message
- DNS error: "service not found" message
- TLS error: specific cert error details

### Performance
- Tests run in parallel (max 30s for all)
- Each test has independent timeout
- Results cached during report generation
- No impact on metrics collection

### Future Extensions
- Custom headers in HTTP tests
- Request body validation
- Response body pattern matching
- Load testing (throughput/latency)
- Latency SLA checks
- Certificate expiration checks
- Authentication tests

---

*Design Document: Phase 2 Smoke Test Framework*
*Status: READY FOR IMPLEMENTATION*
