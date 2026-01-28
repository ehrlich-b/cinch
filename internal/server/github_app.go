package server

import (
	"context"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ehrlich-b/cinch/internal/storage"
	"github.com/golang-jwt/jwt/v4"
)

// GitHubAppConfig holds GitHub App configuration.
type GitHubAppConfig struct {
	AppID         int64
	PrivateKey    string // PEM-encoded private key
	WebhookSecret string
}

// GitHubAppHandler handles GitHub App webhooks and token generation.
type GitHubAppHandler struct {
	config     GitHubAppConfig
	privateKey *rsa.PrivateKey
	storage    storage.Storage
	dispatcher *Dispatcher
	baseURL    string
	log        *slog.Logger

	// Installation token cache
	tokenCache   map[int64]*cachedToken
	tokenCacheMu sync.RWMutex
}

type cachedToken struct {
	Token     string
	ExpiresAt time.Time
}

// NewGitHubAppHandler creates a new GitHub App handler.
func NewGitHubAppHandler(cfg GitHubAppConfig, store storage.Storage, dispatcher *Dispatcher, baseURL string, log *slog.Logger) (*GitHubAppHandler, error) {
	if log == nil {
		log = slog.Default()
	}

	h := &GitHubAppHandler{
		config:     cfg,
		storage:    store,
		dispatcher: dispatcher,
		baseURL:    baseURL,
		log:        log,
		tokenCache: make(map[int64]*cachedToken),
	}

	// Parse private key if provided
	if cfg.PrivateKey != "" {
		block, _ := pem.Decode([]byte(cfg.PrivateKey))
		if block == nil {
			return nil, fmt.Errorf("failed to parse PEM block")
		}

		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			// Try PKCS8
			keyInterface, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err2 != nil {
				return nil, fmt.Errorf("failed to parse private key: %w", err)
			}
			var ok bool
			key, ok = keyInterface.(*rsa.PrivateKey)
			if !ok {
				return nil, fmt.Errorf("private key is not RSA")
			}
		}
		h.privateKey = key
		log.Info("GitHub App configured", "app_id", cfg.AppID)
	}

	return h, nil
}

// IsConfigured returns true if the GitHub App is configured.
func (h *GitHubAppHandler) IsConfigured() bool {
	return h.privateKey != nil && h.config.AppID > 0
}

// ServeHTTP handles GitHub App webhook requests.
func (h *GitHubAppHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.log.Error("failed to read request body", "error", err)
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	// Verify signature
	signature := r.Header.Get("X-Hub-Signature-256")
	if !h.verifySignature(body, signature) {
		h.log.Warn("invalid webhook signature")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	// Route by event type
	eventType := r.Header.Get("X-GitHub-Event")
	h.log.Info("received github app webhook", "event", eventType)

	switch eventType {
	case "push":
		h.handlePush(w, r, body)
	case "pull_request":
		h.handlePullRequest(w, r, body)
	case "installation":
		h.handleInstallation(w, r, body)
	case "installation_repositories":
		h.handleInstallationRepositories(w, r, body)
	case "ping":
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	default:
		h.log.Debug("ignoring event", "event", eventType)
		w.WriteHeader(http.StatusOK)
	}
}

func (h *GitHubAppHandler) verifySignature(payload []byte, signature string) bool {
	if h.config.WebhookSecret == "" {
		h.log.Error("webhook secret not configured - rejecting request")
		return false // No secret configured, reject unverified webhooks
	}

	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}

	sig, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(h.config.WebhookSecret))
	mac.Write(payload)
	expected := mac.Sum(nil)

	return hmac.Equal(sig, expected)
}

