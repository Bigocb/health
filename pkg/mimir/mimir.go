package mimir

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client wraps Mimir HTTP API
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// Metrics represents cluster metrics from Mimir
type Metrics struct {
	Timestamp    time.Time
	Nodes        NodeMetrics
	Pods         PodMetrics
	Resources    ResourceMetrics
	Deployments  DeploymentMetrics
	Jobs         JobMetrics
	Services     ServiceMetrics
	Storage      StorageMetrics
	NodeDetails  []NodeDetail // NEW: per-node breakdown
	Applications ApplicationMetrics
}

// NodeDetail represents per-node metrics
type NodeDetail struct {
	Name               string
	Ready              bool
	Unschedulable      bool
	CPUUsagePercent    float64
	MemoryUsagePercent float64
	DiskUsagePercent   float64
	AvailableMemoryGB  float64
	AvailableDiskGB    float64
	PodCount           int
}

type NodeMetrics struct {
	Total         int
	Ready         int
	NotReady      int
	CPUCores      int
	MemoryGB      float64
	Unschedulable int
}

type PodMetrics struct {
	Total         int
	Running       int
	Pending       int
	Failed        int
	Succeeded     int
	Restarts      int
	Unschedulable int
}

type ResourceMetrics struct {
	CPUUsagePercent     float64
	MemoryUsagePercent  float64
	DiskUsagePercent    float64
	AvailableMemoryGB   float64
	AvailableStorageGB  float64
	CPUCoresAllocatable float64
	MemoryGBAllocatable float64
}

type DeploymentMetrics struct {
	Total       int
	Ready       int
	Unready     int
	Unavailable int
}

type JobMetrics struct {
	Total     int
	Active    int
	Failed    int
	Succeeded int
}

type ServiceMetrics struct {
	Total            int
	ClusterIP        int
	Headless         int
	TypeLoadBalancer int
}

type StorageMetrics struct {
	TotalPVCs         int
	BoundPVCs         int
	PendingPVCs       int
	LostPVCs          int
	StorageUsedGB     float64
	StorageCapacityGB float64
}

type ApplicationMetrics struct {
	Applications map[string]AppDetail // app name -> metrics
}

type AppDetail struct {
	Name              string
	RequestRate       float64 // requests per second
	ErrorRate         float64 // errors per second
	ErrorPercent      float64 // percentage of requests that are errors (4xx/5xx)
	P50LatencyMs      float64 // median latency in ms
	P95LatencyMs      float64 // 95th percentile latency
	P99LatencyMs      float64 // 99th percentile latency
	AvailableReplicas int     // from deployment
	DesiredReplicas   int     // from deployment
}

// NewClient creates a new Mimir client
func NewClient(baseURL string) (*Client, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("mimir base URL cannot be empty")
	}

	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

// Close closes the client
func (c *Client) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}

// GetMetrics retrieves cluster metrics from Mimir
func (c *Client) GetMetrics(ctx context.Context) (*Metrics, error) {
	metrics := &Metrics{
		Timestamp: time.Now(),
	}

	// Query node metrics
	if err := c.queryNodeMetrics(ctx, metrics); err != nil {
		return nil, fmt.Errorf("failed to query node metrics: %w", err)
	}

	// Query per-node detailed metrics
	if err := c.queryNodeDetailsMetrics(ctx, metrics); err != nil {
		fmt.Printf("[WARN] failed to query node details: %v\n", err)
	}

	// Query pod metrics
	if err := c.queryPodMetrics(ctx, metrics); err != nil {
		return nil, fmt.Errorf("failed to query pod metrics: %w", err)
	}

	// Query resource metrics
	if err := c.queryResourceMetrics(ctx, metrics); err != nil {
		return nil, fmt.Errorf("failed to query resource metrics: %w", err)
	}

	// Query deployment metrics
	if err := c.queryDeploymentMetrics(ctx, metrics); err != nil {
		fmt.Printf("[WARN] failed to query deployment metrics: %v\n", err)
	}

	// Query job metrics
	if err := c.queryJobMetrics(ctx, metrics); err != nil {
		fmt.Printf("[WARN] failed to query job metrics: %v\n", err)
	}

	// Query service metrics
	if err := c.queryServiceMetrics(ctx, metrics); err != nil {
		fmt.Printf("[WARN] failed to query service metrics: %v\n", err)
	}

	// Query storage metrics
	if err := c.queryStorageMetrics(ctx, metrics); err != nil {
		fmt.Printf("[WARN] failed to query storage metrics: %v\n", err)
	}

	// Query application metrics (disabled - metrics not found in current setup)
	// if err := c.queryApplicationMetrics(ctx, metrics); err != nil {
	// 	fmt.Printf("[WARN] failed to query application metrics: %v\n", err)
	// }

	return metrics, nil
}

