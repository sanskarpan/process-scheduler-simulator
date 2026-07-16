package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sanskar/scheduler-simulator/internal/api"
	"github.com/sanskar/scheduler-simulator/internal/config"
	"github.com/sanskar/scheduler-simulator/internal/logging"
	"github.com/sanskar/scheduler-simulator/internal/metrics"
	"github.com/sanskar/scheduler-simulator/internal/middleware"
	"github.com/sanskar/scheduler-simulator/internal/store"
	"github.com/sanskar/scheduler-simulator/internal/version"
	"github.com/sanskar/scheduler-simulator/web"
)

// buildVersion is overridden at build time via -ldflags "-X main.buildVersion=...".
var buildVersion = version.Version

func main() {
	// --health flag: perform a quick liveness probe and exit 0/1.
	// Used by the Dockerfile HEALTHCHECK so the binary can self-probe
	// without bundling wget or curl in the distroless image.
	if len(os.Args) == 2 && os.Args[1] == "--health" {
		port := os.Getenv("PORT")
		if port == "" {
			port = ":8082"
		}
		if port[0] != ':' {
			port = ":" + port
		}
		url := fmt.Sprintf("http://localhost%s/health", port)
		resp, err := (&http.Client{Timeout: 3 * time.Second}).Get(url) //nolint:noctx
		if err != nil {
			fmt.Fprintln(os.Stderr, "health check failed:", err)
			os.Exit(1)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			fmt.Fprintln(os.Stderr, "health check status:", resp.StatusCode)
			os.Exit(1)
		}
		os.Exit(0)
	}

	cfg, err := config.FromEnv().Validate()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	logging.Init(cfg.LogLevel, cfg.LogFormat)
	logger := logging.Logger.With("version", buildVersion)
	logger.Info("starting process scheduler simulator",
		"port", cfg.Port,
		"static_dir", cfg.StaticDir,
		"log_level", cfg.LogLevel,
		"metrics_enabled", cfg.EnableMetrics,
	)

	// Resolve static directory.
	staticDir := cfg.StaticDir
	absStatic, err := filepath.Abs(staticDir)
	if err != nil {
		logger.Warn("could not resolve static dir", "error", err)
		absStatic = staticDir
	}

	// Build the web server (WebSocket + simulator engine).
	server := web.NewServer(cfg)

	// Simulation history store for the REST API.
	historyStore := store.New(100)

	// REST API handler.
	apiHandler := api.NewHandler(historyStore, cfg.DefaultTimeQuantum, cfg.DefaultSpeed, cfg.SimConcurrencyLimit)

	// Build the HTTP mux. Go 1.22+ method+pattern routing is used.
	mux := http.NewServeMux()

	// REST API.
	apiHandler.Register(mux)

	// Static files.
	fs := http.FileServer(http.Dir(absStatic))
	mux.Handle("/", fs)

	// WebSocket + health (on the web.Server).
	mux.HandleFunc("/ws", server.HandleWebSocket)
	mux.HandleFunc("/health", server.HandleHealth)

	// Prometheus metrics.
	if cfg.EnableMetrics {
		mux.Handle("/metrics", metrics.Handler())
	}
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"` + buildVersion + `"}`))
	})

	// Wrap mux with middleware (outermost first).
	handler := middleware.Chain(
		mux,
		middleware.Recovery,
		middleware.SecureHeaders,
		middleware.RequestID,
		middleware.LogAndMetrics,
		middleware.CORS(cfg.WSOriginAllow),
	)

	srv := &http.Server{
		Addr:              cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout, // 0 = no deadline (needed for WS)
		IdleTimeout:       cfg.IdleTimeout,
	}
	server.SetHTTPServer(srv)

	logger.Info("listening", "addr", srv.Addr)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen error", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown on SIGINT / SIGTERM.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logger.Info("shutdown signal received, draining...")

	server.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("http shutdown error", "error", err)
	}
	logger.Info("server stopped")
}

// touch time to avoid unused import in some builds.
var _ = time.Second
