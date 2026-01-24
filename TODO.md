# Cinch TODO

**Last Updated:** 2026-01-24

---

## Current: MVP 1.2 - Container Execution

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

### Known Issues
- [ ] **Rejected jobs not re-queued** - When a worker rejects a job (at max concurrency), the job is not put back in the queue. See `ws.go:403` TODO.

---

## Done

### MVP 1.1 - Releases via Cinch (2026-01-24)

Cinch builds itself. Tag push → automatic release. No GitHub Actions.

- [x] Tag detection: differentiate `refs/tags/v1.0.0` from `refs/heads/main`
- [x] Environment variables for tags:
  - `CINCH_REF` - Full ref (refs/heads/main or refs/tags/v1.0.0)
  - `CINCH_BRANCH` - Branch name (empty for tags)
  - `CINCH_TAG` - Tag name (empty for branches)
- [x] Forge token passed to jobs:
  - `GITHUB_TOKEN` - For GitHub repos
  - `GITLAB_TOKEN` / `CI_JOB_TOKEN` - For GitLab repos
  - `GITEA_TOKEN` - For Forgejo/Gitea repos
  - `CINCH_FORGE_TOKEN` - Always set (generic)
- [x] Install script (`install.sh`) - detects OS/arch, downloads from GitHub Releases
- [x] `make release` target - cross-compiles all platforms, uploads to GitHub Releases
- [x] Makefile uses `CINCH_TAG` for version (not `git describe`)
- [x] First release: `v0.1.7`

Release process:
```bash
git tag v0.1.0 && git push --tags
# Cinch worker picks up the tag push
# CINCH_TAG=v0.1.0 is set
# make ci → make release (because CINCH_TAG is set)
# Binaries cross-compiled and uploaded to GitHub Releases
# Users install with: curl -sSL https://cinch.sh/install.sh | sh
```

**Bugs Fixed During MVP 1.1:**
- `clone.go` - Was using empty `repo.Branch` for tag clones; now uses `repo.Tag`
- `github_app.go` - Was ignoring tag pushes entirely (only processed `refs/heads/`)
- `github_app.go` - Wasn't passing `Ref`/`Tag` fields to QueuedJob
- `Makefile` - `git describe` returns wrong tag when multiple tags on same commit
- `Makefile` - Missing `npm install` before web build

---

### MVP 1.0 - Core CI (2026-01-23)

#### Core
- [x] Single binary: server, worker, CLI, web UI
- [x] SQLite storage with migrations
- [x] WebSocket protocol with reconnection
- [x] Job queue with dispatch on worker availability

#### Forges
- [x] GitHub webhooks + Status API
- [x] GitHub App with Checks API (shows logs in GitHub UI)
- [x] Forgejo/Gitea webhooks + Status API
- [x] Forge factory pattern for easy extension

#### Auth
- [x] GitHub OAuth for web UI
- [x] Device flow for CLI (`cinch login`)
- [x] JWT sessions with secure cookies
- [x] Worker token authentication

#### CLI
- [x] `cinch server` - full HTTP server with graceful shutdown
- [x] `cinch worker` - job execution with log streaming
- [x] `cinch run` - local bare-metal execution
- [x] `cinch login` - device flow authentication
- [x] `cinch config validate` - validate .cinch.yaml

#### Web UI
- [x] Job list with status
- [x] Job detail with real-time log streaming
- [x] Workers page
- [x] Dark theme, responsive

#### Execution
- [x] Git clone with token auth (branches and tags)
- [x] Bare-metal command execution
- [x] Log streaming to server AND worker terminal
- [x] Cinch env vars: `CINCH_JOB_ID`, `CINCH_REF`, `CINCH_BRANCH`, `CINCH_TAG`, `CINCH_COMMIT`, `CINCH_REPO`, `CINCH_FORGE`
- [x] Forge tokens: `GITHUB_TOKEN`, `GITLAB_TOKEN`, `GITEA_TOKEN`, `CINCH_FORGE_TOKEN`
- [x] Worker reads `.cinch.yaml` from cloned repo

#### Deployment
- [x] Deployed to Fly.io (cinch.fly.dev / cinch.sh)
- [x] Dogfooding: Cinch runs its own CI and releases

---

## Architecture Notes

### Environment Variables

Every Cinch job gets:

```bash
# Git context
CINCH_REF=refs/heads/main       # Full ref (or refs/tags/v1.0.0)
CINCH_BRANCH=main               # Branch name (empty for tags)
CINCH_TAG=                      # Tag name (empty for branches)
CINCH_COMMIT=abc1234567890      # Commit SHA

# Job context
CINCH_JOB_ID=j_abc123
CINCH_REPO=https://github.com/owner/repo.git
CINCH_FORGE=github              # or gitlab, forgejo, gitea

# Forge token (for API access - releases, comments, etc.)
GITHUB_TOKEN=ghs_xxx            # GitHub
GITLAB_TOKEN=glpat-xxx          # GitLab
GITEA_TOKEN=xxx                 # Forgejo/Gitea
CINCH_FORGE_TOKEN=xxx           # Always set (same as above)
```

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

### Tag vs Branch Flow

```
Push Event (refs/tags/v1.0.0)
    ↓
ParsePush extracts:
  - Ref: refs/tags/v1.0.0
  - Tag: v1.0.0
  - Branch: "" (empty)
    ↓
QueuedJob includes Ref, Branch, Tag
    ↓
Worker clones with: git clone --branch v1.0.0
    ↓
Environment: CINCH_TAG=v1.0.0, CINCH_BRANCH=""
    ↓
Makefile: if [ -n "$CINCH_TAG" ]; then make release; fi
```
