# GitHub App Integration

## Overview

Replace PAT-based GitHub integration with a proper GitHub App. This gives us:
- One-click installation for users
- Auto-configured webhooks (no manual setup)
- Scoped permissions (only what we need)
- Installation tokens managed by GitHub
- Proper branding in GitHub UI

## User Flow

```
1. User visits cinch.sh, clicks "Add to GitHub"
2. Redirected to GitHub App installation page
3. User selects org/account and repos
4. GitHub sends `installation` webhook to Cinch
5. Cinch stores installation, creates repo records
6. Done - webhooks are auto-configured by GitHub
```

No manual webhook setup. No PAT creation. No copying secrets.

## GitHub App Configuration

### Permissions Required

**Repository permissions:**
- `contents: read` - Clone repos
- `statuses: write` - Post commit statuses
- `checks: write` - Create check runs (optional, nicer UI)
- `metadata: read` - Required for all apps

**Subscribe to events:**
- `push` - Trigger builds
- `installation` - Track installs/uninstalls
- `installation_repositories` - Track repo additions/removals

### App Settings

```
Name: Cinch CI
URL: https://cinch.sh
Webhook URL: https://cinch.sh/webhooks/github-app
Webhook secret: <generated>
```

## Database Changes

### New: `installations` table

```sql
CREATE TABLE installations (
    id TEXT PRIMARY KEY,              -- installation_id from GitHub
    account_type TEXT NOT NULL,       -- 'User' or 'Organization'
    account_login TEXT NOT NULL,      -- username or org name
    account_id INTEGER NOT NULL,      -- GitHub account ID
    app_id INTEGER NOT NULL,          -- GitHub App ID
    created_at DATETIME NOT NULL,
    suspended_at DATETIME             -- NULL if active
);
```

### Modified: `repos` table

```sql
-- Add installation_id, remove forge_token
ALTER TABLE repos ADD COLUMN installation_id TEXT REFERENCES installations(id);
ALTER TABLE repos DROP COLUMN forge_token;
ALTER TABLE repos DROP COLUMN webhook_secret;  -- GitHub manages this now
```

## Server Changes

### New: GitHub App Handler (`internal/server/github_app.go`)

Handles GitHub App-specific webhooks:

```go
type GitHubAppHandler struct {
    appID          int64
    privateKey     []byte  // PEM-encoded private key
    webhookSecret  string
    storage        storage.Storage
    dispatcher     *Dispatcher
}

// Webhook events
func (h *GitHubAppHandler) HandleInstallation(event *InstallationEvent)
func (h *GitHubAppHandler) HandleInstallationRepositories(event *InstallationRepositoriesEvent)
func (h *GitHubAppHandler) HandlePush(event *PushEvent)

// Token management
func (h *GitHubAppHandler) GetInstallationToken(installationID string) (string, error)
```

### Installation Token Flow

GitHub Apps use short-lived installation tokens (1 hour):

```
1. App authenticates as itself using JWT (signed with private key)
2. App requests installation token for specific installation
3. Use installation token for API calls (status, clone)
4. Token expires after 1 hour, refresh as needed
```

```go
func (h *GitHubAppHandler) GetInstallationToken(installationID int64) (string, time.Time, error) {
    // 1. Create JWT signed with app private key
    jwt := h.createAppJWT()

    // 2. Request installation token
    // POST /app/installations/{installation_id}/access_tokens
    // Authorization: Bearer <jwt>

    // 3. Return token and expiry
    return token, expiresAt, nil
}
```

### Modified: Webhook Handler

```go
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // Check if this is a GitHub App webhook (has X-GitHub-Hook-Installation-Target-ID)
    if r.Header.Get("X-GitHub-Hook-Installation-Target-Type") == "integration" {
        h.githubApp.HandleWebhook(w, r)
        return
    }

    // Legacy webhook handling (for other forges)
    // ...
}
```

### Modified: Status Posting

```go
func (h *WebhookHandler) PostJobStatus(ctx context.Context, jobID, state, description string) error {
    job, _ := h.storage.GetJob(ctx, jobID)
    repo, _ := h.storage.GetRepo(ctx, job.RepoID)

    // Get installation token
    token, _, err := h.githubApp.GetInstallationToken(repo.InstallationID)
    if err != nil {
        return err
    }

    // Post status with installation token
    return h.postGitHubStatus(token, repo, job.Commit, state, description)
}
```

### Modified: Clone Authentication

Workers need installation tokens for private repos:

```go
// In job assignment, include clone token
func (d *Dispatcher) assignJob(job *Job, worker *WorkerConn) {
    repo, _ := d.storage.GetRepo(ctx, job.RepoID)

    // Get fresh installation token for cloning
    cloneToken, _, _ := d.githubApp.GetInstallationToken(repo.InstallationID)

    assignment := protocol.JobAssign{
        JobID:      job.ID,
        CloneURL:   repo.CloneURL,
        CloneToken: cloneToken,  // Installation token
        // ...
    }
}
```

## CLI Changes

### Remove: `cinch repo add`

No longer needed - repos are added via GitHub App installation.

### Keep: `cinch repo list`

Still useful to see which repos are configured.

### New: Installation management

```bash
$ cinch installations list
ORG/USER          REPOS
ehrlich-b         3 repos
my-org            12 repos

$ cinch installations repos ehrlich-b
ehrlich-b/cinch
ehrlich-b/other-repo
ehrlich-b/another
```

## Web UI Changes

### Add GitHub Button

```tsx
function AddRepoButton() {
    const appSlug = "cinch-ci"  // GitHub App slug
    const installURL = `https://github.com/apps/${appSlug}/installations/new`

    return (
        <a href={installURL} className="btn">
            Add GitHub Repos
        </a>
    )
}
```

### Show Installations

```tsx
function InstallationsPage() {
    // List installations with their repos
    // Show "Add more repos" link for each installation
}
```

## Environment Variables

```bash
# GitHub App credentials
CINCH_GITHUB_APP_ID=123456
CINCH_GITHUB_APP_PRIVATE_KEY="-----BEGIN RSA PRIVATE KEY-----\n..."
CINCH_GITHUB_APP_WEBHOOK_SECRET=whsec_xxx

# OAuth (keep for web UI login)
CINCH_GITHUB_CLIENT_ID=xxx
CINCH_GITHUB_CLIENT_SECRET=xxx
```

## Migration Path

1. Create GitHub App in GitHub settings
2. Implement installation webhook handling
3. Implement installation token generation
4. Update status posting to use installation tokens
5. Update clone to use installation tokens
6. Add "Add to GitHub" button in UI
7. Deprecate `cinch repo add` (keep for other forges)

## Security Considerations

1. **Private key storage** - Store in secrets manager or encrypted at rest
2. **Installation tokens** - Short-lived (1 hour), auto-refresh
3. **Webhook verification** - Verify signature using webhook secret
4. **Scope limitation** - Only request permissions we need

## Forges Other Than GitHub

The GitHub App is GitHub-specific. For Forgejo/Gitea/GitLab:
- Keep the existing webhook + token approach
- `cinch repo add` remains for non-GitHub forges
- Each forge has its own authentication method

## Implementation Order

1. **Create GitHub App** (manual, in GitHub settings)
2. **Database migration** - Add `installations` table
3. **Installation webhooks** - Handle install/uninstall
4. **Installation tokens** - JWT auth, token generation
5. **Status posting** - Use installation tokens
6. **Clone tokens** - Pass to workers
7. **Web UI** - Add GitHub button, show installations
8. **Deploy and test**
