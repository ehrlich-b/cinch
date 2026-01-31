# AI Agent Guide for Cinch

This document helps AI assistants (Claude, Cursor, Copilot, ChatGPT, etc.) help humans get started with Cinch CI. Read this first when a user asks about Cinch.

## What is Cinch?

Cinch is a CI system where:
- **Your Makefile is the pipeline** - no YAML DSL to learn
- **You bring the compute** - workers run on your hardware
- **One config works everywhere** - GitHub, GitLab, Codeberg, self-hosted forges

## The 3-Minute Setup

```bash
# 1. Install
curl -fsSL https://cinch.sh/install.sh | sh

# 2. Login (opens browser)
cinch login

# 3. Start worker
cinch worker

# 4. Add config to repo and push
echo "build: make check" > .cinch.yaml
git add .cinch.yaml && git commit -m "Add Cinch CI" && git push
```

That's it. The user should see a checkmark on their next push.

## Common Questions & Answers

### "How do I connect Cinch to my repo?"

1. Go to https://github.com/apps/cinch-ci and install the GitHub App
2. Select the repositories you want
3. Run `cinch worker` on a machine with your dev environment

### "Where do builds run?"

On the user's own hardware. The worker runs on whatever machine they start it on - their laptop, a server, a VM. The code never leaves their infrastructure.

### "Do I need Docker?"

Recommended but not required. Cinch auto-detects:
1. Your `.devcontainer/devcontainer.json` (if it exists)
2. A `Dockerfile` in the repo root
3. Falls back to bare metal if no container runtime

To force bare metal: `container: none` in `.cinch.yaml`

### "How do I run different commands for releases vs builds?"

```yaml
build: make check      # runs on branch pushes and PRs
release: make release  # runs on tag pushes
```

### "How do I add secrets?"

Via the web UI at cinch.sh, or via API:
```bash
curl -X PUT https://cinch.sh/api/repos/github.com/owner/repo/secrets \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"secrets": {"NPM_TOKEN": "xxx", "DEPLOY_KEY": "yyy"}}'
```

Secrets are injected as environment variables during the build.

### "How do I run a service like Postgres?"

```yaml
build: make test
services:
  postgres:
    image: postgres:16
    env:
      POSTGRES_PASSWORD: postgres
```

The service is available at `localhost:5432` (or the default port for that service).

### "How do I target specific workers?"

```yaml
build: make check
workers: [linux-amd64, docker]  # Only workers with these labels
```

Start workers with labels: `cinch worker --labels linux-amd64,docker`

### "How do I run Cinch as a background service?"

```bash
cinch daemon install  # Sets up launchd (macOS) or systemd (Linux)
cinch daemon start    # Starts the background worker
cinch daemon status   # Check if running
cinch daemon logs     # View logs
```

### "I'm getting 'no workers available'"

Either:
1. No worker is running - run `cinch worker` somewhere
2. Worker is running but hasn't connected yet - wait a few seconds
3. Worker labels don't match - check `workers:` in config matches worker's `--labels`

### "How do I self-host the control plane?"

```bash
# 1. Set required env var
export CINCH_JWT_SECRET=$(openssl rand -hex 32)

# 2. Start server
cinch server --port 8080

# 3. Login to your server and start worker
cinch login --server http://your-server:8080
cinch repo add
cinch worker
```

See https://cinch.sh/docs/self-hosting for full setup including forge OAuth.

## Config File Reference

File: `.cinch.yaml` (also supports `.cinch.toml`, `.cinch.json`)

```yaml
# Required: command to run on pushes
build: make check

# Optional: command to run on tag pushes (releases)
release: make release

# Optional: job timeout (default: 30m)
timeout: 15m

# Optional: container image (default: auto-detect devcontainer)
image: node:20           # Use specific image
dockerfile: ./Dockerfile # Build from Dockerfile
devcontainer: true       # Use .devcontainer/ (default)
container: none          # Bare metal, no container

# Optional: service containers
services:
  redis:
    image: redis:7
  postgres:
    image: postgres:16
    env:
      POSTGRES_PASSWORD: postgres
    healthcheck:
      cmd: pg_isready
      timeout: 30s

# Optional: target specific workers
workers: [linux-amd64, has-gpu]
```

