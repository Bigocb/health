package config

import (
	"os"
	"tempfile"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	// Create temporary config file
	tmpfile, err := os.CreateTemp("", "config*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	configContent := `
mimir:
  url: "http://mimir:9009"
discord:
  webhook_url: "https://discord.com/api/webhooks/test"
health:
  cpu_threshold: 80
  memory_threshold: 85
`

	if _, err := tmpfile.WriteString(configContent); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	tmpfile.Close()

	// Load config
	cfg, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Verify config
	if cfg.Mimir.URL != "http://mimir:9009" {
		t.Errorf("expected mimir URL 'http://mimir:9009', got '%s'", cfg.Mimir.URL)
	}

	if cfg.Discord.WebhookURL != "https://discord.com/api/webhooks/test" {
		t.Errorf("expected webhook URL, got '%s'", cfg.Discord.WebhookURL)
	}

	if cfg.Health.CPUThreshold != 80 {
		t.Errorf("expected CPU threshold 80, got %f", cfg.Health.CPUThreshold)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	cfg, err := LoadConfig("/nonexistent/config.yaml")
	if err == nil {
		t.Errorf("expected error for missing file")
	}
	if cfg != nil {
		t.Errorf("expected nil config for missing file")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg == nil {
		t.Fatalf("expected config, got nil")
	}

	if cfg.Mimir.URL != "http://mimir-query:9009" {
		t.Errorf("expected default mimir URL, got '%s'", cfg.Mimir.URL)
	}

	if cfg.Health.CPUThreshold != 80 {
		t.Errorf("expected default CPU threshold 80, got %f", cfg.Health.CPUThreshold)
	}

	if cfg.Health.MemoryThreshold != 85 {
		t.Errorf("expected default memory threshold 85, got %f", cfg.Health.MemoryThreshold)
	}
}

func TestSaveConfig(t *testing.T) {
	tmpdir, err := os.MkdirTemp("", "config_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpdir)

	cfg := &Config{
		Mimir: MimirConfig{
			URL: "http://test:9009",
		},
		Discord: DiscordConfig{
			WebhookURL: "https://test.webhook",
		},
		Health: HealthConfig{
			CPUThreshold: 75,
		},
	}

	configPath := tmpdir + "/config.yaml"
	if err := SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("config file not created")
	}

	// Load and verify
	loaded, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load saved config: %v", err)
	}

	if loaded.Mimir.URL != cfg.Mimir.URL {
		t.Errorf("mimir URL mismatch after save/load")
	}

	if loaded.Health.CPUThreshold != cfg.Health.CPUThreshold {
		t.Errorf("CPU threshold mismatch after save/load")
	}
}
