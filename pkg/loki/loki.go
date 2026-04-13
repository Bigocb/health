package loki

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
}

type QueryResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Values [][]interface{}   `json:"values"`
			Value  []interface{}     `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

type LogQueryResponse struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

type ErrorSummary struct {
	TotalErrors int
	TopErrors   []string
	ErrorRate5m float64
}

func NewClient(baseURL, username, password string) *Client {
	return &Client{
		baseURL:    baseURL,
		username:   username,
		password:   password,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) Query(ctx context.Context, query string, limit int) ([]string, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("limit", fmt.Sprintf("%d", limit))

	url := fmt.Sprintf("%s/loki/api/v1/query?%s", c.baseURL, params.Encode())
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	// Add Loki org ID header
	req.Header.Set("X-Scope-OrgID", "admin")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Loki query failed: %s", string(body))
	}

	var result QueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse Loki response: %w", err)
	}

	var logs []string
	for _, r := range result.Data.Result {
		if len(r.Value) > 1 {
			logs = append(logs, fmt.Sprintf("%v", r.Value[1]))
		}
		for _, v := range r.Values {
			if len(v) > 1 {
				logs = append(logs, fmt.Sprintf("%v", v[1]))
			}
		}
	}

	return logs, nil
}

func (c *Client) QueryRange(ctx context.Context, query string, start, end time.Time, limit int) ([]string, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("start", start.Format(time.RFC3339Nano))
	params.Set("end", end.Format(time.RFC3339Nano))
	params.Set("limit", fmt.Sprintf("%d", limit))

	url := fmt.Sprintf("%s/loki/api/v1/query_range?%s", c.baseURL, params.Encode())
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	// Add Loki org ID header
	req.Header.Set("X-Scope-OrgID", "admin")
	// DEBUG: Verify header was set
	if val := req.Header.Get("X-Scope-OrgID"); val == "" {
		return nil, fmt.Errorf("CRITICAL: X-Scope-OrgID header not set before request")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Loki query failed: %s", string(body))
	}

	var result QueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse Loki response: %w", err)
	}

	var logs []string
	for _, r := range result.Data.Result {
		for _, v := range r.Values {
			if len(v) > 1 {
				logs = append(logs, fmt.Sprintf("%v", v[1]))
			}
		}
	}

	return logs, nil
}

func (c *Client) GetRecentErrors(ctx context.Context, duration time.Duration) (*ErrorSummary, error) {
	now := time.Now()
	start := now.Add(-duration)

	summary := &ErrorSummary{}

	recentErrorsQuery := `{level="error"} | json | line_format "{{.message}}"`
	errorLogs, err := c.QueryRange(ctx, recentErrorsQuery, start, now, 10)
	if err != nil {
		return nil, fmt.Errorf("failed to query recent errors: %w", err)
	}

	summary.TopErrors = errorLogs
	summary.TotalErrors = len(errorLogs)

	return summary, nil
}

func (c *Client) GetPodErrors(ctx context.Context, namespace, pod string, duration time.Duration) ([]string, error) {
	now := time.Now()
	start := now.Add(-duration)

	query := fmt.Sprintf(`{namespace="%s", pod="%s"} |= "error"`, namespace, pod)
	return c.QueryRange(ctx, query, start, now, 10)
}

func (c *Client) GetFailedPodsErrors(ctx context.Context) (map[string][]string, error) {
	now := time.Now()
	start := now.Add(-1 * time.Hour)

	query := `{namespace!=""} | json | level="error" | line_format "{{.namespace}}|{{.pod}}|{{.message}}"`
	logs, err := c.QueryRange(ctx, query, start, now, 50)
	if err != nil {
		return nil, err
	}

	errorsByPod := make(map[string][]string)
	for _, log := range logs {
		var namespace, pod, message string
		fmt.Sscanf(log, "%s|%s|%s", &namespace, &pod, &message)
		key := namespace + "/" + pod
		if len(errorsByPod[key]) < 3 {
			errorsByPod[key] = append(errorsByPod[key], message)
		}
	}

	return errorsByPod, nil
}

func (c *Client) IsAvailable(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/ready", nil)
	if err != nil {
		return false
	}

	// Add Loki org ID header
	req.Header.Set("X-Scope-OrgID", "admin")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}
