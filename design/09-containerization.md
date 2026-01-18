# Containerization Deep Dive

## The Current Gap

The design docs treat containerization as an afterthought: a `--docker` flag on the worker for "untrusted code." But this is backwards.

**Reality check:** Most CI users don't want builds running on bare metal. They want:
- Reproducibility (same environment every time)
- Isolation (builds can't mess up the host)
- Clean state (no "works on my machine" drift)

The README pitch about "your cache is already warm" is compelling for certain workflows, but **containerization should be the default**, with bare metal as the escape hatch for those who truly want it.

## The Devcontainer Insight

Here's the killer feature hiding in plain sight:

**Most projects already have a `.devcontainer/` or `Dockerfile` that defines their build environment.**

Instead of asking users to:
1. Configure their CI environment separately
2. Install tools on the worker machine
3. Hope the worker matches their dev environment

We can just:
1. Use the devcontainer/Dockerfile that's already in their repo
2. Build once, reuse across builds
3. Guarantee "if it builds locally in the devcontainer, it builds in CI"

```yaml
# .cinch.yaml - the dream
command: make ci

# That's it. We auto-detect and use .devcontainer/
```

## Execution Modes

### Mode 1: Auto-Container (Default)

```yaml
# .cinch.yaml
command: make ci
# container: auto  (implicit default)
```

Discovery order:
1. `.devcontainer/devcontainer.json` → use that image/Dockerfile
2. `.devcontainer/Dockerfile` → build and use
3. `Dockerfile` or `Dockerfile.ci` in repo root → build and use
4. Fall back to `cinch-builder:latest` (our minimal image)

### Mode 2: Explicit Container

```yaml
# .cinch.yaml
command: npm test

container:
  image: node:20-alpine
  # OR
  dockerfile: ./docker/Dockerfile.ci
```

### Mode 3: Bare Metal (Opt-in)

```yaml
# .cinch.yaml
command: make ci

container: none  # Explicit "run on host"
```

Only use this when you:
- Need GPU access that Docker can't provide
- Have massive caches that can't be mounted
- Trust the code completely

## The Caching Problem (And Solutions)

### Problem: Containers = Cold Cache Every Time?

If every build starts with a fresh container, we lose the "warm cache" advantage. `npm install` downloads everything. `cargo build` recompiles from scratch. This is the GitHub Actions experience we're trying to avoid.

### Solution: Persistent Cache Volumes

Workers maintain persistent volumes that get mounted into every container:

```
Worker host filesystem:
~/.cinch/
├── cache/
│   ├── npm/           # Mounted as ~/.npm in container
│   ├── cargo/         # Mounted as ~/.cargo in container
│   ├── pip/           # Mounted as ~/.cache/pip in container
│   ├── go/            # Mounted as ~/go in container
│   └── custom/        # User-defined caches
├── images/            # Cached container images
└── builds/            # Working directories (ephemeral)
```

### How It Works

```go
// internal/worker/container/mounts.go

// Standard cache mounts - auto-detected
var defaultCacheMounts = []Mount{
    // Node.js
    {Host: "~/.cinch/cache/npm", Container: "/root/.npm"},
    {Host: "~/.cinch/cache/npm", Container: "/home/node/.npm"},

    // Rust
    {Host: "~/.cinch/cache/cargo/registry", Container: "/root/.cargo/registry"},
    {Host: "~/.cinch/cache/cargo/git", Container: "/root/.cargo/git"},

    // Go
    {Host: "~/.cinch/cache/go/pkg", Container: "/go/pkg"},

    // Python
    {Host: "~/.cinch/cache/pip", Container: "/root/.cache/pip"},

    // Generic
    {Host: "~/.cinch/cache/ccache", Container: "/root/.ccache"},
}
```

### User-Defined Caches

```yaml
# .cinch.yaml
command: make ci

cache:
  # Named caches that persist across builds
  - name: node_modules
    path: ./node_modules
  - name: build-output
    path: ./dist
```

These get stored at `~/.cinch/cache/custom/{repo-hash}/{cache-name}/` on the worker.

### Result

First build: `npm install` downloads everything (cached for next time)
Second build: `npm install` is instant (node_modules preserved)

**We get the best of both worlds:** container isolation + warm caches.

## Container Reuse Strategy

### Question: Fresh Container Per Build, or Reuse?

**Option A: Fresh container every time**
- Pros: Clean state guaranteed, no drift
- Cons: Container startup overhead (1-5 seconds)

**Option B: Keep container running, exec into it**
- Pros: Instant job start
- Cons: State can leak between builds, cleanup complexity

**Recommendation: Fresh container with image caching**

```
Build flow:
1. Check if container image exists locally → use it
2. If not, pull/build image → cache it
3. Start fresh container with cache mounts
4. Run command
5. Stop and remove container (volumes persist)
```

The image caching means "fresh container" is fast (sub-second after first build), while volumes preserve caches across builds.

### Devcontainer Image Caching

For repos with devcontainers:

```go
func getContainerImage(repo *Repo, commit string) (string, error) {
    // Hash the devcontainer config
    configPath := filepath.Join(repo.Path, ".devcontainer/devcontainer.json")
    configHash := hashFile(configPath)

    imageName := fmt.Sprintf("cinch-dev-%s:%s", repo.ID, configHash[:12])

    // Check if we already built this
    if imageExists(imageName) {
        return imageName, nil
    }

    // Build and tag
    return buildDevcontainer(repo.Path, imageName)
}
```

The image is rebuilt only when devcontainer config changes, not every build.

## Artifact Extraction

### Problem: How Do We Get Stuff Out of the Container?

Build artifacts (binaries, test reports, coverage) are created inside the container. We need to get them out.

### Solution 1: Output Directory Mount

```yaml
# .cinch.yaml
command: make ci

output:
  path: ./artifacts
```

```go
// Mount strategy
mounts := []Mount{
    // Source code (read-only where possible)
    {Host: buildDir, Container: "/workspace", ReadOnly: false},

    // Output directory (survives container death)
    {Host: outputDir, Container: "/workspace/artifacts", ReadOnly: false},
}
```

After build completes, `outputDir` on host contains whatever the build put in `./artifacts`.

### Solution 2: Explicit Copy-Out

```yaml
# .cinch.yaml
command: make ci

artifacts:
  - path: ./dist/app
    name: app-binary
  - path: ./coverage.xml
    name: coverage-report
```

```go
func extractArtifacts(containerID string, artifacts []Artifact) error {
    for _, a := range artifacts {
        // docker cp container:/workspace/path /host/path
        cmd := exec.Command("docker", "cp",
            fmt.Sprintf("%s:/workspace/%s", containerID, a.Path),
            filepath.Join(outputDir, a.Name),
        )
        if err := cmd.Run(); err != nil {
            log.Warnf("artifact %s not found", a.Path)
        }
    }
    return nil
}
```

### Solution 3: Logs Are Often Enough

For most CI, you don't need artifacts extracted. The logs tell you pass/fail.

```yaml
# .cinch.yaml
command: make test  # Just need exit code + logs
```

Only configure `output` or `artifacts` if you actually need the files.

## Build Log Access Inside Container

```go
func runContainerBuild(job *Job) error {
    // Create container
    containerID := createContainer(job.Image, job.Mounts)
    defer removeContainer(containerID)

    // Start and attach to stdout/stderr
    execID := createExec(containerID, job.Command)
    stdout, stderr := attachExec(execID)

    // Stream logs to server
    go streamLogs(job.ID, stdout, "stdout")
    go streamLogs(job.ID, stderr, "stderr")

    // Wait for completion
    exitCode := waitExec(execID)
    return job.Complete(exitCode)
}
```

## Detailed Container Execution Flow

```
Webhook received
       │
       ▼
Server creates Job (status: pending)
       │
       ▼
Server dispatches to Worker
       │
       ▼
Worker receives Job
       │
       ▼
┌──────────────────────────────────────────────────┐
│ 1. Clone repo to ~/.cinch/builds/{job-id}/       │
│                                                  │
│ 2. Determine container strategy:                 │
│    - Check .devcontainer/ → use/build that image │
│    - Check container: in .cinch.yaml             │
│    - Fall back to cinch-builder:latest           │
│                                                  │
│ 3. Prepare mounts:                               │
│    - Build directory → /workspace                │
│    - Cache directories → standard locations      │
│    - Output directory → /workspace/output        │
│                                                  │
│ 4. Start container:                              │
│    docker run --rm \                             │
│      -v build:/workspace \                       │
│      -v npm-cache:/root/.npm \                   │
│      -v cargo-cache:/root/.cargo \              │
│      -w /workspace \                             │
│      --network host \                            │ (or restricted)
│      $IMAGE \                                    │
│      sh -c "$COMMAND"                            │
│                                                  │
│ 5. Stream stdout/stderr to server                │
│                                                  │
│ 6. Container exits                               │
│                                                  │
│ 7. Extract artifacts (if configured)             │
│                                                  │
│ 8. Report exit code to server                    │
│                                                  │
│ 9. Cleanup build directory (keep caches)         │
└──────────────────────────────────────────────────┘
       │
       ▼
Server updates Job status
       │
       ▼
Server posts status to forge
```

## Container Runtime Options

### Docker (Default)

Most workers will have Docker. It's the obvious choice.

```go
type DockerRuntime struct {
    client *docker.Client
}

func (d *DockerRuntime) Run(job *Job) error {
    // Standard docker run flow
}
```

### Podman (Rootless Alternative)

Some users prefer rootless containers:

```yaml
# Worker config
runtime: podman
```

Podman is API-compatible, so same code works.

### Bubblewrap (Minimal Isolation)

For Linux users who don't want full container overhead:

```yaml
# Worker config
runtime: bubblewrap
```

Lighter weight, but Linux-only and less reproducible.

### None (Bare Metal)

```yaml
# Worker config
runtime: none  # Run directly on host
```

Maximum speed, no isolation. You trust the code.

## Security Considerations

### Network Access

```yaml
# .cinch.yaml
container:
  network: none      # No network access (safest)
  network: host      # Full network access (most flexible)
  network: internal  # Access to internal services only
```

Default: `host` (we want npm install to work)

For untrusted code: `none` (with pre-populated caches)

### Resource Limits

```yaml
# .cinch.yaml
container:
  memory: 4g
  cpus: 2
  timeout: 30m
```

```go
containerConfig := &container.Config{
    Image: image,
}
hostConfig := &container.HostConfig{
    Resources: container.Resources{
        Memory:   4 * 1024 * 1024 * 1024, // 4GB
        NanoCPUs: 2 * 1000000000,          // 2 CPUs
    },
}
```

### User Namespaces

Run as non-root inside container:

```go
hostConfig := &container.HostConfig{
    UsernsMode: "host",  // or remap to non-root
}
```

## The Warm Cache Reconciliation

The README says "Your cache is already warm." With containers, is this still true?

**Yes, with our approach:**

| What | GitHub Actions | cinch (bare metal) | cinch (containerized) |
|------|---------------|-------------------|----------------------|
| npm cache | Cold. Upload/download artifact | Hot on disk | Hot via volume mount |
| Docker images | Cold. Pull every time | Hot on disk | Hot on disk |
| node_modules | Cold without actions/cache | Hot on disk | Hot via cache volume |
| First build | Slow (everything cold) | Fast | Medium (image build) |
| Subsequent builds | Medium (cache restore) | Fast | Fast |

**The key insight:** Volume mounts give us warm caches inside containers. We're not throwing away the "warm cache" advantage—we're extending it into isolated environments.

## Config Schema Update

```yaml
# .cinch.yaml - full container options

command: make ci

# Container settings
container:
  # What to run in
  image: node:20-alpine           # Explicit image
  # OR
  dockerfile: ./Dockerfile.ci      # Build from this
  # OR
  devcontainer: true              # Use .devcontainer/ (default if exists)
  # OR
  auto: true                      # Auto-detect (default)

  # Resource limits
  memory: 4g
  cpus: 2

  # Network
  network: host                   # host | none | internal

  # Extra mounts
  mounts:
    - /var/run/docker.sock:/var/run/docker.sock  # Docker-in-docker

# Persistent caches (mounted into container)
cache:
  - name: dependencies
    path: ./node_modules
  - name: build-cache
    path: ./.next/cache

# Files to extract after build
artifacts:
  - path: ./dist
    name: build-output
  - path: ./coverage/lcov.info
    name: coverage

# Or: explicit "no container"
# container: none
```

## Implementation Priority

### Phase 1 (v0.1): Basic Docker Support
- [ ] Docker runtime implementation
- [ ] Volume mounts for standard caches (npm, cargo, pip, go)
- [ ] Basic devcontainer detection (just use the image)
- [ ] `container: none` for bare metal

### Phase 2 (v0.2): Full Devcontainer Support
- [ ] Parse devcontainer.json properly (features, mounts, etc.)
- [ ] Build devcontainer images with caching
- [ ] Custom cache paths from config
- [ ] Artifact extraction

### Phase 3 (v0.3): Advanced Features
- [ ] Podman support
- [ ] Resource limits
- [ ] Network isolation modes
- [ ] Bubblewrap for Linux minimal isolation

## Open Questions

1. **Should we ship a default builder image?**
   - Pro: Works out of the box
   - Con: One more thing to maintain
   - Recommendation: Yes, a minimal `cinch-builder` with common tools (git, make, curl, jq)

2. **How do we handle Docker-in-Docker?**
   - Some builds need to build Docker images
   - Mount the socket? Use sysbox? Kaniko?
   - Recommendation: Mount socket by default (optional), warn about security

3. **What about Windows containers?**
   - Reality: Most CI is Linux
   - Windows containers are niche
   - Recommendation: Linux containers only for v0.1

4. **Should cache volumes be per-repo or shared?**
   - Per-repo: More isolation, more disk usage
   - Shared: Faster first builds, potential conflicts
   - Recommendation: Shared by default, per-repo option

## Summary

- **Default behavior:** Auto-detect container (devcontainer > Dockerfile > default image)
- **Caching:** Persistent volumes mounted into containers
- **Artifacts:** Mount output directory or explicit copy-out
- **Escape hatch:** `container: none` for bare metal
- **Result:** Reproducible isolated builds with warm caches

The README pitch evolves from "your cache is already warm" to **"your cache is already warm, and your builds are isolated and reproducible."**
