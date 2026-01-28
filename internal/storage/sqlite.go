package storage

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStorage implements Storage using SQLite.
type SQLiteStorage struct {
	db *sql.DB
}

// NewSQLite creates a new SQLite storage.
// Use ":memory:" for in-memory database, or a file path for persistent storage.
func NewSQLite(dsn string) (*SQLiteStorage, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable foreign keys and WAL mode for better performance
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if dsn != ":memory:" {
		if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
			db.Close()
			return nil, fmt.Errorf("enable WAL: %w", err)
		}
	}

	s := &SQLiteStorage{db: db}
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
	// Index for efficient forge/owner/name lookups
	_, _ = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_repos_forge_owner_name ON repos(forge_type, owner, name)")

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
		`INSERT INTO jobs (id, repo_id, commit_sha, branch, tag, pr_number, pr_base_branch, status, installation_id, check_run_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.RepoID, job.Commit, job.Branch, job.Tag, job.PRNumber, job.PRBaseBranch, job.Status, job.InstallationID, job.CheckRunID, job.CreatedAt)
	return err
}

func (s *SQLiteStorage) GetJob(ctx context.Context, id string) (*Job, error) {
	job := &Job{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, repo_id, commit_sha, branch, tag, pr_number, pr_base_branch, status, exit_code, worker_id,
		        installation_id, check_run_id, started_at, finished_at, created_at
		 FROM jobs WHERE id = ?`, id).Scan(
		&job.ID, &job.RepoID, &job.Commit, &job.Branch, &job.Tag, &job.PRNumber, &job.PRBaseBranch, &job.Status,
		&job.ExitCode, &job.WorkerID, &job.InstallationID, &job.CheckRunID, &job.StartedAt, &job.FinishedAt, &job.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return job, err
}

func (s *SQLiteStorage) UpdateJobCheckRunID(ctx context.Context, id string, checkRunID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET check_run_id = ? WHERE id = ?`,
		checkRunID, id)
	return err
}

func (s *SQLiteStorage) ListJobs(ctx context.Context, filter JobFilter) ([]*Job, error) {
	query := `SELECT id, repo_id, commit_sha, branch, tag, pr_number, pr_base_branch, status, exit_code, worker_id,
	                 installation_id, check_run_id, started_at, finished_at, created_at FROM jobs WHERE 1=1`
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
			&job.FinishedAt, &job.CreatedAt); err != nil {
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

// --- Workers ---

func (s *SQLiteStorage) CreateWorker(ctx context.Context, worker *Worker) error {
	labels := strings.Join(worker.Labels, ",")
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO workers (id, name, labels, status, last_seen, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		worker.ID, worker.Name, labels, worker.Status, worker.LastSeen, worker.CreatedAt)
	return err
}

func (s *SQLiteStorage) GetWorker(ctx context.Context, id string) (*Worker, error) {
	worker := &Worker{}
	var labels string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, labels, status, last_seen, created_at
		 FROM workers WHERE id = ?`, id).Scan(
		&worker.ID, &worker.Name, &labels, &worker.Status, &worker.LastSeen, &worker.CreatedAt)
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
		`SELECT id, name, labels, status, last_seen, created_at FROM workers ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workers []*Worker
	for rows.Next() {
		worker := &Worker{}
		var labels string
		if err := rows.Scan(&worker.ID, &worker.Name, &labels, &worker.Status,
			&worker.LastSeen, &worker.CreatedAt); err != nil {
			return nil, err
		}
		if labels != "" {
			worker.Labels = strings.Split(labels, ",")
		}
		workers = append(workers, worker)
	}
	return workers, rows.Err()
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

// --- Repos ---

func (s *SQLiteStorage) CreateRepo(ctx context.Context, repo *Repo) error {
	// Use upsert to handle re-onboarding: if repo exists, update token and webhook secret
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO repos (id, forge_type, owner, name, clone_url, html_url, webhook_secret, forge_token, build, release, private, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(clone_url) DO UPDATE SET
		 	webhook_secret = excluded.webhook_secret,
		 	forge_token = excluded.forge_token,
		 	private = excluded.private`,
		repo.ID, repo.ForgeType, repo.Owner, repo.Name, repo.CloneURL, repo.HTMLURL,
		repo.WebhookSecret, repo.ForgeToken, repo.Build, repo.Release, repo.Private, repo.CreatedAt)
	return err
}

