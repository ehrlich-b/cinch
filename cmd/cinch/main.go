package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ehrlich-b/cinch/internal/cli"
	"github.com/ehrlich-b/cinch/internal/config"
	"github.com/ehrlich-b/cinch/internal/daemon"
	"github.com/ehrlich-b/cinch/internal/forge"
	"github.com/ehrlich-b/cinch/internal/server"
	"github.com/ehrlich-b/cinch/internal/storage"
	"github.com/ehrlich-b/cinch/internal/worker"
	"github.com/ehrlich-b/cinch/internal/worker/container"
	"github.com/ehrlich-b/cinch/web"
	"github.com/spf13/cobra"
)

func parseAppID(s string) int64 {
	if s == "" {
		return 0
	}
	id, _ := strconv.ParseInt(s, 10, 64)
	return id
}

var version = "dev"

func main() {
	// Share version with container package for binary download
	container.SetVersion(version)

	rootCmd := &cobra.Command{
		Use:     "cinch",
		Short:   "CI that's a cinch",
		Version: version,
	}

	rootCmd.AddCommand(
		serverCmd(),
		workerCmd(),
		daemonCmd(),
		runCmd(),
		releaseCmd(),
		installCmd(),
		statusCmd(),
		logsCmd(),
		configCmd(),
		tokenCmd(),
		loginCmd(),
		logoutCmd(),
		whoamiCmd(),
		repoCmd(),
		connectCmd(),
		gitlabCmd(), // deprecated, kept for backwards compatibility
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
	// Uses the GitHub App's OAuth credentials (Client ID + Client Secret from App settings)
	authConfig := server.AuthConfig{
		GitHubClientID:     os.Getenv("CINCH_GITHUB_APP_CLIENT_ID"),
		GitHubClientSecret: os.Getenv("CINCH_GITHUB_APP_CLIENT_SECRET"),
		JWTSecret:          os.Getenv("CINCH_JWT_SECRET"),
		BaseURL:            baseURL,
	}
	authHandler := server.NewAuthHandler(authConfig, store, log)

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
	apiHandler := server.NewAPIHandler(store, hub, authHandler, log)
	logStreamHandler := server.NewLogStreamHandler(store, log)
	badgeHandler := server.NewBadgeHandler(store, log, baseURL)

	// Create GitHub App handler
	githubAppConfig := server.GitHubAppConfig{
		AppID:         parseAppID(os.Getenv("CINCH_GITHUB_APP_ID")),
		PrivateKey:    os.Getenv("CINCH_GITHUB_APP_PRIVATE_KEY"),
		WebhookSecret: os.Getenv("CINCH_GITHUB_APP_WEBHOOK_SECRET"),
	}
	githubAppHandler, err := server.NewGitHubAppHandler(githubAppConfig, store, dispatcher, baseURL, log)
	if err != nil {
		return fmt.Errorf("create github app handler: %w", err)
	}
	if githubAppHandler.IsConfigured() {
		log.Info("GitHub App configured", "app_id", githubAppConfig.AppID)
	}

	// Create GitLab OAuth handler
	gitlabOAuthConfig := server.GitLabOAuthConfig{
		ClientID:     os.Getenv("CINCH_GITLAB_CLIENT_ID"),
		ClientSecret: os.Getenv("CINCH_GITLAB_CLIENT_SECRET"),
		BaseURL:      os.Getenv("CINCH_GITLAB_URL"), // defaults to https://gitlab.com
	}
	jwtSecret := []byte(os.Getenv("CINCH_JWT_SECRET"))
	gitlabOAuthHandler := server.NewGitLabOAuthHandler(gitlabOAuthConfig, baseURL, jwtSecret, store, log)
	if gitlabOAuthHandler.IsConfigured() {
		log.Info("GitLab OAuth configured")
	}

	// Create Forgejo OAuth handler (Codeberg)
	forgejoOAuthConfig := server.ForgejoOAuthConfig{
		ClientID:     os.Getenv("CINCH_FORGEJO_CLIENT_ID"),
		ClientSecret: os.Getenv("CINCH_FORGEJO_CLIENT_SECRET"),
		BaseURL:      os.Getenv("CINCH_FORGEJO_URL"), // defaults to https://codeberg.org
	}
	forgejoOAuthHandler := server.NewForgejoOAuthHandler(forgejoOAuthConfig, baseURL, jwtSecret, store, log)
	if forgejoOAuthHandler.IsConfigured() {
		log.Info("Forgejo OAuth configured", "url", forgejoOAuthConfig.BaseURL)
	}

	// Wire up dependencies
	wsHandler.SetStatusPoster(webhookHandler)
	wsHandler.SetLogBroadcaster(logStreamHandler)
	wsHandler.SetJWTValidator(authHandler)
	wsHandler.SetGitHubApp(githubAppHandler)
	wsHandler.SetWorkerNotifier(dispatcher)
	webhookHandler.SetGitHubApp(githubAppHandler)
	dispatcher.SetGitHubApp(githubAppHandler)

	// Register forges (for webhook identification)
	webhookHandler.RegisterForge(&forge.GitHub{})
	webhookHandler.RegisterForge(&forge.GitLab{})
	webhookHandler.RegisterForge(&forge.Forgejo{})
	webhookHandler.RegisterForge(&forge.Forgejo{IsGitea: true})

	// Start dispatcher
	dispatcher.Start()
	defer dispatcher.Stop()

	// Set up HTTP routes
	mux := http.NewServeMux()

	// Auth routes (no caching)
	mux.Handle("/auth/", noCache(authHandler))

	// GitLab OAuth routes (separate from main auth handler)
	mux.HandleFunc("/auth/gitlab", func(w http.ResponseWriter, r *http.Request) {
		gitlabOAuthHandler.HandleLogin(w, r)
	})
	mux.HandleFunc("/auth/gitlab/callback", func(w http.ResponseWriter, r *http.Request) {
		// Callback now handles both onboarding (new users) and connecting (existing users)
		gitlabOAuthHandler.HandleCallback(w, r, authHandler)
	})

	// GitLab API routes
	mux.HandleFunc("/api/gitlab/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"configured": gitlabOAuthHandler.IsConfigured(),
			"base_url":   gitlabOAuthConfig.BaseURL,
		})
	})
	mux.HandleFunc("/api/gitlab/projects", func(w http.ResponseWriter, r *http.Request) {
		user := authHandler.GetUser(r)
		if user == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"authentication required"}`))
			return
		}
		gitlabOAuthHandler.HandleProjects(w, r, user)
	})
	mux.HandleFunc("/api/gitlab/setup", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		user := authHandler.GetUser(r)
		if user == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"authentication required"}`))
			return
		}
		gitlabOAuthHandler.HandleSetup(w, r, user)
	})

	// Forgejo OAuth routes
	mux.HandleFunc("/auth/forgejo", func(w http.ResponseWriter, r *http.Request) {
		forgejoOAuthHandler.HandleLogin(w, r)
	})
	mux.HandleFunc("/auth/forgejo/callback", func(w http.ResponseWriter, r *http.Request) {
		// Callback now handles both onboarding (new users) and connecting (existing users)
		forgejoOAuthHandler.HandleCallback(w, r, authHandler)
	})

	// Forgejo API routes
	mux.HandleFunc("/api/forgejo/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"configured": forgejoOAuthHandler.IsConfigured(),
			"base_url":   forgejoOAuthConfig.BaseURL,
		})
	})
	mux.HandleFunc("/api/forgejo/repos", func(w http.ResponseWriter, r *http.Request) {
		user := authHandler.GetUser(r)
		if user == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"authentication required"}`))
			return
		}
		forgejoOAuthHandler.HandleProjects(w, r, user)
	})
	mux.HandleFunc("/api/forgejo/setup", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		user := authHandler.GetUser(r)
		if user == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"authentication required"}`))
			return
		}
		forgejoOAuthHandler.HandleSetup(w, r, user)
	})

	// API routes with auth middleware for mutations
	// Read-only endpoints are public, mutations require auth
	mux.Handle("/api/", noCache(authMiddleware(apiHandler, authHandler)))

	// Webhook endpoints (no caching) - public (has signature verification)
	mux.Handle("/webhooks/github-app", noCache(githubAppHandler))
	mux.Handle("/webhooks", noCache(webhookHandler))
	mux.Handle("/webhooks/", noCache(webhookHandler))

	// WebSocket for workers - public (has token auth)
	mux.Handle("/ws/worker", wsHandler)

	// WebSocket for UI log streaming - public for now
	mux.HandleFunc("/ws/logs/", logStreamHandler.ServeHTTP)

	// Install script for curl | sh
	mux.HandleFunc("/install.sh", server.InstallScriptHandler)

	// Badge endpoints - public, CDN-friendly
	mux.Handle("/badge/", badgeHandler)
	mux.Handle("/api/badge/", badgeHandler)

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
		Short: "View jobs from the daemon",
		Long: `Connect to the daemon and stream job events.

The daemon must be running (via 'cinch daemon start') or use -s for standalone mode.

Examples:
  cinch worker                 # connect to daemon, watch jobs
  cinch worker -v              # include full build logs
  cinch worker --job j_abc123  # follow specific job
  cinch worker -s              # standalone: temp daemon + viewer`,
		RunE: runWorker,
	}
	cmd.Flags().BoolP("verbose", "v", false, "Show full job logs")
	cmd.Flags().BoolP("standalone", "s", false, "Standalone mode: spawn temp daemon")
	cmd.Flags().String("job", "", "Follow specific job ID")
	cmd.Flags().String("socket", "", "Daemon socket path")
	cmd.Flags().StringSlice("labels", nil, "Worker labels (standalone mode only)")
	return cmd
}

