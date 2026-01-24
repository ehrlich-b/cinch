# Cinch v0.1 Implementation TODO

Target: Private development → clean up → single big commit before public release

**Last reviewed:** 2025-01-19

---

## Phase 0: Project Scaffolding ✅

### Go Project Setup
- [x] Initialize go.mod (`go mod init github.com/cinch-sh/cinch`)
- [x] Create directory structure per design/00-overview.md
- [x] Add Makefile (build, test, fmt, lint targets)
- [x] Set up main.go with cobra CLI skeleton
- [x] Add .gitignore (binaries, .env, etc.)

### Frontend Setup (Minimal React + Vite)
- [x] Set up web/ directory with Vite + React 18
- [x] package.json with minimal deps (react, vite, etc.)
- [x] vite.config.ts (build to web/dist/)
- [x] Create web/embed.go with `//go:embed dist/*` directive
- [ ] CSS: vanilla CSS modules, dark theme, mobile-responsive *(basic styles only)*

---

## Phase 1: Core Infrastructure ✅

### CLI Routing (cmd/cinch/)
- [x] CLI skeleton with all subcommands defined
- [x] `cinch run` - local execution (WORKING)
- [x] `cinch config validate` - validate config file (WORKING)
- [ ] `cinch server` - starts control plane *(stub only - needs wiring)*
- [ ] `cinch worker` - starts worker *(stub only - needs wiring)*
- [ ] `cinch status` - check job status *(stub only)*
- [ ] `cinch logs` - stream logs *(stub only)*
- [ ] `cinch token create` - create worker token *(stub only)*
- [x] Version flag and help text

### Configuration (internal/config/)
- [x] Define Config struct matching design/08-config-format.md
- [x] YAML parser (.cinch.yaml)
- [x] TOML parser (.cinch.toml)
- [x] JSON parser (.cinch.json)
- [x] Config discovery: .cinch.yaml > .cinch.toml > .cinch.json
- [x] Validation (required fields, timeout parsing, etc.)
- [x] Unit tests for all formats

### Storage Layer (internal/storage/)
- [x] Define `Storage` interface (all DB ops go through this)
  - [x] Jobs: Create, Get, List, UpdateStatus
  - [x] Workers: Create, Get, List, UpdateLastSeen
  - [x] Repos: Create, Get, GetByCloneURL, List
  - [x] Tokens: Create, Validate, Revoke
  - [x] Logs: Append, Get
- [x] `SQLiteStorage` implementation
- [x] SQLite setup (modernc.org/sqlite, pure Go, no CGO)
- [x] Migrations (inline in sqlite.go)
- [x] Unit tests with in-memory SQLite

### Worker Registry / Hub (internal/server/hub.go)
- [x] `Hub` struct - tracks live WebSocket connections
- [x] `WorkerConn` struct - worker_id, labels, conn, active jobs, last ping
- [x] Register/unregister worker on connect/disconnect
- [x] Get available workers by label (for job dispatch)
- [x] Least-loaded worker selection
- [x] Broadcast to all workers

### Protocol Types (internal/protocol/)
- [x] Define message types per design/02-protocol.md
- [x] JSON marshal/unmarshal helpers
- [x] Message envelope with type discrimination
- [x] Unit tests

---

## Phase 2: WebSocket & Worker ✅

### Server WebSocket Hub (internal/server/ws.go)
- [x] WebSocket upgrade handler
- [x] Worker authentication (token validation)
- [x] Message routing (dispatch to specific worker)
- [x] Connection cleanup on disconnect
- [x] Ping/pong keepalive
- [x] Handle all message types (REGISTER, JOB_ACK, LOG_CHUNK, JOB_COMPLETE, etc.)
- [x] Unit tests

### Job Dispatch (internal/server/dispatch.go)
- [x] Job queue (pending jobs waiting for workers)
- [x] Worker selection (least-loaded with matching labels)
- [x] Job assignment (send JOB_ASSIGN, mark in-progress)
- [x] Job completion handler (update status)
- [x] Timeout handling (mark failed if worker goes silent)
- [x] Stale worker detection and cleanup
- [x] Unit tests

