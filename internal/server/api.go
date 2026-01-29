package server

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ehrlich-b/cinch/internal/logstore"
	"github.com/ehrlich-b/cinch/internal/storage"
	"golang.org/x/crypto/sha3"
)

// APIHandler handles HTTP API requests.
type APIHandler struct {
	storage    storage.Storage
	logStore   logstore.LogStore
	hub        *Hub
	auth       *AuthHandler
	dispatcher *Dispatcher
	githubApp  *GitHubAppHandler
	wsHandler  *WSHandler
	log        *slog.Logger
}

// NewAPIHandler creates a new API handler.
func NewAPIHandler(store storage.Storage, hub *Hub, auth *AuthHandler, log *slog.Logger) *APIHandler {
	if log == nil {
		log = slog.Default()
	}
	return &APIHandler{
		storage: store,
		hub:     hub,
		auth:    auth,
		log:     log,
	}
}

// SetLogStore sets the log store for retrieving job logs.
func (h *APIHandler) SetLogStore(ls logstore.LogStore) {
	h.logStore = ls
}

// SetDispatcher sets the dispatcher for job re-running.
func (h *APIHandler) SetDispatcher(d *Dispatcher) {
	h.dispatcher = d
}

// SetGitHubApp sets the GitHub App handler for installation token regeneration.
func (h *APIHandler) SetGitHubApp(app *GitHubAppHandler) {
	h.githubApp = app
}

// SetWSHandler sets the WebSocket handler for worker control.
func (h *APIHandler) SetWSHandler(ws *WSHandler) {
	h.wsHandler = ws
}

