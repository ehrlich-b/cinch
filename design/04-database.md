# Database Schema

## Overview

Cinch supports both SQLite (self-hosted) and Postgres (hosted service). Schema is designed to work with both.

**Selection logic:**
- `DATABASE_URL` set → Postgres
- Otherwise → SQLite at `$CINCH_DATA_DIR/cinch.db`

**Hosted service:** Uses Fly Postgres. Single instance, vertical scaling.

**Self-hosted:** SQLite by default. Zero config, embedded in binary.

## Tables

### `workers`

Registered worker machines.

```sql
CREATE TABLE workers (
    id          TEXT PRIMARY KEY,           -- w_abc123
    name        TEXT NOT NULL,              -- "macbook-pro"
    token_hash  TEXT NOT NULL,              -- bcrypt hash of token
    labels      TEXT NOT NULL DEFAULT '[]', -- JSON array: ["linux", "amd64"]
    last_seen   INTEGER,                    -- Unix timestamp
    status      TEXT NOT NULL DEFAULT 'offline', -- online, offline, busy
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE INDEX idx_workers_status ON workers(status);
CREATE INDEX idx_workers_last_seen ON workers(last_seen);
```

### `repos`

Configured repositories.

```sql
CREATE TABLE repos (
    id          TEXT PRIMARY KEY,           -- r_xyz789
    forge_type  TEXT NOT NULL,              -- github, gitlab, forgejo, bitbucket
    owner       TEXT NOT NULL,              -- "user" or "org"
    name        TEXT NOT NULL,              -- "myrepo"
    clone_url   TEXT NOT NULL,              -- https://github.com/user/myrepo.git
    html_url    TEXT NOT NULL,              -- https://github.com/user/myrepo
    private     INTEGER NOT NULL DEFAULT 0, -- boolean
    webhook_secret TEXT,                    -- for verifying webhooks
    forge_token TEXT,                       -- encrypted, for posting statuses
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,

    UNIQUE(forge_type, owner, name)
);

CREATE INDEX idx_repos_forge ON repos(forge_type, owner, name);
```

### `jobs`

Build jobs.

```sql
CREATE TABLE jobs (
    id          TEXT PRIMARY KEY,           -- j_abc123
    repo_id     TEXT NOT NULL REFERENCES repos(id),
    worker_id   TEXT REFERENCES workers(id),

    -- Git info
    commit_sha  TEXT NOT NULL,
    branch      TEXT NOT NULL,
    is_pr       INTEGER NOT NULL DEFAULT 0, -- boolean
    pr_number   INTEGER,

    -- Config (snapshot at job creation time)
    command     TEXT NOT NULL,
    timeout_sec INTEGER NOT NULL DEFAULT 1800, -- 30 min default
    env_json    TEXT NOT NULL DEFAULT '{}',

    -- Status
    status      TEXT NOT NULL DEFAULT 'pending', -- pending, running, success, failure, error, cancelled
    exit_code   INTEGER,
    error_msg   TEXT,

    -- Timing
    created_at  INTEGER NOT NULL,
    started_at  INTEGER,
    finished_at INTEGER,

    -- Metadata
    triggered_by TEXT,                      -- webhook, manual, api
    trigger_user TEXT                       -- username who triggered
);

CREATE INDEX idx_jobs_repo ON jobs(repo_id);
CREATE INDEX idx_jobs_status ON jobs(status);
CREATE INDEX idx_jobs_created ON jobs(created_at DESC);
CREATE INDEX idx_jobs_repo_branch ON jobs(repo_id, branch);
CREATE INDEX idx_jobs_commit ON jobs(commit_sha);
```

### `job_logs`

Log output from jobs. Chunked for streaming.

```sql
CREATE TABLE job_logs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id      TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    chunk_seq   INTEGER NOT NULL,           -- ordering
    stream      TEXT NOT NULL,              -- stdout, stderr
    data        TEXT NOT NULL,              -- log content
    timestamp   INTEGER NOT NULL,

    UNIQUE(job_id, chunk_seq)
);

CREATE INDEX idx_job_logs_job ON job_logs(job_id, chunk_seq);
```

### `tokens`

API tokens for workers and users.

```sql
CREATE TABLE tokens (
    id          TEXT PRIMARY KEY,           -- tok_abc123
    name        TEXT NOT NULL,              -- human-readable name
    token_hash  TEXT NOT NULL,              -- bcrypt hash
    type        TEXT NOT NULL,              -- worker, admin, readonly
    worker_id   TEXT REFERENCES workers(id), -- if worker token
    scopes      TEXT NOT NULL DEFAULT '[]', -- JSON array of permissions
    last_used   INTEGER,
    expires_at  INTEGER,                    -- NULL = never
    created_at  INTEGER NOT NULL,
    revoked_at  INTEGER                     -- NULL = active
);

CREATE INDEX idx_tokens_type ON tokens(type);
```