// queryNodeMetrics queries node status from Mimir
func (c *Client) queryNodeMetrics(ctx context.Context, m *Metrics) error {
	queries := map[string]string{
		"ready_nodes":   `sum(kube_node_status_condition{condition="Ready",status="true"} unless on(node) kube_node_spec_unschedulable == 1)`,
		"total_nodes":   `count(kube_node_info unless on(node) kube_node_spec_unschedulable == 1)`,
		"unschedulable": `count(kube_node_spec_unschedulable == 1)`,
		"cpu_cores":     `sum(kube_node_status_allocatable{resource="cpu"})`,
		"memory_bytes":  `sum(kube_node_status_allocatable{resource="memory"}) / 1024 / 1024 / 1024`,
	}

	results, err := c.queryRange(ctx, queries)
	if err != nil {
		return err
	}

	m.Nodes.Ready = int(floatValue(results["ready_nodes"]))
	m.Nodes.Total = int(floatValue(results["total_nodes"]))
	m.Nodes.NotReady = m.Nodes.Total - m.Nodes.Ready
	m.Nodes.Unschedulable = int(floatValue(results["unschedulable"]))
	m.Nodes.CPUCores = int(floatValue(results["cpu_cores"]))
	m.Nodes.MemoryGB = floatValue(results["memory_bytes"])

	return nil
}

// queryNodeDetailsMetrics queries per-node metrics from Mimir
func (c *Client) queryNodeDetailsMetrics(ctx context.Context, m *Metrics) error {
	// Query node names and basic info
	nodes, err := c.queryWithLabels(ctx, `kube_node_info`)
	if err != nil {
		return fmt.Errorf("failed to get node list: %w", err)
	}

	for _, node := range nodes {
		detail := NodeDetail{
			Name: node,
		}

		// Query per-node metrics
		// Note: node_exporter metrics use 'instance' label (hostname:port), not 'node' label
		// We query by instance and match against node name (instance label may be hostname or hostname:port)
		cpuQuery := fmt.Sprintf(`round(100*(1-avg(rate(node_cpu_seconds_total{instance=~"%s.*",mode="idle"}[5m]))),1)`, node)
		if cpu, err := c.query(ctx, cpuQuery); err == nil && cpu >= 0 {
			detail.CPUUsagePercent = cpu
		}

		memQuery := fmt.Sprintf(`round(100*(1-node_memory_MemAvailable_bytes{instance=~"%s.*"}/node_memory_MemTotal_bytes{instance=~"%s.*"}),1)`, node, node)
		if mem, err := c.query(ctx, memQuery); err == nil && mem >= 0 {
			detail.MemoryUsagePercent = mem
		}

		memAvailQuery := fmt.Sprintf(`round(node_memory_MemAvailable_bytes{instance=~"%s.*"}/1024/1024/1024,1)`, node)
		if memAvail, err := c.query(ctx, memAvailQuery); err == nil && memAvail >= 0 {
			detail.AvailableMemoryGB = memAvail
		}

		// Query per-node disk metrics (root filesystem)
		diskQuery := fmt.Sprintf(`round(100*(1-node_filesystem_avail_bytes{instance=~"%s.*",fstype!~"tmpfs|fuse.lxcfs|squashfs",mountpoint="/"}/node_filesystem_size_bytes{instance=~"%s.*",fstype!~"tmpfs|fuse.lxcfs|squashfs",mountpoint="/"}),1)`, node, node)
		if disk, err := c.query(ctx, diskQuery); err == nil && disk >= 0 {
			detail.DiskUsagePercent = disk
		}

		diskAvailQuery := fmt.Sprintf(`round(node_filesystem_avail_bytes{instance=~"%s.*",fstype!~"tmpfs|fuse.lxcfs|squashfs",mountpoint="/"}/1024/1024/1024,1)`, node)
		if diskAvail, err := c.query(ctx, diskAvailQuery); err == nil && diskAvail >= 0 {
			detail.AvailableDiskGB = diskAvail
		}

		// Check if node is ready and schedulable
		readyQuery := fmt.Sprintf(`kube_node_status_condition{node="%s",condition="Ready",status="true"}`, node)
		if ready, err := c.query(ctx, readyQuery); err == nil && ready > 0 {
			detail.Ready = true
		}

		unschQuery := fmt.Sprintf(`kube_node_spec_unschedulable{node="%s"}`, node)
		if unsch, err := c.query(ctx, unschQuery); err == nil && unsch > 0 {
			detail.Unschedulable = true
		}

		// Query pod count on this node
		podQuery := fmt.Sprintf(`count(kube_pod_info{node="%s"})`, node)
		if podCount, err := c.query(ctx, podQuery); err == nil && podCount >= 0 {
			detail.PodCount = int(podCount)
		}

		fmt.Printf("[DEBUG] Node %s: CPU=%.1f%%, Mem=%.1f%%, Disk=%.1f%%, MemAvail=%.1fGB, DiskAvail=%.1fGB, Ready=%v, Unschedulable=%v, Pods=%d\n",
			detail.Name, detail.CPUUsagePercent, detail.MemoryUsagePercent, detail.DiskUsagePercent, detail.AvailableMemoryGB, detail.AvailableDiskGB,
			detail.Ready, detail.Unschedulable, detail.PodCount)

		m.NodeDetails = append(m.NodeDetails, detail)
	}

	return nil
}

