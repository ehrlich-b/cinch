# Cinch TODO

**Last Updated:** 2026-01-27

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

## Current: Unified Onboarding Flow

**Insight:** Login IS onboarding. No email auth - an account with zero integrations is inert.

**The flow:**
```
cinch.sh (landing)
    ↓
"Get Started" → Select your forge [GitHub] [GitLab] [Forgejo]
    ↓
OAuth with forge → You're logged in AND connected
    ↓
Select repos to onboard (GitHub: already selected, GitLab: multi-select modal)
    ↓
Success! Now show:
    ┌─────────────────────────────────────────────┐
    │  curl -sSL https://cinch.sh/install.sh | sh │
    │  cinch login      # quick bounce, you're in │
    │  cinch worker --all                         │
    └─────────────────────────────────────────────┘
    ↓
Dashboard with your repos
```

**Why this works:**
- No "login then setup later" - you're productive immediately
- `cinch login` is a 1-second bounce (already authed via OAuth)
- `cinch worker --all` is the right default for new users
- Zero friction from "interested" to "first build running"

**Email handling:**
- We need email (notifications, billing) but NOT for auth
- No email verification required (forge OAuth is the trust anchor)
- GitHub users may have multiple emails - let them pick during onboarding
- Show: "Which email should we use?" with dropdown of their GitHub emails
- Default to primary, but allow selection (work vs personal)

**Implementation:**
- [x] Remove separate login button, replace with "Get Started"
- [x] Forge selector as first screen (not login screen)
- [x] After OAuth callback → email selector (if multiple) → repo selector → success page
- [x] Fetch user emails from forge API (GitHub: `user:email` scope)
- [x] Store selected email in user record (email is now the identity)
- [x] Email-based identity: cookie stores email, not username
- [x] Duplicate email check: if email exists, show "account exists" error
- [x] `cinch login` detects existing session, skips device code flow
- [x] Success page emphasizes `--all` flag for first-time setup
- [x] Device code page auto-fills token from URL (`/device?code=XXX`)

**Identity model:**
- [x] Email is the canonical identity (verified by forge)
- [x] Same email = same account (login finds existing, connects forge)
- [x] Onboard with any forge, then connect additional forges from account settings
- [x] Each forge connection = repos from that forge available for CI
- [x] Username is just metadata (not unique) - drop UNIQUE constraint on users.name
- [x] Worker IDs should use email, not username (`user:email@example.com` not `user:username`)

**GitHub App consolidation:**
- [x] Use GitHub App's OAuth credentials for login (not separate OAuth App)
- [x] CINCH_GITHUB_APP_CLIENT_ID/SECRET env vars (from App settings)
- [x] Deleted the separate OAuth App from GitHub
- [x] Redirect to /dashboard after login (not homepage)

**Account settings (per-forge):**
- [x] Account page with "Connect [Forge]" buttons for each unconnected forge
- [x] Disconnect forge (removes repos, keeps account if other forges connected)
- [x] Delete account option (requires disconnecting all forges first)
- [x] Warning when disconnecting last forge: "This will permanently delete your account"

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

- [ ] Light/dark theme toggle
- [ ] Public repo pages: `cinch.sh/jobs/github.com/owner/repo` shows builds for that repo
  - Public repos: accessible without login
  - Private repos: require auth
  - This is what badges should link to
- [ ] Repo detail pages (click repo → see all builds for that repo)
- [ ] Loading skeletons
- [ ] Real Settings page (tokens, repos)
- [ ] Job filtering by repo/status/branch
- [ ] Re-run failed builds
- [ ] Badge repo selector
- [ ] Badge links should go to repo job page (e.g., `cinch.sh/jobs/github.com/owner/repo`)
- [ ] **Distinctive badge design** - Shields.io badges are invisible (everyone uses them, they all look identical). If badges are a distribution channel, they need to look recognizably "Cinch" at a glance. Different shape, proportions, or style. Serve custom SVG from `cinch.sh/badge/...` instead of redirecting to shields.io.

