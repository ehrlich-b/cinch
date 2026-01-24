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

**Key insight:** The worker already has `cinch` installed. Mount it into the container.

```go
// In container/docker.go Run()
args := []string{"run", "--rm"}

// Mount cinch binary from host
cinchPath, _ := os.Executable()
args = append(args, "-v", cinchPath+":/usr/local/bin/cinch:ro")
```

Now every container job has `cinch` available automatically. No install step needed.

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
