# GitLab Integration

**Status: Not Implemented**

## The Grug Goal

Users should click one button and be done. No copying tokens. No manual webhook setup. This is what GitHub Apps give us.

**Can GitLab match this?** Almost. With a trick.

## The GitLab Trick: Project Access Tokens

GitLab doesn't have "GitLab Apps" but it DOES have [Project Access Tokens](https://docs.gitlab.com/ee/user/project/settings/project_access_tokens/) - bot accounts scoped to a single project. And crucially, these can be **created via API**.

**The grug flow:**
```
1. User clicks "Add GitLab" in Cinch UI
2. OAuth dance (user authorizes Cinch)
3. Cinch uses user's OAuth token to:
   a. Create Project Access Token (POST /api/v4/projects/{id}/access_tokens)
   b. Create webhook (POST /api/v4/projects/{id}/hooks)
4. THROW AWAY user's OAuth token (don't store refresh token)
5. Store only the Project Access Token

When webhook fires:
- Use Project Access Token for clone + status posting
- Token belongs to PROJECT, not user
- User can revoke Cinch by deleting the bot in project settings
```

**This is as close to GitHub Apps as GitLab gets.**

## The Catches

### Catch 1: Premium Required on GitLab.com

[Project Access Tokens require Premium or Ultimate](https://docs.gitlab.com/ee/user/project/settings/project_access_tokens/#create-a-project-access-token) on GitLab.com (the SaaS).

On self-hosted GitLab: works on any tier.

**Impact:** Free-tier GitLab.com users can't get the grug experience. They fall back to manual token setup.

### Catch 2: 365-Day Max Expiry

Project Access Tokens expire. Max 365 days (400 in GitLab 17.6+).

**Options:**
1. **Auto-rotate before expiry** - Cinch creates new token, deletes old one (requires storing user's OAuth refresh token... which defeats the point)
2. **Notify user to re-auth** - Email/UI warning when token is expiring
3. **Accept the annual re-auth** - User clicks "Add GitLab" once a year

Option 3 is probably fine. Annual re-auth is annoying but acceptable.

### Catch 3: Permission Level

To create a Project Access Token, user must have Maintainer role on the project. Most users who want CI probably have this, but it's a requirement.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         SETUP (one-time)                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  User clicks "Add GitLab"                                       │
│         │                                                       │
│         ▼                                                       │
│  ┌─────────────────┐    OAuth    ┌─────────────────┐           │
│  │   Cinch Web     │ ──────────► │    GitLab       │           │
│  │                 │ ◄────────── │                 │           │
│  └─────────────────┘  user token └─────────────────┘           │
│         │                                                       │
│         │ Use token once to:                                    │
│         ├──► Create Project Access Token (lives in project)     │
│         ├──► Create webhook                                     │
│         │                                                       │
│         ▼                                                       │
│  ┌─────────────────┐                                           │
│  │ Throw away      │  User's OAuth token is GONE               │
│  │ user token      │  Only Project Access Token stored         │
│  └─────────────────┘                                           │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                      RUNTIME (every push)                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  GitLab ──webhook──► Cinch                                      │
│                        │                                        │
│                        │ Look up Project Access Token           │
│                        ▼                                        │
│                   Clone repo (using PAT)                        │
│                   Run build                                     │
│                   Post status (using PAT)                       │
│                                                                 │
│  Project Access Token is PROJECT's credential, not user's       │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## Fallback: Manual Setup

For users who can't use the grug flow (free GitLab.com tier, weird permissions), we need manual setup:

```bash
# User creates token manually in GitLab UI
# User configures webhook manually in GitLab UI
# User runs:
cinch repo add \
  --forge gitlab \
  --url https://gitlab.com/owner/repo.git \
  --token glpat-xxxx
```

**This sucks but it's GitLab's fault, not ours.**

## API Details

### OAuth Application Setup

Register OAuth app in GitLab (Admin Area → Applications or User Settings → Applications):

```
Name: Cinch CI
Redirect URI: https://cinch.sh/auth/gitlab/callback
Scopes: api, read_repository
Trusted: Yes (skip user confirmation on re-auth)
```

For self-hosted GitLab: users would need to register the app on their instance, OR we support "bring your own OAuth app" config.

### OAuth Flow

```
1. Redirect to:
   GET https://gitlab.com/oauth/authorize
     ?client_id={app_id}
     &redirect_uri=https://cinch.sh/auth/gitlab/callback
     &response_type=code
     &scope=api+read_repository
     &state={csrf_token}

2. User authorizes, redirected back with code

3. Exchange code:
   POST https://gitlab.com/oauth/token
     client_id={app_id}
     client_secret={secret}
     code={code}
     grant_type=authorization_code
     redirect_uri=https://cinch.sh/auth/gitlab/callback

4. Receive:
   {
     "access_token": "xxx",      // 2 hour lifetime
     "refresh_token": "yyy",     // we will NOT store this
     "expires_in": 7200
   }
```

### Create Project Access Token

```
POST /api/v4/projects/{id}/access_tokens
Authorization: Bearer {user_oauth_token}
Content-Type: application/json

{
  "name": "cinch-ci",
  "scopes": ["api", "read_repository"],
  "access_level": 30,
  "expires_at": "2027-01-25"
}

Response:
{
  "id": 42,
  "name": "cinch-ci",
  "token": "glpat-xxxxxxxxxxxx",  // THIS is what we store
  "expires_at": "2027-01-25"
}
```

`access_level` 30 = Developer role (enough for status + clone).

### Create Webhook

```
POST /api/v4/projects/{id}/hooks
Authorization: Bearer {user_oauth_token}
Content-Type: application/json

{
  "url": "https://cinch.sh/webhooks/gitlab",
  "push_events": true,
  "tag_push_events": true,
  "token": "{generated_secret}",
  "enable_ssl_verification": true
}
```

### Post Commit Status

```
POST /api/v4/projects/{id}/statuses/{sha}
Authorization: Bearer {project_access_token}
Content-Type: application/json

{
  "state": "success",
  "context": "cinch",
  "description": "Build passed in 2m 34s",
  "target_url": "https://cinch.sh/jobs/123"
}
```

States: `pending`, `running`, `success`, `failed`, `canceled`

### Webhook Payload

```
POST /webhooks/gitlab
X-Gitlab-Event: Push Hook
X-Gitlab-Token: {webhook_secret}

{
  "object_kind": "push",
  "ref": "refs/heads/main",
  "checkout_sha": "abc1234...",
  "project": {
    "id": 123,
    "path_with_namespace": "owner/repo",
    "git_http_url": "https://gitlab.com/owner/repo.git",
    "visibility_level": 0
  },
  "commits": [...]
}
```

Webhook verification: compare `X-Gitlab-Token` header with stored secret.

## Releases

GitLab releases are a two-step process:

### Step 1: Upload to Generic Package Registry

```
PUT /api/v4/projects/{id}/packages/generic/cinch/{version}/{filename}
Authorization: Bearer {project_access_token}
Content-Type: application/octet-stream

<binary data>
```

### Step 2: Create Release with Links

```
POST /api/v4/projects/{id}/releases
Authorization: Bearer {project_access_token}
Content-Type: application/json

{
  "tag_name": "v1.0.0",
  "name": "v1.0.0",
  "assets": {
    "links": [
      {
        "name": "cinch-linux-amd64",
        "url": "https://gitlab.com/api/v4/projects/{id}/packages/generic/cinch/v1.0.0/cinch-linux-amd64",
        "link_type": "package"
      }
    ]
  }
}
```

**More annoying than GitHub but workable.**

## Implementation Plan

### Phase 1: Grug Flow (Premium GitLab)

1. OAuth application registration
2. OAuth callback handler
3. Project Access Token creation via API
4. Webhook creation via API
5. Commit status posting
6. "Add GitLab" button in web UI

### Phase 2: Fallback Flow

1. `cinch repo add --forge gitlab` for manual setup
2. Store user-provided token directly
3. Instruct user on manual webhook setup

### Phase 3: Releases

1. Generic package upload
2. Release creation with asset links
3. `cinch release` support for GitLab

### Phase 4: Self-Hosted Support

1. Custom instance URL support
2. Per-instance OAuth app config (or "bring your own")
3. Handle API version differences

## Database Changes

```sql
-- For grug flow, store Project Access Token
repos.forge_token = 'glpat-xxxxxxxxxxxx'  -- Project Access Token
repos.forge_token_expires_at = '2027-01-25'  -- For rotation warnings

-- No refresh token stored - we threw it away
```

## Self-Hosted GitLab

For self-hosted instances:
- No Premium requirement (Project Access Tokens work on all tiers)
- User needs to configure OAuth app on their instance OR
- Cinch supports "bring your own OAuth credentials"

Config could look like:
```bash
cinch repo add \
  --forge gitlab \
  --gitlab-url https://gitlab.mycompany.com \
  --url https://gitlab.mycompany.com/team/repo.git
```

## Error Messages

When things go wrong, be clear about whose fault it is:

```
# Free tier GitLab.com
"GitLab.com requires Premium for automatic setup.
 Either upgrade, or set up manually: cinch repo add --help"

# Missing permissions
"You need Maintainer access to this project for automatic setup.
 Ask a maintainer to add Cinch, or set up manually."

# Token expired
"Your Cinch access to owner/repo expired.
 Click here to reconnect: [Re-authorize GitLab]"
```

## Summary

| Aspect | Grug? | Notes |
|--------|-------|-------|
| Setup flow | ✓ One click | OAuth → auto-create PAT + webhook |
| Token management | ✓ Invisible | Project Access Token, user never sees it |
| Webhook setup | ✓ Automatic | Created via API |
| Token refresh | ✗ Annual | 365-day max, need re-auth or warning |
| Free tier | ✗ Manual | GitLab.com Premium required for PAT API |
| Self-hosted | ✓ One click | No tier restriction |

**GitLab gets a B+. Would be an A if tokens didn't expire and free tier worked.**
