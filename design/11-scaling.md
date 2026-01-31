# Scaling Architecture

Cinch uses **vertical scaling** for simplicity. One web server, one database. Scale up machines as needed. Horizontal scaling is explicitly unplanned until proven necessary.

## Philosophy

**Complexity kills.** Multi-node coordination, distributed state, consensus protocols - these are engineering black holes. A single beefy server with Postgres handles way more than most people realize.

**Downtime is acceptable.** Server restarts cause ~1 minute of downtime. That's fine. Workers reconnect automatically. Jobs in flight finish locally and report results on reconnect. No data loss.

**Scale vertically first.** A Fly.io `performance-16x` (16 vCPU, 128GB RAM) costs ~$500/month and handles more traffic than you'll ever see.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Cloudflare                               │
│                  CDN + R2 (logs/artifacts)                   │
└─────────────────────────┬───────────────────────────────────┘
                          │
                    ┌─────▼─────┐
                    │  Fly.io   │
                    │  Web Node │ ◄──── Webhooks from forges
                    │ (1 machine)│ ◄──── WebSocket from workers
                    └─────┬─────┘
                          │
                    ┌─────▼─────┐
                    │ Postgres  │
                    │(Fly or RDS)│
                    └───────────┘

Workers connect via WebSocket, maintain persistent connections.
Logs stream to R2 (not through web server).
Metadata and job state in Postgres.
```

## Capacity Estimates

Back-of-napkin math says vertical scaling gets us further than we'll ever realistically need.

**The gist:**
- WebSocket connections are cheap (~20KB each, mostly idle)
- Postgres handles simple writes at 50k+/sec
- Job throughput is limited by worker count, not server capacity
- A single scaled-up Fly machine handles 100k+ workers, millions of jobs/day

**What actually breaks first:** You run out of workers to run jobs, not server capacity to coordinate them.

We're not doing detailed capacity planning because the numbers are so far beyond realistic usage that it doesn't matter. If we ever hit limits, we'll know.

## Worker Resilience

Workers must survive server restarts without losing work.

### Behavior During Server Downtime

1. **Running job continues** - Worker finishes current job locally
2. **Connection drops** - Worker detects disconnect via heartbeat timeout
3. **Reconnect loop** - Exponential backoff: 1s → 2s → 4s → 8s → 16s → 30s (max)
4. **Result reporting** - On reconnect: "Job X finished with status Y while you were away"

### Implementation

```go
// Worker reconnect loop
func (w *Worker) maintainConnection(ctx context.Context) {
    backoff := time.Second
    maxBackoff := 30 * time.Second

    for {
        err := w.connect(ctx)
        if err == nil {
            backoff = time.Second // reset on success
            w.runLoop(ctx)        // blocks until disconnect
        }

        select {
        case <-ctx.Done():
            return
        case <-time.After(backoff + jitter()):
            backoff = min(backoff*2, maxBackoff)
        }
    }
}

// On reconnect, report any completed jobs
func (w *Worker) reportPendingResults() {
    for jobID, result := range w.pendingResults {
        w.send(JobComplete{JobID: jobID, Status: result.Status, ExitCode: result.ExitCode})
    }
}
```

### Server Tolerance

Server doesn't immediately mark workers as offline:

- **Heartbeat interval:** 30 seconds
- **Offline threshold:** 90 seconds (3 missed heartbeats)
- **Grace period for restarts:** Workers have 90s to reconnect before jobs are considered orphaned

## Database Configuration

### Postgres (Hosted)

```bash
# Fly Postgres
fly postgres create --name cinch-db --region ord

# Connect string in fly.toml
[env]
DATABASE_URL = "postgres://..."  # From fly postgres attach
```

### SQLite (Self-Hosted)

```bash
# Default - no config needed
./cinch server --data-dir /var/lib/cinch

# Explicit
export CINCH_DATA_DIR=/var/lib/cinch
./cinch server
```

### Selection Logic

```go
func NewStorage(cfg Config) (Storage, error) {
    if cfg.DatabaseURL != "" {
        // Postgres connection string provided
        return NewPostgresStorage(cfg.DatabaseURL)
    }
    // Default to SQLite
    return NewSQLiteStorage(filepath.Join(cfg.DataDir, "cinch.db"))
}
```

## Scaling Operations

### Scaling Web Server

```bash
# Check current size
fly status

# Scale up (causes restart, ~1 minute downtime)
fly scale vm performance-4x  # 4 vCPU, 32GB
fly scale vm performance-8x  # 8 vCPU, 64GB
fly scale vm performance-16x # 16 vCPU, 128GB
```

### Scaling Database

```bash
# Fly Postgres
fly postgres config update --vm-size dedicated-cpu-4x

# Or use external Postgres (RDS, etc.)
fly secrets set DATABASE_URL="postgres://user:pass@host:5432/db"
```

### Monitoring

Key metrics to watch:

- **WebSocket connections:** `cinch_websocket_connections`
- **Job queue depth:** `cinch_jobs_pending_total`
- **DB query latency:** `cinch_db_query_duration_seconds`
- **Memory usage:** Standard process metrics

## Why Not Horizontal Scaling?

**Complexity cost:**
- Distributed state coordination
- WebSocket session affinity
- Job dispatch across nodes
- Failure modes multiply

**When you actually need it:**
- 50,000+ concurrent workers
- 500,000+ jobs/day
- Global latency requirements

**What to do instead:**
1. Scale up web server (16 vCPU handles a lot)
2. Scale up Postgres (or move to RDS)
3. Offload logs to R2 (already done)
4. If still not enough: implement horizontal scaling then

## Future: Horizontal Scaling (If Needed)

If we ever need it, the path is:

1. **Multiple web servers** behind load balancer
2. **Sticky sessions** for WebSocket (by worker ID)
3. **Postgres** already supports multiple writers
4. **Job dispatch** via Postgres NOTIFY or Redis pub/sub

But we're explicitly not doing this until proven necessary. YAGNI.
