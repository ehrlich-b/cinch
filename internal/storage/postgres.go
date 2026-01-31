package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ehrlich-b/cinch/internal/crypto"
	_ "github.com/lib/pq"
)

// PostgresStorage implements Storage using PostgreSQL.
type PostgresStorage struct {
	db     *sql.DB
	cipher *crypto.Cipher // nil = no encryption (tests)
	log    *slog.Logger
}

// NewPostgres creates a new Postgres storage.
// DSN format: postgres://user:password@host:port/dbname?sslmode=disable
// If encryptionSecret is provided, sensitive fields are encrypted at rest.
func NewPostgres(dsn string, encryptionSecret string) (*PostgresStorage, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	var cipher *crypto.Cipher
	if encryptionSecret != "" {
		cipher, err = crypto.NewCipher(encryptionSecret)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("create cipher: %w", err)
		}
	}

	s := &PostgresStorage{db: db, cipher: cipher, log: slog.Default()}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

func (s *PostgresStorage) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS workers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			labels TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'offline',
			last_seen TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			owner_name TEXT NOT NULL DEFAULT '',
			mode TEXT NOT NULL DEFAULT 'personal'
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
			workers TEXT NOT NULL DEFAULT '',
			secrets TEXT NOT NULL DEFAULT '',
			private BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS jobs (
			id TEXT PRIMARY KEY,
			repo_id TEXT NOT NULL,
			commit_sha TEXT NOT NULL,
			branch TEXT NOT NULL,
			tag TEXT NOT NULL DEFAULT '',
			pr_number INTEGER,
			pr_base_branch TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			exit_code INTEGER,
			worker_id TEXT,
			installation_id BIGINT,
			check_run_id BIGINT,
			started_at TIMESTAMPTZ,
			finished_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			author TEXT NOT NULL DEFAULT '',
			trust_level TEXT NOT NULL DEFAULT 'collaborator',
			is_fork BOOLEAN NOT NULL DEFAULT FALSE,
			approved_by TEXT,
			approved_at TIMESTAMPTZ
		)`,
		`CREATE TABLE IF NOT EXISTS job_logs (
			id BIGSERIAL PRIMARY KEY,
			job_id TEXT NOT NULL,
			stream TEXT NOT NULL,
			data TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS tokens (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			hash TEXT NOT NULL UNIQUE,
			worker_id TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			revoked_at TIMESTAMPTZ
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT NOT NULL DEFAULT '',
			emails TEXT NOT NULL DEFAULT '[]',
			github_connected_at TIMESTAMPTZ,
			gitlab_credentials TEXT NOT NULL DEFAULT '',
			gitlab_credentials_at TIMESTAMPTZ,
			forgejo_credentials TEXT NOT NULL DEFAULT '',
			forgejo_credentials_at TIMESTAMPTZ,
			tier TEXT NOT NULL DEFAULT 'free',
			storage_used_bytes BIGINT NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
	}

	// Org billing tables for Team Pro
	orgBillingMigrations := []string{
		`CREATE TABLE IF NOT EXISTS org_billing (
			id TEXT PRIMARY KEY,
			forge_type TEXT NOT NULL,
			forge_org TEXT NOT NULL,
			owner_user_id TEXT NOT NULL,
			stripe_customer_id TEXT NOT NULL DEFAULT '',
			stripe_subscription_id TEXT NOT NULL DEFAULT '',
			stripe_subscription_item_id TEXT NOT NULL DEFAULT '',
			seat_limit INTEGER NOT NULL DEFAULT 5,
			seats_used INTEGER NOT NULL DEFAULT 0,
			storage_used_bytes BIGINT NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'active',
			period_start TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(forge_type, forge_org)
		)`,
		`CREATE TABLE IF NOT EXISTS org_seats (
			org_billing_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			forge_username TEXT NOT NULL DEFAULT '',
			consumed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (org_billing_id, user_id),
			FOREIGN KEY (org_billing_id) REFERENCES org_billing(id)
		)`,
	}
	for _, m := range orgBillingMigrations {
		_, _ = s.db.Exec(m)
	}

	// Add columns that may not exist (for migrations)
	alterStatements := []string{
		// Storage quota columns
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS tier TEXT NOT NULL DEFAULT 'free'`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS storage_used_bytes BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE jobs ADD COLUMN IF NOT EXISTS log_size_bytes BIGINT NOT NULL DEFAULT 0`,
	}
	for _, stmt := range alterStatements {
		_, _ = s.db.Exec(stmt)
	}

	for _, m := range migrations {
		if _, err := s.db.Exec(m); err != nil {
			return fmt.Errorf("execute migration: %w", err)
		}
	}

	// Create indexes (ignore errors - may already exist)
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_workers_owner_name ON workers(owner_name)`,
		`CREATE INDEX IF NOT EXISTS idx_repos_forge_owner_name ON repos(forge_type, owner, name)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_repo_id ON jobs(repo_id)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_author ON jobs(author)`,
		`CREATE INDEX IF NOT EXISTS idx_job_logs_job_id ON job_logs(job_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tokens_hash ON tokens(hash)`,
		`CREATE INDEX IF NOT EXISTS idx_users_email ON users(email)`,
	}

	for _, idx := range indexes {
		_, _ = s.db.Exec(idx)
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
func (s *PostgresStorage) migrateEncryptSecrets() error {
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
		if _, err := s.db.Exec(`UPDATE repos SET webhook_secret = $1, forge_token = $2 WHERE id = $3`,
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
		if _, err := s.db.Exec(`UPDATE users SET gitlab_credentials = $1, forgejo_credentials = $2 WHERE id = $3`,
			u.gitlabCreds, u.forgejoCreds, u.id); err != nil {
			return err
		}
		s.log.Info("encrypted user credentials", "user_id", u.id)
	}

	return nil
}

// encrypt encrypts a value if cipher is configured.
func (s *PostgresStorage) encrypt(plaintext string) (string, error) {
	if s.cipher == nil || plaintext == "" {
		return plaintext, nil
	}
	return s.cipher.Encrypt(plaintext)
}

// decrypt decrypts a value if cipher is configured.
func (s *PostgresStorage) decrypt(ciphertext string) (string, error) {
	if s.cipher == nil || ciphertext == "" {
		return ciphertext, nil
	}
	return s.cipher.Decrypt(ciphertext)
}

// decryptUserCredentials decrypts the user's forge credentials.
func (s *PostgresStorage) decryptUserCredentials(user *User) error {
	var err error
	if user.GitLabCredentials, err = s.decrypt(user.GitLabCredentials); err != nil {
		return fmt.Errorf("decrypt gitlab_credentials: %w", err)
	}
	if user.ForgejoCredentials, err = s.decrypt(user.ForgejoCredentials); err != nil {
		return fmt.Errorf("decrypt forgejo_credentials: %w", err)
	}
	return nil
}

func (s *PostgresStorage) Close() error {
	return s.db.Close()
}

// --- Jobs ---

func (s *PostgresStorage) CreateJob(ctx context.Context, job *Job) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO jobs (id, repo_id, commit_sha, branch, tag, pr_number, pr_base_branch, status, installation_id, check_run_id, created_at, author, trust_level, is_fork, approved_by, approved_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		job.ID, job.RepoID, job.Commit, job.Branch, job.Tag, job.PRNumber, job.PRBaseBranch, job.Status, job.InstallationID, job.CheckRunID, job.CreatedAt,
		job.Author, job.TrustLevel, job.IsFork, job.ApprovedBy, job.ApprovedAt)
	return err
}

func (s *PostgresStorage) GetJob(ctx context.Context, id string) (*Job, error) {
	job := &Job{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, commit_sha, branch, tag, pr_number, pr_base_branch, status, exit_code, worker_id,
		        installation_id, check_run_id, started_at, finished_at, created_at,
		        author, trust_level, is_fork, approved_by, approved_at
		 FROM jobs WHERE id = $1`, id).Scan(
		&job.ID, &job.RepoID, &job.Commit, &job.Branch, &job.Tag, &job.PRNumber, &job.PRBaseBranch, &job.Status,
		&job.ExitCode, &job.WorkerID, &job.InstallationID, &job.CheckRunID, &job.StartedAt, &job.FinishedAt, &job.CreatedAt,
		&job.Author, &job.TrustLevel, &job.IsFork, &job.ApprovedBy, &job.ApprovedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return job, err
}

func (s *PostgresStorage) GetJobSiblings(ctx context.Context, repoID, commit, excludeJobID string) ([]*Job, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, repo_id, commit_sha, branch, tag, pr_number, pr_base_branch, status, exit_code, worker_id,
		        installation_id, check_run_id, started_at, finished_at, created_at,
		        author, trust_level, is_fork, approved_by, approved_at
		 FROM jobs WHERE repo_id = $1 AND commit_sha = $2 AND id != $3
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

func (s *PostgresStorage) UpdateJobCheckRunID(ctx context.Context, id string, checkRunID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET check_run_id = $1 WHERE id = $2`,
		checkRunID, id)
	return err
}

func (s *PostgresStorage) ListJobs(ctx context.Context, filter JobFilter) ([]*Job, error) {
	query := `SELECT id, repo_id, commit_sha, branch, tag, pr_number, pr_base_branch, status, exit_code, worker_id,
	                 installation_id, check_run_id, started_at, finished_at, created_at,
	                 author, trust_level, is_fork, approved_by, approved_at FROM jobs WHERE 1=1`
	args := []any{}
	argNum := 1

	if filter.RepoID != "" {
		query += fmt.Sprintf(" AND repo_id = $%d", argNum)
		args = append(args, filter.RepoID)
		argNum++
	}
	if filter.Status != "" {
		query += fmt.Sprintf(" AND status = $%d", argNum)
		args = append(args, filter.Status)
		argNum++
	}
	if filter.Branch != "" {
		query += fmt.Sprintf(" AND branch = $%d", argNum)
		args = append(args, filter.Branch)
		argNum++
	}

	query += " ORDER BY created_at DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argNum)
		args = append(args, filter.Limit)
		argNum++
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argNum)
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

func (s *PostgresStorage) ListJobsByWorker(ctx context.Context, workerID string, limit int) ([]*Job, error) {
	if limit <= 0 {
		limit = 10
	}

	query := `SELECT id, repo_id, commit_sha, branch, tag, pr_number, pr_base_branch, status, exit_code, worker_id,
	                 installation_id, check_run_id, started_at, finished_at, created_at,
	                 author, trust_level, is_fork, approved_by, approved_at
	          FROM jobs WHERE worker_id = $1 ORDER BY created_at DESC LIMIT $2`

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

func (s *PostgresStorage) UpdateJobStatus(ctx context.Context, id string, status JobStatus, exitCode *int) error {
	var err error
	now := time.Now()

	switch status {
	case JobStatusRunning:
		_, err = s.db.ExecContext(ctx,
			`UPDATE jobs SET status = $1, started_at = $2 WHERE id = $3`,
			status, now, id)
	case JobStatusSuccess, JobStatusFailed, JobStatusCancelled, JobStatusError:
		_, err = s.db.ExecContext(ctx,
			`UPDATE jobs SET status = $1, exit_code = $2, finished_at = $3 WHERE id = $4`,
			status, exitCode, now, id)
	default:
		_, err = s.db.ExecContext(ctx,
			`UPDATE jobs SET status = $1 WHERE id = $2`,
			status, id)
	}
	return err
}

func (s *PostgresStorage) UpdateJobWorker(ctx context.Context, jobID, workerID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET worker_id = $1 WHERE id = $2`,
		workerID, jobID)
	return err
}

func (s *PostgresStorage) ApproveJob(ctx context.Context, jobID, approvedBy string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET approved_by = $1, approved_at = $2, status = $3 WHERE id = $4 AND status = $5`,
		approvedBy, time.Now(), JobStatusPending, jobID, JobStatusPendingContributor)
	return err
}

// --- Workers ---

func (s *PostgresStorage) CreateWorker(ctx context.Context, worker *Worker) error {
	labels := strings.Join(worker.Labels, ",")
	mode := worker.Mode
	if mode == "" {
		mode = "personal"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO workers (id, name, labels, status, last_seen, created_at, owner_name, mode)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		worker.ID, worker.Name, labels, worker.Status, worker.LastSeen, worker.CreatedAt, worker.OwnerName, mode)
	return err
}

func (s *PostgresStorage) GetWorker(ctx context.Context, id string) (*Worker, error) {
	worker := &Worker{}
	var labels string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, labels, status, last_seen, created_at, owner_name, mode
		 FROM workers WHERE id = $1`, id).Scan(
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

func (s *PostgresStorage) ListWorkers(ctx context.Context) ([]*Worker, error) {
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

func (s *PostgresStorage) CountWorkersByOwner(ctx context.Context, ownerName string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workers WHERE owner_name = $1`,
		ownerName).Scan(&count)
	return count, err
}

func (s *PostgresStorage) UpdateWorkerLastSeen(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE workers SET last_seen = $1 WHERE id = $2`,
		time.Now(), id)
	return err
}

func (s *PostgresStorage) UpdateWorkerStatus(ctx context.Context, id string, status WorkerStatus) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE workers SET status = $1 WHERE id = $2`,
		status, id)
	return err
}

func (s *PostgresStorage) DeleteWorker(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM workers WHERE id = $1`, id)
	return err
}

