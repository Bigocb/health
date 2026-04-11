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
	Timestamp time.Time
	Nodes     NodeMetrics
	Pods      PodMetrics
	Resources ResourceMetrics
}

type NodeMetrics struct {
	Total    int
	Ready    int
	NotReady int
}

type PodMetrics struct {
	Total    int
	Running  int
	Pending  int
	Failed   int
	Restarts int // Last 1h
}

type ResourceMetrics struct {
	CPUUsagePercent    float64
	MemoryUsagePercent float64
	DiskUsagePercent   float64
	AvailableMemoryGB  float64
	AvailableStorageGB float64
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

	// Query pod metrics
	if err := c.queryPodMetrics(ctx, metrics); err != nil {
		return nil, fmt.Errorf("failed to query pod metrics: %w", err)
	}

	// Query resource metrics
	if err := c.queryResourceMetrics(ctx, metrics); err != nil {
		return nil, fmt.Errorf("failed to query resource metrics: %w", err)
	}

	return metrics, nil
}

// queryNodeMetrics queries node status from Mimir
func (c *Client) queryNodeMetrics(ctx context.Context, m *Metrics) error {
	// Query: node status (ready, not ready)
	queries := map[string]string{
		"ready_nodes":     `count(kube_node_status_condition{condition="Ready",status="true"})`,
		"total_nodes":     `count(kube_node_labels)`,
		"not_ready_nodes": `count(kube_node_status_condition{condition="Ready",status="false"})`,
	}

	results, err := c.queryRange(ctx, queries)
	if err != nil {
		return err
	}

	m.Nodes.Ready = int(floatValue(results["ready_nodes"]))
	m.Nodes.Total = int(floatValue(results["total_nodes"]))
	m.Nodes.NotReady = int(floatValue(results["not_ready_nodes"]))

	return nil
}

// queryPodMetrics queries pod status from Mimir
func (c *Client) queryPodMetrics(ctx context.Context, m *Metrics) error {
	queries := map[string]string{
		"running_pods": `count(kube_pod_status_phase{phase="Running"})`,
		"total_pods":   `count(kube_pod_info)`,
		"pending_pods": `count(kube_pod_status_phase{phase="Pending"})`,
		"failed_pods":  `count(kube_pod_status_phase{phase="Failed"})`,
		"restarts_1h":  `sum(increase(kube_pod_container_status_restarts_total[1h]))`,
	}

	results, err := c.queryRange(ctx, queries)
	if err != nil {
		return err
	}

	m.Pods.Running = int(floatValue(results["running_pods"]))
	m.Pods.Total = int(floatValue(results["total_pods"]))
	m.Pods.Pending = int(floatValue(results["pending_pods"]))
	m.Pods.Failed = int(floatValue(results["failed_pods"]))
	m.Pods.Restarts = int(floatValue(results["restarts_1h"]))

	return nil
}

// queryResourceMetrics queries resource usage from Mimir
func (c *Client) queryResourceMetrics(ctx context.Context, m *Metrics) error {
	queries := map[string]string{
		"cpu_usage":     `sum(rate(container_cpu_usage_seconds_total[5m])) / sum(machine_cpu_cores) * 100`,
		"mem_usage":     `sum(container_memory_usage_bytes) / sum(machine_memory_bytes) * 100`,
		"mem_available": `sum(machine_memory_bytes - container_memory_usage_bytes) / 1024 / 1024 / 1024`,
	}

	results, err := c.queryRange(ctx, queries)
	if err != nil {
		// Return partial results if some queries fail
		m.Resources.CPUUsagePercent = floatValue(results["cpu_usage"])
		m.Resources.MemoryUsagePercent = floatValue(results["mem_usage"])
		m.Resources.AvailableMemoryGB = floatValue(results["mem_available"])
		return nil
	}

	m.Resources.CPUUsagePercent = floatValue(results["cpu_usage"])
	m.Resources.MemoryUsagePercent = floatValue(results["mem_usage"])
	m.Resources.AvailableMemoryGB = floatValue(results["mem_available"])
	m.Resources.DiskUsagePercent = 0 // TODO: Query disk metrics if available

	return nil
}

// queryRange executes multiple PromQL queries and returns results
func (c *Client) queryRange(ctx context.Context, queries map[string]string) (map[string]float64, error) {
	results := make(map[string]float64)

	for key, query := range queries {
		val, err := c.query(ctx, query)
		if err != nil {
			// Log but don't fail entire query set
			fmt.Printf("warning: query failed for %s: %v\n", key, err)
			results[key] = 0
			continue
		}
		results[key] = val
	}

	return results, nil
}

// query executes a single PromQL query
func (c *Client) query(ctx context.Context, promql string) (float64, error) {
	u, err := url.Parse(c.baseURL + "/api/prom/query")
	if err != nil {
		return 0, err
	}

	q := u.Query()
	q.Set("query", promql)
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
		return 0, fmt.Errorf("failed to parse response: %w", err)
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
