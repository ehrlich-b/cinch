# Build Cache

## Overview

Cinch provides zero-config build caching for Pro users. The cache stores **build tool caches** (go module cache, npm cache, cargo registry, etc.). Users configure nothing—Cinch auto-detects what to cache.

**Key Philosophy:** Cinch's approach is "your Makefile is the CI." Most users run `make check` in a base image like `golang:1.21`. They don't have complex Dockerfiles. The valuable cache isn't Docker layers—it's the package manager and build tool caches.

## Two Cache Backends

| Backend | Used By | How It Works |
|---------|---------|--------------|
| **Fly Volumes** | Managed workers | Persistent volume mounted to machine, symlinked to cache paths |
| **R2 Tarballs** | BYOW workers, `cinch run` | Download tar.zst before job, upload after |

Pro users get **10GB cache included**. Same quota, different backends depending on where the job runs.

## What Gets Cached

| Detected File | Cached Directory | Purpose |
|---------------|------------------|---------|
| `go.mod` | `/go/pkg/mod` | Go module cache |
| `go.mod` | `~/.cache/go-build` | Go build cache |
| `.golangci.yml` | `~/.cache/golangci-lint` | Linter cache |
| `package.json` | `~/.npm` | npm tarball cache |
| `Cargo.toml` | `~/.cargo/registry` | Cargo crates |
| `Cargo.toml` | `~/.cargo/git` | Cargo git deps |
| `pom.xml` | `~/.m2/repository` | Maven artifacts |
| `requirements.txt` | `~/.cache/pip` | pip cache |
| `build.gradle` | `~/.gradle/caches` | Gradle cache |

**Users configure: nothing.** Cinch detects and caches automatically.

### Why ~/.npm, Not node_modules?