func (s *PostgresStorage) UpdateWorkerOwner(ctx context.Context, id, ownerName, mode string) error {
	if mode == "" {
		mode = "personal"
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE workers SET owner_name = $1, mode = $2 WHERE id = $3`,
		ownerName, mode, id)
	return err
}

// --- Repos ---

func (s *PostgresStorage) CreateRepo(ctx context.Context, repo *Repo) error {
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
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		 ON CONFLICT (clone_url) DO UPDATE SET
		 	webhook_secret = EXCLUDED.webhook_secret,
		 	forge_token = EXCLUDED.forge_token,
		 	workers = EXCLUDED.workers,
		 	private = EXCLUDED.private`,
		repo.ID, repo.ForgeType, repo.Owner, repo.Name, repo.CloneURL, repo.HTMLURL,
		webhookSecret, forgeToken, repo.Build, repo.Release, workers, secretsJSON, repo.Private, repo.CreatedAt)
	return err
}

func (s *PostgresStorage) GetRepo(ctx context.Context, id string) (*Repo, error) {
	repo := &Repo{}
	var workers, secretsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, forge_type, owner, name, clone_url, html_url, webhook_secret, forge_token, build, release, workers, secrets, private, created_at
		 FROM repos WHERE id = $1`, id).Scan(
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

func (s *PostgresStorage) GetRepoByCloneURL(ctx context.Context, cloneURL string) (*Repo, error) {
	repo := &Repo{}
	var workers, secretsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, forge_type, owner, name, clone_url, html_url, webhook_secret, forge_token, build, release, workers, secrets, private, created_at
		 FROM repos WHERE clone_url = $1`, cloneURL).Scan(
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

func (s *PostgresStorage) ListRepos(ctx context.Context) ([]*Repo, error) {
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

func (s *PostgresStorage) DeleteRepo(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM repos WHERE id = $1`, id)
	return err
}

func (s *PostgresStorage) GetRepoByOwnerName(ctx context.Context, forge, owner, name string) (*Repo, error) {
	repo := &Repo{}
	var workers, secretsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, forge_type, owner, name, clone_url, html_url, webhook_secret, forge_token, build, release, workers, secrets, private, created_at
		 FROM repos WHERE forge_type = $1 AND owner = $2 AND name = $3`, forge, owner, name).Scan(
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

func (s *PostgresStorage) UpdateRepoPrivate(ctx context.Context, id string, private bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE repos SET private = $1 WHERE id = $2`,
		private, id)
	return err
}

func (s *PostgresStorage) UpdateRepoSecrets(ctx context.Context, id string, secrets map[string]string) error {
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
		`UPDATE repos SET secrets = $1 WHERE id = $2`,
		secretsJSON, id)
	return err
}

// --- Tokens ---

func (s *PostgresStorage) CreateToken(ctx context.Context, token *Token) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tokens (id, name, hash, worker_id, created_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		token.ID, token.Name, token.Hash, token.WorkerID, token.CreatedAt)
	return err
}

func (s *PostgresStorage) GetTokenByHash(ctx context.Context, hash string) (*Token, error) {
	token := &Token{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, hash, worker_id, created_at, revoked_at
		 FROM tokens WHERE hash = $1 AND revoked_at IS NULL`, hash).Scan(
		&token.ID, &token.Name, &token.Hash, &token.WorkerID, &token.CreatedAt, &token.RevokedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return token, err
}

func (s *PostgresStorage) ListTokens(ctx context.Context) ([]*Token, error) {
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

func (s *PostgresStorage) RevokeToken(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tokens SET revoked_at = $1 WHERE id = $2`,
		time.Now(), id)
	return err
}

// --- Users ---

func (s *PostgresStorage) GetOrCreateUser(ctx context.Context, name string) (*User, error) {
	user := &User{}
	var gitlabCredentialsAt, forgejoCredentialsAt, githubConnectedAt sql.NullTime
	var emailsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, email, emails, github_connected_at, gitlab_credentials, gitlab_credentials_at,
		        forgejo_credentials, forgejo_credentials_at, created_at
		 FROM users WHERE name = $1`, name).Scan(
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
			`INSERT INTO users (id, name, created_at) VALUES ($1, $2, $3)`,
			user.ID, user.Name, user.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("create user: %w", err)
		}
		return user, nil
	}
	return nil, err
}

func (s *PostgresStorage) GetOrCreateUserByEmail(ctx context.Context, email, name string) (*User, error) {
	user := &User{}
	var gitlabCredentialsAt, forgejoCredentialsAt, githubConnectedAt sql.NullTime
	var emailsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, email, emails, github_connected_at, gitlab_credentials, gitlab_credentials_at,
		        forgejo_credentials, forgejo_credentials_at, created_at
		 FROM users WHERE email = $1`, email).Scan(
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
			`INSERT INTO users (id, name, email, created_at) VALUES ($1, $2, $3, $4)`,
			user.ID, user.Name, user.Email, user.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("create user: %w", err)
		}
		return user, nil
	}
	return nil, err
}

func (s *PostgresStorage) GetUserByName(ctx context.Context, name string) (*User, error) {
	user := &User{}
	var gitlabCredentialsAt, forgejoCredentialsAt, githubConnectedAt sql.NullTime
	var emailsJSON string
	var tier sql.NullString
	var storageUsed sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, email, emails, github_connected_at, gitlab_credentials, gitlab_credentials_at,
		        forgejo_credentials, forgejo_credentials_at, tier, storage_used_bytes, created_at
		 FROM users WHERE name = $1`, name).Scan(
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

func (s *PostgresStorage) UpdateUserGitLabCredentials(ctx context.Context, userID, credentials string) error {
	encrypted, err := s.encrypt(credentials)
	if err != nil {
		return fmt.Errorf("encrypt gitlab_credentials: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE users SET gitlab_credentials = $1, gitlab_credentials_at = $2 WHERE id = $3`,
		encrypted, time.Now(), userID)
	return err
}

func (s *PostgresStorage) UpdateUserForgejoCredentials(ctx context.Context, userID, credentials string) error {
	encrypted, err := s.encrypt(credentials)
	if err != nil {
		return fmt.Errorf("encrypt forgejo_credentials: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE users SET forgejo_credentials = $1, forgejo_credentials_at = $2 WHERE id = $3`,
		encrypted, time.Now(), userID)
	return err
}

func (s *PostgresStorage) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	user := &User{}
	var gitlabCredentialsAt, forgejoCredentialsAt, githubConnectedAt sql.NullTime
	var emailsJSON string
	var tier sql.NullString
	var storageUsed sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, email, emails, github_connected_at, gitlab_credentials, gitlab_credentials_at,
		        forgejo_credentials, forgejo_credentials_at, tier, storage_used_bytes, created_at
		 FROM users WHERE email = $1 OR emails LIKE $2`, email, "%"+email+"%").Scan(
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

func (s *PostgresStorage) UpdateUserEmail(ctx context.Context, userID, email string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET email = $1 WHERE id = $2`,
		email, userID)
	return err
}

func (s *PostgresStorage) AddUserEmail(ctx context.Context, userID, email string) error {
	// Get current emails
	var emailsJSON string
	err := s.db.QueryRowContext(ctx, `SELECT emails FROM users WHERE id = $1`, userID).Scan(&emailsJSON)
	if err != nil {
		return err
	}

	emails := parseEmailsJSON(emailsJSON)
	// Check if email already exists
	for _, e := range emails {
		if e == email {
			return nil // Already exists
		}
	}
	emails = append(emails, email)

	_, err = s.db.ExecContext(ctx,
		`UPDATE users SET emails = $1 WHERE id = $2`,
		formatEmailsJSON(emails), userID)
	return err
}

func (s *PostgresStorage) UpdateUserGitHubConnected(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET github_connected_at = $1 WHERE id = $2`,
		time.Now(), userID)
	return err
}

func (s *PostgresStorage) UpdateUserGitLabConnected(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET gitlab_credentials_at = $1 WHERE id = $2`,
		time.Now(), userID)
	return err
}

func (s *PostgresStorage) UpdateUserForgejoConnected(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET forgejo_credentials_at = $1 WHERE id = $2`,
		time.Now(), userID)
	return err
}

func (s *PostgresStorage) ClearUserGitLabCredentials(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET gitlab_credentials = '', gitlab_credentials_at = NULL WHERE id = $1`,
		userID)
	return err
}

func (s *PostgresStorage) ClearUserForgejoCredentials(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET forgejo_credentials = '', forgejo_credentials_at = NULL WHERE id = $1`,
		userID)
	return err
}

func (s *PostgresStorage) ClearUserGitHubConnected(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET github_connected_at = NULL WHERE id = $1`,
		userID)
	return err
}

