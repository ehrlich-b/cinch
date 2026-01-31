package storage

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
)

// Storage defines the interface for all database operations.
type Storage interface {
	// Jobs
	CreateJob(ctx context.Context, job *Job) error
	GetJob(ctx context.Context, id string) (*Job, error)
	GetJobSiblings(ctx context.Context, repoID, commit, excludeJobID string) ([]*Job, error) // Other jobs for same repo+commit
	ListJobs(ctx context.Context, filter JobFilter) ([]*Job, error)
	ListJobsByWorker(ctx context.Context, workerID string, limit int) ([]*Job, error)
	UpdateJobStatus(ctx context.Context, id string, status JobStatus, exitCode *int) error
	UpdateJobWorker(ctx context.Context, jobID, workerID string) error
	UpdateJobCheckRunID(ctx context.Context, id string, checkRunID int64) error
	ApproveJob(ctx context.Context, jobID, approvedBy string) error

	// Workers
	CreateWorker(ctx context.Context, worker *Worker) error
	GetWorker(ctx context.Context, id string) (*Worker, error)
	ListWorkers(ctx context.Context) ([]*Worker, error)
	UpdateWorkerLastSeen(ctx context.Context, id string) error
	UpdateWorkerStatus(ctx context.Context, id string, status WorkerStatus) error
	UpdateWorkerOwner(ctx context.Context, id, ownerName, mode string) error
	DeleteWorker(ctx context.Context, id string) error

	// Repos
	CreateRepo(ctx context.Context, repo *Repo) error
	GetRepo(ctx context.Context, id string) (*Repo, error)
	GetRepoByCloneURL(ctx context.Context, cloneURL string) (*Repo, error)
	GetRepoByOwnerName(ctx context.Context, forge, owner, name string) (*Repo, error)
	ListRepos(ctx context.Context) ([]*Repo, error)
	UpdateRepoPrivate(ctx context.Context, id string, private bool) error
	UpdateRepoSecrets(ctx context.Context, id string, secrets map[string]string) error
	DeleteRepo(ctx context.Context, id string) error

	// Tokens
	CreateToken(ctx context.Context, token *Token) error
	GetTokenByHash(ctx context.Context, hash string) (*Token, error)
	ListTokens(ctx context.Context) ([]*Token, error)
	RevokeToken(ctx context.Context, id string) error

	// Logs
	AppendLog(ctx context.Context, jobID, stream, data string) error
	GetLogs(ctx context.Context, jobID string) ([]*LogEntry, error)

	// Users
	GetOrCreateUser(ctx context.Context, name string) (*User, error)
	GetOrCreateUserByEmail(ctx context.Context, email, name string) (*User, error)
	GetUserByName(ctx context.Context, name string) (*User, error)
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	UpdateUserEmail(ctx context.Context, userID, email string) error
	AddUserEmail(ctx context.Context, userID, email string) error
	UpdateUserGitHubConnected(ctx context.Context, userID string) error
	UpdateUserGitLabConnected(ctx context.Context, userID string) error
	UpdateUserForgejoConnected(ctx context.Context, userID string) error
	UpdateUserGitLabCredentials(ctx context.Context, userID, credentials string) error
	UpdateUserForgejoCredentials(ctx context.Context, userID, credentials string) error
	ClearUserGitLabCredentials(ctx context.Context, userID string) error
	ClearUserForgejoCredentials(ctx context.Context, userID string) error
	ClearUserGitHubConnected(ctx context.Context, userID string) error
	DeleteUser(ctx context.Context, id string) error

	// Storage quota
	UpdateJobLogSize(ctx context.Context, jobID string, sizeBytes int64) error
	UpdateUserStorageUsed(ctx context.Context, userID string, deltaBytes int64) error

	// Lifecycle
	Close() error
}

// JobStatus represents the state of a job.
type JobStatus string

const (
	JobStatusPending            JobStatus = "pending"
	JobStatusQueued             JobStatus = "queued"
	JobStatusRunning            JobStatus = "running"
	JobStatusSuccess            JobStatus = "success"
	JobStatusFailed             JobStatus = "failed"
	JobStatusCancelled          JobStatus = "cancelled"
	JobStatusError              JobStatus = "error"               // infrastructure error
	JobStatusPendingContributor JobStatus = "pending_contributor" // fork PR awaiting contributor CI
)

// TrustLevel indicates the relationship between job author and repo.
type TrustLevel string

const (
	TrustOwner        TrustLevel = "owner"        // Repo owner
	TrustCollaborator TrustLevel = "collaborator" // Has write access
	TrustExternal     TrustLevel = "external"     // Fork PR, no write access
)

