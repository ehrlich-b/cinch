# Forge Integration Designs

This folder contains detailed design documents for each git forge integration.

## The Grug Test

**Grug developer want:** Click button, done. No copy token. No configure webhook. Magic.

**GitHub passes.** GitHub Apps give us everything: one-click install, auto-webhooks, service-owned credentials.

**Everyone else fails** to varying degrees. Here's why:

| Forge | Can service own credentials? | Auto-webhooks? | Grug Rating |
|-------|------------------------------|----------------|-------------|
| GitHub | ✓ GitHub App private key | ✓ Auto-configured | **A** |
| GitLab | ~ Project Access Token (via OAuth trick) | ✓ Via API | **B+** |
| Forgejo/Gitea | ✗ Token API needs password | ✓ Via API | **C+** |
| Bitbucket | ✗ No repo-scoped token API | ✓ Via API | Not planned |

### Why GitHub Wins

```
GitHub App Model:
1. Cinch has a PRIVATE KEY (service-owned)
2. When webhook fires, Cinch requests installation token FROM GITHUB
3. User credentials never touch Cinch
4. User can revoke by uninstalling app
```

No other forge has this. They all require either:
- Storing user's OAuth refresh token forever (account-level access)
- User manually creating and copying tokens (not grug)

### The GitLab Trick

GitLab has [Project Access Tokens](https://docs.gitlab.com/ee/user/project/settings/project_access_tokens/) - bot users scoped to a project. And they can be **created via API**.

```
1. User does OAuth
2. Use OAuth token to create Project Access Token
3. THROW AWAY OAuth token
4. Project Access Token is project's credential, not user's
```

**Catches:** Premium required on GitLab.com, 365-day max expiry.

### Why Forgejo/Gitea Falls Short

Token creation API [requires basic auth](https://github.com/go-gitea/gitea/issues/21186) (username:password). OAuth tokens don't work.

If Forgejo fixed this, they'd match GitLab. Until then: manual setup.

## Implementation Status

| Forge | Status | Doc |
|-------|--------|-----|
| GitHub | **Complete** | [github.md](./github.md) |
| Forgejo/Gitea | **Partial** (manual only) | [forgejo-gitea.md](./forgejo-gitea.md) |
| GitLab | **Planned** | [gitlab.md](./gitlab.md) |
| Bitbucket | **Not Planned** | [bitbucket.md](./bitbucket.md) |

## Recommended Implementation Order

### 1. GitLab (Next)

- 37% dev usage, lots of public repos
- Project Access Token trick enables near-grug experience
- Self-hosted GitLab has no Premium requirement
- Users frustrated with GitLab CI complexity

### 2. Bitbucket (Not Planned)

Platform limitations make this a poor fit. See [bitbucket.md](./bitbucket.md) for details.

## Cinch Requirements Per Forge

Every forge integration needs:

### 1. Build Verification on Commit

Post status to commits. All forges have this.

| Forge | API |
|-------|-----|
| GitHub | Checks API (rich) or Status API |
| GitLab | `POST /api/v4/projects/{id}/statuses/{sha}` |
| Forgejo | `POST /api/v1/repos/{owner}/{repo}/statuses/{sha}` |
| Bitbucket | `POST /2.0/.../commit/{commit}/statuses/build` |

### 2. Build Verification for PRs / Merge Blocking

Same status API, on PR head commit. Users configure branch protection.

| Forge | Merge Blocking |
|-------|----------------|
| GitHub | All plans |
| GitLab | All plans |
| Forgejo | All instances |

### 3. Releases / Artifacts

| Forge | Releases? |
|-------|-----------|
| GitHub | ✓ Excellent |
| GitLab | ✓ Good (two-step) |
| Forgejo | ✓ Good |

### 4. Ergonomic Auth

| Forge | Best We Can Do |
|-------|----------------|
| GitHub | One-click (GitHub App) |
| GitLab | One-click → annual re-auth (Project Access Token) |
| Forgejo | Manual token + webhook |

## Error Message Philosophy

When platform limitations force bad UX, be clear whose fault it is:

```
"GitLab.com requires Premium for automatic setup.
 Self-hosted GitLab works with any tier."

"Forgejo's API requires your password to create tokens.
 That's not happening. Manual setup is the secure option."
```

Don't apologize for platform limitations. Explain them.

## Files

- [github.md](./github.md) - What we implemented (GitHub App, Checks API)
- [forgejo-gitea.md](./forgejo-gitea.md) - What we implemented + what's missing
- [gitlab.md](./gitlab.md) - The Project Access Token trick
- [bitbucket.md](./bitbucket.md) - Why it's fundamentally broken
