# Forge Integrations

> **Note:** Detailed per-forge design documents are now in `design/forge/`:
> - `design/forge/github.md` - GitHub App integration (implemented)
> - `design/forge/forgejo-gitea.md` - Forgejo/Gitea (implemented)
> - `design/forge/gitlab.md` - GitLab exploration
> - `design/forge/bitbucket.md` - Bitbucket exploration
>
> This file contains the original interface design and general patterns.

## Implementation Status

| Forge | Status | Notes |
|-------|--------|-------|
| GitHub | **Complete** | Webhooks, Status API, Checks API, GitHub App |
| Forgejo | **Complete** | Webhooks, Status API |
| Gitea | **Complete** | Same as Forgejo (shared implementation) |
| GitLab | Planned | See `design/forge/gitlab.md` |
| Bitbucket | Planned | See `design/forge/bitbucket.md` |

## Overview

Cinch supports multiple git forges. Each forge needs:
1. **Webhook parsing** - Understand incoming push/PR events
2. **Status posting** - Report build status back to the forge
3. **Clone auth** - Generate tokens for cloning private repos

## Forge Interface

Located in `internal/forge/forge.go`:

```go
type Forge interface {
    // Name returns the forge name for display
    Name() string

    // Identify returns true if the request is from this forge
    Identify(r *http.Request) bool

    // ParsePush parses a push webhook and verifies the signature
    ParsePush(r *http.Request, secret string) (*PushEvent, error)

    // PostStatus posts a commit status to the forge
    PostStatus(ctx context.Context, repo *Repo, commit string, status *Status) error

    // CloneToken generates a short-lived token for cloning
    CloneToken(ctx context.Context, repo *Repo) (string, time.Time, error)
}
```

## Factory Pattern

Create forge instances via factory (no switch statements needed in handlers):

```go
// Type constants
const (
    TypeGitHub  = "github"
    TypeGitLab  = "gitlab"
    TypeForgejo = "forgejo"
    TypeGitea   = "gitea"
)

// Create a forge instance
f := forge.New(forge.ForgeConfig{
    Type:    forge.TypeGitHub,
    Token:   "ghs_xxx",
    BaseURL: "",  // only needed for self-hosted (Forgejo, GitLab)
})
```

## Adding a New Forge

1. Create `internal/forge/newforge.go` implementing the `Forge` interface
2. Add type constant to `internal/forge/forge.go`
3. Add case to `forge.New()` factory function
4. Register in `cmd/cinch/main.go`:
   ```go
   webhookHandler.RegisterForge(&forge.NewForge{})
   ```
5. Add tests in `internal/forge/newforge_test.go`

## Data Types

```go
type PushEvent struct {
    Repo   *Repo
    Commit string // SHA of the head commit
    Branch string // Branch name (without refs/heads/)
    Sender string // Username who pushed
}

type Repo struct {
    ForgeType string // "github", "forgejo", etc.
    Owner     string
    Name      string
    CloneURL  string
    HTMLURL   string
    Private   bool
}

type Status struct {
    State       StatusState
    Context     string // "cinch"
    Description string // "Build passed in 2m 34s"
    TargetURL   string // Link to job logs
}

type StatusState string
const (
    StatusPending StatusState = "pending"
    StatusRunning StatusState = "running"
    StatusSuccess StatusState = "success"
    StatusFailure StatusState = "failure"
    StatusError   StatusState = "error"
)
```

## GitHub

### Webhook Events

**Push:**
```
POST /webhook/github
X-GitHub-Event: push
X-Hub-Signature-256: sha256=...

{
  "ref": "refs/heads/main",
  "after": "abc1234...",
  "repository": {
    "full_name": "user/repo",
    "clone_url": "https://github.com/user/repo.git",
    "private": false
  },
  "sender": { "login": "username" }
}
```

**Pull Request:**
```
X-GitHub-Event: pull_request

{
  "action": "opened",  // or "synchronize", "closed"
  "number": 42,
  "pull_request": {
    "head": { "sha": "abc1234..." },
    "base": { "ref": "main" }
  },
  "repository": { ... }
}
```

### Status API

```
POST /repos/{owner}/{repo}/statuses/{sha}
Authorization: Bearer {token}

{
  "state": "success",
  "context": "cinch",
  "description": "Build passed in 2m 34s",
  "target_url": "https://cinch.sh/jobs/123"
}
```

### Authentication

**GitHub App (recommended):**
- Create GitHub App with permissions: `statuses:write`, `contents:read`
- Generate JWT, exchange for installation token
- Installation tokens expire in 1 hour (perfect for clone tokens)

**Personal Access Token (simpler):**
- Classic PAT with `repo:status` scope
- Use directly in API calls
- Clone via HTTPS with token as password

## GitLab

### Webhook Events

**Push:**
```
POST /webhook/gitlab
X-Gitlab-Event: Push Hook
X-Gitlab-Token: {secret}

{
  "object_kind": "push",
  "ref": "refs/heads/main",
  "checkout_sha": "abc1234...",
  "project": {
    "path_with_namespace": "user/repo",
    "git_http_url": "https://gitlab.com/user/repo.git"
  }
}
```

**Merge Request:**
```
X-Gitlab-Event: Merge Request Hook

{
  "object_kind": "merge_request",
  "object_attributes": {
    "action": "open",
    "iid": 42,
    "source_branch": "feature",
    "target_branch": "main",
    "last_commit": { "id": "abc1234..." }
  }
}
```

### Commit Status API

