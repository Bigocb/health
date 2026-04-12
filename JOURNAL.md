# Health Reporter Project Journal

## Overview

The Health Reporter is a Kubernetes-native health monitoring system that:
- Collects metrics from Mimir (Prometheus-compatible)
- Runs smoke tests (HTTP, DNS, TCP) via CRD-based smoke tests
- Analyzes health status with LLM-powered insights (Ollama)
- Sends reports to Discord

**Repository**: `github.com/Bigocb/health`
**Current Version**: v0.5.0
**Location**: `C:\Users\bigoc\dev\health`

---

## Phase History

### Phase 1: Basic Health Reporter ✅ (COMPLETE)
- Mimir metrics collection (nodes, pods, resources)
- Health calculation engine (healthy/degraded/critical)
- Discord webhook integration
- YAML/config/CLI configuration
- Dockerfile + Kubernetes deployment ready

### Phase 2: Kubernetes-Native CRD Controller ✅ (COMPLETE)
- Implemented smoke test CRD (`SmokeTest` kind)
- DNS, HTTP, TCP test types
- Controller watches SmokeTest resources in namespace
- Config samples in `config/samples/`

### Phase 3: Scheduled Reporting ✅ (COMPLETE)
- Merged cronjob + controller into single daemon deployment
- Hourly reports via `--interval` flag
- Historical storage in JSON files

### Phase 4: LLM Analysis ✅ (COMPLETE)
- Integrated Ollama for LLM-powered health analysis
- Trend analysis with anomaly detection
- Enhanced Discord reports with AI insights

### Phase 5: Auto-Remediation (DESIGN COMPLETE, NOT IMPLEMENTED)
- Document in `PHASE5_AUTOREMEDIATION.md`
- Ruleset engine for detecting and fixing issues
- Safe actions: restart crashing pods, cleanup completed jobs

### Phase 6: Multi-Type Reporting Platform (DESIGN COMPLETE)
- Document in `PHASE6_REPORTING_PLATFORM.md`
- Multiple report types (health, capacity, security)
- Multiple destinations (Discord, email, Slack)
- Web UI for report management

---

## Current State (as of 2026-04-11)

### What's Deployed
- **Deployment**: `health-reporter` in `monitoring` namespace
- **Image**: `ghcr.io/bigocb/health:latest` (with `pullPolicy: Always`)
- **Schedule**: Hourly reports via daemon mode

### Active Smoke Tests

| Test Name | Type | Target | Status |
|-----------|------|--------|--------|
| kubernetes-dns | DNS | kubernetes.default | ✅ |
| kubernetes-api-health | HTTP | kubernetes.default:443/livez | ✅ |
| kubernetes-api | HTTP | kubernetes.default:443/healthz | ✅ |
| coredns-external | DNS | google.com | ✅ |
| grafana-health | HTTP | grafana.monitoring:3000/api/health | ✅ |
| higress-gateway | HTTP | higress-gateway.higress-system:80/healthz | ✅ |
| argocd-health | HTTP | argocd-server.argocd:8080/health | ✅ |
| mimir-health | HTTP | mimir-nginx.monitoring:80/ready | ✅ |
| minio-health | HTTP | minio.monitoring:9000 | ❌ (disabled) |
| kube-proxy-tcp | TCP | kube-proxy.kube-system:10249 | ❌ (disabled) |

### Disabled/Commented Tests

These tests were disabled because the services run as static pods or aren't accessible via service:

- **kubelet-health**: kubelet runs as static pod on each node, not accessible via service
- **scheduler-connectivity**: kube-scheduler runs as static pod on control plane
- **controller-manager-connectivity**: kube-controller-manager runs as static pod
- **ingress-nginx**: Using Higress instead (see `higress_test.yaml`)

---

## Recent Changes

### 2026-04-11: Smoke Test Fixes

1. **Fixed Higress test** - Changed from ingress-nginx to Higress gateway
   - Old: `http://ingress-nginx-controller.ingress-nginx.svc:10246/healthz`
   - New: `http://higress-gateway.higress-system.svc:80/healthz`

2. **Fixed ArgoCD test** - Was using wrong namespace/port
   - Old: `http://argocd-server.monitoring.svc:80/health` (wrong)
   - New: `http://argocd-server.argocd.svc:8080/health` (correct)

3. **Fixed Mimir test** - Wrong endpoint
   - Old: `http://mimir-nginx.monitoring.svc:80/prometheus Ready`
   - New: `http://mimir-nginx.monitoring.svc:80/ready`

4. **Deleted unreachable tests** - kubelet, scheduler, controller-manager (static pods)

5. **Created FUTURE_SMOKE_TESTS.md** - Research document for future tests (Loki, Tempo, NATS, etc.)

---

## Project Structure

