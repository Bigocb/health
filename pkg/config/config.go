package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Mimir   MimirConfig   `yaml:"mimir"`
	Discord DiscordConfig `yaml:"discord"`
	Health  HealthConfig  `yaml:"health"`
}

type MimirConfig struct {
	URL      string `yaml:"url"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
}

type DiscordConfig struct {
	WebhookURL string `yaml:"webhook_url"`
}

type HealthConfig struct {
	CPUThreshold    float64 `yaml:"cpu_threshold"`
	MemoryThreshold float64 `yaml:"memory_threshold"`
	DiskThreshold   float64 `yaml:"disk_threshold"`
	RestartLimit    int     `yaml:"restart_limit"`
}

// LoadConfig loads configuration from YAML file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &cfg, nil
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		Mimir: MimirConfig{
			URL: "http://mimir-query:9009",
		},
		Discord: DiscordConfig{
			WebhookURL: os.Getenv("DISCORD_WEBHOOK_URL"),
		},
		Health: HealthConfig{
			CPUThreshold:    80,
			MemoryThreshold: 85,
			DiskThreshold:   90,
			RestartLimit:    5,
		},
	}
}

// SaveConfig saves configuration to YAML file
func SaveConfig(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
