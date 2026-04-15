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
	"github.com/ArchipelagoAI/health-reporter/pkg/analysis"
	"github.com/ArchipelagoAI/health-reporter/pkg/cache"
	"github.com/ArchipelagoAI/health-reporter/pkg/config"
	"github.com/ArchipelagoAI/health-reporter/pkg/health"
	"github.com/ArchipelagoAI/health-reporter/pkg/loki"
	"github.com/ArchipelagoAI/health-reporter/pkg/mimir"
	"github.com/ArchipelagoAI/health-reporter/pkg/smoke_tests"
	"github.com/ArchipelagoAI/health-reporter/pkg/storage"
	"github.com/ArchipelagoAI/health-reporter/pkg/webhook"
)

const version = "0.5.0"

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

	// Initialize Loki client if configured
	var lokiClient *loki.Client
	if cfg.Loki.URL != "" {
		lokiClient = loki.NewClient(cfg.Loki.URL, cfg.Loki.Username, cfg.Loki.Password)
		log.Printf("loki: %s", cfg.Loki.URL)
	}

	webhookSender := webhook.NewDiscordSender(cfg.Discord.WebhookURL)

	// Create shared test registry — the single source of truth for smoke tests.
	// The CRD controller populates it, and the reporter reads from it.
	testRegistry := smoke_tests.NewTestRegistry()

	// Create health reporter with shared registry
	reporter := health.NewReporter(mimirClient, webhookSender)
	reporter.SetTestRegistry(testRegistry)

	// Initialize storage for historical reports
	historyMgr := storage.NewHistoryManager(cfg.Storage.ReportsDirectory, cfg.Storage.RetentionHours)
	reporter.SetHistoryManager(historyMgr)

	// Set Loki client if configured
	if lokiClient != nil {
		reporter.SetLokiClient(lokiClient)
		log.Printf("Loki client configured for log analysis")
	}

	// Initialize trend analyzer
	if cfg.Analysis.Enabled {
		trendCfg := analysis.Config{
			WindowHours:      cfg.Analysis.Trends.WindowHours,
			AnomalyThreshold: cfg.Analysis.Trends.AnomalyThreshold,
			MinDataPoints:    cfg.Analysis.Trends.MinDataPoints,
		}
		analyzer := analysis.NewTrendDetector(trendCfg.WindowHours, trendCfg.AnomalyThreshold, trendCfg.MinDataPoints)
		reporter.SetAnalyzer(analyzer)

		// Initialize LLM client if enabled
		if cfg.Analysis.LLM.Enabled {
			llmClient := analysis.NewLLMClient(
				cfg.Analysis.LLM.Endpoint,
				cfg.Analysis.LLM.Model,
				cfg.Analysis.LLM.TimeoutSeconds,
				cfg.Analysis.LLM.MaxRetries,
				cfg.Analysis.LLM.MaxTokens,
				cfg.Analysis.LLM.Temperature,
			)
			reporter.SetLLMClient(llmClient)
			log.Printf("LLM analysis enabled: %s at %s (maxTokens: %d, temperature: %.2f)", cfg.Analysis.LLM.Model, cfg.Analysis.LLM.Endpoint, cfg.Analysis.LLM.MaxTokens, cfg.Analysis.LLM.Temperature)

			// Initialize separate Phase 2 LLM client if a different model is configured
			if cfg.Analysis.LLM.Phase2Model != "" && cfg.Analysis.LLM.Phase2Model != cfg.Analysis.LLM.Model {
				llmClient2 := analysis.NewLLMClient(
					cfg.Analysis.LLM.Endpoint,
					cfg.Analysis.LLM.Phase2Model,
					cfg.Analysis.LLM.TimeoutSeconds,
					cfg.Analysis.LLM.MaxRetries,
					cfg.Analysis.LLM.MaxTokens,
					cfg.Analysis.LLM.Temperature,
				)
				reporter.SetLLMClient2(llmClient2)
				log.Printf("LLM Phase 2 (narrative): %s at %s (maxTokens: %d, temperature: %.2f)", cfg.Analysis.LLM.Phase2Model, cfg.Analysis.LLM.Endpoint, cfg.Analysis.LLM.MaxTokens, cfg.Analysis.LLM.Temperature)
			}
		}
		log.Printf("trend analysis enabled (window: %dh)", cfg.Analysis.Trends.WindowHours)
	}

	log.Printf("health-reporter v%s started", version)
	log.Printf("mimir: %s", cfg.Mimir.URL)
	log.Printf("discord: %s", mask(cfg.Discord.WebhookURL))
	log.Printf("storage: %s", cfg.Storage.ReportsDirectory)

	// Setup root context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize enriched cache and background collector (after ctx is created)
	log.Printf("cache configuration: enabled=%v, interval=%ds, maxEntries=%d, maxAge=%dh, maxMem=%dMB",
		cfg.Cache.Enabled, cfg.Cache.CollectionIntervalSeconds, cfg.Cache.MaxLogEntries,
		cfg.Cache.MaxCacheAgeHours, cfg.Cache.MaxMemoryMB)

	if cfg.Cache.Enabled {
		enrichedCache := cache.NewEnrichedCache(
			cfg.Cache.MaxLogEntries,
			time.Duration(cfg.Cache.MaxCacheAgeHours)*time.Hour,
			int64(cfg.Cache.MaxMemoryMB)*1024*1024,
		)
		// Clear cache on startup to ensure fresh state (prevents stale data on pod restart)
		enrichedCache.Clear()
		log.Printf("cache cleared on startup (fresh state)")
		reporter.SetCache(enrichedCache)

		// Create and start background collector
		collector := cache.NewCacheCollector(
			enrichedCache,
			mimirClient,
			lokiClient,
			cfg.Cache.CollectionIntervalSeconds,
		)
		reporter.SetCacheCollector(collector)
		collector.Start(ctx)
		log.Printf("cache enabled: max %d log entries, %d hour retention, %d MB limit",
			cfg.Cache.MaxLogEntries, cfg.Cache.MaxCacheAgeHours, cfg.Cache.MaxMemoryMB)
		log.Printf("background collector started (interval: %ds)", cfg.Cache.CollectionIntervalSeconds)
	}

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
	// Generate metrics (Mimir queries) - 2 minute timeout
	genCtx, genCancel := context.WithTimeout(context.Background(), 120*time.Second)
	report, err := reporter.Generate(genCtx)
	genCancel()
	if err != nil {
		return fmt.Errorf("failed to generate report: %w", err)
	}

	// Run analysis if configured (Phase 1 + Phase 2 LLM calls) - 8 minute timeout
	var analysis *analysis.AnalysisResult
	if reporter.HasAnalyzer() {
		analyzeCtx, analyzeCancel := context.WithTimeout(context.Background(), 480*time.Second) // 8 minutes
		analysis = reporter.Analyze(analyzeCtx, report)
		analyzeCancel()
		if analysis != nil {
			log.Printf("analysis: %s (confidence: %.2f)", analysis.HealthSummary, analysis.ConfidenceScore)
		}
	}

	// Save and send report (quick operations) - 1 minute timeout
	reportCtx, reportCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer reportCancel()

	// Save analysis to report before sending (so it's saved to disk)
	if analysis != nil {
		report.Analysis = map[string]interface{}{
			"health_summary":   analysis.HealthSummary,
			"confidence_score": analysis.ConfidenceScore,
			"trends":           analysis.Trends,
			"anomalies":        analysis.Anomalies,
			"predictions":      analysis.Predictions,
		}
		// Update the saved report with analysis
		if err := reporter.SaveReportWithAnalysis(reportCtx, report); err != nil {
			log.Printf("failed to save report with analysis: %v", err)
		}
	}

	if err := reporter.SendReportWithAnalysis(reportCtx, report, analysis); err != nil {
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
