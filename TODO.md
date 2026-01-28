# Cinch TODO

**Last Updated:** 2026-01-28

---

## Current: MVP 1.7 - Build Cache

---

**The killer feature.** Fast builds are the #1 thing that makes CI good.

Cache layers in R2, shared across builds. Think "docker layer caching as a service."

```yaml
# .cinch.yaml
cache:
  - node_modules/
  - ~/.cache/go-build/
  - target/
```

**Implementation:**
- [ ] Cache manifest format
- [ ] Upload cache layers to R2 after build
- [ ] Download cache layers before build
- [ ] LRU eviction when quota exceeded
- [ ] Cache hit/miss metrics in UI

**Storage pricing (design now, implement with Stripe):**
- 10GB free
- $10/mo per 100GB additional

---

## Then: MVP 1.8 - PR/MR Support

Currently push-only. PRs are table stakes for real adoption.

- [ ] GitHub Pull Request events
- [ ] GitLab Merge Request events
- [ ] Forgejo/Gitea Pull Request events
- [ ] Status checks on PR head commit

---

## Then: MVP 1.9 - Stripe Integration

**Pricing:** Public repos free, private repos $5/seat/month, self-hosted free.

- [ ] Stripe checkout integration
- [ ] Seat counting logic
- [ ] Public vs private repo detection
- [ ] Payment prompt during onboarding (private repo selected → pay first)
- [ ] Billing page in web UI
- [ ] Storage quota billing (for cache overage)

---

## 2.x - Edge Architecture

Move client-facing traffic to Cloudflare edge, eliminate Fly egress costs.

### a.cinch.sh - Artifact/Storage Worker

Single Cloudflare Worker at `a.cinch.sh` fronting all R2 storage. Fly handles auth only.

```
Client (JWT)
    │
    ▼
a.cinch.sh (Cloudflare Worker)
    │
    ├── ACL check: GET cinch.fly.dev/api/acl?resource=/logs/j_xxx
    │              (tiny request, Fly validates JWT + permissions)
    │
    └── If 200: fetch from R2, stream to client (free egress)
        If 403: return forbidden
```

**Routes:**
```
a.cinch.sh/logs/{job_id}/final.log   → R2: logs/{job_id}/final.log
a.cinch.sh/cache/{repo}/{hash}       → R2: cache/{repo}/{hash}
a.cinch.sh/artifacts/{job_id}/{name} → R2: artifacts/{job_id}/{name}
```

**Implementation:**
- [ ] Create `a.cinch.sh` subdomain on Cloudflare
- [ ] Worker: extract JWT from Authorization header
- [ ] Worker: ACL check to Fly origin (forward JWT)
- [ ] Worker: on 200, fetch from R2 and stream response
- [ ] Worker: cache ACL responses for 60s (reduce Fly calls)
- [ ] Fly: `/api/acl` endpoint - validate JWT, check resource access

**ACL endpoint (Fly):**
```go
GET /api/acl?resource=/logs/j_xxx
Authorization: Bearer <cinch-jwt>

// Check: does this user have access to this job's repo?
// Return: 200 OK or 403 Forbidden
```

**Cost savings:**
- Log reads: 0 Fly egress (was 100%)
- Cache downloads: 0 Fly egress
- Artifact downloads: 0 Fly egress
- Only ACL checks hit Fly (~100 bytes each)

### Direct Uploads (Future)

For writes, workers/CLI could upload directly to R2 with presigned URLs:
- [ ] Server generates presigned PUT URL
- [ ] Client uploads directly to R2
- [ ] Server never sees the data

---

## Future: Scale & Polish

### Postgres + Multi-Node (when needed)

Current single-node SQLite handles ~100 concurrent jobs. When you need HA or more:

- [ ] Postgres storage backend (interface already abstracted)
- [ ] Postgres NOTIFY for cross-node job dispatch
- [ ] `fly scale count 3`

### Worker TUI (nice to have)

The bubbletea TUI for `cinch worker` - makes running a worker feel alive. Deferred because daemon covers the core need.

