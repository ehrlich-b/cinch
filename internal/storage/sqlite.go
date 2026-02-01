package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/ehrlich-b/cinch/internal/crypto"
	_ "modernc.org/sqlite"
)

// SQLiteStorage implements Storage using SQLite.
type SQLiteStorage struct {
	db     *sql.DB
	cipher *crypto.Cipher // nil = no encryption (tests)
	log    *slog.Logger
}

// NewSQLite creates a new SQLite storage.
// Use ":memory:" for in-memory database, or a file path for persistent storage.
// If encryptionSecret is provided, sensitive fields are encrypted at rest.
func NewSQLite(dsn string, encryptionSecret string) (*SQLiteStorage, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable foreign keys, WAL mode, and busy timeout for better concurrency
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}
	if dsn != ":memory:" {
		if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
			db.Close()
			return nil, fmt.Errorf("enable WAL: %w", err)
		}
	}

	var cipher *crypto.Cipher
	if encryptionSecret != "" {
		cipher, err = crypto.NewCipher(encryptionSecret)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("create cipher: %w", err)
		}
	}

	s := &SQLiteStorage{db: db, cipher: cipher, log: slog.Default()}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

func (s *SQLiteStorage) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS workers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			labels TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'offline',
			last_seen DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS repos (
			id TEXT PRIMARY KEY,
			forge_type TEXT NOT NULL,
			owner TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL DEFAULT '',
			clone_url TEXT NOT NULL UNIQUE,
			html_url TEXT NOT NULL DEFAULT '',
			webhook_secret TEXT NOT NULL,
			forge_token TEXT NOT NULL DEFAULT '',
			build TEXT NOT NULL DEFAULT 'make check',
			release TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS jobs (
			id TEXT PRIMARY KEY,
			repo_id TEXT NOT NULL,
			commit_sha TEXT NOT NULL,
			branch TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			exit_code INTEGER,
			worker_id TEXT,
			installation_id INTEGER,
			check_run_id INTEGER,
			started_at DATETIME,
			finished_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (repo_id) REFERENCES repos(id),
			FOREIGN KEY (worker_id) REFERENCES workers(id)
		)`,
		`CREATE TABLE IF NOT EXISTS job_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			job_id TEXT NOT NULL,
			stream TEXT NOT NULL,
			data TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (job_id) REFERENCES jobs(id)
		)`,
		`CREATE TABLE IF NOT EXISTS tokens (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			hash TEXT NOT NULL UNIQUE,
			worker_id TEXT,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			revoked_at DATETIME,
			FOREIGN KEY (worker_id) REFERENCES workers(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_repo_id ON jobs(repo_id)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status)`,
		`CREATE INDEX IF NOT EXISTS idx_job_logs_job_id ON job_logs(job_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tokens_hash ON tokens(hash)`,
	}

	for _, m := range migrations {
		if _, err := s.db.Exec(m); err != nil {
			return fmt.Errorf("execute migration: %w", err)
		}
	}

	// Add columns to existing tables (ignore errors - columns may already exist)
	_, _ = s.db.Exec("ALTER TABLE jobs ADD COLUMN installation_id INTEGER")
	_, _ = s.db.Exec("ALTER TABLE jobs ADD COLUMN check_run_id INTEGER")
	_, _ = s.db.Exec("ALTER TABLE jobs ADD COLUMN tag TEXT NOT NULL DEFAULT ''")
	_, _ = s.db.Exec("ALTER TABLE jobs ADD COLUMN pr_number INTEGER")
	_, _ = s.db.Exec("ALTER TABLE jobs ADD COLUMN pr_base_branch TEXT NOT NULL DEFAULT ''")
	_, _ = s.db.Exec("ALTER TABLE repos ADD COLUMN build TEXT NOT NULL DEFAULT 'make check'")
	_, _ = s.db.Exec("ALTER TABLE repos ADD COLUMN release TEXT NOT NULL DEFAULT ''")

	// Users table for storing forge credentials
	_, _ = s.db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		gitlab_credentials TEXT NOT NULL DEFAULT '',
		gitlab_credentials_at DATETIME,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)

	// Add Forgejo credentials columns
	_, _ = s.db.Exec("ALTER TABLE users ADD COLUMN forgejo_credentials TEXT NOT NULL DEFAULT ''")
	_, _ = s.db.Exec("ALTER TABLE users ADD COLUMN forgejo_credentials_at DATETIME")

	// Add email and GitHub connection tracking
	_, _ = s.db.Exec("ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT ''")
	_, _ = s.db.Exec("ALTER TABLE users ADD COLUMN emails TEXT NOT NULL DEFAULT ''") // JSON array
	_, _ = s.db.Exec("ALTER TABLE users ADD COLUMN github_connected_at DATETIME")
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_users_email ON users(email)")

	// Add private column to repos
	_, _ = s.db.Exec("ALTER TABLE repos ADD COLUMN private INTEGER NOT NULL DEFAULT 0")
	// Add workers column to repos (comma-separated labels for fan-out)
	_, _ = s.db.Exec("ALTER TABLE repos ADD COLUMN workers TEXT NOT NULL DEFAULT ''")
	// Add secrets column to repos (encrypted JSON map of env vars)
	_, _ = s.db.Exec("ALTER TABLE repos ADD COLUMN secrets TEXT NOT NULL DEFAULT ''")
	// Index for efficient forge/owner/name lookups
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_repos_forge_owner_name ON repos(forge_type, owner, name)")

	// Worker trust model: add author tracking to jobs
	_, _ = s.db.Exec("ALTER TABLE jobs ADD COLUMN author TEXT NOT NULL DEFAULT ''")
	_, _ = s.db.Exec("ALTER TABLE jobs ADD COLUMN trust_level TEXT NOT NULL DEFAULT 'collaborator'")
	_, _ = s.db.Exec("ALTER TABLE jobs ADD COLUMN is_fork INTEGER NOT NULL DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE jobs ADD COLUMN approved_by TEXT")
	_, _ = s.db.Exec("ALTER TABLE jobs ADD COLUMN approved_at DATETIME")
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_jobs_author ON jobs(author)")

	// Worker visibility: add owner tracking to workers
	_, _ = s.db.Exec("ALTER TABLE workers ADD COLUMN owner_name TEXT NOT NULL DEFAULT ''")
	_, _ = s.db.Exec("ALTER TABLE workers ADD COLUMN mode TEXT NOT NULL DEFAULT 'personal'")
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_workers_owner_name ON workers(owner_name)")

	// Storage quota: add tier and usage tracking to users
	_, _ = s.db.Exec("ALTER TABLE users ADD COLUMN tier TEXT NOT NULL DEFAULT 'free'")
	_, _ = s.db.Exec("ALTER TABLE users ADD COLUMN storage_used_bytes INTEGER NOT NULL DEFAULT 0")

	// Storage tracking: add log size to jobs
	_, _ = s.db.Exec("ALTER TABLE jobs ADD COLUMN log_size_bytes INTEGER NOT NULL DEFAULT 0")

	// Org billing tables for Team Pro
	_, _ = s.db.Exec(`CREATE TABLE IF NOT EXISTS org_billing (
		id TEXT PRIMARY KEY,
		forge_type TEXT NOT NULL,
		forge_org TEXT NOT NULL,
		owner_user_id TEXT NOT NULL,
		stripe_customer_id TEXT NOT NULL DEFAULT '',
		stripe_subscription_id TEXT NOT NULL DEFAULT '',
		stripe_subscription_item_id TEXT NOT NULL DEFAULT '',
		seat_limit INTEGER NOT NULL DEFAULT 5,
		seats_used INTEGER NOT NULL DEFAULT 0,
		storage_used_bytes INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'active',
		period_start DATETIME,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(forge_type, forge_org)
	)`)
	_, _ = s.db.Exec(`CREATE TABLE IF NOT EXISTS org_seats (
		org_billing_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		forge_username TEXT NOT NULL DEFAULT '',
		consumed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (org_billing_id, user_id),
		FOREIGN KEY (org_billing_id) REFERENCES org_billing(id)
	)`)

	// Relay table for webhook forwarding to self-hosted servers
	_, _ = s.db.Exec(`CREATE TABLE IF NOT EXISTS relays (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_relays_user_id ON relays(user_id)")

	// Drop UNIQUE constraint on users.name (email is the identity, not username)
	// SQLite doesn't support ALTER TABLE DROP CONSTRAINT, so we recreate the table
	if s.hasUniqueConstraintOnUsersName() {
		_, _ = s.db.Exec(`CREATE TABLE users_new (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT NOT NULL DEFAULT '',
			emails TEXT NOT NULL DEFAULT '',
			github_connected_at DATETIME,
			gitlab_credentials TEXT NOT NULL DEFAULT '',
			gitlab_credentials_at DATETIME,
			forgejo_credentials TEXT NOT NULL DEFAULT '',
			forgejo_credentials_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`)
		_, _ = s.db.Exec(`INSERT INTO users_new SELECT id, name, email, emails, github_connected_at,
			gitlab_credentials, gitlab_credentials_at, forgejo_credentials, forgejo_credentials_at, created_at FROM users`)
		_, _ = s.db.Exec(`DROP TABLE users`)
		_, _ = s.db.Exec(`ALTER TABLE users_new RENAME TO users`)
		_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_users_email ON users(email)")
	}

	// Encrypt existing plaintext secrets if cipher is configured
	if s.cipher != nil {
		if err := s.migrateEncryptSecrets(); err != nil {
			return fmt.Errorf("encrypt secrets migration: %w", err)
		}
	}

	return nil
}

// migrateEncryptSecrets encrypts any plaintext secrets that haven't been encrypted yet.
func (s *SQLiteStorage) migrateEncryptSecrets() error {
	// Encrypt repos.webhook_secret and repos.forge_token
	rows, err := s.db.Query(`SELECT id, webhook_secret, forge_token FROM repos`)
	if err != nil {
		return err
	}
	var repoUpdates []struct {
		id, webhookSecret, forgeToken string
	}
	for rows.Next() {
		var id, webhookSecret, forgeToken string
		if err := rows.Scan(&id, &webhookSecret, &forgeToken); err != nil {
			rows.Close()
			return err
		}
		needsUpdate := false
		if webhookSecret != "" && !crypto.IsEncrypted(webhookSecret) {
			enc, err := s.cipher.Encrypt(webhookSecret)
			if err != nil {
				rows.Close()
				return err
			}
			webhookSecret = enc
			needsUpdate = true
		}
		if forgeToken != "" && !crypto.IsEncrypted(forgeToken) {
			enc, err := s.cipher.Encrypt(forgeToken)
			if err != nil {
				rows.Close()
				return err
			}
			forgeToken = enc
			needsUpdate = true
		}
		if needsUpdate {
			repoUpdates = append(repoUpdates, struct{ id, webhookSecret, forgeToken string }{id, webhookSecret, forgeToken})
		}
	}
	rows.Close()

	for _, u := range repoUpdates {
		if _, err := s.db.Exec(`UPDATE repos SET webhook_secret = ?, forge_token = ? WHERE id = ?`,
			u.webhookSecret, u.forgeToken, u.id); err != nil {
			return err
		}
		s.log.Info("encrypted repo secrets", "repo_id", u.id)
	}

	// Encrypt users.gitlab_credentials and users.forgejo_credentials
	rows, err = s.db.Query(`SELECT id, gitlab_credentials, forgejo_credentials FROM users`)
	if err != nil {
		return err
	}
	var userUpdates []struct {
		id, gitlabCreds, forgejoCreds string
	}
	for rows.Next() {
		var id, gitlabCreds, forgejoCreds string
		if err := rows.Scan(&id, &gitlabCreds, &forgejoCreds); err != nil {
			rows.Close()
			return err
		}
		needsUpdate := false
		if gitlabCreds != "" && !crypto.IsEncrypted(gitlabCreds) {
			enc, err := s.cipher.Encrypt(gitlabCreds)
			if err != nil {
				rows.Close()
				return err
			}
			gitlabCreds = enc
			needsUpdate = true
		}
		if forgejoCreds != "" && !crypto.IsEncrypted(forgejoCreds) {
			enc, err := s.cipher.Encrypt(forgejoCreds)
			if err != nil {
				rows.Close()
				return err
			}
			forgejoCreds = enc
			needsUpdate = true
		}
		if needsUpdate {
			userUpdates = append(userUpdates, struct{ id, gitlabCreds, forgejoCreds string }{id, gitlabCreds, forgejoCreds})
		}
	}
	rows.Close()

	for _, u := range userUpdates {
		if _, err := s.db.Exec(`UPDATE users SET gitlab_credentials = ?, forgejo_credentials = ? WHERE id = ?`,
			u.gitlabCreds, u.forgejoCreds, u.id); err != nil {
			return err
		}
		s.log.Info("encrypted user credentials", "user_id", u.id)
	}

	return nil
}

// encrypt encrypts a value if cipher is configured.
func (s *SQLiteStorage) encrypt(plaintext string) (string, error) {
	if s.cipher == nil || plaintext == "" {
		return plaintext, nil
	}
	return s.cipher.Encrypt(plaintext)
}

// decrypt decrypts a value if cipher is configured.
func (s *SQLiteStorage) decrypt(ciphertext string) (string, error) {
	if s.cipher == nil || ciphertext == "" {
		return ciphertext, nil
	}
	return s.cipher.Decrypt(ciphertext)
}

// decryptUserCredentials decrypts the user's forge credentials.
func (s *SQLiteStorage) decryptUserCredentials(user *User) error {
	var err error
	if user.GitLabCredentials, err = s.decrypt(user.GitLabCredentials); err != nil {
		return fmt.Errorf("decrypt gitlab_credentials: %w", err)
	}
	if user.ForgejoCredentials, err = s.decrypt(user.ForgejoCredentials); err != nil {
		return fmt.Errorf("decrypt forgejo_credentials: %w", err)
	}
	return nil
}

// hasUniqueConstraintOnUsersName checks if users.name has a UNIQUE constraint.
func (s *SQLiteStorage) hasUniqueConstraintOnUsersName() bool {
	var sql string
	err := s.db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='users'").Scan(&sql)
	if err != nil {
		return false
	}
	return strings.Contains(sql, "name TEXT NOT NULL UNIQUE")
}

func (s *SQLiteStorage) Close() error {
	return s.db.Close()
}

// --- Jobs ---

func (s *SQLiteStorage) CreateJob(ctx context.Context, job *Job) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO jobs (id, repo_id, commit_sha, branch, tag, pr_number, pr_base_branch, status, installation_id, check_run_id, created_at, author, trust_level, is_fork, approved_by, approved_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.RepoID, job.Commit, job.Branch, job.Tag, job.PRNumber, job.PRBaseBranch, job.Status, job.InstallationID, job.CheckRunID, job.CreatedAt,
		job.Author, job.TrustLevel, job.IsFork, job.ApprovedBy, job.ApprovedAt)
	return err
}

func (s *SQLiteStorage) GetJob(ctx context.Context, id string) (*Job, error) {
	job := &Job{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, commit_sha, branch, tag, pr_number, pr_base_branch, status, exit_code, worker_id,
		        installation_id, check_run_id, started_at, finished_at, created_at,
		        author, trust_level, is_fork, approved_by, approved_at
		 FROM jobs WHERE id = ?`, id).Scan(
		&job.ID, &job.RepoID, &job.Commit, &job.Branch, &job.Tag, &job.PRNumber, &job.PRBaseBranch, &job.Status,
		&job.ExitCode, &job.WorkerID, &job.InstallationID, &job.CheckRunID, &job.StartedAt, &job.FinishedAt, &job.CreatedAt,
		&job.Author, &job.TrustLevel, &job.IsFork, &job.ApprovedBy, &job.ApprovedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return job, err
}

func (s *SQLiteStorage) GetJobSiblings(ctx context.Context, repoID, commit, excludeJobID string) ([]*Job, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, repo_id, commit_sha, branch, tag, pr_number, pr_base_branch, status, exit_code, worker_id,
		        installation_id, check_run_id, started_at, finished_at, created_at,
		        author, trust_level, is_fork, approved_by, approved_at
		 FROM jobs WHERE repo_id = ? AND commit_sha = ? AND id != ?
		 ORDER BY created_at DESC`, repoID, commit, excludeJobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		job := &Job{}
		if err := rows.Scan(
			&job.ID, &job.RepoID, &job.Commit, &job.Branch, &job.Tag, &job.PRNumber, &job.PRBaseBranch, &job.Status,
			&job.ExitCode, &job.WorkerID, &job.InstallationID, &job.CheckRunID, &job.StartedAt, &job.FinishedAt, &job.CreatedAt,
			&job.Author, &job.TrustLevel, &job.IsFork, &job.ApprovedBy, &job.ApprovedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *SQLiteStorage) UpdateJobCheckRunID(ctx context.Context, id string, checkRunID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET check_run_id = ? WHERE id = ?`,
		checkRunID, id)
	return err
}

func (s *SQLiteStorage) ListJobs(ctx context.Context, filter JobFilter) ([]*Job, error) {
	query := `SELECT id, repo_id, commit_sha, branch, tag, pr_number, pr_base_branch, status, exit_code, worker_id,
	                 installation_id, check_run_id, started_at, finished_at, created_at,
	                 author, trust_level, is_fork, approved_by, approved_at FROM jobs WHERE 1=1`
	args := []any{}

	if filter.RepoID != "" {
		query += " AND repo_id = ?"
		args = append(args, filter.RepoID)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}
	if filter.Branch != "" {
		query += " AND branch = ?"
		args = append(args, filter.Branch)
	}

	query += " ORDER BY created_at DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		job := &Job{}
		if err := rows.Scan(&job.ID, &job.RepoID, &job.Commit, &job.Branch, &job.Tag, &job.PRNumber, &job.PRBaseBranch,
			&job.Status, &job.ExitCode, &job.WorkerID, &job.InstallationID, &job.CheckRunID, &job.StartedAt,
			&job.FinishedAt, &job.CreatedAt,
			&job.Author, &job.TrustLevel, &job.IsFork, &job.ApprovedBy, &job.ApprovedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *SQLiteStorage) ListJobsByWorker(ctx context.Context, workerID string, limit int) ([]*Job, error) {
	if limit <= 0 {
		limit = 10
	}

	query := `SELECT id, repo_id, commit_sha, branch, tag, pr_number, pr_base_branch, status, exit_code, worker_id,
	                 installation_id, check_run_id, started_at, finished_at, created_at,
	                 author, trust_level, is_fork, approved_by, approved_at
	          FROM jobs WHERE worker_id = ? ORDER BY created_at DESC LIMIT ?`

	rows, err := s.db.QueryContext(ctx, query, workerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		job := &Job{}
		if err := rows.Scan(&job.ID, &job.RepoID, &job.Commit, &job.Branch, &job.Tag, &job.PRNumber, &job.PRBaseBranch,
			&job.Status, &job.ExitCode, &job.WorkerID, &job.InstallationID, &job.CheckRunID, &job.StartedAt,
			&job.FinishedAt, &job.CreatedAt,
			&job.Author, &job.TrustLevel, &job.IsFork, &job.ApprovedBy, &job.ApprovedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *SQLiteStorage) UpdateJobStatus(ctx context.Context, id string, status JobStatus, exitCode *int) error {
	var err error
	now := time.Now()

	switch status {
	case JobStatusRunning:
		_, err = s.db.ExecContext(ctx,
			`UPDATE jobs SET status = ?, started_at = ? WHERE id = ?`,
			status, now, id)
	case JobStatusSuccess, JobStatusFailed, JobStatusCancelled, JobStatusError:
		_, err = s.db.ExecContext(ctx,
			`UPDATE jobs SET status = ?, exit_code = ?, finished_at = ? WHERE id = ?`,
			status, exitCode, now, id)
	default:
		_, err = s.db.ExecContext(ctx,
			`UPDATE jobs SET status = ? WHERE id = ?`,
			status, id)
	}
	return err
}

func (s *SQLiteStorage) UpdateJobWorker(ctx context.Context, jobID, workerID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET worker_id = ? WHERE id = ?`,
		workerID, jobID)
	return err
}

func (s *SQLiteStorage) ApproveJob(ctx context.Context, jobID, approvedBy string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET approved_by = ?, approved_at = ?, status = ? WHERE id = ? AND status = ?`,
		approvedBy, time.Now(), JobStatusPending, jobID, JobStatusPendingContributor)
	return err
}

// --- Workers ---

func (s *SQLiteStorage) CreateWorker(ctx context.Context, worker *Worker) error {
	labels := strings.Join(worker.Labels, ",")
	mode := worker.Mode
	if mode == "" {
		mode = "personal"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO workers (id, name, labels, status, last_seen, created_at, owner_name, mode)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		worker.ID, worker.Name, labels, worker.Status, worker.LastSeen, worker.CreatedAt, worker.OwnerName, mode)
	return err
}

func (s *SQLiteStorage) GetWorker(ctx context.Context, id string) (*Worker, error) {
	worker := &Worker{}
	var labels string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, labels, status, last_seen, created_at, owner_name, mode
		 FROM workers WHERE id = ?`, id).Scan(
		&worker.ID, &worker.Name, &labels, &worker.Status, &worker.LastSeen, &worker.CreatedAt,
		&worker.OwnerName, &worker.Mode)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if labels != "" {
		worker.Labels = strings.Split(labels, ",")
	}
	return worker, nil
}

func (s *SQLiteStorage) ListWorkers(ctx context.Context) ([]*Worker, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, labels, status, last_seen, created_at, owner_name, mode FROM workers ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workers []*Worker
	for rows.Next() {
		worker := &Worker{}
		var labels string
		if err := rows.Scan(&worker.ID, &worker.Name, &labels, &worker.Status,
			&worker.LastSeen, &worker.CreatedAt, &worker.OwnerName, &worker.Mode); err != nil {
			return nil, err
		}
		if labels != "" {
			worker.Labels = strings.Split(labels, ",")
		}
		workers = append(workers, worker)
	}
	return workers, rows.Err()
}

func (s *SQLiteStorage) CountWorkersByOwner(ctx context.Context, ownerName string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workers WHERE owner_name = ?`,
		ownerName).Scan(&count)
	return count, err
}

func (s *SQLiteStorage) UpdateWorkerLastSeen(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE workers SET last_seen = ? WHERE id = ?`,
		time.Now(), id)
	return err
}

