package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/ehrlich-b/cinch/internal/storage"
)

// BadgeHandler serves build status badges.
// SVG requests redirect to shields.io, JSON provides the data.
type BadgeHandler struct {
	store   storage.Storage
	log     *slog.Logger
	baseURL string // e.g. "https://cinch.sh"
}

// NewBadgeHandler creates a new badge handler.
func NewBadgeHandler(store storage.Storage, log *slog.Logger, baseURL string) *BadgeHandler {
	return &BadgeHandler{
		store:   store,
		log:     log,
		baseURL: strings.TrimSuffix(baseURL, "/"),
	}
}

// ShieldsEndpoint is the JSON response format for shields.io endpoint badges.
// See: https://shields.io/badges/endpoint-badge
type ShieldsEndpoint struct {
	SchemaVersion int    `json:"schemaVersion"`
	Label         string `json:"label"`
	Message       string `json:"message"`
	Color         string `json:"color"`
}

// ServeHTTP handles badge requests.
// JSON: /api/badge/{forge}/{owner}/{repo}.json -> returns shields.io endpoint JSON
// SVG: /badge/{forge}/{owner}/{repo}.svg -> redirects to shields.io
func (h *BadgeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	if strings.HasPrefix(path, "/api/badge/") {
		h.serveJSON(w, r)
		return
	}

	if strings.HasPrefix(path, "/badge/") {
		h.redirectToShields(w, r)
		return
	}

	http.NotFound(w, r)
}

// serveJSON serves the shields.io endpoint JSON.
// Path: /api/badge/{forge}/{owner}/{repo}.json
func (h *BadgeHandler) serveJSON(w http.ResponseWriter, r *http.Request) {
	forge, owner, repo, ok := h.parsePath(r.URL.Path, "/api/badge/", ".json")
	if !ok {
		http.Error(w, "invalid path: expected /api/badge/{forge}/{owner}/{repo}.json", http.StatusBadRequest)
		return
	}

	branch := r.URL.Query().Get("branch")
	status := h.getRepoStatus(r.Context(), forge, owner, repo, branch)

	// Build shields.io endpoint response
	resp := ShieldsEndpoint{
		SchemaVersion: 1,
		Label:         "build",
		Message:       status,
		Color:         statusToColor(status),
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60, s-maxage=60")
	_ = json.NewEncoder(w).Encode(resp)
}

// redirectToShields redirects .svg requests to shields.io.
// Path: /badge/{forge}/{owner}/{repo}.svg
func (h *BadgeHandler) redirectToShields(w http.ResponseWriter, r *http.Request) {
	forge, owner, repo, ok := h.parsePath(r.URL.Path, "/badge/", ".svg")
	if !ok {
		http.Error(w, "invalid path: expected /badge/{forge}/{owner}/{repo}.svg", http.StatusBadRequest)
		return
	}

	// Build our JSON endpoint URL
	jsonURL := fmt.Sprintf("%s/api/badge/%s/%s/%s.json", h.baseURL, forge, owner, repo)
	if branch := r.URL.Query().Get("branch"); branch != "" {
		jsonURL += "?branch=" + url.QueryEscape(branch)
	}

	// Build shields.io URL
	shieldsURL := fmt.Sprintf("https://img.shields.io/endpoint?url=%s&style=flat", url.QueryEscape(jsonURL))

	http.Redirect(w, r, shieldsURL, http.StatusFound)
}

// parsePath extracts forge, owner, repo from paths like /prefix/{forge}/{owner}/{repo}.suffix
func (h *BadgeHandler) parsePath(path, prefix, suffix string) (forge, owner, repo string, ok bool) {
	path = strings.TrimPrefix(path, prefix)
	path = strings.TrimSuffix(path, suffix)
	parts := strings.Split(path, "/")

	// Expect: github.com/owner/repo or forge/owner/repo
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}

	return parts[0], parts[1], parts[2], true
}

func (h *BadgeHandler) getRepoStatus(ctx context.Context, _, owner, repo, branch string) string {
	// Find repo by owner/name
	repos, err := h.store.ListRepos(ctx)
	if err != nil {
		h.log.Error("failed to list repos", "error", err)
		return "unknown"
	}

	var repoID string
	for _, r := range repos {
		if r.Owner == owner && r.Name == repo {
			repoID = r.ID
			break
		}
	}

	if repoID == "" {
		return "unknown"
	}

	// Get latest job for this repo
	filter := storage.JobFilter{
		RepoID: repoID,
		Limit:  1,
	}
	if branch != "" {
		filter.Branch = branch
	}

	jobs, err := h.store.ListJobs(ctx, filter)
	if err != nil {
		h.log.Error("failed to list jobs", "error", err)
		return "unknown"
	}

	if len(jobs) == 0 {
		return "unknown"
	}

	// Map job status to badge status
	switch jobs[0].Status {
	case storage.JobStatusSuccess:
		return "passing"
	case storage.JobStatusFailed, storage.JobStatusError:
		return "failing"
	case storage.JobStatusRunning, storage.JobStatusQueued, storage.JobStatusPending:
		return "running"
	default:
		return "unknown"
	}
}

func statusToColor(status string) string {
	switch status {
	case "passing":
		return "brightgreen"
	case "failing":
		return "red"
	case "running":
		return "yellow"
	default:
		return "lightgrey"
	}
}