// Push event handler
func (h *GitHubAppHandler) handlePush(w http.ResponseWriter, r *http.Request, body []byte) {
	var event struct {
		Ref        string `json:"ref"`
		After      string `json:"after"`
		Repository struct {
			FullName string `json:"full_name"`
			CloneURL string `json:"clone_url"`
			HTMLURL  string `json:"html_url"`
			Private  bool   `json:"private"`
		} `json:"repository"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
	}

	if err := json.Unmarshal(body, &event); err != nil {
		h.log.Error("failed to parse push event", "error", err)
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	// Parse ref to determine if branch or tag
	var branch, tag string
	if strings.HasPrefix(event.Ref, "refs/tags/") {
		tag = strings.TrimPrefix(event.Ref, "refs/tags/")
	} else if strings.HasPrefix(event.Ref, "refs/heads/") {
		branch = strings.TrimPrefix(event.Ref, "refs/heads/")
	} else {
		h.log.Debug("ignoring unknown ref type", "ref", event.Ref)
		w.WriteHeader(http.StatusOK)
		return
	}

	commit := event.After

	// Skip zero commit (branch deletion)
	if commit == "0000000000000000000000000000000000000000" {
		h.log.Debug("ignoring branch deletion", "branch", branch)
		w.WriteHeader(http.StatusOK)
		return
	}

	ctx := r.Context()

	// Find or create repo
	repo, err := h.storage.GetRepoByCloneURL(ctx, event.Repository.CloneURL)
	if err != nil {
		// Auto-create repo
		parts := strings.SplitN(event.Repository.FullName, "/", 2)
		if len(parts) != 2 {
			h.log.Error("invalid repo name", "full_name", event.Repository.FullName)
			http.Error(w, "invalid repo name", http.StatusBadRequest)
			return
		}

		repo = &storage.Repo{
			ID:            fmt.Sprintf("r_%d", time.Now().UnixNano()),
			ForgeType:     storage.ForgeTypeGitHub,
			Owner:         parts[0],
			Name:          parts[1],
			CloneURL:      event.Repository.CloneURL,
			HTMLURL:       event.Repository.HTMLURL,
			WebhookSecret: "", // Not needed for GitHub App
			CreatedAt:     time.Now(),
		}

		if err := h.storage.CreateRepo(ctx, repo); err != nil {
			h.log.Error("failed to create repo", "error", err)
			http.Error(w, "failed to create repo", http.StatusInternalServerError)
			return
		}
		h.log.Info("auto-created repo", "repo", event.Repository.FullName)
	}

	// Create job
	installationID := event.Installation.ID
	job := &storage.Job{
		ID:             fmt.Sprintf("j_%d", time.Now().UnixNano()),
		RepoID:         repo.ID,
		Commit:         commit,
		Branch:         branch,
		Tag:            tag,
		Status:         storage.JobStatusPending,
		InstallationID: &installationID,
		CreatedAt:      time.Now(),
	}

	if err := h.storage.CreateJob(ctx, job); err != nil {
		h.log.Error("failed to create job", "error", err)
		http.Error(w, "failed to create job", http.StatusInternalServerError)
		return
	}

	h.log.Info("job created",
		"job_id", job.ID,
		"repo", event.Repository.FullName,
		"ref", event.Ref,
		"commit", commit[:8],
	)

	// Create GitHub Check Run
	checkRunID, err := h.CreateCheckRun(repo, commit, job.ID, installationID)
	if err != nil {
		h.log.Warn("failed to create check run", "error", err)
	} else {
		// Save check run ID to job
		if err := h.storage.UpdateJobCheckRunID(ctx, job.ID, checkRunID); err != nil {
			h.log.Warn("failed to save check run ID", "error", err)
		}
		job.CheckRunID = &checkRunID
	}

	// Get installation token for API access (releases, status, clone for private)
	var cloneToken string
	token, err := h.GetInstallationToken(installationID)
	if err != nil {
		h.log.Warn("failed to get installation token", "error", err)
	} else {
		cloneToken = token
	}

	// Enqueue job (worker will read .cinch.yaml for command)
	queuedJob := &QueuedJob{
		Job:            job,
		Repo:           repo,
		CloneURL:       repo.CloneURL,
		Ref:            event.Ref,
		Branch:         branch,
		Tag:            tag,
		CloneToken:     cloneToken,
		InstallationID: installationID,
	}
	h.dispatcher.Enqueue(queuedJob)

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"job_id": job.ID})
}

// handlePullRequest handles GitHub pull_request webhook events.
func (h *GitHubAppHandler) handlePullRequest(w http.ResponseWriter, r *http.Request, body []byte) {
	var event struct {
		Action      string `json:"action"`
		PullRequest struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			Head   struct {
				SHA string `json:"sha"`
				Ref string `json:"ref"`
			} `json:"head"`
			Base struct {
				Ref string `json:"ref"`
			} `json:"base"`
		} `json:"pull_request"`
		Repository struct {
			FullName string `json:"full_name"`
			CloneURL string `json:"clone_url"`
			HTMLURL  string `json:"html_url"`
			Private  bool   `json:"private"`
		} `json:"repository"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
	}

	if err := json.Unmarshal(body, &event); err != nil {
		h.log.Error("failed to parse pull_request event", "error", err)
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	// Only build on actionable events
	switch event.Action {
	case "opened", "synchronize", "reopened":
		// These trigger builds
	default:
		h.log.Debug("ignoring PR action", "action", event.Action)
		w.WriteHeader(http.StatusOK)
		return
	}

	commit := event.PullRequest.Head.SHA
	prNum := event.PullRequest.Number

	ctx := r.Context()

	// Find or create repo
	repo, err := h.storage.GetRepoByCloneURL(ctx, event.Repository.CloneURL)
	if err != nil {
		// Auto-create repo
		parts := strings.SplitN(event.Repository.FullName, "/", 2)
		if len(parts) != 2 {
			h.log.Error("invalid repo name", "full_name", event.Repository.FullName)
			http.Error(w, "invalid repo name", http.StatusBadRequest)
			return
		}

		repo = &storage.Repo{
			ID:            fmt.Sprintf("r_%d", time.Now().UnixNano()),
			ForgeType:     storage.ForgeTypeGitHub,
			Owner:         parts[0],
			Name:          parts[1],
			CloneURL:      event.Repository.CloneURL,
			HTMLURL:       event.Repository.HTMLURL,
			WebhookSecret: "",
			CreatedAt:     time.Now(),
		}

		if err := h.storage.CreateRepo(ctx, repo); err != nil {
			h.log.Error("failed to create repo", "error", err)
			http.Error(w, "failed to create repo", http.StatusInternalServerError)
			return
		}
		h.log.Info("auto-created repo", "repo", event.Repository.FullName)
	}

	// Create job
	installationID := event.Installation.ID
	job := &storage.Job{
		ID:             fmt.Sprintf("j_%d", time.Now().UnixNano()),
		RepoID:         repo.ID,
		Commit:         commit,
		Branch:         event.PullRequest.Head.Ref,
		PRNumber:       &prNum,
		PRBaseBranch:   event.PullRequest.Base.Ref,
		Status:         storage.JobStatusPending,
		InstallationID: &installationID,
		CreatedAt:      time.Now(),
	}

	if err := h.storage.CreateJob(ctx, job); err != nil {
		h.log.Error("failed to create job", "error", err)
		http.Error(w, "failed to create job", http.StatusInternalServerError)
		return
	}

	h.log.Info("PR job created",
		"job_id", job.ID,
		"repo", event.Repository.FullName,
		"pr", prNum,
		"commit", commit[:8],
	)

	// Create GitHub Check Run
	checkRunID, err := h.CreateCheckRun(repo, commit, job.ID, installationID)
	if err != nil {
		h.log.Warn("failed to create check run", "error", err)
	} else {
		if err := h.storage.UpdateJobCheckRunID(ctx, job.ID, checkRunID); err != nil {
			h.log.Warn("failed to save check run ID", "error", err)
		}
		job.CheckRunID = &checkRunID
	}

	// Get installation token
	var cloneToken string
	token, err := h.GetInstallationToken(installationID)
	if err != nil {
		h.log.Warn("failed to get installation token", "error", err)
	} else {
		cloneToken = token
	}

	// Enqueue job - PRs always use build command
	queuedJob := &QueuedJob{
		Job:            job,
		Repo:           repo,
		CloneURL:       repo.CloneURL,
		Ref:            fmt.Sprintf("refs/pull/%d/head", prNum),
		Branch:         event.PullRequest.Head.Ref,
		CloneToken:     cloneToken,
		InstallationID: installationID,
	}
	h.dispatcher.Enqueue(queuedJob)

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"job_id": job.ID})
}

