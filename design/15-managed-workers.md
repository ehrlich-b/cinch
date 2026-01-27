# Managed Workers (Post-v1)

## Overview

Cinch offers managed workers powered by Fly.io for users who don't want to run their own hardware. Workers spin up on demand, run the build, and shut down. Users pay only for actual build time, billed per second.

**Target:** Teams who want zero infrastructure. Push code, builds run, pay for what you use.

## The Architecture

### Key Insight: Your Dockerfile Becomes a Fly Image

Users have Dockerfiles (their CI environment). We:
1. Append the cinch harness to their Dockerfile
2. Build it once → push to Fly's registry
3. Boot ephemeral machines from that image for each job
4. Machine runs `cinch worker --one-shot --bare-metal` (we're already in Docker!)
5. Machine auto-destroys when done, billing stops

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ First build (or when Dockerfile changes)                                     │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   User's Dockerfile              What we build                              │
│   ─────────────────              ──────────────                             │
│   FROM golang:1.24               FROM golang:1.24                           │
│   RUN apt-get install nodejs     RUN apt-get install nodejs                 │
│   RUN apt-get install git        RUN apt-get install git                    │
│   ...                            ...                                        │
│                                                                              │
│                                  # === Appended by Cinch ===                │
│                                  ADD https://cinch.sh/.../cinch /cinch      │
│                                  RUN chmod +x /cinch                        │
│                                  ENTRYPOINT ["/cinch", "worker",            │
│                                              "--one-shot", "--bare-metal"]  │
│                                                                              │
│   → Push to: registry.fly.io/cinch-workers:u_abc-repo_123-a1b2c3           │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│ Every job                                                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   1. Create machine via Fly API:                                            │
│      - image: registry.fly.io/cinch-workers:u_abc-repo_123-a1b2c3          │
│      - auto_destroy: true                                                   │
│      - env: CINCH_JOB_ID, CINCH_REPO_URL, CINCH_COMMAND, etc.              │
│                                                                              │
│   2. Machine boots (~2-3s), runs cinch harness:                             │
│      a. Connect to control plane (for log streaming)                        │
│      b. Restore caches from R2 → extract to local paths                    │
│      c. git clone + git checkout                                            │
│      d. Run command (bare metal - we're already in their Docker image!)    │
│      e. Save changed caches to R2                                           │
│      f. Report result, exit                                                 │
│                                                                              │
│   3. Machine exits → auto_destroy kicks in → machine deleted                │
│      Billing stops immediately.                                             │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### No Docker-in-Docker

We're not running Docker inside Fly. We're running the user's Dockerfile AS the Fly machine. The cinch harness runs bare-metal inside their pre-built environment.

```
Traditional CI:                    Cinch on Fly:
┌─────────────────────────┐       ┌─────────────────────────┐
│ VM                      │       │ Fly Machine             │
│  ┌───────────────────┐  │       │ (IS the user's image)   │
│  │ Docker            │  │       │                         │
│  │  ┌─────────────┐  │  │       │  cinch harness          │
│  │  │ User image  │  │  │       │    ↓                    │
│  │  │  ┌───────┐  │  │  │       │  git clone              │
│  │  │  │ build │  │  │  │       │    ↓                    │
│  │  │  └───────┘  │  │  │       │  make check ← bare metal│
│  │  └─────────────┘  │  │       │                         │
│  └───────────────────┘  │       └─────────────────────────┘
└─────────────────────────┘
     Nested, complex              Flat, simple
```

## Fly Configuration

### Single App, Multiple Images

We use ONE Fly app for all managed workers (or one per region):

```
App: cinch-workers
Registry: registry.fly.io/cinch-workers

Images (tagged per repo):
  :u_abc123-repo_456-a1b2c3    # User abc123's repo 456, Dockerfile hash a1b2c3
  :u_abc123-repo_789-d4e5f6    # Same user, different repo
  :u_def456-repo_123-g7h8i9    # Different user
  ...
```

**Why one app?**
- Apps are free, machines cost money
- Simpler billing and management
- No limit concerns (1000 apps vs 1 app with 1000 image tags)
- Image tags are cheap, apps have more overhead

### Creating a Machine (Fly API)

```go
// POST https://api.machines.dev/v1/apps/cinch-workers/machines
func (m *ManagedWorker) CreateJobMachine(ctx context.Context, job *Job) (*Machine, error) {
    req := fly.CreateMachineRequest{
        Config: fly.MachineConfig{
            Image: fmt.Sprintf("registry.fly.io/cinch-workers:%s-%s-%s",
                job.UserID, job.RepoID, job.DockerfileHash),

            // THIS IS THE KEY: machine destroys itself when process exits
            AutoDestroy: true,

            Guest: fly.MachineGuest{
                CPUKind:  sizes[job.Size].CPUKind,
                CPUs:     sizes[job.Size].CPUs,
                MemoryMB: sizes[job.Size].MemoryMB,
            },

            Env: map[string]string{
                // Job details
                "CINCH_JOB_ID":      job.ID,
                "CINCH_REPO_URL":    job.CloneURL, // includes token
                "CINCH_COMMIT":      job.Commit,
                "CINCH_COMMAND":     job.Command,

                // Control plane connection
                "CINCH_CONTROL_PLANE": "wss://cinch.sh/worker",
                "CINCH_JOB_TOKEN":     job.Token,

                // Cache volume mount point
                "CINCH_CACHE_VOLUME": "/vol/cache",

                // Standard CI env vars
                "CI":    "true",
                "CINCH": "true",
            },

            // Mount the persistent cache volume
            Mounts: []fly.MachineMount{{
                Volume: cacheVolume.ID, // Persistent volume for this repo
                Path:   "/vol/cache",
            }},

            // No restart - run once and die
            Restart: fly.RestartConfig{
                Policy: "no",
            },
        },
        Region: selectRegion(job),
    }

    return m.fly.CreateMachine(ctx, "cinch-workers", req)
}
```

### Machine Lifecycle

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Machine Lifecycle                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   Created ──→ Starting ──→ Started ──→ Running ──→ Stopping ──→ Destroyed  │
│     │           │            │           │            │            │        │
│     │        ~2-3s           │        job runs      exit         gone      │
│     │                        │                       │                      │
│     └────────────────────────┴───────────────────────┘                      │
│                    BILLING (per second)              │                      │
│                                                      │                      │
│                                              auto_destroy                   │
│                                              kicks in here                  │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

With `auto_destroy: true`:
- When the cinch harness exits, machine is **destroyed** (not just stopped)
- Billing stops completely
- No rootfs storage charges (stopped machines cost $0.15/GB/month, destroyed = $0)

Source: [Fly.io Machines API](https://fly.io/docs/machines/api/machines-resource/), [Fly.io Pricing](https://fly.io/docs/about/pricing/)

### Cache Volumes

Caches live on **persistent Fly volumes** that survive between jobs. No R2 involved.

```
Fly Volume: /vol/cache (persistent per repo, 10GB included in Pro)
    │
    ├── go-mod/
    ├── go-build/
    ├── golangci-lint/
    └── npm/

Symlinks (created by harness at job start):
    /go/pkg/mod           → /vol/cache/go-mod
    /root/.cache/go-build → /vol/cache/go-build
    /root/.npm            → /vol/cache/npm
```

**Volume management:**
```go
func (m *ManagedWorker) getOrCreateCacheVolume(ctx context.Context, repoID string) (*Volume, error) {
    volumeName := fmt.Sprintf("cache-%s", repoID)

    // Check if volume exists
    vol, err := m.fly.GetVolume(ctx, volumeName)
    if err == nil {
        // Check if volume needs expansion (>80% full)
        if vol.UsedGB > vol.SizeGB * 0.8 && vol.SizeGB < m.getMaxVolumeSize(repoID) {
            m.fly.ExtendVolume(ctx, vol.ID, vol.SizeGB + 2) // Grow by 2GB
        }
        return vol, nil
    }

    // Create new volume - start small (1GB), grow as needed
    return m.fly.CreateVolume(ctx, fly.CreateVolumeRequest{
        Name:   volumeName,
        Region: m.defaultRegion,
        SizeGB: 1, // Start small!
    })
}

func (m *ManagedWorker) getMaxVolumeSize(repoID string) int {
    // 10GB included in Pro, can buy more with paygo
    account := m.db.GetAccountForRepo(repoID)
    return account.CacheQuotaGB // Default 10, can be higher with paygo
}
```

**Volume sizing:**
- Start at **1GB** (most repos need less)
- Auto-expand by 2GB when >80% full
- Cap at 10GB (included in Pro), paygo beyond that
- Volumes expire after 30 days of no use

**Costs:**
- Typical repo: 1-3GB = $0.15-$0.45/mo
- Worst case (maxed out): 10GB = $1.50/mo
- Larger caches: paygo at $0.15/GB/mo to user

**Key insight:** Volume persists between jobs. Second job has instant warm cache—no download, no R2.

### Building the Image

When a repo is connected (or Dockerfile changes):

```go
func (m *ManagedWorker) BuildRepoImage(ctx context.Context, repo *Repo) error {
    // 1. Fetch their Dockerfile
    dockerfile, err := m.fetchDockerfile(ctx, repo)
    if err != nil {
        return err
    }

    // 2. Append cinch harness
    dockerfile = appendCinchHarness(dockerfile)

    // 3. Compute hash for cache key
    hash := sha256Short(dockerfile)
    tag := fmt.Sprintf("%s-%s-%s", repo.UserID, repo.ID, hash)

    // 4. Check if already built
    if m.imageExists(ctx, "cinch-workers", tag) {
        return nil // Already built, skip
    }

    // 5. Build using Fly's remote builder
    return m.fly.Build(ctx, fly.BuildRequest{
        App:        "cinch-workers",
        Dockerfile: dockerfile,
        Tag:        tag,
    })
}

func appendCinchHarness(dockerfile string) string {
    return dockerfile + `

# === Cinch Harness (auto-appended) ===
ARG CINCH_VERSION=0.1.0
ADD https://cinch.sh/releases/${CINCH_VERSION}/cinch-linux-amd64 /usr/local/bin/cinch
RUN chmod +x /usr/local/bin/cinch
ENTRYPOINT ["/usr/local/bin/cinch", "worker", "--one-shot", "--bare-metal"]
`
}
```

### Dockerfile Requirements

User's Dockerfile MUST include:
- `git` - for cloning the repo

That's it. Caches live on Fly volumes (no download needed).

## The Harness: `cinch worker --one-shot --bare-metal`

```go
// cmd/cinch/worker.go

func runWorkerOneShot(ctx context.Context) error {
    // Read job config from environment
    job := JobFromEnv()
    cacheVolume := os.Getenv("CINCH_CACHE_VOLUME") // /vol/cache

    // Connect to control plane for log streaming
    conn, err := connectControlPlane(job.ControlPlane, job.Token)
    if err != nil {
        return err
    }
    defer conn.Close()

    // 1. Clone repo first (need to detect caches based on files)
    workDir := "/workspace"
    if err := gitClone(job.RepoURL, job.Commit, workDir); err != nil {
        return reportFailure(conn, job, err)
    }

    // 2. Detect and setup cache symlinks
    //    Volume is already mounted at /vol/cache (persistent between jobs)
    caches := DetectCaches(workDir)
    if err := setupCacheSymlinks(cacheVolume, caches); err != nil {
        log.Warn("cache symlink setup failed: %v", err)
        // Continue - cache is optimization
    }

    // 3. Run command (bare metal - just exec it)
    //    Cache writes go to volume via symlinks
    cmd := exec.CommandContext(ctx, "sh", "-c", job.Command)
    cmd.Dir = workDir
    cmd.Stdout = conn.LogWriter()
    cmd.Stderr = conn.LogWriter()

    err = cmd.Run()
    exitCode := 0
    if exitErr, ok := err.(*exec.ExitError); ok {
        exitCode = exitErr.ExitCode()
    }

    // 4. Report result
    conn.SendResult(job.ID, exitCode)

    // 5. Exit - auto_destroy cleans up machine, volume persists
    return err
}

func setupCacheSymlinks(cacheVolume string, caches []CacheSpec) error {
    for _, cache := range caches {
        volumePath := filepath.Join(cacheVolume, cache.Name)
        os.MkdirAll(volumePath, 0755)
        os.RemoveAll(cache.MountPath)
        os.MkdirAll(filepath.Dir(cache.MountPath), 0755)
        // /go/pkg/mod → /vol/cache/go-mod
        os.Symlink(volumePath, cache.MountPath)
    }
    return nil
}
```

## Image Cleanup

Old images accumulate. Clean them up:

```go
// Run periodically (daily cron)
func (m *ManagedWorker) CleanupOldImages(ctx context.Context) error {
    images, err := m.fly.ListImages(ctx, "cinch-workers")
    if err != nil {
        return err
    }

    for _, img := range images {
        // Parse tag: u_abc123-repo_456-hash
        userID, repoID, _ := parseImageTag(img.Tag)

        // Check if repo still exists and is active
        repo, err := m.db.GetRepo(repoID)
        if err != nil || repo.DeletedAt != nil {
            // Repo deleted, remove image
            m.fly.DeleteImage(ctx, "cinch-workers", img.Tag)
            continue
        }

        // Check if this hash is current
        currentHash := m.getDockerfileHash(repo)
        if !strings.HasSuffix(img.Tag, currentHash) {
            // Old Dockerfile version, remove
            m.fly.DeleteImage(ctx, "cinch-workers", img.Tag)
        }
    }

    return nil
}
```

## Pricing Model

### What's Included in $5/seat/month (Pro)

| Feature | Included |
|---------|----------|
| Private repos | Yes |
| Build cache | 10GB (team pool) |
| BYOW workers | Unlimited |
| Managed workers | **Pay-as-you-go only** |

### Pay-as-you-go Compute

| Size | Specs | Cinch Price | Fly Cost | Margin |
|------|-------|-------------|----------|--------|
| small | 1 vCPU, 1GB | $0.005/min | ~$0.0015/min | 70% |
| medium | 2 vCPU, 2GB | $0.008/min | ~$0.003/min | 62% |
| large | 4 vCPU, 4GB | $0.015/min | ~$0.006/min | 60% |
| xlarge | 8 vCPU, 8GB | $0.025/min | ~$0.012/min | 52% |

**Billing:** Per second, displayed as per minute. No rounding tricks.

### Image Build Time

First build (or when Dockerfile changes) incurs build time:
- Billed at the same rate as compute
- Typically 1-5 minutes
- Cached by Fly's builder for subsequent builds

Alternative: Don't charge for image builds, eat the cost as "setup".

## Config Options

```yaml
# .cinch.yaml
build: make test

# Use managed workers (Pro only, paygo)
worker: managed
machine: medium  # small, medium, large, xlarge

# With custom Dockerfile
dockerfile: Dockerfile.ci
worker: managed

# Or just use a base image (no custom Dockerfile)
image: golang:1.21
worker: managed
```

### What If No Dockerfile?

If user specifies `image:` instead of `dockerfile:`, we generate a minimal Dockerfile:

```dockerfile
FROM golang:1.21
RUN apt-get update && apt-get install -y git curl
# Cinch harness appended automatically
```

## Coordination from Control Plane

The control plane doesn't need to poll or coordinate shutdown. The flow is:

```
Control Plane                      Fly Machine
      │                                 │
      │  1. Create machine              │
      │─────────────────────────────────>
      │                                 │
      │  2. Machine starts, connects    │
      │<─────────────────────────────────
      │        WebSocket                │
      │                                 │
      │  3. Log streaming               │
      │<─────────────────────────────────
      │        (continuous)             │
      │                                 │
      │  4. Job result                  │
      │<─────────────────────────────────
      │        {exit_code: 0}           │
      │                                 │
      │  5. Connection closed           │
      │        (machine exiting)        │
      │                                 │
      │                           auto_destroy
      │                           (machine gone)
      │                                 │
      │  6. Update job status           │
      │        (based on result msg)    │
```

No external coordination needed. Machine handles its own cleanup.

### Handling Timeouts

What if the machine hangs?

```go
func (m *ManagedWorker) MonitorJob(ctx context.Context, job *Job, machine *Machine) {
    timeout := job.Timeout
    if timeout == 0 {
        timeout = 30 * time.Minute // Default
    }

    timer := time.NewTimer(timeout)
    defer timer.Stop()

    select {
    case <-job.Done:
        // Job completed normally
        return
    case <-timer.C:
        // Timeout - force kill the machine
        m.fly.StopMachine(ctx, machine.ID)
        // auto_destroy will clean it up
        job.Fail("timeout after " + timeout.String())
    }
}
```

## Architecture Diagram

```
                                    ┌─────────────────────────────────────┐
                                    │         Fly.io                       │
Push to GitHub                      │                                     │
      │                             │  registry.fly.io/cinch-workers      │
      ▼                             │    :u_abc-repo_123-a1b2c3          │
┌──────────────┐                    │    :u_abc-repo_456-d4e5f6          │
│   GitHub     │                    │    :u_def-repo_789-g7h8i9          │
│   Webhook    │                    │                                     │
└──────┬───────┘                    │  ┌─────────┐  ┌─────────┐          │
       │                            │  │ Machine │  │ Machine │          │
       ▼                            │  │ j_abc   │  │ j_def   │          │
┌──────────────┐                    │  │ ██████░░│  │ ████░░░░│          │
│    Cinch     │  create machine    │  └────┬────┘  └────┬────┘          │
│ Control Plane│───────────────────►│       │            │               │
│              │                    │       │ WebSocket  │               │
│              │◄───────────────────│───────┴────────────┘               │
│              │  logs + result     │                                     │
└──────┬───────┘                    │  (machines auto_destroy on exit)   │
       │                            └─────────────────────────────────────┘
       ▼
┌──────────────┐
│ Cloudflare   │
│     R2       │
│ (cache store)│
└──────────────┘
```

## Security

### Isolation

Each job runs in its own Fly Machine (Firecracker microVM):
- Fresh VM per job
- Destroyed immediately after completion (auto_destroy)
- No persistent state between jobs
- Network isolated from other machines

### Credentials

All credentials are short-lived and scoped:
- Clone token: expires in 1 hour, repo-scoped
- R2 credentials: expires in 1 hour, prefix-scoped to user's cache
- Job token: expires in 1 hour, job-scoped

### No Docker Daemon

Because we run the user's image directly (not Docker-in-Docker):
- No container escape risks
- No privileged mode needed
- Simpler attack surface

## Limitations (v1)

- **Linux only** - No Windows/macOS managed workers
- **No GPU** - Use BYOW for GPU builds
- **Dockerfile must include git** - Required for repo cloning
- **No persistent workers** - All ephemeral, scale to zero
- **Image build on first run** - First build includes Dockerfile build time

## Cost Examples

### Small Node.js Project
- Build: 45 seconds
- Machine: small
- First build adds: ~2 min (Dockerfile build, cached after)

| Component | First Build | Subsequent |
|-----------|-------------|------------|
| Image build | $0.01 | $0 |
| Compute | $0.004 | $0.004 |
| **Total** | **$0.014** | **$0.004** |

### Active Team (500 builds/month)
- Average: 90 seconds per build
- Machine: medium
- Dockerfile changes: ~2x/month

| Component | Calculation | Cost |
|-----------|-------------|------|
| Compute | 500 × 90s × $0.008/60 | $6.00 |
| Image builds | 2 × 2min × $0.008 | $0.03 |
| Cache | included | $0 |
| **Monthly Total** | | **$6.03** |

Plus $5/seat/month for Pro = **~$11/month** total.

### Worst Case: Heavy Cache, Light Compute

Rust user on yearly plan ($4/mo), 8GB cache, fast builds:

```
BYOW Workers (R2 cache):
  Revenue:  $4.00/mo (subscription)
  Cost:     $0.12/mo (8GB × $0.015 R2 storage)
  Margin:   $3.88/mo (97%)

Managed Workers (Fly volumes):
  Revenue:  $4.00/mo (sub) + $2.00/mo (compute)
  Cost:     $1.20/mo (8GB volume) + $0.75/mo (compute)
  Margin:   $4.05/mo (67%)

Managed, barely uses it:
  Revenue:  $4.00/mo (sub) + ~$0 (minimal compute)
  Cost:     $1.20/mo (8GB volume)
  Margin:   $2.80/mo (70%)
```

**Bottom line:** Even worst case (8GB cache, minimal compute), we keep 70% margin. The subscription covers the volume cost with room to spare. Fast builds mean the cache is working—happy customer who's still paying.

## Implementation Phases

### Phase 1: Basic Managed Workers
- [ ] Dockerfile append + build pipeline
- [ ] Fly Machines API integration
- [ ] `cinch worker --one-shot --bare-metal` mode
- [ ] Log streaming over WebSocket
- [ ] Volume creation + symlink setup in harness
- [ ] Volume auto-expansion (1GB start, grow as needed)

### Phase 2: Billing
- [ ] Usage tracking (per-second)
- [ ] Stripe integration
- [ ] Spending limits/alerts
- [ ] Usage dashboard

### Phase 3: Optimization
- [ ] Image caching improvements
- [ ] Region selection intelligence
- [ ] Machine size auto-selection

### Phase 4: Polish
- [ ] Dockerfile change detection
- [ ] Old image cleanup
- [ ] Better error messages for Dockerfile requirements
