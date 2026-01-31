# Cinch Architecture Overview

## Philosophy

Cinch follows the "single binary" philosophy inspired by tunn.to: one Go executable contains everything. No microservices, no containers required, no dependency hell. Download, run, done.

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              cinch binary                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐        │
│  │   server    │  │   worker    │  │     cli     │  │   web ui    │        │
│  │             │  │             │  │             │  │  (embedded) │        │
│  │ • HTTP API  │  │ • WebSocket │  │ • run       │  │             │        │
│  │ • Webhooks  │  │ • Job exec  │  │ • status    │  │ • Jobs list │        │
│  │ • WebSocket │  │ • Log stream│  │ • logs      │  │ • Log view  │        │
│  │ • Auth      │  │ • Clone     │  │ • config    │  │ • Workers   │        │
│  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘        │
│         │                │                │                │                │
│         └────────────────┴────────────────┴────────────────┘                │
│                                   │                                         │
│                          ┌────────┴────────┐                                │
│                          │   shared/core   │                                │
│                          │                 │                                │
│                          │ • Config parser │                                │
│                          │ • Forge clients │                                │
│                          │ • Protocol defs │                                │
│                          │ • Job types     │                                │
│                          └─────────────────┘                                │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Component Breakdown

### 1. Server (`cinch server`)

The control plane. Runs as a long-lived process.

**Responsibilities:**
- Receive webhooks from forges (GitHub, GitLab, Forgejo, etc.)
- Parse `.cinch.yaml` (or `.toml`/`.json`) from repository
- Dispatch jobs to connected workers
- Post status checks back to forges
- Serve web UI
- Manage authentication/tokens

**Does NOT:**
- Execute builds (that's the worker's job)
- Store secrets (secrets live on worker machines)
- Store artifacts (use your own S3 if needed)

### 2. Worker (`cinch worker`)

The build executor. Runs on user's hardware.

**Responsibilities:**
- Connect to server via WebSocket (outbound connection)
- Receive job assignments
- Clone repositories (using short-lived tokens)
- Run builds in containers (default) or bare metal (opt-in)
- Auto-detect and use project's devcontainer/Dockerfile
- Manage persistent cache volumes for warm builds
- Stream logs back to server
- Report success/failure

**Does NOT:**
- Expose any network ports (connects outbound only)
- Send secrets to server (env vars stay local)

**Containerization (see 09-containerization.md):**
- Default: Auto-detect container (devcontainer > Dockerfile > default image)
- Persistent volume mounts for npm, cargo, pip, go caches
- Artifact extraction via output directory mount
- `container: none` in config for bare metal escape hatch

### 3. CLI (`cinch run`, `cinch status`, etc.)

Developer tools for local use.

**Commands:**
- `cinch run "cmd"` - Run a command as if CI triggered it (local testing)
- `cinch status` - Show build status for current repo
- `cinch logs <id>` - Stream logs from a job
- `cinch config` - Validate `cinch.toml`

### 4. Web UI (embedded)

Single-page app embedded in the binary. **The API is the product** - the bundled UI is just a convenience.

**Features:**
- Job list (with filters)
- Real-time log streaming
- Worker status
- Repo configuration

**Tech:**
- Static assets embedded via `go:embed` (Vite builds to web/dist/)
- Minimal React 18 + Vite (no state libs, no component libs)
- ansi-to-html for log rendering
- Communicates via same HTTP/WebSocket API as CLI

**Philosophy:**
The web UI is just another API client. Someone could build their own frontend (Vue, htmx, terminal UI, whatever) and it would work identically. We'd encourage that. The embedded UI exists so the product works out of the box, not because it's special.

## Data Flow

### Build Trigger Flow

```
1. Developer pushes to GitHub
         │
         ▼
2. GitHub sends webhook to cinch server
         │
         ▼
3. Server fetches .cinch.yaml from repo
         │
         ▼
4. Server creates Job record(s) (status: pending)
   - If workers: [linux-amd64, linux-arm64], creates one job per label
         │
         ▼
5. Server posts "pending" status to GitHub (one per worker label)
         │
         ▼
6. Server dispatches job(s) to available workers via WebSocket
   - Each job goes to least-loaded worker matching its label
         │
         ▼
7. Worker clones repo using short-lived token
         │
         ▼
8. Worker starts service containers, waits for healthy
         │
         ▼
9. Worker executes command in build container, streams logs to server
         │
         ▼
10. Worker reports success/failure
          │
          ▼
11. Server posts final status to GitHub (cinch/linux-amd64 ✓, cinch/linux-arm64 ✓)
```

