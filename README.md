# cinch

CI that's a cinch. One config. Every forge. Your hardware.

**This repo builds on all three forges with the same `.cinch.yaml`:**

[![cinch](https://cinch.sh/badge/github.com/ehrlich-b/cinch.svg)](https://cinch.sh/jobs/github.com/ehrlich-b/cinch)
[![cinch](https://cinch.sh/badge/gitlab.com/ehrlich-b/cinch.svg)](https://cinch.sh/jobs/gitlab.com/ehrlich-b/cinch)
[![cinch](https://cinch.sh/badge/codeberg.org/ehrlich-b/cinch.svg)](https://cinch.sh/jobs/codeberg.org/ehrlich-b/cinch)

```yaml
# .cinch.yaml
build: make check
release: make release
```

Push code, get a green checkmark. Your Makefile is the pipeline.

## Install

```bash
curl -fsSL https://cinch.sh/install.sh | sh
```

Or build from source: `make build`

## Usage

```bash
cinch login    # Auth via browser
cinch worker   # Start building
```

Add `.cinch.yaml` to your repo, push. Done.

```yaml
# .cinch.yaml
build: make check
release: make release  # optional: runs on tag pushes
workers: [linux-amd64, linux-arm64]  # optional: fan-out to multiple platforms
```

Builds run in containers by default (auto-detects your devcontainer). Caches persist between builds.

## CLI Commands

```bash
# Core workflow
cinch login                     # Auth via browser
cinch worker                    # Start building
cinch run                       # Test locally

# Forge connections
cinch connect gitlab            # Add GitLab account
cinch connect codeberg          # Add Codeberg account

# Worker daemon
cinch daemon start|stop|status  # Manage background worker
cinch daemon install            # Install as system service

# Monitoring
cinch status                    # Build status for current repo
cinch logs JOB_ID               # Stream job logs

# Self-hosting
cinch server                    # Run control plane
```

## Self-Host

Cinch is **100% self-hostable**. Single binary, SQLite by default, no external dependencies.

```bash
# Generate secret and start
export CINCH_JWT_SECRET=$(openssl rand -hex 32)
cinch server

# Workers connect to your server
cinch login --server https://ci.example.com
cinch worker
```

Put it behind Caddy or nginx for TLS. That's it.

See **[docs/self-hosting.md](docs/self-hosting.md)** for the full guide: forge OAuth setup, reverse proxy examples, systemd service, and security checklist.

## Multi-Forge

Works with GitHub, GitLab, and Codeberg. Same config, same checkmarks. (Self-hosted Forgejo and GitLab too.)

## How It Works

Cinch is a control plane. You bring the compute.

Your worker runs on your hardware—just the computer you develop on, a $5 VPS, a Fly.io machine, your gaming PC, whatever. It connects outbound to the control plane over WebSocket. When you push, the control plane receives the webhook, dispatches the job, and your worker clones, runs the command in a container, and streams logs back. Status check posted to your forge.

```
Worker (your machine)              cinch.sh (or self-hosted)
├── Connects outbound       ◄────► ├── Receives webhooks
├── Clones repo                    ├── Dispatches jobs
├── Runs command in container      ├── Posts status checks
└── Streams logs                   └── Serves web UI
```

Your code never leaves your infrastructure. Only logs and status checks go to the control plane.

**Why this matters:** Persistent filesystem. Docker layers, cargo/npm/go caches stay warm between builds. No more waiting for cold caches on every run.

## Pricing

- **Self-hosted:** Free forever. MIT licensed. Run your own control plane.
- **Hosted control plane:** $5/seat/month for private repos. Free for public repos. A "seat" is any unique contributor who triggers a build.

You pay for your own compute (VPS, electricity, etc). We just coordinate.

## Development

```bash
make build    # Build the binary
make test     # Run tests
make check    # Full pre-commit check
```

See `CLAUDE.md` for architecture details.

## License

MIT