func (s *SQLiteStorage) GetRepo(ctx context.Context, id string) (*Repo, error) {
	repo := &Repo{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, forge_type, owner, name, clone_url, html_url, webhook_secret, forge_token, build, release, private, created_at
		 FROM repos WHERE id = ?`, id).Scan(
		&repo.ID, &repo.ForgeType, &repo.Owner, &repo.Name, &repo.CloneURL, &repo.HTMLURL,
		&repo.WebhookSecret, &repo.ForgeToken, &repo.Build, &repo.Release, &repo.Private, &repo.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return repo, err
}

func (s *SQLiteStorage) GetRepoByCloneURL(ctx context.Context, cloneURL string) (*Repo, error) {
	repo := &Repo{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, forge_type, owner, name, clone_url, html_url, webhook_secret, forge_token, build, release, private, created_at
		 FROM repos WHERE clone_url = ?`, cloneURL).Scan(
		&repo.ID, &repo.ForgeType, &repo.Owner, &repo.Name, &repo.CloneURL, &repo.HTMLURL,
		&repo.WebhookSecret, &repo.ForgeToken, &repo.Build, &repo.Release, &repo.Private, &repo.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return repo, err
}

func (s *SQLiteStorage) ListRepos(ctx context.Context) ([]*Repo, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, forge_type, owner, name, clone_url, html_url, webhook_secret, forge_token, build, release, private, created_at
		 FROM repos ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repos []*Repo
	for rows.Next() {
		repo := &Repo{}
		if err := rows.Scan(&repo.ID, &repo.ForgeType, &repo.Owner, &repo.Name, &repo.CloneURL,
			&repo.HTMLURL, &repo.WebhookSecret, &repo.ForgeToken, &repo.Build, &repo.Release, &repo.Private, &repo.CreatedAt); err != nil {
			return nil, err
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
	err := s.db.QueryRowContext(ctx,
		`SELECT id, forge_type, owner, name, clone_url, html_url, webhook_secret, forge_token, build, release, private, created_at
		 FROM repos WHERE forge_type = ? AND owner = ? AND name = ?`, forge, owner, name).Scan(
		&repo.ID, &repo.ForgeType, &repo.Owner, &repo.Name, &repo.CloneURL, &repo.HTMLURL,
		&repo.WebhookSecret, &repo.ForgeToken, &repo.Build, &repo.Release, &repo.Private, &repo.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return repo, err
}

func (s *SQLiteStorage) UpdateRepoPrivate(ctx context.Context, id string, private bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE repos SET private = ? WHERE id = ?`,
		private, id)
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
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, email, emails, github_connected_at, gitlab_credentials, gitlab_credentials_at,
		        forgejo_credentials, forgejo_credentials_at, created_at
		 FROM users WHERE name = ?`, name).Scan(
		&user.ID, &user.Name, &user.Email, &emailsJSON, &githubConnectedAt,
		&user.GitLabCredentials, &gitlabCredentialsAt,
		&user.ForgejoCredentials, &forgejoCredentialsAt, &user.CreatedAt)
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
	return user, nil
}

func (s *SQLiteStorage) UpdateUserGitLabCredentials(ctx context.Context, userID, credentials string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET gitlab_credentials = ?, gitlab_credentials_at = ? WHERE id = ?`,
		credentials, time.Now(), userID)
	return err
}

func (s *SQLiteStorage) UpdateUserForgejoCredentials(ctx context.Context, userID, credentials string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET forgejo_credentials = ?, forgejo_credentials_at = ? WHERE id = ?`,
		credentials, time.Now(), userID)
	return err
}

func (s *SQLiteStorage) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	user := &User{}
	var gitlabCredentialsAt, forgejoCredentialsAt, githubConnectedAt sql.NullTime
	var emailsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, email, emails, github_connected_at, gitlab_credentials, gitlab_credentials_at,
		        forgejo_credentials, forgejo_credentials_at, created_at
		 FROM users WHERE email = ? OR emails LIKE ?`, email, "%"+email+"%").Scan(
		&user.ID, &user.Name, &user.Email, &emailsJSON, &githubConnectedAt,
		&user.GitLabCredentials, &gitlabCredentialsAt,
		&user.ForgejoCredentials, &forgejoCredentialsAt, &user.CreatedAt)
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

// Helper functions for emails JSON
func parseEmailsJSON(s string) []string {
	if s == "" || s == "[]" {
		return nil
	}
	// Simple JSON array parsing: ["a","b","c"]
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var emails []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, "\"")
		if p != "" {
			emails = append(emails, p)
		}
	}
	return emails
}

func formatEmailsJSON(emails []string) string {
	if len(emails) == 0 {
		return "[]"
	}
	var parts []string
	for _, e := range emails {
		parts = append(parts, "\""+e+"\"")
	}
	return "[" + strings.Join(parts, ",") + "]"
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