We cache `~/.npm` (npm's download cache), NOT `node_modules/`:

1. `npm ci` deletes node_modules and rebuilds from lockfile
2. But `npm ci` reuses tarballs from `~/.npm` cache
3. Caching `~/.npm` is correct; caching `node_modules` doesn't help

Same principle for all package managers: cache the **download cache**, not the installed output.

## Managed Workers: Fly Volumes

For managed workers on Fly.io, caches live on **persistent Fly volumes** that survive between jobs.

### How It Works

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ Fly Volume: /vol/cache (persistent, survives between jobs)                   │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   /vol/cache/                                                               │
│     ├── go-mod/           # Go modules                                      │
│     ├── go-build/         # Go build cache                                  │
│     ├── golangci-lint/    # Linter cache                                    │
│     ├── npm/              # npm tarball cache                               │
│     ├── cargo-registry/   # Rust crates                                     │
│     └── ...                                                                 │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘

Symlinks (created at job start):
    /go/pkg/mod                    → /vol/cache/go-mod
    /root/.cache/go-build          → /vol/cache/go-build
    /root/.cache/golangci-lint     → /vol/cache/golangci-lint
    /root/.npm                     → /vol/cache/npm
    /root/.cargo/registry          → /vol/cache/cargo-registry
```

### Job Flow (Managed Workers)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ Job starts                                                                   │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   1. Create machine from user's image                                       │
│   2. Attach volume for this repo (create if first job)                      │
│   3. Create symlinks: expected paths → volume paths                         │
│   4. git clone                                                              │
│   5. Run command (make check)                                               │
│      - All cache writes go to volume via symlinks                          │
│   6. Machine destroyed                                                       │
│   7. Volume persists (warm for next job)                                    │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Key insight:** Volume persists. Second job has instant warm cache—no download needed.

### Volume Management

```go
func (m *ManagedWorker) getOrCreateVolume(ctx context.Context, repoID string) (*Volume, error) {
    volumeName := fmt.Sprintf("cache-%s", repoID)

    // Check if volume exists
    vol, err := m.fly.GetVolume(ctx, volumeName)
    if err == nil {
        // Auto-expand if >80% full (up to quota)
        if vol.UsedGB > vol.SizeGB * 0.8 && vol.SizeGB < m.getQuota(repoID) {
            m.fly.ExtendVolume(ctx, vol.ID, vol.SizeGB + 2)
        }
        return vol, nil
    }

    // Create new volume - START SMALL (1GB), grow as needed
    return m.fly.CreateVolume(ctx, fly.CreateVolumeRequest{
        Name:   volumeName,
        Region: m.defaultRegion,
        SizeGB: 1, // Start small!
    })
}
```

### Symlink Setup (in cinch harness)

```go
func setupCacheSymlinks(cacheVolume string, detectedCaches []CacheSpec) error {
    for _, cache := range detectedCaches {
        volumePath := filepath.Join(cacheVolume, cache.Name)

        // Ensure directory exists in volume
        os.MkdirAll(volumePath, 0755)

        // Remove existing path if it exists
        os.RemoveAll(cache.MountPath)

        // Ensure parent directory exists
        os.MkdirAll(filepath.Dir(cache.MountPath), 0755)

        // Create symlink: /go/pkg/mod → /vol/cache/go-mod
        if err := os.Symlink(volumePath, cache.MountPath); err != nil {
            return fmt.Errorf("symlink %s → %s: %w", cache.MountPath, volumePath, err)
        }
    }
    return nil
}
```

### Volume Costs

Volumes start at 1GB and auto-expand as needed:

| Scenario | Volume Size | Monthly Cost | Who Pays |
|----------|-------------|--------------|----------|
| Typical | 1-3GB | $0.15-$0.45 | Cinch (included in Pro) |
| Heavy | 5-10GB | $0.75-$1.50 | Cinch (included in Pro) |
| Paygo | >10GB | $0.15/GB/mo | User |

**Typical case:** Most repos need 1-3GB = ~$0.30/mo cost to us.
**Worst case:** Maxed out 10GB = $1.50/mo. But they're also paying margins on compute.

### Volume Expiry

Volumes expire after 30 days of no use:

```go
// Daily cleanup job
func (m *ManagedWorker) cleanupStaleVolumes(ctx context.Context) error {
    volumes, _ := m.fly.ListVolumes(ctx)

    for _, vol := range volumes {
        lastUsed := m.db.GetVolumeLastUsed(vol.Name)
        if time.Since(lastUsed) > 30*24*time.Hour {
            m.fly.DeleteVolume(ctx, vol.ID)
            m.db.DeleteVolumeRecord(vol.Name)
        }
    }
    return nil
}
```

## BYOW Workers: R2 Tarballs

For bring-your-own workers, caches sync to/from Cloudflare R2 as compressed tarballs.

### How It Works

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ Before Build                                                                 │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   Download from R2 (parallel):                                              │
│     → /u_abc/repo_123/go-mod.tar.zst        (89MB)                          │
│     → /u_abc/repo_123/go-build.tar.zst      (234MB)                         │
│                                                                              │
│   Extract to local cache paths                                               │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ Run Build                                                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   docker run \                                                               │
│     -v /local/cache/go-mod:/go/pkg/mod \                                    │
│     -v /local/cache/go-build:/root/.cache/go-build \                        │
│     golang:1.21 make check                                                   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
                                        │
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ After Build                                                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   For each cache:                                                           │
│     1. Hash contents                                                        │
│     2. If changed: tar.zst → upload to R2                                   │
│     3. If unchanged: skip                                                   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Cache Versioning (R2)

**Q: What if I build an old commit but the cache has newer dependencies?**

**A: Works fine.** Package managers with lockfiles are idempotent:

```
Scenario: Build old commit that has package@1.0, but cache has package@2.0

1. git checkout old-commit
   → package-lock.json says "package@1.0.0"

2. Cache restored: ~/.npm has package@1.0 AND package@2.0 tarballs

3. npm ci runs
   → Reads lockfile (wants 1.0.0)
   → Finds 1.0.0 in cache ✓

Result: Correct build. Extra cached packages don't hurt.
```

**The cache is always additive.** Old packages accumulate. Lockfiles ensure correct versions.

### R2 Costs

| Resource | Price |
|----------|-------|
| Storage | $0.015/GB/month |
| GET (download) | $0.36/million |
| PUT (upload) | $4.50/million |
| **Egress** | **$0** |

**Per user per month (BYOW):**
- 500 builds, 80% cache hit rate, 300MB cache
- Storage: $0.0045
- Operations: $0.002
- **Total: ~$0.01/month**

## Implementation

### Cache Detection

```go
var defaultCaches = []CacheSpec{
    // Go
    {Name: "go-mod", MountPath: "/go/pkg/mod", DetectFile: "go.mod"},
    {Name: "go-build", MountPath: "/root/.cache/go-build", DetectFile: "go.mod"},
    {Name: "golangci-lint", MountPath: "/root/.cache/golangci-lint", DetectFile: ".golangci.yml"},

    // Node
    {Name: "npm", MountPath: "/root/.npm", DetectFile: "package.json"},

    // Rust
    {Name: "cargo-registry", MountPath: "/root/.cargo/registry", DetectFile: "Cargo.toml"},
    {Name: "cargo-git", MountPath: "/root/.cargo/git", DetectFile: "Cargo.toml"},

    // Python
    {Name: "pip", MountPath: "/root/.cache/pip", DetectFile: "requirements.txt"},

    // Java
    {Name: "maven", MountPath: "/root/.m2/repository", DetectFile: "pom.xml"},
    {Name: "gradle", MountPath: "/root/.gradle/caches", DetectFile: "build.gradle"},
}

func DetectCaches(workDir string) []CacheSpec {
    var active []CacheSpec
    for _, spec := range defaultCaches {
        if fileExists(filepath.Join(workDir, spec.DetectFile)) {
            active = append(active, spec)
        }
    }
    return active
}
```

### R2 Cache Manager (BYOW)

```go
type R2CacheManager struct {
    r2       *r2.Client
    cacheDir string
}

func (cm *R2CacheManager) RestoreCaches(ctx context.Context, repoID string, caches []CacheSpec) error {
    var wg sync.WaitGroup
    for _, cache := range caches {
        wg.Add(1)
        go func(c CacheSpec) {
            defer wg.Done()
            key := fmt.Sprintf("%s/%s.tar.zst", repoID, c.Name)
            data, err := cm.r2.Get(ctx, key)
            if err != nil {
                return // Cache miss, continue without
            }
            extractTarZst(data, filepath.Join(cm.cacheDir, c.Name))
        }(cache)
    }
    wg.Wait()
    return nil
}

func (cm *R2CacheManager) SaveCaches(ctx context.Context, repoID string, caches []CacheSpec) error {
    for _, cache := range caches {
        path := filepath.Join(cm.cacheDir, cache.Name)
        if !dirExists(path) {
            continue
        }

        currentHash := hashDir(path)
        if currentHash == cm.getPreviousHash(repoID, cache.Name) {
            continue // Unchanged
        }

        data, _ := createTarZst(path)
        key := fmt.Sprintf("%s/%s.tar.zst", repoID, cache.Name)
        cm.r2.Put(ctx, key, data)
        cm.setPreviousHash(repoID, cache.Name, currentHash)
    }
    return nil
}
```

## Quota Enforcement

Pro users get 10GB cache (team pool). Enforced differently per backend:

**Fly Volumes:** Volume size capped at quota. Expansion requires paygo.

**R2:** LRU eviction when quota exceeded.

```go
func (cm *CacheManager) enforceQuota(ctx context.Context, accountID string) error {
    usage := cm.db.GetCacheUsage(accountID)
    quota := cm.db.GetCacheQuota(accountID)

    if usage <= quota {
        return nil
    }

    // Evict oldest caches until under quota
    caches := cm.db.GetCachesByLastUsed(accountID)
    for _, cache := range caches {
        if usage <= quota {
            break
        }
        cm.deleteCache(ctx, cache)
        usage -= cache.Size
    }
    return nil
}
```

## What We're NOT Doing

- **Docker layer caching** - Not a registry, not BuildKit
- **Branch-specific caches** - Same cache for all branches (additive)
- **Cache key configuration** - Auto-detect everything
- **Cross-repo sharing** - Each repo has own cache namespace

## Rollout

### Phase 1: BYOW with R2
- [ ] Cache detection
- [ ] R2 tarball upload/download
- [ ] Volume mounting in Docker executor

### Phase 2: Managed Workers with Fly Volumes
- [ ] Volume creation/attachment
- [ ] Symlink setup in harness
- [ ] Volume expiry cleanup

### Phase 3: Polish
- [ ] Quota enforcement
- [ ] Usage dashboard
- [ ] Cache hit/miss metrics
