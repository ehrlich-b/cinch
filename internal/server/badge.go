package server

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ehrlich-b/cinch/internal/storage"
)

//go:embed badges/*.svg
var badgeFS embed.FS

// BadgeHandler serves build status badges as SVG.
type BadgeHandler struct {
	store     storage.Storage
	log       *slog.Logger
	templates map[string]*template.Template
}

// NewBadgeHandler creates a new badge handler.
func NewBadgeHandler(store storage.Storage, log *slog.Logger) *BadgeHandler {
	h := &BadgeHandler{
		store:     store,
		log:       log,
		templates: make(map[string]*template.Template),
	}

	// Load all badge templates
	styles := []string{"shields", "flat", "modern", "neon", "electric", "terminal",
		"brutalist", "gradient", "holographic", "pixel", "minimal", "outlined"}

	for _, style := range styles {
		data, err := badgeFS.ReadFile("badges/" + style + ".svg")
		if err != nil {
			log.Error("failed to load badge template", "style", style, "error", err)
			continue
		}
		tmpl, err := template.New(style).Parse(string(data))
		if err != nil {
			log.Error("failed to parse badge template", "style", style, "error", err)
			continue
		}
		h.templates[style] = tmpl
	}

	return h
}

// BadgeData holds the template data for rendering a badge.
type BadgeData struct {
	Status      string
	StatusUpper string
	StatusShort string
	Color       string
	Glow        string
	Icon        template.HTML // For minimal badge icons
}

// ServeHTTP handles badge requests.
// Path format: /badge/{owner}/{repo}.svg
// Query params: ?style=neon&branch=main
func (h *BadgeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Parse path: /badge/owner/repo.svg
	path := strings.TrimPrefix(r.URL.Path, "/badge/")
	path = strings.TrimSuffix(path, ".svg")
	parts := strings.Split(path, "/")

	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		http.Error(w, "invalid path: expected /badge/{owner}/{repo}.svg", http.StatusBadRequest)
		return
	}

	owner := parts[0]
	repo := parts[1]

	// Get query params
	style := r.URL.Query().Get("style")
	if style == "" {
		style = "shields" // Default to shields.io style
	}
	branch := r.URL.Query().Get("branch")

	// Look up repo and get latest job status
	status := h.getRepoStatus(r.Context(), owner, repo, branch)

	// Generate and serve SVG
	svg := h.renderBadge(style, status)

	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=60, s-maxage=60") // 1 minute cache
	w.Header().Set("ETag", fmt.Sprintf(`"%s-%s-%s"`, owner, repo, status))
	w.Write([]byte(svg))
}

func (h *BadgeHandler) getRepoStatus(ctx context.Context, owner, repo, branch string) string {
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

// Color schemes for each status
var statusColors = map[string]struct {
	main string
	glow string
}{
	"passing": {"#22c55e", "#4ade80"},
	"failing": {"#ef4444", "#f87171"},
	"running": {"#eab308", "#facc15"},
	"unknown": {"#6b7280", "#9ca3af"},
}

// Icons for minimal badge
var statusIcons = map[string]string{
	"passing": `<path d="M16 10 L20 14 L28 6" stroke="#fff" stroke-width="2.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/>`,
	"failing": `<path d="M16 8 L26 18 M26 8 L16 18" stroke="#fff" stroke-width="2.5" fill="none" stroke-linecap="round"/>`,
	"running": `<circle cx="21" cy="12" r="5" stroke="#fff" stroke-width="2" fill="none"/><path d="M21 9 L21 12 L23 14" stroke="#fff" stroke-width="2" fill="none" stroke-linecap="round"/>`,
	"unknown": `<text x="21" y="16" font-family="-apple-system,sans-serif" font-size="14" font-weight="700" fill="#fff" text-anchor="middle">?</text>`,
}

func (h *BadgeHandler) renderBadge(style, status string) string {
	tmpl, ok := h.templates[style]
	if !ok {
		tmpl = h.templates["shields"] // Fallback to shields
	}
	if tmpl == nil {
		return fallbackBadge(status)
	}

	colors, ok := statusColors[status]
	if !ok {
		colors = statusColors["unknown"]
	}

	statusShort := strings.ToUpper(status)
	if len(statusShort) > 4 {
		statusShort = statusShort[:4]
	}

	data := BadgeData{
		Status:      status,
		StatusUpper: strings.ToUpper(status),
		StatusShort: statusShort,
		Color:       colors.main,
		Glow:        colors.glow,
		Icon:        template.HTML(statusIcons[status]),
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fallbackBadge(status)
	}

	return buf.String()
}

// fallbackBadge returns a simple badge if templates fail
func fallbackBadge(status string) string {
	colors := statusColors[status]
	if colors.main == "" {
		colors = statusColors["unknown"]
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="108" height="20">
  <rect width="49" height="20" fill="#555"/>
  <rect x="49" width="59" height="20" fill="%s"/>
  <g fill="#fff" text-anchor="middle" font-family="sans-serif" font-size="11">
    <text x="24.5" y="14">cinch</text>
    <text x="77.5" y="14">%s</text>
  </g>
</svg>`, colors.main, status)
}
