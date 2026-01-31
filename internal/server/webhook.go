package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ehrlich-b/cinch/internal/forge"
	"github.com/ehrlich-b/cinch/internal/logstore"
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
	logStore   logstore.LogStore
}

// SetGitHubApp sets the GitHub App handler for installation-based status posting.
func (h *WebhookHandler) SetGitHubApp(gh *GitHubAppHandler) {
	h.githubApp = gh
}

// SetLogStore sets the log store for fetching logs (for Check Run output).
func (h *WebhookHandler) SetLogStore(ls logstore.LogStore) {
	h.logStore = ls
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

	// We need to read the body but also need to pass the request to parsers
	// which will read it again. So we need to buffer and restore.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.log.Error("failed to read body", "error", err)
		http.Error(w, "failed to read body", http.StatusInternalServerError)
		return
	}
	r.Body = io.NopCloser(strings.NewReader(string(body)))

	// Try to parse as PR first, then push
	// We'll verify signature after we look up the webhook secret
	prEvent, prErr := matchedForge.ParsePullRequest(r, "")
	if prErr == nil {
		// Handle PR event
		h.handlePullRequest(w, r, body, matchedForge, prEvent)
		return
	}

	// Not a PR, try push
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	event, err := matchedForge.ParsePush(r, "")
	if err != nil {
		h.log.Warn("failed to parse webhook", "forge", matchedForge.Name(), "error", err, "pr_error", prErr)
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

	// SECURITY: Verify signature BEFORE any state changes
	if repo.WebhookSecret != "" {
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		_, err = matchedForge.ParsePush(r, repo.WebhookSecret)
		if err != nil {
			h.log.Warn("webhook signature verification failed", "repo", event.Repo.FullName(), "error", err)
			http.Error(w, "signature verification failed", http.StatusUnauthorized)
			return
		}
	}

	// Now safe to sync private flag (after signature verified)
	if repo.Private != event.Repo.Private {
		if err := h.storage.UpdateRepoPrivate(ctx, repo.ID, event.Repo.Private); err != nil {
			h.log.Warn("failed to update repo private flag", "error", err)
		} else {
			h.log.Info("repo private flag updated", "repo", event.Repo.FullName(), "private", event.Repo.Private)
			repo.Private = event.Repo.Private
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
		"author", event.Sender,
		"trust_level", job.TrustLevel,
	)

	// Check if private repo can run builds (requires Pro)
	if err := h.checkPrivateRepoAccess(ctx, repo); err != nil {
		h.log.Info("private repo blocked", "repo", event.Repo.FullName(), "error", err)
		// Create a visible error - fail the job with a helpful message
		errMsg := "Private repos require Pro. Get Pro free at cinch.sh/account"
		exitCode := 1
		if updateErr := h.storage.UpdateJobStatus(ctx, job.ID, storage.JobStatusFailed, &exitCode); updateErr != nil {
			h.log.Error("failed to update job status", "error", updateErr)
		}
		// Post failed status to GitHub/GitLab so user sees it
		if statusErr := h.postStatus(ctx, matchedForge, repo, event.Commit, job.ID, forge.StatusError, errMsg); statusErr != nil {
			h.log.Warn("failed to post billing error status", "error", statusErr)
		}
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, `{"job_id": %q, "error": "billing_required"}`, job.ID)
		return
	}

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
		Labels:   repo.Workers, // Worker labels for fan-out
		CloneURL: event.Repo.CloneURL,
		Ref:      event.Ref,
		Branch:   event.Branch,
		Tag:      event.Tag,
		Config: protocol.JobConfig{
			Command: command,
			Env:     repo.Secrets, // Inject repo secrets as env vars
		},
		CloneToken: repo.ForgeToken,
	})

	w.WriteHeader(http.StatusAccepted)
	fmt.Fprintf(w, `{"job_id": %q}`, job.ID)
}

