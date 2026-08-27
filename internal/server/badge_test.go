package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/ehrlich-b/cinch/internal/storage"
)

// badgeJSONBody mirrors ShieldsEndpoint so we can assert on the full JSON body.
type badgeJSONBody struct {
	SchemaVersion int    `json:"schemaVersion"`
	Label         string `json:"label"`
	Message       string `json:"message"`
	Color         string `json:"color"`
}

func newBadgeTestHandler(t *testing.T) (storage.Storage, *BadgeHandler) {
	t.Helper()
	store, err := storage.NewSQLite(":memory:", "", "")
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store, NewBadgeHandler(store, slog.Default(), "https://cinch.sh")
}

func createBadgeRepo(t *testing.T, store storage.Storage, id string, forgeType storage.ForgeType, owner, name, htmlURL string, private bool) {
	t.Helper()
	err := store.CreateRepo(t.Context(), &storage.Repo{
		ID:        id,
		ForgeType: forgeType,
		Owner:     owner,
		Name:      name,
		CloneURL:  htmlURL + ".git",
		HTMLURL:   htmlURL,
		Private:   private,
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("CreateRepo(%s): %v", id, err)
	}
}

func createBadgeJob(t *testing.T, store storage.Storage, id, repoID, branch string, status storage.JobStatus, createdAt time.Time) {
	t.Helper()
	err := store.CreateJob(t.Context(), &storage.Job{
		ID:        id,
		RepoID:    repoID,
		Commit:    "abc123",
		Branch:    branch,
		Status:    status,
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("CreateJob(%s): %v", id, err)
	}
}

func doBadgeRequest(h *BadgeHandler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeBadgeBody(t *testing.T, rec *httptest.ResponseRecorder) badgeJSONBody {
	t.Helper()
	var body badgeJSONBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode JSON body %q: %v", rec.Body.String(), err)
	}
	return body
}

func TestParsePath(t *testing.T) {
	h := &BadgeHandler{}

	tests := []struct {
		name      string
		path      string
		prefix    string
		suffix    string
		wantForge string
		wantOwner string
		wantRepo  string
		wantOK    bool
	}{
		{
			name:      "valid",
			path:      "/api/badge/github.com/acme/widget.json",
			prefix:    "/api/badge/",
			suffix:    ".json",
			wantForge: "github.com",
			wantOwner: "acme",
			wantRepo:  "widget",
			wantOK:    true,
		},
		{
			name:      "valid svg path",
			path:      "/badge/gitlab.com/acme/widget.svg",
			prefix:    "/badge/",
			suffix:    ".svg",
			wantForge: "gitlab.com",
			wantOwner: "acme",
			wantRepo:  "widget",
			wantOK:    true,
		},
		{
			name:   "too few segments",
			path:   "/api/badge/github.com/acme.json",
			prefix: "/api/badge/",
			suffix: ".json",
		},
		{
			name:   "too many segments",
			path:   "/api/badge/github.com/acme/widget/extra.json",
			prefix: "/api/badge/",
			suffix: ".json",
		},
		{
			name:   "empty forge segment",
			path:   "/api/badge//acme/widget.json",
			prefix: "/api/badge/",
			suffix: ".json",
		},
		{
			name:   "empty owner segment",
			path:   "/api/badge/github.com//widget.json",
			prefix: "/api/badge/",
			suffix: ".json",
		},
		{
			name:   "empty repo segment",
			path:   "/api/badge/github.com/acme/",
			prefix: "/api/badge/",
			suffix: ".json",
		},
		{
			name:   "slice with slashes",
			path:   "/api/badge/github.com/acme///widget.json",
			prefix: "/api/badge/",
			suffix: ".json",
		},
		{
			name:   "missing prefix",
			path:   "/api-badge/github.com/acme/widget.json",
			prefix: "/api/badge/",
			suffix: ".json",
		},
		{
			name:   "suffix missing with trailing slash",
			path:   "/api/badge/github.com/acme/widget/",
			prefix: "/api/badge/",
			suffix: ".json",
		},
		{
			// NOTE: parsePath trims the suffix leniently — a path that is a valid
			// 3-segment path but simply omits ".json" still parses as ok=true.
			// ServeHTTP routes on the prefix alone, so this is not observable
			// as an error through the handler. Pinned here rather than asserted
			// as false so the actual (lenient) behavior is documented.
			name:      "suffix missing but path valid (lenient)",
			path:      "/api/badge/github.com/acme/widget",
			prefix:    "/api/badge/",
			suffix:    ".json",
			wantForge: "github.com",
			wantOwner: "acme",
			wantRepo:  "widget",
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			forge, owner, repo, ok := h.parsePath(tt.path, tt.prefix, tt.suffix)
			if ok != tt.wantOK {
				t.Errorf("parsePath(%q) ok = %v, want %v", tt.path, ok, tt.wantOK)
			}
			if forge != tt.wantForge || owner != tt.wantOwner || repo != tt.wantRepo {
				t.Errorf("parsePath(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tt.path, forge, owner, repo, tt.wantForge, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}

func TestForgeToLogo(t *testing.T) {
	tests := []struct {
		forge string
		want  string
	}{
		{"github.com", "github"},
		{"gitlab.com", "gitlab"},
		{"codeberg.org", "codeberg"},
		{"gitea.example.org", "gitea"},
		{"forgejo.example.org", "forgejo"},
		{"bitbucket.org", ""},
	}

	for _, tt := range tests {
		if got := forgeToLogo(tt.forge); got != tt.want {
			t.Errorf("forgeToLogo(%q) = %q, want %q", tt.forge, got, tt.want)
		}
	}
}

func TestStatusToColor(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"passing", "brightgreen"},
		{"failing", "red"},
		{"running", "yellow"},
		{"unknown", "lightgrey"},
		{"anything else", "lightgrey"},
	}

	for _, tt := range tests {
		if got := statusToColor(tt.status); got != tt.want {
			t.Errorf("statusToColor(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestBadgeJSONEndpoint(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		setup func(*testing.T, storage.Storage)
		want  badgeJSONBody
	}{
		{
			name:  "unknown repo",
			path:  "/api/badge/github.com/acme/nope.json",
			setup: func(t *testing.T, store storage.Storage) {},
			want:  badgeJSONBody{SchemaVersion: 1, Label: "cinch", Message: "unknown", Color: "lightgrey"},
		},
		{
			name: "zero jobs",
			path: "/api/badge/github.com/acme/widget.json",
			setup: func(t *testing.T, store storage.Storage) {
				createBadgeRepo(t, store, "r_1", storage.ForgeTypeGitHub, "acme", "widget", "https://github.com/acme/widget", false)
			},
			want: badgeJSONBody{SchemaVersion: 1, Label: "cinch", Message: "unknown", Color: "lightgrey"},
		},
		{
			name: "private repo never leaks status",
			path: "/api/badge/github.com/acme/widget.json",
			setup: func(t *testing.T, store storage.Storage) {
				createBadgeRepo(t, store, "r_1", storage.ForgeTypeGitHub, "acme", "widget", "https://github.com/acme/widget", true)
				createBadgeJob(t, store, "j_1", "r_1", "main", storage.JobStatusSuccess, time.Now())
			},
			want: badgeJSONBody{SchemaVersion: 1, Label: "cinch", Message: "unknown", Color: "lightgrey"},
		},
		{
			name: "success job",
			path: "/api/badge/github.com/acme/widget.json",
			setup: func(t *testing.T, store storage.Storage) {
				createBadgeRepo(t, store, "r_1", storage.ForgeTypeGitHub, "acme", "widget", "https://github.com/acme/widget", false)
				createBadgeJob(t, store, "j_1", "r_1", "main", storage.JobStatusSuccess, time.Now())
			},
			want: badgeJSONBody{SchemaVersion: 1, Label: "cinch", Message: "passing", Color: "brightgreen"},
		},
		{
			name: "failed job",
			path: "/api/badge/github.com/acme/widget.json",
			setup: func(t *testing.T, store storage.Storage) {
				createBadgeRepo(t, store, "r_1", storage.ForgeTypeGitHub, "acme", "widget", "https://github.com/acme/widget", false)
				createBadgeJob(t, store, "j_1", "r_1", "main", storage.JobStatusFailed, time.Now())
			},
			want: badgeJSONBody{SchemaVersion: 1, Label: "cinch", Message: "failing", Color: "red"},
		},
		{
			name: "running job",
			path: "/api/badge/github.com/acme/widget.json",
			setup: func(t *testing.T, store storage.Storage) {
				createBadgeRepo(t, store, "r_1", storage.ForgeTypeGitHub, "acme", "widget", "https://github.com/acme/widget", false)
				createBadgeJob(t, store, "j_1", "r_1", "main", storage.JobStatusRunning, time.Now())
			},
			want: badgeJSONBody{SchemaVersion: 1, Label: "cinch", Message: "running", Color: "yellow"},
		},
		{
			name: "cancelled job maps to unknown",
			path: "/api/badge/github.com/acme/widget.json",
			setup: func(t *testing.T, store storage.Storage) {
				createBadgeRepo(t, store, "r_1", storage.ForgeTypeGitHub, "acme", "widget", "https://github.com/acme/widget", false)
				createBadgeJob(t, store, "j_1", "r_1", "main", storage.JobStatusCancelled, time.Now())
			},
			want: badgeJSONBody{SchemaVersion: 1, Label: "cinch", Message: "unknown", Color: "lightgrey"},
		},
		{
			name: "forge not present in html url is unknown",
			path: "/api/badge/gitlab.com/acme/widget.json",
			setup: func(t *testing.T, store storage.Storage) {
				createBadgeRepo(t, store, "r_1", storage.ForgeTypeGitHub, "acme", "widget", "https://github.com/acme/widget", false)
				createBadgeJob(t, store, "j_1", "r_1", "main", storage.JobStatusSuccess, time.Now())
			},
			want: badgeJSONBody{SchemaVersion: 1, Label: "cinch", Message: "unknown", Color: "lightgrey"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, h := newBadgeTestHandler(t)
			tt.setup(t, store)

			rec := doBadgeRequest(h, tt.path)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}

			got := decodeBadgeBody(t, rec)
			if got != tt.want {
				t.Errorf("body = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestBadgeForgeIsolation(t *testing.T) {
	// Two repos with the same owner/name on different forges must not
	// cross-contaminate: the forge segment selects which one is reported.
	store, h := newBadgeTestHandler(t)

	createBadgeRepo(t, store, "r_gh", storage.ForgeTypeGitHub, "acme", "widget", "https://github.com/acme/widget", false)
	createBadgeJob(t, store, "j_gh", "r_gh", "main", storage.JobStatusSuccess, time.Now())

	createBadgeRepo(t, store, "r_gl", storage.ForgeTypeGitLab, "acme", "widget", "https://gitlab.com/acme/widget", false)
	createBadgeJob(t, store, "j_gl", "r_gl", "main", storage.JobStatusFailed, time.Now())

	tests := []struct {
		path string
		want badgeJSONBody
	}{
		{
			path: "/api/badge/github.com/acme/widget.json",
			want: badgeJSONBody{SchemaVersion: 1, Label: "cinch", Message: "passing", Color: "brightgreen"},
		},
		{
			path: "/api/badge/gitlab.com/acme/widget.json",
			want: badgeJSONBody{SchemaVersion: 1, Label: "cinch", Message: "failing", Color: "red"},
		},
	}

	for _, tt := range tests {
		rec := doBadgeRequest(h, tt.path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", tt.path, rec.Code)
		}
		if got := decodeBadgeBody(t, rec); got != tt.want {
			t.Errorf("%s: body = %+v, want %+v", tt.path, got, tt.want)
		}
	}
}

func TestBadgeBranchFilter(t *testing.T) {
	store, h := newBadgeTestHandler(t)

	createBadgeRepo(t, store, "r_1", storage.ForgeTypeGitHub, "acme", "widget", "https://github.com/acme/widget", false)
	// Dev is the newest job, main is older, so without a branch filter the
	// badge would report dev's status.
	createBadgeJob(t, store, "j_main", "r_1", "main", storage.JobStatusSuccess, time.Now().Add(-2*time.Second))
	createBadgeJob(t, store, "j_dev", "r_1", "dev", storage.JobStatusFailed, time.Now().Add(-time.Second))

	tests := []struct {
		path string
		want badgeJSONBody
	}{
		{
			path: "/api/badge/github.com/acme/widget.json?branch=main",
			want: badgeJSONBody{SchemaVersion: 1, Label: "cinch", Message: "passing", Color: "brightgreen"},
		},
		{
			path: "/api/badge/github.com/acme/widget.json?branch=dev",
			want: badgeJSONBody{SchemaVersion: 1, Label: "cinch", Message: "failing", Color: "red"},
		},
		{
			path: "/api/badge/github.com/acme/widget.json",
			want: badgeJSONBody{SchemaVersion: 1, Label: "cinch", Message: "failing", Color: "red"},
		},
		{
			path: "/api/badge/github.com/acme/widget.json?branch=release-2.x",
			want: badgeJSONBody{SchemaVersion: 1, Label: "cinch", Message: "unknown", Color: "lightgrey"},
		},
	}

	for _, tt := range tests {
		rec := doBadgeRequest(h, tt.path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", tt.path, rec.Code)
		}
		if got := decodeBadgeBody(t, rec); got != tt.want {
			t.Errorf("%s: body = %+v, want %+v", tt.path, got, tt.want)
		}
	}
}

func TestBadgeRedirect(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		wantLogo    string
		wantJSONURL string
	}{
		{
			name:        "github with branch",
			path:        "/badge/github.com/acme/widget.svg?branch=dev",
			wantLogo:    "github",
			wantJSONURL: "https://cinch.sh/api/badge/github.com/acme/widget.json?branch=dev",
		},
		{
			// The branch is QueryEscaped when appended to the JSON URL, then the
			// whole JSON URL is QueryEscaped again into shields.io's url= param.
			// Slashes therefore arrive double-escaped (%252F), which the JSON
			// endpoint decodes back to the literal branch value on the other side.
			name:        "branch with slash is escaped",
			path:        "/badge/github.com/acme/widget.svg?branch=feature%2Ffix",
			wantLogo:    "github",
			wantJSONURL: "https://cinch.sh/api/badge/github.com/acme/widget.json?branch=feature%2Ffix",
		},
		{
			name:        "gitlab",
			path:        "/badge/gitlab.com/acme/widget.svg",
			wantLogo:    "gitlab",
			wantJSONURL: "https://cinch.sh/api/badge/gitlab.com/acme/widget.json",
		},
		{
			name:        "codeberg",
			path:        "/badge/codeberg.org/acme/widget.svg",
			wantLogo:    "codeberg",
			wantJSONURL: "https://cinch.sh/api/badge/codeberg.org/acme/widget.json",
		},
		{
			name:        "gitea",
			path:        "/badge/gitea.example.com/acme/widget.svg",
			wantLogo:    "gitea",
			wantJSONURL: "https://cinch.sh/api/badge/gitea.example.com/acme/widget.json",
		},
		{
			name:        "forgejo",
			path:        "/badge/forgejo.example.org/acme/widget.svg",
			wantLogo:    "forgejo",
			wantJSONURL: "https://cinch.sh/api/badge/forgejo.example.org/acme/widget.json",
		},
		{
			name:        "unknown forge has empty logo",
			path:        "/badge/bitbucket.org/acme/widget.svg",
			wantLogo:    "",
			wantJSONURL: "https://cinch.sh/api/badge/bitbucket.org/acme/widget.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, h := newBadgeTestHandler(t)

			rec := doBadgeRequest(h, tt.path)
			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302", rec.Code)
			}

			loc := rec.Header().Get("Location")
			u, err := url.Parse(loc)
			if err != nil {
				t.Fatalf("parse Location %q: %v", loc, err)
			}
			if u.Scheme != "https" || u.Host != "img.shields.io" {
				t.Errorf("redirect host = %s://%s, want https://img.shields.io (Location %q)", u.Scheme, u.Host, loc)
			}

			q := u.Query()
			if got := q.Get("url"); got != tt.wantJSONURL {
				t.Errorf("url param = %q, want %q (Location %q)", got, tt.wantJSONURL, loc)
			}
			if got := q.Get("label"); got != "cinch" {
				t.Errorf("label param = %q, want cinch", got)
			}
			if got := q.Get("logo"); got != tt.wantLogo {
				t.Errorf("logo param = %q, want %q", got, tt.wantLogo)
			}
			if got := q.Get("style"); got != "flat" {
				t.Errorf("style param = %q, want flat", got)
			}
		})
	}
}

func TestBadgeRedirectRoundTrip(t *testing.T) {
	// Simulate the full shields.io flow without network: take the url= param
	// from the redirect Location and serve it back through the JSON endpoint.
	// This proves the double-escaped branch survives the round trip intact.
	store, h := newBadgeTestHandler(t)
	createBadgeRepo(t, store, "r_1", storage.ForgeTypeGitHub, "acme", "widget", "https://github.com/acme/widget", false)
	createBadgeJob(t, store, "j_1", "r_1", "feature/fix", storage.JobStatusSuccess, time.Now())

	rec := doBadgeRequest(h, "/badge/github.com/acme/widget.svg?branch=feature%2Ffix")
	if rec.Code != http.StatusFound {
		t.Fatalf("redirect status = %d, want 302", rec.Code)
	}

	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	jsonURL, err := url.Parse(loc.Query().Get("url"))
	if err != nil {
		t.Fatalf("parse url param: %v", err)
	}
	if got := jsonURL.Query().Get("branch"); got != "feature/fix" {
		t.Fatalf("round-tripped branch = %q, want feature/fix", got)
	}

	reqPath := jsonURL.Path
	if jsonURL.RawQuery != "" {
		reqPath += "?" + jsonURL.RawQuery
	}
	got := decodeBadgeBody(t, doBadgeRequest(h, reqPath))
	want := badgeJSONBody{SchemaVersion: 1, Label: "cinch", Message: "passing", Color: "brightgreen"}
	if got != want {
		t.Errorf("JSON endpoint body = %+v, want %+v", got, want)
	}
}

func TestBadgeRouting(t *testing.T) {
	_, h := newBadgeTestHandler(t)

	tests := []struct {
		name string
		path string
		want int
	}{
		{"neither prefix", "/foo/bar/baz.json", http.StatusNotFound},
		{"unmatched suffix still routes to serveJSON", "/api/badge/github.com/acme", http.StatusBadRequest},
		{"unmatched suffix still routes to redirect", "/badge/github.com/acme", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doBadgeRequest(h, tt.path)
			if rec.Code != tt.want {
				t.Errorf("GET %s status = %d, want %d", tt.path, rec.Code, tt.want)
			}
		})
	}
}
