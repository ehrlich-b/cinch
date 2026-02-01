# Cinch

CI that's a cinch. One config. Every forge. Your hardware.

**Public website:** https://cinch.sh (hosted on fly.io as `cinch` app)

## What This Is

Cinch is a radically simple CI system:
- Control plane receives webhooks, dispatches jobs, posts status checks
- Workers run on your hardware, execute ONE command, stream logs back
- Multi-forge: GitHub, GitLab, Codeberg (plus self-hosted Forgejo/GitLab)

**Philosophy:** Your Makefile is the pipeline. The exact `make build` you run locally—that's your CI.

## User Flow (IMPORTANT - memorize this)

```bash
# 1. Install cinch
curl -sSL https://cinch.sh/install.sh | sh

# 2. Login (opens browser, saves credentials to ~/.cinch/config)
cinch login

# 3. Add your repo (sets up webhooks)
cinch repo add

# 4. Start a worker (uses saved credentials - NO TOKEN FLAG NEEDED)
cinch worker

# That's it. Push and builds run.
```

**NEVER tell users to use `--token=xxx`** - the normal flow is `cinch login` then `cinch worker` with no flags.

## Config File (.cinch.yaml)

```yaml
# Minimal - just specify what to run on push
build: make build

# With releases (runs on tag push)
build: make build
release: make release

# With timeout
build: make check
timeout: 15m

# Container options
image: node:20              # Use specific image
dockerfile: ./Dockerfile    # Build from Dockerfile
devcontainer: true          # Use .devcontainer/
container: none             # Bare metal (no container)
```

**Key insight:** `build:` runs `make build` - the SAME command you run locally. No new syntax to learn.

**Default timeout:** 30 minutes (configurable via `timeout:` field).

**Footgun warning:** Don't use `build: true` - YAML parses that as boolean. Use `build: "true"` or better yet, an actual command.

## CLI Commands

```bash
# Authentication
cinch login                 # Auth via browser, saves to ~/.cinch/config
cinch logout                # Remove credentials
cinch whoami                # Show current auth status

# Forge connection (after login)
cinch connect gitlab        # Connect GitLab account
cinch connect codeberg      # Connect Codeberg account
cinch connect forgejo       # Connect self-hosted Forgejo

# Worker
cinch worker                # Start worker (foreground, ctrl+c to stop)
cinch worker --labels gpu   # With labels for job routing
cinch worker --shared       # Shared mode: run collaborator code

# Worker daemon (background service)
cinch daemon start          # Start worker as background daemon
cinch daemon stop           # Stop the daemon
cinch daemon status         # Check daemon status
cinch daemon install        # Install as system service (launchd/systemd)
cinch daemon uninstall      # Remove system service
cinch daemon logs           # View daemon logs

# Local development
cinch run                   # Run build locally (uses .cinch.yaml)
cinch run "make test"       # Run specific command
cinch run --bare-metal      # Skip container

# Status & Jobs
cinch status                # Show build status for current repo
cinch jobs                  # List recent jobs
cinch jobs --failed         # List failed jobs only
cinch jobs --pending        # List pending jobs
cinch logs JOB_ID           # Stream logs from a job
cinch logs --last           # Logs from most recent job
cinch retry JOB_ID          # Retry a failed job
cinch cancel JOB_ID         # Cancel a pending/running job

# Releases (used in CI, not manually)
cinch release dist/*        # Upload release assets to forge

# Repository management
cinch repo add              # Add repo to Cinch
cinch repo list             # List connected repos

# Secrets management
cinch secrets list          # List secret names for current repo
cinch secrets set KEY=VALUE # Set a secret
cinch secrets delete KEY    # Delete a secret

# Config validation
cinch config validate       # Validate .cinch.yaml

# Installation
cinch install               # Install cinch binary to PATH
cinch install --with-daemon # Install and set up daemon
```

## Environment Variables in Jobs

Every CI job gets these environment variables:

```bash
# Git context
CINCH_REF=refs/heads/main       # Full ref (or refs/tags/v1.0.0)
CINCH_BRANCH=main               # Branch name (empty for tags)
CINCH_TAG=                      # Tag name (empty for branches)
CINCH_COMMIT=abc1234567890      # Full commit SHA

# Job context
CINCH_JOB_ID=j_abc123
CINCH_REPO=https://github.com/owner/repo.git
CINCH_FORGE=github              # github, gitlab, forgejo, gitea

# Forge API token (for releases, comments, etc.)
GITHUB_TOKEN=ghs_xxx            # GitHub
GITLAB_TOKEN=glpat-xxx          # GitLab
GITEA_TOKEN=xxx                 # Forgejo/Gitea
CINCH_FORGE_TOKEN=xxx           # Always set (same as forge-specific)
CI_JOB_TOKEN=xxx                # GitLab compatibility (same as GITLAB_TOKEN)
```

## Server Environment Variables

For self-hosting `cinch server`:

