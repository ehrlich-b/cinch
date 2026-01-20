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
	Branch string // Branch name (without refs/heads/)
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
