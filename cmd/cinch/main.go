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

	"github.com/ehrlich-b/cinch/internal/cli"
	"github.com/ehrlich-b/cinch/internal/config"
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
		// User must be logged in to Cinch first
		user := authHandler.GetUser(r)
		if user == "" {
			// Redirect to login with return to GitLab callback
			returnURL := r.URL.String()
			http.Redirect(w, r, "/auth/login?return_to="+returnURL, http.StatusFound)
			return
		}
		gitlabOAuthHandler.HandleCallback(w, r, user)
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
		Short: "Start a worker that connects to the server",
		Long: `Start a worker that connects to a Cinch server.

If --server and --token are not provided, uses credentials from ~/.cinch/config
(set via 'cinch login').

Example:
  cinch worker                              # uses saved credentials
  cinch worker --server wss://cinch.sh/ws/worker --token xxx`,
		RunE: runWorker,
	}
	cmd.Flags().String("server", "", "Server WebSocket URL (uses saved credentials if not set)")
	cmd.Flags().String("token", "", "Authentication token (uses saved credentials if not set)")
	cmd.Flags().StringSlice("labels", nil, "Worker labels (e.g., linux-amd64,docker)")
	return cmd
}

func runWorker(cmd *cobra.Command, args []string) error {
	serverURL, _ := cmd.Flags().GetString("server")
	token, _ := cmd.Flags().GetString("token")
	labels, _ := cmd.Flags().GetStringSlice("labels")

	log := slog.Default()

	// If server/token not provided, try to use saved credentials
	if serverURL == "" || token == "" {
		cliCfg, err := cli.LoadConfig()
		if err != nil {
			return fmt.Errorf("load credentials: %w", err)
		}

		defaultServer, ok := cliCfg.Servers["default"]
		if !ok || defaultServer.Token == "" {
			return fmt.Errorf("not logged in - run 'cinch login' first, or provide --server and --token")
		}

		if serverURL == "" {
			// Convert HTTP URL to WebSocket URL
			wsURL := defaultServer.URL
			wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
			wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
			serverURL = wsURL + "/ws/worker"
		}
		if token == "" {
			token = defaultServer.Token
		}

		log.Info("using saved credentials", "user", defaultServer.User, "server", defaultServer.URL)
	}

	workerCfg := worker.WorkerConfig{
		ServerURL: serverURL,
		Token:     token,
		Labels:    labels,
		Docker:    true, // Assume Docker available
	}

	w := worker.NewWorker(workerCfg, log)

	// Start worker
	log.Info("starting worker", "server", serverURL, "labels", labels)
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

	fmt.Printf("Logging in to %s...\n", serverURL)

	// Request device code
	deviceResp, err := cli.RequestDeviceCode(serverURL)
	if err != nil {
		return fmt.Errorf("failed to start login: %w", err)
	}

	// Show user code and open browser
	fmt.Printf("\nYour code: %s\n", deviceResp.UserCode)
	fmt.Printf("Opening browser to: %s\n", deviceResp.VerificationURI)
	fmt.Println("\nWaiting for authorization...")

	// Try to open browser
	openBrowser(deviceResp.VerificationURI + "?code=" + deviceResp.UserCode)

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
		User:  tokenResp.User,
	})

	if err := cli.SaveConfig(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("\nLogged in as %s\n", tokenResp.User)
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
				fmt.Printf("User: %s\n", sc.User)
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
	return &cobra.Command{
		Use:   "install",
		Short: "Install or update cinch",
		Long: `Download and run the cinch install script.

This fetches the latest version from GitHub releases and installs
all platform binaries to ~/.cinch/bin/.`,
		RunE: func(cmd *cobra.Command, args []string) error {
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

			return shCmd.Run()
		},
	}
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
