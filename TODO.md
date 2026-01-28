# Cinch TODO

**Last Updated:** 2026-01-27

---

## Immediate: Tech Debt

- [x] **Break up web/src/App.tsx** - Split into components/pages (2200 lines → 184 + modules)

---

## Current: MVP 1.5 - Worker Ergonomics

**Goal:** Worker setup so simple that "I don't want to run my own runners" stops being a complaint. And running a worker should feel *cool as shit* - way better than the sad GitHub Actions runner experience (static "Listening for Jobs" message, then silence).

### Worker TUI - Make It Feel Alive

Running a worker should be a visual experience. GitHub's runner is a sad terminal that prints one line and sits there. We can do better.

**Vision:**
```bash
cinch daemon start           # Background daemon
cinch worker                 # Attach with TUI (or just start worker if no daemon)
```

**The TUI (bubbletea):**
```
┌─────────────────────────────────────────────────────────────┐
│  CINCH WORKER                              ehrlich@macbook  │
├─────────────────────────────────────────────────────────────┤
│  ● CONNECTED to cinch.sh                    3 repos         │
│                                                             │
│  ┌─ CURRENT BUILD ────────────────────────────────────────┐ │
│  │  owner/repo @ main (abc1234)                           │ │
│  │  $ make build                                          │ │
│  │  ████████████░░░░░░░░░░░░░░░░░  38%  2m 14s            │ │
│  │                                                        │ │
│  │  > Compiling src/main.go...                            │ │
│  │  > go build -o bin/app ./cmd/app                       │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                             │
│  ┌─ RECENT ───────────────────────────────────────────────┐ │
│  │  ✓ owner/other  make test         12s    2m ago        │ │
│  │  ✓ owner/repo   make build        1m 3s  5m ago        │ │
│  │  ✗ team/proj    make check        45s    12m ago       │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                             │
│  [q] quit  [l] logs  [r] retry                              │
└─────────────────────────────────────────────────────────────┘
```

**Key elements:**
- Live progress bar (if we can estimate from log output)
- Scrolling last few lines of current build
- Recent builds with pass/fail, duration, time ago
- Keyboard shortcuts for common actions
- Status indicator (connected, jobs waiting, etc.)
- **Retry failed builds** - `r` to retry from TUI

**Feature parity: TUI ↔ Web UI**

Anything you can do in the TUI, you should be able to do in the web UI and vice versa:
- Retry/re-run failed builds
- View live logs
- See worker status (connected, current job, recent jobs)
- **Stop a worker remotely** (from web UI → signal worker to gracefully shut down)

**Daemon parallelism:**

Parallelism = run more worker processes. No code-level concurrency - just spawn more.

```bash
cinch daemon start              # Starts daemon with 1 worker
cinch daemon start -n 4         # Starts daemon with 4 worker processes
cinch daemon scale 2            # Adjust running workers (add/remove)
cinch daemon status             # Show all worker processes
```

The daemon harness manages N worker child processes. Each worker is independent, connects to cinch.sh, claims jobs. Want more parallelism? `cinch daemon scale 8`.

**Remote daemon control (stretch):**

From the web UI, manage your daemon:
- See all your workers (which machines, how many processes each)
- Shut down a specific worker
- Scale up/down workers on a machine
- "Manage workers on your machine" sounds enterprise but it's useful

**Non-TTY mode (pipes, logs, CI):**

No banners, no decoration, just raw output:
```
[worker] connected to cinch.sh
[worker] claiming job j_abc123
[job] cloning owner/repo@main
[job] running: make build
go build -o bin/app ./cmd/app
...actual build output...
[job] exit 0 (12.3s)
[worker] job complete, waiting for next
```

The current `terminal.go` banner code (━━━ lines, ANSI colors, "GITHUB STARTED") goes away for non-TTY. All that pretty stuff is TUI-only.

