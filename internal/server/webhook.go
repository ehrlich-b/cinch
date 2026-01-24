package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ehrlich-b/cinch/internal/forge"
	"github.com/ehrlich-b/cinch/internal/protocol"
	"github.com/ehrlich-b/cinch/internal/storage"
)

// WebhookHandler handles incoming webhooks from forges.
type WebhookHandler struct {
	storage    storage.Storage
	dispatcher *Dispatcher
	forges     []forge.Forge
	baseURL    string // Base URL for job links (e.g., "https://cinch.example.com")
	log        *slog.Logger
	githubApp  *GitHubAppHandler
}

// SetGitHubApp sets the GitHub App handler for installation-based status posting.
func (h *WebhookHandler) SetGitHubApp(gh *GitHubAppHandler) {
	h.githubApp = gh
}

// NewWebhookHandler creates a new webhook handler.
func NewWebhookHandler(store storage.Storage, dispatcher *Dispatcher, baseURL string, log *slog.Logger) *WebhookHandler {
	if log == nil {
		log = slog.Default()
	}
	return &WebhookHandler{
		storage:    store,
		dispatcher: dispatcher,
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		log:        log,
	}
}

// RegisterForge adds a forge implementation.
func (h *WebhookHandler) RegisterForge(f forge.Forge) {
	h.forges = append(h.forges, f)
}

