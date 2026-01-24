package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ehrlich-b/cinch/internal/storage"
)

// BadgeHandler serves build status badges as SVG.
type BadgeHandler struct {
	store storage.Storage
	log   *slog.Logger
}

// NewBadgeHandler creates a new badge handler.
func NewBadgeHandler(store storage.Storage, log *slog.Logger) *BadgeHandler {
	return &BadgeHandler{store: store, log: log}
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
		style = "neon" // Default to neon - it's the coolest
	}
	branch := r.URL.Query().Get("branch")

	// Look up repo and get latest job status
	status := h.getRepoStatus(r.Context(), owner, repo, branch)

	// Generate and serve SVG
	svg := generateBadgeSVG(style, status)

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

// generateBadgeSVG creates an SVG badge for the given style and status.
func generateBadgeSVG(style, status string) string {
	colors := map[string]struct {
		main string
		glow string
	}{
		"passing": {"#22c55e", "#4ade80"},
		"failing": {"#ef4444", "#f87171"},
		"running": {"#eab308", "#facc15"},
		"unknown": {"#6b7280", "#9ca3af"},
	}

	c, ok := colors[status]
	if !ok {
		c = colors["unknown"]
	}

	switch style {
	case "flat":
		return flatBadge(status, c.main)
	case "modern":
		return modernBadge(status, c.main)
	case "neon":
		return neonBadge(status, c.main, c.glow)
	case "electric":
		return electricBadge(status, c.main, c.glow)
	case "terminal":
		return terminalBadge(status, c.main)
	case "glass":
		return glassBadge(status, c.main, c.glow)
	case "holographic":
		return holographicBadge(status, c.main, c.glow)
	case "pixel":
		return pixelBadge(status, c.main)
	case "minimal":
		return minimalBadge(status, c.main, c.glow)
	case "outlined":
		return outlinedBadge(status, c.main)
	default:
		return neonBadge(status, c.main, c.glow)
	}
}

func flatBadge(status, color string) string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="108" height="20">
  <linearGradient id="b" x2="0" y2="100%%">
    <stop offset="0" stop-color="#bbb" stop-opacity=".1"/>
    <stop offset="1" stop-opacity=".1"/>
  </linearGradient>
  <clipPath id="a"><rect width="108" height="20" rx="3"/></clipPath>
  <g clip-path="url(#a)">
    <path fill="#555" d="M0 0h49v20H0z"/>
    <path fill="%s" d="M49 0h59v20H49z"/>
    <path fill="url(#b)" d="M0 0h108v20H0z"/>
  </g>
  <g fill="#fff" text-anchor="middle" font-family="DejaVu Sans,Verdana,sans-serif" font-size="11">
    <text x="24.5" y="15" fill-opacity=".3">cinch</text>
    <text x="24.5" y="14">cinch</text>
    <text x="77.5" y="15" fill-opacity=".3">%s</text>
    <text x="77.5" y="14">%s</text>
  </g>
</svg>`, color, status, status)
}

func modernBadge(status, color string) string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="116" height="26">
  <defs>
    <linearGradient id="bg" x1="0%%" y1="0%%" x2="0%%" y2="100%%">
      <stop offset="0%%" stop-color="#27272a"/>
      <stop offset="100%%" stop-color="#18181b"/>
    </linearGradient>
  </defs>
  <rect width="116" height="26" rx="6" fill="url(#bg)"/>
  <circle cx="16" cy="13" r="5" fill="%s"/>
  <text x="28" y="17" font-family="-apple-system,sans-serif" font-size="11" font-weight="600" fill="#71717a">cinch</text>
  <text x="66" y="17" font-family="-apple-system,sans-serif" font-size="11" font-weight="500" fill="#a1a1aa">%s</text>
</svg>`, color, status)
}