// queryWithLabels queries a metric and returns all unique values of the 'node' label
func (c *Client) queryWithLabels(ctx context.Context, promql string) ([]string, error) {
	u, err := url.Parse(c.baseURL + "/api/v1/query")
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("query", promql)
	q.Set("time", fmt.Sprintf("%d", time.Now().Unix()-300))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-Scope-OrgID", "admin")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("query returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
			} `json:"result"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	nodeSet := make(map[string]bool)
	for _, item := range result.Data.Result {
		if node, ok := item.Metric["node"]; ok {
			nodeSet[node] = true
		}
	}

	nodes := make([]string, 0, len(nodeSet))
	for node := range nodeSet {
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// queryPodMetrics queries pod status from Mimir
// NOTE: Excludes ollama namespace for cleaner health analysis
func (c *Client) queryPodMetrics(ctx context.Context, m *Metrics) error {
	queries := map[string]string{
		"running_pods":   `count(kube_pod_status_phase{namespace!="ollama",phase="Running"})`,
		"total_pods":     `count(kube_pod_info{namespace!="ollama"})`,
		"pending_pods":   `count(kube_pod_status_phase{namespace!="ollama",phase="Pending"} == 1)`,
		"failed_pods":    `count(kube_pod_status_phase{namespace!="ollama",phase="Failed"} == 1)`,
		"succeeded_pods": `count(kube_pod_status_phase{namespace!="ollama",phase="Succeeded"})`,
		"restarts_1h":    `sum(increase(kube_pod_container_status_restarts_total{namespace!="ollama"}[1h]))`,
		"unschedulable":  `count(kube_pod_status_phase{namespace!="ollama",phase="Pending"} and kube_pod_condition{condition="PodScheduled",status="false"})`,
	}

	results, err := c.queryRange(ctx, queries)
	if err != nil {
		return err
	}

	m.Pods.Running = int(floatValue(results["running_pods"]))
	m.Pods.Total = int(floatValue(results["total_pods"]))
	m.Pods.Pending = int(floatValue(results["pending_pods"]))
	m.Pods.Failed = int(floatValue(results["failed_pods"]))
	m.Pods.Succeeded = int(floatValue(results["succeeded_pods"]))
	m.Pods.Restarts = int(floatValue(results["restarts_1h"]))
	m.Pods.Unschedulable = int(floatValue(results["unschedulable"]))

	return nil
}

// queryDeploymentMetrics queries deployment status from Mimir
// NOTE: Excludes ollama namespace
func (c *Client) queryDeploymentMetrics(ctx context.Context, m *Metrics) error {
	queries := map[string]string{
		"total_deployments":  `count(kube_deployment_labels{namespace!="ollama"})`,
		"available_replicas": `sum(kube_deployment_status_replicas_available{namespace!="ollama"})`,
		"desired_replicas":   `sum(kube_deployment_spec_replicas{namespace!="ollama"})`,
		"unavailable":        `sum(kube_deployment_status_replicas_unavailable{namespace!="ollama"})`,
	}

	results, err := c.queryRange(ctx, queries)
	if err != nil {
		return err
	}

	m.Deployments.Total = int(floatValue(results["total_deployments"]))
	m.Deployments.Ready = int(floatValue(results["available_replicas"]))
	m.Deployments.Unready = int(floatValue(results["desired_replicas"])) - int(floatValue(results["available_replicas"]))
	m.Deployments.Unavailable = int(floatValue(results["unavailable"]))

	return nil
}

// queryJobMetrics queries job status from Mimir
// NOTE: Excludes ollama namespace
func (c *Client) queryJobMetrics(ctx context.Context, m *Metrics) error {
	queries := map[string]string{
		"total_jobs":     `count(kube_job_labels{namespace!="ollama"})`,
		"active_jobs":    `count(kube_job_status_active{namespace!="ollama"})`,
		"failed_jobs":    `count(kube_job_status_failed{namespace!="ollama"})`,
		"succeeded_jobs": `count(kube_job_status_succeeded{namespace!="ollama"})`,
	}

	results, err := c.queryRange(ctx, queries)
	if err != nil {
		return err
	}

	m.Jobs.Total = int(floatValue(results["total_jobs"]))
	m.Jobs.Active = int(floatValue(results["active_jobs"]))
	m.Jobs.Failed = int(floatValue(results["failed_jobs"]))
	m.Jobs.Succeeded = int(floatValue(results["succeeded_jobs"]))

	return nil
}

// queryServiceMetrics queries service status from Mimir
func (c *Client) queryServiceMetrics(ctx context.Context, m *Metrics) error {
	queries := map[string]string{
		"total_services": `count(kube_service_labels)`,
		"cluster_ip":     `count(kube_service_spec_type{type="ClusterIP"})`,
		"headless":       `count(kube_service_spec_type{type="ClusterIP"} unless kube_service_spec_cluster_ip)`,
		"loadbalancer":   `count(kube_service_spec_type{type="LoadBalancer"})`,
	}

	results, err := c.queryRange(ctx, queries)
	if err != nil {
		return err
	}

	m.Services.Total = int(floatValue(results["total_services"]))
	m.Services.ClusterIP = int(floatValue(results["cluster_ip"]))
	m.Services.Headless = int(floatValue(results["headless"]))
	m.Services.TypeLoadBalancer = int(floatValue(results["loadbalancer"]))

	return nil
}

// queryStorageMetrics queries PVC status from Mimir
func (c *Client) queryStorageMetrics(ctx context.Context, m *Metrics) error {
	queries := map[string]string{
		"total_pvcs":   `count(kube_persistentvolumeclaim_info)`,
		"bound_pvcs":   `count(kube_persistentvolumeclaim_status_phase{phase="Bound"})`,
		"pending_pvcs": `count(kube_persistentvolumeclaim_status_phase{phase="Pending"})`,
		"lost_pvcs":    `count(kube_persistentvolumeclaim_status_phase{phase="Lost"})`,
	}

	results, err := c.queryRange(ctx, queries)
	if err != nil {
		return err
	}

	m.Storage.TotalPVCs = int(floatValue(results["total_pvcs"]))
	m.Storage.BoundPVCs = int(floatValue(results["bound_pvcs"]))
	m.Storage.PendingPVCs = int(floatValue(results["pending_pvcs"]))
	m.Storage.LostPVCs = int(floatValue(results["lost_pvcs"]))

	return nil
}

// queryResourceMetrics queries resource usage from Mimir
func (c *Client) queryResourceMetrics(ctx context.Context, m *Metrics) error {
	queries := map[string]string{
		"cpu_usage":      `round(100*(1-avg(rate(node_cpu_seconds_total{mode="idle"}[5m]))),1)`,
		"mem_usage":      `round(100*(1-sum(node_memory_MemAvailable_bytes)/sum(node_memory_MemTotal_bytes)),1)`,
		"disk_usage":     `round(100*(1-sum(node_filesystem_avail_bytes{mountpoint="/"})/sum(node_filesystem_size_bytes{mountpoint="/"})),1)`,
		"mem_available":  `round(sum(node_memory_MemAvailable_bytes)/1024/1024/1024,1)`,
		"disk_available": `round(sum(node_filesystem_avail_bytes{mountpoint="/"})/1024/1024/1024,1)`,
	}

	results, err := c.queryRange(ctx, queries)
	if err != nil {
		// Return partial results if some queries fail
		m.Resources.CPUUsagePercent = floatValue(results["cpu_usage"])
		m.Resources.MemoryUsagePercent = floatValue(results["mem_usage"])
		m.Resources.DiskUsagePercent = floatValue(results["disk_usage"])
		m.Resources.AvailableMemoryGB = floatValue(results["mem_available"])
		fmt.Printf("[DEBUG] Resource metrics (partial): cpu=%.2f%%, mem=%.2f%%, disk=%.2f%%\n", m.Resources.CPUUsagePercent, m.Resources.MemoryUsagePercent, m.Resources.DiskUsagePercent)
		return nil
	}

	m.Resources.CPUUsagePercent = floatValue(results["cpu_usage"])
	m.Resources.MemoryUsagePercent = floatValue(results["mem_usage"])
	m.Resources.DiskUsagePercent = floatValue(results["disk_usage"])
	m.Resources.AvailableMemoryGB = floatValue(results["mem_available"])
	m.Resources.AvailableStorageGB = floatValue(results["disk_available"])

	return nil
}

// queryApplicationMetrics queries application-level metrics like HTTP request rates and latency
func (c *Client) queryApplicationMetrics(ctx context.Context, m *Metrics) error {
	m.Applications.Applications = make(map[string]AppDetail)

	// List of applications to monitor
	apps := []string{
		"fresnel-backend",
		"argocd-server",
		"argocd-repo-server",
		"registry",
	}

	for _, app := range apps {
		detail := AppDetail{Name: app}

		// Query request rate (requests per second over last 5 minutes)
		reqRateQuery := fmt.Sprintf(
			`sum(rate(http_requests_total{job="%s"}[5m]))`,
			app,
		)
		if val, err := c.query(ctx, reqRateQuery); err == nil {
			detail.RequestRate = val
		}

		// Query error rate (5xx and 4xx errors per second)
		errRateQuery := fmt.Sprintf(
			`sum(rate(http_requests_total{job="%s",status=~"[45].."}[5m]))`,
			app,
		)
		if val, err := c.query(ctx, errRateQuery); err == nil {
			detail.ErrorRate = val
		}

		// Query error percentage
		errPctQuery := fmt.Sprintf(
			`round(100*sum(rate(http_requests_total{job="%s",status=~"[45].."}[5m]))/sum(rate(http_requests_total{job="%s"}[5m])),1)`,
			app, app,
		)
		if val, err := c.query(ctx, errPctQuery); err == nil {
			detail.ErrorPercent = val
		}

		// Query p50 latency (milliseconds)
		p50Query := fmt.Sprintf(
			`histogram_quantile(0.5, sum(rate(http_request_duration_seconds_bucket{job="%s"}[5m])) by (le)) * 1000`,
			app,
		)
		if val, err := c.query(ctx, p50Query); err == nil {
			detail.P50LatencyMs = val
		}

		// Query p95 latency
		p95Query := fmt.Sprintf(
			`histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket{job="%s"}[5m])) by (le)) * 1000`,
			app,
		)
		if val, err := c.query(ctx, p95Query); err == nil {
			detail.P95LatencyMs = val
		}

		// Query p99 latency
		p99Query := fmt.Sprintf(
			`histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket{job="%s"}[5m])) by (le)) * 1000`,
			app,
		)
		if val, err := c.query(ctx, p99Query); err == nil {
			detail.P99LatencyMs = val
		}

		// Query deployment replicas
		replicasQuery := fmt.Sprintf(
			`sum(kube_deployment_status_replicas_available{deployment="%s"})`,
			app,
		)
		if val, err := c.query(ctx, replicasQuery); err == nil {
			detail.AvailableReplicas = int(val)
		}

		desiredQuery := fmt.Sprintf(
			`sum(kube_deployment_spec_replicas{deployment="%s"})`,
			app,
		)
		if val, err := c.query(ctx, desiredQuery); err == nil {
			detail.DesiredReplicas = int(val)
		}

		m.Applications.Applications[app] = detail
	}

	return nil
}

// queryRange executes multiple PromQL queries and returns results
func (c *Client) queryRange(ctx context.Context, queries map[string]string) (map[string]float64, error) {
	results := make(map[string]float64)

	for key, query := range queries {
		val, err := c.query(ctx, query)
		if err != nil {
			// Log but don't fail entire query set
			fmt.Printf("[WARN] query failed for %s: %v\n", key, err)
			results[key] = 0
			continue
		}
		results[key] = val
	}

	return results, nil
}

// query executes a single PromQL query
func (c *Client) query(ctx context.Context, promql string) (float64, error) {
	u, err := url.Parse(c.baseURL + "/api/v1/query")
	if err != nil {
		return 0, err
	}

	q := u.Query()
	q.Set("query", promql)
	// Query at now-5min to stay within Mimir staleness window (scrape data lags ~5min)
	q.Set("time", fmt.Sprintf("%d", time.Now().Unix()-300))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return 0, err
	}

	// Add Mimir tenant header
	req.Header.Set("X-Scope-OrgID", "admin")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("query failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("query returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			Result []struct {
				Value [2]interface{} `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(result.Data.Result) == 0 {
		return 0, nil
	}

	// Extract numeric value from response
	if len(result.Data.Result[0].Value) >= 2 {
		return floatFromInterface(result.Data.Result[0].Value[1]), nil
	}

	return 0, nil
}

