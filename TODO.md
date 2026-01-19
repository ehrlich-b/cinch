# Cinch v0.1 Implementation TODO

Target: Private development → clean up → single big commit before public release

## Phase 0: Project Scaffolding

### Go Project Setup
- [ ] Initialize go.mod (`go mod init github.com/cinch-sh/cinch`)
- [ ] Create directory structure per design/00-overview.md
- [ ] Add Makefile (build, test, fmt, lint targets)
- [ ] Set up main.go with cobra CLI skeleton
- [ ] Add .gitignore (binaries, .env, etc.)

### Frontend Setup (Minimal React + Vite)
- [ ] Set up web/ directory with Vite + React 18
- [ ] package.json with minimal deps:
  - [ ] react, react-dom
  - [ ] vite, @vitejs/plugin-react
  - [ ] ansi-to-html (for log rendering)
  - [ ] That's it. No state libs, no component libs, no CSS frameworks.
- [ ] vite.config.ts (build to web/dist/)
- [ ] Add `make web` target to build frontend
- [ ] Create web/embed.go with `//go:embed dist/*` directive
- [ ] CSS: vanilla CSS modules, dark theme, mobile-responsive

---

## Phase 1: Core Infrastructure (Days 1-4)

### CLI Routing (cmd/cinch/)
- [ ] `cinch server` - starts control plane
- [ ] `cinch worker` - starts worker
- [ ] `cinch run` - local execution (stub)
- [ ] `cinch status` - check job status (stub)
- [ ] `cinch logs` - stream logs (stub)
- [ ] `cinch config validate` - validate config file
- [ ] `cinch token create` - create worker token
- [ ] Version flag and help text

### Configuration (internal/config/)
- [ ] Define Config struct matching design/08-config-format.md
- [ ] YAML parser (.cinch.yaml)
- [ ] TOML parser (.cinch.toml)
- [ ] JSON parser (.cinch.json)
- [ ] Config discovery: .cinch.yaml > .cinch.toml > .cinch.json
- [ ] Validation (required fields, timeout parsing, etc.)
- [ ] Unit tests for all formats

### Storage Layer (internal/storage/) - tunn pattern
- [ ] Define `Storage` interface (all DB ops go through this)
  - [ ] Jobs: Create, Get, List, UpdateStatus
  - [ ] Workers: Create, Get, List, UpdateLastSeen
  - [ ] Repos: Create, Get, GetByCloneURL, List
  - [ ] Tokens: Create, Validate, Revoke
  - [ ] Logs: Append, Get
  - [ ] Available() bool
- [ ] `SQLiteStorage` implementation (v0.1, ships free)
- [ ] Stub `ProxyStorage` interface (gRPC to login node, v0.2)
- [ ] Interface designed for future backends:
  - [ ] `PostgresStorage` (paid support, multi-instance)
  - [ ] `MySQLStorage` (paid support, if someone pays)
- [ ] SQLite setup (modernc.org/sqlite, pure Go, no CGO)
- [ ] Migrations system (embed SQL files, per-backend)
- [ ] Tables:
  - [ ] workers (id, name, labels, status, last_seen, created_at)
  - [ ] repos (id, forge_type, clone_url, webhook_secret, forge_token)
  - [ ] jobs (id, repo_id, commit, branch, status, exit_code, started_at, finished_at, worker_id)
  - [ ] job_logs (id, job_id, stream, data, created_at)
  - [ ] tokens (id, name, hash, worker_id, created_at, revoked_at)
- [ ] Unit tests with in-memory SQLite

### Worker Registry / Hub (internal/server/hub.go)
- [ ] `Hub` struct - tracks live WebSocket connections
- [ ] `WorkerConn` struct - worker_id, labels, conn, active jobs, last ping
- [ ] Register/unregister worker on connect/disconnect
- [ ] Get available workers by label (for job dispatch)
- [ ] Least-loaded worker selection
- [ ] Broadcast to all workers (future: status updates)
- [ ] Note: This is separate from Storage - Storage tracks persistent info, Hub tracks live connections

### Protocol Types (internal/protocol/)
- [ ] Define message types per design/02-protocol.md:
  - [ ] AUTH (token)
  - [ ] AUTH_OK / AUTH_FAIL
  - [ ] REGISTER (labels, capabilities)
  - [ ] JOB_ASSIGN (job details, clone token)
  - [ ] LOG_CHUNK (job_id, stream, data)
  - [ ] JOB_COMPLETE (job_id, exit_code)
  - [ ] JOB_ERROR (job_id, error)
  - [ ] PING / PONG
- [ ] JSON marshal/unmarshal helpers
- [ ] Message envelope with type discrimination

---

## Phase 2: WebSocket & Worker (Days 5-8)

