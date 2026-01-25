# Forgejo / Gitea Integration

**Status: Implemented (Manual Flow Only)**

Forgejo and Gitea share the same API (Forgejo is a fork of Gitea), so one implementation covers both.

## The Grug Goal

Users should click one button and be done. No copying tokens. No manual webhook setup.

**Can Forgejo/Gitea match this?** Almost, but not quite. Close enough that it hurts.

## Why Forgejo/Gitea Falls Short

### The Problem: Token API Requires Password Auth

Forgejo/Gitea has an API to [create tokens](https://docs.gitea.com/development/api-usage):

```bash
curl -H "Content-Type: application/json" \
  -d '{"name":"cinch","scopes":["repo"]}' \
  -u username:password \
  https://forgejo.example/api/v1/users/{username}/tokens
```

See that `-u username:password`? **OAuth tokens don't work for this endpoint.** You need the user's actual password.

This is a [known limitation](https://github.com/go-gitea/gitea/issues/21186). The token creation endpoint explicitly requires basic auth.

### What We CAN Do with OAuth

If a user does OAuth with Cinch, we can:
- ✓ Create webhooks via API
- ✓ Create deploy keys (read-only SSH clone access)
- ✓ Post commit statuses
- ✗ Create a token that outlives the OAuth session

### The Catch

OAuth tokens expire (default 1 hour). To keep working, we'd need to store the refresh token forever - same problem as Bitbucket.

**Unlike GitLab**, we can't use OAuth to create a project-scoped token that lives independently.

## What's Actually Implemented

Currently: **Manual flow only**

```bash
# User creates token in Forgejo/Gitea UI
# User configures webhook in Forgejo/Gitea UI
# User runs:
cinch repo add \
  --forge forgejo \
  --url https://codeberg.org/owner/repo.git \
  --token {token}
```

This works. It's not grug, but it works.

## Could We Do Better?

### Option A: OAuth + Keep Refresh Token (Like Bitbucket)

```
1. User clicks "Add Forgejo"
2. OAuth dance
3. Cinch stores refresh token
4. Cinch creates webhook via API
5. Use OAuth token for clone + status
6. Refresh every hour forever
```

**Problem:** Each Forgejo/Gitea instance is separate. We'd need:
- Instance-specific OAuth app registration
- Or users "bring their own" OAuth credentials
- Still holding account-level creds forever

**Not great, but possible.**

### Option B: OAuth + Deploy Key (Partial Automation)

```
1. User clicks "Add Forgejo"
2. OAuth dance
3. Cinch creates webhook via API
4. Cinch creates deploy key (SSH, read-only)
5. For status posting... still need OAuth refresh token
```

Deploy keys let us clone without the user's token, but we still need a token for posting status.

**Doesn't solve the core problem.**

### Option C: Ask Forgejo to Fix Their API

The real fix: allow OAuth tokens to create access tokens.

```
POST /api/v1/users/{username}/tokens
Authorization: Bearer {oauth_token}  ← Should work but doesn't

{
  "name": "cinch-bot",
  "scopes": ["repo"]
}
```

If this worked, we could:
1. OAuth dance
2. Create long-lived token via API
3. Throw away OAuth creds
4. Use the created token forever

**This is what GitLab's Project Access Token API does.**

Forgejo/Gitea issue: https://github.com/go-gitea/gitea/issues/21186

## Current Implementation Details

### Webhook Handling

```go
// internal/forge/forgejo.go

func (f *Forgejo) Identify(r *http.Request) bool {
    return r.Header.Get("X-Forgejo-Event") != "" ||
           r.Header.Get("X-Gitea-Event") != ""
}

func (f *Forgejo) ParsePush(r *http.Request, secret string) (*PushEvent, error) {
    // Check event header (try both)
    event := r.Header.Get("X-Forgejo-Event")
    if event == "" {
        event = r.Header.Get("X-Gitea-Event")
    }

    // Verify HMAC-SHA256 signature
    sig := r.Header.Get("X-Forgejo-Signature")
    if sig == "" {
        sig = r.Header.Get("X-Gitea-Signature")
    }
    // ... verify and parse
}
```

**Webhook security:** HMAC-SHA256, same as GitHub. This part is good.

### Commit Status API

```
POST /api/v1/repos/{owner}/{repo}/statuses/{sha}
Authorization: token {access_token}
Content-Type: application/json

{
  "state": "success",
  "context": "cinch",
  "description": "Build passed in 2m 34s",
  "target_url": "https://cinch.sh/jobs/123"
}
```

States: `pending`, `success`, `error`, `failure`, `warning`

### Releases API

```
# Create release
POST /api/v1/repos/{owner}/{repo}/releases
Authorization: token {access_token}

{
  "tag_name": "v1.0.0",
  "name": "v1.0.0",
  "draft": false,
  "prerelease": false
}

# Upload asset
POST /api/v1/repos/{owner}/{repo}/releases/{id}/assets?name={filename}
Authorization: token {access_token}
Content-Type: application/octet-stream

<binary data>
```

**Releases work well.** Same API design as GitHub.

## The Self-Hosted Complexity

Every Forgejo/Gitea instance is independent:
- codeberg.org
- gitea.com
- git.mycompany.com
- git.someotherthing.org

There's no central "Forgejo Inc" that manages OAuth apps.

**For OAuth to work, either:**
1. User registers OAuth app on their instance, gives us credentials
2. We maintain a list of "known instances" with pre-registered apps
3. Instance admins install a Cinch OAuth app

None of these are grug.

## Prominent Instances

| Instance | Type | Notes |
|----------|------|-------|
| codeberg.org | Forgejo | Largest, 300k+ repos, community-run |
| gitea.com | Gitea | Official Gitea hosting |
| Self-hosted | Both | Many companies/individuals |

Codeberg is the only instance big enough that pre-registering an OAuth app might be worth it.

## Implementation Plan

### Current: Manual Flow (Done)

- ✓ Webhook parsing
- ✓ Signature verification
- ✓ Commit status posting
- ✓ Releases
- ✓ `cinch repo add --forge forgejo`

### Future: OAuth Flow (Maybe)

1. OAuth support for Codeberg specifically
2. "Bring your own OAuth app" for other instances
3. Store refresh tokens (reluctantly)
4. Auto-create webhooks

### Future: Push for API Fix

1. File issue/PR with Forgejo to allow OAuth token creation
2. If accepted, implement proper grug flow
3. This is the real solution

## Database Schema

```sql
-- Current: manual token
repos.forge_type = 'forgejo' or 'gitea'
repos.forge_token = 'user_provided_token'
repos.webhook_secret = 'shared_secret_for_hmac'
repos.clone_url = 'https://codeberg.org/owner/repo.git'
repos.html_url = 'https://codeberg.org/owner/repo'

-- Future OAuth: would add
repos.forge_refresh_token = 'encrypted_refresh'
repos.forge_instance_url = 'https://codeberg.org'
```

## Error Messages

```
# Manual setup
"Forgejo/Gitea requires manual setup:
 1. Create an access token in Settings → Applications
 2. Create a webhook pointing to https://cinch.sh/webhooks
 3. Run: cinch repo add --forge forgejo --url {url} --token {token}"

# Why no one-click (if asked)
"Forgejo/Gitea's API doesn't allow creating tokens via OAuth.
 We'd need your password, and that's not happening.
 Manual token setup is the secure option."
```

## Forgejo vs Gitea

| Aspect | Forgejo | Gitea |
|--------|---------|-------|
| Governance | Community nonprofit | For-profit company |
| License | GPL v3 | MIT |
| Header names | `X-Forgejo-*` | `X-Gitea-*` |
| API | Same | Same |
| Our code | `IsGitea` flag | `IsGitea` flag |

In Cinch, both are handled by the same `Forgejo` struct with an `IsGitea` boolean.

## Summary

| Aspect | Grug? | Notes |
|--------|-------|-------|
| Setup flow | ✗ Manual | Token + webhook created by user |
| Token management | ✓ Simple | User's token, stored directly |
| Webhook setup | ✗ Manual | User configures in forge UI |
| Token refresh | ✓ None | Tokens don't expire |
| Releases | ✓ Good | Same API as GitHub |
| Self-hosted | ~ Fragmented | Each instance is separate |

**Forgejo/Gitea gets a C+. Would be a B+ if OAuth could create tokens.**

## The Irony

Forgejo/Gitea users are exactly our target audience - self-hosters, open source enthusiasts, people who care about owning their infrastructure.

And yet their platform doesn't support the seamless integration we want to give them.

**The fix is upstream.** If Forgejo accepted a PR to allow OAuth-based token creation, we could give these users the GitHub App experience.

Until then: manual setup with clear instructions.