// handleInstallation handles GitHub App installation events.
// When action is "created", we auto-create repos for all selected repositories.
func (h *GitHubAppHandler) handleInstallation(w http.ResponseWriter, r *http.Request, body []byte) {
	var event struct {
		Action       string `json:"action"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
		Repositories []struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
			Private  bool   `json:"private"`
		} `json:"repositories"`
	}

	if err := json.Unmarshal(body, &event); err != nil {
		h.log.Error("failed to parse installation event", "error", err)
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	h.log.Info("installation event",
		"action", event.Action,
		"installation_id", event.Installation.ID,
		"repos_count", len(event.Repositories),
	)

	ctx := r.Context()

	switch event.Action {
	case "created":
		// User installed the app - create repos for all selected repositories
		for _, repo := range event.Repositories {
			if err := h.createRepoFromInstallation(ctx, repo.FullName, repo.Private); err != nil {
				h.log.Warn("failed to create repo from installation", "repo", repo.FullName, "error", err)
			}
		}
	case "deleted":
		// User uninstalled the app - we could delete repos, but leaving them is safer
		h.log.Info("app uninstalled", "installation_id", event.Installation.ID)
	}

	w.WriteHeader(http.StatusOK)
}

// handleInstallationRepositories handles when users add/remove repos from an existing installation.
func (h *GitHubAppHandler) handleInstallationRepositories(w http.ResponseWriter, r *http.Request, body []byte) {
	var event struct {
		Action       string `json:"action"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
		RepositoriesAdded []struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
			Private  bool   `json:"private"`
		} `json:"repositories_added"`
		RepositoriesRemoved []struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
		} `json:"repositories_removed"`
	}

	if err := json.Unmarshal(body, &event); err != nil {
		h.log.Error("failed to parse installation_repositories event", "error", err)
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	h.log.Info("installation_repositories event",
		"action", event.Action,
		"installation_id", event.Installation.ID,
		"added", len(event.RepositoriesAdded),
		"removed", len(event.RepositoriesRemoved),
	)

	ctx := r.Context()

	// Handle added repositories
	for _, repo := range event.RepositoriesAdded {
		if err := h.createRepoFromInstallation(ctx, repo.FullName, repo.Private); err != nil {
			h.log.Warn("failed to create repo from installation", "repo", repo.FullName, "error", err)
		}
	}

	// Handle removed repositories - we leave them in DB but log it
	// Could optionally mark them as "disconnected" in future
	for _, repo := range event.RepositoriesRemoved {
		h.log.Info("repo removed from installation", "repo", repo.FullName)
	}

	w.WriteHeader(http.StatusOK)
}