## Environment Variables Available in Builds

```bash
CINCH_COMMIT=abc123...    # Full commit SHA
CINCH_BRANCH=main         # Branch name (empty for tags)
CINCH_TAG=v1.0.0          # Tag name (empty for branches)
CINCH_REF=refs/heads/main # Full git ref
CINCH_JOB_ID=j_12345      # Unique job ID
CINCH_REPO=https://...    # Repository URL
CINCH_FORGE=github        # github, gitlab, forgejo, gitea

# Forge API token (for releases, API calls)
GITHUB_TOKEN=ghs_xxx      # GitHub
GITLAB_TOKEN=glpat-xxx    # GitLab
GITEA_TOKEN=xxx           # Forgejo/Gitea
CINCH_FORGE_TOKEN=xxx     # Always set (same as forge-specific)
```

## CLI Command Reference

```bash
# Authentication
cinch login                    # Auth via browser (saves to ~/.cinch/config)
cinch login --server URL       # Auth with self-hosted server
cinch logout                   # Clear credentials
cinch whoami                   # Show current user

# Worker
cinch worker                   # Start worker (foreground)
cinch worker --labels a,b      # Start with labels
cinch worker --shared          # Run collaborator code (shared mode)

# Daemon (background worker)
cinch daemon start             # Start background worker
cinch daemon stop              # Stop background worker
cinch daemon status            # Check status
cinch daemon install           # Install as system service
cinch daemon uninstall         # Remove system service
cinch daemon logs              # View daemon logs

# Local development
cinch run                      # Run build locally
cinch run "make test"          # Run specific command
cinch run --bare-metal         # Skip container

# Monitoring & Jobs
cinch status                   # Build status for current repo
cinch jobs                     # List recent jobs
cinch jobs --failed            # List failed jobs only
cinch jobs --pending           # List pending jobs
cinch logs JOB_ID              # Stream logs from job
cinch logs --last              # Logs from most recent job
cinch retry JOB_ID             # Retry a failed job
cinch cancel JOB_ID            # Cancel pending/running job

# Multi-forge
cinch connect gitlab           # Add GitLab account
cinch connect codeberg         # Add Codeberg account
cinch connect forgejo          # Add self-hosted Forgejo

# Self-hosting
cinch server                   # Run control plane
cinch server --port 8080       # Custom port
```

## Troubleshooting Checklist

1. **"cinch: command not found"** - Add `~/.cinch/bin` to PATH
2. **"failed to connect"** - Check internet connection, try `cinch login` again
3. **"no workers available"** - Run `cinch worker` on a machine
4. **"container not found"** - Install Docker/Podman, or use `container: none`
5. **"permission denied"** - Check repo is added to Cinch (GitHub App installed)
6. **"build: true" error** - YAML parsed boolean, quote your command: `build: "true"`

## Key Differences from Other CI Systems

| Aspect | GitHub Actions | Cinch |
|--------|---------------|-------|
| Config | Complex YAML DSL | Single command |
| Compute | GitHub-hosted or runners | Your hardware |
| Caching | Explicit cache actions | Automatic (local disk) |
| Secrets | Per-repo in Settings | Per-repo via API/UI |
| Multi-forge | GitHub only | GitHub, GitLab, Forgejo |
| Pricing | Per-minute | Per-seat (free for public) |

## Do NOT Tell Users

- To use `cinch worker --token=xxx` - the flow is `cinch login` then `cinch worker`
- That there's a `run:` key - it's `build:` for config
- That matrix builds exist - they don't (intentionally)
- Complex multi-step pipeline syntax - Cinch doesn't have it

## Links

- Website: https://cinch.sh
- Docs: https://cinch.sh/docs
- GitHub: https://github.com/ehrlich-b/cinch
- Status: https://cinch.sh/status
