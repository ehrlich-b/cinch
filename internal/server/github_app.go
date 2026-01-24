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
	case "installation", "installation_repositories":
		// Log but don't process yet - repos auto-created on push
		h.log.Info("installation event received", "action", eventType)
		w.WriteHeader(http.StatusOK)
	case "ping":
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	default:
		h.log.Debug("ignoring event", "event", eventType)
		w.WriteHeader(http.StatusOK)
	}
}

func (h *GitHubAppHandler) verifySignature(payload []byte, signature string) bool {
	if h.config.WebhookSecret == "" {
		return true // No secret configured, skip verification
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

	// Skip if not a branch push
	if !strings.HasPrefix(event.Ref, "refs/heads/") {
		h.log.Debug("ignoring non-branch push", "ref", event.Ref)
		w.WriteHeader(http.StatusOK)
		return
	}

	branch := strings.TrimPrefix(event.Ref, "refs/heads/")
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
	job := &storage.Job{
		ID:        fmt.Sprintf("j_%d", time.Now().UnixNano()),
		RepoID:    repo.ID,
		Commit:    commit,
		Branch:    branch,
		Status:    storage.JobStatusPending,
		CreatedAt: time.Now(),
	}

	if err := h.storage.CreateJob(ctx, job); err != nil {
		h.log.Error("failed to create job", "error", err)
		http.Error(w, "failed to create job", http.StatusInternalServerError)
		return
	}

	h.log.Info("job created",
		"job_id", job.ID,
		"repo", event.Repository.FullName,
		"branch", branch,
		"commit", commit[:8],
	)

	// Post pending status
	installationID := event.Installation.ID
	if err := h.PostStatusWithInstallation(repo, commit, "pending", "Build queued", installationID); err != nil {
		h.log.Warn("failed to post pending status", "error", err)
	}

	// Enqueue job
	queuedJob := &QueuedJob{
		Job:            job,
		Repo:           repo,
		CloneURL:       repo.CloneURL,
		Branch:         branch,
		InstallationID: installationID,
	}
	h.dispatcher.Enqueue(queuedJob)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"job_id": job.ID})
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

// PostStatus posts a commit status using installation token.
func (h *GitHubAppHandler) PostStatus(ctx context.Context, repo *storage.Repo, commit, state, description string) error {
	// We need to find the installation ID - for now, try to get it from recent webhook
	// This is a limitation - we'll fix it properly later
	return h.postStatusWithToken(repo, commit, state, description, 0)
}

// PostStatusWithInstallation posts status with a specific installation ID.
func (h *GitHubAppHandler) PostStatusWithInstallation(repo *storage.Repo, commit, state, description string, installationID int64) error {
	return h.postStatusWithToken(repo, commit, state, description, installationID)
}

func (h *GitHubAppHandler) postStatusWithToken(repo *storage.Repo, commit, state, description string, installationID int64) error {
	if installationID == 0 {
		return fmt.Errorf("no installation ID available")
	}

	token, err := h.GetInstallationToken(installationID)
	if err != nil {
		return fmt.Errorf("get installation token: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/statuses/%s", repo.Owner, repo.Name, commit)

	// Add target URL if base URL is configured
	payload := map[string]string{
		"state":       state,
		"description": description,
		"context":     "cinch",
	}
	if h.baseURL != "" {
		// We'd need job ID here - skip for now
	}

	payloadBytes, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", url, strings.NewReader(string(payloadBytes)))
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

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github api error: %d - %s", resp.StatusCode, string(body))
	}

	return nil
}
