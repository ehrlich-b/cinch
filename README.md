# cinch

CI that's a cinch.

```yaml
# .cinch.yaml
command: make ci
```

Push code, get a green checkmark. Your Makefile is the pipeline.

## Install

```bash
curl -fsSL https://cinch.sh/install.sh | sh
```

Or build from source: `make build`

## Usage

```bash
cinch worker --server https://cinch.sh --token <your-token>
```

Add `.cinch.yaml` to your repo, configure the webhook, push. Done.

```yaml
# .cinch.yaml
command: make ci

services:
  postgres:
    image: postgres:16
    env:
      POSTGRES_PASSWORD: postgres

workers: [linux-amd64, linux-arm64]  # optional: fan-out to multiple platforms

trigger:
  pull_requests: true
  schedule: "0 0 * * *"  # nightly
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

Your worker runs on your hardware—a $5 VPS, a Fly.io machine, your gaming PC, whatever. It connects outbound to the control plane over WebSocket. When you push, the control plane receives the webhook, dispatches the job, and your worker clones, runs the command in a container, and streams logs back. Status check posted to your forge.

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
# Cinch CI
