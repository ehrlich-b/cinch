package main

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ehrlich-b/cinch/internal/cli"
	"github.com/ehrlich-b/cinch/internal/config"
	"github.com/ehrlich-b/cinch/internal/forge"
	"github.com/ehrlich-b/cinch/internal/server"
	"github.com/ehrlich-b/cinch/internal/storage"
	"github.com/ehrlich-b/cinch/internal/worker"
	"github.com/ehrlich-b/cinch/web"
	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	rootCmd := &cobra.Command{
		Use:     "cinch",
		Short:   "CI that's a cinch",
		Version: version,
	}

	rootCmd.AddCommand(
		serverCmd(),
		workerCmd(),
		runCmd(),
		statusCmd(),
		logsCmd(),
		configCmd(),
		tokenCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func serverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start the control plane server",
		RunE:  runServer,
	}
	cmd.Flags().String("addr", ":8080", "Address to listen on")
	cmd.Flags().String("data-dir", "", "Directory for SQLite database (default: current directory)")
	cmd.Flags().String("base-url", "", "Base URL for job links (e.g., https://cinch.example.com)")
	return cmd
}

func runServer(cmd *cobra.Command, args []string) error {
	addr, _ := cmd.Flags().GetString("addr")
	dataDir, _ := cmd.Flags().GetString("data-dir")
	baseURL, _ := cmd.Flags().GetString("base-url")

	// Allow env vars to override flags
	if envAddr := os.Getenv("CINCH_ADDR"); envAddr != "" {
		addr = envAddr
	}
	if envDataDir := os.Getenv("CINCH_DATA_DIR"); envDataDir != "" {
		dataDir = envDataDir
	}
	if envBaseURL := os.Getenv("CINCH_BASE_URL"); envBaseURL != "" {
		baseURL = envBaseURL
	}

	// Set up logger
	log := slog.Default()

	// Determine database path
	dbPath := "cinch.db"
	if dataDir != "" {
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return fmt.Errorf("create data directory: %w", err)
		}
		dbPath = filepath.Join(dataDir, "cinch.db")
	}

	// Initialize storage
	log.Info("initializing storage", "path", dbPath)
	store, err := storage.NewSQLite(dbPath)
	if err != nil {
		return fmt.Errorf("initialize storage: %w", err)
	}
	defer store.Close()

	// Create auth handler
	authConfig := server.AuthConfig{
		GitHubClientID:     os.Getenv("CINCH_GITHUB_CLIENT_ID"),
		GitHubClientSecret: os.Getenv("CINCH_GITHUB_CLIENT_SECRET"),
		JWTSecret:          os.Getenv("CINCH_JWT_SECRET"),
		BaseURL:            baseURL,
	}
	authHandler := server.NewAuthHandler(authConfig, log)

	// Log auth status
	if authConfig.GitHubClientID != "" {
		log.Info("GitHub OAuth configured")
	} else {
		log.Warn("GitHub OAuth not configured - auth disabled")
	}

	// Create components
	hub := server.NewHub()
	wsHandler := server.NewWSHandler(hub, store, log)
	dispatcher := server.NewDispatcher(hub, store, wsHandler, log)
	webhookHandler := server.NewWebhookHandler(store, dispatcher, baseURL, log)
	apiHandler := server.NewAPIHandler(store, hub, log)
	logStreamHandler := server.NewLogStreamHandler(store, log)

	// Wire up dependencies
	wsHandler.SetStatusPoster(webhookHandler)
	wsHandler.SetLogBroadcaster(logStreamHandler)

	// Register forges (for webhook identification)
	webhookHandler.RegisterForge(&forge.GitHub{})
	webhookHandler.RegisterForge(&forge.Forgejo{})
	webhookHandler.RegisterForge(&forge.Forgejo{IsGitea: true})

	// Start dispatcher
	dispatcher.Start()
	defer dispatcher.Stop()

	// Set up HTTP routes
	mux := http.NewServeMux()

	// Auth routes (no caching)
	mux.Handle("/auth/", noCache(authHandler))

	// API routes with auth middleware for mutations
	// Read-only endpoints are public, mutations require auth
	mux.Handle("/api/", noCache(authMiddleware(apiHandler, authHandler)))

	// Webhook endpoint (no caching) - public (has signature verification)
	mux.Handle("/webhooks", noCache(webhookHandler))
	mux.Handle("/webhooks/", noCache(webhookHandler))

	// WebSocket for workers - public (has token auth)
	mux.Handle("/ws/worker", wsHandler)

	// WebSocket for UI log streaming - public for now
	mux.HandleFunc("/ws/logs/", logStreamHandler.ServeHTTP)

	// Serve embedded web assets
	webFS, err := fs.Sub(web.Assets, "dist")
	if err != nil {
		return fmt.Errorf("web assets: %w", err)
	}
	fileServer := http.FileServer(http.FS(webFS))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file directly
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		// Check if file exists
		f, err := webFS.Open(strings.TrimPrefix(path, "/"))
		if err == nil {
			f.Close()
			// Hashed assets (Vite build) get long cache
			// index.html gets no-cache so updates propagate
			if path == "/index.html" || path == "/" {
				w.Header().Set("Cache-Control", "no-cache")
			} else if strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".css") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		// For SPA routing, serve index.html for non-API routes
		if !strings.HasPrefix(r.URL.Path, "/api/") &&
			!strings.HasPrefix(r.URL.Path, "/ws/") &&
			!strings.HasPrefix(r.URL.Path, "/webhooks") &&
			!strings.HasPrefix(r.URL.Path, "/auth/") {
			w.Header().Set("Cache-Control", "no-cache")
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}

		http.NotFound(w, r)
	})

	// Create HTTP server
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Handle graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		log.Info("starting server", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Wait for shutdown signal or error
	select {
	case err := <-errChan:
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		log.Info("shutting down server")
		if err := srv.Shutdown(context.Background()); err != nil {
			log.Warn("shutdown error", "error", err)
		}
	}

	return nil
}

func workerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Start a worker that connects to the server",
		RunE:  runWorker,
	}
	cmd.Flags().String("server", "", "Server WebSocket URL to connect to (e.g., wss://cinch.example.com/ws/worker)")
	cmd.Flags().String("token", "", "Authentication token")
	cmd.Flags().StringSlice("labels", nil, "Worker labels (e.g., linux-amd64,docker)")
	cmd.Flags().Int("concurrency", 1, "Max concurrent jobs")
	cmd.MarkFlagRequired("server")
	cmd.MarkFlagRequired("token")
	return cmd
}

func runWorker(cmd *cobra.Command, args []string) error {
	serverURL, _ := cmd.Flags().GetString("server")
	token, _ := cmd.Flags().GetString("token")
	labels, _ := cmd.Flags().GetStringSlice("labels")
	concurrency, _ := cmd.Flags().GetInt("concurrency")

	log := slog.Default()

	cfg := worker.WorkerConfig{
		ServerURL:   serverURL,
		Token:       token,
		Labels:      labels,
		Concurrency: concurrency,
		Docker:      true, // Assume Docker available
	}

	w := worker.NewWorker(cfg, log)

	// Start worker
	log.Info("starting worker", "server", serverURL, "labels", labels, "concurrency", concurrency)
	if err := w.Start(); err != nil {
		return fmt.Errorf("start worker: %w", err)
	}

	// Wait for interrupt
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
	log.Info("shutting down worker")
	w.Stop()

	return nil
}

// noCache wraps a handler to add no-store cache headers (for API/webhooks behind CDN)
func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}

// authMiddleware requires auth for mutation endpoints (POST, DELETE, PUT, PATCH)
// Read-only endpoints (GET) are public
func authMiddleware(next http.Handler, auth *server.AuthHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GET requests are public (read-only)
		if r.Method == http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}

		// Mutations require auth
		if !auth.IsAuthenticated(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"authentication required"}`))
			return
		}

		next.ServeHTTP(w, r)
	})
}

func runCmd() *cobra.Command {
	var bareMetal bool

	cmd := &cobra.Command{
		Use:   "run [command]",
		Short: "Run a command locally as if CI triggered it",
		Long: `Run a command locally, simulating what CI would do.

If no command is given, reads from .cinch.yaml (or .toml/.json).
By default, runs in a container (auto-detects devcontainer/Dockerfile).
Use --bare-metal to run directly on host.

Examples:
  cinch run                        # uses command from .cinch.yaml
  cinch run "make test"            # explicit command
  cinch run --bare-metal "go test ./..."`,
		Run: func(cmd *cobra.Command, args []string) {
			command := strings.Join(args, " ")
			exitCode := cli.Run(cli.RunOptions{
				Command:   command,
				BareMetal: bareMetal,
			})
			os.Exit(exitCode)
		},
	}
	cmd.Flags().BoolVar(&bareMetal, "bare-metal", false, "Run without container")
	return cmd
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show build status for current repo",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("status not yet implemented")
			return nil
		},
	}
}

func logsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs [job-id]",
		Short: "Stream logs from a job",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("logs not yet implemented")
			return nil
		},
	}
	cmd.Flags().BoolP("follow", "f", false, "Follow log output")
	return cmd
}

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Configuration commands",
	}
	cmd.AddCommand(configValidateCmd())
	return cmd
}

func configValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate config file",
		Run: func(cmd *cobra.Command, args []string) {
			workDir, err := os.Getwd()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			cfg, configFile, err := config.Load(workDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			fmt.Printf("Valid: %s\n", configFile)
			fmt.Printf("  command: %s\n", cfg.Command)
			if cfg.Timeout != 0 {
				fmt.Printf("  timeout: %s\n", cfg.Timeout.Duration())
			}
			if len(cfg.Workers) > 0 {
				fmt.Printf("  workers: %v\n", cfg.Workers)
			}
			if len(cfg.Services) > 0 {
				fmt.Printf("  services: %d configured\n", len(cfg.Services))
				for name, svc := range cfg.Services {
					fmt.Printf("    - %s: %s\n", name, svc.Image)
				}
			}
		},
	}
}

func tokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Manage worker tokens",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "create",
		Short: "Create a new worker token",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("token create not yet implemented")
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List active tokens",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("token list not yet implemented")
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "revoke [token-id]",
		Short: "Revoke a token",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("token revoke not yet implemented")
			return nil
		},
	})
	return cmd
}