func (s *SQLiteStorage) UpdateWorkerStatus(ctx context.Context, id string, status WorkerStatus) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE workers SET status = ? WHERE id = ?`,
		status, id)
	return err
}

func (s *SQLiteStorage) DeleteWorker(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM workers WHERE id = ?`, id)
	return err
}

func (s *SQLiteStorage) UpdateWorkerOwner(ctx context.Context, id, ownerName, mode string) error {
	if mode == "" {
		mode = "personal"
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE workers SET owner_name = ?, mode = ? WHERE id = ?`,
		ownerName, mode, id)
	return err
}

// --- Repos ---

func (s *SQLiteStorage) CreateRepo(ctx context.Context, repo *Repo) error {
	// Encrypt secrets before storing
	webhookSecret, err := s.encrypt(repo.WebhookSecret)
	if err != nil {
		return fmt.Errorf("encrypt webhook_secret: %w", err)
	}
	forgeToken, err := s.encrypt(repo.ForgeToken)
	if err != nil {
		return fmt.Errorf("encrypt forge_token: %w", err)
	}

	// Convert workers slice to comma-separated string
	workers := strings.Join(repo.Workers, ",")

	// Convert and encrypt secrets map to JSON
	var secretsJSON string
	if len(repo.Secrets) > 0 {
		secretsBytes, err := json.Marshal(repo.Secrets)
		if err != nil {
			return fmt.Errorf("marshal secrets: %w", err)
		}
		secretsJSON, err = s.encrypt(string(secretsBytes))
		if err != nil {
			return fmt.Errorf("encrypt secrets: %w", err)
		}
	}

	// Use upsert to handle re-onboarding: if repo exists, update token and webhook secret
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO repos (id, forge_type, owner, name, clone_url, html_url, webhook_secret, forge_token, build, release, workers, secrets, private, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(clone_url) DO UPDATE SET
		 	webhook_secret = excluded.webhook_secret,
		 	forge_token = excluded.forge_token,
		 	workers = excluded.workers,
		 	private = excluded.private`,
		repo.ID, repo.ForgeType, repo.Owner, repo.Name, repo.CloneURL, repo.HTMLURL,
		webhookSecret, forgeToken, repo.Build, repo.Release, workers, secretsJSON, repo.Private, repo.CreatedAt)
	return err
}