---

## Then: MVP 1.4 - Forge Expansion + Multi-Forge Presence

**Goal:** Cover the major forges beyond GitHub AND dogfood multi-forge by hosting Cinch itself on all supported platforms.

See `design/forge/` for detailed integration plans per forge.
See `design/12-multi-forge-presence.md` for Cinch's own multi-forge hosting strategy.

| Forge | Users | Priority | Notes |
|-------|-------|----------|-------|
| GitHub | 65M+ | ✅ Done | Full integration (App + Checks API) |
| GitLab | 30M | ✅ Done | Full integration (OAuth + auto-webhook + fallback) |
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

**Client-side (forge interface):**
- [x] Core forge implementation (`internal/forge/gitlab.go`)
- [x] Webhook parsing (X-Gitlab-Token header verification)
- [x] Status API (commit statuses via project path)
- [x] Release support (`cinch release` uploads to GitLab package registry)
- [x] CLI manual fallback (`cinch repo add --forge gitlab --token xxx`)

**Server-side (automated onboarding):**
- [x] Register GitLab OAuth app (gitlab.com)
- [x] `GET /auth/gitlab` - Start OAuth flow
- [x] `GET /auth/gitlab/callback` - Exchange code, store token
- [x] `GET /api/gitlab/projects` - List user's projects
- [x] `POST /api/gitlab/setup` - Create webhook + attempt PAT creation
- [x] Webhook creation via OAuth token
- [x] PAT auto-creation attempt via API (Project Access Tokens)
- [x] Graceful fallback with options: manual token OR OAuth session
- [x] OAuth token refresh in forge (when using OAuth session fallback)
- [x] Self-hosted instance support (CINCH_GITLAB_URL env var)
- [x] PAT created with Maintainer access level (required for commit status)
- [x] Idempotent re-onboarding (delete old webhooks, upsert repo tokens)
- [x] Fixed webhook owner parsing (use path_with_namespace, not display name)

**Full onboarding flow (DONE):**
- [x] `cinch gitlab connect` - CLI command to connect GitLab account
  - Opens browser for OAuth flow
  - If Premium: creates PAT and stores it
  - If free tier: stores OAuth credentials
  - Either way, credentials persist for future `repo add` calls
- [x] User table to store GitLab credentials per user (migration + methods)
- [x] After connect, list ALL repos with checkboxes to select which to onboard
- [x] `cinch repo add owner/name --forge gitlab` uses stored credentials to auto-create webhook
- [x] Same UX via CLI or web (both hit same API)
- [x] Web UI: Repos page with "Connect GitLab" button → project selector (multi-select)

**GitHub App auto-onboarding (TODO):**
Currently repos are created lazily on first push. The GitHub App already knows which repos user selected during install.
- [ ] On `installation` webhook event, auto-create repos for all selected repositories
- [ ] On `installation_repositories` (added/removed), sync repo list
- [ ] Show repos in UI immediately after app install, before first push

**GitHub web onboarding (TODO - loop back):**
- [ ] Allow GitHub repo onboarding through web UI (not just GitHub App)
- [ ] "Connect GitHub" button similar to GitLab flow

### Forgejo/Gitea Hybrid Flow ✅

- [x] Register OAuth app on Codeberg
- [x] Use OAuth to create webhook (automated)
- [x] Prompt for manual PAT (status posting)
- [x] `cinch connect codeberg` CLI command
- [x] Idempotent re-onboarding (delete old webhooks, upsert repo tokens)
- [ ] Self-hosted support: `cinch connect gitlab --host gitlab.mycompany.com`
- [ ] Self-hosted support: `cinch connect forgejo --host git.mycompany.com`
- [ ] Differentiate "codeberg" vs "forgejo" in CLI/UI (codeberg = forgejo at codeberg.org)

### Multi-Forge Setup for Cinch ✅

