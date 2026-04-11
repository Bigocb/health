# Health Reporter

A Kubernetes cluster health monitoring tool that collects metrics from Mimir and sends intelligent reports to Discord (and future integrations).

## Features

### Phase 1 (Current)
- ✅ Mimir metrics collection (nodes, pods, resources)
- ✅ Health status calculation (healthy/degraded/critical)
- ✅ Discord webhook integration
- ✅ Configurable thresholds
- ✅ Daemon mode or one-off execution

### Phase 2 (Planned)
- Smoke test suite (DNS, HTTP, TCP connectivity)
- Integration of smoke test results into reports

### Phase 3 (Planned)
- Multi-channel output (Slack, generic webhooks)
- Per-channel configuration

### Phase 4 (Planned)
- LLM integration (Ollama with Llama 3.2 1B)
- Intelligent cluster analysis and recommendations
- JSON-formatted LLM output

### Phase 5 (Planned)
- Trend analysis and historical tracking
- Performance degradation detection

## Quick Start

### Prerequisites
- Go 1.21+
- Access to Mimir query endpoint
- Discord webhook URL (for reporting)

### Build

```bash
go build -o health-reporter ./cmd/health-reporter
```

### Run Once (Test Mode)

```bash
./health-reporter --once \
  --mimir-url "http://localhost:9009" \
  --discord-webhook "https://discord.com/api/webhooks/..."
```

### Run as Daemon (Hourly Reports)

```bash
./health-reporter \
  --mimir-url "http://localhost:9009" \
  --discord-webhook "https://discord.com/api/webhooks/..." \
  --interval 1h
```

## Configuration

### Via Environment Variables
```bash
export DISCORD_WEBHOOK_URL="https://discord.com/api/webhooks/..."
./health-reporter
```

### Via Config File
```bash
./health-reporter --config config.yaml
```

See `config.yaml.example` for format.

### Via Command-Line Flags
```bash
./health-reporter \
  --mimir-url "http://mimir-query:9009" \
  --discord-webhook "https://discord.com/api/webhooks/..." \
  --interval 30m \
  --verbose
```

## Project Structure

```
health-reporter/
├── cmd/
│   └── health-reporter/
│       └── main.go          # Entry point
├── pkg/
│   ├── config/
│   │   └── config.go        # Configuration management
│   ├── health/
│   │   └── health.go        # Core health reporting logic
│   ├── mimir/
│   │   └── mimir.go         # Mimir metrics client
│   └── webhook/
│       └── discord.go       # Discord webhook sender
├── go.mod                   # Go module definition
├── go.sum                   # Dependency checksums
├── config.yaml.example      # Example configuration
├── Dockerfile               # Container image
├── Helm/                    # Helm chart (Phase 1)
└── README.md                # This file
```

## Metrics Collected

### Nodes
- Total node count
- Ready nodes
- Not-ready nodes

### Pods
- Running pods
- Pending pods
- Failed pods
- Pod restarts (last 1 hour)

### Resources
- CPU usage percentage
- Memory usage percentage
- Available memory (GB)
- Disk usage (planned for Phase 2)

### Health Thresholds

| Metric | Threshold | Level |
|--------|-----------|-------|
| CPU Usage | > 90% | Critical |
| CPU Usage | > 80% | Degraded |
| Memory Usage | > 90% | Critical |
| Memory Usage | > 80% | Degraded |
| Pod Restarts (1h) | > 5 | Degraded |
| Pending Pods | > 2 | Degraded |
| Failed Pods | > 0 | Critical |
| Not Ready Nodes | > 0 | Critical |

## Discord Report Format

Reports are sent as embeds with the following information:
- **Status**: Healthy ✅, Degraded ⚠️, or Critical 🚨
- **Summary**: Brief overview of cluster state
- **Metrics**: Nodes, pods, CPU, memory usage
- **Concerns**: Specific issues identified
- **Recommendations**: Suggested actions

## Kubernetes Deployment

### Deploy with Helm (Phase 1 complete)

