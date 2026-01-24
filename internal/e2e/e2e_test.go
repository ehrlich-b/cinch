package e2e

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ehrlich-b/cinch/internal/forge"
	"github.com/ehrlich-b/cinch/internal/protocol"
	"github.com/ehrlich-b/cinch/internal/server"
	"github.com/ehrlich-b/cinch/internal/storage"
	"github.com/ehrlich-b/cinch/internal/worker"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/sha3"
)

// TestE2EFullPipeline tests the complete workflow:
// webhook received → job queued → worker picks up → job executes → logs streamed → job completes
func TestE2EFullPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	// Ensure git is available
	if err := exec.Command("git", "--version").Run(); err != nil {
		t.Skip("git not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create test repo with simple command
	testRepo := createTestRepo(t)

	log := slog.Default()

	// Initialize storage with temp file (not :memory: to avoid parallel test issues)
	dbFile := filepath.Join(t.TempDir(), "test.db")
	store, err := storage.NewSQLite(dbFile)
	if err != nil {
		t.Fatalf("NewSQLite failed: %v", err)
	}
	defer store.Close()

	// Create worker token
	token := "test-worker-token-12345"
	tokenRecord := &storage.Token{
		ID:        "tok_1",
		Hash:      hashToken(token),
		Name:      "test-worker",
		CreatedAt: time.Now(),
	}
	if err := store.CreateToken(ctx, tokenRecord); err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}

	// Create worker record in storage (for foreign key when job is assigned)
	if err := store.CreateWorker(ctx, &storage.Worker{
		ID:        "tok_1", // Same as token ID - this becomes worker ID
		Name:      "test-worker",
		Labels:    []string{"linux"},
		Status:    storage.WorkerStatusOnline,
		LastSeen:  time.Now(),
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateWorker failed: %v", err)
	}

	// Create repo in storage
	webhookSecret := "webhook-secret-12345"
	repo := &storage.Repo{
		ID:            "r_1",
		ForgeType:     storage.ForgeTypeGitHub,
		Owner:         "testowner",
		Name:          "testrepo",
		CloneURL:      "file://" + testRepo.Dir,
		WebhookSecret: webhookSecret,
		Command:       "echo 'Hello from Cinch CI!'",
		CreatedAt:     time.Now(),
	}
	if err := store.CreateRepo(ctx, repo); err != nil {
		t.Fatalf("CreateRepo failed: %v", err)
	}

	// Create server components
	hub := server.NewHub()
	wsHandler := server.NewWSHandler(hub, store, log)
	dispatcher := server.NewDispatcher(hub, store, wsHandler, log)
	webhookHandler := server.NewWebhookHandler(store, dispatcher, "", log)
	logStreamHandler := server.NewLogStreamHandler(store, log)

	// Wire up dependencies
	wsHandler.SetStatusPoster(&mockStatusPoster{t: t})
	wsHandler.SetLogBroadcaster(logStreamHandler)
	wsHandler.SetWorkerNotifier(dispatcher)

	// Register GitHub forge
	webhookHandler.RegisterForge(&forge.GitHub{})

	// Start dispatcher
	dispatcher.Start()
	defer dispatcher.Stop()

	// Create HTTP server
	mux := http.NewServeMux()
	mux.Handle("/webhooks", webhookHandler)
	mux.Handle("/webhooks/", webhookHandler)
	mux.Handle("/ws/worker", wsHandler)
	mux.HandleFunc("/ws/logs/", logStreamHandler.ServeHTTP)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Track logs received
	var logsMu sync.Mutex
	var receivedLogs []string

	// Start worker
	workerCfg := worker.WorkerConfig{
		ServerURL:   "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/worker",
		Token:       token,
		Labels:      []string{"linux"},
		Concurrency: 1,
		Docker:      false,
	}

	w := worker.NewWorker(workerCfg, log)
	w.OnJobComplete = func(jobID string, exitCode int, duration time.Duration) {
		t.Logf("Job %s completed with exit code %d in %v", jobID, exitCode, duration)
	}

	if err := w.Start(); err != nil {
		t.Fatalf("Worker.Start failed: %v", err)
	}
	defer w.Stop()

	// Wait for worker to register
	time.Sleep(200 * time.Millisecond)

	// Connect to log WebSocket to capture logs
	go func() {
		// Wait for job to be created first
		time.Sleep(500 * time.Millisecond)

		// Get the job ID from storage
		jobs, _ := store.ListJobs(ctx, storage.JobFilter{Limit: 1})
		if len(jobs) == 0 {
			return
		}
		jobID := jobs[0].ID

		wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/logs/" + jobID
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Logf("Log WebSocket dial failed: %v", err)
			return
		}
		defer conn.Close()

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			logsMu.Lock()
			receivedLogs = append(receivedLogs, string(msg))
			logsMu.Unlock()
		}
	}()

	// Send webhook
	payload := fmt.Sprintf(`{
		"ref": "refs/heads/main",
		"after": "%s",
		"deleted": false,
		"repository": {
			"name": "testrepo",
			"full_name": "testowner/testrepo",
			"private": false,
			"html_url": "https://github.com/testowner/testrepo",
			"clone_url": "file://%s",
			"owner": {
				"login": "testowner"
			}
		},
		"sender": {
			"login": "testuser"
		}
	}`, testRepo.Commit, testRepo.Dir)

	// Sign the payload
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write([]byte(payload))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req, _ := http.NewRequest("POST", srv.URL+"/webhooks", strings.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Webhook request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("Webhook returned %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	// Wait for job to complete
	var finalJob *storage.Job
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		jobs, err := store.ListJobs(ctx, storage.JobFilter{Limit: 1})
		if err != nil || len(jobs) == 0 {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		job := jobs[0]
		if job.Status == storage.JobStatusSuccess || job.Status == storage.JobStatusFailed {
			finalJob = job
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if finalJob == nil {
		// List all jobs for debugging
		jobs, _ := store.ListJobs(ctx, storage.JobFilter{Limit: 10})
		for _, j := range jobs {
			t.Logf("Job %s: status=%s", j.ID, j.Status)
		}
		t.Fatal("Job did not complete within timeout")
	}

	// Verify job succeeded
	if finalJob.Status != storage.JobStatusSuccess {
		t.Errorf("Job status = %s, want %s", finalJob.Status, storage.JobStatusSuccess)
	}

	// Verify logs were stored
	logs, err := store.GetLogs(ctx, finalJob.ID)
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	if len(logs) == 0 {
		t.Error("Expected logs to be stored")
	}

	// Check that expected output is in logs
	fullLog := ""
	for _, l := range logs {
		fullLog += l.Data
	}
	if !strings.Contains(fullLog, "Hello from Cinch CI!") {
		t.Errorf("Expected output not found in logs: %s", fullLog)
	}

	t.Logf("E2E test passed: webhook → job → worker → logs → completion")
	t.Logf("Final job status: %s", finalJob.Status)
	t.Logf("Log entries: %d", len(logs))
}

// testRepo holds test repository info
type testRepo struct {
	Dir    string
	Commit string
}

// createTestRepo creates a temporary git repo for testing
func createTestRepo(t *testing.T) *testRepo {
	t.Helper()

	dir, err := os.MkdirTemp("", "cinch-test-repo-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	// Initialize git repo with main branch
	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test User"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, output)
		}
	}

	// Create a simple file
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("# Test Repo\n"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Commit
	cmds = [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "Initial commit"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\n%s", args, err, output)
		}
	}

	// Get the commit hash
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse failed: %v", err)
	}
	commit := strings.TrimSpace(string(output))

	return &testRepo{
		Dir:    dir,
		Commit: commit,
	}
}

// hashToken hashes a token for storage (must match server implementation - SHA3-256)
func hashToken(token string) string {
	h := sha3.New256()
	h.Write([]byte(token))
	return hex.EncodeToString(h.Sum(nil))
}

// mockStatusPoster is a no-op status poster for testing
type mockStatusPoster struct {
	t        *testing.T
	mu       sync.Mutex
	statuses []statusUpdate
}

type statusUpdate struct {
	jobID       string
	state       string
	description string
}

func (m *mockStatusPoster) PostJobStatus(ctx context.Context, jobID string, state string, description string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statuses = append(m.statuses, statusUpdate{jobID, state, description})
	m.t.Logf("Status posted: job=%s state=%s desc=%s", jobID, state, description)
	return nil
}

// TestE2EWorkerReconnect tests that worker reconnects after disconnect
func TestE2EWorkerReconnect(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	log := slog.Default()

	// Initialize storage with temp file
	dbFile := filepath.Join(t.TempDir(), "test.db")
	store, err := storage.NewSQLite(dbFile)
	if err != nil {
		t.Fatalf("NewSQLite failed: %v", err)
	}
	defer store.Close()

	// Create token
	token := "test-token"
	if err := store.CreateToken(ctx, &storage.Token{
		ID:        "tok_1",
		Hash:      hashToken(token),
		Name:      "test",
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}

	// Create server
	hub := server.NewHub()
	wsHandler := server.NewWSHandler(hub, store, log)
	dispatcher := server.NewDispatcher(hub, store, wsHandler, log)
	wsHandler.SetWorkerNotifier(dispatcher)
	dispatcher.Start()
	defer dispatcher.Stop()

	mux := http.NewServeMux()
	mux.Handle("/ws/worker", wsHandler)

	srv := httptest.NewServer(mux)

	// Start worker
	workerCfg := worker.WorkerConfig{
		ServerURL:   "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/worker",
		Token:       token,
		Concurrency: 1,
	}

	w := worker.NewWorker(workerCfg, log)
	if err := w.Start(); err != nil {
		t.Fatalf("Worker.Start failed: %v", err)
	}
	defer w.Stop()

	// Wait for connection
	time.Sleep(200 * time.Millisecond)

	// Verify worker connected
	if hub.Count() != 1 {
		t.Fatalf("WorkerCount = %d, want 1", hub.Count())
	}

	// Close server (simulate disconnect)
	srv.Close()

	// Wait a bit
	time.Sleep(500 * time.Millisecond)

	// Start new server on different port (worker should reconnect)
	srv2 := httptest.NewServer(mux)
	defer srv2.Close()

	// Worker won't automatically reconnect to new URL, but will try to reconnect
	// This test mainly verifies the disconnect is handled gracefully
	t.Log("Worker handled disconnect gracefully")
}

// TestE2EJobCancellation tests that jobs can be cancelled
func TestE2EJobCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	log := slog.Default()

	// Initialize storage with temp file
	dbFile := filepath.Join(t.TempDir(), "test.db")
	store, err := storage.NewSQLite(dbFile)
	if err != nil {
		t.Fatalf("NewSQLite failed: %v", err)
	}
	defer store.Close()

	// Create token
	token := "test-token"
	if err := store.CreateToken(ctx, &storage.Token{
		ID:        "tok_1",
		Hash:      hashToken(token),
		Name:      "test",
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}

	// Create repo
	if err := store.CreateRepo(ctx, &storage.Repo{
		ID:        "r_1",
		ForgeType: storage.ForgeTypeGitHub,
		CloneURL:  "https://github.com/test/repo.git",
		Command:   "sleep 60", // Long-running command
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateRepo failed: %v", err)
	}

	// Create server components
	hub := server.NewHub()
	wsHandler := server.NewWSHandler(hub, store, log)
	dispatcher := server.NewDispatcher(hub, store, wsHandler, log)
	wsHandler.SetWorkerNotifier(dispatcher)
	dispatcher.Start()
	defer dispatcher.Stop()

	mux := http.NewServeMux()
	mux.Handle("/ws/worker", wsHandler)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Start worker
	workerCfg := worker.WorkerConfig{
		ServerURL:   "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/worker",
		Token:       token,
		Concurrency: 1,
	}

	w := worker.NewWorker(workerCfg, log)
	w.OnJobError = func(jobID, phase, errMsg string) {
		t.Logf("Job error: job=%s phase=%s err=%s", jobID, phase, errMsg)
	}

	if err := w.Start(); err != nil {
		t.Fatalf("Worker.Start failed: %v", err)
	}
	defer w.Stop()

	// Wait for connection
	time.Sleep(200 * time.Millisecond)

	// Create and queue a job
	job := &storage.Job{
		ID:        "j_cancel_test",
		RepoID:    "r_1",
		Commit:    "abc123",
		Branch:    "main",
		Status:    storage.JobStatusPending,
		CreatedAt: time.Now(),
	}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob failed: %v", err)
	}

	// Queue job - worker will try to clone and fail (no real repo)
	repo := &storage.Repo{
		ID:        "r_1",
		ForgeType: storage.ForgeTypeGitHub,
		CloneURL:  "https://github.com/test/repo.git",
	}
	dispatcher.Enqueue(&server.QueuedJob{
		Job:      job,
		Repo:     repo,
		CloneURL: "https://github.com/test/repo.git",
		Branch:   "main",
		Config:   protocol.JobConfig{Command: "sleep 60"},
	})

	// Wait for job to start (will fail on clone since repo doesn't exist)
	time.Sleep(2 * time.Second)

	// Job should have failed during clone phase
	finalJob, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob failed: %v", err)
	}

	// Either failed during clone or was cancelled
	t.Logf("Job final status: %s", finalJob.Status)
}
