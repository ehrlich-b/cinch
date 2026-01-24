package forge

import (
	"context"
	"net/http"
	"time"
)

// Forge defines the interface for git forge integrations.
// Each forge (GitHub, Forgejo, etc.) implements this interface.
type Forge interface {
	// Name returns the forge name for display.
	Name() string

	// Identify returns true if the request is from this forge.
	Identify(r *http.Request) bool

	// ParsePush parses a push webhook and verifies the signature.
	// Returns an error if signature verification fails.
	ParsePush(r *http.Request, secret string) (*PushEvent, error)

	// PostStatus posts a commit status to the forge.
	PostStatus(ctx context.Context, repo *Repo, commit string, status *Status) error

	// CloneToken generates a short-lived token for cloning.
	// Returns the token and its expiry time.
	// For public repos, may return empty string (no token needed).
	CloneToken(ctx context.Context, repo *Repo) (string, time.Time, error)
}

// PushEvent represents a push webhook event.
type PushEvent struct {
	Repo   *Repo
	Commit string // SHA of the head commit
	Ref    string // Full ref (refs/heads/main or refs/tags/v1.0.0)
	Branch string // Branch name (empty for tag pushes)
	Tag    string // Tag name (empty for branch pushes)
	Sender string // Username who pushed
}

// Repo represents a git repository.
type Repo struct {
	ForgeType string // "github", "forgejo", "gitea"
	Owner     string
	Name      string
	CloneURL  string
	HTMLURL   string
	Private   bool
}

// FullName returns "owner/name".
func (r *Repo) FullName() string {
	return r.Owner + "/" + r.Name
}

// Status represents a commit status to post.
type Status struct {
	State       StatusState
	Context     string // e.g., "cinch" or "cinch/build"
	Description string // e.g., "Build passed in 2m 34s"
	TargetURL   string // Link to job logs
}

// StatusState represents the state of a commit status.
type StatusState string

const (
	StatusPending StatusState = "pending"
	StatusRunning StatusState = "running"
	StatusSuccess StatusState = "success"
	StatusFailure StatusState = "failure"
	StatusError   StatusState = "error"
)

// Forge type constants - match storage.ForgeType values
const (
	TypeGitHub  = "github"
	TypeGitLab  = "gitlab"
	TypeForgejo = "forgejo"
	TypeGitea   = "gitea"
)

// ForgeConfig holds configuration for creating a forge instance.
type ForgeConfig struct {
	Type    string // TypeGitHub, TypeForgejo, etc.
	Token   string // API token for authentication
	BaseURL string // Base URL for self-hosted instances (Forgejo, GitLab)
}

// New creates a Forge instance based on the config.
// Returns nil if the forge type is unknown.
func New(cfg ForgeConfig) Forge {
	switch cfg.Type {
	case TypeGitHub:
		return &GitHub{Token: cfg.Token}
	case TypeForgejo:
		return &Forgejo{Token: cfg.Token, BaseURL: cfg.BaseURL}
	case TypeGitea:
		return &Forgejo{Token: cfg.Token, BaseURL: cfg.BaseURL, IsGitea: true}
	default:
		return nil
	}
}