func neonBadge(status, color, glow string) string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="130" height="32">
  <defs>
    <filter id="glow" x="-50%%" y="-50%%" width="200%%" height="200%%">
      <feGaussianBlur stdDeviation="3" result="blur"/>
      <feMerge><feMergeNode in="blur"/><feMergeNode in="SourceGraphic"/></feMerge>
    </filter>
    <linearGradient id="neon" x1="0%%" y1="0%%" x2="100%%" y2="0%%">
      <stop offset="0%%" stop-color="%s"/>
      <stop offset="100%%" stop-color="%s"/>
    </linearGradient>
  </defs>
  <rect width="130" height="32" rx="6" fill="#0a0a0a"/>
  <rect x="1" y="1" width="128" height="30" rx="5" fill="none" stroke="url(#neon)" stroke-width="1.5" filter="url(#glow)" opacity="0.6"/>
  <text x="14" y="21" font-family="JetBrains Mono,SF Mono,monospace" font-size="13" font-weight="700" fill="#888">cinch</text>
  <line x1="60" y1="8" x2="60" y2="24" stroke="#333" stroke-width="1"/>
  <text x="70" y="21" font-family="JetBrains Mono,SF Mono,monospace" font-size="13" font-weight="600" fill="url(#neon)" filter="url(#glow)">%s</text>
</svg>`, color, glow, status)
}

func electricBadge(status, color, glow string) string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="140" height="28">
  <defs>
    <linearGradient id="elec" x1="0%%" y1="0%%" x2="100%%" y2="0%%">
      <stop offset="0%%" stop-color="%s"/>
      <stop offset="100%%" stop-color="%s"/>
    </linearGradient>
    <filter id="glow" x="-100%%" y="-100%%" width="300%%" height="300%%">
      <feGaussianBlur stdDeviation="2" result="blur"/>
      <feMerge><feMergeNode in="blur"/><feMergeNode in="SourceGraphic"/></feMerge>
    </filter>
  </defs>
  <rect width="140" height="28" rx="4" fill="#0c0c0c"/>
  <rect x="0" y="0" width="140" height="1" fill="url(#elec)" opacity="0.3"/>
  <text x="16" y="18" font-family="Inter,-apple-system,sans-serif" font-size="12" font-weight="800" fill="#666" letter-spacing="1">CINCH</text>
  <rect x="68" y="6" width="1" height="16" fill="#333"/>
  <text x="80" y="18" font-family="Inter,-apple-system,sans-serif" font-size="12" font-weight="600" fill="url(#elec)" filter="url(#glow)">%s</text>
</svg>`, color, glow, strings.ToUpper(status))
}

func terminalBadge(status, color string) string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="145" height="24">
  <rect width="145" height="24" rx="4" fill="#0d1117"/>
  <rect x="1" y="1" width="143" height="22" rx="3" fill="none" stroke="#30363d" stroke-width="1"/>
  <text x="8" y="16" font-family="SF Mono,Menlo,monospace" font-size="11" fill="%s">$</text>
  <text x="20" y="16" font-family="SF Mono,Menlo,monospace" font-size="11" fill="#c9d1d9">cinch</text>
  <text x="58" y="16" font-family="SF Mono,Menlo,monospace" font-size="11" fill="#484f58">[</text>
  <text x="64" y="16" font-family="SF Mono,Menlo,monospace" font-size="11" fill="%s" font-weight="bold">%s</text>
  <text x="122" y="16" font-family="SF Mono,Menlo,monospace" font-size="11" fill="#484f58">]</text>
  <rect x="132" y="6" width="7" height="12" fill="#58a6ff" opacity="0.8"/>
</svg>`, color, color, strings.ToUpper(status))
}

func glassBadge(status, color, glow string) string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="120" height="30">
  <defs>
    <linearGradient id="glass" x1="0%%" y1="0%%" x2="0%%" y2="100%%">
      <stop offset="0%%" stop-color="#ffffff" stop-opacity="0.15"/>
      <stop offset="100%%" stop-color="#ffffff" stop-opacity="0.05"/>
    </linearGradient>
    <filter id="blur" x="-50%%" y="-50%%" width="200%%" height="200%%">
      <feGaussianBlur in="SourceGraphic" stdDeviation="10"/>
    </filter>
  </defs>
  <circle cx="20" cy="15" r="20" fill="#3b82f6" filter="url(#blur)" opacity="0.5"/>
  <circle cx="100" cy="15" r="20" fill="%s" filter="url(#blur)" opacity="0.5"/>
  <rect x="2" y="2" width="116" height="26" rx="13" fill="url(#glass)"/>
  <rect x="2" y="2" width="116" height="26" rx="13" fill="none" stroke="#ffffff" stroke-opacity="0.2" stroke-width="1"/>
  <circle cx="18" cy="15" r="5" fill="#3b82f6"/>
  <text x="30" y="19" font-family="-apple-system,sans-serif" font-size="11" font-weight="600" fill="#fff">cinch</text>
  <text x="68" y="19" font-family="-apple-system,sans-serif" font-size="11" font-weight="500" fill="%s">%s</text>
</svg>`, color, glow, status)
}