// handlePullRequest handles PR/MR webhook events.
func (h *WebhookHandler) handlePullRequest(w http.ResponseWriter, r *http.Request, body []byte, matchedForge forge.Forge, prEvent *forge.PullRequestEvent) {
	ctx := r.Context()

	// Look up the repo to get the webhook secret
	repo, err := h.storage.GetRepoByCloneURL(ctx, prEvent.Repo.CloneURL)
	if err != nil {
		if err == storage.ErrNotFound {
			h.log.Warn("repo not configured", "clone_url", prEvent.Repo.CloneURL)
			http.Error(w, "repo not configured", http.StatusNotFound)
			return
		}
		h.log.Error("failed to get repo", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// SECURITY: Verify signature BEFORE any state changes
	if repo.WebhookSecret != "" {
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		_, err = matchedForge.ParsePullRequest(r, repo.WebhookSecret)
		if err != nil {
			h.log.Warn("webhook signature verification failed", "repo", prEvent.Repo.FullName(), "error", err)
			http.Error(w, "signature verification failed", http.StatusUnauthorized)
			return
		}
	}

	// Now safe to sync private flag (after signature verified)
	if repo.Private != prEvent.Repo.Private {
		if err := h.storage.UpdateRepoPrivate(ctx, repo.ID, prEvent.Repo.Private); err != nil {
			h.log.Warn("failed to update repo private flag", "error", err)
		} else {
			h.log.Info("repo private flag updated", "repo", prEvent.Repo.FullName(), "private", prEvent.Repo.Private)
			repo.Private = prEvent.Repo.Private
		}
	}

	// Create job for PR
	job, err := h.createPRJob(ctx, repo, prEvent)
	if err != nil {
		h.log.Error("failed to create job", "error", err)
		http.Error(w, "failed to create job", http.StatusInternalServerError)
		return
	}

	h.log.Info("PR job created",
		"job_id", job.ID,
		"repo", prEvent.Repo.FullName(),
		"pr", prEvent.Number,
		"branch", prEvent.HeadBranch,
		"commit", prEvent.Commit[:8],
		"author", prEvent.Sender,
		"is_fork", prEvent.IsFork,
		"trust_level", job.TrustLevel,
	)

	// Check if private repo can run builds (requires Pro)
	if err := h.checkPrivateRepoAccess(ctx, repo); err != nil {
		h.log.Info("private repo blocked", "repo", prEvent.Repo.FullName(), "error", err)
		// Create a visible error - fail the job with a helpful message
		errMsg := "Private repos require Pro. Get Pro free at cinch.sh/account"
		exitCode := 1
		if updateErr := h.storage.UpdateJobStatus(ctx, job.ID, storage.JobStatusFailed, &exitCode); updateErr != nil {
			h.log.Error("failed to update job status", "error", updateErr)
		}
		// Post failed status to GitHub/GitLab so user sees it
		if statusErr := h.postStatus(ctx, matchedForge, repo, prEvent.Commit, job.ID, forge.StatusError, errMsg); statusErr != nil {
			h.log.Warn("failed to post billing error status", "error", statusErr)
		}
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, `{"job_id": %q, "error": "billing_required"}`, job.ID)
		return
	}

	// Post pending status - different message for fork PRs awaiting contributor CI
	statusMsg := "Build queued"
	if job.Status == storage.JobStatusPendingContributor {
		statusMsg = "Awaiting contributor CI - run `cinch worker` to provide results"
	}
	if err := h.postStatus(ctx, matchedForge, repo, prEvent.Commit, job.ID, forge.StatusPending, statusMsg); err != nil {
		h.log.Warn("failed to post pending status", "error", err)
	}

	// PRs always run the build command (not release)
	command := repo.Build

	// Queue job for dispatch
	h.dispatcher.Enqueue(&QueuedJob{
		Job:      job,
		Repo:     repo,
		Forge:    matchedForge,
		Labels:   repo.Workers, // Worker labels for fan-out
		CloneURL: prEvent.Repo.CloneURL,
		Ref:      "refs/pull/" + fmt.Sprintf("%d", prEvent.Number) + "/head", // PR ref format
		Branch:   prEvent.HeadBranch,
		Config: protocol.JobConfig{
			Command: command,
			Env:     repo.Secrets, // Inject repo secrets as env vars
		},
		CloneToken: repo.ForgeToken,
	})

	w.WriteHeader(http.StatusAccepted)
	fmt.Fprintf(w, `{"job_id": %q}`, job.ID)
}

func (h *WebhookHandler) createPRJob(ctx context.Context, repo *storage.Repo, event *forge.PullRequestEvent) (*storage.Job, error) {
	prNum := event.Number

	// Determine trust level for PR author
	trustLevel := h.determinePRTrustLevel(repo, event)

	// Fork PRs from external contributors start in pending_contributor status
	status := storage.JobStatusPending
	if event.IsFork && trustLevel == storage.TrustExternal {
		status = storage.JobStatusPendingContributor
	}

	job := &storage.Job{
		ID:           generateJobID(),
		RepoID:       repo.ID,
		Commit:       event.Commit,
		Branch:       event.HeadBranch,
		PRNumber:     &prNum,
		PRBaseBranch: event.BaseBranch,
		Status:       status,
		CreatedAt:    time.Now(),
		Author:       event.Sender,
		TrustLevel:   trustLevel,
		IsFork:       event.IsFork,
	}

	if err := h.storage.CreateJob(ctx, job); err != nil {
		return nil, err
	}

	return job, nil
}

// determinePRTrustLevel determines the trust level for a PR author.
// For now, we treat fork PRs as external and same-repo PRs as collaborator.
// TODO: Query forge API to check actual collaborator status.
func (h *WebhookHandler) determinePRTrustLevel(repo *storage.Repo, event *forge.PullRequestEvent) storage.TrustLevel {
	// Fork PRs are always treated as external (even if from a collaborator)
	if event.IsFork {
		return storage.TrustExternal
	}

	// Same-repo PRs from the owner
	if event.Sender == repo.Owner {
		return storage.TrustOwner
	}

	// Same-repo PRs from non-owners are collaborators (they have push access)
	return storage.TrustCollaborator
}

func (h *WebhookHandler) createJob(ctx context.Context, repo *storage.Repo, event *forge.PushEvent) (*storage.Job, error) {
	// Determine trust level for push author
	trustLevel := h.determinePushTrustLevel(repo, event)

	job := &storage.Job{
		ID:         generateJobID(),
		RepoID:     repo.ID,
		Commit:     event.Commit,
		Branch:     event.Branch,
		Tag:        event.Tag,
		Status:     storage.JobStatusPending,
		CreatedAt:  time.Now(),
		Author:     event.Sender,
		TrustLevel: trustLevel,
		IsFork:     false, // Push events are never from forks
	}

	if err := h.storage.CreateJob(ctx, job); err != nil {
		return nil, err
	}

	return job, nil
}

// determinePushTrustLevel determines the trust level for a push author.
// Anyone who can push to the repo is at least a collaborator.
func (h *WebhookHandler) determinePushTrustLevel(repo *storage.Repo, event *forge.PushEvent) storage.TrustLevel {
	if event.Sender == repo.Owner {
		return storage.TrustOwner
	}
	// If they can push, they're a collaborator
	return storage.TrustCollaborator
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

// checkPrivateRepoAccess checks if a private repo can trigger builds.
// Returns an error if access should be denied (private repos require Pro).
func (h *WebhookHandler) checkPrivateRepoAccess(ctx context.Context, repo *storage.Repo) error {
	if !repo.Private {
		return nil // Public repos always allowed
	}

	// Check if org has Team Pro billing
	billing, err := h.storage.GetOrgBilling(ctx, repo.ForgeType, repo.Owner)
	if err == nil && billing != nil && billing.Status == "active" {
		return nil // Org has active billing
	}

	// For MVP, we don't have user-to-repo linking, so private repos require org billing
	// In the future, we could also check if repo owner (as Cinch user) has personal Pro
	return fmt.Errorf("private repos require Pro. Get Pro at cinch.sh/account")
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
		var logText string
		if h.logStore != nil {
			if reader, err := h.logStore.GetLogs(ctx, jobID); err == nil {
				defer reader.Close()
				scanner := bufio.NewScanner(reader)
				for scanner.Scan() {
					var entry logstore.LogEntry
					if err := json.Unmarshal(scanner.Bytes(), &entry); err == nil {
						logText += entry.Data
					}
				}
			}
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