func runWorker(cmd *cobra.Command, args []string) error {
	verbose, _ := cmd.Flags().GetBool("verbose")
	standalone, _ := cmd.Flags().GetBool("standalone")
	jobID, _ := cmd.Flags().GetString("job")
	socketPath, _ := cmd.Flags().GetString("socket")

	if socketPath == "" {
		socketPath = cli.DefaultDaemonConfig().SocketPath
	}

	// Standalone mode: spawn a temp daemon with concurrency=1
	if standalone {
		labels, _ := cmd.Flags().GetStringSlice("labels")
		return runStandaloneWorker(verbose, labels)
	}

	// Check if daemon is running
	if daemon.IsDaemonRunning(socketPath) {
		return runDaemonClient(socketPath, jobID, verbose)
	}

	// No daemon running - tell user how to start one
	return fmt.Errorf("no daemon running\n\nStart a daemon:  cinch daemon start\nOr standalone:   cinch worker -s")
}

// runDirectWorker starts a worker that connects directly to the server.
// runDaemonClient connects to a running daemon and streams events.
func runDaemonClient(socketPath, jobID string, verbose bool) error {
	term := worker.NewTerminal(os.Stdout)

	client, err := daemon.Connect(socketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer client.Close()

	// Get initial status
	status, err := client.Status()
	if err != nil {
		return fmt.Errorf("get daemon status: %w", err)
	}

	fmt.Printf("Connected to daemon (%d/%d slots)\n", status.SlotsBusy, status.SlotsTotal)

	// Start streaming events
	if err := client.StartStream(jobID, verbose); err != nil {
		return fmt.Errorf("start stream: %w", err)
	}

	// Handle interrupt
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Read events in a goroutine
	eventDone := make(chan error, 1)
	go func() {
		for {
			msgType, payload, err := client.ReadEvent()
			if err != nil {
				eventDone <- err
				return
			}

			switch msgType {
			case daemon.TypeJobStarted:
				var event daemon.JobStarted
				if err := json.Unmarshal(payload, &event); err != nil {
					continue
				}
				term.PrintJobStart(event.Repo, event.Branch, event.Tag, event.Commit, event.Command, event.Mode, event.Forge)

			case daemon.TypeLogChunk:
				var event daemon.LogChunk
				if err := json.Unmarshal(payload, &event); err != nil {
					continue
				}
				// Write log output directly
				if event.Stream == "stderr" {
					fmt.Fprint(os.Stderr, event.Data)
				} else {
					fmt.Print(event.Data)
				}

			case daemon.TypeJobCompleted:
				var event daemon.JobCompleted
				if err := json.Unmarshal(payload, &event); err != nil {
					continue
				}
				duration := time.Duration(event.DurationMs) * time.Millisecond
				term.PrintJobComplete(event.ExitCode, duration)
			}
		}
	}()

	// Wait for interrupt or event error
	select {
	case <-ctx.Done():
		_ = client.StopStream()
		term.PrintShutdown()
	case err := <-eventDone:
		if err != nil {
			return fmt.Errorf("event stream error: %w", err)
		}
	}

	return nil
}

// runStandaloneWorker spawns a temporary daemon and attaches to it.
func runStandaloneWorker(verbose bool, labels []string) error {
	term := worker.NewTerminal(os.Stdout)

	// Create temp socket path
	socketPath := fmt.Sprintf("/tmp/cinch-%d.sock", os.Getpid())
	defer os.Remove(socketPath)
	defer os.Remove(socketPath + ".pid")

	// Load credentials to validate they exist (daemon run will use them)
	cliCfg, err := cli.LoadConfig()
	if err != nil {
		return fmt.Errorf("load credentials: %w", err)
	}

	serverCfg, ok := cliCfg.Servers["default"]
	if !ok || serverCfg.Token == "" {
		return fmt.Errorf("not logged in - run 'cinch login' first")
	}

	// Get the path to the current executable
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	// Build command arguments for temp daemon
	args := []string{"daemon", "run",
		"-n", "1", // concurrency=1 so only one job to follow
		"--socket", socketPath,
	}
	if len(labels) > 0 {
		args = append(args, "--labels", strings.Join(labels, ","))
	}

	// Start the temp daemon process
	daemonCmd := exec.Command(executable, args...)
	daemonCmd.Stdout = nil
	daemonCmd.Stderr = nil

	if err := daemonCmd.Start(); err != nil {
		return fmt.Errorf("start temp daemon: %w", err)
	}

	// Cleanup on exit
	defer func() {
		_ = daemonCmd.Process.Signal(syscall.SIGTERM)
		_ = daemonCmd.Wait()
	}()

	// Wait for daemon to be ready
	for i := 0; i < 30; i++ {
		if daemon.IsDaemonRunning(socketPath) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !daemon.IsDaemonRunning(socketPath) {
		return fmt.Errorf("temp daemon failed to start")
	}

	fmt.Printf("Standalone worker started (connected to %s)\n", serverCfg.URL)
	fmt.Println("Press Ctrl-C to stop")

	// Connect to temp daemon
	client, err := daemon.Connect(socketPath)
	if err != nil {
		return fmt.Errorf("connect to temp daemon: %w", err)
	}
	defer client.Close()

	// Start streaming (no job ID since concurrency=1)
	if err := client.StartStream("", verbose); err != nil {
		return fmt.Errorf("start stream: %w", err)
	}

	// Handle interrupt
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Read events in a goroutine
	eventDone := make(chan error, 1)
	go func() {
		for {
			msgType, payload, err := client.ReadEvent()
			if err != nil {
				eventDone <- err
				return
			}

			switch msgType {
			case daemon.TypeJobStarted:
				var event daemon.JobStarted
				if err := json.Unmarshal(payload, &event); err != nil {
					continue
				}
				term.PrintJobStart(event.Repo, event.Branch, event.Tag, event.Commit, event.Command, event.Mode, event.Forge)

			case daemon.TypeLogChunk:
				var event daemon.LogChunk
				if err := json.Unmarshal(payload, &event); err != nil {
					continue
				}
				if event.Stream == "stderr" {
					fmt.Fprint(os.Stderr, event.Data)
				} else {
					fmt.Print(event.Data)
				}

			case daemon.TypeJobCompleted:
				var event daemon.JobCompleted
				if err := json.Unmarshal(payload, &event); err != nil {
					continue
				}
				duration := time.Duration(event.DurationMs) * time.Millisecond
				term.PrintJobComplete(event.ExitCode, duration)
			}
		}
	}()

	// Wait for interrupt or event error
	select {
	case <-ctx.Done():
		_ = client.StopStream()
		term.PrintShutdown()
	case err := <-eventDone:
		// Connection closed (daemon stopped or error)
		if err != nil && ctx.Err() == nil {
			return fmt.Errorf("event stream error: %w", err)
		}
	}

	return nil
}

func daemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the worker daemon",
		Long: `The daemon runs workers in the background with configurable concurrency.

The daemon can run multiple jobs simultaneously and streams events to
connected 'cinch worker' clients.

Commands:
  start     Start the daemon in the background
  stop      Stop the running daemon
  status    Show daemon status and running jobs
  install   Install as a system service (launchd/systemd)
  uninstall Remove system service
  logs      View daemon logs
  run       Run daemon in foreground (internal use)`,
	}

	cmd.AddCommand(daemonStartCmd())
	cmd.AddCommand(daemonStopCmd())
	cmd.AddCommand(daemonStatusCmd())
	cmd.AddCommand(daemonInstallCmd())
	cmd.AddCommand(daemonUninstallCmd())
	cmd.AddCommand(daemonLogsCmd())
	cmd.AddCommand(daemonRunCmd())

	return cmd
}

func daemonStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the daemon in the background",
		RunE: func(cmd *cobra.Command, args []string) error {
			concurrency, _ := cmd.Flags().GetInt("concurrency")
			socketPath, _ := cmd.Flags().GetString("socket")
			verbose, _ := cmd.Flags().GetBool("verbose")
			labels, _ := cmd.Flags().GetStringSlice("labels")

			// Load credentials
			cliCfg, err := cli.LoadConfig()
			if err != nil {
				return fmt.Errorf("load credentials: %w", err)
			}

			serverCfg, ok := cliCfg.Servers["default"]
			if !ok || serverCfg.Token == "" {
				return fmt.Errorf("not logged in - run 'cinch login' first")
			}

			// Convert HTTP URL to WebSocket URL
			wsURL := serverCfg.URL
			wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
			wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
			serverURL := wsURL + "/ws/worker"

			cfg := cli.DefaultDaemonConfig()
			cfg.Concurrency = concurrency
			if socketPath != "" {
				cfg.SocketPath = socketPath
			}
			cfg.Verbose = verbose

			return cli.StartDaemon(cfg, serverURL, serverCfg.Token, labels)
		},
	}

	cfg := cli.DefaultDaemonConfig()
	cmd.Flags().IntP("concurrency", "n", 1, "Number of concurrent jobs")
	cmd.Flags().String("socket", cfg.SocketPath, "Unix socket path")
	cmd.Flags().BoolP("verbose", "v", false, "Verbose logging")
	cmd.Flags().StringSlice("labels", nil, "Worker labels (e.g., linux-amd64,docker)")

	return cmd
}

func daemonStopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the running daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			socketPath, _ := cmd.Flags().GetString("socket")
			if socketPath == "" {
				socketPath = cli.DefaultDaemonConfig().SocketPath
			}
			return cli.StopDaemon(socketPath)
		},
	}

	cfg := cli.DefaultDaemonConfig()
	cmd.Flags().String("socket", cfg.SocketPath, "Unix socket path")

	return cmd
}

func daemonStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show daemon status and running jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			socketPath, _ := cmd.Flags().GetString("socket")
			if socketPath == "" {
				socketPath = cli.DefaultDaemonConfig().SocketPath
			}
			return cli.DaemonStatus(socketPath)
		},
	}

	cfg := cli.DefaultDaemonConfig()
	cmd.Flags().String("socket", cfg.SocketPath, "Unix socket path")

	return cmd
}

func daemonInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install daemon as a system service",
		Long: `Install the daemon as a system service that starts automatically.

On macOS: Installs a launchd plist in ~/Library/LaunchAgents
On Linux: Installs a systemd user service in ~/.config/systemd/user`,
		RunE: func(cmd *cobra.Command, args []string) error {
			concurrency, _ := cmd.Flags().GetInt("concurrency")
			return cli.InstallDaemonService(concurrency)
		},
	}

	cmd.Flags().IntP("concurrency", "n", 1, "Number of concurrent jobs")

	return cmd
}

func daemonUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove daemon system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.UninstallDaemonService()
		},
	}
}

func daemonLogsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "View daemon logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			logFile, _ := cmd.Flags().GetString("log-file")
			follow, _ := cmd.Flags().GetBool("follow")
			if logFile == "" {
				logFile = cli.DefaultDaemonConfig().LogFile
			}
			return cli.DaemonLogs(logFile, follow)
		},
	}

	cfg := cli.DefaultDaemonConfig()
	cmd.Flags().String("log-file", cfg.LogFile, "Log file path")
	cmd.Flags().BoolP("follow", "f", false, "Follow log output")

	return cmd
}

func daemonRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "run",
		Short:  "Run daemon in foreground (internal use)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			concurrency, _ := cmd.Flags().GetInt("concurrency")
			socketPath, _ := cmd.Flags().GetString("socket")
			verbose, _ := cmd.Flags().GetBool("verbose")
			labels, _ := cmd.Flags().GetStringSlice("labels")

			// Load credentials
			cliCfg, err := cli.LoadConfig()
			if err != nil {
				return fmt.Errorf("load credentials: %w", err)
			}

			serverCfg, ok := cliCfg.Servers["default"]
			if !ok || serverCfg.Token == "" {
				return fmt.Errorf("not logged in - run 'cinch login' first")
			}

			// Convert HTTP URL to WebSocket URL
			wsURL := serverCfg.URL
			wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
			wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
			serverURL := wsURL + "/ws/worker"

			cfg := cli.DefaultDaemonConfig()
			cfg.Concurrency = concurrency
			if socketPath != "" {
				cfg.SocketPath = socketPath
			}
			cfg.Verbose = verbose

			return cli.RunDaemon(cfg, serverURL, serverCfg.Token, labels)
		},
	}

	cfg := cli.DefaultDaemonConfig()
	cmd.Flags().IntP("concurrency", "n", 1, "Number of concurrent jobs")
	cmd.Flags().String("socket", cfg.SocketPath, "Unix socket path")
	cmd.Flags().StringSlice("labels", nil, "Worker labels")
	cmd.Flags().BoolP("verbose", "v", false, "Verbose logging")

	return cmd
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
			_, _ = w.Write([]byte(`{"error":"authentication required"}`))
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
			fmt.Printf("  build: %s\n", cfg.Build)
			if cfg.Release != "" {
				fmt.Printf("  release: %s\n", cfg.Release)
			}
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

func loginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with a Cinch server",
		Long: `Authenticate with a Cinch server using device authorization.

This opens your browser to complete authentication. Once authorized,
credentials are saved to ~/.cinch/config.

Example:
  cinch login --server https://cinch.sh`,
		RunE: runLogin,
	}
	cmd.Flags().String("server", "https://cinch.sh", "Server URL to authenticate with")
	return cmd
}