func holographicBadge(status, color, glow string) string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="120" height="28">
  <defs>
    <linearGradient id="holo" x1="0%%" y1="0%%" x2="100%%" y2="0%%">
      <stop offset="0%%" stop-color="#ff00ff"/>
      <stop offset="33%%" stop-color="#00ffff"/>
      <stop offset="66%%" stop-color="#ffff00"/>
      <stop offset="100%%" stop-color="#ff00ff"/>
    </linearGradient>
  </defs>
  <rect width="120" height="28" rx="6" fill="#0a0a0a"/>
  <rect x="1" y="1" width="118" height="26" rx="5" fill="none" stroke="url(#holo)" stroke-width="1"/>
  <text x="16" y="18" font-family="Inter,-apple-system,sans-serif" font-size="11" font-weight="700" fill="#888">cinch</text>
  <text x="60" y="18" font-family="Inter,-apple-system,sans-serif" font-size="11" font-weight="600" fill="%s">%s</text>
  <circle cx="106" cy="14" r="4" fill="%s"/>
</svg>`, glow, status, color)
}

func pixelBadge(status, color string) string {
	statusShort := status
	if len(statusShort) > 4 {
		statusShort = statusShort[:4]
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="112" height="24">
  <rect width="112" height="24" fill="#1a1a2e"/>
  <rect x="0" y="0" width="112" height="2" fill="%s"/>
  <rect x="0" y="22" width="112" height="2" fill="%s"/>
  <rect x="0" y="0" width="2" height="24" fill="%s"/>
  <rect x="110" y="0" width="2" height="24" fill="%s"/>
  <text x="12" y="16" font-family="Monaco,Consolas,monospace" font-size="10" fill="#eee" letter-spacing="1">CINCH</text>
  <text x="58" y="16" font-family="Monaco,Consolas,monospace" font-size="10" fill="%s" letter-spacing="1">%s</text>
</svg>`, color, color, color, color, color, strings.ToUpper(statusShort))
}

func minimalBadge(status, color, glow string) string {
	// Show different icon based on status
	icon := `<path d="M16 10 L20 14 L28 6" stroke="#fff" stroke-width="2.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/>` // checkmark
	if status == "failing" {
		icon = `<path d="M16 8 L26 18 M26 8 L16 18" stroke="#fff" stroke-width="2.5" fill="none" stroke-linecap="round"/>` // X
	} else if status == "running" {
		icon = `<circle cx="21" cy="12" r="5" stroke="#fff" stroke-width="2" fill="none"/><path d="M21 9 L21 12 L23 14" stroke="#fff" stroke-width="2" fill="none" stroke-linecap="round"/>` // clock
	} else if status == "unknown" {
		icon = `<text x="21" y="16" font-family="-apple-system,sans-serif" font-size="14" font-weight="700" fill="#fff" text-anchor="middle">?</text>` // question mark
	}

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="72" height="24">
  <defs>
    <linearGradient id="min" x1="0%%" y1="0%%" x2="0%%" y2="100%%">
      <stop offset="0%%" stop-color="%s"/>
      <stop offset="100%%" stop-color="%s"/>
    </linearGradient>
  </defs>
  <rect width="72" height="24" rx="12" fill="url(#min)"/>
  %s
  <text x="38" y="16" font-family="-apple-system,sans-serif" font-size="11" font-weight="600" fill="#fff">cinch</text>
</svg>`, color, glow, icon)
}

func outlinedBadge(status, color string) string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="108" height="24">
  <rect x="1" y="1" width="106" height="22" rx="4" fill="none" stroke="%s" stroke-width="1.5"/>
  <line x1="50" y1="5" x2="50" y2="19" stroke="%s" stroke-width="1" opacity="0.5"/>
  <text x="25" y="16" font-family="-apple-system,sans-serif" font-size="11" font-weight="600" fill="%s" text-anchor="middle">cinch</text>
  <text x="78" y="16" font-family="-apple-system,sans-serif" font-size="11" font-weight="500" fill="%s" text-anchor="middle">%s</text>
</svg>`, color, color, color, color, status)
}