func (s *SQLiteStorage) GetRepo(ctx context.Context, id string) (*Repo, error) {
	repo := &Repo{}
	var workers, secretsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, forge_type, owner, name, clone_url, html_url, webhook_secret, forge_token, build, release, workers, secrets, private, created_at
		 FROM repos WHERE id = ?`, id).Scan(
		&repo.ID, &repo.ForgeType, &repo.Owner, &repo.Name, &repo.CloneURL, &repo.HTMLURL,
		&repo.WebhookSecret, &repo.ForgeToken, &repo.Build, &repo.Release, &workers, &secretsJSON, &repo.Private, &repo.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	// Parse workers from comma-separated string
	if workers != "" {
		repo.Workers = strings.Split(workers, ",")
	}
	// Decrypt secrets
	if repo.WebhookSecret, err = s.decrypt(repo.WebhookSecret); err != nil {
		return nil, fmt.Errorf("decrypt webhook_secret: %w", err)
	}
	if repo.ForgeToken, err = s.decrypt(repo.ForgeToken); err != nil {
		return nil, fmt.Errorf("decrypt forge_token: %w", err)
	}
	// Decrypt and parse secrets map
	if secretsJSON != "" {
		decrypted, err := s.decrypt(secretsJSON)
		if err != nil {
			return nil, fmt.Errorf("decrypt secrets: %w", err)
		}
		if decrypted != "" {
			if err := json.Unmarshal([]byte(decrypted), &repo.Secrets); err != nil {
				return nil, fmt.Errorf("unmarshal secrets: %w", err)
			}
		}
	}
	return repo, nil
}

func (s *SQLiteStorage) GetRepoByCloneURL(ctx context.Context, cloneURL string) (*Repo, error) {
	repo := &Repo{}
	var workers, secretsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, forge_type, owner, name, clone_url, html_url, webhook_secret, forge_token, build, release, workers, secrets, private, created_at
		 FROM repos WHERE clone_url = ?`, cloneURL).Scan(
		&repo.ID, &repo.ForgeType, &repo.Owner, &repo.Name, &repo.CloneURL, &repo.HTMLURL,
		&repo.WebhookSecret, &repo.ForgeToken, &repo.Build, &repo.Release, &workers, &secretsJSON, &repo.Private, &repo.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	// Parse workers from comma-separated string
	if workers != "" {
		repo.Workers = strings.Split(workers, ",")
	}
	// Decrypt secrets
	if repo.WebhookSecret, err = s.decrypt(repo.WebhookSecret); err != nil {
		return nil, fmt.Errorf("decrypt webhook_secret: %w", err)
	}
	if repo.ForgeToken, err = s.decrypt(repo.ForgeToken); err != nil {
		return nil, fmt.Errorf("decrypt forge_token: %w", err)
	}
	// Decrypt and parse secrets map
	if secretsJSON != "" {
		decrypted, err := s.decrypt(secretsJSON)
		if err != nil {
			return nil, fmt.Errorf("decrypt secrets: %w", err)
		}
		if decrypted != "" {
			if err := json.Unmarshal([]byte(decrypted), &repo.Secrets); err != nil {
				return nil, fmt.Errorf("unmarshal secrets: %w", err)
			}
		}
	}
	return repo, nil
}

