package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/user/wc-cal-sync/internal/config"
	"github.com/user/wc-cal-sync/internal/store"
	"github.com/user/wc-cal-sync/internal/sync"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to config file")
	runOnce := flag.Bool("once", false, "Run one sync cycle and exit")
	dryRun := flag.Bool("dry-run", false, "Log what would happen without making changes")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// CLI flag overrides config
	if *dryRun {
		cfg.Sync.DryRun = true
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Shutting down...")
		cancel()
	}()

	// Load state
	state := store.New(cfg.StateFile)
	if err := state.Load(); err != nil {
		log.Fatalf("Failed to load state: %v", err)
	}

	// Initialize providers
	providers, err := sync.BuildProviders(ctx, cfg)
	if err != nil {
		log.Fatalf("Failed to initialize providers: %v", err)
	}
	log.Printf("Initialized %d calendar providers", len(providers))

	// Initialize merged calendar writer
	var merged *sync.MergedWriter
	if cfg.MergedCalendar.Provider != "" {
		merged, err = sync.NewMergedWriter(ctx, cfg)
		if err != nil {
			log.Fatalf("Failed to initialize merged calendar: %v", err)
		}
		log.Println("Merged calendar writer initialized")
	}

	// Build engine
	engine := sync.NewEngine(cfg, providers, merged, state)

	if *runOnce {
		if err := engine.Run(ctx); err != nil {
			log.Fatalf("Sync failed: %v", err)
		}
		return
	}

	// HTTP server mode
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Sync endpoint (triggered by Cloud Scheduler or manually)
	mux.HandleFunc("/sync", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		log.Println("Sync triggered via HTTP")
		syncCtx, syncCancel := context.WithTimeout(ctx, 5*time.Minute)
		defer syncCancel()

		if err := engine.Run(syncCtx); err != nil {
			log.Printf("Sync error: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("sync complete"))
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	// Start periodic sync in background
	go func() {
		ticker := time.NewTicker(time.Duration(cfg.Sync.IntervalMinutes) * time.Minute)
		defer ticker.Stop()

		// Run once immediately on startup
		if err := engine.Run(ctx); err != nil {
			log.Printf("Initial sync error: %v", err)
		}

		for {
			select {
			case <-ticker.C:
				syncCtx, syncCancel := context.WithTimeout(ctx, 5*time.Minute)
				if err := engine.Run(syncCtx); err != nil {
					log.Printf("Periodic sync error: %v", err)
				}
				syncCancel()
			case <-ctx.Done():
				return
			}
		}
	}()

	log.Printf("Server listening on :%s", port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}