- [x] Create GitLab repo (gitlab.com/ehrlich-b/cinch)
- [x] Create Codeberg repo (codeberg.org/ehrlich/cinch)
- [x] Add all three as remotes locally
- [x] Register all three repos with cinch.sh
- [x] `make push` target to push to all forges
- [x] Verify releases land on all three simultaneously

---

## Then: MVP 1.5 - Worker Ergonomics

**Goal:** Worker setup so simple that "I don't want to run my own runners" stops being a complaint.

### `cinch daemon` - Dead Simple Background Worker

The dream:
```bash
cinch login
cinch daemon install   # creates user-level service, no sudo
cinch daemon start     # starts it
# Done. Your Mac/Linux box is now a CI runner.
```

**Daemonology in 2026:**

| Platform | User-level daemon | Location | No sudo? |
|----------|------------------|----------|----------|
| macOS | launchd agent | `~/Library/LaunchAgents/` | ✅ |
| Linux | systemd user service | `~/.config/systemd/user/` | ✅ |

Both platforms support user-level daemons without sudo. This is the path.

**Commands:**
```bash
cinch daemon install    # Write service file, enable on boot
cinch daemon start      # Start the worker daemon
cinch daemon stop       # Stop the worker daemon
cinch daemon status     # Is it running? Last job?
cinch daemon uninstall  # Remove service file
cinch daemon logs       # Tail the daemon logs
```

**macOS implementation:**
```xml
<!-- ~/Library/LaunchAgents/sh.cinch.worker.plist -->
<plist version="1.0">
<dict>
    <key>Label</key><string>sh.cinch.worker</string>
    <key>ProgramArguments</key>
    <array>
        <string>/Users/you/.cinch/bin/cinch</string>
        <string>worker</string>
        <string>--all</string>
    </array>
    <key>RunAtLoad</key><true/>
    <key>KeepAlive</key><true/>
    <key>StandardOutPath</key><string>/Users/you/.cinch/logs/worker.log</string>
    <key>StandardErrorPath</key><string>/Users/you/.cinch/logs/worker.err</string>
</dict>
</plist>
```

**Linux implementation:**
```ini
# ~/.config/systemd/user/cinch-worker.service
[Unit]
Description=Cinch CI Worker
After=network.target

[Service]
ExecStart=%h/.cinch/bin/cinch worker --all
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
```

**Open questions:**
- [ ] What about Windows? (Probably just "run in terminal" for now)
- [ ] Docker-in-Docker on Mac? (Docker Desktop socket passthrough)
- [ ] Should `cinch daemon install` also run `cinch login` if not logged in?

---

**Current goal (before daemon):** More intuitive worker setup with sensible defaults for different use cases.

**Problem:** Two use cases in tension:
- **Local machine:** Only build MY stuff on shared repos (don't build other people's PRs)
- **Cloud worker:** Build everything (this is a dedicated CI runner)

**Proposed UX:**
```bash
cinch worker                           # Interactive: select repos or "all"
cinch worker --all                     # Worker for ALL your authed repos
cinch worker --repos owner/a,team/b    # Specific repos only
```

**Interactive mode (default):**
- Show list of connected repos with checkboxes
- "Select all" option
- Remember selection for next time (in ~/.cinch/config)

**Smart defaults:**
- First run: interactive selector
- Subsequent runs: use saved selection (show "Using saved repos: x, y, z")
- `--all` overrides to build everything

**Open questions:**
- [ ] What about large shared repos? Only build commits from "your" branches/PRs?
- [ ] Should workers have a "personal" vs "team" mode?
- [ ] How to handle org repos where you're a member but not pushing?

**Deeper issue: Teams & Permissions (needs thinking)**

This is "too enterprise" for our primary user, but we need a coherent answer.

Core tension: Our accounts aren't "real" - they're bound to forge identities. So how do teams work?

- Teams should reflect org structure / forge permission model
- When PATs are onboarded, how do we ensure we only show repos the user should see?
  - Does this poke a hole in our auth model?
  - PAT might have access to repos the *user* shouldn't see in Cinch context