### Worker Client (internal/worker/worker.go)
- [x] WebSocket connect with reconnect backoff
- [x] Token-based authentication
- [x] REGISTER message on connect
- [x] Job receive loop
- [x] Graceful shutdown (finish current job)
- [x] Config: server URL, token, labels

### Git Clone (internal/worker/clone.go)
- [x] Clone with token authentication
- [x] Shallow clone (--depth=1)
- [x] Checkout specific commit
- [x] Cleanup after job
- [x] Unit tests

### Bare Metal Execution (internal/worker/executor.go)
- [x] Execute command (exec.Command)
- [x] Capture exit code
- [x] Environment variable injection
- [x] **Cinch env vars:** `CINCH_BRANCH`, `CINCH_COMMIT`, `CINCH_REPO`, `CINCH_JOB_ID`
- [x] Working directory setup
- [x] Unit tests

### Log Streaming (internal/worker/stream.go)
- [x] Stream stdout/stderr to server
- [x] Chunk output (max 64KB per message)
- [x] Buffer and batch small outputs
- [x] Unit tests

---

## Phase 3: Containerization ✅

### Container Detection (internal/worker/container/detect.go)
- [x] Check for .devcontainer/devcontainer.json
- [x] Parse devcontainer.json (image, dockerFile, build.dockerfile)
- [x] Check for .devcontainer/Dockerfile
- [x] Check for repo root Dockerfile
- [x] Fallback to default image (ubuntu:22.04)
- [x] Unit tests

### Container Execution (internal/worker/container/docker.go)
- [x] docker run with workspace mount
- [x] Cache volume mounts
- [x] Environment variables
- [x] Working directory
- [x] Capture exit code
- [x] Build from Dockerfile
- [x] Pull images

### Cache Volumes
- [x] Default cache mappings (npm, cargo, pip, go)
- [x] Volume creation on first use

### Service Containers (internal/worker/container/services.go)
- [x] Parse services from config
- [x] Start service containers before build
- [x] Health check polling
- [x] Network setup for service communication
- [x] Stop services after build

---

## Phase 4: Forge Integrations ✅

### Forge Interface (internal/forge/forge.go)
- [x] Forge interface defined:
  - [x] Identify(r *http.Request) bool
  - [x] ParsePush(r, secret) (*PushEvent, error)
  - [x] PostStatus(ctx, repo, commit, status) error
  - [x] CloneToken(ctx, repo) (string, time.Time, error)

### GitHub (internal/forge/github.go)
- [x] HMAC-SHA256 webhook verification
- [x] Push event parsing
- [x] Status API (POST /repos/:owner/:repo/statuses/:sha)
- [x] Clone token support
- [x] Unit tests

### Forgejo/Gitea (internal/forge/forgejo.go)
- [x] HMAC-SHA256 webhook verification
- [x] Push event parsing
- [x] Status API
- [x] Clone token support
- [x] Unit tests

### Webhook Handler (internal/server/webhook.go)
- [x] POST /webhooks handler
- [x] Forge identification (GitHub vs Forgejo)
- [x] Signature verification per forge
- [x] Event parsing (push)
- [x] Job creation
- [x] Initial status post (pending)
- [x] Queue job for dispatch

---

## Phase 5: HTTP API ✅

### API Handlers (internal/server/api.go)
- [x] GET /api/jobs - list jobs (with pagination, filters)
- [x] GET /api/jobs/:id - job details
- [x] GET /api/jobs/:id/logs - job logs (full)
- [x] GET /api/workers - list connected workers
- [x] GET /api/repos - list configured repos
- [x] POST /api/repos - add repo (generates webhook secret)
- [x] DELETE /api/repos/:id - remove repo
- [x] POST /api/tokens - create worker token
- [x] DELETE /api/tokens/:id - revoke token
- [x] Unit tests

