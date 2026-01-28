package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/ehrlich-b/cinch/internal/daemon"
	"github.com/ehrlich-b/cinch/internal/worker"
)

// DaemonConfig holds configuration for daemon commands.
type DaemonConfig struct {
	Concurrency int
	SocketPath  string
	LogFile     string
	Verbose     bool
}

// DefaultDaemonConfig returns the default daemon configuration.
func DefaultDaemonConfig() DaemonConfig {
	home, _ := os.UserHomeDir()
	return DaemonConfig{
		Concurrency: 1,
		SocketPath:  filepath.Join(home, ".cinch", "daemon.sock"),
		LogFile:     filepath.Join(home, ".cinch", "daemon.log"),
	}
}

// RunDaemon runs the daemon in the foreground (used by daemon run command).
func RunDaemon(cfg DaemonConfig, serverURL, token string, labels []string) error {
	// Ensure config directory exists
	configDir := filepath.Dir(cfg.SocketPath)
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	// Set up logging
	var logWriter io.Writer = os.Stderr
	if cfg.LogFile != "" {
		f, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
		defer f.Close()
		logWriter = io.MultiWriter(os.Stderr, f)
	}

	var log *slog.Logger
	if cfg.Verbose {
		log = slog.New(slog.NewTextHandler(logWriter, nil))
	} else {
		log = slog.New(slog.NewTextHandler(logWriter, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}

	// Create worker
	workerCfg := worker.WorkerConfig{
		ServerURL:   serverURL,
		Token:       token,
		Labels:      labels,
		Docker:      true,
		Concurrency: cfg.Concurrency,
		SocketPath:  cfg.SocketPath,
	}
	w := worker.NewWorker(workerCfg, log)

	// Create daemon server
	srv := daemon.NewServer(cfg.SocketPath, w, log)

	// Wire up event broadcasting
	w.SetEventBroadcaster(srv)

	// Start daemon server
	if err := srv.Start(); err != nil {
		return fmt.Errorf("start daemon server: %w", err)
	}
	defer srv.Stop()

	// Start worker
	if err := w.Start(); err != nil {
		return fmt.Errorf("start worker: %w", err)
	}

	log.Info("daemon running", "concurrency", cfg.Concurrency, "socket", cfg.SocketPath)

	// Wait for interrupt
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
	log.Info("shutting down daemon")
	w.Stop()

	return nil
}

// StartDaemon starts the daemon in the background.
func StartDaemon(cfg DaemonConfig, serverURL, token string, labels []string) error {
	// Check if daemon is already running
	if daemon.IsDaemonRunning(cfg.SocketPath) {
		return fmt.Errorf("daemon already running at %s", cfg.SocketPath)
	}

	// Get the path to the current executable
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	// Build command arguments
	args := []string{"daemon", "run",
		"-n", strconv.Itoa(cfg.Concurrency),
		"--socket", cfg.SocketPath,
	}
	if cfg.Verbose {
		args = append(args, "-v")
	}

	// Start the daemon process
	cmd := exec.Command(executable, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Create new session (detach from terminal)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon process: %w", err)
	}

	// Write PID file
	pidFile := cfg.SocketPath + ".pid"
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0600); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}

	// Wait for daemon to be ready
	for i := 0; i < 30; i++ {
		if daemon.IsDaemonRunning(cfg.SocketPath) {
			fmt.Printf("Daemon started (pid %d)\n", cmd.Process.Pid)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("daemon failed to start")
}

// StopDaemon stops the running daemon.
func StopDaemon(socketPath string) error {
	pidFile := socketPath + ".pid"
	data, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("daemon not running (no pid file)")
		}
		return fmt.Errorf("read pid file: %w", err)
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return fmt.Errorf("invalid pid file: %w", err)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process: %w", err)
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("send signal: %w", err)
	}

	// Wait for socket to disappear
	for i := 0; i < 30; i++ {
		if !daemon.IsDaemonRunning(socketPath) {
			os.Remove(pidFile)
			fmt.Println("Daemon stopped")
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Force kill if still running
	_ = process.Kill()
	os.Remove(pidFile)
	os.Remove(socketPath)
	fmt.Println("Daemon killed")

	return nil
}

// DaemonStatus returns the daemon's status.
func DaemonStatus(socketPath string) error {
	client, err := daemon.Connect(socketPath)
	if err != nil {
		return fmt.Errorf("daemon not running: %w", err)
	}
	defer client.Close()

	status, err := client.Status()
	if err != nil {
		return fmt.Errorf("get status: %w", err)
	}

	fmt.Printf("Slots: %d/%d\n", status.SlotsBusy, status.SlotsTotal)

	if len(status.RunningJobs) == 0 {
		fmt.Println("No jobs running")
		return nil
	}

	fmt.Println("\nRunning jobs:")
	for _, job := range status.RunningJobs {
		duration := time.Since(time.Unix(job.StartedAt, 0)).Truncate(time.Second)
		ref := job.Branch
		if job.Tag != "" {
			ref = job.Tag
		}
		commit := job.Commit
		if len(commit) > 8 {
			commit = commit[:8]
		}

		repo := parseRepoShort(job.Repo)
		fmt.Printf("  %s  %s@%s (%s)  %s\n", job.JobID, repo, ref, commit, duration)
	}

	return nil
}

// DaemonLogs tails the daemon log file.
func DaemonLogs(logFile string, follow bool) error {
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		return fmt.Errorf("log file not found: %s", logFile)
	}

	args := []string{"-n", "100"}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, logFile)

	cmd := exec.Command("tail", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// InstallDaemonService installs the daemon as a system service.
func InstallDaemonService(concurrency int) error {
	switch runtime.GOOS {
	case "darwin":
		return installLaunchdService(concurrency)
	case "linux":
		return installSystemdService(concurrency)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// UninstallDaemonService removes the daemon system service.
func UninstallDaemonService() error {
	switch runtime.GOOS {
	case "darwin":
		return uninstallLaunchdService()
	case "linux":
		return uninstallSystemdService()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// parseRepoShort extracts "owner/repo" from a clone URL.
func parseRepoShort(cloneURL string) string {
	// Simple extraction - strip protocol and .git suffix
	url := cloneURL
	for _, prefix := range []string{"https://", "http://", "git@"} {
		if len(url) > len(prefix) && url[:len(prefix)] == prefix {
			url = url[len(prefix):]
			break
		}
	}
	// Handle git@host:owner/repo format
	for i, c := range url {
		if c == ':' {
			url = url[i+1:]
			break
		}
		if c == '/' {
			break
		}
	}
	// Remove host for https URLs
	if idx := findFirstSlash(url); idx > 0 && !hasSlashBefore(url, idx) {
		url = url[idx+1:]
	}
	// Remove .git suffix
	if len(url) > 4 && url[len(url)-4:] == ".git" {
		url = url[:len(url)-4]
	}
	return url
}

func findFirstSlash(s string) int {
	for i, c := range s {
		if c == '/' {
			return i
		}
	}
	return -1
}

func hasSlashBefore(s string, idx int) bool {
	for i := 0; i < idx; i++ {
		if s[i] == '/' {
			return true
		}
	}
	return false
}