// ServeHTTP routes API requests.
func (h *APIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api")
	path = strings.TrimSuffix(path, "/")

	// Route based on path and method
	switch {
	// Jobs
	case path == "/jobs" && r.Method == http.MethodGet:
		h.listJobs(w, r)
	case strings.HasPrefix(path, "/jobs/") && strings.HasSuffix(path, "/logs"):
		jobID := strings.TrimSuffix(strings.TrimPrefix(path, "/jobs/"), "/logs")
		if r.Method == http.MethodGet {
			h.getJobLogs(w, r, jobID)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case strings.HasPrefix(path, "/jobs/") && strings.HasSuffix(path, "/run"):
		jobID := strings.TrimSuffix(strings.TrimPrefix(path, "/jobs/"), "/run")
		if r.Method == http.MethodPost {
			h.runJob(w, r, jobID)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case strings.HasPrefix(path, "/jobs/"):
		jobID := strings.TrimPrefix(path, "/jobs/")
		if r.Method == http.MethodGet {
			h.getJob(w, r, jobID)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}

	// Workers
	case path == "/workers" && r.Method == http.MethodGet:
		h.listWorkers(w, r)
	case strings.HasPrefix(path, "/workers/") && strings.HasSuffix(path, "/jobs"):
		workerID := strings.TrimSuffix(strings.TrimPrefix(path, "/workers/"), "/jobs")
		if r.Method == http.MethodGet {
			h.listWorkerJobs(w, r, workerID)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case strings.HasPrefix(path, "/workers/") && strings.HasSuffix(path, "/drain"):
		workerID := strings.TrimSuffix(strings.TrimPrefix(path, "/workers/"), "/drain")
		if r.Method == http.MethodPost {
			h.drainWorker(w, r, workerID)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case strings.HasPrefix(path, "/workers/") && strings.HasSuffix(path, "/disconnect"):
		workerID := strings.TrimSuffix(strings.TrimPrefix(path, "/workers/"), "/disconnect")
		if r.Method == http.MethodPost {
			h.disconnectWorker(w, r, workerID)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}

	// Repos
	case path == "/repos" && r.Method == http.MethodGet:
		h.listRepos(w, r)
	case path == "/repos" && r.Method == http.MethodPost:
		h.createRepo(w, r)
	case strings.HasPrefix(path, "/repos/"):
		repoPath := strings.TrimPrefix(path, "/repos/")
		// Check if this is a forge/owner/repo path (forge contains a dot like github.com)
		parts := strings.SplitN(repoPath, "/", 4)
		if len(parts) >= 3 && strings.Contains(parts[0], ".") {
			// This is /repos/{forge}/{owner}/{repo}[/jobs]
			forge, owner, repoName := parts[0], parts[1], parts[2]
			if len(parts) == 4 && parts[3] == "jobs" {
				// /repos/{forge}/{owner}/{repo}/jobs
				if r.Method == http.MethodGet {
					h.listRepoJobs(w, r, forge, owner, repoName)
				} else {
					http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				}
			} else if len(parts) == 3 {
				// /repos/{forge}/{owner}/{repo}
				if r.Method == http.MethodGet {
					h.getRepoByPath(w, r, forge, owner, repoName)
				} else {
					http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				}
			} else {
				http.Error(w, "not found", http.StatusNotFound)
			}
		} else {
			// This is /repos/{id} (legacy)
			repoID := repoPath
			switch r.Method {
			case http.MethodGet:
				h.getRepo(w, r, repoID)
			case http.MethodDelete:
				h.deleteRepo(w, r, repoID)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		}

	// Tokens
	case path == "/tokens" && r.Method == http.MethodGet:
		h.listTokens(w, r)
	case path == "/tokens" && r.Method == http.MethodPost:
		h.createToken(w, r)
	case strings.HasPrefix(path, "/tokens/"):
		tokenID := strings.TrimPrefix(path, "/tokens/")
		if r.Method == http.MethodDelete {
			h.revokeToken(w, r, tokenID)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}

	// Give me pro (free during beta)
	case path == "/give-me-pro" && r.Method == http.MethodPost:
		h.giveMePro(w, r)

	// User/Account
	case path == "/user" && r.Method == http.MethodGet:
		h.getUser(w, r)
	case path == "/user" && r.Method == http.MethodDelete:
		h.deleteUser(w, r)
	case strings.HasPrefix(path, "/user/forges/"):
		forgeType := strings.TrimPrefix(path, "/user/forges/")
		if r.Method == http.MethodDelete {
			h.disconnectForge(w, r, forgeType)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}

	// Forge connect (self-hosted instances)
	case path == "/forge/connect" && r.Method == http.MethodPost:
		h.connectForge(w, r)

	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// --- Jobs ---

type jobResponse struct {
	ID           string     `json:"id"`
	RepoID       string     `json:"repo_id"`
	Repo         string     `json:"repo"` // repo name for frontend display
	Commit       string     `json:"commit"`
	Branch       string     `json:"branch"`
	Tag          string     `json:"tag,omitempty"`
	PRNumber     *int       `json:"pr_number,omitempty"`
	PRBaseBranch string     `json:"pr_base_branch,omitempty"`
	Status       string     `json:"status"`
	Duration     *int64     `json:"duration,omitempty"` // duration in ms
	ExitCode     *int       `json:"exit_code,omitempty"`
	WorkerID     *string    `json:"worker_id,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

func jobToResponse(j *storage.Job) jobResponse {
	resp := jobResponse{
		ID:           j.ID,
		RepoID:       j.RepoID,
		Commit:       j.Commit,
		Branch:       j.Branch,
		Tag:          j.Tag,
		PRNumber:     j.PRNumber,
		PRBaseBranch: j.PRBaseBranch,
		Status:       string(j.Status),
		ExitCode:     j.ExitCode,
		WorkerID:     j.WorkerID,
		StartedAt:    j.StartedAt,
		FinishedAt:   j.FinishedAt,
		CreatedAt:    j.CreatedAt,
	}
	// Calculate duration if job finished
	if j.StartedAt != nil && j.FinishedAt != nil {
		d := j.FinishedAt.Sub(*j.StartedAt).Milliseconds()
		resp.Duration = &d
	}
	return resp
}

func (h *APIHandler) listJobs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filter := storage.JobFilter{
		RepoID: q.Get("repo_id"),
		Status: storage.JobStatus(q.Get("status")),
		Branch: q.Get("branch"),
		Limit:  50, // default
	}

	if limit := q.Get("limit"); limit != "" {
		if n, err := strconv.Atoi(limit); err == nil && n > 0 && n <= 100 {
			filter.Limit = n
		}
	}
	if offset := q.Get("offset"); offset != "" {
		if n, err := strconv.Atoi(offset); err == nil && n >= 0 {
			filter.Offset = n
		}
	}

	jobs, err := h.storage.ListJobs(r.Context(), filter)
	if err != nil {
		h.log.Error("failed to list jobs", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Build response with repo names
	resp := make([]jobResponse, len(jobs))
	for i, j := range jobs {
		resp[i] = jobToResponse(j)
		// Try to get repo name
		if repo, err := h.storage.GetRepo(r.Context(), j.RepoID); err == nil {
			resp[i].Repo = repo.Owner + "/" + repo.Name
		}
	}

	h.writeJSON(w, map[string]any{"jobs": resp})
}

func (h *APIHandler) getJob(w http.ResponseWriter, r *http.Request, jobID string) {
	job, err := h.storage.GetJob(r.Context(), jobID)
	if err != nil {
		if err == storage.ErrNotFound {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		h.log.Error("failed to get job", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, jobToResponse(job))
}

func (h *APIHandler) getJobLogs(w http.ResponseWriter, r *http.Request, jobID string) {
	type logResponse struct {
		Stream    string    `json:"stream"`
		Data      string    `json:"data"`
		CreatedAt time.Time `json:"created_at"`
	}

	// Use logStore if available
	if h.logStore != nil {
		reader, err := h.logStore.GetLogs(r.Context(), jobID)
		if err != nil {
			h.log.Error("failed to get logs", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer reader.Close()

		// Read NDJSON and convert to response format
		var resp []logResponse
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			var entry logstore.LogEntry
			if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
				continue
			}
			resp = append(resp, logResponse{
				Stream:    entry.Stream,
				Data:      entry.Data,
				CreatedAt: entry.Time,
			})
		}
		if err := scanner.Err(); err != nil {
			h.log.Error("failed to read logs", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		h.writeJSON(w, resp)
		return
	}

	// Fallback to direct storage access
	logs, err := h.storage.GetLogs(r.Context(), jobID)
	if err != nil {
		h.log.Error("failed to get logs", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := make([]logResponse, len(logs))
	for i, l := range logs {
		resp[i] = logResponse{
			Stream:    l.Stream,
			Data:      l.Data,
			CreatedAt: l.CreatedAt,
		}
	}

	h.writeJSON(w, resp)
}

// runJob handles POST /api/jobs/{id}/run
// For failed/success/error/cancelled jobs: creates a new job with same params (retry)
// For pending_contributor jobs: approves and queues the existing job
func (h *APIHandler) runJob(w http.ResponseWriter, r *http.Request, jobID string) {
	ctx := r.Context()

	// Check auth
	username := h.auth.GetUser(r)
	if username == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Get original job
	job, err := h.storage.GetJob(ctx, jobID)
	if err != nil {
		if err == storage.ErrNotFound {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		h.log.Error("failed to get job", "job_id", jobID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Get repo
	repo, err := h.storage.GetRepo(ctx, job.RepoID)
	if err != nil {
		h.log.Error("failed to get repo", "repo_id", job.RepoID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Check dispatcher is available
	if h.dispatcher == nil {
		http.Error(w, "job dispatch not available", http.StatusServiceUnavailable)
		return
	}

	var newJobID string

	switch job.Status {
	case storage.JobStatusPendingContributor:
		// Approve and queue the existing job
		if err := h.storage.ApproveJob(ctx, jobID, username); err != nil {
			h.log.Error("failed to approve job", "job_id", jobID, "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// Reload job with updated status
		job, _ = h.storage.GetJob(ctx, jobID)
		newJobID = jobID

		h.log.Info("job approved", "job_id", jobID, "approved_by", username)

	case storage.JobStatusFailed, storage.JobStatusSuccess, storage.JobStatusError, storage.JobStatusCancelled:
		// Create a new job (retry)
		newJob := &storage.Job{
			ID:             fmt.Sprintf("j_%d", time.Now().UnixNano()),
			RepoID:         job.RepoID,
			Commit:         job.Commit,
			Branch:         job.Branch,
			Tag:            job.Tag,
			PRNumber:       job.PRNumber,
			PRBaseBranch:   job.PRBaseBranch,
			Status:         storage.JobStatusPending,
			InstallationID: job.InstallationID,
			CreatedAt:      time.Now(),
			Author:         username, // Current user is the one retrying
			TrustLevel:     storage.TrustCollaborator,
			IsFork:         false, // Retries aren't from forks
		}

		if err := h.storage.CreateJob(ctx, newJob); err != nil {
			h.log.Error("failed to create retry job", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		job = newJob
		newJobID = newJob.ID

		h.log.Info("job retry created", "job_id", newJobID, "original_job_id", jobID, "user", username)

	case storage.JobStatusPending, storage.JobStatusQueued, storage.JobStatusRunning:
		http.Error(w, "job already in progress", http.StatusConflict)
		return

	default:
		http.Error(w, "job cannot be run", http.StatusBadRequest)
		return
	}

	// Build clone token
	var cloneToken string
	var installationID int64

	if job.InstallationID != nil && h.githubApp != nil && h.githubApp.IsConfigured() {
		// GitHub App job - get fresh token
		installationID = *job.InstallationID
		token, err := h.githubApp.GetInstallationToken(installationID)
		if err != nil {
			h.log.Error("failed to get installation token", "job_id", newJobID, "error", err)
			http.Error(w, "failed to get clone token", http.StatusInternalServerError)
			return
		}
		cloneToken = token
	} else {
		// Non-GitHub App - use stored forge token
		cloneToken = repo.ForgeToken
	}

	// Construct ref from branch or tag
	var ref string
	if job.PRNumber != nil {
		ref = fmt.Sprintf("refs/pull/%d/head", *job.PRNumber)
	} else if job.Tag != "" {
		ref = "refs/tags/" + job.Tag
	} else if job.Branch != "" {
		ref = "refs/heads/" + job.Branch
	} else {
		h.log.Error("job has no branch or tag", "job_id", newJobID)
		http.Error(w, "job has no branch or tag", http.StatusBadRequest)
		return
	}

	// Queue the job
	queuedJob := &QueuedJob{
		Job:            job,
		Repo:           repo,
		CloneURL:       repo.CloneURL,
		Ref:            ref,
		Branch:         job.Branch,
		Tag:            job.Tag,
		CloneToken:     cloneToken,
		InstallationID: installationID,
		// Config is empty - worker reads .cinch.yaml after clone
	}

	h.dispatcher.Enqueue(queuedJob)

	h.log.Info("job queued for run", "job_id", newJobID)

	w.WriteHeader(http.StatusCreated)
	h.writeJSON(w, map[string]string{"job_id": newJobID})
}

// --- Workers ---

type workerResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Hostname  string    `json:"hostname,omitempty"`
	Labels    []string  `json:"labels"`
	Mode      string    `json:"mode"`
	OwnerName string    `json:"owner_name,omitempty"`
	Version   string    `json:"version,omitempty"`
	Status    string    `json:"status"`
	LastSeen  time.Time `json:"last_seen"`
	CreatedAt time.Time `json:"created_at"`
	// Live info from hub
	Connected  bool     `json:"connected"`
	ActiveJobs []string `json:"active_jobs,omitempty"`
	CurrentJob *string  `json:"currentJob,omitempty"` // First active job for frontend
}

func (h *APIHandler) listWorkers(w http.ResponseWriter, r *http.Request) {
	// Get authenticated user for visibility filtering
	username := h.auth.GetUser(r)

	workers, err := h.storage.ListWorkers(r.Context())
	if err != nil {
		h.log.Error("failed to list workers", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	var resp []workerResponse
	for _, wk := range workers {
		// Get live info from hub if connected (for current owner/mode)
		var conn *WorkerConn
		if h.hub != nil {
			conn = h.hub.Get(wk.ID)
		}

		// Determine owner and mode (prefer live data, fallback to DB)
		ownerName := wk.OwnerName
		mode := wk.Mode
		if conn != nil {
			if conn.OwnerName != "" {
				ownerName = conn.OwnerName
			}
			if conn.Mode != "" {
				mode = string(conn.Mode)
			}
		}
		if mode == "" {
			mode = "personal"
		}

		// Visibility filtering:
		// - Personal workers: only visible to owner
		// - Shared workers: visible to all authenticated users
		if mode == "personal" && ownerName != username {
			continue // Skip - not visible to this user
		}

		wr := workerResponse{
			ID:        wk.ID,
			Name:      wk.Name,
			Labels:    wk.Labels,
			Status:    string(wk.Status),
			LastSeen:  wk.LastSeen,
			CreatedAt: wk.CreatedAt,
			Mode:      mode,
			OwnerName: ownerName,
		}

		// Add live info from hub if connected
		if conn != nil {
			wr.Connected = true
			wr.ActiveJobs = conn.ActiveJobs
			wr.Hostname = conn.Hostname
			wr.Version = conn.Version
			if len(conn.ActiveJobs) > 0 {
				wr.CurrentJob = &conn.ActiveJobs[0]
			}
		}

		resp = append(resp, wr)
	}

	h.writeJSON(w, map[string]any{"workers": resp})
}

// listWorkerJobs returns recent jobs for a specific worker.
func (h *APIHandler) listWorkerJobs(w http.ResponseWriter, r *http.Request, workerID string) {
	q := r.URL.Query()
	limit := 10
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 50 {
			limit = n
		}
	}

	jobs, err := h.storage.ListJobsByWorker(r.Context(), workerID, limit)
	if err != nil {
		h.log.Error("failed to list worker jobs", "worker_id", workerID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := make([]jobResponse, len(jobs))
	for i, j := range jobs {
		resp[i] = jobToResponse(j)
		if repo, err := h.storage.GetRepo(r.Context(), j.RepoID); err == nil {
			resp[i].Repo = repo.Owner + "/" + repo.Name
		}
	}

	h.writeJSON(w, map[string]any{"jobs": resp})
}

// drainWorker sends a drain request to a worker (graceful shutdown).
func (h *APIHandler) drainWorker(w http.ResponseWriter, r *http.Request, workerID string) {
	// Check auth
	username := h.auth.GetUser(r)
	if username == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Get worker from hub
	worker := h.hub.Get(workerID)
	if worker == nil {
		http.Error(w, "worker not found or not connected", http.StatusNotFound)
		return
	}

	// Check permission - only shared worker owner can control
	if worker.Mode != "shared" {
		http.Error(w, "only shared workers can be remotely controlled", http.StatusForbidden)
		return
	}
	if worker.OwnerName != username {
		http.Error(w, "only the worker owner can control this worker", http.StatusForbidden)
		return
	}

	// Parse request body
	var req struct {
		Timeout int    `json:"timeout"`
		Reason  string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Use defaults if no body
		req.Timeout = 300 // 5 minutes default
	}

	// Send drain command
	if h.wsHandler == nil {
		http.Error(w, "worker control not available", http.StatusServiceUnavailable)
		return
	}

	if err := h.wsHandler.SendDrain(workerID, req.Timeout, req.Reason); err != nil {
		h.log.Error("failed to send drain", "worker_id", workerID, "error", err)
		http.Error(w, "failed to send drain command", http.StatusInternalServerError)
		return
	}

	h.log.Info("worker drain requested", "worker_id", workerID, "by", username, "timeout", req.Timeout)
	h.writeJSON(w, map[string]any{"ok": true, "message": "drain command sent"})
}

// disconnectWorker sends a kill request to a worker (force disconnect).
func (h *APIHandler) disconnectWorker(w http.ResponseWriter, r *http.Request, workerID string) {
	// Check auth
	username := h.auth.GetUser(r)
	if username == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Get worker from hub
	worker := h.hub.Get(workerID)
	if worker == nil {
		http.Error(w, "worker not found or not connected", http.StatusNotFound)
		return
	}

	// Check permission - only shared worker owner can control
	if worker.Mode != "shared" {
		http.Error(w, "only shared workers can be remotely controlled", http.StatusForbidden)
		return
	}
	if worker.OwnerName != username {
		http.Error(w, "only the worker owner can control this worker", http.StatusForbidden)
		return
	}

	// Parse request body
	var req struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // Ignore error, reason is optional

	// Send kill command
	if h.wsHandler == nil {
		http.Error(w, "worker control not available", http.StatusServiceUnavailable)
		return
	}

	if err := h.wsHandler.SendKill(workerID, req.Reason); err != nil {
		h.log.Error("failed to send kill", "worker_id", workerID, "error", err)
		http.Error(w, "failed to send disconnect command", http.StatusInternalServerError)
		return
	}

	h.log.Info("worker disconnect requested", "worker_id", workerID, "by", username)
	h.writeJSON(w, map[string]any{"ok": true, "message": "disconnect command sent"})
}

// --- Repos ---

type repoResponse struct {
	ID              string    `json:"id"`
	ForgeType       string    `json:"forge_type"`
	Owner           string    `json:"owner"`
	Name            string    `json:"name"`
	Private         bool      `json:"private,omitempty"`
	CloneURL        string    `json:"clone_url"`
	HTMLURL         string    `json:"html_url,omitempty"`
	WebhookSecret   string    `json:"webhook_secret,omitempty"`
	Build           string    `json:"build"`
	Release         string    `json:"release,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	LatestJobStatus *string   `json:"latest_job_status,omitempty"` // For ?include_status=true
}

type createRepoRequest struct {
	ForgeType  string `json:"forge_type"`
	Owner      string `json:"owner"`
	Name       string `json:"name"`
	CloneURL   string `json:"clone_url"`
	HTMLURL    string `json:"html_url"`
	ForgeToken string `json:"forge_token"`
	Build      string `json:"build"`
	Release    string `json:"release"`
}

func (h *APIHandler) listRepos(w http.ResponseWriter, r *http.Request) {
	repos, err := h.storage.ListRepos(r.Context())
	if err != nil {
		h.log.Error("failed to list repos", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	includeStatus := r.URL.Query().Get("include_status") == "true"

	resp := make([]repoResponse, len(repos))
	for i, repo := range repos {
		htmlURL := repo.HTMLURL
		if htmlURL == "" {
			htmlURL = computeHTMLURL(repo.ForgeType, repo.Owner, repo.Name)
		}
		resp[i] = repoResponse{
			ID:        repo.ID,
			ForgeType: string(repo.ForgeType),
			Owner:     repo.Owner,
			Name:      repo.Name,
			Private:   repo.Private,
			CloneURL:  repo.CloneURL,
			HTMLURL:   htmlURL,
			Build:     repo.Build,
			Release:   repo.Release,
			CreatedAt: repo.CreatedAt,
		}

		// Include latest job status if requested
		if includeStatus {
			jobs, err := h.storage.ListJobs(r.Context(), storage.JobFilter{
				RepoID: repo.ID,
				Limit:  1,
			})
			if err == nil && len(jobs) > 0 {
				status := string(jobs[0].Status)
				resp[i].LatestJobStatus = &status
			}
		}
	}

	h.writeJSON(w, resp)
}

func (h *APIHandler) getRepo(w http.ResponseWriter, r *http.Request, repoID string) {
	repo, err := h.storage.GetRepo(r.Context(), repoID)
	if err != nil {
		if err == storage.ErrNotFound {
			http.Error(w, "repo not found", http.StatusNotFound)
			return
		}
		h.log.Error("failed to get repo", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := repoResponse{
		ID:            repo.ID,
		ForgeType:     string(repo.ForgeType),
		Owner:         repo.Owner,
		Name:          repo.Name,
		Private:       repo.Private,
		CloneURL:      repo.CloneURL,
		HTMLURL:       repo.HTMLURL,
		WebhookSecret: repo.WebhookSecret, // Include for admin viewing
		Build:         repo.Build,
		Release:       repo.Release,
		CreatedAt:     repo.CreatedAt,
	}

	h.writeJSON(w, resp)
}

func (h *APIHandler) createRepo(w http.ResponseWriter, r *http.Request) {
	var req createRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.ForgeType == "" || req.Owner == "" || req.Name == "" || req.CloneURL == "" {
		http.Error(w, "forge_type, owner, name, and clone_url are required", http.StatusBadRequest)
		return
	}

	// Generate webhook secret
	secret, err := generateSecret(32)
	if err != nil {
		h.log.Error("failed to generate webhook secret", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	repo := &storage.Repo{
		ID:            fmt.Sprintf("r_%d", time.Now().UnixNano()),
		ForgeType:     storage.ForgeType(req.ForgeType),
		Owner:         req.Owner,
		Name:          req.Name,
		CloneURL:      req.CloneURL,
		HTMLURL:       req.HTMLURL,
		WebhookSecret: secret,
		ForgeToken:    req.ForgeToken,
		Build:         req.Build,
		Release:       req.Release,
		CreatedAt:     time.Now(),
	}

	if err := h.storage.CreateRepo(r.Context(), repo); err != nil {
		h.log.Error("failed to create repo", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.log.Info("repo created", "repo_id", repo.ID, "clone_url", repo.CloneURL)

	// Return full repo with webhook secret
	resp := repoResponse{
		ID:            repo.ID,
		ForgeType:     string(repo.ForgeType),
		Owner:         repo.Owner,
		Name:          repo.Name,
		CloneURL:      repo.CloneURL,
		HTMLURL:       repo.HTMLURL,
		WebhookSecret: repo.WebhookSecret,
		Build:         repo.Build,
		Release:       repo.Release,
		CreatedAt:     repo.CreatedAt,
	}

	w.WriteHeader(http.StatusCreated)
	h.writeJSON(w, resp)
}

func (h *APIHandler) deleteRepo(w http.ResponseWriter, r *http.Request, repoID string) {
	if err := h.storage.DeleteRepo(r.Context(), repoID); err != nil {
		if err == storage.ErrNotFound {
			http.Error(w, "repo not found", http.StatusNotFound)
			return
		}
		h.log.Error("failed to delete repo", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.log.Info("repo deleted", "repo_id", repoID)
	w.WriteHeader(http.StatusNoContent)
}

// forgeDomainToType converts a domain like "github.com" to a forge type like "github"
func forgeDomainToType(domain string) string {
	switch domain {
	case "github.com":
		return "github"
	case "gitlab.com":
		return "gitlab"
	case "codeberg.org":
		return "forgejo"
	case "gitea.com":
		return "gitea"
	default:
		// For self-hosted instances, try to infer from domain
		if strings.Contains(domain, "gitlab") {
			return "gitlab"
		}
		if strings.Contains(domain, "gitea") {
			return "gitea"
		}
		if strings.Contains(domain, "forgejo") || strings.Contains(domain, "codeberg") {
			return "forgejo"
		}
		return domain
	}
}

// checkRepoAccess verifies user has access to a private repo
// Returns true if access is allowed, false otherwise (and writes error response)
func (h *APIHandler) checkRepoAccess(w http.ResponseWriter, r *http.Request, repo *storage.Repo) bool {
	if !repo.Private {
		return true // Public repos are always accessible
	}

	// Private repo - check auth
	username := h.auth.GetUser(r)
	if username == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}

	// Check if user has connected the required forge
	user, err := h.storage.GetUserByName(r.Context(), username)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}

	// Check if user has the required forge connected
	hasAccess := false
	switch repo.ForgeType {
	case storage.ForgeTypeGitHub:
		hasAccess = !user.GitHubConnectedAt.IsZero()
	case storage.ForgeTypeGitLab:
		hasAccess = user.GitLabCredentials != ""
	case storage.ForgeTypeForgejo, storage.ForgeTypeGitea:
		hasAccess = user.ForgejoCredentials != ""
	}

	if !hasAccess {
		http.Error(w, "forbidden: connect your "+string(repo.ForgeType)+" account to access this repo", http.StatusForbidden)
		return false
	}

	return true
}

// Response type for per-repo endpoint with latest job
type repoWithStatusResponse struct {
	ID        string       `json:"id"`
	ForgeType string       `json:"forge_type"`
	Owner     string       `json:"owner"`
	Name      string       `json:"name"`
	Private   bool         `json:"private"`
	CloneURL  string       `json:"clone_url"`
	HTMLURL   string       `json:"html_url,omitempty"`
	Build     string       `json:"build"`
	Release   string       `json:"release,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
	LatestJob *jobResponse `json:"latest_job,omitempty"`
}

func (h *APIHandler) getRepoByPath(w http.ResponseWriter, r *http.Request, forge, owner, repoName string) {
	forgeType := forgeDomainToType(forge)

	repo, err := h.storage.GetRepoByOwnerName(r.Context(), forgeType, owner, repoName)
	if err != nil {
		if err == storage.ErrNotFound {
			http.Error(w, "repo not found", http.StatusNotFound)
			return
		}
		h.log.Error("failed to get repo", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Check access for private repos
	if !h.checkRepoAccess(w, r, repo) {
		return
	}

	htmlURL := repo.HTMLURL
	if htmlURL == "" {
		htmlURL = computeHTMLURL(repo.ForgeType, repo.Owner, repo.Name)
	}

	resp := repoWithStatusResponse{
		ID:        repo.ID,
		ForgeType: string(repo.ForgeType),
		Owner:     repo.Owner,
		Name:      repo.Name,
		Private:   repo.Private,
		CloneURL:  repo.CloneURL,
		HTMLURL:   htmlURL,
		Build:     repo.Build,
		Release:   repo.Release,
		CreatedAt: repo.CreatedAt,
	}

	// Get latest job
	jobs, err := h.storage.ListJobs(r.Context(), storage.JobFilter{
		RepoID: repo.ID,
		Limit:  1,
	})
	if err == nil && len(jobs) > 0 {
		jr := jobToResponse(jobs[0])
		jr.Repo = repo.Owner + "/" + repo.Name
		resp.LatestJob = &jr
	}

	h.writeJSON(w, resp)
}

func (h *APIHandler) listRepoJobs(w http.ResponseWriter, r *http.Request, forge, owner, repoName string) {
	forgeType := forgeDomainToType(forge)

	repo, err := h.storage.GetRepoByOwnerName(r.Context(), forgeType, owner, repoName)
	if err != nil {
		if err == storage.ErrNotFound {
			http.Error(w, "repo not found", http.StatusNotFound)
			return
		}
		h.log.Error("failed to get repo", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Check access for private repos
	if !h.checkRepoAccess(w, r, repo) {
		return
	}

	q := r.URL.Query()
	filter := storage.JobFilter{
		RepoID: repo.ID,
		Status: storage.JobStatus(q.Get("status")),
		Branch: q.Get("branch"),
		Limit:  50,
	}

	if limit := q.Get("limit"); limit != "" {
		if n, err := strconv.Atoi(limit); err == nil && n > 0 && n <= 100 {
			filter.Limit = n
		}
	}
	if offset := q.Get("offset"); offset != "" {
		if n, err := strconv.Atoi(offset); err == nil && n >= 0 {
			filter.Offset = n
		}
	}

	jobs, err := h.storage.ListJobs(r.Context(), filter)
	if err != nil {
		h.log.Error("failed to list jobs", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := make([]jobResponse, len(jobs))
	for i, j := range jobs {
		resp[i] = jobToResponse(j)
		resp[i].Repo = repo.Owner + "/" + repo.Name
	}

	h.writeJSON(w, map[string]any{"jobs": resp})
}

// --- Tokens ---

type tokenResponse struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	WorkerID  *string    `json:"worker_id,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

type createTokenRequest struct {
	Name string `json:"name"`
}

type createTokenResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Token     string    `json:"token"` // Plain text token (only shown once)
	CreatedAt time.Time `json:"created_at"`
}

func (h *APIHandler) listTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := h.storage.ListTokens(r.Context())
	if err != nil {
		h.log.Error("failed to list tokens", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := make([]tokenResponse, len(tokens))
	for i, t := range tokens {
		resp[i] = tokenResponse{
			ID:        t.ID,
			Name:      t.Name,
			WorkerID:  t.WorkerID,
			CreatedAt: t.CreatedAt,
			RevokedAt: t.RevokedAt,
		}
	}

	h.writeJSON(w, resp)
}

func (h *APIHandler) createToken(w http.ResponseWriter, r *http.Request) {
	var req createTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	// Generate random token
	plainToken, err := generateSecret(32)
	if err != nil {
		h.log.Error("failed to generate token", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Hash for storage (SHA3-256 to match ws.go)
	hasher := sha3.New256()
	hasher.Write([]byte(plainToken))
	hashHex := hex.EncodeToString(hasher.Sum(nil))

	token := &storage.Token{
		ID:        fmt.Sprintf("t_%d", time.Now().UnixNano()),
		Name:      req.Name,
		Hash:      hashHex,
		CreatedAt: time.Now(),
	}

	if err := h.storage.CreateToken(r.Context(), token); err != nil {
		h.log.Error("failed to create token", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.log.Info("token created", "token_id", token.ID, "name", token.Name)

	resp := createTokenResponse{
		ID:        token.ID,
		Name:      token.Name,
		Token:     plainToken,
		CreatedAt: token.CreatedAt,
	}

	w.WriteHeader(http.StatusCreated)
	h.writeJSON(w, resp)
}

func (h *APIHandler) revokeToken(w http.ResponseWriter, r *http.Request, tokenID string) {
	if err := h.storage.RevokeToken(r.Context(), tokenID); err != nil {
		if err == storage.ErrNotFound {
			http.Error(w, "token not found", http.StatusNotFound)
			return
		}
		h.log.Error("failed to revoke token", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.log.Info("token revoked", "token_id", tokenID)
	w.WriteHeader(http.StatusNoContent)
}

// --- Give Me Pro (Beta) ---

func (h *APIHandler) giveMePro(w http.ResponseWriter, r *http.Request) {
	// Free during beta - just return success
	h.writeJSON(w, map[string]any{"ok": true, "message": "Pro activated! Free during beta."})
}

// --- User/Account ---

type connectedForge struct {
	Type        string     `json:"type"`
	Username    string     `json:"username,omitempty"`
	ConnectedAt *time.Time `json:"connected_at,omitempty"`
}

type userResponse struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Email           string           `json:"email,omitempty"`
	ConnectedForges []connectedForge `json:"connected_forges"`
	CreatedAt       time.Time        `json:"created_at"`
}

func (h *APIHandler) getUser(w http.ResponseWriter, r *http.Request) {
	// Get username from auth context
	username := h.auth.GetUser(r)
	if username == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	user, err := h.storage.GetUserByName(r.Context(), username)
	if err != nil {
		if err == storage.ErrNotFound {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		h.log.Error("failed to get user", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Build connected forges list
	forges := []connectedForge{}

	// GitHub is always connected if they're logged in (it's the auth provider)
	// We track the connection time if we have it, otherwise use account creation time
	githubConnectedAt := user.GitHubConnectedAt
	if githubConnectedAt.IsZero() {
		githubConnectedAt = user.CreatedAt
	}
	forges = append(forges, connectedForge{
		Type:        "github",
		Username:    user.Name,
		ConnectedAt: &githubConnectedAt,
	})

	// GitLab
	if user.GitLabCredentials != "" {
		forges = append(forges, connectedForge{
			Type:        "gitlab",
			ConnectedAt: &user.GitLabCredentialsAt,
		})
	}

	// Forgejo/Codeberg
	if user.ForgejoCredentials != "" {
		forges = append(forges, connectedForge{
			Type:        "forgejo",
			ConnectedAt: &user.ForgejoCredentialsAt,
		})
	}

	resp := userResponse{
		ID:              user.ID,
		Name:            user.Name,
		Email:           user.Email,
		ConnectedForges: forges,
		CreatedAt:       user.CreatedAt,
	}

	h.writeJSON(w, resp)
}

func (h *APIHandler) disconnectForge(w http.ResponseWriter, r *http.Request, forgeType string) {
	username := h.auth.GetUser(r)
	if username == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	user, err := h.storage.GetUserByName(r.Context(), username)
	if err != nil {
		if err == storage.ErrNotFound {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		h.log.Error("failed to get user", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Count connected forges to detect "last forge" scenario
	connectedCount := 1 // GitHub is always connected
	if user.GitLabCredentials != "" {
		connectedCount++
	}
	if user.ForgejoCredentials != "" {
		connectedCount++
	}

	switch forgeType {
	case "gitlab":
		if user.GitLabCredentials == "" {
			http.Error(w, "GitLab not connected", http.StatusBadRequest)
			return
		}
		if err := h.storage.ClearUserGitLabCredentials(r.Context(), user.ID); err != nil {
			h.log.Error("failed to disconnect GitLab", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		h.log.Info("user disconnected GitLab", "user", username)

	case "forgejo", "codeberg":
		if user.ForgejoCredentials == "" {
			http.Error(w, "Forgejo/Codeberg not connected", http.StatusBadRequest)
			return
		}
		if err := h.storage.ClearUserForgejoCredentials(r.Context(), user.ID); err != nil {
			h.log.Error("failed to disconnect Forgejo", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		h.log.Info("user disconnected Forgejo/Codeberg", "user", username)

	case "github":
		// GitHub is the login provider - warn them
		if connectedCount == 1 {
			// This is the ONLY connected forge
			h.writeJSON(w, map[string]any{
				"error":   "last_login_method",
				"message": "GitHub is your only login method. Disconnecting it will lock you out. Did you mean to delete your account?",
			})
			return
		}
		// For now, don't allow disconnecting GitHub even if other forges connected
		// (we'd need to support login via other forges first)
		http.Error(w, "Cannot disconnect GitHub (it's your login method). Connect another forge as primary first.", http.StatusBadRequest)
		return

	default:
		http.Error(w, "unknown forge type", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// connectForge handles connecting self-hosted forge instances via PAT.
// POST /api/forge/connect
func (h *APIHandler) connectForge(w http.ResponseWriter, r *http.Request) {
	username := h.auth.GetUser(r)
	if username == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Forge    string `json:"forge"`    // "gitlab", "forgejo", "gitea"
		Host     string `json:"host"`     // Base URL (e.g., "https://gitlab.mycompany.com")
		Token    string `json:"token"`    // Personal Access Token
		Username string `json:"username"` // Forge username (for display)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Forge == "" || req.Host == "" || req.Token == "" {
		http.Error(w, "forge, host, and token are required", http.StatusBadRequest)
		return
	}

	// Get user
	user, err := h.storage.GetUserByEmail(r.Context(), username)
	if err != nil {
		h.log.Error("failed to get user", "error", err)
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	// Store credentials based on forge type
	// We use a JSON structure to store host + token together
	creds := map[string]string{
		"type":     "pat",
		"host":     req.Host,
		"token":    req.Token,
		"username": req.Username,
	}
	credsJSON, _ := json.Marshal(creds)

	switch req.Forge {
	case "gitlab":
		if err := h.storage.UpdateUserGitLabCredentials(r.Context(), user.ID, string(credsJSON)); err != nil {
			h.log.Error("failed to save GitLab credentials", "error", err)
			http.Error(w, "failed to save credentials", http.StatusInternalServerError)
			return
		}
		if err := h.storage.UpdateUserGitLabConnected(r.Context(), user.ID); err != nil {
			h.log.Error("failed to mark GitLab connected", "error", err)
		}
		h.log.Info("user connected self-hosted GitLab", "user", username, "host", req.Host)

	case "forgejo", "gitea":
		if err := h.storage.UpdateUserForgejoCredentials(r.Context(), user.ID, string(credsJSON)); err != nil {
			h.log.Error("failed to save Forgejo credentials", "error", err)
			http.Error(w, "failed to save credentials", http.StatusInternalServerError)
			return
		}
		if err := h.storage.UpdateUserForgejoConnected(r.Context(), user.ID); err != nil {
			h.log.Error("failed to mark Forgejo connected", "error", err)
		}
		h.log.Info("user connected self-hosted Forgejo/Gitea", "user", username, "host", req.Host)

	default:
		http.Error(w, "unsupported forge type", http.StatusBadRequest)
		return
	}

	h.writeJSON(w, map[string]any{
		"ok":      true,
		"message": fmt.Sprintf("Connected to %s at %s", req.Forge, req.Host),
	})
}

func (h *APIHandler) deleteUser(w http.ResponseWriter, r *http.Request) {
	username := h.auth.GetUser(r)
	if username == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	user, err := h.storage.GetUserByName(r.Context(), username)
	if err != nil {
		if err == storage.ErrNotFound {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		h.log.Error("failed to get user", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Delete the user
	if err := h.storage.DeleteUser(r.Context(), user.ID); err != nil {
		h.log.Error("failed to delete user", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.log.Info("user deleted account", "user", username, "user_id", user.ID)

	// Return success - frontend should clear cookies and redirect
	h.writeJSON(w, map[string]any{
		"ok":      true,
		"message": "Account deleted successfully",
	})
}

// --- Helpers ---

func (h *APIHandler) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.log.Error("failed to encode response", "error", err)
	}
}

func generateSecret(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// computeHTMLURL constructs a web URL for a repo if not stored in DB
func computeHTMLURL(forgeType storage.ForgeType, owner, name string) string {
	switch forgeType {
	case storage.ForgeTypeGitHub:
		return fmt.Sprintf("https://github.com/%s/%s", owner, name)
	case storage.ForgeTypeGitLab:
		return fmt.Sprintf("https://gitlab.com/%s/%s", owner, name)
	case storage.ForgeTypeGitea:
		return fmt.Sprintf("https://gitea.com/%s/%s", owner, name)
	case storage.ForgeTypeForgejo:
		return fmt.Sprintf("https://codeberg.org/%s/%s", owner, name)
	default:
		return ""
	}
}
