# Forgejo / Gitea Integration

**Status: Implemented (Manual Flow Only)**

Forgejo and Gitea share the same API (Forgejo is a fork of Gitea), so one implementation covers both.

## The Grug Goal

Users should click one button and be done. No copying tokens. No manual webhook setup.

**Can Forgejo/Gitea match this?** No, but we can get close with a hybrid approach.

## Why Full Automation Is Impossible

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
- ✓ Post commit statuses (while token is valid)
- ✗ Create a token that outlives the OAuth session

### What We NEED for Runtime

To post commit statuses, we need `write:repository` scope ([source](https://github.com/go-gitea/gitea/blob/main/routers/api/v1/api.go)). Options:
- **Personal Access Token** - Doesn't expire, user controls it
- **OAuth token** - Expires every 1-2 hours, requires refresh token storage

### Why NOT Store Refresh Tokens (The Woodpecker Approach)

[Woodpecker CI stores OAuth refresh tokens](https://woodpecker-ci.org/docs/administration/configuration/forges/forgejo) and refreshes forever. This works, but:
- We hold account-level credentials forever
- User can't easily see what we have access to
- If Forgejo OAuth scopes change, our stored tokens may break
- Less transparent than "here's the one token I gave you"

**Note:** OAuth scopes in Forgejo [aren't fully implemented](https://forgejo.org/docs/latest/user/oauth2-provider/) - tokens currently grant full admin access. A [PR to improve this](https://codeberg.org/forgejo/forgejo/pulls/6197) was closed.

## The Hybrid Approach (Best We Can Do)

### For Codeberg (and other major instances with our OAuth app)

```
┌─────────────────────────────────────────────────────────────────┐
│                    SETUP (hybrid flow)                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  User clicks "Add Codeberg"                                     │
│         │                                                       │
│         ▼                                                       │
│  ┌─────────────────┐    OAuth    ┌─────────────────┐           │
│  │   Cinch Web     │ ──────────► │    Codeberg     │           │
│  │                 │ ◄────────── │                 │           │
│  └─────────────────┘   token     └─────────────────┘           │
│         │                                                       │
│         │ Use token ONCE to:                                    │
│         ├──► Create webhook (automated!)                        │
│         ├──► Get repo metadata                                  │
│         │                                                       │
│         ▼                                                       │
│  ┌─────────────────┐                                           │
│  │ THROW AWAY      │  Don't store refresh token                │
│  │ OAuth token     │                                           │
│  └─────────────────┘                                           │
│         │                                                       │
│         ▼                                                       │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  "One more step: Create a token for status posting"     │   │
│  │                                                         │   │
│  │  1. Click here: [link to user's token settings]         │   │
│  │  2. Create token with "repository" scope                │   │
│  │  3. Paste it here: [___________________________]        │   │
│  │                                                         │   │
│  └─────────────────────────────────────────────────────────┘   │
│         │                                                       │
│         ▼                                                       │
│  Store user's PAT (doesn't expire)                             │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

**Why this works:**
- Webhook setup is automated (fewer errors)
- We don't store refresh tokens (more secure)
- PAT doesn't expire (no refresh dance)
- User knows exactly what credential we hold
- User can revoke it anytime from Forgejo settings

**Comparison:**

| Aspect | Full manual | Hybrid | Store refresh (Woodpecker) |
|--------|-------------|--------|----------------------------|
| Webhook setup | Manual | Automated | Automated |
| What we store | PAT | PAT | Refresh token (account-level) |
| Token expiry | Never | Never | Must refresh hourly |
| Transparency | Clear | Clear | Opaque |
| User effort | 3 steps | 2 steps | 1 step |

### For Self-Hosted (no OAuth app registered)

Fall back to full manual, but with the same "paste token here" UI:

```bash
# Cinch shows:
"We don't have an OAuth app for git.yourcompany.com.
 Please set up manually:

 1. Create webhook: [shows URL and secret]
 2. Create token with 'repository' scope
 3. Paste token here: [___________________________]"
```

Same UI, just without the automated webhook step.

## What's Actually Implemented (Current)

Currently: **Manual flow only** (CLI-based)

```bash
# User creates token in Forgejo/Gitea UI
# User configures webhook in Forgejo/Gitea UI
# User runs:
cinch repo add \
  --forge forgejo \
  --url https://codeberg.org/owner/repo.git \
  --token {token}
```

This works but requires users to figure out webhook setup on their own.

## How Other CI Systems Do It

| System | Approach | Token Storage | Our Take |
|--------|----------|---------------|----------|
| [Woodpecker CI](https://woodpecker-ci.org/docs/administration/configuration/forges/forgejo) | OAuth + refresh forever | Account-level creds | Too much trust |
| [Jenkins](https://plugins.jenkins.io/gitea/) | Manual PAT only | User-provided token | Works, but no automation |
| **Cinch (proposed)** | OAuth for setup + manual PAT | User-provided token | Best of both |

Jenkins [requires `write:repository` scope](https://deepwiki.com/jenkinsci/gitea-plugin/4.5-credentials-and-authentication) for status posting, same as us.

## The Dream: Upstream Fix

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

There's also [discussion about enhanced ephemeral credentials](https://codeberg.org/forgejo/forgejo/issues/3571) (like GitLab's `$CI_JOB_TOKEN`) but that's for Forgejo Actions, not external CI.

## API Details

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

**Webhook security:** HMAC-SHA256, same as GitHub.

### Create Webhook (OAuth setup step)

```
POST /api/v1/repos/{owner}/{repo}/hooks
Authorization: token {oauth_access_token}
Content-Type: application/json

{
  "type": "forgejo",
  "config": {
    "url": "https://cinch.sh/webhooks/forgejo",
    "content_type": "json",
    "secret": "{generated_secret}"
  },
  "events": ["push"],
  "active": true
}
```

This works with OAuth tokens. We use this ONCE during setup, then throw away the OAuth token.

### Commit Status API (runtime)

```
POST /api/v1/repos/{owner}/{repo}/statuses/{sha}
Authorization: token {personal_access_token}
Content-Type: application/json

{
  "state": "success",
  "context": "cinch",
  "description": "Build passed in 2m 34s",
  "target_url": "https://cinch.sh/jobs/123"
}
```

States: `pending`, `success`, `error`, `failure`, `warning`

**Requires `write:repository` scope** on the PAT.

### Releases API

```
# Create release
POST /api/v1/repos/{owner}/{repo}/releases
Authorization: token {personal_access_token}

{
  "tag_name": "v1.0.0",
  "name": "v1.0.0",
  "draft": false,
  "prerelease": false
}

# Upload asset
POST /api/v1/repos/{owner}/{repo}/releases/{id}/assets?name={filename}
Authorization: token {personal_access_token}
Content-Type: application/octet-stream

<binary data>
```

Same API design as GitHub. Also requires `write:repository` scope.

## Instance Strategy

Every Forgejo/Gitea instance is independent - there's no central authority.

### Tier 1: Codeberg (OAuth app pre-registered)

We register an OAuth app on codeberg.org. Users get the hybrid flow:
- Automated webhook setup
- Manual PAT for status posting

### Tier 2: Other Major Instances (gitea.com, etc.)

Consider registering OAuth apps if demand warrants. Same hybrid flow.

### Tier 3: Self-Hosted (no OAuth app)

Fall back to full manual with good UI:
- Show webhook URL and secret
- Instructions for PAT creation
- Same "paste token here" UI

### Instance Detection

```
codeberg.org     → Use Cinch's Codeberg OAuth app
gitea.com        → Use Cinch's Gitea.com OAuth app (if registered)
git.example.com  → Manual setup (no OAuth app)
```

## Implementation Plan

### Phase 1: Improve Manual Flow (Current Priority)

1. Web UI for "Add Forgejo repo"
2. Show webhook URL and secret clearly
3. "Paste token here" text input
4. Step-by-step instructions with screenshots
5. Link directly to token creation page

### Phase 2: Hybrid Flow for Codeberg

1. Register OAuth app on Codeberg
2. OAuth callback handler
3. Use OAuth to create webhook
4. Throw away OAuth token (don't store refresh)
5. Prompt for manual PAT

### Phase 3: Other Instances

1. Register on gitea.com if demand exists
2. "Bring your own OAuth app" for enterprises
3. Instance URL detection and routing

## Database Schema

```sql
-- Store user's PAT (doesn't expire)
repos.forge_type = 'forgejo' or 'gitea'
repos.forge_token = 'user_provided_pat'  -- encrypted
repos.webhook_secret = 'shared_secret_for_hmac'
repos.clone_url = 'https://codeberg.org/owner/repo.git'
repos.html_url = 'https://codeberg.org/owner/repo'
repos.forge_instance_url = 'https://codeberg.org'

-- NO refresh token stored
-- repos.forge_refresh_token is NOT used
```

## Error Messages

```
# Hybrid flow (Codeberg)
"Almost done! We've set up the webhook.

 One more step: Create a token for build status updates.

 1. Go to: https://codeberg.org/user/settings/applications
 2. Click 'Generate New Token'
 3. Name it 'Cinch CI' (or whatever you like)
 4. Select scope: 'repository' (read & write)
 5. Paste it here: [___________________________]"

# Manual flow (self-hosted)
"We don't have automatic setup for git.yourcompany.com.

 1. Create webhook:
    URL: https://cinch.sh/webhooks/forgejo
    Secret: {generated_secret}
    Events: Push

 2. Create access token:
    Go to: Settings → Applications → Generate New Token
    Scope: repository (read & write)

 3. Paste token here: [___________________________]"

# Why we need a separate token (if asked)
"Forgejo's API doesn't let us create tokens automatically.
 Your token is stored securely and only used to post build status.
 You can revoke it anytime from Forgejo settings."
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

| Aspect | Current | With Hybrid Flow |
|--------|---------|------------------|
| Setup flow | Full manual | Webhook automated, token manual |
| Token storage | PAT | PAT (same) |
| Webhook setup | Manual | Automated (on known instances) |
| Token refresh | None needed | None needed |
| Releases | Good | Good |
| Security | Good | Good (no refresh token stored) |

**Forgejo/Gitea gets a B- with the hybrid flow.** Better than full manual, more secure than storing refresh tokens.

## The Target User

Forgejo/Gitea users are exactly our target audience - self-hosters, open source enthusiasts, people who care about owning their infrastructure.

The hybrid approach respects this:
- We automate what we can (webhooks)
- We're transparent about what we store (their PAT)
- We don't hold account-level OAuth credentials
- User can revoke access anytime from Forgejo settings

## Future: The Real Fix

**The fix is upstream.** If Forgejo allowed OAuth-based token creation:

```
POST /api/v1/users/{username}/tokens
Authorization: Bearer {oauth_token}  ← Should work but doesn't
```

Then we could:
1. OAuth dance
2. Create scoped token via API
3. Throw away OAuth
4. Full grug experience

Tracked at: https://github.com/go-gitea/gitea/issues/21186

Until then: hybrid flow with clear instructions.