### `forge_credentials`

OAuth tokens and app credentials for forges.

```sql
CREATE TABLE forge_credentials (
    id          TEXT PRIMARY KEY,
    forge_type  TEXT NOT NULL,              -- github, gitlab, etc.
    cred_type   TEXT NOT NULL,              -- app, oauth, pat

    -- For GitHub Apps
    app_id      TEXT,
    private_key TEXT,                       -- encrypted

    -- For OAuth / PAT
    access_token TEXT,                      -- encrypted
    refresh_token TEXT,                     -- encrypted
    expires_at  INTEGER,

    -- Scope
    owner       TEXT,                       -- NULL = global, else specific owner

    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE INDEX idx_forge_creds ON forge_credentials(forge_type, owner);
```

## Migrations

Use simple numbered migrations:

```
migrations/
  001_initial.sql
  002_add_job_logs.sql
  003_add_forge_credentials.sql
```

```go
func Migrate(db *sql.DB) error {
    // Create migrations table if not exists
    // Get current version
    // Apply pending migrations in order
    // Update version
}
```

## SQLite vs Postgres Differences

### Auto-increment

```sql
-- SQLite
id INTEGER PRIMARY KEY AUTOINCREMENT

-- Postgres
id SERIAL PRIMARY KEY
-- or: id TEXT PRIMARY KEY (use UUIDs)
```

### JSON

```sql
-- SQLite: store as TEXT, query with json_extract()
SELECT * FROM workers WHERE json_extract(labels, '$[0]') = 'linux';

-- Postgres: use JSONB
SELECT * FROM workers WHERE labels @> '["linux"]';
```

### Timestamps

```sql
-- SQLite: INTEGER (Unix timestamps)
created_at INTEGER NOT NULL

-- Postgres: TIMESTAMP WITH TIME ZONE
created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
```

### Abstraction Layer

```go
type DB interface {
    // Jobs
    CreateJob(j *Job) error
    GetJob(id string) (*Job, error)
    UpdateJobStatus(id string, status Status, exitCode *int) error
    ListJobs(filter JobFilter) ([]*Job, error)

    // Workers
    CreateWorker(w *Worker) error
    GetWorker(id string) (*Worker, error)
    UpdateWorkerStatus(id string, status WorkerStatus) error
    ListWorkers() ([]*Worker, error)

    // Repos
    CreateRepo(r *Repo) error
    GetRepo(id string) (*Repo, error)
    GetRepoByName(forge, owner, name string) (*Repo, error)

    // Logs
    AppendLog(jobID string, chunk *LogChunk) error
    GetLogs(jobID string, since int) ([]*LogChunk, error)

    // Tokens
    CreateToken(t *Token) error
    ValidateToken(raw string) (*Token, error)
    RevokeToken(id string) error
}
```

## Performance Considerations

### Indexes

Essential indexes already defined above. Monitor for:
- Slow queries on `jobs` table as it grows
- Log table can grow large - consider archival

### Log Storage

Options for large deployments:
1. **SQLite/Postgres**: Fine for small-medium (< 100 builds/day)
2. **Filesystem**: Store logs as files, DB just tracks metadata
3. **S3-compatible**: For hosted service, use object storage

### Cleanup

Scheduled job to clean old data:

```go
func Cleanup(db DB, retention time.Duration) error {
    cutoff := time.Now().Add(-retention)

    // Delete old jobs and logs (CASCADE handles logs)
    return db.Exec(`
        DELETE FROM jobs
        WHERE finished_at < ?
        AND status IN ('success', 'failure', 'error', 'cancelled')
    `, cutoff.Unix())
}
```

Default retention: 30 days for finished jobs.

## Encryption

Sensitive fields (tokens, credentials) should be encrypted at rest:

```go
type EncryptedField struct {
    Ciphertext []byte
    Nonce      []byte
}

func Encrypt(key, plaintext []byte) EncryptedField {
    // AES-256-GCM
}

func Decrypt(key []byte, ef EncryptedField) ([]byte, error) {
    // AES-256-GCM
}
```

Store encryption key in environment variable, not database.

## Transactions

Use transactions for multi-step operations:

```go
func (db *sqliteDB) CreateJobWithLogs(j *Job, logs []*LogChunk) error {
    tx, err := db.db.Begin()
    if err != nil {
        return err
    }
    defer tx.Rollback()

    // Insert job
    // Insert logs

    return tx.Commit()
}
```

## Testing

Use in-memory SQLite for tests:

```go
func TestDB(t *testing.T) {
    db, err := NewSQLite(":memory:")
    require.NoError(t, err)
    defer db.Close()

    // Run migrations
    err = db.Migrate()
    require.NoError(t, err)

    // Tests...
}
```
