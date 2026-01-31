# Cinch TODO

**Last Updated:** 2026-01-30

**Goal:** Someone besides me uses this by Sunday (2026-02-02)

---

## Launch Blockers

These must be done before public launch. No exceptions.

### WebSocket Endpoint Split (MUST)

Separate WebSocket from HTTP so we can proxy HTTP through Cloudflare without hitting WS limits. Self-hosted users get single-domain simplicity. See `design/18-websocket-endpoint.md`.

- [x] **Server: CINCH_WS_BASE_URL config** - optional env var, defaults to same as BASE_URL
- [x] **Server: include ws_url in login response** - tells clients where to connect
- [x] **Client: save ws_url from login** - store in ~/.cinch/config alongside url
- [x] **Client: connect to ws_url** - worker uses ws_url instead of deriving from url
- [x] **Fly: add ws.cinch.sh cert** - `fly certs create ws.cinch.sh`
- [x] **Cloudflare: ws.cinch.sh DNS only** - gray cloud, direct to Fly

### Worker Resilience (MVP)

Workers must survive server restarts gracefully.

- [x] **Finish job on disconnect** - worker completes current job even if server goes away
- [x] **Reconnect with backoff** - exponential backoff with jitter (1s → 60s max)
- [x] **Report result on reconnect** - pending results stored to ~/.cinch/pending/, flushed on reconnect
- [x] **Heartbeat tolerance** - already implemented: 90s pong timeout (3 missed heartbeats)

### Self-Hosting MVP (MUST)

- [x] **Filesystem log store default** - logs stored to ~/.cinch/logs/ (NDJSON format)
- [x] **Object storage path** - R2 for hosted, filesystem default for self-hosted, CINCH_LOG_DIR configurable
- [ ] **Public webhook ingress guidance** - clear setup + base URL required for webhooks/OAuth
- [x] **Secrets (minimal)** - repo-level env secrets injection for jobs
- [x] **Labels/worker targeting** - wire `config.Workers` into job dispatch (MVP requirement)

### GitHub App Public

- [ ] **Make GitHub App public** - Currently private, strangers can't install

### Landing Page

- [ ] **cinch.sh landing page overhaul** - Explain the product in 10 seconds
- [ ] **Problem/solution/how it works/pricing** - All visible without scrolling
- [ ] **"GitHub is charging for self-hosted runners"** - Lead with the pain point

### Security Review

See `REVIEW.md` for full security audit. Critical issues must be fixed before launch.

**CRITICAL (must fix):**
- [x] **Public access to job logs** - Fixed: private repo logs require auth (REST + WebSocket)
- [x] **Webhook secret in API response** - Fixed: removed from repoResponse, only returned on create
- [x] **Unrestricted PR approval** - Fixed: only repo owner can approve (TODO: proper collaborator check)

**HIGH (should fix):**
- [x] **WebSocket origin checks** - Fixed: exact host match (not substring), prevents evil-cinch.sh.com bypass
- [x] **Webhook mutation before signature verify** - Fixed: signature verified before any state changes
- [x] **Sensitive GET endpoints public** - Fixed: private repo jobs/logs require auth; listJobs/listRepos filter private
- [x] **Device code weak entropy** - Fixed: now uses 8 alphanumeric chars (32^8 possibilities)
- [x] **Open redirect via return_to** - Fixed: URL parsing with exact hostname match
- [x] **listTokens public** - Fixed: now requires authentication
- [x] **GitHub App missing fork detection** - Fixed: sets IsFork, TrustLevel, Author fields on PR jobs

**MEDIUM (fix soon):**
- [x] **Forge tokens plaintext** - Fixed: AES-256-GCM encryption using JWT secret. Migration encrypts existing values.
- [ ] **Git token in clone URL** - Token visible in process list. Use git credential helper.
- [ ] **Worker ID collision** - Duplicate IDs overwrite without cleanup.

### Fair Use Limits

Prevent R2 abuse without restricting legitimate use. See `design/20-fair-use-limits.md`.

**Three tiers:**
| Resource | Free | Pro ($5/seat) | Self-Hosted |
|----------|------|---------------|-------------|
| Storage quota | 100 MB | 10 GB | Unlimited |
| Log retention | 7 days | 90 days | Unlimited |
| Workers | 10 | 1000 | Unlimited |
| Concurrent jobs | 5 | 100 | Unlimited |
| Job timeout max | 1 hour | 6 hours | Unlimited |