```bash
# Core server config
CINCH_ADDR=:8080                # Listen address (default :8080)
CINCH_DATA_DIR=/var/lib/cinch   # Data directory for SQLite, logs
CINCH_BASE_URL=https://ci.example.com    # Public URL for webhooks
CINCH_WS_BASE_URL=wss://ci.example.com   # WebSocket URL (usually same host)
CINCH_SECRET_KEY=xxx            # CRITICAL: Secret for JWT signing and encryption
CINCH_LOG_DIR=/var/log/cinch    # Log storage directory

# R2 log storage (optional, for cloud log storage)
CINCH_R2_ACCOUNT_ID=xxx
CINCH_R2_ACCESS_KEY_ID=xxx
CINCH_R2_SECRET_ACCESS_KEY=xxx
CINCH_R2_BUCKET=cinch-logs

# GitHub App (for GitHub integration)
CINCH_GITHUB_APP_ID=123456
CINCH_GITHUB_APP_PRIVATE_KEY=-----BEGIN RSA PRIVATE KEY-----...
CINCH_GITHUB_APP_WEBHOOK_SECRET=xxx
CINCH_GITHUB_APP_CLIENT_ID=Iv1.xxx
CINCH_GITHUB_APP_CLIENT_SECRET=xxx

# GitLab OAuth (for GitLab integration)
CINCH_GITLAB_CLIENT_ID=xxx
CINCH_GITLAB_CLIENT_SECRET=xxx
CINCH_GITLAB_URL=https://gitlab.com  # Or self-hosted URL

# Forgejo/Gitea OAuth (for Forgejo/Codeberg integration)
CINCH_FORGEJO_CLIENT_ID=xxx
CINCH_FORGEJO_CLIENT_SECRET=xxx
CINCH_FORGEJO_URL=https://codeberg.org  # Or self-hosted URL
```

## Cinch's Own CI (.cinch.yaml)

```yaml
build: make check
release: make release
timeout: 15m
```

The Makefile has `build`, `check`, `release` targets. Cinch just invokes them.

## Key Decisions

- **Pricing:** Free during beta. Later: $5/seat/month for private repos, free for public, free self-hosted (MIT)
- **Tech stack:** Go, single binary, WebSocket + JSON protocol, SQLite
- **Config formats:** YAML, TOML, JSON (all supported)
- **Containers:** Default (Docker/Podman), opt-out with `--bare-metal` or `container: none`
- **No:** multi-step pipelines, DAGs, matrix builds, plugins, marketplace
- **Yes:** services (docker containers), worker labels, fan-out to multiple workers

## Architecture

```
Worker (your machine)              Control Plane (cinch.sh)
├── cinch login (once)             ├── Receives forge webhooks
├── cinch worker (runs forever)    ├── Dispatches jobs to workers
├── Runs ONE command per job       ├── Posts status checks to forge
├── Streams logs back              └── Web UI for monitoring
└── Uses Docker/Podman
```

## Repository Structure

```
cmd/cinch/        # CLI entry point (main.go)
internal/
  cli/            # CLI commands
  config/         # Config parsing (.cinch.yaml)
  worker/         # Job execution, container runtime
  server/         # HTTP server, webhooks, job dispatch
  storage/        # SQLite database
  forge/          # GitHub, GitLab, Forgejo, Gitea clients
web/              # React frontend (Vite + TypeScript)
design/           # Technical design docs
```

## Build Commands

```bash
make build-go     # Build Go binary only (fast iteration)
make build        # Build everything (Go + web assets)
make test         # Run tests
make check        # fmt + test + lint (what CI runs)
make release      # Cross-compile + upload (CI only, needs CINCH_TAG)
```

## Design Docs

- `design/00-overview.md` - Architecture overview
- `design/08-config-format.md` - Config file spec
- `design/09-containerization.md` - Container execution
- `design/11-web-ui.md` - Web UI design

## Style

- Go code, standard formatting (gofmt)
- Single binary: `cinch server`, `cinch worker`, `cinch run`, etc.
- Aggressively simple - reject complexity without clear value
- The web UI uses React + TypeScript + Vite, dark theme, green accent color

## Git Workflow (Multi-Forge)

Cinch is hosted on GitHub, GitLab, and Codeberg simultaneously. **Always use `make push` instead of `git push`.**

```bash
make push        # Push to all forges (github, gitlab, codeberg)
make push-tags   # Push tags to all forges (triggers releases)
```

**NEVER use `git push`** - it only pushes to one remote.

## Fly.io Deployment

```bash
fly deploy              # Deploy to Fly.io
make fly-logs           # View recent logs (uses --no-tail)
```

**NEVER use `fly logs` directly** - it tails forever and hangs. Always use `make fly-logs`.

## Common Mistakes to Avoid

1. **DON'T** tell users to use `cinch worker --token=xxx` - the flow is `cinch login` then `cinch worker`
2. **DON'T** use `run: make ci` in examples - use `build: make build` (the actual config key)
3. **DON'T** show complex YAML - Cinch's whole point is ONE command
4. **DON'T** use `git push` - use `make push` to push to all forges
5. **DON'T** use `fly logs` - use `make fly-logs` (avoids infinite tailing)
6. **DO** emphasize "same command locally and in CI"
7. **DO** mention `cinch release` works across forges (GitHub → GitLab migration keeps working)