func (s *PostgresStorage) DeleteUser(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id)
	return err
}

// UpdateJobLogSize updates the log size for a job.
func (s *PostgresStorage) UpdateJobLogSize(ctx context.Context, jobID string, sizeBytes int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET log_size_bytes = $1 WHERE id = $2`,
		sizeBytes, jobID)
	return err
}

// UpdateUserStorageUsed adds deltaBytes to the user's storage usage.
func (s *PostgresStorage) UpdateUserStorageUsed(ctx context.Context, userID string, deltaBytes int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET storage_used_bytes = storage_used_bytes + $1 WHERE id = $2`,
		deltaBytes, userID)
	return err
}

// --- Billing ---

// UpdateUserTier updates a user's subscription tier.
func (s *PostgresStorage) UpdateUserTier(ctx context.Context, userID string, tier UserTier) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET tier = $1 WHERE id = $2`,
		string(tier), userID)
	return err
}

// CreateOrgBilling creates a new org billing record.
func (s *PostgresStorage) CreateOrgBilling(ctx context.Context, billing *OrgBilling) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO org_billing (id, forge_type, forge_org, owner_user_id, stripe_customer_id,
		 stripe_subscription_id, stripe_subscription_item_id, seat_limit, seats_used,
		 storage_used_bytes, status, period_start, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		billing.ID, string(billing.ForgeType), billing.ForgeOrg, billing.OwnerUserID,
		billing.StripeCustomerID, billing.StripeSubscriptionID, billing.StripeSubscriptionItemID,
		billing.SeatLimit, billing.SeatsUsed, billing.StorageUsedBytes, billing.Status,
		billing.PeriodStart, billing.CreatedAt)
	return err
}

// GetOrgBilling retrieves org billing by forge type and org name.
func (s *PostgresStorage) GetOrgBilling(ctx context.Context, forgeType ForgeType, forgeOrg string) (*OrgBilling, error) {
	billing := &OrgBilling{}
	var periodStart sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, forge_type, forge_org, owner_user_id, stripe_customer_id,
		        stripe_subscription_id, stripe_subscription_item_id, seat_limit, seats_used,
		        storage_used_bytes, status, period_start, created_at
		 FROM org_billing WHERE forge_type = $1 AND forge_org = $2`,
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
func (s *PostgresStorage) UpdateOrgBillingSeatLimit(ctx context.Context, id string, seatLimit int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE org_billing SET seat_limit = $1 WHERE id = $2`,
		seatLimit, id)
	return err
}

// UpdateOrgBillingSeatsUsed updates the seats used count for an org.
func (s *PostgresStorage) UpdateOrgBillingSeatsUsed(ctx context.Context, id string, seatsUsed int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE org_billing SET seats_used = $1 WHERE id = $2`,
		seatsUsed, id)
	return err
}

