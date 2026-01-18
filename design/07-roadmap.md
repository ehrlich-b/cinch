# Two-Week Implementation Roadmap

## Overview

This roadmap assumes AI agents working full-time (~8 hours/day) for two weeks. Goal: working v0.1 with GitHub + Forgejo support, self-hosted only.

## Week 1: Core Infrastructure

### Day 1-2: Project Scaffolding & CLI

**Goal:** Binary that compiles, subcommand routing works.

**Tasks:**
- [ ] Initialize Go module (`go mod init github.com/ehrlich-b/cinch`)
- [ ] Set up directory structure (see 00-overview.md)
- [ ] Implement CLI framework with cobra
  - [ ] `cinch version`
  - [ ] `cinch server` (placeholder)
  - [ ] `cinch worker` (placeholder)
  - [ ] `cinch run` (placeholder)
  - [ ] `cinch config` (placeholder)
- [ ] Basic logging setup (structured, leveled)
- [ ] Configuration loading (flags, env vars, config file)
- [ ] Build script / Makefile

**Deliverable:** `cinch version` works, `cinch server` starts and exits cleanly.

### Day 3-4: Database Layer

**Goal:** SQLite working, all tables created, basic CRUD.

**Tasks:**
- [ ] Implement database interface (`internal/db/db.go`)
- [ ] SQLite implementation
  - [ ] Connection management
  - [ ] Migration system
  - [ ] Initial schema (all tables from 04-database.md)
- [ ] Repository methods
  - [ ] CreateJob, GetJob, ListJobs, UpdateJobStatus
  - [ ] CreateWorker, GetWorker, ListWorkers, UpdateWorkerStatus
  - [ ] CreateRepo, GetRepo, GetRepoByName
  - [ ] AppendLog, GetLogs
  - [ ] CreateToken, ValidateToken, RevokeToken
- [ ] Unit tests for all DB operations

**Deliverable:** Can create/read/update/delete all entities in SQLite.

### Day 5-6: WebSocket Protocol & Worker

**Goal:** Server accepts WebSocket connections, workers can connect and receive jobs.

**Tasks:**
- [ ] Define protocol types (`internal/protocol/protocol.go`)
- [ ] Server WebSocket hub
  - [ ] Connection handling
  - [ ] Authentication (token validation)
  - [ ] Worker registration
  - [ ] Heartbeat (ping/pong)
- [ ] Worker client
  - [ ] Connect to server
  - [ ] Handle reconnection with backoff
  - [ ] Receive job assignments
  - [ ] Send acknowledgments
- [ ] Basic job dispatch logic (pick available worker)
- [ ] Integration test: worker connects, receives dummy job

**Deliverable:** `cinch worker` connects to `cinch server`, heartbeat works.

### Day 7: Job Execution (Bare Metal)

**Goal:** Worker can clone repos and execute commands on bare metal.

**Tasks:**
- [ ] Git clone logic
  - [ ] Clone with token in URL
  - [ ] Checkout specific commit
  - [ ] Handle errors gracefully
- [ ] Command executor
  - [ ] Run command with timeout
  - [ ] Capture stdout/stderr
  - [ ] Stream logs to server via WebSocket
  - [ ] Report exit code
- [ ] cinch.yaml parser
  - [ ] YAML parsing
  - [ ] JSON fallback
  - [ ] Validation
  - [ ] Fetch from repo (via raw file URL or clone)
- [ ] End-to-end test: push triggers clone + command execution

**Deliverable:** Worker clones repo, runs `make ci`, streams logs back.

### Day 7.5-8: Containerization (Core Feature)

**Goal:** Worker runs builds in containers by default with warm caches.

**Tasks:**
- [ ] Container runtime interface (`internal/worker/container/container.go`)
  - [ ] Define Runtime interface: Run, Pull, Build, Stop
  - [ ] Container config: image, mounts, resources
- [ ] Docker implementation
  - [ ] Connect to Docker daemon
  - [ ] Pull/build images
  - [ ] Create and start containers
  - [ ] Attach to stdout/stderr for log streaming
  - [ ] Wait for exit and cleanup
- [ ] Cache volume management
  - [ ] Create persistent cache directories (~/.cinch/cache/)
  - [ ] Standard cache mounts (npm, cargo, pip, go)
  - [ ] Custom cache paths from config
- [ ] Devcontainer detection
  - [ ] Parse .devcontainer/devcontainer.json
  - [ ] Build devcontainer image with caching
  - [ ] Fall back to Dockerfile, then default image
- [ ] Artifact extraction
  - [ ] Output directory mount
  - [ ] Post-build copy-out
- [ ] Integration test: build runs in container with warm cache

**Deliverable:** Push triggers containerized build using project's devcontainer.

## Week 2: Forge Integration & Web UI

### Day 9-10: GitHub Integration

**Goal:** GitHub webhooks work, status checks posted.

**Tasks:**
- [ ] Forge interface (`internal/forge/forge.go`)
- [ ] GitHub implementation
  - [ ] Webhook parsing (push, pull_request)
  - [ ] Signature verification (HMAC-SHA256)
  - [ ] Status API posting (pending, success, failure)
  - [ ] Clone token generation (if using GitHub App)
- [ ] Webhook HTTP handler
  - [ ] Route by forge type
  - [ ] Create job from webhook
  - [ ] Error handling
- [ ] Full flow test: GitHub push → webhook → job → status posted

**Deliverable:** Push to GitHub repo, green checkmark appears.

