# Future Smoke Tests Research

## Current Smoke Tests (Active)

| Test Name | Type | Target | Status |
|-----------|------|--------|---------|
| kubernetes-dns | DNS | kubernetes.default | ✅ |
| kubernetes-api-health | HTTP | kubernetes.default:443/livez | ✅ |
| etcd-tcp | TCP | etcd.default:2379 | ✅ |
| kubernetes-api | HTTP | kubernetes.default:443/healthz | ✅ |
| kubelet-health | HTTP | localhost:10250/healthz | ✅ |
| coredns-external | DNS | google.com | ✅ |
| grafana-health | HTTP | grafana.monitoring:3000/api/health | ✅ |
| mimir-health | HTTP | mimir-nginx.monitoring:80/prometheus Ready | ✅ |
| argocd-health | HTTP | argocd-server.argocd:8080/health | ✅ |
| minio-health | HTTP | minio.monitoring:9000/minio/health/live | ❌ (disabled) |
| higress-gateway | HTTP | higress-gateway.higress-system:80/healthz | ✅ |
| etcd-connectivity | TCP | etcd.default:2379 | ✅ (duplicate) |
| scheduler-connectivity | TCP | kube-scheduler.kube-system:10259 | ✅ |
| controller-manager-connectivity | TCP | kube-controller-manager.kube-system:10257 | ✅ |
| kube-proxy-tcp | TCP | kube-proxy.kube-system:10249 | ❌ (disabled) |

---

## Service Map & Future Tests

### Core Kubernetes (Already covered)
- ✅ kube-apiserver
- ✅ etcd
- ✅ kube-scheduler
- ✅ kube-controller-manager
- ✅ kubelet
- ✅ kube-proxy
- ✅ CoreDNS

### Observability Stack

| Service | Namespace | Purpose | Current Tests | Future Tests |
|---------|------------|---------|----------------|---------------|
| **Grafana** | monitoring | Metrics visualization | HTTP /api/health | None needed |
| **Mimir** | monitoring | Metrics storage | HTTP /prometheus Ready | Query latency test |
| **Loki** | monitoring | Log aggregation | None | HTTP /ready endpoint |
| **Tempo** | monitoring | Trace storage | None | HTTP /ready |
| **Prometheus** | monitoring | Metrics collection | None | Target scrape success |
| **Alloy** | monitoring | Metrics pipeline | None | OTLP endpoint health |
| **Higress** | higress-system | API Gateway | HTTP /healthz | DNS resolution, TLS handshake |

### Messaging

| Service | Namespace | Purpose | Future Tests |
|---------|------------|---------|---------------|
| **NATS** | mcp-servers | Message queue | TCP connectivity (4222) |
| **NATS Monitor** | mcp-servers | NATS monitoring | HTTP :9090/healthz |

### Application Stack (Fresnel)

| Component | Namespace | Purpose | Future Tests |
|-----------|------------|---------|---------------|
| **Fresnel Backend** | fresnel | API backend | HTTP /health, DB connectivity |
| **Fresnel Frontend** | fresnel | Web UI | HTTP /health |
| **PostgreSQL** | fresnel | Database | TCP 5432 connectivity |
| **Redis** | fresnel | Cache | TCP 6379 connectivity |

### External Dependencies

| Service | Type | Future Tests |
|---------|------|---------------|
| **Cloudflare** | DNS/API | External DNS resolution |
| **GitHub** | API | Rate limit status, API connectivity |

---

## Recommended Future Tests by Priority

### High Priority (Core Services)

1. **Loki** - Add HTTP test to `http://loki.monitoring.svc:3100/ready`
2. **Tempo** - Add HTTP test to `http://tempo.monitoring.svc:4318/ready`
3. **NATS** - Add TCP test to `nats.mcp-servers:4222`

### Medium Priority (Application)

4. **PostgreSQL (fresnel)** - TCP test to `postgres.fresnel:5432`
5. **Redis (fresnel)** - TCP test to `redis.fresnel:6379`
6. **Alloy** - Test OTLP receiver endpoint

### Lower Priority (Monitoring)

7. **Higress metrics** - Test prometheus metrics endpoint
8. **Grafana Alloy** - Test River config reload
9. **Mimir query latency** - Custom query time test

---

## Tests to Remove (Duplicates/Not Needed)

1. **etcd-connectivity** - Duplicate of existing etcd-tcp
2. **kubernetes-api** - Very similar to kubernetes-api-health, may want to consolidate

---

## Notes

- Some services may not have standard /health endpoints - may need to check documentation
- TLS tests for external services (Cloudflare API) would require managing certificates
- Database connectivity tests need to handle authentication

---

*Document created: 2026-04-12*
*Status: RESEARCH COMPLETE*