# Cinch TODO

**Last Updated:** 2026-01-24

---

## Current: MVP 1.3 - Public Launch Prep

**Goal:** Make cinch presentable for public use.

### Branding

**Identity:** Friendly/utilitarian. "CI that's a CInch."

- Logo: **CI** in monospace caps (double meaning: Continuous Integration + CInch)
- Minimal footprint, obviously noticeable
- Pro: swap in own icon + own link destination

### Build Badges

**Approach:** Lock in `.svg` endpoint now, redirect to shields.io. Render custom Cinch badges later.

```
cinch.sh/badge/github.com/owner/repo.svg
    ↓
302 redirect to:
img.shields.io/endpoint?url=https://cinch.sh/api/badge/github.com/owner/repo.json
```

- [x] Endpoint format: `/badge/{forge}/{owner}/{repo}.svg` (e.g. `/badge/github.com/owner/repo.svg`)
- [x] JSON endpoint for shields.io: `/api/badge/{forge}/{owner}/{repo}.json`
  - Returns: `{"schemaVersion":1,"label":"","message":"passing","color":"brightgreen","logoSvg":"..."}`
- [x] `.svg` endpoint redirects to shields.io with our JSON URL
- [x] Cache headers on JSON endpoint
- [x] Removed badge rendering code (12 styles) - using shields.io
- [x] Badge label changed from "CI" to "build" (shows "build: passing")

### Queue Reliability ✅
- [x] Jobs re-queue on worker disconnect
- [x] Jobs re-queue on server restart (fresh tokens regenerated)
- [x] Tags stored in job record for recovery
- [x] Simplified: one worker = one job (no concurrency config)

---

## Current: Web UI Redesign

**Goal:** A polished, cohesive web experience with proper UX at every touchpoint.

See `design/11-web-ui.md` for full spec.

### Competitor Analysis

| Tool | Value Prop | Config Complexity | Our Angle |
|------|-----------|-------------------|-----------|
| GitHub Actions | "Automate everything" | Complex YAML, marketplace | We're simpler |
| GitLab CI | "DevOps platform" | Complex YAML, many features | We're forge-agnostic |
| CircleCI | "Fast, reliable CI" | YAML + orbs | We're self-hosted friendly |
| Buildkite | "Your infra, our dashboard" | YAML pipelines | We're even simpler |
| Dagger | "CI that runs anywhere" | SDK in Go/Python/TS | We use your Makefile |
| Earthly | "Makefile + Dockerfile" | Earthfile syntax | We use your ACTUAL Makefile |
| Gitea Runner | "Built-in CI for Gitea" | GH Actions YAML | We work with ANY forge |

**Key insight:** Dagger/Earthly nail "local = CI" but require new syntax. We use your EXISTING Makefile.

### Core Value Proposition

**"The exact `make build` you run locally. That's your CI."**

Not: "We simplified CI config" (implies we invented new config)
But: "Your Makefile already works. We just run it on push."

Hero should show BOTH files side by side:

```
.cinch.yaml            Makefile
───────────            ────────
build: make build      build:
release: make release      go build -o bin/app ./cmd/app

                       release:
                           cinch release dist/*
```

Note: `cinch release` works on GitHub, GitLab, Gitea - move forges, keep your Makefile.

### Critical Bugs

- [x] **Back button broken** - Added URL routing with history API
- [x] **Badge shows "CI CI"** - Fixed, now just shows "build: passing"
- [x] **Settings page empty** - Removed (non-functional)
- [x] **No onboarding** - Added empty states with setup instructions

### UX Issues

- [x] **Hero unclear** - Now shows .cinch.yaml + Makefile side by side
- [x] **Empty states useless** - Now show setup steps with correct commands
- [x] **No error handling** - Added ErrorState component with retry buttons
- [x] **No relative timestamps** - Added relativeTime() helper, shows "2m ago"

### Remaining Polish (Backlog)

- [ ] Loading skeletons
- [ ] Real Settings page (tokens, repos)
- [ ] Job filtering by repo/status/branch
- [ ] Re-run failed builds
- [ ] Badge repo selector

---

## Then: MVP 1.4 - Forge Expansion + Multi-Forge Presence

**Goal:** Cover the major forges beyond GitHub AND dogfood multi-forge by hosting Cinch itself on all supported platforms.

See `design/forge/` for detailed integration plans per forge.
See `design/12-multi-forge-presence.md` for Cinch's own multi-forge hosting strategy.

| Forge | Users | Priority | Notes |
|-------|-------|----------|-------|
| GitHub | 65M+ | ✅ Done | Full integration (App + Checks API) |
| GitLab | 30M | ✅ Done | Core implementation complete, OAuth flow pending |
| Forgejo/Gitea | ? | ✅ Done (manual) | Hybrid flow planned (OAuth webhook + manual PAT) |
| Bitbucket | 10M+ | **Not Planned** | Platform limitations make grug UX impossible |

GitLab positioning: We're not replacing GitLab as a forge—we're replacing `.gitlab-ci.yml` with something simpler. "Keep GitLab for code, use Cinch for CI."

### Cinch Multi-Forge Presence (Dogfooding)

**Strategy:** True multi-primary. Cinch source lives on GitHub, GitLab, AND Codeberg simultaneously.

- All three are "real" upstreams (accept PRs from any)
- Releases land on ALL forges simultaneously via Cinch
- One Makefile, three webhooks, three release targets
- Proves "one config, every forge" isn't just marketing

See `design/12-multi-forge-presence.md` for mechanical details.

### GitLab Integration

- [x] Core forge implementation (`internal/forge/gitlab.go`)
- [x] Webhook parsing (X-Gitlab-Token verification)
- [x] Status API (commit statuses)
- [x] CLI support (`cinch repo add --forge gitlab`)
- [x] Release support (`cinch release` for GitLab)
- [ ] OAuth app registration (for automated setup)
- [ ] Project Access Token creation via API
- [ ] Self-hosted instance documentation

