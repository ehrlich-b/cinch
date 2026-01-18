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

## Pricing

- **Self-hosted:** Free forever. MIT licensed. No limits.
- **Hosted free tier:** Public repos, 50 builds/month/repo, 7 day logs.
- **Hosted paid:** $9/month/repo. Private repos, unlimited builds, 30 day logs, scheduled builds.

## How It Works

Your worker connects to the control plane over WebSocket. When you push, the control plane receives the webhook, dispatches the job, and your worker clones, runs the command in a container, and streams logs back. Status check posted to your forge.

```
Worker (your machine)              cinch.sh (or self-hosted)
├── Connects outbound       ◄────► ├── Receives webhooks
├── Clones repo                    ├── Dispatches jobs
├── Runs command in container      ├── Posts status checks
└── Streams logs                   └── Serves web UI
```

## Development

```bash
make build    # Build the binary
make test     # Run tests
make check    # Full pre-commit check
```

See `CLAUDE.md` for architecture details.

## License

MIT