// Job represents a CI job.
type Job struct {
	ID             string
	RepoID         string
	Commit         string
	Branch         string // Branch name (empty for tags)
	Tag            string // Tag name (empty for branches)
	PRNumber       *int   // Pull request number (nil for push events)
	PRBaseBranch   string // PR target branch (empty for push events)
	Status         JobStatus
	ExitCode       *int
	WorkerID       *string
	InstallationID *int64 // GitHub App installation ID (for status posting)
	CheckRunID     *int64 // GitHub Check Run ID (for Checks API)
	StartedAt      *time.Time
	FinishedAt     *time.Time
	CreatedAt      time.Time

	// Author tracking for worker trust model
	Author     string     // Username who authored the code
	TrustLevel TrustLevel // owner, collaborator, or external
	IsFork     bool       // True if PR is from a fork

	// Approval for external PRs
	ApprovedBy *string    // Username who approved shared worker execution
	ApprovedAt *time.Time // When approval was granted

	// Storage tracking
	LogSizeBytes int64 // Size of compressed logs in bytes
}

// JobFilter for listing jobs.
type JobFilter struct {
	RepoID string
	Status JobStatus
	Branch string
	Limit  int
	Offset int
}

// WorkerStatus represents the connection state of a worker.
type WorkerStatus string

const (
	WorkerStatusOnline  WorkerStatus = "online"
	WorkerStatusOffline WorkerStatus = "offline"
)

// Worker represents a registered worker.
type Worker struct {
	ID        string
	Name      string
	Labels    []string
	Status    WorkerStatus
	LastSeen  time.Time
	CreatedAt time.Time
	// Owner info for visibility filtering
	OwnerName string // Email of worker owner
	Mode      string // "personal" or "shared"
}

// ForgeType represents the type of git forge.
type ForgeType string

const (
	ForgeTypeGitHub  ForgeType = "github"
	ForgeTypeGitLab  ForgeType = "gitlab"
	ForgeTypeForgejo ForgeType = "forgejo"
	ForgeTypeGitea   ForgeType = "gitea"
)

// UserTier represents the subscription tier.
type UserTier string

const (
	UserTierFree UserTier = "free"
	UserTierPro  UserTier = "pro"
	// Note: Self-hosted has no tier enforcement - limits are not applied.
	// Self-hosted is detected by the server (no R2 config = self-hosted).
)

// Storage quotas by tier (hosted service only)
const (
	StorageQuotaFree = 100 * 1024 * 1024       // 100 MB
	StorageQuotaPro  = 10 * 1024 * 1024 * 1024 // 10 GB
	// Self-hosted: no quota (return math.MaxInt64 or skip enforcement)
)

// User represents a Cinch user with connected forge credentials.
type User struct {
	ID                   string
	Name                 string    // Username (from primary auth provider)
	Email                string    // Primary email (for account linking)
	Emails               []string  // All known emails (for account linking during OAuth)
	GitHubConnectedAt    time.Time // When GitHub was connected (zero = not connected)
	GitLabCredentials    string    // JSON-encoded OAuth credentials (access_token, refresh_token, expires_at, base_url)
	GitLabCredentialsAt  time.Time // When GitLab was connected
	ForgejoCredentials   string    // JSON-encoded OAuth credentials for Forgejo/Codeberg
	ForgejoCredentialsAt time.Time // When Forgejo was connected
	CreatedAt            time.Time

	// Storage quota (fair use limits)
	Tier             UserTier // "free" or "pro"
	StorageUsedBytes int64    // Total storage used across all jobs
}

// StorageQuota returns the storage quota in bytes for this user's tier.
func (u *User) StorageQuota() int64 {
	if u.Tier == UserTierPro {
		return StorageQuotaPro
	}
	return StorageQuotaFree
}

// IsOverQuota returns true if the user has exceeded their storage quota.
func (u *User) IsOverQuota() bool {
	return u.StorageUsedBytes >= u.StorageQuota()
}

// Repo represents a configured repository.
type Repo struct {
	ID            string
	ForgeType     ForgeType
	Owner         string // e.g., "user" or "org" (forge owner, not Cinch user)
	Name          string // e.g., "repo"
	CloneURL      string
	HTMLURL       string
	WebhookSecret string
	ForgeToken    string            // Token for API calls (status posting, cloning private repos)
	Build         string            // Build command (e.g., "make check") - runs on branches/PRs
	Release       string            // Release command (e.g., "make release") - runs on tags
	Workers       []string          // Worker labels for fan-out (e.g., ["linux-amd64", "linux-arm64"])
	Secrets       map[string]string // Environment secrets injected into jobs (encrypted at rest)
	Private       bool              // Whether the repo is private
	CreatedAt     time.Time
}

// Token represents a worker authentication token.
type Token struct {
	ID        string
	Name      string
	Hash      string // bcrypt hash of the token
	WorkerID  *string
	CreatedAt time.Time
	RevokedAt *time.Time
}

// LogEntry represents a chunk of log output.
type LogEntry struct {
	ID        int64
	JobID     string
	Stream    string // "stdout" or "stderr"
	Data      string
	CreatedAt time.Time
}
