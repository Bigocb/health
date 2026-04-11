package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ArchipelagoAI/health-reporter/pkg/health"
)

func TestDiscordSender_Send_Success(t *testing.T) {
	// Create mock Discord server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content type")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sender := NewDiscordSender(server.URL)
	report := &health.Report{
		Timestamp: time.Now(),
		Status:    health.StatusHealthy,
		Summary:   "Test report",
		ClusterMetrics: health.Metrics{
			Nodes: health.NodeMetrics{
				Total: 3,
				Ready: 3,
			},
			Pods: health.PodMetrics{
				Running: 100,
			},
			Resources: health.ResourceMetrics{
				CPUUsagePercent:    50.0,
				MemoryUsagePercent: 60.0,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := sender.Send(ctx, report)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestDiscordSender_Send_NoWebhookURL(t *testing.T) {
	sender := NewDiscordSender("")
	report := &health.Report{
		Timestamp: time.Now(),
		Status:    health.StatusHealthy,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := sender.Send(ctx, report)
	if err == nil {
		t.Errorf("expected error for missing webhook URL")
	}
}

func TestDiscordSender_Send_ServerError(t *testing.T) {
	// Create mock server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("invalid payload"))
	}))
	defer server.Close()

	sender := NewDiscordSender(server.URL)
	report := &health.Report{
		Timestamp: time.Now(),
		Status:    health.StatusHealthy,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := sender.Send(ctx, report)
	if err == nil {
		t.Errorf("expected error for server error response")
	}
}

func TestColorForStatus_Healthy(t *testing.T) {
	sender := NewDiscordSender("http://test")
	color := sender.colorForStatus(health.StatusHealthy)
	if color != 0x00FF00 {
		t.Errorf("expected green (0x00FF00), got %x", color)
	}
}

func TestColorForStatus_Degraded(t *testing.T) {
	sender := NewDiscordSender("http://test")
	color := sender.colorForStatus(health.StatusDegraded)
	if color != 0xFFA500 {
		t.Errorf("expected orange (0xFFA500), got %x", color)
	}
}

func TestColorForStatus_Critical(t *testing.T) {
	sender := NewDiscordSender("http://test")
	color := sender.colorForStatus(health.StatusCritical)
	if color != 0xFF0000 {
		t.Errorf("expected red (0xFF0000), got %x", color)
	}
}

func TestEmojiForStatus_Healthy(t *testing.T) {
	sender := NewDiscordSender("http://test")
	emoji := sender.emojiForStatus(health.StatusHealthy)
	if emoji != "✅" {
		t.Errorf("expected ✅, got %s", emoji)
	}
}

func TestEmojiForStatus_Degraded(t *testing.T) {
	sender := NewDiscordSender("http://test")
	emoji := sender.emojiForStatus(health.StatusDegraded)
	if emoji != "⚠️" {
		t.Errorf("expected ⚠️, got %s", emoji)
	}
}

func TestEmojiForStatus_Critical(t *testing.T) {
	sender := NewDiscordSender("http://test")
	emoji := sender.emojiForStatus(health.StatusCritical)
	if emoji != "🚨" {
		t.Errorf("expected 🚨, got %s", emoji)
	}
}