func runLogin(cmd *cobra.Command, args []string) error {
	serverURL, _ := cmd.Flags().GetString("server")

	// Normalize URL (remove trailing slash)
	serverURL = strings.TrimSuffix(serverURL, "/")

	// Check for existing valid session
	if existingCfg, loadErr := cli.LoadConfig(); loadErr == nil {
		if serverCfg, ok := existingCfg.Servers["default"]; ok && serverCfg.Token != "" {
			// Verify token is still valid
			req, _ := http.NewRequest("GET", serverURL+"/api/whoami", nil)
			req.Header.Set("Authorization", "Bearer "+serverCfg.Token)
			resp, doErr := http.DefaultClient.Do(req)
			if doErr == nil && resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				fmt.Printf("Already logged in as %s\n", serverCfg.Email)
				return nil
			}
			if resp != nil {
				resp.Body.Close()
			}
		}
	}

	fmt.Printf("Logging in to %s...\n", serverURL)

	// Request device code
	deviceResp, err := cli.RequestDeviceCode(serverURL)
	if err != nil {
		return fmt.Errorf("failed to start login: %w", err)
	}

	// Show user code and open browser
	verifyURL := deviceResp.VerificationURI + "?code=" + deviceResp.UserCode
	fmt.Printf("\nOpen: %s\n", verifyURL)
	fmt.Println("\nWaiting for authorization...")

	// Try to open browser
	openBrowser(verifyURL)

	// Poll for token
	tokenResp, err := cli.PollForToken(serverURL, deviceResp.DeviceCode, deviceResp.Interval)
	if err != nil {
		return fmt.Errorf("authorization failed: %w", err)
	}

	// Save credentials
	cfg, err := cli.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	cfg.SetServerConfig("default", cli.ServerConfig{
		URL:   serverURL,
		Token: tokenResp.AccessToken,
		Email: tokenResp.Email,
	})

	if err := cli.SaveConfig(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("\nLogged in as %s\n", tokenResp.Email)
	fmt.Printf("Credentials saved to %s\n", cli.DefaultConfigPath())

	return nil
}

func logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := cli.LoadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			if len(cfg.Servers) == 0 {
				fmt.Println("Not logged in")
				return nil
			}

			// Clear all servers
			cfg.Servers = make(map[string]cli.ServerConfig)
			if err := cli.SaveConfig(cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}

			fmt.Println("Logged out")
			return nil
		},
	}
}

func whoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show current authentication status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := cli.LoadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			if len(cfg.Servers) == 0 {
				fmt.Println("Not logged in")
				return nil
			}

			for name, sc := range cfg.Servers {
				fmt.Printf("Server: %s (%s)\n", sc.URL, name)
				fmt.Printf("Email: %s\n", sc.Email)
			}

			return nil
		},
	}
}

func repoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Manage repositories",
	}
	cmd.AddCommand(repoAddCmd())
	cmd.AddCommand(repoListCmd())
	return cmd
}

func repoAddCmd() *cobra.Command {
	var forgeType string
	var forgeURL string
	var forgeToken string

	cmd := &cobra.Command{
		Use:   "add <owner/name>",
		Short: "Add a repository to Cinch",
		Long: `Add a repository to Cinch for CI.

Examples:
  cinch repo add ehrlich-b/cinch
  cinch repo add myorg/myproject --forge gitlab
  cinch repo add myorg/myproject --forge gitlab --url https://gitlab.mycompany.com --token glpat-xxx

After adding, configure the webhook in your forge:
  - URL: shown in output
  - Secret: shown in output (or token for GitLab)
  - Events: push`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRepoAdd(args[0], forgeType, forgeURL, forgeToken)
		},
	}
	cmd.Flags().StringVar(&forgeType, "forge", "github", "Forge type (github, gitlab, forgejo, gitea)")
	cmd.Flags().StringVar(&forgeURL, "url", "", "Base URL for self-hosted instances (e.g., https://gitlab.mycompany.com)")
	cmd.Flags().StringVar(&forgeToken, "token", "", "API token for status posting (e.g., glpat-xxx for GitLab)")
	return cmd
}

func runRepoAdd(repoPath string, forgeType string, forgeURL string, forgeToken string) error {
	parts := strings.SplitN(repoPath, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo format: use owner/name")
	}
	owner, name := parts[0], parts[1]

	// Load credentials
	cfg, err := cli.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	serverCfg, ok := cfg.Servers["default"]
	if !ok || serverCfg.Token == "" {
		return fmt.Errorf("not logged in - run 'cinch login' first")
	}

	// For GitLab, try to use stored OAuth credentials for automatic webhook setup
	if forgeType == "gitlab" && forgeToken == "" {
		return runGitLabRepoAdd(serverCfg, repoPath, owner, name, forgeURL)
	}

	// For other forges or when token is provided, use manual setup
	return runManualRepoAdd(serverCfg, repoPath, forgeType, forgeURL, forgeToken, owner, name)
}

