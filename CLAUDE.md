# Cinch

CI that's a cinch. One config. Every forge. Your hardware.

## What This Is

Cinch is a radically simple CI system:
- Control plane receives webhooks, dispatches jobs, posts status checks
- Workers run on your hardware, execute ONE command, stream logs back
- Multi-forge: GitHub, GitLab, Forgejo, Gitea, Bitbucket

**Philosophy:** Your Makefile is the pipeline. We just invoke it.

## Key Decisions

- **Pricing:** $9/month flat for hosted control plane, free self-hosted (MIT)
- **Tech stack:** Go, single binary, WebSocket + JSON protocol, SQLite/Postgres
- **No:** multi-step pipelines, DAGs, matrix builds, plugins, marketplace
- **Yes:** services (docker containers), worker labels, fan-out to multiple workers

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
design/           # Technical design docs (00-09)
cinch_biz.md      # Business philosophy and pricing rationale
README.md         # Public-facing pitch
```

## Build Commands

TBD - project is pre-code.

## Design Docs

Read these before implementing:
- `design/00-overview.md` - Architecture overview
- `design/08-config-format.md` - The .cinch.yaml spec
- `design/02-protocol.md` - WebSocket protocol between worker and server

## Style

- Go code, standard formatting (gofmt)
- Single binary that does everything: `cinch server`, `cinch worker`, `cinch run`
- Aggressively simple - reject features that add complexity without clear value