### WebSocket for UI (internal/server/logstream.go)
- [x] /ws/logs/:job_id - real-time log streaming
- [x] Send existing logs on connect
- [x] Broadcast new logs to subscribers
- [x] Handle job completion

---

## Phase 6: Web UI 🚧

### Static Assets
- [x] Basic layout (header, nav, main content)
- [x] Dark theme CSS *(minimal styling only)*
- [x] Responsive (mobile-friendly)

### Jobs Page
- [x] Jobs list with status icons
- [x] Job detail page with log viewer
- [x] ANSI color stripping (basic)
- [x] Real-time updates via WebSocket
- [ ] Filter by repo, status, branch

### Workers Page
- [x] List workers with status
- [ ] Show current job per worker
- [ ] Add worker instructions

### Settings Page
- [ ] Add/remove repos *(placeholder only)*
- [ ] View webhook URLs and secrets
- [ ] Token management

---

## Phase 7: CLI Completion 🚧

### cinch run ✅
- [x] Load local config
- [x] Execute command (bare metal or container)
- [x] Stream logs to terminal
- [x] Exit with command's exit code
- [x] Support --bare-metal flag
- [x] Service container support

### cinch server ✅
- [x] Parse flags (--addr, --data-dir, --base-url)
- [x] Initialize storage (SQLite)
- [x] Create Hub, Dispatcher, WSHandler, APIHandler, WebhookHandler, LogStreamHandler
- [x] Mount routes:
  - [x] /api/* → APIHandler
  - [x] /webhooks → WebhookHandler
  - [x] /ws/worker → WSHandler (workers)
  - [x] /ws/logs/* → LogStreamHandler (UI)
  - [x] /* → embedded web assets (with SPA routing)
- [x] Start HTTP server
- [x] Graceful shutdown

### cinch worker ✅
- [x] Parse flags (--server, --token, --labels, --concurrency)
- [x] Create Worker with config
- [x] Call Worker.Start()
- [x] Wait for interrupt, call Worker.Stop()

### cinch status
- [ ] Detect repo from .git
- [ ] Query server API
- [ ] Display recent builds

### cinch logs
- [ ] Stream logs from server
- [ ] Follow mode (-f)

### cinch config validate ✅
- [x] Load and validate config
- [x] Report errors
- [x] Show parsed config summary

### cinch token
- [ ] create: call API, print token
- [ ] list: call API, print table
- [ ] revoke: call API

---

## Phase 8: Polish & Release

### Documentation
- [ ] README.md (quick start, self-hosted setup)
- [ ] Worker setup guide
- [ ] Config reference

### Testing
- [x] **E2E test (in-process):** Start server + worker in same test process, simulate webhook, verify job completes
  - No network mocking - real HTTP, real WebSocket, real SQLite (in-memory)
  - Assert: webhook accepted → job queued → worker picks up → logs streamed → status posted
- [ ] Integration test: webhook → job → status (against running server)
- [ ] Test with real GitHub repo
- [ ] Test with Forgejo instance

### Release
- [ ] Build binaries (linux/amd64, linux/arm64, darwin/arm64, darwin/amd64)
- [ ] Docker image (optional)

---

## Deferred to v0.2+

- [ ] Worker trust model (--allow-repo, --allow-org, login-based auto-allow)
- [ ] Built-in TLS (--tls-cert, --tls-key flags for standalone deployments)
- [ ] GitLab integration
- [ ] Bitbucket integration
- [ ] Postgres support (multi-instance)
- [ ] Scheduled builds
- [ ] Manual triggers
- [ ] Build badges
- [ ] User authentication in web UI
- [ ] Notifications (Slack, email)

---

## Notes

### CD (Continuous Deployment)
We don't do CD. Your Makefile does.

Cinch exposes `CINCH_BRANCH`, `CINCH_COMMIT`, etc. Your Makefile decides what to do:
```makefile
ci:
	make test
	@[ "$$CINCH_BRANCH" = "release" ] && make deploy || true
```

No branch filtering in YAML. No trigger config. No pipeline stages. Your Makefile is smart, our config is dumb. If you want conditional deployment logic, write a shell conditional - you already know how.

### TLS
Worker connects via `wss://` URLs automatically (gorilla/websocket handles TLS). Server assumes TLS termination at reverse proxy (Caddy, nginx, or platform like Fly.io). This is standard practice and works out of the box with any deployment platform. For v0.2, consider adding `--tls-cert`/`--tls-key` flags for standalone deployments.

---

## Critical Path to MVP

1. ~~**`cinch server`** - Wire up HTTP server with existing handlers~~ ✅
2. ~~**`cinch worker`** - Instantiate Worker and call Start()~~ ✅
3. ~~**Cinch env vars** - Expose `CINCH_BRANCH`, `CINCH_COMMIT`, etc. to commands~~ ✅
4. ~~**E2E test** - Verify server + worker actually work together~~ ✅
5. ~~**Fix frontend API mismatch** - Response format issue~~ ✅
6. ~~**Basic log viewing in UI** - Connect to /ws/logs/:id~~ ✅
7. **GitHub OAuth** - Protect API/UI before public deployment ✅
   - [x] `internal/server/auth.go` - GitHub OAuth + JWT sessions (steal from tunn)
   - [x] Routes: `/auth/login`, `/auth/github`, `/auth/callback`, `/auth/logout`
   - [x] JWT cookie: `cinch_auth`, 7-day expiry, HttpOnly, Secure, SameSite=Lax
   - [x] `requireAuth` middleware for protected routes
   - [x] Protected: `POST/DELETE /api/*` (mutations)
   - [x] Public: `GET /api/jobs`, `GET /api/jobs/:id/logs` (read-only, for now)
   - [x] Public: `/webhooks` (already has signature verification)
   - [x] Public: `/ws/worker` (already has token auth)
   - [x] Config: `CINCH_GITHUB_CLIENT_ID`, `CINCH_GITHUB_CLIENT_SECRET`, `CINCH_JWT_SECRET`
   - [x] Login page in web UI (or simple server-rendered page)
   - [x] Show logged-in user in UI header
8. **Deploy to cinch.sh** - Fly.io with TLS termination ✅
   - [x] fly.toml config
   - [x] Dockerfile for server
   - [x] Set secrets: `fly secrets set CINCH_JWT_SECRET=... CINCH_GITHUB_CLIENT_ID=... CINCH_GITHUB_CLIENT_SECRET=...`
   - [x] Create GitHub OAuth app (callback: `https://cinch.sh/auth/callback`)
   - [x] Deploy to Fly.io
   - [x] DNS: cinch.sh CNAME cinch.fly.dev (Cloudflare, proxied)
9. **CLI Auth & Self-Registration** - See design/11-cli-auth.md ✅
   - [x] Device auth flow (`POST /auth/device`, polling, browser verify)
   - [x] `cinch login` command
   - [x] Config file (`~/.cinch/config`)
   - [x] Worker self-registration with Bearer token
   - [x] `cinch repo add` command (deprecated, use GitHub App)
10. **GitHub App Integration** - See design/12-github-app.md 🚧
   - [ ] Create GitHub App (manual setup in GitHub)
   - [ ] Database: `installations` table
   - [ ] Handle `installation` webhook (install/uninstall)
   - [ ] Handle `installation_repositories` webhook (repo add/remove)
   - [ ] Installation token generation (JWT → installation token)
   - [ ] Status posting with installation tokens
   - [ ] Clone tokens for workers
   - [ ] Web UI: "Add to GitHub" button
   - [ ] Web UI: Show installations and repos
11. **Dogfood** - Cinch runs its own CI
   - [x] Add .cinch.yaml to this repo
   - [x] Run worker locally (Mac)
   - [x] Verify push → build works
   - [ ] Verify status check posts to GitHub (needs GitHub App)

**No more features until dogfooding works.** Everything else can ship as-is or be cut.
