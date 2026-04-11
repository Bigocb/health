package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ArchipelagoAI/health-reporter/pkg/config"
	"github.com/ArchipelagoAI/health-reporter/pkg/health"
	"github.com/ArchipelagoAI/health-reporter/pkg/mimir"
	"github.com/ArchipelagoAI/health-reporter/pkg/webhook"
)

const version = "0.1.0"

func main() {
	var (
		configPath  = flag.String("config", "config.yaml", "Path to config file")
		runOnce     = flag.Bool("once", false, "Run report once and exit (no daemon mode)")
		interval    = flag.Duration("interval", 1*time.Hour, "Interval between reports (daemon mode)")
		showVersion = flag.Bool("version", false, "Show version and exit")
		mimirURL    = flag.String("mimir-url", "", "Mimir query endpoint (overrides config)")
		discordURL  = flag.String("discord-webhook", "", "Discord webhook URL (overrides config)")
		verbose     = flag.Bool("verbose", false, "Enable verbose logging")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("health-reporter v%s\n", version)
		os.Exit(0)
	}

	// Setup logging
	logFlags := log.LstdFlags | log.Lshortfile
	if *verbose {
		log.SetFlags(logFlags)
	}
	log.SetPrefix("[health-reporter] ")

	// Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Printf("warning: config file not found (%v), using defaults", err)
		cfg = config.DefaultConfig()
	}

	// Override config with CLI flags (only if explicitly provided)
	if *mimirURL != "" {
		cfg.Mimir.URL = *mimirURL
	}
	if *discordURL != "" {
		cfg.Discord.WebhookURL = *discordURL
	}

	// Initialize clients
	mimirClient, err := mimir.NewClient(cfg.Mimir.URL)
	if err != nil {
		log.Fatalf("failed to initialize mimir client: %v", err)
	}
	defer mimirClient.Close()

	webhookSender := webhook.NewDiscordSender(cfg.Discord.WebhookURL)

	// Create health reporter
	reporter := health.NewReporter(mimirClient, webhookSender)

	log.Printf("health-reporter v%s started", version)
	log.Printf("mimir: %s", cfg.Mimir.URL)
	log.Printf("discord: %s", mask(cfg.Discord.WebhookURL))

	if *runOnce {
		// Run once and exit
		if err := runReport(reporter); err != nil {
			log.Printf("error: %v", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Daemon mode
	runDaemon(reporter, *interval)
}

func runReport(reporter *health.Reporter) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	report, err := reporter.Generate(ctx)
	if err != nil {
		return fmt.Errorf("failed to generate report: %w", err)
	}

	if err := reporter.SendReport(ctx, report); err != nil {
		return fmt.Errorf("failed to send report: %w", err)
	}

	log.Printf("report sent successfully (status: %s)", report.Status)
	return nil
}

func runDaemon(reporter *health.Reporter, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Initial report
	if err := runReport(reporter); err != nil {
		log.Printf("initial report failed: %v", err)
	}

	for {
		select {
		case <-ticker.C:
			if err := runReport(reporter); err != nil {
				log.Printf("report cycle failed: %v", err)
			}

		case sig := <-sigChan:
			log.Printf("received signal: %v, shutting down", sig)
			os.Exit(0)
		}
	}
}

// mask returns last 8 chars of URL for logging (hides token)
func mask(url string) string {
	if len(url) <= 8 {
		return "***"
	}
	return "***" + url[len(url)-8:]
}
