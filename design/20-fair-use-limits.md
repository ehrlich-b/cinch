# Design Doc 20: Fair Use Limits

**Status:** Proposed
**Author:** Claude
**Date:** 2026-01-30

## Problem

Cinch uses Cloudflare R2 for log storage (and soon image caching + releases). Currently there are **zero limits** on:

- Log storage per job/user/account
- Number of workers per user
- Job execution time (configurable per-repo, no maximum)
- Number of concurrent jobs
- API request rates

This creates abuse vectors where users could use Cinch as free infinite cloud storage.

## Research: Industry Benchmarks

### GitHub Actions
- **Artifacts:** 500MB (Free), 2GB (Pro) per repo
- **Cache:** 10GB per repo (recently expanded from 5GB)
- **Logs:** 90-day retention, configurable 1-400 days
- **Job timeout:** 6 hours (GitHub-hosted), 5 days (self-hosted)
- **Concurrent jobs:** 20 (Free), 40 (Team), 180 (Enterprise)

### GitLab CI (Free Tier)
- **Storage:** 10GB per project (combined git + LFS + artifacts)
- **Artifacts:** 100MB per file
- **Logs:** Configurable `output_limit` per runner
- **Job timeout:** Project-configurable, instance maximum

### CircleCI
- **Artifacts:** 30-day retention
- **Workspaces:** 15-day retention
- **Credits:** Monthly, expire without rollover

## Cinch Pricing Context

| Tier | Cost | Storage Quota | Workers | Enforcement |
|------|------|---------------|---------|-------------|
| Free | $0 | 100 MB | 10 | Hard limits |
| Pro | $5/seat/month | 10 GB | 1000 | Hard limits |
| Self-Hosted | $0 (MIT) | Unlimited | Unlimited | System health only |