// createRepoFromInstallation creates a repo record from installation event data.
func (h *GitHubAppHandler) createRepoFromInstallation(ctx context.Context, fullName string, private bool) error {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo name: %s", fullName)
	}

	owner, name := parts[0], parts[1]
	cloneURL := fmt.Sprintf("https://github.com/%s.git", fullName)
	htmlURL := fmt.Sprintf("https://github.com/%s", fullName)

	// Check if repo already exists
	_, err := h.storage.GetRepoByCloneURL(ctx, cloneURL)
	if err == nil {
		// Repo already exists
		h.log.Debug("repo already exists", "repo", fullName)
		return nil
	}

	// Create new repo
	repo := &storage.Repo{
		ID:            fmt.Sprintf("r_%d", time.Now().UnixNano()),
		ForgeType:     storage.ForgeTypeGitHub,
		Owner:         owner,
		Name:          name,
		CloneURL:      cloneURL,
		HTMLURL:       htmlURL,
		WebhookSecret: "", // Not needed for GitHub App
		Private:       private,
		CreatedAt:     time.Now(),
	}

	if err := h.storage.CreateRepo(ctx, repo); err != nil {
		return fmt.Errorf("create repo: %w", err)
	}

	h.log.Info("created repo from installation", "repo", fullName, "private", private)
	return nil
}

// GetInstallationToken gets or refreshes an installation token.
func (h *GitHubAppHandler) GetInstallationToken(installationID int64) (string, error) {
	// Check cache
	h.tokenCacheMu.RLock()
	cached, ok := h.tokenCache[installationID]
	h.tokenCacheMu.RUnlock()

	if ok && time.Now().Add(5*time.Minute).Before(cached.ExpiresAt) {
		return cached.Token, nil
	}

	// Generate new token
	token, expiresAt, err := h.requestInstallationToken(installationID)
	if err != nil {
		return "", err
	}

	// Cache it
	h.tokenCacheMu.Lock()
	h.tokenCache[installationID] = &cachedToken{
		Token:     token,
		ExpiresAt: expiresAt,
	}
	h.tokenCacheMu.Unlock()

	return token, nil
}