```
health-reporter/
├── cmd/health-reporter/     # Main application entry point
├── pkg/
│   ├── mimir/              # Mimir metrics collection (expanded in v0.5.0)
│   ├── health/             # Health calculation engine
│   ├── webhook/discord.go  # Discord integration
│   ├── config/             # Configuration management
│   ├── smoke/              # Smoke test runner
│   ├── analysis/           # LLM prompts for Discord reports
│   └── types/              # Shared type definitions
├── config/
│   └── samples/            # Example SmokeTest CRDs
├── helm/health-reporter/   # Helm chart for deployment
└── .github/workflows/      # CI/CD (build-push-ghcr.yml)
```

---

## Configuration

### Helm Values (`helm/health-reporter/values.yaml`)

```yaml
image:
  repository: ghcr.io/bigocb/health
  tag: latest
  pullPolicy: Always

reporting:
  interval: "1h"

analysis:
  enabled: true
  llm:
    enabled: true
    provider: "ollama"
    model: "llama3.2:1b"
    endpoint: "http://ollama.ollama.svc.cluster.local:11434"
```

### Smoke Test CRD Example

```yaml
apiVersion: health.archipelago.ai/v1alpha1
kind: SmokeTest
metadata:
  name: higress-gateway
  namespace: monitoring
spec:
  type: http
  enabled: true
  url: "http://higress-gateway.higress-system.svc:80/healthz"
  expectedStatus: 200
```

---

## CI/CD

### GitHub Actions Workflow
- **File**: `.github/workflows/build-push-ghcr.yml`
- **Trigger**: Push to main branch
- **Registry**: GHCR (`ghcr.io/bigocb/health`)
- **Tags**: `latest`, semver (`v1.2.3`), sha prefix

### Deployment
- Not managed via ArgoCD (configs repo)
- Must be deployed manually: `helm install health-reporter ./helm/health-reporter -n monitoring`
- Image auto-updates on restart due to `pullPolicy: Always`

---

## Current Issues (2026-04-11)

### Failing Smoke Tests (2026-04-11 Evening)

| Test Name | Status | Error | Action Needed |
|-----------|--------|-------|---------------|
| kubernetes-api-health | ❌ FAIL | HTTP 401 (expected 200) | Needs ServiceAccount token auth |
| kubernetes-api | ❌ FAIL | HTTP 401 (expected 200) | Needs ServiceAccount token auth |
| mimir-health | ❌ FAIL | HTTP 404 (expected 200) | Verify endpoint path |
| higress-gateway | ❌ FAIL | HTTP 404 (expected 200) | Verify endpoint path |
| grafana-health | ❌ FAIL | Connection timeout (5s) | Check if Grafana is accessible |

### Passing Tests

| Test Name | Status |
|-----------|--------|
| kubernetes-dns | ✅ PASS |
| coredns-external | ✅ PASS |
| argocd-health | ✅ PASS |

### Pod Metrics Bug

**Issue**: Pod counts showing incorrect values
- Running: 164
- Pending: 164 (should be low, not equal to running!)
- Failed: 164 (should be low, not equal to running!)

**Status**: Investigating - queries look correct but all return same value

---

## Future Work

### High Priority
1. Add Loki health check (`http://loki.monitoring.svc:3100/ready`)
2. Add Tempo health check (`http://tempo.monitoring.svc:3200/ready`)
3. Add NATS connectivity test (`nats.mcp-servers:4222`)

### Medium Priority
4. PostgreSQL (fresnel) TCP test
5. Redis (fresnel) TCP test
6. Add Kargo health check

### Lower Priority
7. Mimir query latency test
8. Grafana Alloy OTLP endpoint test

### Research Needed
9. **Data Persistence**: Currently reports stored in pod's filesystem (`/var/lib/health-reporter/reports`). If pod restarts, history is lost.
   - Options: PVC, external DB (PostgreSQL), object storage (S3/Minio), separate reporting statefulset

---

## Key Files

| File | Purpose |
|------|---------|
| `PHASE1_COMPLETE.md` | Phase 1 implementation summary |
| `PHASE5_AUTOREMEDIATION.md` | Auto-remediation design (not implemented) |
| `PHASE6_REPORTING_PLATFORM.md` | Multi-type reporting design (not implemented) |
| `FUTURE_SMOKE_TESTS.md` | Research for future smoke tests |
| `config/samples/extended_tests.yaml` | Main smoke test definitions |
| `config/samples/higress_test.yaml` | Higress-specific test |
| `helm/health-reporter/values.yaml` | Deployment configuration |

---

## Notes

- Uses MicroK8s cluster
- No ArgoCD in this repo (deployed separately via configs repo)
- LLM analysis uses Ollama at `ollama.ollama.svc.cluster.local:11434`
- Mimir at `mimir-nginx.monitoring.svc.cluster.local:80/prometheus`
- Smoke tests run as part of the main daemon, not separate cronjobs

---

*Last updated: 2026-04-11*
*Version: v0.5.0*