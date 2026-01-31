# Postgres Migration Plan

## Overview

Migrate from SQLite to Postgres for hosted deployment. Self-hosted users continue with SQLite by default.

## Database Selection

```
DATABASE_URL set?
    │
    ├─ Yes → Postgres (hosted / advanced self-hosted)
    │
    └─ No  → SQLite at ~/.cinch/cinch.db (default)
```

## Schema Differences

| SQLite | Postgres | Notes |
|--------|----------|-------|
| `INTEGER PRIMARY KEY AUTOINCREMENT` | `BIGSERIAL PRIMARY KEY` | Auto-increment |
| `DATETIME` | `TIMESTAMPTZ` | Timestamps |
| `TEXT` (JSON manually) | `JSONB` | Native JSON for secrets, emails |
| `INTEGER` (0/1) | `BOOLEAN` | Booleans |
| `?` placeholders | `$1, $2, $3` | Query params |
| `ON CONFLICT(col) DO UPDATE` | `ON CONFLICT (col) DO UPDATE` | Upsert syntax (same) |
| `PRAGMA foreign_keys = ON` | (enabled by default) | Foreign keys |
| `PRAGMA journal_mode = WAL` | (not applicable) | WAL mode |

## Postgres Schema

```sql
CREATE TABLE IF NOT EXISTS workers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    labels TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'offline',
    last_seen TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    owner_name TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL DEFAULT 'personal'
);
CREATE INDEX IF NOT EXISTS idx_workers_owner_name ON workers(owner_name);

CREATE TABLE IF NOT EXISTS repos (
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
);
CREATE INDEX IF NOT EXISTS idx_repos_forge_owner_name ON repos(forge_type, owner, name);

CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    repo_id TEXT NOT NULL REFERENCES repos(id),
    commit_sha TEXT NOT NULL,
    branch TEXT NOT NULL,
    tag TEXT NOT NULL DEFAULT '',
    pr_number INTEGER,
    pr_base_branch TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    exit_code INTEGER,
    worker_id TEXT REFERENCES workers(id),
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
);
CREATE INDEX IF NOT EXISTS idx_jobs_repo_id ON jobs(repo_id);
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_author ON jobs(author);

CREATE TABLE IF NOT EXISTS job_logs (
    id BIGSERIAL PRIMARY KEY,
    job_id TEXT NOT NULL REFERENCES jobs(id),
    stream TEXT NOT NULL,
    data TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_job_logs_job_id ON job_logs(job_id);

CREATE TABLE IF NOT EXISTS tokens (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    hash TEXT NOT NULL UNIQUE,
    worker_id TEXT REFERENCES workers(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_tokens_hash ON tokens(hash);

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT NOT NULL DEFAULT '',
    emails JSONB NOT NULL DEFAULT '[]',
    github_connected_at TIMESTAMPTZ,
    gitlab_credentials TEXT NOT NULL DEFAULT '',
    gitlab_credentials_at TIMESTAMPTZ,
    forgejo_credentials TEXT NOT NULL DEFAULT '',
    forgejo_credentials_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
```

## Migration Steps

### 1. Add PostgresStorage Implementation

Create `internal/storage/postgres.go`:
- Implement Storage interface using `lib/pq` driver
- Use `$1, $2, ...` placeholders
- Handle `TIMESTAMPTZ` properly
- Use `BOOLEAN` instead of `INTEGER` for is_fork, private

### 2. Add DATABASE_URL Detection

In `cmd/cinch/main.go`:
```go
func newStorage(encryptionSecret string) (storage.Storage, error) {
    if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
        return storage.NewPostgres(dbURL, encryptionSecret)
    }
    // Default to SQLite
    dbPath := filepath.Join(configDir, "cinch.db")
    return storage.NewSQLite(dbPath, encryptionSecret)
}
```

### 3. Data Migration (One-Time)

For hosted deployment, migrate data from SQLite to Postgres:

```bash
# 1. Backup SQLite
cp ~/.cinch/cinch.db ~/.cinch/cinch.db.backup

# 2. Export data
sqlite3 ~/.cinch/cinch.db '.dump' > cinch_dump.sql

# 3. Convert syntax (manual or script)
# - DATETIME → TIMESTAMPTZ
# - INTEGER booleans → BOOLEAN
# - Remove AUTOINCREMENT

# 4. Import to Postgres
psql $DATABASE_URL < cinch_dump.sql
```

Or implement programmatic migration:
```go
func MigrateToPostgres(sqlite *SQLiteStorage, pg *PostgresStorage) error {
    // Export all data from SQLite
    // Import to Postgres
    // Verify counts match
}
```

### 4. Deploy Postgres on Fly

```bash
# Create Postgres cluster
fly postgres create --name cinch-db --region iad

# Attach to app (sets DATABASE_URL automatically)
fly postgres attach cinch-db --app cinch

# Verify
fly postgres connect -a cinch-db
```

## Testing Strategy

### Unit Tests

Test PostgresStorage against a real Postgres instance:

```go
func TestPostgresStorage(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping postgres tests in short mode")
    }

    dsn := os.Getenv("TEST_DATABASE_URL")
    if dsn == "" {
        t.Skip("TEST_DATABASE_URL not set")
    }

    store, err := storage.NewPostgres(dsn, "test-secret")
    require.NoError(t, err)
    defer store.Close()

    // Run shared test suite
    runStorageTests(t, store)
}
```

### Local Testing

```bash
# Start Postgres via Docker
docker run -d --name cinch-postgres \
    -e POSTGRES_PASSWORD=test \
    -e POSTGRES_DB=cinch_test \
    -p 5432:5432 \
    postgres:16

# Run tests
TEST_DATABASE_URL="postgres://postgres:test@localhost:5432/cinch_test?sslmode=disable" \
    go test ./internal/storage/... -run TestPostgres
```

### CI Testing

Add Postgres service to CI:
```yaml
services:
  postgres:
    image: postgres:16
    env:
      POSTGRES_PASSWORD: test
      POSTGRES_DB: cinch_test
    ports:
      - 5432:5432
```

## Rollback Plan

1. Keep SQLite as default for self-hosted
2. If Postgres issues in production:
   - Set `DATABASE_URL=""` to revert to SQLite
   - Data loss of any Postgres-only writes

## Timeline

1. **Phase 1**: PostgresStorage implementation + tests (this PR)
2. **Phase 2**: Deploy Fly Postgres, test with shadow traffic
3. **Phase 3**: Migrate production data, switch over
4. **Phase 4**: Monitor, keep SQLite backup for 30 days

## Open Questions

1. **Connection pooling**: Use `pgxpool` or rely on Fly's PgBouncer?
   - Decision: Start simple with `lib/pq`, add pooling if needed

2. **Encryption**: Same AES-256-GCM as SQLite?
   - Decision: Yes, use same crypto.Cipher

3. **Emails field**: Keep as JSON string or use Postgres array?
   - Decision: Use JSONB for now, matches SQLite behavior
