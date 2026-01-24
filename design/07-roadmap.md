# Roadmap & Status

## Current Status: v0.1 MVP Complete

**Last Updated:** 2026-01-23

### What's Working

- [x] Single binary: server, worker, CLI, web UI
- [x] GitHub webhooks trigger builds
- [x] GitHub App integration with Checks API (shows logs in GitHub UI)
- [x] Forgejo/Gitea webhooks trigger builds
- [x] Status checks posted back to forges
- [x] Logs stream in real-time to web UI
- [x] Logs stream to worker terminal (for debugging)
- [x] WebSocket protocol with reconnection
- [x] Job queue with dispatch when workers come online
- [x] OAuth authentication (GitHub) for web UI
- [x] Device flow authentication for CLI (`cinch login`)
- [x] `cinch run` for local bare-metal execution
- [x] `cinch server` with full HTTP routing
- [x] `cinch worker` with job execution
- [x] SQLite storage backend
- [x] Deployed to Fly.io (cinch.fly.dev)
- [x] Dogfooding: Cinch runs its own CI

### What's NOT Working Yet

- [ ] Container execution (bare-metal only currently)
- [ ] Devcontainer auto-detection
- [ ] Service containers (postgres, redis)
- [ ] Fan-out to multiple workers
- [ ] Worker labels/routing
- [ ] GitLab support
- [ ] Bitbucket support

---

## MVP 1.1: Releases (via Cinch)

**Goal:** Cinch builds and releases itself. No GitHub Actions.

The whole point of Cinch is that CI runs on YOUR hardware. We dogfood this for releases.

### Tasks

- [x] Install script (`install.sh`) - detects OS/arch, downloads from GitHub Releases
- [ ] `make release` target - cross-compiles all platforms, uploads to GitHub Releases
- [ ] Tag-triggered builds - worker runs release on tag push
- [ ] First release: `v0.1.0`

### Release Process

```bash
# Developer tags a release
git tag v0.1.0 && git push --tags

# Cinch worker (running on dev's machine or build box) picks up the push
# Runs: make release
# Which cross-compiles and uploads to GitHub Releases

# Users install
curl -fsSL https://raw.githubusercontent.com/ehrlich-b/cinch/main/install.sh | sh
```

### Why Not GitHub Actions?

Because that defeats the entire value proposition:
- **"Your hardware"** - Workers run on YOUR machines, not GitHub's
- **"Your Makefile is the pipeline"** - We just invoke `make release`

If we used GitHub Actions, we'd be saying "use Cinch for CI... except we don't."

---

## MVP 1.2: Container Execution

**Goal:** Jobs run in containers by default with cache mounts.

### Tasks

- [ ] Docker runtime implementation
- [ ] Cache volume management (~/.cinch/cache/)
- [ ] Devcontainer detection (use project's container)
- [ ] Fallback to Dockerfile, then default image
- [ ] `container: none` escape hatch in config

---

## MVP 1.3: Service Containers

**Goal:** Spin up postgres/redis alongside builds.

### Tasks

- [ ] Parse `services:` from .cinch.yaml
- [ ] Docker network per job
- [ ] Health checks with timeout
- [ ] Environment variable injection
- [ ] Auto-cleanup on job completion

---

## Future Releases

### v0.2: Multi-Forge & Scale

- [ ] GitLab integration
- [ ] Postgres storage backend
- [ ] Fan-out to multiple workers
- [ ] Worker labels and routing
- [ ] Build badges

### v0.3: Hosted Service

- [ ] Bitbucket integration
- [ ] Multi-tenant isolation
- [ ] Usage metering
- [ ] Team management

---

## Architecture Notes

### Forge Abstraction

The forge system is designed for easy extension:

```go
// Add a new forge by implementing this interface
type Forge interface {
    Name() string
    Identify(r *http.Request) bool
    ParsePush(r *http.Request, secret string) (*PushEvent, error)
    PostStatus(ctx context.Context, repo *Repo, commit string, status *Status) error
    CloneToken(ctx context.Context, repo *Repo) (string, time.Time, error)
}

// Create instances via factory
f := forge.New(forge.ForgeConfig{
    Type:    forge.TypeGitHub,  // or TypeGitLab, TypeForgejo, TypeGitea
    Token:   "...",
    BaseURL: "...",  // for self-hosted instances
})
```

To add a new forge:
1. Create `internal/forge/newforge.go` implementing the interface
2. Add type constant in `internal/forge/forge.go`
3. Add case to `forge.New()` factory
4. Register in `cmd/cinch/main.go` webhook handler

### Job Flow

```
Webhook → Parse → Create Job → Queue → Dispatch → Worker → Execute → Report
                                  ↓
                            Wait for worker
                            (jobs queue until
                            eligible worker
                            comes online)
```

### Config Loading

Worker reads `.cinch.yaml` from the cloned repo, not from server config. This allows per-repo configuration without server-side setup.

```yaml
# .cinch.yaml
command: make check
timeout: 30m
```