### Day 11: Forgejo/Gitea Integration

**Goal:** Forgejo works (same API as Gitea).

**Tasks:**
- [ ] Forgejo implementation (copy GitHub, adjust for Forgejo API)
  - [ ] Webhook parsing
  - [ ] Signature verification
  - [ ] Status posting
- [ ] Test with local Forgejo instance (docker-compose)
- [ ] Verify works with Gitea too

**Deliverable:** Push to Forgejo repo, checkmark appears.

### Day 12-13: Web UI (Basic)

**Goal:** Can view jobs and logs in browser.

**Tasks:**
- [ ] Static file embedding setup
- [ ] HTML/CSS structure
  - [ ] Dashboard page (job list)
  - [ ] Job detail page (with logs)
  - [ ] Workers page
- [ ] JavaScript
  - [ ] Fetch and display jobs
  - [ ] WebSocket for real-time log streaming
  - [ ] ANSI color rendering
  - [ ] Auto-refresh job list
- [ ] API endpoints
  - [ ] GET /api/jobs
  - [ ] GET /api/jobs/:id
  - [ ] GET /api/workers
  - [ ] WebSocket /ws/logs/:id
- [ ] Polish: status colors, timestamps, responsive layout

**Deliverable:** Can watch build logs in real-time in browser.

### Day 13.5: CLI Completeness & Polish

**Goal:** All CLI commands working.

**Tasks:**
- [ ] `cinch run` - local execution
  - [ ] Parse local cinch.toml
  - [ ] Execute command
  - [ ] Set CI env vars
- [ ] `cinch status` - show build status
  - [ ] Detect current repo/branch
  - [ ] Query server API
  - [ ] Display formatted output
- [ ] `cinch logs` - stream/view logs
  - [ ] Fetch from server
  - [ ] Follow mode with WebSocket
- [ ] `cinch config` - validate config
  - [ ] Parse and validate
  - [ ] Pretty-print parsed result
- [ ] `cinch token create/list/revoke` - token management

**Deliverable:** All documented CLI commands functional.

### Day 14: Documentation & Release

**Goal:** Ready for first users.

**Tasks:**
- [ ] Installation script (`install.sh`)
  - [ ] Detect OS/arch
  - [ ] Download appropriate binary
  - [ ] Install to PATH
- [ ] README updates
  - [ ] Quick start guide
  - [ ] Configuration reference
  - [ ] FAQ
- [ ] Example configs
  - [ ] Basic cinch.toml examples
  - [ ] Docker-compose for self-hosted setup
- [ ] GitHub releases
  - [ ] Build binaries for Linux/macOS/Windows (amd64/arm64)
  - [ ] Create release with changelog
- [ ] Smoke test full flow
  - [ ] Fresh install
  - [ ] Configure with GitHub repo
  - [ ] Push and verify green checkmark

**Deliverable:** v0.1.0 released, works end-to-end.

## Success Criteria for v0.1

The mantra: **your Makefile is the pipeline.** Cinch runs one command. Services are the exception because Makefile postgres is pain.

- [ ] Single binary contains: server, worker, CLI, web UI
- [ ] GitHub webhooks trigger builds
- [ ] Forgejo webhooks trigger builds
- [ ] Status checks posted back to forges
- [ ] Logs stream in real-time to web UI
- [ ] Devcontainer auto-detection (use project's container with zero config)
- [ ] Fan-out to multiple workers (`workers: [linux, arm64]`)
- [ ] Sticky worker routing (prefer last worker for warm cache)
- [ ] **Service containers (postgres, redis) with health checks and auto-cleanup**
- [ ] Self-hosted setup works with SQLite
- [ ] Installation script works on Linux and macOS

## What's NOT in v0.1

**Cut to keep it minimal:**
- Artifact extraction - your Makefile uploads to S3 if you need artifacts
- Scheduled/manual builds - push code or wait for v0.2
- Trigger filtering (branches, paths) - all pushes trigger builds
- Explicit container config - auto-detect only, override in v0.2
- Resource limits (memory, CPU)

**Deferred infrastructure:**
- Postgres support (v0.2 - SQLite is fine for self-hosted)
- GitLab support (v0.2 - GitHub + Forgejo first)
- Bitbucket support (v0.3)
- Hosted service infrastructure (v0.3)

**Nice to have:**
- Build badges (v0.2)
- User authentication in web UI (self-hosted is single-user for now)
- Notifications

## Risk Mitigation

### Risk: GitHub App complexity
**Mitigation:** Support Personal Access Token first. GitHub App is nice-to-have for v0.1.

### Risk: WebSocket reliability
**Mitigation:** Implement reconnection early. Test with network disruption scenarios.

### Risk: Log streaming performance
**Mitigation:** Chunk logs appropriately. Add backpressure if worker produces faster than server can relay.

### Risk: Web UI takes too long
**Mitigation:** Keep it minimal. Job list + log viewer is the MVP. No fancy features.

### Risk: Containerization complexity
**Mitigation:** Focus on Docker only for v0.1. Devcontainer detection can be basic (just use the image, don't parse all features). Fall back to a simple default image if detection fails.

## Daily Standup Format

Each day, AI agent should:
1. Report what was completed
2. List any blockers
3. Confirm next day's tasks
4. Update checkboxes in this roadmap

## Definition of Done

A task is done when:
- Code compiles without warnings
- Unit tests pass
- Integration tests pass (where applicable)
- Feature works end-to-end
- No obvious security issues
- Code is reasonably documented (godoc comments)
