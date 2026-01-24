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

### What's NOT Working Yet

- [ ] Container execution (bare-metal only currently)
- [ ] Devcontainer auto-detection
- [ ] Service containers (postgres, redis)
- [ ] Fan-out to multiple workers
- [ ] Worker labels/routing
- [ ] GitLab support
- [ ] Bitbucket support

---

## MVP 1.1: GitHub Releases & Install Script - COMPLETE

**Goal:** Users can install Cinch with a single curl command.

### Tasks

- [x] GitHub Actions workflow for releases
  - [x] Build binaries: Linux/macOS (amd64/arm64)
  - [x] Create GitHub Release with changelog
  - [x] Upload binaries as release assets
- [x] GitHub Actions CI workflow (tests + lint on push/PR)
- [x] Install script (`install.sh`)
  - [x] Detect OS/arch
  - [x] Download from GitHub Releases
  - [x] Install to ~/.local/bin or /usr/local/bin
- [x] README quick start guide

### Ready to Release

Tag `v0.1.0` to trigger first release:
```bash
git tag v0.1.0 && git push --tags
```

Then users can install with:
```bash
curl -fsSL https://raw.githubusercontent.com/ehrlich-b/cinch/main/install.sh | sh
cinch version
```

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
