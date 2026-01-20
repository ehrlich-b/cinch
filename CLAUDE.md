# Cinch

CI that's a cinch. One config. Every forge. Your hardware.

## What This Is

Cinch is a radically simple CI system:
- Control plane receives webhooks, dispatches jobs, posts status checks
- Workers run on your hardware, execute ONE command, stream logs back
- Multi-forge: GitHub, GitLab, Forgejo, Gitea, Bitbucket

**Philosophy:** Your Makefile is the pipeline. We just invoke it.

## Key Decisions

- **Pricing:** $5/seat/month for private repos, free for public, free self-hosted (MIT)
- **Tech stack:** Go, single binary, WebSocket + JSON protocol, SQLite/Postgres
- **Config formats:** YAML, TOML, JSON (all supported, user's choice)
- **Containers:** Default (Docker), opt-out with `--bare-metal` or `container: none`
- **No:** multi-step pipelines, DAGs, matrix builds, plugins, marketplace
- **Yes:** services (docker containers), worker labels, fan-out to multiple workers, scheduled/manual triggers

## Architecture

```
Worker (your machine)              Control Plane (cinch.sh or self-hosted)
├── Connects via WebSocket  ◄────► ├── Receives forge webhooks
├── Registers labels + concurrency ├── Routes jobs to workers
├── Runs ONE command in container  ├── Posts status checks back
├── Streams logs                   └── Streams logs to web UI
└── Local cache (filesystem)
```

## Repository Structure

```
cmd/cinch/        # CLI entry point
internal/
  cli/            # CLI commands (run, etc.)
  config/         # Config parsing (YAML/TOML/JSON)
  worker/         # Executor, container runtime
  server/         # HTTP server, webhooks, dispatch (TODO)
  storage/        # Database layer (TODO)
  protocol/       # WebSocket message types (TODO)
  forge/          # GitHub/GitLab/Forgejo clients (TODO)
web/              # React frontend (Vite)
design/           # Technical design docs
```

## Build Commands

```bash
make build-go     # Build Go binary (fast, for iteration)
make build        # Build everything (Go + web assets)
make test         # Run tests
make fmt          # Format code
make check        # fmt + test + lint

make run CMD="echo hello"      # Run command in container
make run-bare CMD="echo hello" # Run command bare metal
make ci                        # Run using .cinch.yaml config
make validate                  # Validate config file
```

## Design Docs

Read these before implementing:
- `design/00-overview.md` - Architecture overview
- `design/08-config-format.md` - The config file spec (.cinch.yaml/.toml/.json)
- `design/02-protocol.md` - WebSocket protocol between worker and server
- `design/09-containerization.md` - Container execution, caching, devcontainer support
- `design/10-services.md` - Service containers (postgres, redis, etc.)

## Style

- Go code, standard formatting (gofmt)
- Single binary that does everything: `cinch server`, `cinch worker`, `cinch run`
- Aggressively simple - reject features that add complexity without clear value
