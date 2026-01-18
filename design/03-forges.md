# Forge Integrations

## Overview

Cinch supports multiple git forges. Each forge needs:
1. **Webhook parsing** - Understand incoming push/PR events
2. **Status posting** - Report build status back to the forge
3. **Clone auth** - Generate tokens for cloning private repos

## Forge Interface

```go
type Forge interface {
    // Identify the forge from webhook headers/payload
    Identify(r *http.Request) bool

    // Parse webhook into normalized event
    ParseWebhook(r *http.Request, secret string) (*WebhookEvent, error)

    // Post status check to commit
    PostStatus(ctx context.Context, repo *Repo, commit string, status *Status) error

    // Generate short-lived clone token
    CloneToken(ctx context.Context, repo *Repo) (string, time.Time, error)

    // Name for display
    Name() string
}

type WebhookEvent struct {
    Type      EventType  // push, pull_request, etc.
    Repo      *Repo
    Commit    string
    Branch    string
    IsPR      bool
    PRNumber  int
    PRAction  string     // opened, synchronized, closed
    Sender    string     // username who triggered
    Timestamp time.Time
}

type Repo struct {
    ForgeType  string    // github, gitlab, forgejo, etc.
    Owner      string
    Name       string
    CloneURL   string
    HTMLURL    string
    Private    bool
}

type Status struct {
    State       StatusState  // pending, success, failure, error
    Context     string       // "cinch" or "cinch/build"
    Description string       // "Build passed in 2m 34s"
    TargetURL   string       // Link to logs
}

type StatusState string
const (
    StatusPending StatusState = "pending"
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
