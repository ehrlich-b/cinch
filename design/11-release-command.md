# Design: `cinch release` Command

## Overview

The `cinch` binary becomes the swiss army knife for CI operations. Instead of requiring forge-specific CLIs (gh, glab, etc.), `cinch release` handles releases across all forges with zero configuration.

## Usage

```yaml
# .cinch.yaml
build: make check
release: make build-dist && cinch release dist/*
```

```bash
# Manual usage
cinch release dist/*                    # Auto-detect forge from env
cinch release --forge github dist/*     # Explicit forge
cinch release --tag v1.0.0 dist/*       # Override tag
```

## Auto-Detection

When running inside a Cinch job, everything is auto-detected:

| Source | Environment Variable |
|--------|---------------------|
| Forge type | `CINCH_FORGE` (github, gitlab, gitea, forgejo) |
| Repository | `CINCH_REPO` (clone URL) |
| Tag | `CINCH_TAG` (e.g., v0.1.8) |
| Token | `CINCH_FORGE_TOKEN` (or GITHUB_TOKEN, etc.) |

Outside of Cinch jobs, falls back to git remotes and prompts for token.

## Binary Injection

**Key insight:** The worker already has `cinch` installed. Inject the correct Linux binary into containers.

### The Challenge

macOS binaries (Mach-O format) cannot run in Linux containers (ELF format), even when CPU architecture matches.
A darwin/arm64 binary cannot run in a linux/arm64 container.

### Solution: Multi-Platform Installation

The install script downloads ALL platform variants:

```
~/.cinch/bin/
├── cinch -> cinch-darwin-arm64  (symlink to local platform)
├── cinch-darwin-arm64
├── cinch-darwin-amd64
├── cinch-linux-arm64
└── cinch-linux-amd64
```

When injecting into containers, use the Linux binary:

```go
// In container/docker.go Run()
// Use Linux binary - macOS Mach-O can't run in Linux containers
if cinchPath, err := GetLinuxBinary(); err == nil {
    args = append(args, "-v", cinchPath+":/usr/local/bin/cinch:ro")
}
```

### Binary Resolution

`GetLinuxBinary()` checks:
1. `~/.cinch/bin/cinch-linux-{arch}` - from install.sh
2. If not present or version mismatch, downloads from GitHub releases

This ensures:
- Workers running on macOS can inject Linux binaries into containers
- Version consistency (binary matches running cinch version)
- Zero config for users (install once, binary injection "just works")

### Cinch's Own Releases

For cinch's own CI (dogfooding), the release target uses the just-built native binary:

```makefile
./dist/cinch-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m | sed 's/aarch64/arm64/') release dist/*
```

This works because cinch builds all platform variants, so the linux binary is available in dist/.

## Forge APIs

### GitHub
```
POST /repos/{owner}/{repo}/releases
  {"tag_name": "v1.0.0", "name": "v1.0.0", "generate_release_notes": true}

POST https://uploads.github.com/repos/{owner}/{repo}/releases/{id}/assets?name=filename
  [binary data]
```

### GitLab
```
POST /projects/{id}/releases
  {"tag_name": "v1.0.0", "name": "v1.0.0"}

POST /projects/{id}/uploads
  [multipart form]

PUT /projects/{id}/releases/{tag}/assets/links
  {"name": "filename", "url": "..."}
```

### Gitea/Forgejo
```
POST /repos/{owner}/{repo}/releases
  {"tag_name": "v1.0.0", "name": "v1.0.0"}

POST /repos/{owner}/{repo}/releases/{id}/assets?name=filename
  [binary data]
```

## Implementation Plan

1. **Binary injection** - Mount cinch into containers (quick win)
2. **`cinch release` command** - New CLI command with forge detection
3. **GitHub release** - Implement first (we need it for our own releases)
4. **GitLab/Gitea release** - Add other forges

## CLI Interface

```
cinch release [flags] <files...>

Flags:
  --forge string    Override forge detection (github, gitlab, gitea)
  --tag string      Override tag (default: from CINCH_TAG or git describe)
  --repo string     Override repository (default: from CINCH_REPO or git remote)
  --token string    Override token (default: from env)
  --notes string    Release notes (default: auto-generated)
  --draft           Create as draft release
  --prerelease      Mark as prerelease

Examples:
  cinch release dist/*
  cinch release --tag v1.0.0 dist/myapp-linux-amd64 dist/myapp-darwin-arm64
```

## Error Handling

- Missing CINCH_TAG when not in CI: prompt or error
- Missing token: clear error with instructions
- Upload failures: retry with backoff
- Existing release: update or error based on flag

## Future: More Commands

The pattern extends naturally:
- `cinch release` - Create releases
- `cinch comment` - Comment on PRs/MRs
- `cinch status` - Post commit status (already done by worker, but useful for manual)
- `cinch artifact` - Upload/download build artifacts