```
┌─────────────────────────────────────────────────────────────┐
│  CINCH WORKER                              ehrlich@macbook  │
├─────────────────────────────────────────────────────────────┤
│  ● CONNECTED to cinch.sh                    3 repos         │
│  ┌─ CURRENT BUILD ────────────────────────────────────────┐ │
│  │  owner/repo @ main (abc1234)                           │ │
│  │  ████████████░░░░░░░░░░░░░░░░░  38%  2m 14s            │ │
│  └────────────────────────────────────────────────────────┘ │
│  [q] quit  [l] logs  [r] retry                              │
└─────────────────────────────────────────────────────────────┘
```

- [ ] bubbletea TUI for `cinch worker` attach mode
- [ ] Real-time log streaming in TUI
- [ ] Recent jobs list (last 5-10)
- [ ] Progress estimation from log patterns
- [ ] Keyboard navigation (view full logs, quit, etc.)
- [ ] Retry from TUI (`r` key on failed build)

### Parallel Builds

```yaml
# Array = parallel jobs (no DAG)
build:
  - make build
  - make test
  - make docs
```

Array items fan out as independent parallel jobs. No DAG, no workflow DSL.

### Artifacts

Native artifact storage (beyond `cinch release` which pushes to forge releases). Only useful after parallel builds exist (cross-job artifact sharing).

- [ ] `cinch artifact upload dist/*`
- [ ] Artifact download in subsequent jobs
- [ ] Artifact browser in web UI

### Distinctive Badge Design

**Problem:** Shields.io badges are invisible - everyone uses them.

- [ ] Design distinctive badge style (shape, colors, typography)
- [ ] Implement custom SVG rendering
- [ ] Remove shields.io redirect, serve SVG directly

---

## Backlog

### Web UI Polish
- [ ] Light/dark theme toggle
- [ ] Loading skeletons
- [ ] Real Settings page (tokens, repos)
- [ ] Badge repo selector (generate badge markdown)
- [ ] Retry from web UI (button on failed job)
- [ ] Remote worker shutdown (web UI → graceful stop signal)
- [ ] Worker list in web UI (see all connected workers)

### Daemon Polish
- [ ] `cinch daemon scale N` to adjust running worker count
- [ ] Windows support (probably just "run in terminal" for now)

### CLI Polish
- [ ] `cinch status` - check job status from CLI
- [ ] `cinch logs -f` - stream logs from CLI

### Known Issues
- [ ] Worker should check for docker/podman on startup (fail fast)
- [ ] Cache volumes need docs + configurability

### Forge Expansion (Low Priority)
- [ ] GitHub web onboarding (not just GitHub App)
- [ ] Azure DevOps, AWS CodeCommit, Gitee (demand-driven)

---

## Done

### MVP 1.6 - Logs → R2 (2026-01-28)

Job logs stored in Cloudflare R2 instead of SQLite.

- ✅ R2 bucket setup (`cinch` bucket)
- ✅ LogStore interface with SQLite fallback
- ✅ R2 implementation with 256KB buffer + 30s flush
- ✅ Log streaming to R2 (chunks during job, final.log on complete)
- ✅ Log retrieval from R2 (web UI, API)
- [ ] Retention policy (30 days free tier, configurable for paid)
- [ ] Migration: existing SQLite logs → R2

### MVP 1.5 - Daemon (2026-01-28)

Background worker daemon with internal parallelism via Unix socket.

- ✅ `cinch daemon start/stop/status/logs` commands
- ✅ `cinch daemon install/uninstall` for service management
- ✅ launchd plist generation for macOS
- ✅ systemd unit generation for Linux
- ✅ Internal parallelism (`-n 4` = 4 concurrent job slots)
- ✅ `cinch worker` connects to daemon, streams job events
- ✅ `cinch worker -s` standalone mode (temp daemon + viewer)
- ✅ Graceful shutdown with job quiescing
- ✅ `cinch install --with-daemon` option

### MVP 1.4 - Forge Expansion (2026-01-27)