func (s *SQLiteStorage) ListRepos(ctx context.Context) ([]*Repo, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, forge_type, owner, name, clone_url, html_url, webhook_secret, forge_token, build, release, workers, secrets, private, created_at
		 FROM repos ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repos []*Repo
	for rows.Next() {
		repo := &Repo{}
		var workers, secretsJSON string
		if err := rows.Scan(&repo.ID, &repo.ForgeType, &repo.Owner, &repo.Name, &repo.CloneURL,
			&repo.HTMLURL, &repo.WebhookSecret, &repo.ForgeToken, &repo.Build, &repo.Release, &workers, &secretsJSON, &repo.Private, &repo.CreatedAt); err != nil {
			return nil, err
		}
		// Parse workers from comma-separated string
		if workers != "" {
			repo.Workers = strings.Split(workers, ",")
		}
		// Decrypt secrets
		if repo.WebhookSecret, err = s.decrypt(repo.WebhookSecret); err != nil {
			return nil, fmt.Errorf("decrypt webhook_secret: %w", err)
		}
		if repo.ForgeToken, err = s.decrypt(repo.ForgeToken); err != nil {
			return nil, fmt.Errorf("decrypt forge_token: %w", err)
		}
		// Decrypt and parse secrets map
		if secretsJSON != "" {
			decrypted, err := s.decrypt(secretsJSON)
			if err != nil {
				return nil, fmt.Errorf("decrypt secrets: %w", err)
			}
			if decrypted != "" {
				if err := json.Unmarshal([]byte(decrypted), &repo.Secrets); err != nil {
					return nil, fmt.Errorf("unmarshal secrets: %w", err)
				}
			}
		}
		repos = append(repos, repo)
	}
	return repos, rows.Err()
}