// Helper functions

func floatValue(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case string:
		// Try to parse as float
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	default:
		return 0
	}
}

func floatFromInterface(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	default:
		return 0
	}
}

// ToMap converts Metrics to map format for cache enrichment
func (m *Metrics) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"nodes": map[string]interface{}{
			"total":         m.Nodes.Total,
			"ready":         m.Nodes.Ready,
			"not_ready":     m.Nodes.NotReady,
			"unschedulable": m.Nodes.Unschedulable,
			"cpu_cores":     m.Nodes.CPUCores,
			"memory_gb":     m.Nodes.MemoryGB,
		},
		"pods": map[string]interface{}{
			"total":         m.Pods.Total,
			"running":       m.Pods.Running,
			"pending":       m.Pods.Pending,
			"failed":        m.Pods.Failed,
			"succeeded":     m.Pods.Succeeded,
			"restarts":      m.Pods.Restarts,
			"unschedulable": m.Pods.Unschedulable,
		},
		"resources": map[string]interface{}{
			"cpu_usage_percent":     m.Resources.CPUUsagePercent,
			"memory_usage_percent":  m.Resources.MemoryUsagePercent,
			"disk_usage_percent":    m.Resources.DiskUsagePercent,
			"available_memory_gb":   m.Resources.AvailableMemoryGB,
			"available_storage_gb":  m.Resources.AvailableStorageGB,
			"cpu_cores_allocatable": m.Resources.CPUCoresAllocatable,
			"memory_gb_allocatable": m.Resources.MemoryGBAllocatable,
		},
		"deployments": map[string]interface{}{
			"total":       m.Deployments.Total,
			"ready":       m.Deployments.Ready,
			"unready":     m.Deployments.Unready,
			"unavailable": m.Deployments.Unavailable,
		},
		"jobs": map[string]interface{}{
			"total":     m.Jobs.Total,
			"active":    m.Jobs.Active,
			"failed":    m.Jobs.Failed,
			"succeeded": m.Jobs.Succeeded,
		},
		"services": map[string]interface{}{
			"total":        m.Services.Total,
			"cluster_ip":   m.Services.ClusterIP,
			"headless":     m.Services.Headless,
			"loadbalancer": m.Services.TypeLoadBalancer,
		},
		"storage": map[string]interface{}{
			"total_pvcs":          m.Storage.TotalPVCs,
			"bound_pvcs":          m.Storage.BoundPVCs,
			"pending_pvcs":        m.Storage.PendingPVCs,
			"lost_pvcs":           m.Storage.LostPVCs,
			"storage_used_gb":     m.Storage.StorageUsedGB,
			"storage_capacity_gb": m.Storage.StorageCapacityGB,
		},
		"applications": m.applicationsToMap(),
	}
}

// applicationsToMap converts application metrics to map format
func (m *Metrics) applicationsToMap() map[string]interface{} {
	appMaps := make(map[string]interface{})
	for name, detail := range m.Applications.Applications {
		appMaps[name] = map[string]interface{}{
			"request_rate_rps":   detail.RequestRate,
			"error_rate_rps":     detail.ErrorRate,
			"error_percent":      detail.ErrorPercent,
			"p50_latency_ms":     detail.P50LatencyMs,
			"p95_latency_ms":     detail.P95LatencyMs,
			"p99_latency_ms":     detail.P99LatencyMs,
			"available_replicas": detail.AvailableReplicas,
			"desired_replicas":   detail.DesiredReplicas,
		}
	}
	return appMaps
}