**Contrast with GitHub Actions runner:**
```
√ Connected to GitHub
2024-01-24 05:45:56Z: Listening for Jobs
```
That's it. That's the whole UX. Cinch should feel like a living dashboard.

**Implementation:**
- [ ] bubbletea TUI for `cinch worker` attach mode
- [ ] Real-time log streaming in TUI
- [ ] Recent jobs list (last 5-10)
- [ ] Progress estimation from log patterns
- [ ] Keyboard navigation (view full logs, quit, etc.)
- [x] Graceful fallback to simple mode for non-TTY (just raw log stream, no banners/decoration)
- [ ] Retry from TUI (`r` key on failed build)
- [ ] Retry from web UI (button on failed job)
- [ ] Daemon harness with multi-worker support (`-n` flag)
- [ ] `cinch daemon scale N` to adjust worker count
- [ ] Remote worker shutdown (web UI → graceful stop signal)
- [ ] Worker list in web UI (see all your connected workers)

### `cinch daemon` - Dead Simple Background Worker

```bash
cinch login
cinch daemon install   # creates user-level service, no sudo
cinch daemon start     # starts it
# Done. Your Mac/Linux box is now a CI runner.
```

| Platform | User-level daemon | Location | No sudo? |
|----------|------------------|----------|----------|
| macOS | launchd agent | `~/Library/LaunchAgents/` | ✅ |
| Linux | systemd user service | `~/.config/systemd/user/` | ✅ |

**Commands:**
```bash
cinch daemon install    # Write service file, enable on boot
cinch daemon start      # Start the worker daemon
cinch daemon stop       # Stop the worker daemon
cinch daemon status     # Is it running? Last job?
cinch daemon uninstall  # Remove service file
cinch daemon logs       # Tail the daemon logs
```

**Open questions:**
- [ ] What about Windows? (Probably just "run in terminal" for now)
- [ ] Docker-in-Docker on Mac? (Docker Desktop socket passthrough)
- [ ] Should `cinch daemon install` also run `cinch login` if not logged in?

---

## Then: MVP 1.6 - PR/MR Support

Currently push-only. PRs are table stakes for real adoption.

- [ ] GitHub Pull Request events
- [ ] GitLab Merge Request events
- [ ] Forgejo/Gitea Pull Request events
- [ ] Status checks on PR head commit

---

## Then: MVP 1.7 - Stripe Integration

**Pricing:** Public repos free, private repos $5/seat/month, self-hosted free.

- [ ] Stripe checkout integration
- [ ] Seat counting logic
- [ ] Public vs private repo detection
- [ ] Payment prompt during onboarding (private repo selected → pay first)
- [ ] Billing page in web UI

---

## Then: MVP 1.8 - Distinctive Badge Design

**Problem:** Shields.io badges are invisible - everyone uses them. Cinch badges should be recognizable.

- [ ] Design distinctive badge style (shape, colors, typography)
- [ ] Implement custom SVG rendering
- [ ] Remove shields.io redirect, serve SVG directly

---

## Future: Parallel Commands

```yaml
# Array = parallel jobs (no DAG)
build:
  - make build
  - make test
  - make docs
```

Array items fan out as independent parallel jobs. No DAG, no workflow DSL.

---

## Backlog

### Web UI Polish
- [ ] Light/dark theme toggle
- [ ] Loading skeletons
- [ ] Real Settings page (tokens, repos)
- [ ] Badge repo selector (generate badge markdown)

### CLI Polish
- [ ] `cinch status` - check job status from CLI
- [ ] `cinch logs -f` - stream logs from CLI

### Scale
- [ ] Worker labels and routing
- [ ] Fan-out to multiple workers
- [ ] Postgres storage backend

### Known Issues
- [ ] Worker should check for docker/podman on startup (fail fast)
- [ ] Cache volumes need docs + configurability

### Forge Expansion (Low Priority)
- [ ] GitHub web onboarding (not just GitHub App)
- [ ] Azure DevOps, AWS CodeCommit, Gitee (demand-driven)

---

## Done

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