func (s *SQLiteStorage) DeleteRepo(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM repos WHERE id = ?`, id)
	return err
}

func (s *SQLiteStorage) GetRepoByOwnerName(ctx context.Context, forge, owner, name string) (*Repo, error) {
	repo := &Repo{}
	var workers, secretsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, forge_type, owner, name, clone_url, html_url, webhook_secret, forge_token, build, release, workers, secrets, private, created_at
		 FROM repos WHERE forge_type = ? AND owner = ? AND name = ?`, forge, owner, name).Scan(
		&repo.ID, &repo.ForgeType, &repo.Owner, &repo.Name, &repo.CloneURL, &repo.HTMLURL,
		&repo.WebhookSecret, &repo.ForgeToken, &repo.Build, &repo.Release, &workers, &secretsJSON, &repo.Private, &repo.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	// Parse workers from comma-separated string
	if workers != "" {
		repo.Workers = strings.Split(workers, ",")
	}
	// Decrypt secrets
	if repo.WebhookSecret, err = s.decrypt(repo.WebhookSecret); err != nil {
		return nil, fmt.Errorf("decrypt webhook_secret: %w", err)
	}
	if repo.ForgeToken, err = s.decrypt(repo.ForgeToken); err != nil {
		return nil, fmt.Errorf("decrypt forge_token: %w", err)
	}
	// Decrypt and parse secrets map
	if secretsJSON != "" {
		decrypted, err := s.decrypt(secretsJSON)
		if err != nil {
			return nil, fmt.Errorf("decrypt secrets: %w", err)
		}
		if decrypted != "" {
			if err := json.Unmarshal([]byte(decrypted), &repo.Secrets); err != nil {
				return nil, fmt.Errorf("unmarshal secrets: %w", err)
			}
		}
	}
	return repo, nil
}

