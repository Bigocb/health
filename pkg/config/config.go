package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Mimir    MimirConfig    `yaml:"mimir"`
	Discord  DiscordConfig  `yaml:"discord"`
	Health   HealthConfig   `yaml:"health"`
	Storage  StorageConfig  `yaml:"storage"`
	Analysis AnalysisConfig `yaml:"analysis"`
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

type StorageConfig struct {
	ReportsDirectory string `yaml:"reports_directory"`
	RetentionHours   int    `yaml:"retention_hours"`
}

type AnalysisConfig struct {
	Enabled        bool         `yaml:"enabled"`
	TimeoutSeconds int          `yaml:"timeout_seconds"`
	Trends         TrendsConfig `yaml:"trends"`
	LLM            LLMConfig    `yaml:"llm"`
	Output         OutputConfig `yaml:"output"`
}

type TrendsConfig struct {
	WindowHours      int     `yaml:"window_hours"`
	AnomalyThreshold float64 `yaml:"anomaly_threshold"`
	MinDataPoints    int     `yaml:"min_data_points"`
}

type LLMConfig struct {
	Enabled        bool   `yaml:"enabled"`
	Provider       string `yaml:"provider"` // "ollama", "openai", "anthropic"
	Model          string `yaml:"model"`
	Endpoint       string `yaml:"endpoint"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
	MaxRetries     int    `yaml:"max_retries"`
}

type OutputConfig struct {
	Format                 string `yaml:"format"`
	IncludeTrends          bool   `yaml:"include_trends"`
	IncludePredictions     bool   `yaml:"include_predictions"`
	IncludeRecommendations bool   `yaml:"include_recommendations"`
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

	// Fill in Discord webhook URL from env var if not in config
	if cfg.Discord.WebhookURL == "" {
		cfg.Discord.WebhookURL = os.Getenv("DISCORD_WEBHOOK_URL")
	}

	// Apply defaults for optional sections
	if cfg.Storage.ReportsDirectory == "" {
		cfg.Storage.ReportsDirectory = "/var/lib/health-reporter/reports"
	}
	if cfg.Storage.RetentionHours == 0 {
		cfg.Storage.RetentionHours = 168 // 7 days
	}
	if cfg.Analysis.TimeoutSeconds == 0 {
		cfg.Analysis.TimeoutSeconds = 30
	}
	if cfg.Analysis.Trends.WindowHours == 0 {
		cfg.Analysis.Trends.WindowHours = 24
	}
	if cfg.Analysis.Trends.MinDataPoints == 0 {
		cfg.Analysis.Trends.MinDataPoints = 6
	}
	if cfg.Analysis.Trends.AnomalyThreshold == 0 {
		cfg.Analysis.Trends.AnomalyThreshold = 1.5
	}
	if cfg.Analysis.LLM.TimeoutSeconds == 0 {
		cfg.Analysis.LLM.TimeoutSeconds = 15
	}
	if cfg.Analysis.LLM.MaxRetries == 0 {
		cfg.Analysis.LLM.MaxRetries = 2
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
		Storage: StorageConfig{
			ReportsDirectory: "/var/lib/health-reporter/reports",
			RetentionHours:   168,
		},
		Analysis: AnalysisConfig{
			Enabled:        false,
			TimeoutSeconds: 30,
			Trends: TrendsConfig{
				WindowHours:      24,
				AnomalyThreshold: 1.5,
				MinDataPoints:    6,
			},
			LLM: LLMConfig{
				Enabled:        false,
				Provider:       "ollama",
				Model:          "llama3.2:1b",
				Endpoint:       "http://ollama:11434",
				TimeoutSeconds: 15,
				MaxRetries:     2,
			},
			Output: OutputConfig{
				Format:                 "json",
				IncludeTrends:          true,
				IncludePredictions:     true,
				IncludeRecommendations: true,
			},
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