**Philosophy:**
- **Free/Pro (hosted):** Be generous enough that legitimate users never hit limits. Be restrictive enough that abuse is economically infeasible.
- **Self-Hosted:** No artificial limits. Only prevent system health issues (e.g., don't allow 1 billion connections from 1 IP). Self-hosters manage their own resources.

## Storage Quota (Unified)

All R2 storage shares a single quota per account:
- **Logs** (NDJSON in `logs/{job_id}/`)
- **Artifacts** (future: `artifacts/{job_id}/`)
- **Image cache** (future: `cache/{repo_id}/layers/`)
- **Releases** (future: `releases/{repo_id}/{tag}/`)

### Why 100MB Free?

A typical CI job produces 10-100KB of logs. At 50KB average:
- 100MB = ~2,000 builds
- With 30-day retention, that's ~66 builds/day sustained

This is generous for hobbyist usage but prevents storage abuse.

### Why 10GB Pro?

Matches GitHub Actions cache limit. At $5/seat/month with R2 at $0.015/GB/month, 10GB costs us $0.15/month in storage—3% of revenue. Acceptable margin.

## Limits Overview

| Resource | Free | Pro | Self-Hosted |
|----------|------|-----|-------------|
| **Storage quota** | 100 MB | 10 GB | Unlimited |
| **Log retention** | 7 days | 90 days | Configurable |
| **Max log size per job** | 10 MB | 100 MB | Configurable |
| **Workers** | 10 | 1000 | Unlimited |
| **Concurrent jobs** | 5 | 100 | Unlimited |
| **Job timeout max** | 1 hour | 6 hours | Unlimited |
| **Repos** | Unlimited | Unlimited | Unlimited |
| **Forges** | Unlimited | Unlimited | Unlimited |

### Justification

**Workers (10 Free, 1000 Pro):**
10 workers is enough for any solo developer. 1000 handles enterprise fleets. The user suggested 5096 as "unlimited but sane"—I'm proposing 1000 because it's a rounder number and still insanely generous. Nobody needs 1000 workers.

**Concurrent jobs (5 Free, 100 Pro):**
Matches worker limits roughly. Prevents queue flooding.

**Job timeout max (1h Free, 6h Pro):**
GitHub uses 6h. One hour handles 99% of builds. Long-running jobs are an abuse vector (crypto mining, etc.).

**Log size per job (10MB Free, 100MB Pro):**
A 10MB log is already absurdly verbose. 100MB handles edge cases like full test suite output with verbose logging.

**Repos and Forges: Unlimited:**
These have negligible storage cost. Limiting them creates friction without meaningful protection.

## Implementation

### Schema Changes

Add to `users` table:
```sql
-- Account limits
storage_quota_bytes BIGINT NOT NULL DEFAULT 104857600,    -- 100MB
storage_used_bytes BIGINT NOT NULL DEFAULT 0,
max_workers INT NOT NULL DEFAULT 10,
max_concurrent_jobs INT NOT NULL DEFAULT 5,
max_job_timeout_minutes INT NOT NULL DEFAULT 60,
max_log_size_bytes BIGINT NOT NULL DEFAULT 10485760,      -- 10MB
log_retention_days INT NOT NULL DEFAULT 7,
tier TEXT NOT NULL DEFAULT 'free'                         -- 'free' or 'pro'
```

### Storage Tracking

Track storage usage in real-time:

```go
// Before writing to R2
func (s *R2LogStore) AppendChunk(ctx context.Context, jobID, stream string, data []byte) error {
    // Check job log size limit
    job := s.getJobMeta(jobID)
    if job.LogSize + len(data) > job.MaxLogSize {
        return ErrLogSizeLimitExceeded
    }

    // Check account storage quota
    account := s.getAccount(job.AccountID)
    if account.StorageUsed + len(data) > account.StorageQuota {
        return ErrStorageQuotaExceeded
    }

    // Write and update counters
    if err := s.write(ctx, jobID, data); err != nil {
        return err
    }

    s.updateJobLogSize(jobID, len(data))
    s.updateAccountStorage(account.ID, len(data))
    return nil
}
```

### Worker Registration Limit

```go
func (h *WSHandler) handleRegister(ctx context.Context, conn *websocket.Conn, msg protocol.Register) error {
    user := h.getUser(ctx)

    // Count current workers
    count, _ := h.storage.CountWorkersByOwner(ctx, user.Email)
    if count >= user.MaxWorkers {
        return protocol.Error{
            Code:    "WORKER_LIMIT_EXCEEDED",
            Message: fmt.Sprintf("Maximum %d workers allowed. Upgrade to Pro for more.", user.MaxWorkers),
        }
    }

    // ... continue registration
}
```

### Job Timeout Enforcement

```go
func (d *Dispatcher) dispatchJob(ctx context.Context, job *storage.Job) error {
    user := d.getJobOwner(ctx, job)

    // Cap timeout at user's max
    timeout := job.Timeout
    if timeout > time.Duration(user.MaxJobTimeoutMinutes) * time.Minute {
        timeout = time.Duration(user.MaxJobTimeoutMinutes) * time.Minute
    }

    // ... dispatch with capped timeout
}
```

### Concurrent Job Limit

```go
func (d *Dispatcher) enqueueJob(ctx context.Context, job *storage.Job) error {
    user := d.getRepoOwner(ctx, job.RepoID)

    // Count running + queued jobs
    active, _ := d.storage.CountActiveJobsByOwner(ctx, user.ID)
    if active >= user.MaxConcurrentJobs {
        return ErrConcurrentJobLimitExceeded
    }

    // ... enqueue
}
```

### Log Retention Cleanup

Cron job or background goroutine:

```go
func (s *R2LogStore) CleanupExpiredLogs(ctx context.Context) error {
    // Find jobs past retention
    expired, _ := s.storage.ListExpiredJobs(ctx, time.Now())

    for _, job := range expired {
        // Delete from R2
        s.Delete(ctx, job.ID)

        // Update storage usage
        s.updateAccountStorage(job.AccountID, -job.LogSize)
    }

    return nil
}
```

### API for Checking Limits

```go
// GET /api/account/usage
type UsageResponse struct {
    Tier              string `json:"tier"`
    StorageUsedBytes  int64  `json:"storage_used_bytes"`
    StorageQuotaBytes int64  `json:"storage_quota_bytes"`
    WorkerCount       int    `json:"worker_count"`
    WorkerLimit       int    `json:"worker_limit"`
    ActiveJobs        int    `json:"active_jobs"`
    ConcurrentLimit   int    `json:"concurrent_limit"`
}
```

## Error Messages

Clear, actionable error messages:

```
❌ Storage quota exceeded (95MB / 100MB used)
   Free accounts get 100MB. Upgrade to Pro for 10GB: https://cinch.sh/pricing

❌ Worker limit exceeded (10/10 workers registered)
   Free accounts can register 10 workers. Upgrade to Pro for 1000.

❌ Job log truncated at 10MB
   Free accounts have 10MB log limit per job. Full logs available on Pro.

❌ Too many concurrent jobs (5/5 running)
   Wait for a job to complete, or upgrade to Pro for 100 concurrent jobs.
```

## Soft vs Hard Limits

**Hard limits (block immediately):**
- Storage quota (can't write logs)
- Worker registration (can't add more workers)
- Concurrent jobs (can't start more jobs)

**Soft limits (warn, then enforce):**
- Log size per job (truncate with warning, don't fail the job)
- Job timeout (kill job at limit, mark as error)

## Upgrade Path

When a free user hits limits:
1. Show clear error with current usage
2. Link to pricing page
3. Offer Pro trial (7 days, no card required)

## Self-Hosted: System Health Limits Only

Self-hosted deployments have **no artificial limits**. Users manage their own resources.

The only limits are system health guards that prevent obviously pathological behavior:
- WebSocket message size: 1MB (prevents memory exhaustion)
- Log chunk size: 64KB (prevents WebSocket saturation)
- Stale worker timeout: 90s (cleanup disconnected workers)
- Job queue timeout: 30min (don't hold stale jobs forever)

These are hardcoded and not configurable because they're not about fairness—they're about the server not crashing.

**Optional limits (future):** Self-hosters could configure limits via environment variables if they want to run a multi-tenant deployment:

```bash
# Optional - not implemented yet
CINCH_MAX_WORKERS=1000
CINCH_MAX_CONCURRENT_JOBS=100
CINCH_MAX_JOB_TIMEOUT=6h
CINCH_MAX_LOG_SIZE=100MB
CINCH_STORAGE_QUOTA=10GB
CINCH_LOG_RETENTION=90d
```

## Migration

For existing users at launch:
1. All existing users start as "free"
2. Storage used = sum of current log sizes
3. If over quota, don't delete—just prevent new writes until they delete old jobs or upgrade

## Monitoring

Track limit hits for product decisions:
- How many users hit storage quota?
- How many workers do active users actually run?
- What's the p99 job log size?

If 20%+ of free users hit limits regularly, we're too restrictive.
If <1% ever hit limits, we're too generous (for abuse prevention).

## Future: Pay-As-You-Go

Beyond Pro tier, offer PAYG for heavy users:
- Additional storage: $0.05/GB/month (vs R2 cost of $0.015)
- Additional workers: $1/worker/month

This lets enterprises scale without negotiating custom contracts.

## Summary

| Action | Priority | Effort |
|--------|----------|--------|
| Add storage_used tracking to users | High | Medium |
| Enforce storage quota on log writes | High | Low |
| Add worker count limit | High | Low |
| Add concurrent job limit | Medium | Low |
| Cap job timeout at tier max | Medium | Low |
| Per-job log size limit (soft) | Medium | Medium |
| Log retention cleanup job | Medium | Medium |
| Usage API endpoint | Low | Low |
| Self-hosted configurable limits | Low | Low |

**MVP for launch:** Storage quota + worker limit + concurrent job limit. The rest can wait.