func (s *SQLiteStorage) UpdateRepoPrivate(ctx context.Context, id string, private bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE repos SET private = ? WHERE id = ?`,
		private, id)
	return err
}

func (s *SQLiteStorage) UpdateRepoSecrets(ctx context.Context, id string, secrets map[string]string) error {
	// Convert and encrypt secrets map to JSON
	var secretsJSON string
	if len(secrets) > 0 {
		secretsBytes, err := json.Marshal(secrets)
		if err != nil {
			return fmt.Errorf("marshal secrets: %w", err)
		}
		secretsJSON, err = s.encrypt(string(secretsBytes))
		if err != nil {
			return fmt.Errorf("encrypt secrets: %w", err)
		}
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE repos SET secrets = ? WHERE id = ?`,
		secretsJSON, id)
	return err
}

// --- Tokens ---

func (s *SQLiteStorage) CreateToken(ctx context.Context, token *Token) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tokens (id, name, hash, worker_id, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		token.ID, token.Name, token.Hash, token.WorkerID, token.CreatedAt)
	return err
}

func (s *SQLiteStorage) GetTokenByHash(ctx context.Context, hash string) (*Token, error) {
	token := &Token{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, hash, worker_id, created_at, revoked_at
		 FROM tokens WHERE hash = ? AND revoked_at IS NULL`, hash).Scan(
		&token.ID, &token.Name, &token.Hash, &token.WorkerID, &token.CreatedAt, &token.RevokedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return token, err
}

func (s *SQLiteStorage) ListTokens(ctx context.Context) ([]*Token, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, hash, worker_id, created_at, revoked_at FROM tokens ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []*Token
	for rows.Next() {
		token := &Token{}
		if err := rows.Scan(&token.ID, &token.Name, &token.Hash,
			&token.WorkerID, &token.CreatedAt, &token.RevokedAt); err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

func (s *SQLiteStorage) RevokeToken(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tokens SET revoked_at = ? WHERE id = ?`,
		time.Now(), id)
	return err
}

// --- Users ---

func (s *SQLiteStorage) GetOrCreateUser(ctx context.Context, name string) (*User, error) {
	user := &User{}
	var gitlabCredentialsAt, forgejoCredentialsAt, githubConnectedAt sql.NullTime
	var emailsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, email, emails, github_connected_at, gitlab_credentials, gitlab_credentials_at,
		        forgejo_credentials, forgejo_credentials_at, created_at
		 FROM users WHERE name = ?`, name).Scan(
		&user.ID, &user.Name, &user.Email, &emailsJSON, &githubConnectedAt,
		&user.GitLabCredentials, &gitlabCredentialsAt,
		&user.ForgejoCredentials, &forgejoCredentialsAt, &user.CreatedAt)
	if err == nil {
		if gitlabCredentialsAt.Valid {
			user.GitLabCredentialsAt = gitlabCredentialsAt.Time
		}
		if forgejoCredentialsAt.Valid {
			user.ForgejoCredentialsAt = forgejoCredentialsAt.Time
		}
		if githubConnectedAt.Valid {
			user.GitHubConnectedAt = githubConnectedAt.Time
		}
		if emailsJSON != "" {
			user.Emails = parseEmailsJSON(emailsJSON)
		}
		// Decrypt credentials
		if err := s.decryptUserCredentials(user); err != nil {
			return nil, err
		}
		return user, nil
	}
	if err == sql.ErrNoRows {
		// Create new user
		user = &User{
			ID:        fmt.Sprintf("u_%d", time.Now().UnixNano()),
			Name:      name,
			CreatedAt: time.Now(),
		}
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO users (id, name, created_at) VALUES (?, ?, ?)`,
			user.ID, user.Name, user.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("create user: %w", err)
		}
		return user, nil
	}
	return nil, err
}

func (s *SQLiteStorage) GetOrCreateUserByEmail(ctx context.Context, email, name string) (*User, error) {
	user := &User{}
	var gitlabCredentialsAt, forgejoCredentialsAt, githubConnectedAt sql.NullTime
	var emailsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, email, emails, github_connected_at, gitlab_credentials, gitlab_credentials_at,
		        forgejo_credentials, forgejo_credentials_at, created_at
		 FROM users WHERE email = ?`, email).Scan(
		&user.ID, &user.Name, &user.Email, &emailsJSON, &githubConnectedAt,
		&user.GitLabCredentials, &gitlabCredentialsAt,
		&user.ForgejoCredentials, &forgejoCredentialsAt, &user.CreatedAt)
	if err == nil {
		if gitlabCredentialsAt.Valid {
			user.GitLabCredentialsAt = gitlabCredentialsAt.Time
		}
		if forgejoCredentialsAt.Valid {
			user.ForgejoCredentialsAt = forgejoCredentialsAt.Time
		}
		if githubConnectedAt.Valid {
			user.GitHubConnectedAt = githubConnectedAt.Time
		}
		if emailsJSON != "" {
			user.Emails = parseEmailsJSON(emailsJSON)
		}
		// Decrypt credentials
		if err := s.decryptUserCredentials(user); err != nil {
			return nil, err
		}
		return user, nil
	}
	if err == sql.ErrNoRows {
		// Create new user with email as primary identity
		user = &User{
			ID:        fmt.Sprintf("u_%d", time.Now().UnixNano()),
			Name:      name,
			Email:     email,
			CreatedAt: time.Now(),
		}
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO users (id, name, email, created_at) VALUES (?, ?, ?, ?)`,
			user.ID, user.Name, user.Email, user.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("create user: %w", err)
		}
		return user, nil
	}
	return nil, err
}

