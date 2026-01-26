# Auth Understanding Checkpoint

## The Core Problem

We need email-based identity across all forges (GitHub, GitLab, Forgejo). The current code is half-baked - GitHub has email collection, but GitLab/Forgejo don't, and the logic for finding/creating accounts is wrong.

## Key Insight: Check ALL Emails

When a user logs in via ANY forge:
1. Forge returns ALL verified emails (GitHub: primary + secondary, GitLab: email from profile, etc.)
2. We check if ANY of those emails exist in ANY account
3. If found → that's their account, log them in
4. If not found → new user, create account with their selected email

**Example:**
- User onboards via GitHub, selects secondary email `ehrlich.bryan@gmail.com`
- Account created with that email
- Later, user logs in via GitHub again
- GitHub returns: `["bryan@slide.tech" (primary), "ehrlich.bryan@gmail.com" (secondary)]`
- We check BOTH → find account with `ehrlich.bryan@gmail.com` → log them in
- This works even though their primary changed!

## Onboard vs Connect = Same Flow

There should be ONE flow that handles both cases:

```
User clicks "GitHub" (whether from Get Started or Account Settings)
    ↓
GitHub OAuth → get all verified emails
    ↓
Check: do ANY of these emails exist in an account?
    ↓
YES → Log them in, mark GitHub as connected to that account
NO  → Show email selector → Create account → Log them in
```

The only difference between "onboard" and "connect":
- **Onboard**: User not logged in, might create new account
- **Connect**: User already logged in, always connects to existing account

But if a logged-out user clicks GitHub and we find their account by email, we just log them in. Same result as "connect".

## The Flows

### Flow 1: New User Onboarding (not logged in)

```
1. User clicks "Get Started with GitHub"
2. GitHub OAuth → get emails ["primary@example.com", "secondary@example.com"]
3. Check storage for ANY email match → none found
4. Show email selector (if multiple)
5. User selects "secondary@example.com"
6. Create account with email="secondary@example.com"
7. Mark GitHub as connected
8. Set auth cookie with "secondary@example.com"
9. Redirect to success/repo selection
```

### Flow 2: Returning User Login (not logged in)

```
1. User clicks "GitHub" (or any forge button)
2. GitHub OAuth → get emails ["primary@example.com", "secondary@example.com"]
3. Check storage for ANY email match → found account with "secondary@example.com"
4. Mark GitHub as connected (if not already)
5. Set auth cookie with account's email
6. Redirect to dashboard
```

### Flow 3: Connect Additional Forge (already logged in)

```
1. User on Account page, clicks "Connect GitLab"
2. GitLab OAuth → get email "work@company.com"
3. User is already logged in as "secondary@example.com"
4. Check: does "work@company.com" exist in ANOTHER account?
   - If yes → Error: "This email is associated with another account"
   - If no → Add "work@company.com" to current account's email list
5. Mark GitLab as connected
6. Redirect back to Account page
```

### Flow 4: Edge Case - Email Conflict

```
1. User A creates account with "alice@example.com" via GitHub
2. User B creates account with "bob@example.com" via GitLab
3. User A later adds "bob@example.com" to their GitHub
4. User A tries to log in via GitHub
5. GitHub returns ["alice@example.com", "bob@example.com"]
6. We find TWO accounts!
7. Options:
   a) Log into the first match (alice's account) - simplest
   b) Error: "Multiple accounts found, contact support"
   c) Auto-merge (complex, risky)

Recommendation: Option (a) - first match wins. Edge case is rare, user can contact support.
```

## What Each Forge Provides

### GitHub
- `GET /user/emails` with `user:email` scope
- Returns array: `[{email, primary, verified}, ...]`
- We only use verified emails
- Can have multiple verified emails

### GitLab
- `GET /api/v4/user` with `read_user` scope
- Returns single `email` field (their primary)
- Also has `GET /api/v4/user/emails` for all emails
- Should fetch all emails for consistency

### Forgejo/Gitea
- `GET /api/v1/user` with appropriate scope
- Returns `email` field
- May have `GET /api/v1/user/emails` for all (need to verify)
- At minimum, use the primary email

## Required Code Changes

### 1. Unified Auth Flow (auth.go)

