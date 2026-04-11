package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ArchipelagoAI/health-reporter/pkg/types"
)

// Sender sends reports to external services
type Sender interface {
	Send(ctx context.Context, report *types.Report) error
}

// DiscordSender sends reports to Discord
type DiscordSender struct {
	webhookURL string
	httpClient *http.Client
}

// NewDiscordSender creates a new Discord webhook sender
func NewDiscordSender(webhookURL string) *DiscordSender {
	return &DiscordSender{
		webhookURL: webhookURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Send sends a health report to Discord
func (d *DiscordSender) Send(ctx context.Context, report *types.Report) error {
	if d.webhookURL == "" {
		return fmt.Errorf("discord webhook URL not configured")
	}

	// Format Discord message
	embed := d.formatEmbed(report)
	payload := map[string]interface{}{
		"embeds": []interface{}{embed},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Send webhook
	req, err := http.NewRequestWithContext(ctx, "POST", d.webhookURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook returned %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// formatEmbed creates a Discord embed from the health report
func (d *DiscordSender) formatEmbed(report *types.Report) map[string]interface{} {
	color := d.colorForStatus(report.Status)
	emoji := d.emojiForStatus(report.Status)

	// Extract metrics safely
	nodesReady := 0
	nodesTotal := 0
	podsRunning := 0
	podsPending := 0
	podsFailed := 0
	podRestarts := 0
	cpuUsage := 0.0
	memUsage := 0.0

	if cm, ok := report.ClusterMetrics["nodes"].(map[string]interface{}); ok {
		if v, ok := cm["ready"].(float64); ok {
			nodesReady = int(v)
		}
		if v, ok := cm["total"].(float64); ok {
			nodesTotal = int(v)
		}
	}

	if cm, ok := report.ClusterMetrics["pods"].(map[string]interface{}); ok {
		if v, ok := cm["running"].(float64); ok {
			podsRunning = int(v)
		}
		if v, ok := cm["pending"].(float64); ok {
			podsPending = int(v)
		}
		if v, ok := cm["failed"].(float64); ok {
			podsFailed = int(v)
		}
		if v, ok := cm["restarts"].(float64); ok {
			podRestarts = int(v)
		}
	}

	if cm, ok := report.ClusterMetrics["resources"].(map[string]interface{}); ok {
		if v, ok := cm["cpu_usage_percent"].(float64); ok {
			cpuUsage = v
		}
		if v, ok := cm["memory_usage_percent"].(float64); ok {
			memUsage = v
		}
	}

	fields := []map[string]interface{}{
		{
			"name":   "Timestamp",
			"value":  report.Timestamp.Format(time.RFC3339),
			"inline": true,
		},
		{
			"name":   "Status",
			"value":  fmt.Sprintf("%s %s", emoji, report.Status),
			"inline": true,
		},
		{
			"name":   "Nodes",
			"value":  fmt.Sprintf("%d/%d ready", nodesReady, nodesTotal),
			"inline": true,
		},
		{
			"name":   "Pods",
			"value":  fmt.Sprintf("%d running, %d pending, %d failed", podsRunning, podsPending, podsFailed),
			"inline": true,
		},
		{
			"name":   "Restarts (1h)",
			"value":  fmt.Sprintf("%d", podRestarts),
			"inline": true,
		},
		{
			"name":   "CPU Usage",
			"value":  fmt.Sprintf("%.1f%%", cpuUsage),
			"inline": true,
		},
		{
			"name":   "Memory Usage",
			"value":  fmt.Sprintf("%.1f%%", memUsage),
			"inline": true,
		},
	}

	// Add concerns section
	if len(report.Concerns) > 0 {
		concernsText := ""
		for i, concern := range report.Concerns {
			if i > 0 {
				concernsText += "\n"
			}
			concernsText += fmt.Sprintf("• **%s** [%s]: %s", concern.Title, concern.Severity, concern.Details)
		}
		fields = append(fields, map[string]interface{}{
			"name":  "Concerns",
			"value": concernsText,
		})
	}

	// Add recommendations section
	if len(report.Recommendations) > 0 {
		recsText := ""
		for i, rec := range report.Recommendations {
			if i > 0 {
				recsText += "\n"
			}
			recsText += fmt.Sprintf("• %s", rec)
		}
		fields = append(fields, map[string]interface{}{
			"name":  "Recommendations",
			"value": recsText,
		})
	}

	embed := map[string]interface{}{
		"title":       fmt.Sprintf("Cluster Health Report - %s", report.Status),
		"description": report.Summary,
		"color":       color,
		"fields":      fields,
		"timestamp":   report.Timestamp.Format(time.RFC3339),
	}

	return embed
}

// colorForStatus returns Discord embed color for status
func (d *DiscordSender) colorForStatus(status types.HealthStatus) int {
	switch status {
	case types.StatusHealthy:
		return 0x00FF00 // Green
	case types.StatusDegraded:
		return 0xFFA500 // Orange
	case types.StatusCritical:
		return 0xFF0000 // Red
	default:
		return 0x808080 // Gray
	}
}

// emojiForStatus returns emoji for status
func (d *DiscordSender) emojiForStatus(status types.HealthStatus) string {
	switch status {
	case types.StatusHealthy:
		return "✅"
	case types.StatusDegraded:
		return "⚠️"
	case types.StatusCritical:
		return "🚨"
	default:
		return "❓"
	}
}
