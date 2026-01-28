# cinch

CI that's a cinch. One config. Every forge. Your hardware.

**This repo builds on all three forges with the same `.cinch.yaml`:**

[![GitHub](https://img.shields.io/endpoint?url=https%3A%2F%2Fcinch.sh%2Fapi%2Fbadge%2Fgithub.com%2Fehrlich-b%2Fcinch.json&label=GitHub&logo=github)](https://cinch.sh/jobs/github.com/ehrlich-b/cinch)
[![GitLab](https://img.shields.io/endpoint?url=https%3A%2F%2Fcinch.sh%2Fapi%2Fbadge%2Fgitlab.com%2Fehrlich-b%2Fcinch.json&label=GitLab&logo=gitlab)](https://cinch.sh/jobs/gitlab.com/ehrlich-b/cinch)
[![Codeberg](https://img.shields.io/endpoint?url=https%3A%2F%2Fcinch.sh%2Fapi%2Fbadge%2Fcodeberg.org%2Fehrlich-b%2Fcinch.json&label=Codeberg&logo=codeberg)](https://cinch.sh/jobs/codeberg.org/ehrlich-b/cinch)

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

services:
  postgres:
    image: postgres:16
    env:
      POSTGRES_PASSWORD: postgres

workers: [linux-amd64, linux-arm64]  # optional: fan-out to multiple platforms
```

Builds run in containers by default (auto-detects your devcontainer). Caches persist between builds.

## Self-Host

Run your own cinch server:

```bash
cinch server --port 8080 --db ./cinch.db
```

Point your workers at it, configure webhooks to hit your server. No dependency on cinch.sh.

See `cinch server --help` for configuration options.

## Multi-Forge

Works with GitHub, GitLab, Forgejo, Gitea, Bitbucket. Same config, same checkmarks.

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

<!-- PR support test -->