The GitHub callback should:
```go
func (h *AuthHandler) handleCallback(...) {
    // Get all emails from GitHub
    emails := getGitHubEmails(accessToken)
    username := getGitHubUser(accessToken).Login

    // Check if user is already logged in
    if existingEmail, ok := h.getAuthFromCookie(r); ok {
        // They're connecting GitHub to existing account
        // Just update the connection, don't create/switch accounts
        user := h.storage.GetUserByEmail(existingEmail)
        h.storage.UpdateUserGitHubConnected(user.ID)
        // Optionally add any new emails to their account
        redirect to account page
        return
    }

    // Not logged in - find or create account
    // Check ALL emails against storage
    for _, email := range emails {
        if user := h.storage.GetUserByEmail(email); user != nil {
            // Found existing account! Log them in
            h.storage.UpdateUserGitHubConnected(user.ID)
            h.setAuthCookie(w, user.Email) // Use account's canonical email
            redirect to dashboard
            return
        }
    }

    // No account found - new user
    if len(emails) == 1 {
        // Auto-select single email
        createAccountAndLogin(emails[0], username)
    } else {
        // Show email selector
        renderEmailSelector(emails, username, returnTo)
    }
}
```

### 2. GitLab OAuth (gitlab_oauth.go)

Currently only works for "connect" (requires login first). Needs to:
1. Support onboarding (create new accounts)
2. Fetch email(s) from GitLab
3. Use same find-or-create logic as GitHub

```go
func (h *GitLabOAuthHandler) HandleCallback(...) {
    // Exchange code for token
    token := exchangeCode(code)

    // Get user info INCLUDING email
    gitlabUser := fetchGitLabUser(token) // has .Email
    emails := []string{gitlabUser.Email}
    // Optionally: fetch all emails from /user/emails endpoint

    // Check if already logged in (connecting to existing account)
    if existingEmail := getAuthFromCookie(r); existingEmail != "" {
        // Connect mode - just add GitLab to existing account
        ...
    }

    // Find or create account (same logic as GitHub)
    for _, email := range emails {
        if user := storage.GetUserByEmail(email); user != nil {
            // Found account, log them in
            ...
        }
    }

    // New user - create account
    // GitLab usually only has one email, so no selector needed
    createAccountAndLogin(emails[0], gitlabUser.Username)
}
```

### 3. Forgejo OAuth (forgejo_oauth.go)

Same pattern as GitLab.

### 4. Storage Changes

Need:
- `GetUserByEmail(email) *User` - already exists
- `GetOrCreateUserByEmail(email, name) *User` - already added
- `AddEmailToUser(userID, email)` - to add secondary emails when connecting

### 5. Remove Routing Complexity

Current code in main.go:
```go
// GitLab callback requires login first
if user == "" {
    redirect to /auth/login?return_to=...
}
```

This should be removed. The callback itself decides whether to create or find account.

## The Simple Mental Model

```
ANY forge OAuth callback:
    1. Get emails from forge
    2. Am I logged in?
       - Yes → Connect forge to my account
       - No → Find account by any email, or create new one
    3. Set cookie, redirect
```

## What About Secrets?

The user asked about secrets. Current state:
- `CINCH_GITHUB_CLIENT_ID/SECRET` - might be OAuth App or GitHub App's OAuth creds
- `CINCH_GITHUB_APP_ID/PRIVATE_KEY/WEBHOOK_SECRET` - GitHub App for webhooks

If using GitHub App for both:
- Use App's client_id/secret for OAuth (login)
- Use App's app_id/private_key for installation tokens (webhooks)
- Can delete separate OAuth App

User needs to verify which credentials are currently configured and whether they're from the App or a separate OAuth App.

## Action Items

1. **Fix GitHub callback** - check ALL emails, find existing account or create new
2. **Update GitLab OAuth** - support onboarding (not just connect), fetch email
3. **Update Forgejo OAuth** - same as GitLab
4. **Remove login requirement** from GitLab/Forgejo callbacks
5. **Test the flow** - onboard with one email, login should find account

## Edge Cases We Accept

1. **Same email in multiple accounts** - First match wins, contact support for merge
2. **User changes primary email** - Works because we check ALL emails
3. **User onboards with weird email then can't find account** - Support issue
4. **Forge doesn't return all emails** - We do our best with what we get