func runGitLabRepoAdd(serverCfg cli.ServerConfig, repoPath, owner, name, forgeURL string) error {
	// First, get list of projects to find the project ID
	req, err := http.NewRequest("GET", serverCfg.URL+"/api/gitlab/projects", nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+serverCfg.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(body), "GitLab not connected") {
			fmt.Println("GitLab not connected. Run 'cinch gitlab connect' first.")
			return fmt.Errorf("GitLab not connected")
		}
		return fmt.Errorf("authentication required")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to list projects: %s", string(body))
	}

	var projects []struct {
		ID                int    `json:"id"`
		PathWithNamespace string `json:"path_with_namespace"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
		return fmt.Errorf("decode projects: %w", err)
	}

	// Find matching project
	var projectID int
	for _, p := range projects {
		if strings.EqualFold(p.PathWithNamespace, repoPath) {
			projectID = p.ID
			break
		}
	}

	if projectID == 0 {
		return fmt.Errorf("project %s not found in your GitLab account", repoPath)
	}

	// Call setup endpoint
	setupData := map[string]any{
		"project_id":   projectID,
		"project_path": repoPath,
		"use_oauth":    true, // Use OAuth token by default
	}
	setupBody, _ := json.Marshal(setupData)

	req, err = http.NewRequest("POST", serverCfg.URL+"/api/gitlab/setup",
		bytes.NewReader(setupBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+serverCfg.Token)

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	// Check if we need manual token (free tier)
	if resp.StatusCode == http.StatusAccepted {
		// Webhook created but need manual token
		fmt.Printf("Added repo %s - webhook created!\n", repoPath)
		fmt.Println()
		fmt.Println("For build status updates, choose an option:")
		if options, ok := result["options"].([]any); ok {
			for i, opt := range options {
				if optMap, ok := opt.(map[string]any); ok {
					fmt.Printf("  %d. %s\n", i+1, optMap["label"])
					if desc, ok := optMap["description"].(string); ok {
						fmt.Printf("     %s\n", desc)
					}
					if url, ok := optMap["token_url"].(string); ok {
						fmt.Printf("     Create token at: %s\n", url)
					}
				}
			}
		}
		fmt.Println()
		fmt.Println("Use the web UI to complete setup, or run:")
		fmt.Printf("  cinch repo add %s --forge gitlab --token <your-token>\n", repoPath)
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		if errMsg, ok := result["error"].(string); ok {
			return fmt.Errorf("setup failed: %s", errMsg)
		}
		return fmt.Errorf("setup failed with status %d", resp.StatusCode)
	}

	fmt.Printf("Added repo %s\n", repoPath)
	fmt.Println("Webhook created automatically!")
	if tokenType, ok := result["token_type"].(string); ok {
		switch tokenType {
		case "pat":
			fmt.Println("Bot token created for status updates (bot identity).")
		case "oauth":
			fmt.Println("Using your GitLab session for status updates (your identity).")
		}
	}

	return nil
}

func runManualRepoAdd(serverCfg cli.ServerConfig, repoPath, forgeType, forgeURL, forgeToken, owner, name string) error {
	// Build clone URL based on forge
	var cloneURL string
	var baseURL string

	switch forgeType {
	case "github":
		cloneURL = fmt.Sprintf("https://github.com/%s/%s.git", owner, name)
		baseURL = "https://github.com"
	case "gitlab":
		if forgeURL != "" {
			baseURL = strings.TrimSuffix(forgeURL, "/")
			cloneURL = fmt.Sprintf("%s/%s/%s.git", baseURL, owner, name)
		} else {
			baseURL = "https://gitlab.com"
			cloneURL = fmt.Sprintf("https://gitlab.com/%s/%s.git", owner, name)
		}
	case "forgejo", "gitea":
		if forgeURL == "" {
			return fmt.Errorf("%s requires --url flag for self-hosted instance", forgeType)
		}
		baseURL = strings.TrimSuffix(forgeURL, "/")
		cloneURL = fmt.Sprintf("%s/%s/%s.git", baseURL, owner, name)
	default:
		return fmt.Errorf("unknown forge type: %s", forgeType)
	}

	// Build request body
	reqData := map[string]string{
		"forge_type": forgeType,
		"owner":      owner,
		"name":       name,
		"clone_url":  cloneURL,
	}
	if forgeToken != "" {
		reqData["forge_token"] = forgeToken
	}
	reqBody, _ := json.Marshal(reqData)

	req, err := http.NewRequest("POST", serverCfg.URL+"/api/repos",
		bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+serverCfg.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		ID            string `json:"id"`
		WebhookSecret string `json:"webhook_secret"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	webhookURL := serverCfg.URL + "/webhooks"

	fmt.Printf("Added repo %s/%s\n", owner, name)
	fmt.Println()

	// Show forge-specific webhook configuration
	switch forgeType {
	case "gitlab":
		fmt.Println("Configure webhook in GitLab:")
		fmt.Printf("  URL: %s\n", webhookURL)
		fmt.Printf("  Secret token: %s\n", result.WebhookSecret)
		fmt.Println("  Trigger: Push events, Tag push events")
		if forgeToken == "" {
			fmt.Println()
			fmt.Println("Note: For status updates, create a Project Access Token with 'api' scope:")
			fmt.Printf("  %s/%s/%s/-/settings/access_tokens\n", baseURL, owner, name)
			fmt.Println("  Then run: cinch repo add --forge gitlab --token <token> ...")
		}
	case "github":
		fmt.Println("Configure webhook in GitHub:")
		fmt.Printf("  URL: %s\n", webhookURL)
		fmt.Printf("  Secret: %s\n", result.WebhookSecret)
		fmt.Println("  Content type: application/json")
		fmt.Println("  Events: push")
	default:
		fmt.Printf("Configure webhook in %s:\n", forgeType)
		fmt.Printf("  URL: %s\n", webhookURL)
		fmt.Printf("  Secret: %s\n", result.WebhookSecret)
		fmt.Println("  Content type: application/json")
		fmt.Println("  Events: push")
	}

	return nil
}

func repoListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List repositories",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := cli.LoadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			serverCfg, ok := cfg.Servers["default"]
			if !ok || serverCfg.Token == "" {
				return fmt.Errorf("not logged in - run 'cinch login' first")
			}

			req, err := http.NewRequest("GET", serverCfg.URL+"/api/repos", nil)
			if err != nil {
				return fmt.Errorf("create request: %w", err)
			}
			req.Header.Set("Authorization", "Bearer "+serverCfg.Token)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("request failed: %w", err)
			}
			defer resp.Body.Close()

			var repos []struct {
				ID    string `json:"id"`
				Owner string `json:"owner"`
				Name  string `json:"name"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}

			if len(repos) == 0 {
				fmt.Println("No repositories configured")
				return nil
			}

			for _, r := range repos {
				fmt.Printf("%s/%s\n", r.Owner, r.Name)
			}
			return nil
		},
	}
}

func releaseCmd() *cobra.Command {
	var opts cli.ReleaseOptions

	cmd := &cobra.Command{
		Use:   "release [files...]",
		Short: "Create a release on the forge and upload assets",
		Long: `Create a release on the detected forge (GitHub, GitLab, Gitea, etc.)
and upload the specified files as release assets.

When running inside a Cinch job, forge, tag, repository, and token are
auto-detected from environment variables. Outside of CI, use flags.`,
		Example: `  cinch release dist/*
  cinch release --tag v1.0.0 dist/myapp-linux-amd64
  cinch release --forge github --repo owner/repo dist/*`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Files = args
			return cli.Release(opts)
		},
	}

	cmd.Flags().StringVar(&opts.Forge, "forge", "", "Override forge detection (github, gitlab, gitea)")
	cmd.Flags().StringVar(&opts.Tag, "tag", "", "Override tag (default: CINCH_TAG)")
	cmd.Flags().StringVar(&opts.Repo, "repo", "", "Override repository (owner/repo)")
	cmd.Flags().StringVar(&opts.Token, "token", "", "Override token (default: CINCH_FORGE_TOKEN)")
	cmd.Flags().BoolVar(&opts.Draft, "draft", false, "Create as draft release")
	cmd.Flags().BoolVar(&opts.Prerelease, "prerelease", false, "Mark as prerelease")

	return cmd
}

func installCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install or update cinch",
		Long: `Download and run the cinch install script.

This fetches the latest version from GitHub releases and installs
all platform binaries to ~/.cinch/bin/.

After installation, optionally sets up the daemon as a system service.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			daemonOnly, _ := cmd.Flags().GetBool("daemon")

			if !daemonOnly {
				fmt.Println("Downloading install script...")

				resp, err := http.Get("https://cinch.sh/install.sh")
				if err != nil {
					return fmt.Errorf("fetch install script: %w", err)
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					return fmt.Errorf("failed to fetch install script: %s", resp.Status)
				}

				script, err := io.ReadAll(resp.Body)
				if err != nil {
					return fmt.Errorf("read install script: %w", err)
				}

				// Run the install script
				shCmd := exec.Command("sh")
				shCmd.Stdin = strings.NewReader(string(script))
				shCmd.Stdout = os.Stdout
				shCmd.Stderr = os.Stderr

				if err := shCmd.Run(); err != nil {
					return err
				}
			}

			// Offer to set up daemon service
			setupDaemon, _ := cmd.Flags().GetBool("with-daemon")
			concurrency, _ := cmd.Flags().GetInt("concurrency")

			if setupDaemon || daemonOnly {
				fmt.Println()
				fmt.Println("Setting up daemon service...")

				if err := cli.InstallDaemonService(concurrency); err != nil {
					return fmt.Errorf("install daemon service: %w", err)
				}

				// Check if logged in
				cliCfg, err := cli.LoadConfig()
				if err == nil {
					if _, ok := cliCfg.Servers["default"]; ok {
						fmt.Println()
						fmt.Println("Starting daemon...")
						cfg := cli.DefaultDaemonConfig()
						cfg.Concurrency = concurrency
						if err := cli.StartDaemon(cfg, "", "", nil); err != nil {
							fmt.Printf("Note: Could not start daemon: %v\n", err)
							fmt.Println("Start manually with: cinch daemon start")
						}
					} else {
						fmt.Println()
						fmt.Println("Run 'cinch login' then 'cinch daemon start' to start the worker.")
					}
				}
			} else if !daemonOnly {
				fmt.Println()
				fmt.Println("Tip: Run 'cinch install --with-daemon' to also set up background worker service.")
			}

			return nil
		},
	}

	cmd.Flags().Bool("with-daemon", false, "Also install daemon as system service")
	cmd.Flags().Bool("daemon", false, "Only set up daemon service (skip binary install)")
	cmd.Flags().IntP("concurrency", "n", 1, "Daemon concurrency (number of parallel jobs)")

	return cmd
}

func connectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connect <forge>",
		Short: "Connect a forge account to Cinch",
		Long: `Connect a forge account (GitLab, Codeberg, Forgejo, etc.) to Cinch.

For hosted services (gitlab.com, codeberg.org), this opens your browser
to authenticate via OAuth. For self-hosted instances, you'll be prompted
for a Personal Access Token (PAT).

Supported forges:
  gitlab    - GitLab.com (or self-hosted GitLab)
  codeberg  - Codeberg.org (Forgejo)
  forgejo   - Self-hosted Forgejo/Gitea

Example:
  cinch connect gitlab                              # gitlab.com via OAuth
  cinch connect gitlab --host gitlab.mycompany.com  # self-hosted via PAT
  cinch connect codeberg                            # codeberg.org via OAuth
  cinch connect forgejo --host git.mycompany.com    # self-hosted via PAT`,
		Args: cobra.ExactArgs(1),
		RunE: runConnect,
	}
	cmd.Flags().String("host", "", "Self-hosted instance URL (e.g., gitlab.mycompany.com)")
	return cmd
}

func runConnect(cmd *cobra.Command, args []string) error {
	forge := strings.ToLower(args[0])
	host, _ := cmd.Flags().GetString("host")

	// Map forge names to defaults
	var apiPath, authPath, forgeName, defaultHost string
	switch forge {
	case "gitlab":
		apiPath = "/api/gitlab/status"
		authPath = "/auth/gitlab"
		forgeName = "GitLab"
		defaultHost = "gitlab.com"
	case "codeberg":
		apiPath = "/api/forgejo/status"
		authPath = "/auth/forgejo"
		forgeName = "Codeberg"
		defaultHost = "codeberg.org"
	case "forgejo", "gitea":
		forgeName = "Forgejo"
		defaultHost = "" // No default for generic forgejo/gitea
	default:
		return fmt.Errorf("unknown forge: %s (supported: gitlab, codeberg, forgejo)", forge)
	}

	// Load credentials to get server URL
	cfg, err := cli.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	serverCfg, ok := cfg.Servers["default"]
	if !ok || serverCfg.Token == "" {
		return fmt.Errorf("not logged in - run 'cinch login' first")
	}

	serverURL := serverCfg.URL

	// If host is specified or forge has no default (forgejo/gitea), use PAT flow
	if host != "" || defaultHost == "" {
		if host == "" {
			return fmt.Errorf("%s requires --host flag (e.g., --host git.mycompany.com)", forge)
		}
		return runConnectPAT(forge, host, forgeName, serverURL, serverCfg.Token)
	}

	// Otherwise, use OAuth flow for hosted services
	// Check if OAuth is configured on server
	resp, err := http.Get(serverURL + apiPath)
	if err != nil {
		return fmt.Errorf("check %s status: %w", forgeName, err)
	}
	defer resp.Body.Close()

	var status struct {
		Configured bool   `json:"configured"`
		BaseURL    string `json:"base_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if !status.Configured {
		return fmt.Errorf("%s OAuth not configured on server", forgeName)
	}

	// Open browser to OAuth flow
	authURL := serverURL + authPath
	fmt.Printf("Opening browser to connect %s...\n", forgeName)
	fmt.Printf("If browser doesn't open, visit: %s\n", authURL)

	openBrowser(authURL)

	fmt.Println()
	fmt.Printf("After authorizing, you can add %s repos:\n", forgeName)
	fmt.Printf("  cinch repo add owner/name --forge %s\n", forge)
	fmt.Println()
	fmt.Println("Or visit the web UI to select repositories to onboard.")

	return nil
}

// runConnectPAT handles PAT-based connection for self-hosted instances.
func runConnectPAT(forge, host, forgeName, serverURL, userToken string) error {
	// Normalize host to URL
	baseURL := host
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "https://" + baseURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	fmt.Printf("Connecting to self-hosted %s at %s\n", forgeName, baseURL)
	fmt.Println()
	fmt.Println("You'll need a Personal Access Token (PAT) with the following scopes:")
	fmt.Println("  - api (for webhook creation and status updates)")
	fmt.Println()

	// Provide URL to create token based on forge type
	switch forge {
	case "gitlab":
		fmt.Printf("Create a token at: %s/-/user_settings/personal_access_tokens\n", baseURL)
	case "forgejo", "gitea":
		fmt.Printf("Create a token at: %s/user/settings/applications\n", baseURL)
	}
	fmt.Println()

	// Prompt for PAT
	fmt.Print("Enter your Personal Access Token: ")
	var pat string
	if _, err := fmt.Scanln(&pat); err != nil {
		return fmt.Errorf("failed to read token: %w", err)
	}
	pat = strings.TrimSpace(pat)

	if pat == "" {
		return fmt.Errorf("token cannot be empty")
	}

	// Test the token by calling the API
	fmt.Printf("Verifying token with %s...\n", forgeName)

	var apiURL string
	switch forge {
	case "gitlab":
		apiURL = baseURL + "/api/v4/user"
	case "forgejo", "gitea":
		apiURL = baseURL + "/api/v1/user"
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	// Set auth header based on forge type
	switch forge {
	case "gitlab":
		req.Header.Set("PRIVATE-TOKEN", pat)
	case "forgejo", "gitea":
		req.Header.Set("Authorization", "token "+pat)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("verify token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("invalid token (status %d): %s", resp.StatusCode, string(body))
	}

	var userInfo struct {
		Username string `json:"username"`
		Login    string `json:"login"` // Forgejo/Gitea uses login
	}
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return fmt.Errorf("decode user info: %w", err)
	}

	username := userInfo.Username
	if username == "" {
		username = userInfo.Login
	}

	fmt.Printf("✓ Token verified for user: %s\n", username)
	fmt.Println()

	// Store the credentials on the server
	fmt.Println("Saving credentials...")

	storeReq := map[string]string{
		"forge":    forge,
		"host":     baseURL,
		"token":    pat,
		"username": username,
	}
	storeBody, _ := json.Marshal(storeReq)

	req, err = http.NewRequest("POST", serverURL+"/api/forge/connect", strings.NewReader(string(storeBody)))
	if err != nil {
		return fmt.Errorf("create store request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+userToken)

	resp, err = client.Do(req)
	if err != nil {
		return fmt.Errorf("store credentials: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to store credentials (status %d): %s", resp.StatusCode, string(body))
	}

	fmt.Println("✓ Connected!")
	fmt.Println()
	fmt.Printf("You can now add repos from %s:\n", host)
	fmt.Printf("  cinch repo add owner/name --forge %s --url %s\n", forge, baseURL)

	return nil
}

// gitlabCmd is kept for backwards compatibility (cinch gitlab connect)
func gitlabCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "gitlab",
		Short:  "GitLab integration commands (deprecated: use 'cinch connect gitlab')",
		Hidden: true,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "connect",
		Short: "Connect GitLab (deprecated: use 'cinch connect gitlab')",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Note: 'cinch gitlab connect' is deprecated. Use 'cinch connect gitlab' instead.")
			return runConnect(cmd, []string{"gitlab"})
		},
	})
	return cmd
}

// openBrowser tries to open a URL in the default browser.
func openBrowser(url string) {
	var cmd string
	var cmdArgs []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		cmdArgs = []string{url}
	case "linux":
		cmd = "xdg-open"
		cmdArgs = []string{url}
	case "windows":
		cmd = "cmd"
		cmdArgs = []string{"/c", "start", url}
	default:
		return
	}

	_ = exec.Command(cmd, cmdArgs...).Start()
}