```
POST /api/v4/projects/{id}/statuses/{sha}
PRIVATE-TOKEN: {token}

{
  "state": "success",
  "context": "cinch",
  "description": "Build passed",
  "target_url": "https://cinch.sh/jobs/123"
}
```

States: `pending`, `running`, `success`, `failed`, `canceled`

### Authentication

**Project Access Token:**
- Scoped to single project
- Role: Maintainer
- Scopes: `api`, `read_repository`

**Personal Access Token:**
- Scopes: `api`, `read_repository`

## Forgejo / Gitea

Nearly identical APIs (Forgejo is a Gitea fork).

### Webhook Events

**Push:**
```
POST /webhook/forgejo
X-Forgejo-Event: push  (or X-Gitea-Event)

{
  "ref": "refs/heads/main",
  "after": "abc1234...",
  "repository": {
    "full_name": "user/repo",
    "clone_url": "https://forgejo.example/user/repo.git"
  }
}
```

**Pull Request:**
```
X-Forgejo-Event: pull_request

{
  "action": "opened",
  "number": 42,
  "pull_request": {
    "head": { "sha": "abc1234..." }
  }
}
```

### Commit Status API

```
POST /api/v1/repos/{owner}/{repo}/statuses/{sha}
Authorization: token {token}

{
  "state": "success",
  "context": "cinch",
  "description": "Build passed",
  "target_url": "https://cinch.sh/jobs/123"
}
```

States: `pending`, `success`, `error`, `failure`, `warning`

### Authentication

**Access Token:**
- Settings → Applications → Generate Token
- Scopes: `repo:status`, `read:repository`

## Bitbucket

### Webhook Events

**Push:**
```
POST /webhook/bitbucket
X-Event-Key: repo:push

{
  "push": {
    "changes": [{
      "new": {
        "type": "branch",
        "name": "main",
        "target": { "hash": "abc1234..." }
      }
    }]
  },
  "repository": {
    "full_name": "user/repo"
  }
}
```

**Pull Request:**
```
X-Event-Key: pullrequest:created

{
  "pullrequest": {
    "id": 42,
    "source": { "commit": { "hash": "abc1234..." } },
    "destination": { "branch": { "name": "main" } }
  }
}
```

### Build Status API

```
POST /2.0/repositories/{workspace}/{repo}/commit/{commit}/statuses/build
Authorization: Bearer {token}

{
  "state": "SUCCESSFUL",
  "key": "cinch",
  "name": "Cinch CI",
  "description": "Build passed",
  "url": "https://cinch.sh/jobs/123"
}
```

States: `INPROGRESS`, `SUCCESSFUL`, `FAILED`, `STOPPED`

### Authentication

**App Password:**
- Account Settings → App Passwords
- Permissions: `repository:read`, `pullrequest:read`, `repository:write` (for statuses)

## Generic Git Server

For self-hosted git without forge features:

### Webhook

Standard format (configurable):
```json
{
  "ref": "refs/heads/main",
  "commit": "abc1234...",
  "clone_url": "https://git.example/repo.git"
}
```

### No Status API

- Can't post status checks
- Logs visible in Cinch UI only
- Optional: update README badge via commit

## Implementation Priority

### v0.1 (MVP)
1. **GitHub** - Most popular, well-documented API
2. **Forgejo/Gitea** - Same API, covers self-hosted users

### v0.2
3. **GitLab** - Significant user base

### v0.3
4. **Bitbucket** - Different API style, lower priority

## Webhook Security

### Signature Verification

Each forge has different signature methods:

| Forge | Header | Algorithm |
|-------|--------|-----------|
| GitHub | `X-Hub-Signature-256` | HMAC-SHA256 |
| GitLab | `X-Gitlab-Token` | Direct secret comparison |
| Forgejo | `X-Forgejo-Signature` | HMAC-SHA256 |
| Bitbucket | `X-Hub-Signature` | HMAC-SHA256 |

### Implementation

```go
func (g *GitHub) VerifySignature(r *http.Request, secret string) error {
    sig := r.Header.Get("X-Hub-Signature-256")
    if sig == "" {
        return errors.New("missing signature")
    }

    body, _ := io.ReadAll(r.Body)
    expected := "sha256=" + hmacSHA256(body, secret)

    if !hmac.Equal([]byte(sig), []byte(expected)) {
        return errors.New("signature mismatch")
    }

    return nil
}
```

## Clone URL Handling

### Private Repos

Transform clone URL to include auth:

```
Original:  https://github.com/user/repo.git
With auth: https://x-access-token:{token}@github.com/user/repo.git

Original:  https://gitlab.com/user/repo.git
With auth: https://oauth2:{token}@gitlab.com/user/repo.git

Original:  https://forgejo.example/user/repo.git
With auth: https://{username}:{token}@forgejo.example/user/repo.git
```

### Public Repos

No auth needed - clone URL used directly.

## Rate Limiting

Each forge has rate limits. Handle gracefully:

| Forge | Rate Limit | Reset |
|-------|------------|-------|
| GitHub | 5000/hr (authenticated) | X-RateLimit-Reset |
| GitLab | 2000/min | RateLimit-Reset |
| Forgejo | Varies by instance | X-RateLimit-Reset |
| Bitbucket | 1000/hr | Retry-After |

```go
func (c *Client) PostStatus(...) error {
    resp, err := c.do(req)
    if resp.StatusCode == 429 {
        reset := resp.Header.Get("X-RateLimit-Reset")
        // Wait and retry, or queue for later
    }
    // ...
}
```