// IsOrgSeat checks if a user has consumed a seat in the current billing period.
func (s *PostgresStorage) IsOrgSeat(ctx context.Context, orgBillingID, userID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM org_seats WHERE org_billing_id = $1 AND user_id = $2`,
		orgBillingID, userID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// AddOrgSeat records that a user has consumed a seat this billing period.
func (s *PostgresStorage) AddOrgSeat(ctx context.Context, orgBillingID, userID, forgeUsername string) error {
	// Use ON CONFLICT DO NOTHING to handle race conditions
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO org_seats (org_billing_id, user_id, forge_username, consumed_at)
		 VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
		orgBillingID, userID, forgeUsername, time.Now())
	if err != nil {
		return err
	}

	// Update seats_used in org_billing (count actual seats)
	_, err = s.db.ExecContext(ctx,
		`UPDATE org_billing SET seats_used = (SELECT COUNT(*) FROM org_seats WHERE org_billing_id = $1)
		 WHERE id = $1`,
		orgBillingID)
	return err
}

// ResetOrgSeats clears all seat consumption at the start of a new billing period.
func (s *PostgresStorage) ResetOrgSeats(ctx context.Context, orgBillingID string) error {
	// Delete all seat records for this org
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM org_seats WHERE org_billing_id = $1`,
		orgBillingID)
	if err != nil {
		return err
	}

	// Reset seats_used counter and update period_start
	_, err = s.db.ExecContext(ctx,
		`UPDATE org_billing SET seats_used = 0, period_start = $1 WHERE id = $2`,
		time.Now(), orgBillingID)
	return err
}

// --- Logs ---

func (s *PostgresStorage) AppendLog(ctx context.Context, jobID, stream, data string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO job_logs (job_id, stream, data, created_at)
		 VALUES ($1, $2, $3, $4)`,
		jobID, stream, data, time.Now())
	return err
}

func (s *PostgresStorage) GetLogs(ctx context.Context, jobID string) ([]*LogEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, job_id, stream, data, created_at FROM job_logs WHERE job_id = $1 ORDER BY id`,
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