### Fan-Out (Multi-Platform Builds)

When config specifies multiple workers:

```yaml
workers: [linux-amd64, linux-arm64, darwin-arm64]
```

The server creates N independent jobs, one per label. Each job:
- Has its own status check on the forge (`cinch/linux-amd64`, etc.)
- Runs on the least-loaded worker matching that label
- Succeeds or fails independently

This enables cross-platform testing without complex matrix syntax.

### Connection Model

```
Workers always connect OUT to server (NAT-friendly):

┌──────────────┐                    ┌──────────────┐
│   Worker 1   │────WebSocket──────►│              │
└──────────────┘                    │              │
                                    │    Server    │◄──── Webhooks from forges
┌──────────────┐                    │              │
│   Worker 2   │────WebSocket──────►│              │
└──────────────┘                    └──────────────┘
                                           │
┌──────────────┐                           │
│   Worker N   │────WebSocket─────────────►│
└──────────────┘
```

## Technology Choices

| Component | Choice | Rationale |
|-----------|--------|-----------|
| Language | Go | Single binary, cross-compile, good concurrency |
| Database | SQLite (self-hosted) / Postgres (hosted) | SQLite needs zero config, Postgres scales |
| Protocol | WebSocket + JSON | Bidirectional, firewall-friendly, debuggable |
| Web UI | Embedded static files | No separate deployment, works offline |
| Config | YAML, TOML, JSON | All common formats supported |
| Containers | Docker (default) | Reproducible builds, devcontainer support |

## Directory Structure

```
cinch/
├── cmd/
│   └── cinch/
│       └── main.go           # Entry point, subcommand routing
├── internal/
│   ├── server/
│   │   ├── server.go         # HTTP server setup
│   │   ├── webhook.go        # Webhook handlers per forge
│   │   ├── api.go            # REST API handlers
│   │   ├── ws.go             # WebSocket hub for workers
│   │   └── dispatch.go       # Job dispatch logic
│   ├── worker/
│   │   ├── worker.go         # Worker main loop
│   │   ├── executor.go       # Command execution
│   │   ├── clone.go          # Git clone logic
│   │   ├── stream.go         # Log streaming
│   │   └── container/
│   │       ├── container.go  # Container runtime interface
│   │       ├── docker.go     # Docker implementation
│   │       ├── devcontainer.go # Devcontainer detection/building
│   │       ├── cache.go      # Cache volume management
│   │       └── artifacts.go  # Artifact extraction
│   ├── cli/
│   │   ├── run.go            # cinch run
│   │   ├── status.go         # cinch status
│   │   └── logs.go           # cinch logs
│   ├── forge/
│   │   ├── forge.go          # Interface definition
│   │   ├── github.go         # GitHub implementation
│   │   ├── gitlab.go         # GitLab implementation
│   │   ├── forgejo.go        # Forgejo/Gitea implementation
│   │   └── bitbucket.go      # Bitbucket implementation
│   ├── config/
│   │   └── config.go         # cinch.toml parsing
│   ├── protocol/
│   │   └── protocol.go       # WebSocket message types
│   └── db/
│       ├── db.go             # Database interface
│       ├── sqlite.go         # SQLite implementation
│       └── postgres.go       # Postgres implementation
├── web/
│   ├── static/               # Embedded static files
│   └── templates/            # HTML templates (if any)
├── design/                   # This folder
└── README.md
```

## Scaling Considerations

Cinch uses **vertical scaling** for simplicity. See `design/11-scaling.md` for details.

**Self-hosted (SQLite):**
- Single server instance
- Multiple workers connect to it
- Good for: individual developers, small teams, homelab

**Hosted (Postgres):**
- Single server instance + Fly Postgres
- Scale up machines as needed
- Good for: SaaS, larger teams

**Capacity:** Way more than you'd expect. A single beefy server handles 100k+ workers, millions of jobs. See `design/11-scaling.md`.

Horizontal scaling is explicitly unplanned until proven necessary.