Multi-forge support complete. Cinch hosted on GitHub, GitLab, and Codeberg simultaneously.

- ✅ GitLab: Full integration (OAuth + auto-webhook + PAT fallback + self-hosted)
- ✅ Forgejo/Gitea: Hybrid flow (OAuth webhook + manual PAT + self-hosted)
- ✅ Codeberg differentiation (shows "CODEBERG" not "FORGEJO" in UI)
- ✅ `make push` pushes to all forges, releases land on all three

### MVP 1.3 - Public Launch Prep (2026-01-26)

- ✅ Build badges via shields.io (`/badge/{forge}/{owner}/{repo}.svg`)
- ✅ Queue reliability (jobs re-queue on disconnect/restart)
- ✅ Unified onboarding (login = onboarding, email-based identity)
- ✅ GitHub App consolidation (one OAuth, not two)
- ✅ Account settings (connect/disconnect forges)
- ✅ Web UI redesign (routing, empty states, error handling, relative timestamps)
- ✅ Public repo pages (`cinch.sh/jobs/github.com/owner/repo`)

### MVP 1.2 - Container Execution (2026-01-24)

- ✅ Container runtime (Docker/Podman) with image/dockerfile/devcontainer support
- ✅ Cache volumes for warm builds
- ✅ Multi-platform binaries, Linux binary injected into containers
- ✅ `cinch release` command
- ✅ Bootstrap loop closed (Cinch releases itself)

### MVP 1.1 - Releases (2026-01-24)

- ✅ Tag detection and `CINCH_TAG` env var
- ✅ Forge tokens passed to jobs
- ✅ Install script and `make release` target

### MVP 1.0 - Core CI (2026-01-23)

- ✅ Single binary (server, worker, CLI, web UI)
- ✅ GitHub/Forgejo/Gitea webhooks + status API
- ✅ GitHub App with Checks API
- ✅ OAuth + device flow auth
- ✅ Job queue with WebSocket dispatch
- ✅ Real-time log streaming
- ✅ Deployed to Fly.io

### Tech Debt (2026-01-28)

- ✅ Break up web/src/App.tsx - Split into components/pages (2200 lines → 184 + modules)
- ✅ Non-TTY mode for worker output (`[worker]`/`[job]` prefixes, no ANSI decoration)

---

## Architecture Reference

### Environment Variables

```bash
CINCH_REF=refs/heads/main       # Full ref
CINCH_BRANCH=main               # Branch (empty for tags)
CINCH_TAG=                      # Tag (empty for branches)
CINCH_COMMIT=abc1234567890      # Commit SHA
CINCH_JOB_ID=j_abc123
CINCH_REPO=https://github.com/owner/repo.git
CINCH_FORGE=github              # github, gitlab, forgejo, gitea
GITHUB_TOKEN=ghs_xxx            # Forge-specific token
CINCH_FORGE_TOKEN=xxx           # Always set
```

### Adding a New Forge

```go
// 1. Implement interface in internal/forge/newforge.go
// 2. Add type constant in internal/forge/forge.go
// 3. Add to factory switch statement
// 4. Register in cmd/cinch/main.go
```

### Scaling Architecture (Future)

```
                    ┌─────────────────┐
                    │   Cloudflare    │
                    │   (CDN + R2)    │
                    └────────┬────────┘
                             │
              ┌──────────────┼──────────────┐
              │              │              │
        ┌─────▼─────┐  ┌─────▼─────┐  ┌─────▼─────┐
        │  Fly Node │  │  Fly Node │  │  Fly Node │
        │  (web/ws) │  │  (web/ws) │  │  (web/ws) │
        └─────┬─────┘  └─────┬─────┘  └─────┬─────┘
              │              │              │
              └──────────────┼──────────────┘
                             │
                    ┌────────▼────────┐
                    │    Postgres     │
                    │  (jobs, repos)  │
                    └─────────────────┘

Workers connect via websocket to any Fly node.
Jobs dispatched via Postgres NOTIFY.
Logs/cache stored in R2.
```
