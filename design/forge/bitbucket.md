# Bitbucket Integration

**Status: Not Implemented**

## The Grug Goal

Users should click one button and be done. No copying tokens. No manual webhook setup.

**Can Bitbucket match this?** No. Bitbucket's platform fundamentally cannot support a grug-friendly flow.

## Why Bitbucket Fails the Grug Test

### The Problem: No API for Repository Access Tokens

Bitbucket has [Repository Access Tokens](https://support.atlassian.com/bitbucket-cloud/docs/repository-access-tokens/) - tokens scoped to a single repo (like GitLab's Project Access Tokens). This would be perfect.

**But you cannot create them via API.**

From Atlassian's docs: tokens can only be created through the web UI. There's no `POST /repositories/{workspace}/{repo}/access-tokens` endpoint.

This means even if a user does OAuth with Cinch:
- We CAN create webhooks via API ✓
- We CANNOT create a repository-scoped token ✗

So we're stuck with user-scoped tokens forever.

### The Options (All Bad)

**Option 1: Keep OAuth Refresh Tokens Forever**
```
User does OAuth → Cinch stores refresh token → Refresh every 2 hours forever
```
This works but violates the "throw away user creds" goal. We're permanently holding keys to the user's entire Bitbucket account.

**Option 2: Ask User to Create Token Manually**
```
User does OAuth → Cinch creates webhook
User manually creates Repository Access Token in Bitbucket UI → Copies to Cinch
```
Two-step process. Not grug.

**Option 3: Just Use OAuth Tokens**
```
User does OAuth → Cinch stores refresh token → Use for everything
```
Same as Option 1. We hold user's account-level access forever.

**There is no good option. This is Bitbucket's fault.**

## What We'll Implement

Given the constraints, the least-bad approach:

### Primary Flow: OAuth + Stored Refresh Token

```
1. User clicks "Add Bitbucket"
2. OAuth dance
3. Cinch stores refresh token (unfortunately)
4. Cinch creates webhook via API
5. Done (but we're holding account-level creds forever)
```

**The dirty secret:** This is what every Bitbucket integration does. CircleCI, Jenkins, everyone. They all hold refresh tokens because Bitbucket gives no alternative.

### Alternative Flow: Manual Token

```
1. User creates Repository Access Token in Bitbucket UI
2. User creates webhook in Bitbucket UI
3. User runs: cinch repo add --forge bitbucket --token {token}
```

For paranoid users who don't want Cinch holding account-level OAuth.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    SETUP (OAuth flow)                           │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  User clicks "Add Bitbucket"                                    │
│         │                                                       │
│         ▼                                                       │
│  ┌─────────────────┐    OAuth    ┌─────────────────┐           │
│  │   Cinch Web     │ ──────────► │   Bitbucket     │           │
│  │                 │ ◄────────── │                 │           │
│  └─────────────────┘   tokens    └─────────────────┘           │
│         │                                                       │
│         │ access_token (2hr) + refresh_token                    │
│         │                                                       │
│         ├──► Create webhook via API                             │
│         │                                                       │
│         ▼                                                       │
│  ┌─────────────────┐                                           │
│  │ Store refresh   │  ← We HAVE to store this                  │
│  │ token (sadly)   │    No way to create repo-scoped token     │
│  └─────────────────┘                                           │
│                                                                 │
│  ⚠️  We now hold keys to user's entire Bitbucket account       │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                      RUNTIME (every push)                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  Bitbucket ──webhook──► Cinch                                   │
│                           │                                     │
│                           │ Check if access_token expired       │
│                           │ If yes: refresh using refresh_token │
│                           ▼                                     │
│                      Clone repo                                 │
│                      Run build                                  │
│                      Post status                                │
│                                                                 │
│  Every 2 hours we refresh. Forever. Until user revokes.        │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## API Details

### OAuth Consumer Setup

Register OAuth Consumer in Bitbucket (Workspace settings → OAuth consumers):

```
Name: Cinch CI
Callback URL: https://cinch.sh/auth/bitbucket/callback
Permissions:
  - Repository: Write (for status)
  - Pull requests: Read
  - Webhooks: Read and Write
```

### OAuth Flow

```
1. Redirect to:
   GET https://bitbucket.org/site/oauth2/authorize
     ?client_id={consumer_key}
     &response_type=code

2. User authorizes, redirected back with code

3. Exchange code:
   POST https://bitbucket.org/site/oauth2/access_token
   Authorization: Basic {base64(consumer_key:consumer_secret)}
   Content-Type: application/x-www-form-urlencoded

   grant_type=authorization_code&code={code}

4. Receive:
   {
     "access_token": "xxx",
     "token_type": "Bearer",
     "expires_in": 7200,
     "refresh_token": "yyy",    // We MUST store this
     "scopes": "repository:write pullrequest:read webhook"
   }

5. Refresh (every 2 hours, forever):
   POST https://bitbucket.org/site/oauth2/access_token
   Authorization: Basic {base64(consumer_key:consumer_secret)}

   grant_type=refresh_token&refresh_token={refresh_token}
```

### Create Webhook

```
POST /2.0/repositories/{workspace}/{repo_slug}/hooks
Authorization: Bearer {access_token}
Content-Type: application/json

{
  "description": "Cinch CI",
  "url": "https://cinch.sh/webhooks/bitbucket",
  "active": true,
  "events": ["repo:push"]
}

Response:
{
  "uuid": "{...}",  // Save this for verification
  ...
}
```

### Post Build Status

```
POST /2.0/repositories/{workspace}/{repo_slug}/commit/{commit}/statuses/build
Authorization: Bearer {access_token}
Content-Type: application/json

{
  "state": "SUCCESSFUL",
  "key": "cinch",
  "name": "Cinch CI",
  "description": "Build passed in 2m 34s",
  "url": "https://cinch.sh/jobs/123"
}
```

States: `INPROGRESS`, `SUCCESSFUL`, `FAILED`, `STOPPED`

### Webhook Payload

```
POST /webhooks/bitbucket
X-Event-Key: repo:push
X-Hook-UUID: {uuid}

{
  "push": {
    "changes": [
      {
        "new": {
          "type": "branch",
          "name": "main",
          "target": {
            "hash": "abc1234567890..."
          }
        }
      }
    ]
  },
  "repository": {
    "full_name": "workspace/repo",
    "uuid": "{...}"
  },
  "actor": {
    "username": "user"
  }
}
```

**Webhook verification:** Bitbucket doesn't sign webhooks. Verify `X-Hook-UUID` matches stored webhook UUID.

## Releases (LOL)

**Bitbucket has no releases feature.**

They have a [Downloads API](https://developer.atlassian.com/cloud/bitbucket/rest/api-group-downloads/) which is just... file hosting. No release notes. No tag association. Just files.

```
POST /2.0/repositories/{workspace}/{repo_slug}/downloads
Authorization: Bearer {access_token}
Content-Type: multipart/form-data

--boundary
Content-Disposition: form-data; name="files"; filename="cinch-linux-amd64"
Content-Type: application/octet-stream

<binary data>
--boundary--
```

**What `cinch release` would do:**
1. Upload files to Downloads
2. That's it. No release object. No changelog. Nothing.

**We should probably:**
- Support it for completeness
- Warn users that Bitbucket "releases" are just file dumps
- Suggest they use GitHub/GitLab for real releases

## Other Bitbucket Bullshit

### Merge Blocking Requires Premium

To block PR merges based on build status, you need [Bitbucket Premium](https://support.atlassian.com/bitbucket-cloud/docs/check-build-status-in-a-pull-request/).

Free tier: status shows in PR but doesn't block merge.

**Great platform design, Atlassian.**

### Two Different Products

Bitbucket Cloud and Bitbucket Data Center (self-hosted) have **completely different APIs**.

Cloud: `https://api.bitbucket.org/2.0/`
Data Center: `https://{instance}/rest/api/1.0/`

Different auth, different endpoints, different payloads.

**If we want to support both, it's basically two implementations.**

### App Passwords Deprecated

[App passwords are deprecated](https://www.atlassian.com/blog/bitbucket/bitbucket-cloud-transitions-to-api-tokens-enhancing-security-with-app-password-deprecation) and will stop working **June 9, 2026**.

Replaced by "API Tokens" which are... also user-scoped. No improvement for our use case.

## Implementation Plan

### Phase 1: OAuth Flow (Cloud Only)

1. OAuth consumer registration
2. OAuth callback with refresh token storage
3. Token refresh logic (every 2 hours)
4. Webhook creation via API
5. Build status posting
6. "Add Bitbucket" button in web UI

### Phase 2: Manual Token Flow

1. `cinch repo add --forge bitbucket` for manual setup
2. Accept Repository Access Token (user creates in UI)
3. Instruct user on manual webhook setup

### Phase 3: Downloads "Releases"

1. Upload to Downloads API
2. Warn users this isn't a real release
3. `cinch release` support with caveat

### Phase 4: Data Center (Maybe Never)

1. Separate implementation
2. Different auth (HTTP Access Tokens)
3. Different API structure
4. Probably not worth it

## Database Changes

```sql
-- OAuth flow stores refresh token (unfortunately)
repos.forge_refresh_token = 'encrypted_refresh_token'
repos.forge_token = 'current_access_token'
repos.forge_token_expires_at = timestamp

-- Manual flow stores repository access token directly
repos.forge_token = 'repository_access_token'
repos.forge_refresh_token = NULL
```

## Error Messages

Be clear about platform limitations:

```
# Merge blocking
"Bitbucket merge blocking requires Premium.
 Your builds will show on PRs but won't block merging.
 Upgrade Bitbucket or switch to GitHub/GitLab for merge protection."

# Releases
"Bitbucket doesn't have releases, only file downloads.
 Your artifacts are uploaded but there's no release page.
 Consider using GitHub or GitLab if you need proper releases."

# OAuth scope
"Cinch needs account-level access because Bitbucket doesn't support
 repository-scoped tokens via API. We only access repos you connect.
 For tighter security, create a Repository Access Token manually."
```

## Summary

| Aspect | Grug? | Notes |
|--------|-------|-------|
| Setup flow | ✗ Compromised | OAuth works but we hold account-level creds |
| Token management | ✗ Leaky | Must store refresh token forever |
| Webhook setup | ✓ Automatic | Can create via API |
| Token refresh | ✗ Constant | Every 2 hours, forever |
| Releases | ✗ Broken | Downloads API, not real releases |
| Merge blocking | ✗ Paywalled | Premium only |
| Self-hosted | ✗ Different API | Would need separate implementation |

**Bitbucket gets a D. The platform fundamentally doesn't support what we need.**

## Priority: Not Planned

Given the platform limitations documented above, Bitbucket integration is not on the roadmap.

If demand materializes, the implementation path is documented here for reference.