func (s *SQLiteStorage) GetUserByName(ctx context.Context, name string) (*User, error) {
	user := &User{}
	var gitlabCredentialsAt, forgejoCredentialsAt, githubConnectedAt sql.NullTime
	var emailsJSON string
	var tier sql.NullString
	var storageUsed sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, email, emails, github_connected_at, gitlab_credentials, gitlab_credentials_at,
		        forgejo_credentials, forgejo_credentials_at, tier, storage_used_bytes, created_at
		 FROM users WHERE name = ?`, name).Scan(
		&user.ID, &user.Name, &user.Email, &emailsJSON, &githubConnectedAt,
		&user.GitLabCredentials, &gitlabCredentialsAt,
		&user.ForgejoCredentials, &forgejoCredentialsAt, &tier, &storageUsed, &user.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if gitlabCredentialsAt.Valid {
		user.GitLabCredentialsAt = gitlabCredentialsAt.Time
	}
	if forgejoCredentialsAt.Valid {
		user.ForgejoCredentialsAt = forgejoCredentialsAt.Time
	}
	if githubConnectedAt.Valid {
		user.GitHubConnectedAt = githubConnectedAt.Time
	}
	if emailsJSON != "" {
		user.Emails = parseEmailsJSON(emailsJSON)
	}
	if tier.Valid {
		user.Tier = UserTier(tier.String)
	} else {
		user.Tier = UserTierFree
	}
	if storageUsed.Valid {
		user.StorageUsedBytes = storageUsed.Int64
	}
	// Decrypt credentials
	if err := s.decryptUserCredentials(user); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *SQLiteStorage) UpdateUserGitLabCredentials(ctx context.Context, userID, credentials string) error {
	encrypted, err := s.encrypt(credentials)
	if err != nil {
		return fmt.Errorf("encrypt gitlab_credentials: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE users SET gitlab_credentials = ?, gitlab_credentials_at = ? WHERE id = ?`,
		encrypted, time.Now(), userID)
	return err
}

func (s *SQLiteStorage) UpdateUserForgejoCredentials(ctx context.Context, userID, credentials string) error {
	encrypted, err := s.encrypt(credentials)
	if err != nil {
		return fmt.Errorf("encrypt forgejo_credentials: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE users SET forgejo_credentials = ?, forgejo_credentials_at = ? WHERE id = ?`,
		encrypted, time.Now(), userID)
	return err
}

func (s *SQLiteStorage) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	user := &User{}
	var gitlabCredentialsAt, forgejoCredentialsAt, githubConnectedAt sql.NullTime
	var emailsJSON string
	var tier sql.NullString
	var storageUsed sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, email, emails, github_connected_at, gitlab_credentials, gitlab_credentials_at,
		        forgejo_credentials, forgejo_credentials_at, tier, storage_used_bytes, created_at
		 FROM users WHERE email = ? OR emails LIKE ?`, email, "%"+email+"%").Scan(
		&user.ID, &user.Name, &user.Email, &emailsJSON, &githubConnectedAt,
		&user.GitLabCredentials, &gitlabCredentialsAt,
		&user.ForgejoCredentials, &forgejoCredentialsAt, &tier, &storageUsed, &user.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if gitlabCredentialsAt.Valid {
		user.GitLabCredentialsAt = gitlabCredentialsAt.Time
	}
	if forgejoCredentialsAt.Valid {
		user.ForgejoCredentialsAt = forgejoCredentialsAt.Time
	}
	if githubConnectedAt.Valid {
		user.GitHubConnectedAt = githubConnectedAt.Time
	}
	if emailsJSON != "" {
		// Parse JSON array of emails
		user.Emails = parseEmailsJSON(emailsJSON)
	}
	if tier.Valid {
		user.Tier = UserTier(tier.String)
	} else {
		user.Tier = UserTierFree
	}
	if storageUsed.Valid {
		user.StorageUsedBytes = storageUsed.Int64
	}
	// Decrypt credentials
	if err := s.decryptUserCredentials(user); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *SQLiteStorage) UpdateUserEmail(ctx context.Context, userID, email string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET email = ? WHERE id = ?`,
		email, userID)
	return err
}

func (s *SQLiteStorage) AddUserEmail(ctx context.Context, userID, email string) error {
	// Get current emails
	var emailsJSON string
	err := s.db.QueryRowContext(ctx, `SELECT emails FROM users WHERE id = ?`, userID).Scan(&emailsJSON)
	if err != nil {
		return err
	}

	emails := parseEmailsJSON(emailsJSON)
	// Check if email already exists
	if slices.Contains(emails, email) {
		return nil // Already exists
	}
	emails = append(emails, email)

	_, err = s.db.ExecContext(ctx,
		`UPDATE users SET emails = ? WHERE id = ?`,
		formatEmailsJSON(emails), userID)
	return err
}

func (s *SQLiteStorage) UpdateUserGitHubConnected(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET github_connected_at = ? WHERE id = ?`,
		time.Now(), userID)
	return err
}

func (s *SQLiteStorage) UpdateUserGitLabConnected(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET gitlab_credentials_at = ? WHERE id = ?`,
		time.Now(), userID)
	return err
}

func (s *SQLiteStorage) UpdateUserForgejoConnected(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET forgejo_credentials_at = ? WHERE id = ?`,
		time.Now(), userID)
	return err
}

func (s *SQLiteStorage) ClearUserGitLabCredentials(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET gitlab_credentials = '', gitlab_credentials_at = NULL WHERE id = ?`,
		userID)
	return err
}

func (s *SQLiteStorage) ClearUserForgejoCredentials(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET forgejo_credentials = '', forgejo_credentials_at = NULL WHERE id = ?`,
		userID)
	return err
}

func (s *SQLiteStorage) ClearUserGitHubConnected(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET github_connected_at = NULL WHERE id = ?`,
		userID)
	return err
}

func (s *SQLiteStorage) DeleteUser(ctx context.Context, id string) error {
	// Delete user and all associated data
	// Note: repos are not currently linked to users, so they remain
	// In a future version, we might want to cascade delete repos too

	// Delete tokens created by this user (we don't have user_id on tokens yet, so skip)
	// For now, just delete the user record
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

// UpdateJobLogSize updates the log size for a job.
func (s *SQLiteStorage) UpdateJobLogSize(ctx context.Context, jobID string, sizeBytes int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET log_size_bytes = ? WHERE id = ?`,
		sizeBytes, jobID)
	return err
}

// UpdateUserStorageUsed adds deltaBytes to the user's storage usage.
func (s *SQLiteStorage) UpdateUserStorageUsed(ctx context.Context, userID string, deltaBytes int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET storage_used_bytes = storage_used_bytes + ? WHERE id = ?`,
		deltaBytes, userID)
	return err
}

// --- Billing ---