### Forgejo/Gitea Hybrid Flow

- [ ] Register OAuth app on Codeberg
- [ ] Use OAuth to create webhook (automated)
- [ ] Prompt for manual PAT (status posting)
- [ ] Fall back to full manual for self-hosted

### Multi-Forge Setup for Cinch

- [ ] Create GitLab repo (gitlab.com/ehrlich-b/cinch)
- [ ] Create Codeberg repo (codeberg.org/ehrlich/cinch)
- [ ] Add all three as remotes locally
- [ ] Register all three repos with cinch.sh
- [ ] `make push` target to push to all forges
- [ ] Verify releases land on all three simultaneously

---

## Then: MVP 1.5 - PR/MR Support

Currently push-only. PRs are table stakes for real adoption.

- [ ] GitHub Pull Request events
- [ ] GitLab Merge Request events
- [ ] Bitbucket Pull Request events
- [ ] Forgejo/Gitea Pull Request events
- [ ] Status checks on PR head commit

---

## Then: MVP 1.6 - Stripe Integration (Deferred)

**Goal:** Validate the business model with paying customers.

**Status:** Deferred - not working on this for at least a week. Focus on UX/forge work first.

**Pricing model:**
- Public repos: Free forever (marketing)
- Private repos: $5/seat/month (the business)
- Self-hosted: Free forever (MIT escape hatch)

**Why charge now:**
1. $5 validates the business - If people won't pay $5/seat, the thesis is broken
2. Paying customers are better customers - Real bugs, useful feedback, stick around
3. "Free now, paid later" is a trap - Keeps getting pushed, then users are mad
4. $5 is an impulse buy - Below "ask my manager" threshold
5. Fits the longevity story - "$5 in 2026, $5 in 2036"
6. Stripe is already set up - No technical barrier

**First paying customer > 1000 free users** for validating this works.

- [ ] Stripe checkout integration
- [ ] Seat counting logic
- [ ] Public vs private repo detection
- [ ] Billing page in web UI
- [ ] Grace period for failed payments

---

## Backlog: Forge Expansion (Tier 2)

Enterprise and ecosystem-specific forges. Low priority - demand-driven.

| Forge | Users | Notes |
|-------|-------|-------|
| Azure DevOps | Enterprise | Microsoft shops, deep Azure integration |
| AWS CodeCommit | Enterprise | AWS ecosystem, declining but still used |
| Gitee | Millions | Chinese market (if we want international reach) |

- [ ] Azure DevOps integration
- [ ] AWS CodeCommit integration
- [ ] Gitee integration (optional - requires Chinese localization consideration)

---

## Backlog

### Forge Expansion (Tier 3) - Self-Hosted / Niche

Already done: Gitea, Forgejo (same codebase). These cover the self-hosted community well.

| Forge | Status | Notes |
|-------|--------|-------|
| Gitea | ✅ Done | Popular self-hosted |
| Forgejo | ✅ Done | Community fork, powers Codeberg |
| Gogs | Maybe | Gitea predecessor, some legacy installs |
| SourceHut | Maybe | Minimalist, email-based patches, very niche |
| Gerrit | Maybe | Google-style code review, enterprise |

### Scale
- [ ] Worker labels and routing
- [ ] Fan-out to multiple workers
- [ ] Postgres storage backend

### Polish
- [ ] `cinch status` - check job status from CLI
- [ ] `cinch logs -f` - stream logs from CLI
- [ ] Filter jobs by repo/status/branch in UI
- [ ] Show current job per worker in UI
- [ ] Homepage should only show your own jobs (not all jobs)
- [ ] Retry/rerun failed builds from UI
- [ ] Create releases from cinch.sh UI (push tag to user's repo via forge API)

### Known Issues
- [x] **Rejected jobs not re-queued** - Fixed: jobs re-queue automatically on worker disconnect.
- [x] **Jobs lost on server restart** - Fixed: orphaned jobs re-queue with fresh tokens (GitHub App regenerates, other forges use stored token).
- [x] **One worker = one job** - Removed concurrency config. Want parallelism? Run more workers.
- [ ] **Worker should check dependencies on startup** - Probe for docker/podman before accepting jobs. Fail fast with "no container runtime found" instead of failing on first job.

---

## Done

### MVP 1.2 - Container Execution & Bootstrap (2026-01-24)

Container execution fully wired. Cinch releases itself.

- [x] Wire container runtime into `worker.go:executeJob()`
  - Detect image source (config > devcontainer > ubuntu:22.04)
  - Build/pull image
  - Run command in container with workspace mount
- [x] Container config options in .cinch.yaml
  - `image: node:20` - pre-built image
  - `dockerfile: path/to/Dockerfile` - build Dockerfile
  - `devcontainer: path` or `devcontainer: false`
  - `container: none` - bare metal escape hatch
- [x] Cache volumes for warm builds (npm, cargo, pip, go)
- [x] Devcontainer for cinch itself (`.devcontainer/`)
- [x] Multi-platform binary installation (`~/.cinch/bin/`)
  - All platforms downloaded: linux-amd64, linux-arm64, darwin-amd64, darwin-arm64
  - Symlink to local platform
  - Linux binary injected into containers (macOS Mach-O can't run in Linux)
- [x] `cinch release` command - create releases on any forge
- [x] `cinch install` command - download and run install script
- [x] Install script verifies binary works before swapping
- [x] Bootstrap loop closed: v0.1.10 built v0.1.11

**Remaining for containers:**
- [ ] Wire service containers into job execution
- [ ] Test matrix: devcontainer.json, Dockerfile, no container config

---

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