**Infrastructure (done):**
- [x] **Log compression** - Gzip on finalize (~10-30x savings), backwards compatible
- [x] **User tier model** - `tier` field: free/pro (self-hosted = no enforcement)
- [x] **Storage tracking fields** - `user.storage_used_bytes`, `job.log_size_bytes`
- [x] **Storage interface methods** - `UpdateJobLogSize`, `UpdateUserStorageUsed`, `GetUserByRepoID`
- [x] **Size tracking on finalize** - `Finalize()` returns compressed size, stored in job

**Enforcement (TODO):**
- [ ] **Quota check at job dispatch** - Block new jobs if user over quota
- [ ] **Aggregate user storage** - Update `user.storage_used_bytes` on job complete
- [ ] **Link repos to users** - Set `repos.owner_user_id` on repo create
- [ ] **Log retention cleanup** - Background job to delete old logs
- [ ] **Worker limit enforcement** - Reject registration when at limit
- [ ] **Usage API endpoint** - `GET /api/account/usage`

---

## Bugs to Fix

- [x] **Worker visibility broken** - Fixed 2026-01-29. Unauthenticated users get empty list. Personal workers only visible to owner.

- [ ] **No container config should fail, not default to ubuntu:22.04** - If no image/dockerfile/devcontainer specified, error with helpful message instead of silently using ubuntu:22.04

- [ ] **Container-first clarity** - if Docker missing, fail with explicit "install Docker or set `container: none`"

---

## After Launch: First Users

### User Acquisition

Network is unavailable (coworkers going through stuff). Need outside channels:

- [ ] **r/selfhosted post** - "I built a self-hosted CI that runs on your hardware" - low bar, they expect rough edges
- [ ] **awesome-selfhosted PR** - Get listed
- [ ] **Hacker News Show HN** - Higher risk/reward, needs polish first
- [ ] **Indie Hackers** - Journey post
- [ ] **One badge on someone else's repo** - Social proof

### Install Flow Polish

- [ ] **Zero to green checkmark without questions** - Can a stranger do it?
- [ ] **Error messages that help** - Not just "failed", but "here's what to do"

---

## Ready When Needed

### Postgres (One Weekend Away)

SQLite handles launch. Postgres is implemented and tested, ready to deploy if we hit scale issues.

- [x] **PostgresStorage implementation** - `internal/storage/postgres.go` implements full Storage interface
- [x] **Schema migrations** - Postgres-compatible schema (TIMESTAMPTZ, JSONB, proper indexes)
- [x] **Tests passing** - `TEST_DATABASE_URL=postgres://... go test ./internal/storage/...`
- [ ] **DATABASE_URL config** - wire up in main.go (30 min)
- [ ] **Deploy Fly Postgres** - `fly postgres create` (10 min)
- [ ] **Data migration** - export SQLite, import Postgres (see `design/19-postgres-migration.md`)

**When to migrate:** If SQLite write latency spikes or we need concurrent writers.

---

## After First Users: Billing

**Pricing decision: Keep it simple.**

$5/seat/month. One SKU. No personal/team split, no yearly commitment complexity. A seat is anyone who triggers a build on a private repo. Public repos free forever. Self-hosted free forever.

