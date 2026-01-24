# Containerization

## Philosophy

**The Dockerfile is the config.**

Cinch's job is simple:
1. Figure out what container to use
2. Run your command inside it

That's it. No `memory:` limits. No `cache:` directories. No `artifacts:` extraction. If you need something in your CI environment, put it in your Dockerfile.

## Container Resolution

Cinch resolves the container in priority order:

```yaml
# .cinch.yaml

build: make test

# Container options (pick one, or use defaults):
image: node:20                                    # Pre-built image, no build step
dockerfile: docker/Dockerfile.ci                  # Build this Dockerfile
devcontainer: ./.devcontainer/devcontainer.json   # Parse this JSON (DEFAULT)
container: none                                   # Bare metal escape hatch
```

| Option | What it does |
|--------|--------------|
| `image:` | Use this pre-built image directly (e.g., `node:20`, `python:3.11`) |
| `dockerfile:` | Build this Dockerfile and use the resulting image |
| `devcontainer:` | Parse this JSON file for `image` or `build.dockerfile` |
| `container: none` | Run directly on host (requires worker to allow it) |

**Default behavior:** `devcontainer: ./.devcontainer/devcontainer.json`

If you specify nothing, we look for a devcontainer.json and parse it. This makes the "magic" explicit and shut-offable:

```yaml
# Disable devcontainer auto-detection, use ubuntu:22.04
devcontainer: false
```

## The 80% Case

Most projects either have a devcontainer or just need a standard base image:

```yaml
# Project with devcontainer - zero container config needed
build: make test
```

```yaml
# Project with no containers - just specify the image
build: npm test
image: node:20
```

Done. No Dockerfile needed for simple cases.

## The 20% Case: Custom Dockerfile

For projects that need a CI-specific environment:

```dockerfile
# docker/Dockerfile.ci
FROM ubuntu:22.04

RUN apt-get update && apt-get install -y \
    build-essential \
    nodejs \
    npm

# All your CI "config" lives here.
```

```yaml
# .cinch.yaml
build: make test
dockerfile: docker/Dockerfile.ci
```

**All complex configuration belongs in the Dockerfile.** Need specific Node version? Put it in the Dockerfile. Need postgres client? Put it in the Dockerfile.

## devcontainer.json Parsing

When `devcontainer:` points to a JSON file (the default), we parse it for:

```json
// Option A: Pre-built image
{"image": "node:20"}

// Option B: Build a Dockerfile
{"build": {"dockerfile": "Dockerfile"}}

// Option C: Legacy field
{"dockerFile": "Dockerfile"}
```

**What we support:**
- `image` → use directly
- `build.dockerfile` → build it (relative to devcontainer.json location)
- `dockerFile` → legacy field, same as above

**What we ignore:**
- `features` - requires devcontainer CLI
- `postCreateCommand` - interactive setup
- `mounts`, `runArgs`, `capabilities` - too complex
- Everything else

If the devcontainer.json uses features or other unsupported options, users should either:
1. Add a Dockerfile that bakes in what they need
2. Specify `image:` or `dockerfile:` explicitly in .cinch.yaml

## Resolution Flow

```
.cinch.yaml parsed
       │
       ▼
┌─────────────────────────────────────────────────────┐
│ image: specified?                                   │
│   YES → use that image directly                     │
│                                                     │
│ dockerfile: specified?                              │
│   YES → build that Dockerfile                       │
│                                                     │
│ container: none?                                    │
│   YES → run bare metal (if worker allows)           │
│                                                     │
│ devcontainer: false?                                │
│   YES → use ubuntu:22.04                            │
│                                                     │
│ devcontainer: path (default .devcontainer/...)      │
│   File exists?                                      │
│     YES → parse JSON:                               │
│       - has "image" → use it                        │
│       - has "build.dockerfile" → build it           │
│       - has "dockerFile" → build it                 │
│       - else → error: can't determine image         │
│     NO → use ubuntu:22.04                           │
└─────────────────────────────────────────────────────┘
```

## Artifacts: Your Problem

**Cinch does not extract artifacts from containers.**

Your build command publishes its own artifacts:

```yaml
release: make release
```

```makefile
release:
    go build -o myapp ./cmd/myapp
    gh release create $(VERSION) myapp  # YOU upload
```

For checks/PRs, you just need pass/fail:

```yaml
build: make test  # Exit code 0 = green, non-zero = red
```

## Caching

Caching is handled at the worker level via Docker's build cache. No repo config needed.

For faster dependency installation, use standard Docker layer caching:

```dockerfile
FROM node:20

COPY package*.json ./
RUN npm ci

COPY . .
```

## Container Runtime Detection

### Fail, Don't Fall Back

If no container runtime is available, we **fail loudly**. We do NOT silently run on bare metal.

### Supported Runtimes

| Runtime | Detection |
|---------|-----------|
| Docker Desktop | `docker info` |
| Colima | `docker info` |
| OrbStack | `docker info` |
| Podman | `podman info` |
| Rancher Desktop | `docker info` |

All Docker-compatible runtimes expose the `docker` CLI.

### Worker Startup

```
$ cinch worker start
cinch worker v0.1.0
  Runtime: docker (colima) v28.4.0

Connecting to server...
```

```
$ cinch worker start  (no runtime)
ERROR: No container runtime found.

Install one of:
  macOS:   brew install colima && colima start
  Linux:   apt install docker.io
  Windows: Install Docker Desktop

Or use --bare-metal if you know what you're doing.
```

## Bare Metal Escape Hatch

```yaml
build: make test
container: none
```

Requires the worker to allow it:

```yaml
# Worker config
allow_bare_metal: true
```

Use cases:
- GPU access
- Massive local caches
- You own the worker and trust all code

## Complete Config Reference

```yaml
# .cinch.yaml - complete container options

build: make test
release: make release

# Container resolution (first match wins):
image: node:20                                    # Pre-built image
dockerfile: path/to/Dockerfile                    # Build this Dockerfile
devcontainer: ./.devcontainer/devcontainer.json   # Parse JSON (DEFAULT)
container: none                                   # Bare metal

# To disable devcontainer auto-detection:
devcontainer: false
```

**That's it.** Five optional keys for container config. Everything else goes in your Dockerfile or Makefile.

## What We Don't Support

Intentionally omitted:

- **`memory:`, `cpus:` limits** - Worker operator concern
- **`cache:` directories** - Use Docker layer caching
- **`artifacts:` extraction** - Your Makefile publishes
- **`mounts:` config** - Put it in your Dockerfile
- **devcontainer features** - Use a Dockerfile instead

## Implementation Priority

### Phase 1 (v0.1): It Works
- [x] Docker runtime (shells to `docker` CLI)
- [x] Basic devcontainer.json parsing (image, build.dockerfile)
- [x] Mount workspace, run command, stream logs
- [x] `image:` config option
- [x] `dockerfile:` config option
- [x] `devcontainer: false` to disable auto-detect
- [x] `container: none` bare metal escape hatch
- [ ] Runtime detection with helpful errors

### Phase 2 (v0.2): Polish
- [ ] Podman support
- [ ] Better build caching (BuildKit)

### Not Planned
- devcontainer features
- Artifact extraction
- Per-job resource limits
- Custom volume mounts