### Server WebSocket Hub (internal/server/ws.go)
- [ ] WebSocket upgrade handler
- [ ] Worker connection registry (map of connected workers)
- [ ] Worker authentication (token validation)
- [ ] Message routing (dispatch to specific worker)
- [ ] Broadcast (status updates to UI clients)
- [ ] Connection cleanup on disconnect
- [ ] Ping/pong keepalive
- [ ] Unit tests with mock connections

### Job Dispatch (internal/server/dispatch.go)
- [ ] Job queue (pending jobs waiting for workers)
- [ ] Worker selection (least-loaded with matching labels)
- [ ] Job assignment (send JOB_ASSIGN, mark in-progress)
- [ ] Job completion handler (update status, post to forge)
- [ ] Timeout handling (mark failed if worker goes silent)
- [ ] Fan-out: create N jobs for N worker labels

### Worker Connection (internal/worker/worker.go)
- [ ] WebSocket connect with reconnect backoff
- [ ] Token-based authentication
- [ ] REGISTER message on connect
- [ ] Job receive loop
- [ ] Graceful shutdown (finish current job)
- [ ] Config: server URL, token, labels

### Git Clone (internal/worker/clone.go)
- [ ] Clone with short-lived token
- [ ] Shallow clone (--depth=1)
- [ ] Checkout specific commit
- [ ] Cleanup after job

### Bare Metal Execution (internal/worker/executor.go)
- [ ] Execute command (exec.Command)
- [ ] Stream stdout/stderr to server
- [ ] Capture exit code
- [ ] Timeout handling
- [ ] Environment variable injection
- [ ] Working directory setup