// ServeHTTP handles webhook requests.
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Find the forge that matches this request
	var matchedForge forge.Forge
	for _, f := range h.forges {
		if f.Identify(r) {
			matchedForge = f
			break
		}
	}

	if matchedForge == nil {
		h.log.Warn("unknown forge", "headers", r.Header)
		http.Error(w, "unknown forge", http.StatusBadRequest)
		return
	}

	h.log.Debug("webhook received", "forge", matchedForge.Name())

	// We need to read the body but also need to pass the request to ParsePush
	// which will read it again. So we need to buffer and restore.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.log.Error("failed to read body", "error", err)
		http.Error(w, "failed to read body", http.StatusInternalServerError)
		return
	}
	r.Body = io.NopCloser(strings.NewReader(string(body)))

	// Try to parse without verification first to get the repo
	// We'll verify signature after we look up the webhook secret
	event, err := matchedForge.ParsePush(r, "")
	if err != nil {
		h.log.Warn("failed to parse webhook", "forge", matchedForge.Name(), "error", err)
		// Some errors are expected (like branch deletions)
		if strings.Contains(err.Error(), "deletion") {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "failed to parse webhook", http.StatusBadRequest)
		return
	}

	// Look up the repo to get the webhook secret
	ctx := r.Context()
	repo, err := h.storage.GetRepoByCloneURL(ctx, event.Repo.CloneURL)
	if err != nil {
		if err == storage.ErrNotFound {
			h.log.Warn("repo not configured", "clone_url", event.Repo.CloneURL)
			http.Error(w, "repo not configured", http.StatusNotFound)
			return
		}
		h.log.Error("failed to get repo", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Now verify signature with the stored secret
	if repo.WebhookSecret != "" {
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		_, err = matchedForge.ParsePush(r, repo.WebhookSecret)
		if err != nil {
			h.log.Warn("webhook signature verification failed", "repo", event.Repo.FullName(), "error", err)
			http.Error(w, "signature verification failed", http.StatusUnauthorized)
			return
		}
	}

	// Create job
	job, err := h.createJob(ctx, repo, event)
	if err != nil {
		h.log.Error("failed to create job", "error", err)
		http.Error(w, "failed to create job", http.StatusInternalServerError)
		return
	}

	h.log.Info("job created",
		"job_id", job.ID,
		"repo", event.Repo.FullName(),
		"branch", event.Branch,
		"commit", event.Commit[:8],
	)

	// Post pending status
	if err := h.postStatus(ctx, matchedForge, repo, event.Commit, job.ID, forge.StatusPending, "Build queued"); err != nil {
		h.log.Warn("failed to post pending status", "error", err)
		// Don't fail the webhook - job is already created
	}

	// Select command based on event type: release for tags, build for branches/PRs
	command := repo.Build
	if event.Tag != "" && repo.Release != "" {
		command = repo.Release
	}

	// Queue job for dispatch
	h.dispatcher.Enqueue(&QueuedJob{
		Job:      job,
		Repo:     repo,
		Forge:    matchedForge,
		CloneURL: event.Repo.CloneURL,
		Ref:      event.Ref,
		Branch:   event.Branch,
		Tag:      event.Tag,
		Config: protocol.JobConfig{
			Command: command,
		},
		CloneToken: repo.ForgeToken,
	})

	w.WriteHeader(http.StatusAccepted)
	fmt.Fprintf(w, `{"job_id": %q}`, job.ID)
}

func (h *WebhookHandler) createJob(ctx context.Context, repo *storage.Repo, event *forge.PushEvent) (*storage.Job, error) {
	job := &storage.Job{
		ID:        generateJobID(),
		RepoID:    repo.ID,
		Commit:    event.Commit,
		Branch:    event.Branch,
		Status:    storage.JobStatusPending,
		CreatedAt: time.Now(),
	}

	if err := h.storage.CreateJob(ctx, job); err != nil {
		return nil, err
	}

	return job, nil
}

func (h *WebhookHandler) postStatus(ctx context.Context, f forge.Forge, repo *storage.Repo, commit, jobID string, state forge.StatusState, description string) error {
	// Build target URL for logs
	targetURL := ""
	if h.baseURL != "" {
		targetURL = fmt.Sprintf("%s/jobs/%s", h.baseURL, jobID)
	}

	status := &forge.Status{
		State:       state,
		Context:     "cinch",
		Description: description,
		TargetURL:   targetURL,
	}

	// Create forge instance with token
	forgeInstance := forge.New(forge.ForgeConfig{
		Type:    f.Name(),
		Token:   repo.ForgeToken,
		BaseURL: repo.HTMLURL, // Used by Forgejo to derive API URL
	})
	if forgeInstance == nil {
		return fmt.Errorf("unknown forge: %s", f.Name())
	}

	return forgeInstance.PostStatus(ctx, &forge.Repo{
		Owner:   repo.Owner,
		Name:    repo.Name,
		HTMLURL: repo.HTMLURL,
	}, commit, status)
}

func generateJobID() string {
	return fmt.Sprintf("j_%d", time.Now().UnixNano())
}

// PostJobStatus implements StatusPoster interface.
// It looks up job info and posts status to the appropriate forge.
func (h *WebhookHandler) PostJobStatus(ctx context.Context, jobID string, state string, description string) error {
	// Get job to find repo
	job, err := h.storage.GetJob(ctx, jobID)
	if err != nil {
		return fmt.Errorf("get job: %w", err)
	}

	// Get repo to find forge info
	repo, err := h.storage.GetRepo(ctx, job.RepoID)
	if err != nil {
		return fmt.Errorf("get repo: %w", err)
	}

	// Use GitHub Check Run API if job has installation ID and check run ID
	if job.InstallationID != nil && job.CheckRunID != nil && h.githubApp != nil && h.githubApp.IsConfigured() {
		// Map state to GitHub conclusion
		conclusion := state // success, failure already map directly
		if state == "error" {
			conclusion = "failure"
		}

		// Fetch logs for the check run output
		logs, _ := h.storage.GetLogs(ctx, jobID)
		var logText string
		for _, log := range logs {
			logText += log.Data
		}

		title := "Build " + state
		if state == "success" {
			title = "Build passed"
		} else if state == "failure" || state == "error" {
			title = "Build failed"
		}

		return h.githubApp.UpdateCheckRun(repo, *job.CheckRunID, *job.InstallationID, conclusion, title, description, logText)
	}

	// Build target URL for logs
	targetURL := ""
	if h.baseURL != "" {
		targetURL = fmt.Sprintf("%s/jobs/%s", h.baseURL, jobID)
	}

	status := &forge.Status{
		State:       forge.StatusState(state),
		Context:     "cinch",
		Description: description,
		TargetURL:   targetURL,
	}

	// Post based on forge type
	forgeInstance := forge.New(forge.ForgeConfig{
		Type:    string(repo.ForgeType),
		Token:   repo.ForgeToken,
		BaseURL: repo.HTMLURL,
	})
	if forgeInstance == nil {
		return fmt.Errorf("unknown forge type: %s", repo.ForgeType)
	}

	return forgeInstance.PostStatus(ctx, &forge.Repo{
		Owner:   repo.Owner,
		Name:    repo.Name,
		HTMLURL: repo.HTMLURL,
	}, job.Commit, status)
}
