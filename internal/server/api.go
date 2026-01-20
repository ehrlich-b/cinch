package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ehrlich-b/cinch/internal/storage"
	"golang.org/x/crypto/sha3"
)

// APIHandler handles HTTP API requests.
type APIHandler struct {
	storage storage.Storage
	hub     *Hub
	log     *slog.Logger
}

// NewAPIHandler creates a new API handler.
func NewAPIHandler(store storage.Storage, hub *Hub, log *slog.Logger) *APIHandler {
	if log == nil {
		log = slog.Default()
	}
	return &APIHandler{
		storage: store,
		hub:     hub,
		log:     log,
	}
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

	// Repos
	case path == "/repos" && r.Method == http.MethodGet:
		h.listRepos(w, r)
	case path == "/repos" && r.Method == http.MethodPost:
		h.createRepo(w, r)
	case strings.HasPrefix(path, "/repos/"):
		repoID := strings.TrimPrefix(path, "/repos/")
		switch r.Method {
		case http.MethodGet:
			h.getRepo(w, r, repoID)
		case http.MethodDelete:
			h.deleteRepo(w, r, repoID)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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

	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// --- Jobs ---

type jobResponse struct {
	ID         string     `json:"id"`
	RepoID     string     `json:"repo_id"`
	Commit     string     `json:"commit"`
	Branch     string     `json:"branch"`
	Status     string     `json:"status"`
	ExitCode   *int       `json:"exit_code,omitempty"`
	WorkerID   *string    `json:"worker_id,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

func jobToResponse(j *storage.Job) jobResponse {
	return jobResponse{
		ID:         j.ID,
		RepoID:     j.RepoID,
		Commit:     j.Commit,
		Branch:     j.Branch,
		Status:     string(j.Status),
		ExitCode:   j.ExitCode,
		WorkerID:   j.WorkerID,
		StartedAt:  j.StartedAt,
		FinishedAt: j.FinishedAt,
		CreatedAt:  j.CreatedAt,
	}
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

	resp := make([]jobResponse, len(jobs))
	for i, j := range jobs {
		resp[i] = jobToResponse(j)
	}

	h.writeJSON(w, resp)
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
	logs, err := h.storage.GetLogs(r.Context(), jobID)
	if err != nil {
		h.log.Error("failed to get logs", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	type logResponse struct {
		Stream    string    `json:"stream"`
		Data      string    `json:"data"`
		CreatedAt time.Time `json:"created_at"`
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

// --- Workers ---

type workerResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Labels    []string  `json:"labels"`
	Status    string    `json:"status"`
	LastSeen  time.Time `json:"last_seen"`
	CreatedAt time.Time `json:"created_at"`
	// Live info from hub
	Connected   bool     `json:"connected"`
	ActiveJobs  []string `json:"active_jobs,omitempty"`
	Concurrency int      `json:"concurrency,omitempty"`
}

func (h *APIHandler) listWorkers(w http.ResponseWriter, r *http.Request) {
	workers, err := h.storage.ListWorkers(r.Context())
	if err != nil {
		h.log.Error("failed to list workers", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := make([]workerResponse, len(workers))
	for i, wk := range workers {
		resp[i] = workerResponse{
			ID:        wk.ID,
			Name:      wk.Name,
			Labels:    wk.Labels,
			Status:    string(wk.Status),
			LastSeen:  wk.LastSeen,
			CreatedAt: wk.CreatedAt,
		}

		// Add live info from hub if connected
		if h.hub != nil {
			if conn := h.hub.Get(wk.ID); conn != nil {
				resp[i].Connected = true
				resp[i].ActiveJobs = conn.ActiveJobs
				resp[i].Concurrency = conn.Capabilities.Concurrency
			}
		}
	}

	h.writeJSON(w, resp)
}

// --- Repos ---

type repoResponse struct {
	ID            string    `json:"id"`
	ForgeType     string    `json:"forge_type"`
	Owner         string    `json:"owner"`
	Name          string    `json:"name"`
	CloneURL      string    `json:"clone_url"`
	HTMLURL       string    `json:"html_url,omitempty"`
	WebhookSecret string    `json:"webhook_secret,omitempty"`
	Command       string    `json:"command"`
	CreatedAt     time.Time `json:"created_at"`
}

type createRepoRequest struct {
	ForgeType  string `json:"forge_type"`
	Owner      string `json:"owner"`
	Name       string `json:"name"`
	CloneURL   string `json:"clone_url"`
	HTMLURL    string `json:"html_url"`
	ForgeToken string `json:"forge_token"`
	Command    string `json:"command"`
}

func (h *APIHandler) listRepos(w http.ResponseWriter, r *http.Request) {
	repos, err := h.storage.ListRepos(r.Context())
	if err != nil {
		h.log.Error("failed to list repos", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp := make([]repoResponse, len(repos))
	for i, repo := range repos {
		resp[i] = repoResponse{
			ID:        repo.ID,
			ForgeType: string(repo.ForgeType),
			Owner:     repo.Owner,
			Name:      repo.Name,
			CloneURL:  repo.CloneURL,
			HTMLURL:   repo.HTMLURL,
			Command:   repo.Command,
			CreatedAt: repo.CreatedAt,
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
		CloneURL:      repo.CloneURL,
		HTMLURL:       repo.HTMLURL,
		WebhookSecret: repo.WebhookSecret, // Include for admin viewing
		Command:       repo.Command,
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
		Command:       req.Command,
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
		Command:       repo.Command,
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

// --- Helpers ---

func (h *APIHandler) writeJSON(w http.ResponseWriter, v interface{}) {
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