### Log Streaming (internal/worker/stream.go)
- [ ] Chunk stdout/stderr (max 64KB per message)
- [ ] Rate limiting (don't flood server)
- [ ] Buffer and batch small outputs
- [ ] Handle large outputs gracefully

---

## Phase 3: Containerization (Days 7-8)

### Container Interface (internal/worker/container/)
- [ ] ContainerRuntime interface (Run, Stop, Pull)
- [ ] Docker implementation via CLI (docker run, docker stop)

### Devcontainer Detection (internal/worker/container/devcontainer.go)
- [ ] Check for .devcontainer/devcontainer.json
- [ ] Parse devcontainer.json (image, dockerFile, build.dockerfile)
- [ ] Check for .devcontainer/Dockerfile
- [ ] Check for repo root Dockerfile
- [ ] Fallback to default image (cinch-builder:latest or alpine)
- [ ] Build image if Dockerfile specified

### Container Execution (internal/worker/container/docker.go)
- [ ] docker run with:
  - [ ] Workspace mount (-v)
  - [ ] Cache volume mounts per design/09-containerization.md
  - [ ] Environment variables (--env)
  - [ ] Working directory (--workdir)
  - [ ] Remove after exit (--rm)
- [ ] Capture stdout/stderr
- [ ] Capture exit code
- [ ] Timeout handling (docker stop)

### Cache Volumes (internal/worker/container/cache.go)
- [ ] Create ~/.cinch/cache/{npm,cargo,pip,go,ccache}
- [ ] Mount mappings:
  - [ ] npm → ~/.npm
  - [ ] cargo → ~/.cargo
  - [ ] pip → ~/.cache/pip
  - [ ] go → ~/go/pkg
- [ ] Custom cache support from config
- [ ] Volume creation on first use

### Service Containers (internal/worker/container/services.go)
- [ ] Parse services from config
- [ ] Start service containers before build
- [ ] Health check polling (pg_isready, redis-cli ping, etc.)
- [ ] DNS/network setup (--network)
- [ ] Inject service env vars (DATABASE_URL, etc.)
- [ ] Stop services after build

---

## Phase 4: Forge Integrations (Days 9-11)

### Forge Interface (internal/forge/)
- [ ] Forge interface:
  - [ ] VerifyWebhook(signature, body) bool
  - [ ] ParsePushEvent(body) (repo, commit, branch)
  - [ ] PostStatus(repo, commit, state, context, url)
  - [ ] FetchConfig(repo, commit) ([]byte, error)
  - [ ] CreateCloneToken(repo) (token, expiry)

### GitHub (internal/forge/github.go)
- [ ] HMAC-SHA256 webhook verification
- [ ] Push event parsing
- [ ] Status API (POST /repos/:owner/:repo/statuses/:sha)
- [ ] Contents API (fetch .cinch.yaml)
- [ ] Installation token for clone
- [ ] OAuth app token refresh
- [ ] Unit tests with recorded responses

### Forgejo/Gitea (internal/forge/forgejo.go)
- [ ] HMAC-SHA256 webhook verification (same as GitHub)
- [ ] Push event parsing (slightly different payload)
- [ ] Status API (POST /api/v1/repos/:owner/:repo/statuses/:sha)
- [ ] Contents API
- [ ] Token handling
- [ ] Unit tests

### Webhook Handler (internal/server/webhook.go)
- [ ] POST /webhooks/github
- [ ] POST /webhooks/forgejo
- [ ] Signature verification per forge
- [ ] Event parsing (push only for v0.1)
- [ ] Config fetch and validation
- [ ] Job creation
- [ ] Initial status post (pending)

---

## Phase 5: HTTP API (Day 11-12)

### API Handlers (internal/server/api.go)
- [ ] GET /api/jobs - list jobs (with pagination, filters)
- [ ] GET /api/jobs/:id - job details
- [ ] GET /api/jobs/:id/logs - job logs (full)
- [ ] GET /api/workers - list connected workers
- [ ] GET /api/repos - list configured repos
- [ ] POST /api/repos - add repo (generates webhook secret)
- [ ] DELETE /api/repos/:id - remove repo
- [ ] POST /api/tokens - create worker token
- [ ] DELETE /api/tokens/:id - revoke token

### WebSocket for UI (internal/server/ws.go)
- [ ] /ws/logs/:job_id - real-time log streaming
- [ ] /ws/status - job status updates broadcast
- [ ] Authentication (session or token)

---

## Phase 6: Web UI (Days 12-13)

### Static Assets
- [ ] Basic layout (header, sidebar, main content)
- [ ] Dark theme CSS
- [ ] Responsive (mobile-friendly)

### Dashboard Page (/)
- [ ] Recent jobs list
- [ ] Status icons (✓ ✗ ◐ ◷)
- [ ] Filter by repo, status, branch
- [ ] Pagination
- [ ] Worker count indicator in header
- [ ] Auto-refresh via WebSocket

### Job Detail Page (/jobs/:id)
- [ ] Job metadata (repo, branch, commit, worker, duration)
- [ ] Real-time log viewer
- [ ] ANSI color rendering
- [ ] Auto-scroll with "pause" button
- [ ] Copy logs button
- [ ] Download raw logs

### Workers Page (/workers)
- [ ] List connected workers
- [ ] Status (connected/disconnected)
- [ ] Labels
- [ ] Current job (if any)
- [ ] Last seen
- [ ] Add worker instructions (shows token)

### Settings Page (/settings) - optional for v0.1
- [ ] Add/remove repos
- [ ] View webhook URLs and secrets
- [ ] Token management

---

## Phase 7: CLI Completion (Day 13-14)

### cinch run
- [ ] Load local config
- [ ] Execute command locally (bare metal or container)
- [ ] Stream logs to terminal
- [ ] Exit with command's exit code
- [ ] Support --bare-metal flag

### cinch status
- [ ] Detect repo from .git
- [ ] Query server API
- [ ] Display recent builds
- [ ] Color-coded status

### cinch logs
- [ ] Stream logs from server
- [ ] ANSI passthrough
- [ ] Follow mode (-f)

### cinch config validate
- [ ] Load and validate config
- [ ] Report errors with line numbers

### cinch token
- [ ] create: generate and print token
- [ ] list: show active tokens
- [ ] revoke: revoke by ID

---

## Phase 8: Polish & Release (Day 14)

### Documentation
- [ ] README.md (quick start, self-hosted setup)
- [ ] install.sh (curl | sh installer)
- [ ] Worker setup guide
- [ ] Config reference

### Testing
- [ ] Integration test: push → webhook → job → status
- [ ] Test with real GitHub repo
- [ ] Test with Forgejo instance
- [ ] Test Docker execution
- [ ] Test devcontainer detection
- [ ] Test service containers

### Release
- [ ] Build binaries (linux/amd64, linux/arm64, darwin/arm64, darwin/amd64)
- [ ] Create GitHub release (after going public)
- [ ] Docker image (optional)

---

## Out of Scope (v0.2+)

- [ ] GitLab integration
- [ ] Bitbucket integration
- [ ] Postgres support (multi-instance)
- [ ] Scheduled builds
- [ ] Manual triggers
- [ ] Build badges
- [ ] User authentication in web UI
- [ ] Notifications (Slack, email)
- [ ] Resource limits
- [ ] Artifact storage
- [ ] Trigger filtering (branch patterns)

---

## Open Questions

1. ~~**Frontend:** Vanilla JS vs Preact+Vite?~~ → **Minimal React + Vite** (resolved)
2. ~~**ANSI rendering:**~~ → **ansi-to-html** lib (resolved)
3. **Auth for web UI:** Skip for v0.1 (assume localhost/trusted network)?
4. **Default container image:** Build our own cinch-builder or use alpine?
5. **Worker labels:** Predefined set or freeform strings?
