# Cinch TODO

**Last Updated:** 2026-01-23

---

## Current: MVP 1.1 - GitHub Releases

- [ ] GitHub Actions release workflow
  - [ ] Trigger on tag push (v*)
  - [ ] Build: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64
  - [ ] Create GitHub Release with binaries
- [ ] Install script (`install.sh`)
  - [ ] Detect OS/arch
  - [ ] Download from GitHub Releases
  - [ ] Install to ~/.local/bin or /usr/local/bin
- [ ] README quick start guide

---

## Next: MVP 1.2 - Container Execution

- [ ] Docker runtime in worker (`internal/worker/container/docker.go`)
- [ ] Cache volumes (~/.cinch/cache/ for npm, cargo, pip, go)
- [ ] Devcontainer detection (use project's container)
- [ ] Fallback: Dockerfile → default image (ubuntu:22.04)
- [ ] `container: none` escape hatch in .cinch.yaml

---

## Next: MVP 1.3 - Service Containers

- [ ] Parse `services:` from .cinch.yaml
- [ ] Docker network per job
- [ ] Health checks with timeout
- [ ] Environment variable injection (DATABASE_URL, etc.)
- [ ] Auto-cleanup on job completion

---

## Backlog

### Forges
- [ ] GitLab integration (v0.2)
- [ ] Bitbucket integration (v0.3)
- [ ] PR support (not just push events)

### Scale
- [ ] Worker labels and routing
- [ ] Fan-out to multiple workers
- [ ] Postgres storage backend

### Polish
- [ ] Build badges
- [ ] `cinch status` - check job status from CLI
- [ ] `cinch logs -f` - stream logs from CLI
- [ ] Filter jobs by repo/status/branch in UI
- [ ] Show current job per worker in UI

---

## Done (v0.1 MVP)

### Core
- [x] Single binary: server, worker, CLI, web UI
- [x] SQLite storage with migrations
- [x] WebSocket protocol with reconnection
- [x] Job queue with dispatch on worker availability

### Forges
- [x] GitHub webhooks + Status API
- [x] GitHub App with Checks API (shows logs in GitHub UI)
- [x] Forgejo/Gitea webhooks + Status API
- [x] Forge factory pattern for easy extension

### Auth
- [x] GitHub OAuth for web UI
- [x] Device flow for CLI (`cinch login`)
- [x] JWT sessions with secure cookies
- [x] Worker token authentication

### CLI
- [x] `cinch server` - full HTTP server with graceful shutdown
- [x] `cinch worker` - job execution with log streaming
- [x] `cinch run` - local bare-metal execution
- [x] `cinch login` - device flow authentication
- [x] `cinch config validate` - validate .cinch.yaml

### Web UI
- [x] Job list with status
- [x] Job detail with real-time log streaming
- [x] Workers page
- [x] Dark theme, responsive

### Execution
- [x] Git clone with token auth
- [x] Bare-metal command execution
- [x] Log streaming to server AND worker terminal
- [x] Cinch env vars: `CINCH_JOB_ID`, `CINCH_BRANCH`, `CINCH_COMMIT`, `CINCH_REPO`
- [x] Worker reads `.cinch.yaml` from cloned repo

### Deployment
- [x] Deployed to Fly.io (cinch.fly.dev)
- [x] Dogfooding: Cinch runs its own CI

---

## Architecture Notes

### Adding a New Forge

```go
// 1. Implement the interface in internal/forge/newforge.go
type NewForge struct { ... }
func (f *NewForge) Name() string { return "newforge" }
func (f *NewForge) Identify(r *http.Request) bool { ... }
func (f *NewForge) ParsePush(r *http.Request, secret string) (*PushEvent, error) { ... }
func (f *NewForge) PostStatus(ctx context.Context, repo *Repo, commit string, status *Status) error { ... }
func (f *NewForge) CloneToken(ctx context.Context, repo *Repo) (string, time.Time, error) { ... }

// 2. Add type constant in internal/forge/forge.go
const TypeNewForge = "newforge"

// 3. Add to factory in internal/forge/forge.go
case TypeNewForge:
    return &NewForge{Token: cfg.Token, BaseURL: cfg.BaseURL}

// 4. Register in cmd/cinch/main.go
webhookHandler.RegisterForge(&forge.NewForge{})
```

### Job Flow

```
Webhook → Parse → Create Job → Queue → Dispatch → Worker → Execute → Report
                                 ↓
                           Wait for worker
                           (jobs queue until
                           eligible worker
                           comes online)
```
