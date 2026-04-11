# Health Reporter: Build, Helm Chart, and Deployment Guide

## Overview

This guide covers building the Docker image locally, creating the Helm chart, and deploying to Kubernetes.

**Current Status**:
- ✅ Docker image built: `health-reporter:v0.1.0` (13.4 MB)
- ✅ Helm chart created: `helm/health-reporter/`
- ⏳ Ready for deployment

---

## Part 1: Docker Image Build

### Build Locally

```bash
cd C:\Users\bigoc\dev\arch\health-reporter

# Build with tags
docker build -t health-reporter:v0.1.0 -t health-reporter:latest .

# Verify image
docker images health-reporter
```

### Output

```
REPOSITORY        TAG       IMAGE ID       CREATED         SIZE
health-reporter   latest    251ce2cb001c   7 seconds ago   13.4MB
health-reporter   v0.1.0    251ce2cb001c   7 seconds ago   13.4MB
```

### Multi-Stage Build Benefits

- **Builder stage**: Full Go compiler environment (~350MB)
- **Final stage**: Only runtime binary + Alpine (~13MB)
- **Size reduction**: ~96% smaller than builder image
- **Security**: Non-root user (UID 1000)

### Test Docker Image

```bash
# Run once with test parameters
docker run --rm \
  -e DISCORD_WEBHOOK_URL="https://discord.com/api/webhooks/..." \
  health-reporter:v0.1.0 \
  --mimir-url "http://host.docker.internal:9009" \
  --once \
  --verbose
```

---

## Part 2: Helm Chart Structure

### Directory Layout

```
helm/health-reporter/
├── Chart.yaml                  # Chart metadata
├── values.yaml                 # Default values
└── templates/
    ├── _helpers.tpl            # Template helpers
    ├── serviceaccount.yaml      # Service account
    ├── clusterrole.yaml         # RBAC ClusterRole
    ├── clusterrolebinding.yaml  # RBAC ClusterRoleBinding
    ├── configmap.yaml           # Configuration
    ├── secret.yaml              # Discord webhook secret
    ├── cronjob.yaml             # CronJob (hourly)
    └── deployment.yaml          # Deployment (test mode)
```

### Key Features

1. **Multiple Modes**: CronJob (production) or Deployment (testing)
2. **RBAC-Ready**: Includes ServiceAccount, ClusterRole, ClusterRoleBinding
3. **ConfigMap**: Application configuration (Mimir URL, thresholds)
4. **Secret**: Discord webhook URL (sensitive data)
5. **Flexible Configuration**: Override any value via Helm

---

## Part 3: Deployment

### Prerequisites

```bash
# Verify cluster access
kubectl cluster-info
kubectl get nodes

# Verify you can access Mimir
kubectl get svc -n monitoring mimir-query
```

### Step 1: Create Namespace (if not exists)

```bash
kubectl create namespace monitoring
```

### Step 2: Verify Docker Image is Accessible

For local deployment (Docker Desktop), the image should be loaded:

```bash
docker images health-reporter:v0.1.0
```

### Step 3: Deploy via Helm (CronJob Mode - Production)

```bash
# Install with Discord webhook URL
helm install health-reporter \
  ./helm/health-reporter \
  -n monitoring \
  --set discord.webhookUrl="https://discord.com/api/webhooks/YOUR_WEBHOOK_ID/YOUR_WEBHOOK_TOKEN"

# Verify installation
helm status health-reporter -n monitoring
helm get values health-reporter -n monitoring
```

### Step 4: Deploy via Helm (Deployment Mode - Testing)

```bash
# Install in deployment mode for continuous testing
helm install health-reporter \
  ./helm/health-reporter \
  -n monitoring \
  --set mode=deployment \
  --set discord.webhookUrl="https://discord.com/api/webhooks/YOUR_WEBHOOK_ID/YOUR_WEBHOOK_TOKEN" \
  --set logging.verbose=true

# Watch logs
kubectl logs -f -n monitoring deployment/health-reporter-health-reporter
```

### Step 5: Dry-Run (Test Without Deploying)

```bash
# See what would be deployed
helm install health-reporter \
  ./helm/health-reporter \
  -n monitoring \
  --set discord.webhookUrl="https://discord.com/api/webhooks/..." \
  --dry-run \
  --debug

# Generate manifests
helm template health-reporter \
  ./helm/health-reporter \
  -n monitoring \
  --set discord.webhookUrl="https://discord.com/api/webhooks/..." \
  > health-reporter-manifests.yaml

# Review manifests
cat health-reporter-manifests.yaml
```

---

## Part 4: Verify Deployment

### Check Resources Created

```bash
# List CronJobs
kubectl get cronjob -n monitoring
kubectl describe cronjob health-reporter-health-reporter -n monitoring

# List ConfigMaps
kubectl get configmap -n monitoring
kubectl get configmap health-reporter-health-reporter-config -n monitoring -o yaml

# List Secrets
kubectl get secret -n monitoring
kubectl get secret health-reporter-discord -n monitoring

# List ServiceAccount
kubectl get serviceaccount -n monitoring
kubectl describe serviceaccount health-reporter -n monitoring
```

### Manually Trigger CronJob (for Testing)

```bash
# Create job from CronJob spec
kubectl create job -n monitoring --from=cronjob/health-reporter-health-reporter health-reporter-manual-1

# Watch job
kubectl describe job -n monitoring health-reporter-manual-1
kubectl logs -n monitoring job/health-reporter-manual-1

# Check if report was sent to Discord
```

### View Recent Reports