// UpdateUserTier updates a user's subscription tier.
func (s *SQLiteStorage) UpdateUserTier(ctx context.Context, userID string, tier UserTier) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET tier = ? WHERE id = ?`,
		string(tier), userID)
	return err
}

// CreateOrgBilling creates a new org billing record.
func (s *SQLiteStorage) CreateOrgBilling(ctx context.Context, billing *OrgBilling) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO org_billing (id, forge_type, forge_org, owner_user_id, stripe_customer_id,
		 stripe_subscription_id, stripe_subscription_item_id, seat_limit, seats_used,
		 storage_used_bytes, status, period_start, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		billing.ID, string(billing.ForgeType), billing.ForgeOrg, billing.OwnerUserID,
		billing.StripeCustomerID, billing.StripeSubscriptionID, billing.StripeSubscriptionItemID,
		billing.SeatLimit, billing.SeatsUsed, billing.StorageUsedBytes, billing.Status,
		billing.PeriodStart, billing.CreatedAt)
	return err
}

// GetOrgBilling retrieves org billing by forge type and org name.
func (s *SQLiteStorage) GetOrgBilling(ctx context.Context, forgeType ForgeType, forgeOrg string) (*OrgBilling, error) {
	billing := &OrgBilling{}
	var periodStart sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, forge_type, forge_org, owner_user_id, stripe_customer_id,
		        stripe_subscription_id, stripe_subscription_item_id, seat_limit, seats_used,
		        storage_used_bytes, status, period_start, created_at
		 FROM org_billing WHERE forge_type = ? AND forge_org = ?`,
		string(forgeType), forgeOrg).Scan(
		&billing.ID, &billing.ForgeType, &billing.ForgeOrg, &billing.OwnerUserID,
		&billing.StripeCustomerID, &billing.StripeSubscriptionID, &billing.StripeSubscriptionItemID,
		&billing.SeatLimit, &billing.SeatsUsed, &billing.StorageUsedBytes, &billing.Status,
		&periodStart, &billing.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if periodStart.Valid {
		billing.PeriodStart = periodStart.Time
	}
	return billing, nil
}

// UpdateOrgBillingSeatLimit updates the seat limit for an org.
func (s *SQLiteStorage) UpdateOrgBillingSeatLimit(ctx context.Context, id string, seatLimit int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE org_billing SET seat_limit = ? WHERE id = ?`,
		seatLimit, id)
	return err
}

// UpdateOrgBillingSeatsUsed updates the seats used count for an org.
func (s *SQLiteStorage) UpdateOrgBillingSeatsUsed(ctx context.Context, id string, seatsUsed int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE org_billing SET seats_used = ? WHERE id = ?`,
		seatsUsed, id)
	return err
}

// IsOrgSeat checks if a user has consumed a seat in the current billing period.
func (s *SQLiteStorage) IsOrgSeat(ctx context.Context, orgBillingID, userID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM org_seats WHERE org_billing_id = ? AND user_id = ?`,
		orgBillingID, userID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// AddOrgSeat records that a user has consumed a seat this billing period.
func (s *SQLiteStorage) AddOrgSeat(ctx context.Context, orgBillingID, userID, forgeUsername string) error {
	// Use INSERT OR IGNORE to handle race conditions
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO org_seats (org_billing_id, user_id, forge_username, consumed_at)
		 VALUES (?, ?, ?, ?)`,
		orgBillingID, userID, forgeUsername, time.Now())
	if err != nil {
		return err
	}

	// Increment seats_used in org_billing (atomic with check)
	_, err = s.db.ExecContext(ctx,
		`UPDATE org_billing SET seats_used = (SELECT COUNT(*) FROM org_seats WHERE org_billing_id = ?)
		 WHERE id = ?`,
		orgBillingID, orgBillingID)
	return err
}

// ResetOrgSeats clears all seat consumption at the start of a new billing period.
func (s *SQLiteStorage) ResetOrgSeats(ctx context.Context, orgBillingID string) error {
	// Delete all seat records for this org
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM org_seats WHERE org_billing_id = ?`,
		orgBillingID)
	if err != nil {
		return err
	}

	// Reset seats_used counter and update period_start
	_, err = s.db.ExecContext(ctx,
		`UPDATE org_billing SET seats_used = 0, period_start = ? WHERE id = ?`,
		time.Now(), orgBillingID)
	return err
}

// Helper functions for emails JSON
func parseEmailsJSON(s string) []string {
	if s == "" || s == "[]" {
		return nil
	}
	var emails []string
	if err := json.Unmarshal([]byte(s), &emails); err != nil {
		return nil
	}
	return emails
}

func formatEmailsJSON(emails []string) string {
	if len(emails) == 0 {
		return "[]"
	}
	b, err := json.Marshal(emails)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// --- Relays ---

// generateRelayID creates a short random ID for a relay.
func generateRelayID() string {
	// Generate a short 5-character alphanumeric ID
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 5)
	for i := range b {
		b[i] = chars[time.Now().UnixNano()%int64(len(chars))]
		time.Sleep(time.Nanosecond) // Ensure different values
	}
	return string(b)
}

func (s *SQLiteStorage) GetOrCreateRelayID(ctx context.Context, userID string) (string, error) {
	// Check if user already has a relay
	var relayID string
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM relays WHERE user_id = ?`, userID).Scan(&relayID)
	if err == nil {
		return relayID, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}

	// Create new relay
	relayID = generateRelayID()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO relays (id, user_id, created_at) VALUES (?, ?, ?)`,
		relayID, userID, time.Now())
	if err != nil {
		return "", err
	}
	return relayID, nil
}

func (s *SQLiteStorage) GetRelayByID(ctx context.Context, relayID string) (*Relay, error) {
	relay := &Relay{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, created_at FROM relays WHERE id = ?`, relayID).Scan(
		&relay.ID, &relay.UserID, &relay.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return relay, err
}

// --- Logs ---

func (s *SQLiteStorage) AppendLog(ctx context.Context, jobID, stream, data string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO job_logs (job_id, stream, data, created_at)
		 VALUES (?, ?, ?, ?)`,
		jobID, stream, data, time.Now())
	return err
}

func (s *SQLiteStorage) GetLogs(ctx context.Context, jobID string) ([]*LogEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, job_id, stream, data, created_at FROM job_logs WHERE job_id = ? ORDER BY id`,
		jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []*LogEntry
	for rows.Next() {
		log := &LogEntry{}
		if err := rows.Scan(&log.ID, &log.JobID, &log.Stream, &log.Data, &log.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	return logs, rows.Err()
}
