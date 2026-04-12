# Health Reporter Development Memory

## Current Session Progress (2026-04-12)

### Model Migration ✅ COMPLETE
- **Previous State**: Config was set to phi3.5:3.8b, but model was NEVER INSTALLED
  - Available models in Ollama were only: qwen3.5:latest, llama3.2:latest
  - Health-reporter was actually using llama3.2 (fallback or Ollama auto-used available model)
  - This explains the slow 4+ minute per phase times with llama3.2:1b model
- **Issue**: llama3.2 (3.2B params) was too slow, timing out Phase 2
- **Solution**: Switched to Mistral 7B (7B params, much faster and better quality)
- **Status**: Mistral now active - confirmed in logs at 22:27:49
- **Evidence**: `LLM analysis enabled: mistral:7b` in pod logs
- **Impact**: Expected to reduce analysis time from 8+ minutes to 3-4 minutes

### Node Metrics Fix ✅ DEPLOYED
- **Commit 1**: da864eb - Fixed instance label in node_exporter queries (pkg/mimir/mimir.go:210-223)
- **Commit 2**: 505c178 - Fixed headless service query PromQL (pkg/mimir/mimir.go:388)
- **Build**: Completed successfully in 1m34s (Run #24318910506)
- **Deployment**: Pod restarted, new build active (pod: health-reporter-7b7b9cbf45-986xj)
- **Status**: ✅ Node metrics now showing correct values (CPU, Memory, Available)
  - Example: Node internal: CPU=15.0%, Mem=19.0%, MemAvail=102.0GB
  - No more 0.0 values in Discord reports
- **Verification**: Debug output confirms fixes are working

## Known Issues to Address

### 1. Node Metrics Showing 0.0 (HIGH PRIORITY) 
- **ROOT CAUSE FOUND**: queryNodeDetailsMetrics() uses wrong label for node_exporter metrics
- **File**: `pkg/mimir/mimir.go` lines 210-223
- **Problem**: Queries use `node="%s"` label, but node_exporter metrics use `instance` label
  - Line 210: `node_cpu_seconds_total{node="%s",...}` ❌ should be `instance="%s"`
  - Line 215: `node_memory_MemTotal_bytes{node="%s"}` ❌ should be `instance="%s"`
  - Line 220: `node_memory_MemAvailable_bytes{node="%s"}` ❌ should be `instance="%s"`
- **Solution**: Join node_exporter metrics (instance label) with kube_node_info (node label)
- **Fix Approach**:
  - Query node names from kube_node_info to get node names
  - Get instance labels from node_exporter via kube-state-metrics node_info
  - Use instance label to query node_exporter metrics (or use relabeling in scrape config)

### 2. Mimir Query Error ✅ FIXED
- **Error Message**: `query returned 400: parse error: binary expression must contain only scalar and instant vector types`
- **Root Cause**: Line 388 in pkg/mimir/mimir.go used invalid PromQL `and` operator inside count aggregation
- **Old Query**: `count(kube_service_spec_type{type="ClusterIP"} and kube_service_spec_cluster_ip == "")`
- **Fixed Query**: `count(kube_service_spec_type{type="ClusterIP"} unless kube_service_spec_cluster_ip)`
- **Commit**: 505c178 - Use proper `unless` set operator for headless service matching

### 3. Pod Metrics Bug (MEDIUM PRIORITY)
- **Issue**: Running/Pending/Failed pod counts all showing same value (164)
- **Expected**: Only running should be high, pending/failed should be low
- **Status**: Listed in JOURNAL.md but not yet investigated
- **Next Steps**: Check pod query logic

## Ollama Models Installed
- **mistral:7b** - 4-5GB, ~1.5-2 min per request (NEW, ACTIVE)
- qwen3.5:latest - 6.6GB, medium speed
- llama3.2:latest - 2.0GB, faster but lower quality

**PVC Storage**: 7.4GB used of 20GB available in ollama-models PVC

## Helm Configuration
- **Chart**: `./helm/health-reporter`
- **Namespace**: monitoring
- **Deployment**: health-reporter (running)
- **Config Source**: ConfigMap (health-reporter-config)
- **Last Updated**: 2026-04-12 18:27:12 (Revision 52)

## Next Steps

1. **Monitor GitHub Build**: Check Actions tab for build completion (should be 5-10 min)
   - Building: da864eb (node metrics label fix) + 505c178 (headless query fix)
2. **Restart Pod**: Once image is pushed (pullPolicy: Always will auto-pull)
   ```bash
   kubectl rollout restart deployment/health-reporter -n monitoring
   ```
3. **Verify Fixes**: Check logs for:
   - Node metrics showing non-zero values (should appear in next hourly run)
   - No more `[WARN] query failed for headless` error
   - Next hourly report should have correct node metrics

## Deployment Commands
```bash
# Monitor pod logs in real-time
kubectl logs -n monitoring deployment/health-reporter -f --tail=50

# Check pod status
kubectl get all -n monitoring | grep health

# Restart pod (forces pull of new image)
kubectl rollout restart deployment/health-reporter -n monitoring

# Check image SHA
kubectl describe pod -n monitoring -l app=health-reporter | grep Image:
```

## Files to Check
- `pkg/mimir/mimir.go` - Node and pod metric queries
- `config/samples/extended_tests.yaml` - Smoke test definitions
- `helm/health-reporter/templates/configmap.yaml` - Config template

