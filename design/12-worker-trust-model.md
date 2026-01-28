# Design 12: Worker Trust Model

## Status: Draft

## Problem

Current dispatch model assumes "repo access = trusted to run any code." This breaks for:

1. **Fork PRs**: Random person's code dispatched to maintainer's worker (backdoor)
2. **Team repos**: Teammate's push runs on my laptop (unexpected)
3. **Shared infra**: No way to distinguish "my machine" from "team CI box"

The default should be safe: your worker only runs your code.

## Core Insight

Trust flows from **code authorship**, not repo membership.

| Who wrote the code? | Who should run it? |
|---------------------|-------------------|
| Me | My worker |
| My teammate | Their worker, OR shared worker if they don't have one |
| Random fork contributor | Their worker (their code, their machine) |

## Worker Modes

### Personal Worker (Default)

```bash
cinch worker
cinch worker -s
```

**Only runs code authored by the logged-in user.**

- Your pushes to any repo you have access to
- Your PRs (including to repos you don't own)
- NOT teammate's pushes, even on shared repos
- NOT fork PRs to your repos

This is the safe default for "running on my laptop."

### Shared Worker (Opt-in)

```bash
cinch worker --shared
```

**Runs code from trusted team members, with conditions:**

- Only if the author doesn't have their own personal worker online
- Respects repo-level collaborator trust
- Still won't run fork PRs without approval

This is for dedicated CI infrastructure: a VPS, a build server, a Mac Mini in the closet.

## Dispatch Algorithm

### Priority Order

```
Job arrives for repo R, authored by user A:

1. Is A's personal worker online?
   → Dispatch to A's worker

2. Is a shared worker for R online?
   → Is A a trusted collaborator of R?
     → Yes: Dispatch to shared worker
     → No (fork PR): Hold for approval

3. No eligible workers
   → Job stays queued (or times out)
```

### Fork PR Flow

```
Alice (external) opens PR to Bob's repo:

1. Alice's personal worker online?
   → Dispatch to Alice's worker
   → Alice runs her own code on her own machine
   → Results posted, Bob sees green/red check

2. Alice has no worker?
   → Post status: "⏳ Awaiting contributor CI"
   → Alice sees instructions: "Run `cinch worker -s` to provide CI"
   → Job held until Alice runs it OR Bob approves

3. Bob clicks "Run on shared worker" (explicit approval)
   → Job dispatched to Bob's shared worker
   → Bob accepted the risk
```

## Data Model Changes

### Job

```go
type Job struct {
    // ... existing fields ...

    // Author identity (GitHub/GitLab/etc username)
    Author      string
    AuthorID    string  // Forge user ID for reliable matching

    // Trust classification
    TrustLevel  TrustLevel  // owner, collaborator, external

    // Fork PR flag (already exists)
    IsFork      bool

    // Approval state for external PRs
    ApprovedBy  *string     // Username who approved team-worker execution
    ApprovedAt  *time.Time
}

type TrustLevel string
const (
    TrustOwner        TrustLevel = "owner"        // Repo owner
    TrustCollaborator TrustLevel = "collaborator" // Has write access
    TrustExternal     TrustLevel = "external"     // Fork PR, no write access
)
```

### Worker

```go
type WorkerInfo struct {
    // ... existing fields ...

    // Owner identity
    OwnerID       string  // User ID from auth token
    OwnerUsername string

    // Worker mode
    Mode          WorkerMode
}

type WorkerMode string
const (
    ModePersonal WorkerMode = "personal"  // Default: only my code
    ModeShared     WorkerMode = "team"      // Opt-in: team code
)
```

### Dispatch Logic

```go
func (h *Hub) canWorkerRunJob(w *WorkerInfo, j *Job) bool {
    // Must have repo access (existing check)
    if !w.HasRepoAccess(j.Repo) {
        return false
    }

    // Personal worker: only author's own code
    if w.Mode == ModePersonal {
        return j.AuthorID == w.OwnerID
    }

    // Team worker: collaborators, or approved externals
    if w.Mode == ModeShared {
        switch j.TrustLevel {
        case TrustOwner, TrustCollaborator:
            // Check if author has their own worker online
            if h.hasPersonalWorkerOnline(j.AuthorID) {
                return false  // Defer to their worker
            }
            return true
        case TrustExternal:
            return j.ApprovedBy != nil  // Must be explicitly approved
        }
    }

    return false
}

func (h *Hub) hasPersonalWorkerOnline(userID string) bool {
    for _, w := range h.workers {
        if w.OwnerID == userID && w.Mode == ModePersonal {
            return true
        }
    }
    return false
}
```

## Webhook Changes

### Determining Trust Level

```go
func determineTrustLevel(event PushEvent, repo *Repo) TrustLevel {
    if event.Sender == repo.Owner {
        return TrustOwner
    }
    if isCollaborator(event.Sender, repo) {
        return TrustCollaborator
    }
    return TrustExternal
}

func determinePRTrustLevel(event PullRequestEvent) TrustLevel {
    if event.IsFork {
        return TrustExternal
    }
    // Same-repo PR from collaborator
    return TrustCollaborator
}
```

### Checking Collaborator Status

Need to query forge API to determine if sender has write access. Cache this.

```go
type CollaboratorCache interface {
    IsCollaborator(forgeType, repoURL, username string) (bool, error)
}
```

## CLI Changes

### Worker Command

```bash
# Personal mode (default) - only your code
cinch worker

# Team mode - shared infrastructure
cinch worker --shared

# Standalone with personal mode
cinch worker -s

# Standalone with team mode
cinch worker -s --shared
```

### Worker Output

Personal mode:
```
[worker] Connected as alice (personal mode)
[worker] Will run: your pushes and PRs only
[worker] Watching: 3 repos
```

Team mode:
```
[worker] Connected as alice (team mode)
[worker] Will run: team collaborator code (deferring to personal workers)
[worker] Watching: 3 repos
```

### Pending Jobs Display

```
[worker] You have 2 PRs awaiting CI:
         - owner/repo#42: "Add feature X"
         - other/project#17: "Fix bug Y"
[worker] These will run on your next job slot.
```

## Web UI Changes

### Job Status for Fork PRs

```
Status: ⏳ Awaiting Contributor CI

This PR is from a fork. The contributor can run CI by:

    curl -fsSL https://cinch.sh/install.sh | sh
    cinch login
    cinch worker -s

Or a maintainer can run it on team infrastructure:

    [Run on Team Worker]  (button, requires confirmation)
```

### Approval Flow

Maintainer clicks "Run on Team Worker":
```
⚠️  Run External Code?

This will execute code from @external-user on your team's
infrastructure. Only do this if you've reviewed the changes.

PR: owner/repo#42
Author: @external-user (no previous contributions)
Changed files: 3

[Cancel]  [Run on Team Worker]
```

### Worker List (future)

```
Workers Online

alice's MacBook (personal)     ● online    Jobs: 3
bob's MacBook (personal)       ● online    Jobs: 1
CI Server (team)               ● online    Jobs: 12
```

## API Changes

### Job Response

```json
{
  "id": "j_abc123",
  "repo": "owner/repo",
  "author": "alice",
  "trust_level": "external",
  "status": "pending_contributor",
  "approved_by": null,
  "can_approve": true
}
```

### Approve Endpoint

```
POST /api/jobs/{job_id}/approve

Authorization: Bearer <maintainer-token>

Response: 200 OK
```

### Worker Registration

```json
{
  "type": "register",
  "repos": ["owner/repo"],
  "labels": ["linux-amd64"],
  "mode": "personal"
}
```

## Status Check States

| State | GitHub Status | Description |
|-------|--------------|-------------|
| `queued` | pending | Waiting for worker |
| `pending_contributor` | pending | Fork PR, awaiting contributor CI |
| `running` | pending | Job in progress |
| `success` | success | Build passed |
| `failure` | failure | Build failed |
| `error` | error | Infrastructure error |

Status message for `pending_contributor`:
```
Awaiting contributor CI - run `cinch worker -s` to provide results
```

## Migration

### Database

```sql
ALTER TABLE jobs ADD COLUMN author TEXT;
ALTER TABLE jobs ADD COLUMN author_id TEXT;
ALTER TABLE jobs ADD COLUMN trust_level TEXT DEFAULT 'collaborator';
ALTER TABLE jobs ADD COLUMN approved_by TEXT;
ALTER TABLE jobs ADD COLUMN approved_at TIMESTAMP;
```

### Existing Jobs

Backfill `author` from commit info where possible. Default `trust_level` to `collaborator` for existing jobs (safe assumption for pre-fork-PR era).

## Security Considerations

### Personal Worker Isolation

Personal workers should never receive jobs they didn't author. The dispatch check is:

```go
if w.Mode == ModePersonal && j.AuthorID != w.OwnerID {
    return false  // Hard block, not just preference
}
```

### Approval Audit

Log all approvals for external PRs:
```
[audit] job=j_abc123 action=approved by=maintainer pr=owner/repo#42 author=external-user
```

### Token Scope

Workers only get tokens scoped to their authorized actions:
- Personal worker: only author's repos
- Team worker: only repos the shared worker is registered for

## Edge Cases

### Author Has Multiple Workers

If Alice has both a MacBook and a Linux server as personal workers:
- Job goes to first available
- Both are "her workers" running "her code"

### Author Starts Worker Mid-Job

Job queued, no worker. Author runs `cinch worker -s`:
- Worker comes online, sees pending job
- Claims it immediately
- Runs on author's machine

### Maintainer and Contributor Same Person

Alice opens PR to her own repo from a branch (not fork):
- `IsFork = false`
- `TrustLevel = owner` or `collaborator`
- Runs on her worker normally

### Collaborator Opens PR from Fork

Bob is a collaborator on Alice's repo but opens PR from his fork:
- `IsFork = true`
- But Bob has write access, so `TrustLevel = collaborator`?
- Decision: Still treat as external (fork = untrusted code path)
- Bob can run on his own worker, or Alice can approve

## Implementation Order

1. **Author tracking**: Add author/author_id to Job, populate from webhooks
2. **Trust level**: Add trust_level, compute from forge API
3. **Worker mode**: Add --shared flag, mode to registration
4. **Dispatch changes**: Implement trust-aware matching
5. **Pending contributor status**: New job state, status message
6. **Approval flow**: API endpoint, web UI
7. **CLI improvements**: Show pending PRs, better output

## Open Questions

1. **Flag name**: `--shared`, `--shared`, `--server`? Current preference: `--shared`

2. **Approval granularity**: Per-job, per-PR, per-contributor? Start with per-job.

3. **Trust cache TTL**: How long to cache collaborator status? 5 minutes?

4. **Org-level trust**: Should org members auto-trust each other? Probably yes for shared workers.

5. **Self-hosted**: How does this work for self-hosted Cinch? Simpler - you control everything.
