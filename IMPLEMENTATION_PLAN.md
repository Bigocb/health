# Health-Reporter Optimization: Cache-First Architecture

**Date:** 2026-04-14  
**Status:** Ready for Implementation  
**Impact:** 50% reduction in Mimir query load, 100x faster report generation

---

## Executive Summary

The health-reporter is currently making fresh Mimir queries every time a report is generated. This causes:
- Spiky load on Mimir (33+ queries per report cycle)
- Report generation takes 90-120 seconds
- Overlapping queries when collection interval is too short

**Solution:** Decouple metric collection from report generation using the existing enriched cache infrastructure:
- **Collection:** CacheCollector runs every 5 minutes continuously (steady, predictable)
- **Reporting:** Report generation reads from cache (instant, no queries)
- **Pinning:** Run on internal node only (131GB available vs vps01's 32GB)

---

## Architecture Changes

### Before (Current)
```
Report Generation (every 1h or 2h)
  ├─ Call GetMetrics() → 33+ queries to Mimir
  ├─ Wait 90-120 seconds
  ├─ Generate report
  └─ Send Discord
  
CacheCollector (background, every 60s)
  ├─ Also calls GetMetrics() → DUPLICATE queries
  └─ Stores in enriched cache (but only as fallback)
```

**Problem:** Two sources calling GetMetrics() independently = redundant queries + spiky load

### After (Proposed)
```
CacheCollector (background, every 5 minutes)
  ├─ Call GetMetrics() → 33+ queries to Mimir
  ├─ Store in EnrichedCache
  └─ Keep 2-hour rolling window (24 data points)

Report Generation (every 1h or 2h) 
  ├─ Read cache.GetLatestMetrics() → NO queries
  ├─ Convert cache format to report format
  ├─ Generate report (instant)
  └─ Send Discord
```

**Benefit:** Single source of truth, steady load, instant reports

---

## Implementation Details

### 1. Cache-to-Metrics Conversion Function

**File:** `pkg/health/health.go`

Add new helper function to convert `cache.EnrichedMetrics` back to `mimir.Metrics` format:

```go
// convertCacheToMetrics converts enriched cache format back to mimir.Metrics
func convertCacheToMetrics(cached *cache.EnrichedMetrics) *mimir.Metrics {
    metrics := &mimir.Metrics{
        Nodes: mimir.NodeMetrics{},
        Pods: mimir.PodMetrics{},
        Resources: mimir.ResourceMetrics{},
        Deployments: mimir.DeploymentMetrics{},
        Jobs: mimir.JobMetrics{},
        Services: mimir.ServiceMetrics{},
        Storage: mimir.StorageMetrics{},
        NodeDetails: make([]mimir.NodeDetail, 0),
    }
    
    // Extract cluster-level metrics from cache ClusterMetrics map
    if clusterMetrics, ok := cached.ClusterMetrics["nodes"].(map[string]interface{}); ok {
        metrics.Nodes.Total = intOrZero(clusterMetrics["total"])
        metrics.Nodes.Ready = intOrZero(clusterMetrics["ready"])
        metrics.Nodes.NotReady = intOrZero(clusterMetrics["not_ready"])
        metrics.Nodes.Unschedulable = intOrZero(clusterMetrics["unschedulable"])
        metrics.Nodes.CPUCores = intOrZero(clusterMetrics["cpu_cores"])
        metrics.Nodes.MemoryGB = floatOrZero(clusterMetrics["memory_gb"])
    }
    
    if clusterMetrics, ok := cached.ClusterMetrics["pods"].(map[string]interface{}); ok {
        metrics.Pods.Total = intOrZero(clusterMetrics["total"])
        metrics.Pods.Running = intOrZero(clusterMetrics["running"])
        metrics.Pods.Pending = intOrZero(clusterMetrics["pending"])
        metrics.Pods.Failed = intOrZero(clusterMetrics["failed"])
        metrics.Pods.Succeeded = intOrZero(clusterMetrics["succeeded"])
        metrics.Pods.Restarts = intOrZero(clusterMetrics["restarts"])
    }
    
    if resourceMetrics, ok := cached.ClusterMetrics["resources"].(map[string]interface{}); ok {
        metrics.Resources.CPUUsagePercent = floatOrZero(resourceMetrics["cpu_usage_percent"])
        metrics.Resources.MemoryUsagePercent = floatOrZero(resourceMetrics["memory_usage_percent"])
        metrics.Resources.DiskUsagePercent = floatOrZero(resourceMetrics["disk_usage_percent"])
        metrics.Resources.AvailableMemoryGB = floatOrZero(resourceMetrics["available_memory_gb"])
        metrics.Resources.AvailableStorageGB = floatOrZero(resourceMetrics["available_storage_gb"])
    }
    
    if deployMetrics, ok := cached.ClusterMetrics["deployments"].(map[string]interface{}); ok {
        metrics.Deployments.Total = intOrZero(deployMetrics["total"])
        metrics.Deployments.Ready = intOrZero(deployMetrics["ready"])
        metrics.Deployments.Unready = intOrZero(deployMetrics["unready"])
    }
    
    if jobMetrics, ok := cached.ClusterMetrics["jobs"].(map[string]interface{}); ok {
        metrics.Jobs.Total = intOrZero(jobMetrics["total"])
        metrics.Jobs.Active = intOrZero(jobMetrics["active"])
        metrics.Jobs.Failed = intOrZero(jobMetrics["failed"])
        metrics.Jobs.Succeeded = intOrZero(jobMetrics["succeeded"])
    }
    
    // Convert per-node metrics from cache snapshots
    for _, nodeSnapshot := range cached.NodeMetrics {
        metrics.NodeDetails = append(metrics.NodeDetails, mimir.NodeDetail{
            Name:                nodeSnapshot.NodeName,
            Ready:               nodeSnapshot.Ready,
            Unschedulable:       nodeSnapshot.Unschedulable,
            CPUUsagePercent:     nodeSnapshot.CPUUsagePercent,
            MemoryUsagePercent:  nodeSnapshot.MemoryUsagePercent,
            AvailableMemoryGB:   nodeSnapshot.AvailableMemoryGB,
            PodCount:            nodeSnapshot.PodCount,
        })
    }
    
    return metrics
}
```

---

### 2. Modify Report Generation to Use Cache

**File:** `pkg/health/health.go` - Function `Generate()` starting at line 96

**REPLACE lines 96-100:**
```go
func (r *Reporter) Generate(ctx context.Context) (*types.Report, error) {
	metrics, err := r.mimirClient.GetMetrics(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get metrics: %w", err)
	}
```

**WITH:**
```go
func (r *Reporter) Generate(ctx context.Context) (*types.Report, error) {
	// Read from enriched cache instead of making fresh Mimir queries
	// Cache is populated by CacheCollector running every 5 minutes
	if r.cache == nil {
		return nil, fmt.Errorf("cache not initialized; CacheCollector must be running")
	}
	
	cachedEnriched := r.cache.GetLatestMetrics()
	if cachedEnriched == nil {
		return nil, fmt.Errorf("no cached metrics available (CacheCollector may not have completed first cycle)")
	}
	
	// Convert cache format back to mimir.Metrics format
	metrics := convertCacheToMetrics(cachedEnriched)
	log.Printf("[Report] Generating report from cached metrics (timestamp: %v, cache age: %v)", 
		cachedEnriched.Timestamp, time.Since(cachedEnriched.Timestamp))
```

**Key Change:** Remove the `err` handling for GetMetrics() since we're no longer calling it.

---

### 3. Initialize Cache and Collector in Main

**File:** `cmd/health-reporter/main.go`

Add initialization in the main function before starting daemon mode (around line 100-150):

```go
// Create enriched cache with 2-hour retention and 300MB limit
enrichedCache := cache.NewEnrichedCache(
    10000,                    // max log entries across all pods
    2*time.Hour,              // evict data older than 2 hours
    300*1024*1024,            // 300MB hard limit
)
log.Printf("[Init] Created enriched cache (2h retention, 300MB limit)")

// Create cache collector with 5-minute collection interval
cacheCollector := cache.NewCacheCollector(
    enrichedCache,
    mimirClient,
    lokiClient,
    300,  // interval in seconds (5 minutes)
)
log.Printf("[Init] Created cache collector (5-minute intervals)")

// Start background collection BEFORE daemon loop
cacheCollector.Start(ctx)
log.Printf("[Init] Cache collector started - will collect metrics every 5 minutes")

// Pass cache and collector to reporter
reporter.SetCache(enrichedCache)
reporter.SetCacheCollector(cacheCollector)
log.Printf("[Init] Reporter configured to use enriched cache")
```

**Location:** Add this code right after `reporter` is created and configured, before the `runDaemon()` call.

---

### 4. Configure Collection Interval

**File:** `helm/health-reporter/values.yaml`

Add/update cache collector section (around line 50-60):

```yaml
# Cache collector configuration (runs independently from reporting)
cacheCollector:
  enabled: true
  intervalSeconds: 300        # 5 minutes = 300 seconds
  
# Cache retention
cache:
  maxCacheAge: 2h             # Keep 2 hours of historical data
  maxMemoryBytes: 314572800   # 300MB limit
  maxLogEntries: 10000        # Max error entries across all pods
```

---

### 5. Pin to Internal Node

**File:** `helm/health-reporter/values.yaml` - Line 101 (nodeSelector)

**CHANGE FROM:**
```yaml
nodeSelector: {}
```

**CHANGE TO:**
```yaml
nodeSelector:
  node-type: internal
```

**Verification command:**
```bash
kubectl get nodes --show-labels | grep internal
```

If the label doesn't exist, use hostname instead:
```yaml
nodeSelector:
  kubernetes.io/hostname: internal
```

---

### 6. Verify Resource Limits (No Changes)

**File:** `helm/health-reporter/values.yaml` - Lines 82-88

Current configuration is appropriate for the new architecture:

```yaml
resources:
  requests:
    memory: "256Mi"    # Guaranteed allocation
    cpu: "100m"
  limits:
    memory: "512Mi"    # Hard cap (metrics collection ~150MB peak)
    cpu: "500m"        # Hard cap (queries use ~200-300mCPU, idle <50mCPU)
```

With 5-minute collection intervals:
- Memory stays ~150MB (well under 512Mi limit)
- CPU spikes to ~200-300mCPU every 5 minutes for ~30-60 seconds, then idle
- No continuous load like before

---

## Files Modified Summary

| File | Change Type | Scope | Lines |
|------|-------------|-------|-------|
| `pkg/health/health.go` | Add function | `convertCacheToMetrics()` | ~80 new |
| `pkg/health/health.go` | Modify function | `Generate()` at line 96 | ~20 changed |
| `cmd/health-reporter/main.go` | Add initialization | Cache + collector setup | ~15 new |
| `helm/health-reporter/values.yaml` | Update config | Cache config + nodeSelector | 4 changed |

**Total:** 4 files, ~119 lines of changes

---

## Data Flow Diagram

```
┌────────────────────────────────────────────────────────────────┐
│                    CacheCollector                              │
│              (Background, every 5 minutes)                      │
│                                                                │
│  1. Timer triggers → collectOnce()                            │
│  2. Call mimirClient.GetMetrics()                             │
│     └─ Makes 33+ queries to Mimir                             │
│  3. Enrich metrics with trends                                │
│  4. Call cache.UpdateMetrics(enriched)                        │
│  5. Collect enriched failed pods from Loki                    │
│  6. Complete, wait 5 minutes                                  │
└────────────────────────────────────────────────────────────────┘
                           ↓
                    (every 5 min)
                           ↓
┌────────────────────────────────────────────────────────────────┐
│                   EnrichedCache                                │
│              (Memory-backed time-series store)                 │
│                                                                │
│  metrics: []EnrichedMetrics                                   │
│    └─ Length: ~24 (one per 5 minutes over 2 hours)           │
│  nodeMetrics: map[string][]NodeMetricsSnapshot               │
│    └─ Per-node time-series with 24 snapshots each            │
│  failedPods: map[string]*EnrichedFailedPod                   │
│  maxCacheAge: 2 hours (auto-evict older)                     │
│  maxMemoryBytes: 300MB (soft limit)                          │
└────────────────────────────────────────────────────────────────┘
                           ↓
              (read when report needed)
                           ↓
┌────────────────────────────────────────────────────────────────┐
│               Report Generation                                │
│          (Triggered every 1h or 2h, instant)                  │
│                                                                │
│  1. Reporting interval ticker fires                           │
│  2. Call cache.GetLatestMetrics()                             │
│     └─ NO Mimir queries, just memory read                     │
│  3. Convert cache format → mimir.Metrics format               │
│  4. Build report structure                                    │
│  5. Classify metrics deterministically                        │
│  6. Send Discord webhook                                      │
│  7. Save to history storage                                   │
│                                                                │
│  Total time: <1 second (vs 90-120s before)                   │
└────────────────────────────────────────────────────────────────┘
                           ↓
                  Discord Webhook
                   (Report sent)
```

---

## Performance Comparison

### Before (Report-Triggered Queries)
```
Report every 1h:
  ├─ 60 minutes waiting
  ├─ Report fires → GetMetrics() → 33 queries
  ├─ Mimir under spiky load (30-120s per query)
  ├─ Report takes 90-120 seconds to generate
  ├─ CacheCollector ALSO running every 60s (DUPLICATE queries)
  └─ Total daily load: ~1440 queries + report spikes
```

### After (Cache-Backed Reports)
```
Continuous collection:
  ├─ Every 5 minutes: CacheCollector → GetMetrics() → 33 queries
  ├─ Mimir gets predictable load every 5 minutes
  ├─ Cache stores data
  
Report every 1h (or any interval):
  ├─ Fire → Read cache (instant)
  ├─ Convert format (instant)
  ├─ Send report (<1 second total)
  └─ Total daily load: ~288 queries (same collection, no report spike)
```

### Metrics

| Metric | Before | After | Improvement |
|--------|--------|-------|-------------|
| **Queries per day** | ~1440 (60s intervals) | ~288 (300s intervals) | 80% ↓ |
| **Query spikes** | Per report (~120s wait) | Steady (every 5min) | Predictable |
| **Report generation time** | 90-120 seconds | <1 second | 100x faster |
| **Mimir API load** | Spiky | Flat | Better stability |
| **Memory usage** | 100-150MB | 100-150MB | Same |
| **CPU during collection** | 200-300mCPU (60-90s) | 200-300mCPU (30-60s) | More efficient |

---

## Deployment Steps

### Step 1: Code Changes
1. Add `convertCacheToMetrics()` function to `pkg/health/health.go`
2. Modify `Generate()` function to read from cache
3. Add cache/collector initialization to `cmd/health-reporter/main.go`

### Step 2: Config Changes
1. Update `helm/health-reporter/values.yaml` with cache collector config
2. Add nodeSelector to pin to internal node

### Step 3: Deploy
```bash
# Commit changes
git add .
git commit -m "refactor: decouple metric collection from reporting via enriched cache

- CacheCollector now runs every 5 minutes independently
- Report generation reads from cache (no fresh queries)
- Reduces Mimir load by 80%
- Makes report generation instant (<1s)
- Pins to internal node only
- Improves cluster stability and predictability"

# Deploy
helm upgrade health-reporter ./helm/health-reporter -n monitoring

# Watch rollout
kubectl rollout status deployment/health-reporter -n monitoring
```

### Step 4: Restart Pod
```bash
kubectl rollout restart deployment/health-reporter -n monitoring
```

---

## Verification Checklist

- [ ] Pod scheduled on internal node: `kubectl get pod -n monitoring -l app=health-reporter -o wide`
- [ ] Logs show cache collector started: `kubectl logs -n monitoring deployment/health-reporter | grep "Cache collector started"`
- [ ] Reports generated from cache: `kubectl logs -n monitoring deployment/health-reporter | grep "Generating report from cached"`
- [ ] No GetMetrics errors in main report path: `kubectl logs -n monitoring deployment/health-reporter | grep "failed to get metrics" -c` should be 0
- [ ] Memory stays stable: `kubectl top pod -n monitoring -l app=health-reporter` (expect <200MB)
- [ ] Reports still accurate (3 cycles): Check Discord for correct node metrics
- [ ] No pod restarts: `kubectl describe pod -n monitoring -l app=health-reporter | grep Restarts`
- [ ] Collection cycle logs show: `[Collector] Starting collection cycle...` every 5 minutes
- [ ] Reports arrive on schedule: Check Discord timestamps (1h or 2h apart)

---

## Rollback Plan

If issues arise, rollback is simple:

```bash
# Revert deployment
helm rollout undo health-reporter -n monitoring

# Or revert code commits
git revert <commit-hash>
```

The change is minimal and low-risk:
- No changes to core metrics collection logic
- Just reroutes where metrics are read from
- Cache is already in codebase (just unused for reports)
- Easy to revert if needed

---

## Success Criteria

✅ Health-reporter pod runs on internal node  
✅ Metrics collected every 5 minutes (not 60 seconds)  
✅ Report generation takes <1 second (reads cache only)  
✅ Mimir query volume reduced to ~288/day (was 1440+)  
✅ Memory usage stays <200MB (under 512Mi limit)  
✅ CPU usage is predictable (200-300mCPU spikes, then idle)  
✅ Discord reports accurate with per-node metrics  
✅ Zero pod restarts or OOMKill events  
✅ Logs show "Generating report from cached metrics"  
✅ Cache hits > 95% (data fresh every 5 min, reports pull from it)

---

## Post-Implementation

After verification:
1. Monitor health-reporter for 24 hours
2. Check Mimir query metrics (should be ~80% lower)
3. Verify cluster stability (kubectl responsiveness)
4. Plan next phase: conditional smart path (LLM when degraded)
5. Consider future: external LLM API instead of ollama

---

## Questions / Notes

- **Interval tuning:** 5 minutes is baseline; can adjust to 10, 15, or 30 minutes if Mimir still under load
- **Cache size:** 300MB limit allows ~2 hours at current query size; monitor cache.GetStats() for usage
- **Failed pods:** Collector also enriches failed pods from Loki; these are included in cache for analysis
- **Trends:** EnrichedMetrics includes CPUTrend and MemoryTrend calculations (24h window helpful for alerting)