func (h *GitHubAppHandler) requestInstallationToken(installationID int64) (string, time.Time, error) {
	// Create app JWT
	appJWT, err := h.createAppJWT()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create app JWT: %w", err)
	}

	// Request installation token
	url := fmt.Sprintf("https://api.github.com/app/installations/%d/access_tokens", installationID)
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", time.Time{}, fmt.Errorf("github api error: %d - %s", resp.StatusCode, string(body))
	}

	var result struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", time.Time{}, fmt.Errorf("decode response: %w", err)
	}

	return result.Token, result.ExpiresAt, nil
}

func (h *GitHubAppHandler) createAppJWT() (string, error) {
	if h.privateKey == nil {
		return "", fmt.Errorf("private key not configured")
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iat": now.Add(-60 * time.Second).Unix(), // 60 seconds in the past for clock drift
		"exp": now.Add(10 * time.Minute).Unix(),  // 10 minute max
		"iss": h.config.AppID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(h.privateKey)
}

// CreateCheckRun creates a GitHub Check Run and returns its ID.
func (h *GitHubAppHandler) CreateCheckRun(repo *storage.Repo, commit, jobID string, installationID int64) (int64, error) {
	if installationID == 0 {
		return 0, fmt.Errorf("no installation ID available")
	}

	token, err := h.GetInstallationToken(installationID)
	if err != nil {
		return 0, fmt.Errorf("get installation token: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/check-runs", repo.Owner, repo.Name)

	payload := map[string]any{
		"name":     "cinch",
		"head_sha": commit,
		"status":   "queued",
	}
	if h.baseURL != "" {
		payload["details_url"] = fmt.Sprintf("%s/jobs/%s", h.baseURL, jobID)
	}

	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", url, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("github api error: %d - %s", resp.StatusCode, string(body))
	}

	var result struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}

	return result.ID, nil
}

// UpdateCheckRun updates a GitHub Check Run with completion status.
func (h *GitHubAppHandler) UpdateCheckRun(repo *storage.Repo, checkRunID int64, installationID int64, conclusion string, title string, summary string, logs string) error {
	if installationID == 0 {
		return fmt.Errorf("no installation ID available")
	}
	if checkRunID == 0 {
		return fmt.Errorf("no check run ID available")
	}

	token, err := h.GetInstallationToken(installationID)
	if err != nil {
		return fmt.Errorf("get installation token: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/check-runs/%d", repo.Owner, repo.Name, checkRunID)

	output := map[string]string{
		"title":   title,
		"summary": summary,
	}
	if logs != "" {
		// Truncate logs to fit GitHub's limit (65535 chars for text field)
		if len(logs) > 60000 {
			logs = "... (truncated)\n" + logs[len(logs)-60000:]
		}
		output["text"] = "```\n" + logs + "\n```"
	}

	payload := map[string]any{
		"status":       "completed",
		"conclusion":   conclusion, // success, failure, neutral, cancelled, skipped, timed_out, action_required
		"completed_at": time.Now().UTC().Format(time.RFC3339),
		"output":       output,
	}

	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest("PATCH", url, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github api error: %d - %s", resp.StatusCode, string(body))
	}

	return nil
}

// UpdateCheckRunInProgress marks a check run as in progress.
func (h *GitHubAppHandler) UpdateCheckRunInProgress(repo *storage.Repo, checkRunID int64, installationID int64) error {
	if installationID == 0 || checkRunID == 0 {
		return nil // silently skip if not configured
	}

	token, err := h.GetInstallationToken(installationID)
	if err != nil {
		return fmt.Errorf("get installation token: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/check-runs/%d", repo.Owner, repo.Name, checkRunID)

	payload := map[string]any{
		"status":     "in_progress",
		"started_at": time.Now().UTC().Format(time.RFC3339),
	}

	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest("PATCH", url, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github api error: %d - %s", resp.StatusCode, string(body))
	}

	return nil
}