```bash
# Get latest completed jobs
kubectl get jobs -n monitoring -o json | jq '.items[] | {name: .metadata.name, status: .status}'

# Get logs from specific job
kubectl logs -n monitoring -l job-name=health-reporter-manual-1

# Get logs from specific pod
kubectl get pods -n monitoring -l app=health-reporter
kubectl logs -n monitoring <pod-name>
```

---

## Part 5: Updating Configuration

### Update Mimir URL

```bash
helm upgrade health-reporter \
  ./helm/health-reporter \
  -n monitoring \
  --set mimir.url="http://mimir-query-new:9009"
```

### Update Discord Webhook

```bash
helm upgrade health-reporter \
  ./helm/health-reporter \
  -n monitoring \
  --set discord.webhookUrl="https://discord.com/api/webhooks/NEW_ID/NEW_TOKEN"
```

### Update Health Thresholds

```bash
helm upgrade health-reporter \
  ./helm/health-reporter \
  -n monitoring \
  --set health.cpuThreshold=75 \
  --set health.memoryThreshold=80
```

### Enable Verbose Logging

```bash
helm upgrade health-reporter \
  ./helm/health-reporter \
  -n monitoring \
  --set logging.verbose=true
```

---

## Part 6: Troubleshooting

### Pod Logs

```bash
# View CronJob execution
kubectl logs -n monitoring -l app=health-reporter --tail=50

# Follow logs in real time
kubectl logs -f -n monitoring -l app=health-reporter

# Get logs from specific date/time
kubectl logs -n monitoring --since=1h -l app=health-reporter
```

### ConfigMap Issues

```bash
# View ConfigMap content
kubectl get configmap -n monitoring health-reporter-health-reporter-config -o yaml

# Check if config is mounted
kubectl exec -n monitoring <pod-name> -- cat /etc/health-reporter/config.yaml
```

### Secret Issues

```bash
# Verify secret exists
kubectl get secret -n monitoring health-reporter-discord

# Decode secret (verify webhook URL is correct)
kubectl get secret -n monitoring health-reporter-discord -o jsonpath='{.data.webhook-url}' | base64 -d

# Verify environment variable is set
kubectl exec -n monitoring <pod-name> -- env | grep DISCORD
```

### Mimir Connectivity

```bash
# Test Mimir access from pod
kubectl exec -n monitoring <pod-name> -- curl -s http://mimir-query:9009/api/prom/query?query=up

# Port-forward for local testing
kubectl port-forward -n monitoring svc/mimir-query 9009:9009 &
curl http://localhost:9009/api/prom/query?query=up
```

### CronJob Not Running

```bash
# Check CronJob status
kubectl describe cronjob -n monitoring health-reporter-health-reporter

# Check last schedule time
kubectl get cronjob -n monitoring health-reporter-health-reporter -o jsonpath='{.status.lastScheduleTime}'

# Check events
kubectl get events -n monitoring --sort-by='.lastTimestamp' | tail -20
```

---

## Part 7: Uninstall

### Remove Helm Release

```bash
# Uninstall
helm uninstall health-reporter -n monitoring

# Verify removal
helm list -n monitoring
kubectl get cronjob -n monitoring
```

### Remove Namespace (if no other resources)

```bash
kubectl delete namespace monitoring
```

---

## Part 8: Advanced: Custom Values File

### Create `my-values.yaml`

```yaml
# my-values.yaml
image:
  tag: v0.1.0

mimir:
  url: "http://mimir-query:9009"

health:
  cpuThreshold: 75
  memoryThreshold: 80

logging:
  verbose: true

scheduling:
  schedule: "*/30 * * * *"  # Every 30 minutes instead of hourly

discord:
  webhookUrl: ""  # Set via CLI
```

### Deploy with Custom Values

```bash
helm install health-reporter \
  ./helm/health-reporter \
  -n monitoring \
  -f my-values.yaml \
  --set discord.webhookUrl="https://discord.com/api/webhooks/..."
```

---

## Part 9: Production Checklist

- [ ] Docker image built and tested locally
- [ ] Helm chart syntax validated (`helm template` no errors)
- [ ] Mimir endpoint verified (`kubectl port-forward` test)
- [ ] Discord webhook URL is valid (test with curl)
- [ ] ServiceAccount has correct RBAC permissions
- [ ] ConfigMap contains correct Mimir URL
- [ ] Secret contains correct Discord webhook
- [ ] CronJob schedule is correct
- [ ] First manual job runs successfully
- [ ] Discord report appears in channel
- [ ] Logs are clean (no errors)

---

## Part 10: Next Steps (Phase 2+)

### For Phase 2 (Smoke Tests)
- Add smoke test configuration to values.yaml
- Create additional template for smoke test config
- Update deployment to include smoke test runner

### For Phase 4 (LLM Integration)
- Add Ollama deployment Helm chart
- Add LLM configuration to health-reporter values
- Create LLM-specific templates

### For GitHub Integration
- Push health-reporter repo to GitHub
- Create GitHub Actions for image building
- Setup automated Helm chart releases

---

## Commands Quick Reference

```bash
# Build
docker build -t health-reporter:v0.1.0 .

# Test locally
docker run --rm health-reporter:v0.1.0 --help

# Deploy
helm install health-reporter ./helm/health-reporter -n monitoring \
  --set discord.webhookUrl="https://..."

# Verify
helm status health-reporter -n monitoring
kubectl get cronjob -n monitoring
kubectl logs -n monitoring -l app=health-reporter

# Update
helm upgrade health-reporter ./helm/health-reporter -n monitoring \
  --set logging.verbose=true

# Uninstall
helm uninstall health-reporter -n monitoring
```

---

*Last updated: 2026-04-11*
*Status: Deployment Ready ✅*
