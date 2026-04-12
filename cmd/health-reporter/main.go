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

	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"

	healthv1alpha1 "github.com/ArchipelagoAI/health-reporter/api/v1alpha1"
	"github.com/ArchipelagoAI/health-reporter/controllers"
	"github.com/ArchipelagoAI/health-reporter/pkg/config"
	"github.com/ArchipelagoAI/health-reporter/pkg/health"
	"github.com/ArchipelagoAI/health-reporter/pkg/mimir"
	"github.com/ArchipelagoAI/health-reporter/pkg/smoke_tests"
	"github.com/ArchipelagoAI/health-reporter/pkg/webhook"
)

const version = "0.2.0"

func main() {
	var (
		configPath  = flag.String("config", "config.yaml", "Path to config file")
		runOnce     = flag.Bool("once", false, "Run one report and exit (for local testing)")
		interval    = flag.Duration("interval", 1*time.Hour, "Interval between reports")
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

	// Create shared test registry — the single source of truth for smoke tests.
	// The CRD controller populates it, and the reporter reads from it.
	testRegistry := smoke_tests.NewTestRegistry()

	// Create health reporter with shared registry
	reporter := health.NewReporter(mimirClient, webhookSender)
	reporter.SetTestRegistry(testRegistry)

	log.Printf("health-reporter v%s started", version)
	log.Printf("mimir: %s", cfg.Mimir.URL)
	log.Printf("discord: %s", mask(cfg.Discord.WebhookURL))

	// Setup root context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start the CRD controller in background.
	// The controller watches SmokeTest CRDs and keeps testRegistry in sync.
	controllerReady := make(chan struct{})
	go func() {
		if err := startController(ctx, testRegistry, controllerReady); err != nil {
			log.Fatalf("controller failed: %v", err)
		}
	}()

	// Wait for controller cache to sync before generating reports.
	// This ensures the test registry is populated from existing CRDs.
	log.Println("waiting for controller cache sync...")
	select {
	case <-controllerReady:
		log.Println("controller ready, test registry populated")
	case <-time.After(30 * time.Second):
		log.Println("warning: controller cache sync timed out after 30s, proceeding anyway")
	case sig := <-sigChan:
		log.Printf("received signal during startup: %v, shutting down", sig)
		cancel()
		os.Exit(0)
	}

	if *runOnce {
		// Run one report and exit (useful for local testing / debugging)
		if err := runReport(reporter); err != nil {
			log.Printf("error: %v", err)
			os.Exit(1)
		}
		cancel()
		os.Exit(0)
	}

	// Daemon mode: generate reports on interval
	log.Printf("entering daemon mode (interval: %s)", *interval)
	runDaemon(ctx, cancel, reporter, *interval, sigChan)
}

func runReport(reporter *health.Reporter) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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

func runDaemon(ctx context.Context, cancel context.CancelFunc, reporter *health.Reporter, interval time.Duration, sigChan <-chan os.Signal) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run initial report immediately
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
			log.Printf("received signal: %v, shutting down gracefully", sig)
			cancel()
			return

		case <-ctx.Done():
			log.Println("context cancelled, shutting down")
			return
		}
	}
}

// startController initializes and starts the Kubernetes CRD controller.
// It signals readiness via the ready channel once the cache is synced.
func startController(ctx context.Context, testRegistry smoke_tests.TestRegistry, ready chan<- struct{}) error {
	// Register API types
	if err := healthv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		return fmt.Errorf("failed to register SmokeTest type: %w", err)
	}

	// Create controller manager
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme.Scheme,
	})
	if err != nil {
		return fmt.Errorf("failed to create manager: %w", err)
	}

	// Setup reconciler
	if err = (&controllers.SmokeTestReconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		TestRegistry: testRegistry,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("failed to setup controller: %w", err)
	}

	// Signal ready once cache is synced (runs in background)
	go func() {
		cache := mgr.GetCache()
		if cache.WaitForCacheSync(ctx) {
			log.Println("controller cache synced")
			close(ready)
		}
	}()

	log.Println("starting SmokeTest controller")

	// Start manager (blocks until context is cancelled)
	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("manager exited with error: %w", err)
	}

	return nil
}

// mask returns last 8 chars of URL for logging (hides token)
func mask(url string) string {
	if len(url) <= 8 {
		return "***"
	}
	return "***" + url[len(url)-8:]
}