- [ ] Stripe checkout integration
- [ ] Seat counting logic (unique pushers to private repos per billing period)
- [ ] Public vs private repo detection
- [ ] Billing page in web UI
- [ ] Soft enforcement (warn, don't block)

---

## Polish (Post-Launch)

### Web UI
- [ ] Visual polish pass
- [ ] Distinctive badge design
- [ ] Light/dark theme toggle
- [ ] Loading skeletons

### Documentation
- [ ] Branch protection setup guide (GitHub rulesets)
- [ ] GitLab/Forgejo merge protection setup
- [ ] Troubleshooting: "why isn't my PR gated?"

### Daemon
- [ ] `cinch daemon scale N` to adjust worker count
- [ ] Windows support

### Known Issues
- [ ] Worker should check for docker/podman on startup (fail fast)
- [ ] Cache volumes need docs + configurability

### Pro Features (Post-Launch)
- [ ] **Artifacts (Pro only)** - Upload build artifacts, 10GB shared quota
- [ ] **Extended log retention** - 90 days (vs 7 days free)
- [ ] **Higher concurrency** - 100 concurrent jobs (vs 5 free)

---

## 2.x - Future (Unplanned)

### Horizontal Scaling

Not planned for MVP. If we outgrow a single 16-vCPU server + Postgres, we either:
1. Scale up to a bigger machine
2. Implement horizontal scaling at that point

See `design/11-scaling.md` for capacity estimates. A single beefy server handles 10,000+ workers, 100,000+ jobs/day.

### Edge Architecture

Move client-facing traffic to Cloudflare edge, eliminate Fly egress costs.

```
Client (JWT)
    │
    ▼
a.cinch.sh (Cloudflare Worker)
    │
    ├── ACL check: GET cinch.fly.dev/api/acl?resource=/logs/j_xxx
    │
    └── If 200: fetch from R2, stream to client (free egress)
```

- [ ] Create `a.cinch.sh` Cloudflare Worker
- [ ] ACL endpoint on Fly
- [ ] Direct uploads via presigned URLs

### Features

**Artifacts:**
- [ ] `cinch artifact upload dist/*`
- [ ] Artifact browser in web UI

**Build Cache:**
- [ ] Cache layers in R2
- [ ] LRU eviction

**Parallel Builds:**
```yaml
build:
  - make build
  - make test
```

**Worker TUI:**
- [ ] bubbletea TUI for `cinch worker`

### Forge Expansion
- [ ] Azure DevOps, AWS CodeCommit (demand-driven)

---

## Done

### MVP 1.9 - Polish (2026-01-29)

- ✅ Retry failed jobs from web UI
- ✅ `cinch status` - check job status from CLI
- ✅ `cinch logs` / `cinch logs -f` - fetch or stream logs from CLI
- ✅ Worker list in web UI with live updates
- ✅ Remote worker control (shutdown/disconnect buttons)
- ✅ Unique worker IDs per machine
- ✅ Worker visibility model (personal = owner only, shared = all authenticated)

### MVP 1.8 - Worker Trust Model (2026-01-28)

- ✅ Personal/shared worker modes
- ✅ Dispatch priority based on author and trust level
- ✅ `pending_contributor` status for fork PRs
- ✅ Web UI approval flow for fork PRs

### MVP 1.7 - PR/MR Support (2026-01-28)

- ✅ GitHub/GitLab/Forgejo PR events
- ✅ Status checks on PR head commit
- ✅ PR fields in Job struct and API
- ✅ Web UI displays PR info

### MVP 1.6 - Logs → R2 (2026-01-28)

- ✅ R2 bucket setup
- ✅ LogStore interface with SQLite fallback
- ✅ Log streaming to R2
- ✅ Log retrieval from R2

### MVP 1.5 - Daemon (2026-01-28)

- ✅ `cinch daemon start/stop/status/logs`
- ✅ launchd/systemd service management
- ✅ Internal parallelism (`-n 4`)
- ✅ Graceful shutdown

### MVP 1.4 - Forge Expansion (2026-01-27)

- ✅ GitLab full integration
- ✅ Forgejo/Gitea hybrid flow
- ✅ `make push` to all forges

### MVP 1.3 - Public Launch Prep (2026-01-26)

- ✅ Build badges
- ✅ Queue reliability
- ✅ Unified onboarding
- ✅ Public repo pages

### MVP 1.2 - Container Execution (2026-01-24)

- ✅ Docker/Podman runtime
- ✅ Cache volumes
- ✅ `cinch release` command
- ✅ Bootstrap loop closed

### MVP 1.1 - Releases (2026-01-24)

- ✅ Tag detection
- ✅ Forge tokens passed to jobs
- ✅ Install script

### MVP 1.0 - Core CI (2026-01-23)

- ✅ Single binary
- ✅ Webhooks + status API
- ✅ GitHub App with Checks API
- ✅ OAuth + device flow
- ✅ Job queue with WebSocket dispatch
- ✅ Real-time log streaming
- ✅ Deployed to Fly.io

---

## Architecture Reference

### Environment Variables

```bash
CINCH_REF=refs/heads/main
CINCH_BRANCH=main
CINCH_TAG=
CINCH_COMMIT=abc1234567890
CINCH_JOB_ID=j_abc123
CINCH_REPO=https://github.com/owner/repo.git
CINCH_FORGE=github
GITHUB_TOKEN=ghs_xxx
CINCH_FORGE_TOKEN=xxx
```

### Scaling Architecture (V1)

```
┌─────────────────────────────────────────────────────────────┐
│                     Cloudflare (CDN)                        │
│                     R2 (logs/artifacts)                     │
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

Single web server. Single database. Vertical scaling.
Server restart = ~1 minute downtime. Workers retry automatically.
```

**Capacity:** Back-of-napkin math says way more than we'll ever need. See design/11-scaling.md.