```bash
helm install health-reporter ./helm/health-reporter \
  -n monitoring \
  --values helm/values.yaml
```

### Manual Deployment

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: health-reporter-config
  namespace: monitoring
data:
  config.yaml: |
    mimir:
      url: "http://mimir-query:9009"
    discord:
      webhook_url: "${DISCORD_WEBHOOK_URL}"
    health:
      cpu_threshold: 80
      memory_threshold: 85
      restart_limit: 5
---
apiVersion: batch/v1
kind: CronJob
metadata:
  name: health-reporter
  namespace: monitoring
spec:
  schedule: "0 * * * *"  # Hourly
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: health-reporter
          containers:
          - name: health-reporter
            image: health-reporter:latest
            args:
            - "--config"
            - "/etc/health-reporter/config.yaml"
            - "--once"
            env:
            - name: DISCORD_WEBHOOK_URL
              valueFrom:
                secretKeyRef:
                  name: health-reporter-secrets
                  key: discord-webhook
            volumeMounts:
            - name: config
              mountPath: /etc/health-reporter
          volumes:
          - name: config
            configMap:
              name: health-reporter-config
          restartPolicy: OnFailure
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: health-reporter
  namespace: monitoring
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: health-reporter
rules:
- apiGroups: [""]
  resources: ["nodes", "pods"]
  verbs: ["get", "list"]
- apiGroups: [""]
  resources: ["events"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: health-reporter
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: health-reporter
subjects:
- kind: ServiceAccount
  name: health-reporter
  namespace: monitoring
```

## Development

### Run Tests

```bash
go test ./...
```

### Run with Verbose Logging

```bash
./health-reporter --once --verbose
```

### Build Docker Image

```bash
docker build -t health-reporter:latest .
docker run --rm \
  -e DISCORD_WEBHOOK_URL="https://..." \
  health-reporter:latest \
  --mimir-url "http://host.docker.internal:9009" \
  --once
```

## Monitoring & Debugging

### Check Recent Reports

```bash
kubectl logs -n monitoring deployment/health-reporter-cron --tail=50
```

### Manual Report Generation

```bash
kubectl run -it --rm \
  --image=health-reporter:latest \
  debug \
  -n monitoring \
  -- --once --verbose
```

### Verify Mimir Connectivity

```bash
# From a pod in the cluster
curl -s "http://mimir-query:9009/api/prom/query?query=up" | jq .
```

## Troubleshooting

### Discord Webhook Not Sending

1. Verify webhook URL is correct:
   ```bash
   curl -X POST -H 'Content-Type: application/json' \
     -d '{"content":"test"}' \
     <WEBHOOK_URL>
   ```

2. Check logs for errors:
   ```bash
   ./health-reporter --once --verbose
   ```

3. Verify network connectivity to Discord:
   ```bash
   curl -I https://discord.com
   ```

### Mimir Query Failing

1. Verify Mimir is accessible:
   ```bash
   curl -s "http://mimir-query:9009/api/prom/query?query=up"
   ```

2. Check Mimir API is responding:
   ```bash
   curl http://mimir-query:9009/-/ready
   ```

3. Verify metrics exist:
   ```bash
   # From a pod in cluster
   curl "http://mimir-query:9009/api/prom/label/__name__/values"
   ```

### High Memory or CPU Usage

This is expected during metric collection. If it persists:

1. Reduce query range
2. Use sampling for large clusters
3. Optimize PromQL queries

## Next Steps

- **Phase 2**: Add smoke test suite
- **Phase 3**: Implement Slack integration
- **Phase 4**: Integrate LLM for intelligent analysis
- **Phase 5**: Add trend analysis and historical tracking

## License

MIT

## Support

For issues or feature requests, open an issue on GitHub.

---

## Timeline

- **Phase 1** (Current): Core metrics + Discord → 1-2 weeks
- **Phase 2**: Smoke tests → 1 week
- **Phase 3**: Multi-channel → 3-5 days
- **Phase 4**: LLM integration → 2-3 weeks
- **Phase 5**: Trend analysis → 1 week