- PR workflows: If I run a worker on my laptop, I only want MY PRs to run
  - Not a generic worker for the whole org
  - Need to filter by PR author, not just repo access
- Org repos where user is member but not actively contributing?

Possible directions:
- [ ] Mirror forge permissions exactly (simplest, but limits us)
- [ ] Personal vs org context toggle
- [ ] Worker claims jobs by author, not just repo
- [ ] Defer entirely - single-user / small-team focus for now

---

## Then: MVP 1.6 - PR/MR Support

Currently push-only. PRs are table stakes for real adoption.

- [ ] GitHub Pull Request events
- [ ] GitLab Merge Request events
- [ ] Bitbucket Pull Request events
- [ ] Forgejo/Gitea Pull Request events
- [ ] Status checks on PR head commit

---

## Then: MVP 1.7 - Stripe Integration (Deferred)

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
- [ ] **Payment prompt during onboarding** - When user selects a private repo, prompt for payment before completing setup
- [ ] Test payment flow with a real private repo (dogfood the pro tier)
- [ ] Billing page in web UI
- [ ] Grace period for failed payments

---

## Future: Parallel Commands + Event Abstraction

**The insight:** `.cinch.yaml` is just a thin wrapper around webhook event types.

```yaml
build: make check      # ← what to run on commit push
release: make release  # ← what to run on tag push
```

That's it. `build` and `release` are opinionated aliases for:
- `commit:` - any commit pushed to any branch
- `tag:` - any tag pushed

**The abstraction:**
```
webhook event → cinch config key → your command(s)
```

We're just a webhook router. You tell us what to run when we get each event type. Your Makefile does the actual work.

**Parallel execution (future):**
```yaml
# String = one command
build: make check

# Array = parallel jobs (no DAG, no dependencies)
build:
  - make build
  - make test
  - make docs
```

Array items fan out as independent parallel jobs. Need sequencing? Put it in your Makefile. Need conditionals? Use `CINCH_BRANCH` in your Makefile. No workflow DSL.

**Future extensibility:**
- Support `commit:`/`tag:` as aliases for `build:`/`release:`
- Potentially expose raw webhook types: `pull_request:`, `issue_comment:`, etc.
- Forge-specific events: `gitlab_merge_request:`, `github_check_suite:`
- But start minimal - `build` and `release` cover 95% of use cases

**Why NO DAG:**
- If you need A→B→C, write `make a && make b && make c`
- Dependency management belongs in build tools, not CI config
- DAGs are where CI complexity explodes

**Implementation:**
- [ ] Config: `build` and `release` accept string OR string array
- [ ] Array items dispatch as parallel jobs (fan-out)
- [ ] Each job reports status independently
- [ ] Combined status: all pass = green, any fail = red
- [ ] Aliases: accept `commit:`/`tag:` as synonyms (maybe v2)

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

### Dev Experience
- [x] **Pre-commit hook** - Runs `cinch run` before every commit (dogfooding). Install with `make install-hooks`.

### Known Issues
- [x] **Rejected jobs not re-queued** - Fixed: jobs re-queue automatically on worker disconnect.
- [x] **Jobs lost on server restart** - Fixed: orphaned jobs re-queue with fresh tokens (GitHub App regenerates, other forges use stored token).
- [x] **One worker = one job** - Removed concurrency config. Want parallelism? Run more workers.
- [x] **Worker logs too noisy** - Added `--verbose` flag, quiet mode by default shows only banners.
- [x] **Banner should show forge** - Now shows "GITHUB STARTED" / "GITLAB STARTED" / "FORGEJO STARTED".
- [ ] **Worker should check dependencies on startup** - Probe for docker/podman before accepting jobs. Fail fast with "no container runtime found" instead of failing on first job.
- [ ] **Cache volumes need docs + configurability** - Currently hardcoded defaults (npm, cargo, gomod, ~/.cache). Document what's cached, allow override via .cinch.yaml if needed.

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
