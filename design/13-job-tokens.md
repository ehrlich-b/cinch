# Job Tokens & Environment

## The Problem

CI jobs need authenticated access to forge APIs for:
- Creating releases
- Posting comments on PRs
- Pushing tags
- Updating repo contents

GitHub Actions solves this with `GITHUB_TOKEN` - automatically available in every workflow.

Cinch needs the same: **automatic, scoped tokens available to every job.**

## Solution: Pass Installation Token as GITHUB_TOKEN

Cinch already generates GitHub App installation tokens for cloning and status posting. We extend this:

```
GitHub App (Cinch CI)
    │
    ├── contents: write    ← Allows releases, pushes
    ├── statuses: write    ← Commit statuses
    ├── checks: write      ← Check runs
    └── metadata: read     ← Required

    │
    ▼
Installation Token (1 hour)
    │
    ▼
Passed to Worker as GITHUB_TOKEN
    │
    ▼
Job script uses it like GitHub Actions
```

### What This Enables

Any script that works with GitHub Actions works with Cinch:

```bash
# Release script - works identically on both platforms
gh release create v1.0.0 dist/* --generate-notes
```

```bash
# Or with curl
curl -X POST \
  -H "Authorization: token $GITHUB_TOKEN" \
  -H "Accept: application/vnd.github+json" \
  https://api.github.com/repos/OWNER/REPO/releases \
  -d '{"tag_name":"v1.0.0"}'
```

## Implementation

### 1. Update GitHub App Permissions

In GitHub App settings, change:
- `contents: read` → `contents: write`

Existing installations will need to accept the new permissions.

### 2. Add Forge Type to Protocol

```go
// protocol/message.go
type JobRepo struct {
    CloneURL   string `json:"clone_url"`
    CloneToken string `json:"clone_token,omitempty"`
    Commit     string `json:"commit"`
    Branch     string `json:"branch"`
    ForgeType  string `json:"forge_type"`  // ← Add this
    // ...
}
```

### 3. Pass Forge Type in Job Assignment

```go
// server/dispatch.go - in assignJob()
Repo: protocol.JobRepo{
    CloneURL:   qj.CloneURL,
    CloneToken: qj.CloneToken,
    Commit:     qj.Commit,
    Branch:     qj.Branch,
    ForgeType:  qj.ForgeType,  // ← Add this
}
```

### 4. Worker Sets Token Environment Variables

```go
// worker/worker.go - in executeJob()
env["CINCH_JOB_ID"] = jobID
env["CINCH_BRANCH"] = assign.Repo.Branch
env["CINCH_COMMIT"] = assign.Repo.Commit
env["CINCH_REPO"] = assign.Repo.CloneURL
env["CINCH_FORGE"] = assign.Repo.ForgeType

// Set forge-specific token env var
if assign.Repo.CloneToken != "" {
    switch assign.Repo.ForgeType {
    case "github":
        env["GITHUB_TOKEN"] = assign.Repo.CloneToken
    case "gitlab":
        env["GITLAB_TOKEN"] = assign.Repo.CloneToken
        env["CI_JOB_TOKEN"] = assign.Repo.CloneToken  // GitLab compat
    case "forgejo", "gitea":
        env["GITEA_TOKEN"] = assign.Repo.CloneToken
    }
    // Always set generic var
    env["CINCH_FORGE_TOKEN"] = assign.Repo.CloneToken
}
```

### 4. User's Release Script

```yaml
# .cinch.yaml
command: make release
```

```makefile
# Makefile
release:
    # GITHUB_TOKEN is automatically available
    gh release create $(VERSION) dist/* --generate-notes
```

## Token Scope & Security

### Permissions

The token has whatever permissions the GitHub App has:
- Can create releases (contents: write)
- Can post statuses (statuses: write)
- Cannot access other repos (scoped to installation)
- Cannot change repo settings (no admin permissions)

### Lifetime

- Token expires in 1 hour
- Fine for builds (most complete in minutes)
- For long builds, Cinch refreshes token

### Revocation

- Uninstalling the GitHub App revokes all tokens
- Removing a repo from the installation removes access
- No manual token management needed

## Other Forges

For non-GitHub forges (GitLab, Forgejo, Gitea):

### GitLab

```
GITLAB_TOKEN=<project access token>
CI_JOB_TOKEN=<same, for compatibility>
```

GitLab doesn't have GitHub App equivalent. Options:
1. User creates Project Access Token
2. Store in Cinch repo config
3. Pass to worker

### Forgejo/Gitea

```
GITEA_TOKEN=<access token>
```

Similar to GitLab - user-provided token stored in Cinch.

### Generic

```
CINCH_FORGE_TOKEN=<whatever token was configured>
```

Always set, regardless of forge type.

## Environment Variables Summary

Every Cinch job gets:

```bash
# Cinch-specific
CINCH_JOB_ID=j_abc123
CINCH_REPO=owner/repo
CINCH_BRANCH=main
CINCH_COMMIT=abc1234567890
CINCH_FORGE=github  # or gitlab, forgejo, gitea

# Forge token (for API access)
GITHUB_TOKEN=ghs_xxx        # GitHub
GITLAB_TOKEN=glpat-xxx      # GitLab
GITEA_TOKEN=xxx             # Forgejo/Gitea
CINCH_FORGE_TOKEN=xxx       # Always set (same as above)

# Clone already authenticated via URL
# https://x-access-token:TOKEN@github.com/owner/repo.git
```

## Why This Matters

The whole point of Cinch is: **your Makefile is the pipeline**.

If your Makefile works locally with a GITHUB_TOKEN, it should work identically on Cinch. No special Cinch release action, no Cinch-specific API. Just standard tools using standard environment variables.

This is how Cinch releases itself:

```yaml
# .cinch.yaml (Cinch's own config)
command: make release
```

```makefile
# Cinch's Makefile
release:
    go build ...
    gh release create $(VERSION) dist/*
```

Cinch builds Cinch. Same tools, same env vars, your hardware.
