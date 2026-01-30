# Design 16: Worker Visibility & Forge-Based Access

## Status: Draft

## Problem

Current worker visibility is broken:
1. Personal workers with empty `ownerName` leak to everyone
2. No concept of who should see shared workers
3. We built a "personal vs shared" model without defining visibility rules

The deeper question: **How do multiple people on a team share CI infrastructure?**

We don't want to build our own teams/orgs system. The forges already have this:
- GitHub has orgs, teams, collaborators
- GitLab has groups, projects, members
- Forgejo has orgs and collaborators

## Core Insight

**Shared workers are shared at the REPO level, not the user level.**

A shared worker registers for specific repos. Anyone with write access to those repos (per the forge) can:
1. See the worker exists
2. Have their jobs run on it

This matches GitHub Actions: runners are scoped to repos/orgs, and collaborators with write access can trigger workflows.

## Visibility Rules

### Personal Workers

```
Visible to: owner only
Can dispatch to: owner's jobs only
```

Personal workers are simple. You own it, you see it, it runs your code.

### Shared Workers

```
Visible to: anyone with write access to ANY registered repo
Can dispatch to: jobs for registered repos, from authors with write access
```

A shared worker registers like:
```bash
cinch worker --shared --repos owner/repo1,owner/repo2
```

Anyone who can push to `owner/repo1` can see this worker and have their pushes run on it.

## Forge-Based Collaborator Checking

Instead of maintaining our own permission system, query the forge.

### API Endpoints

| Forge | Endpoint | Auth Required |
|-------|----------|---------------|
| GitHub | `GET /repos/{owner}/{repo}/collaborators/{user}/permission` | Repo read access |
| GitLab | `GET /projects/{id}/members/{user_id}` | Project access |
| Forgejo | `GET /repos/{owner}/{repo}/collaborators/{user}` | Repo access |

### Permission Levels

Map forge permissions to Cinch access:

| Forge Permission | Cinch Access |
|-----------------|--------------|
| GitHub: admin, maintain, write | write |
| GitHub: triage, read | read |
| GitLab: owner, maintainer, developer | write |
| GitLab: reporter, guest | read |
| Forgejo: admin, write | write |
| Forgejo: read | read |

**Write access = can use shared workers for that repo**

### Caching Strategy

Forge API calls are expensive. Cache aggressively.

```go
type CollaboratorCache struct {
    // Key: "forge:owner/repo:username"
    // Value: {access: "write"|"read"|"none", cachedAt: time}
    cache map[string]*CachedAccess
    ttl   time.Duration // 5 minutes
}

func (c *CollaboratorCache) HasWriteAccess(forge, repo, username string) (bool, error) {
    key := fmt.Sprintf("%s:%s:%s", forge, repo, username)

    if cached, ok := c.cache[key]; ok && time.Since(cached.cachedAt) < c.ttl {
        return cached.access == "write", nil
    }

    // Query forge API
    access, err := c.queryForge(forge, repo, username)
    if err != nil {
        // On error, fail closed (deny access)
        return false, err
    }

    c.cache[key] = &CachedAccess{access: access, cachedAt: time.Now()}
    return access == "write", nil
}
```

### When to Query

1. **Webhook received** - Check author's access for trust level
2. **Worker list API** - Check viewer's access to registered repos
3. **Job dispatch** - Verify author still has access (cache usually hits)

### Rate Limit Handling

- GitHub: 5000/hour with auth, 60/hour without
- GitLab: 300/minute for most endpoints
- Forgejo: Varies by instance

With 5-minute TTL and typical usage, we should stay well under limits. If rate limited:
- Return cached value even if stale
- Log warning
- Retry with exponential backoff

## Data Model Changes

### Worker Registration

Workers currently register with just labels. Add repo list:

```go
type RegisterMessage struct {
    Type     string   `json:"type"`     // "register"
    Labels   []string `json:"labels"`
    Mode     string   `json:"mode"`     // "personal" or "shared"
    Repos    []string `json:"repos"`    // For shared: ["owner/repo1", "owner/repo2"]
    Hostname string   `json:"hostname"`
}
```

### Worker Connection (Hub)

```go
type WorkerConn struct {
    ID              string
    OwnerID         string      // User ID of worker owner
    OwnerName       string      // Username (for display)
    Mode            WorkerMode  // personal or shared
    RegisteredRepos []string    // Repos this worker serves (shared only)
    Labels          []string
    Hostname        string
    // ... existing fields
}
```

### Database Schema

```sql
-- New table for shared worker repo registrations
CREATE TABLE worker_repos (
    worker_id TEXT NOT NULL,
    repo_id TEXT NOT NULL,
    registered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (worker_id, repo_id),
    FOREIGN KEY (worker_id) REFERENCES workers(id),
    FOREIGN KEY (repo_id) REFERENCES repos(id)
);

-- Collaborator cache (optional - could be in-memory only)
CREATE TABLE collaborator_cache (
    forge_type TEXT NOT NULL,
    repo_full_name TEXT NOT NULL,  -- "owner/repo"
    username TEXT NOT NULL,
    access_level TEXT NOT NULL,    -- "write", "read", "none"
    cached_at TIMESTAMP NOT NULL,
    PRIMARY KEY (forge_type, repo_full_name, username)
);
```

## API Changes

### GET /api/workers

Response changes to include registered repos:

```json
{
  "workers": [
    {
      "id": "user:alice:macbook",
      "name": "alice's MacBook",
      "mode": "personal",
      "owner": "alice",
      "connected": true,
      "status": "online"
    },
    {
      "id": "w_abc123",
      "name": "CI Server",
      "mode": "shared",
      "owner": "alice",
      "repos": ["acme/backend", "acme/frontend"],
      "connected": true,
      "status": "online",
      "active_jobs": 2
    }
  ]
}
```

Visibility filtering:
```go
func (h *APIHandler) listWorkers(w http.ResponseWriter, r *http.Request) {
    user := h.auth.GetUser(r)
    if user == "" {
        // Unauthenticated: no workers visible
        h.writeJSON(w, map[string]any{"workers": []any{}})
        return
    }

    var visible []Worker
    for _, worker := range allWorkers {
        if worker.Mode == "personal" {
            if worker.OwnerName == user {
                visible = append(visible, worker)
            }
        } else { // shared
            // Check if user has write access to any registered repo
            for _, repo := range worker.RegisteredRepos {
                if h.collabCache.HasWriteAccess(repo.Forge, repo.FullName, user) {
                    visible = append(visible, worker)
                    break
                }
            }
        }
    }

    h.writeJSON(w, map[string]any{"workers": visible})
}
```

### POST /api/workers (future: register shared worker via API)

```json
{
  "name": "CI Server",
  "mode": "shared",
  "repos": ["acme/backend", "acme/frontend"],
  "labels": ["linux", "docker"]
}
```

Only the worker owner can register. Repos must already be connected to Cinch.

## CLI Changes

### Shared Worker Registration

```bash
# Register shared worker for specific repos
cinch worker --shared --repos acme/backend,acme/frontend

# Or interactively select repos
cinch worker --shared
# → Select repos to serve:
#   [x] acme/backend
#   [x] acme/frontend
#   [ ] personal/my-project
```

### Worker Output

```
[worker] Connected as alice (shared mode)
[worker] Serving repos:
         - acme/backend (3 collaborators)
         - acme/frontend (3 collaborators)
[worker] Waiting for jobs...
```

## Web UI Changes

### Workers Page

Show repos for shared workers:

```
Workers

┌─────────────────────────────────────────────────────────┐
│ alice's MacBook          personal    ● online    0 jobs │
├─────────────────────────────────────────────────────────┤
│ CI Server                shared      ● online    2 jobs │
│ Repos: acme/backend, acme/frontend                      │
│ Owner: alice                                            │
└─────────────────────────────────────────────────────────┘
```

### Repo Page

Show which workers serve this repo:

```
acme/backend

Workers serving this repo:
  ● CI Server (shared, 2 jobs running)
  ○ bob's laptop (personal, offline)
```

## Security Considerations

### Forge Token Refresh

To query collaborator status, we need valid forge tokens. Options:

1. **GitHub App** - Installation tokens auto-refresh, scoped to repos
2. **User OAuth tokens** - May expire, need refresh flow
3. **Webhook-provided tokens** - Only available during webhook processing

Recommendation: Use GitHub App tokens for GitHub, stored OAuth for GitLab/Forgejo.

### Cache Poisoning

If an attacker can poison the collaborator cache, they could:
- Make themselves appear as a collaborator (gain access)
- Make real collaborators appear as non-collaborators (DoS)

Mitigation:
- Only cache results from authenticated forge API calls
- Short TTL (5 minutes)
- Log cache updates for audit

### Removed Collaborators

If someone is removed as a collaborator:
- They can still use shared workers until cache expires (5 min)
- Their in-progress jobs continue (don't kill running jobs)
- New jobs fail dispatch

This is acceptable. GitHub Actions has similar behavior.

## Implementation Order

1. **Fix immediate bug** - Empty ownerName visibility leak
2. **Add repo list to worker registration** - Protocol + storage
3. **Implement collaborator cache** - In-memory first
4. **Add forge API clients** - GitHub, GitLab, Forgejo
5. **Update worker list API** - Filter by repo access
6. **CLI changes** - --repos flag for shared workers
7. **Web UI** - Show repos on workers, workers on repos

## Open Questions

### 1. Billing

This design solves visibility and access. But who pays?

Current model (implicit): The user who runs `cinch worker` pays for their usage.

But for shared workers:
- Alice runs a shared worker for `acme/backend`
- Bob pushes to `acme/backend`, job runs on Alice's worker
- Who gets billed? Alice (worker owner) or Bob (job author)?

Options:
- **Bill worker owner** - Alice pays for all jobs on her worker
- **Bill job author** - Bob pays for his jobs, regardless of worker
- **Bill repo owner** - The org/owner of acme/backend pays
- **Bill by seat** - Anyone who triggers jobs in a billing period

See: Billing design (separate doc needed)

### 2. Repo Removal

What happens when a repo is removed from Cinch?
- Shared workers registered for that repo: keep registration but skip for dispatch
- Or: automatically unregister workers from removed repos

### 3. Cross-Forge Workers

Can a shared worker serve repos from multiple forges?
- e.g., serve both `github.com/acme/app` and `gitlab.com/acme/api`

Probably yes - worker just needs valid clone URLs. But visibility gets complex:
- Check collaborator status on each forge separately
- User might have access on GitHub but not GitLab

### 4. Org-Level Workers

GitHub Actions supports org-level runners that serve all org repos. Do we want this?

Could be: `cinch worker --shared --org acme` which auto-registers for all acme/* repos.

Defer to future design.
