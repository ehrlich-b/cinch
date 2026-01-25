# GitHub Integration

**Status: Implemented**

This documents the current implementation, not aspirational features.

## The Grug Test: PASSED

**GitHub is the only forge that gets this right.**

GitHub Apps are the gold standard for third-party integrations:
- User clicks one button
- GitHub configures webhooks automatically
- Cinch never sees user credentials
- Service owns its own private key

No other forge has this model. They all require either storing user tokens forever or manual setup.

## Architecture Overview

Cinch uses a **GitHub App** for GitHub integration. This was chosen because:

1. **No token passing** - Users approve the app in GitHub's UI, Cinch never sees user credentials
2. **Auto-configured webhooks** - GitHub manages webhook delivery, no manual setup
3. **Scoped permissions** - Only request what we need
4. **Short-lived tokens** - Installation tokens expire in 1 hour, automatically refreshed

## User Flow

```
1. User visits cinch.sh, clicks "Add to GitHub"
2. Redirected to GitHub App installation page
3. User selects org/account and repos to grant access
4. GitHub sends webhooks directly to Cinch (auto-configured)
5. Done - no manual webhook setup, no copying tokens
```

## What's Implemented

### 1. Build Verification on Commit

**Mechanism:** GitHub Checks API (not just commit statuses)

When a push webhook arrives:
1. Cinch creates a **Check Run** via `POST /repos/{owner}/{repo}/check-runs`
2. Check Run starts as `queued`
3. When worker picks up job: updated to `in_progress`
4. When job completes: updated to `completed` with `conclusion: success|failure`

The Check Run includes:
- Link to Cinch job page (`details_url`)
- Build logs embedded in the Check output (truncated to 60k chars)
- Timestamps for started/completed

**Why Checks API over Status API:**
- Richer UI in GitHub
- Can include build logs directly
- Shows timing information
- Better for merge blocking (branch protection rules)

**Code:** `internal/server/github_app.go`
- `CreateCheckRun()` - creates initial check run
- `UpdateCheckRunInProgress()` - marks as running
- `UpdateCheckRun()` - marks complete with logs

### 2. Build Verification for PRs / Merge Blocking

**Current state:** Works automatically via Check Runs

When someone pushes to a PR branch:
1. Push webhook fires
2. Cinch creates Check Run on the head commit
3. GitHub shows the check on the PR

**Branch protection:** Users configure this in GitHub settings:
- Require status checks to pass before merging
- Select "cinch" as a required check

**Not implemented:**
- Pull request webhooks (we only handle push)
- PR-specific features (commenting on PRs, etc.)

### 3. Releases / Artifacts

**Mechanism:** `cinch release` CLI command

The worker runs with environment variables including `CINCH_FORGE_TOKEN` (the installation token). The release command uses this to:

1. Create release via `POST /repos/{owner}/{repo}/releases`
2. Upload assets via `POST {upload_url}?name={filename}`

**Code:** `internal/cli/release.go` - `releaseGitHub()`

**What works:**
- Create release from tag
- Upload binary assets
- Auto-detect forge/tag/repo from environment
- Draft and prerelease flags

**Environment variables provided to jobs:**
```bash
CINCH_FORGE=github
CINCH_TAG=v1.0.0           # Only on tag pushes
CINCH_FORGE_TOKEN=ghs_xxx  # Installation token
GITHUB_TOKEN=ghs_xxx       # Alias for compatibility
CINCH_REPO=https://github.com/owner/repo.git
```

### 4. Authentication Flow

**GitHub App Authentication:**

```
                     GitHub App (Cinch)
                            |
                            v
            +-------------------------------+
            |  App Private Key (RSA)        |
            |  - Stored on Cinch server     |
            |  - Never leaves server        |
            +-------------------------------+
                            |
                            | Sign JWT
                            v
            +-------------------------------+
            |  App JWT (10 min lifetime)    |
            +-------------------------------+
                            |
                            | POST /app/installations/{id}/access_tokens
                            v
            +-------------------------------+
            |  Installation Token (1 hr)    |
            |  - Scoped to installed repos  |
            |  - Used for: clone, status,   |
            |    releases, checks           |
            +-------------------------------+
```

**Code:** `internal/server/github_app.go`
- `createAppJWT()` - signs JWT with private key
- `GetInstallationToken()` - exchanges JWT for installation token
- Token cache with 5-minute refresh buffer

### 5. Webhook Handling

**Endpoint:** `POST /webhooks/github-app`

**Events handled:**
- `push` - Creates job, check run, dispatches to worker
- `installation` - Logged (repos auto-created on first push)
- `installation_repositories` - Logged
- `ping` - Returns OK

**Signature verification:** HMAC-SHA256 with webhook secret

**Code:** `internal/server/github_app.go` - `ServeHTTP()`

## Permissions Required

**Repository permissions:**
- `contents: write` - Clone repos AND create releases
- `checks: write` - Create/update check runs
- `statuses: write` - Post commit statuses (backup)
- `metadata: read` - Required for all apps

**Events subscribed:**
- `push`
- `installation`
- `installation_repositories`

## What's NOT Implemented

1. **PR webhooks** - Don't handle `pull_request` events separately (push covers PR commits)
2. **PR comments** - No build summary comments on PRs
3. **Checks re-run** - Can't re-trigger from GitHub UI
4. **Multiple check runs** - Always creates one check called "cinch"
5. **Fine-grained permissions** - All repos in installation get same access
6. **Workflow integration** - No GitHub Actions interop

## Configuration

**Environment variables for Cinch server:**
```bash
CINCH_GITHUB_APP_ID=123456
CINCH_GITHUB_APP_PRIVATE_KEY="-----BEGIN RSA PRIVATE KEY-----\n..."
CINCH_GITHUB_APP_WEBHOOK_SECRET=whsec_xxx
```

## Database Schema

```sql
-- Jobs table includes GitHub-specific fields
jobs.installation_id  -- GitHub App installation ID
jobs.check_run_id     -- GitHub Check Run ID (for updates)
```

## API Endpoints Used

| Operation | Endpoint | When |
|-----------|----------|------|
| Create check run | `POST /repos/{owner}/{repo}/check-runs` | Job created |
| Update check run | `PATCH /repos/{owner}/{repo}/check-runs/{id}` | Job status change |
| Get installation token | `POST /app/installations/{id}/access_tokens` | On demand |
| Create release | `POST /repos/{owner}/{repo}/releases` | `cinch release` |
| Upload asset | `POST {upload_url}` | `cinch release` |
| Post status | `POST /repos/{owner}/{repo}/statuses/{sha}` | Fallback |

## Rate Limits

- 5000 requests/hour per installation token
- Installation token requests: 15,000/hour per app
- Not currently handling rate limit errors gracefully

## Summary

| Aspect | Grug? | Notes |
|--------|-------|-------|
| Setup flow | ✓ One click | Install app, select repos, done |
| Token management | ✓ Invisible | Installation tokens auto-generated |
| Webhook setup | ✓ Automatic | GitHub configures them |
| Token refresh | ✓ Automatic | 1 hour tokens, refresh on demand |
| Releases | ✓ Excellent | Native API |
| Self-hosted | ✓ Works | GitHub Enterprise supported |

**GitHub gets an A. This is how it should work everywhere.**
