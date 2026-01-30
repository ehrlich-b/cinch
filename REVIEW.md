**Overview**

Cinch is a CI system with a control plane (webhooks, API, job dispatch) and workers (WebSocket, container execution). This is an updated review checking fixes from the previous audit and identifying new issues.

---

# Previously Identified Issues - Status

## ✅ FIXED: Public access to job logs

**File:** internal/server/api.go:367-408, internal/server/logstream.go:73-91

The `getJobLogs` endpoint and `/ws/logs` WebSocket now check `repo.Private` and require authentication for private repo logs. Both handlers fetch the job's repo and verify auth before serving logs.

## ✅ FIXED: Webhook secret leakage via public endpoint

**File:** internal/server/api.go:1044

`getRepo` no longer returns `WebhookSecret` in the response. Comment explicitly notes: "WebhookSecret intentionally omitted - never expose secrets in API". The secret is only returned during initial repo creation.

## ✅ FIXED: Unrestricted approval of external PR jobs

**File:** internal/server/api.go:514-525

The `runJob` handler now verifies that only the repo owner can approve fork PRs: `if repo.Owner != username { ... return 403 }`. This is restrictive but secure.

## ✅ FIXED: Webhook side-effects before signature verification

**File:** internal/server/webhook.go:121-139, 207-226

Both `handlePush` and `handlePullRequest` now verify signature BEFORE any state changes. The `repo.Private` sync only happens after successful verification.

## ✅ FIXED: Forge tokens stored in plaintext

**File:** internal/crypto/crypto.go, internal/storage/sqlite.go

AES-256-GCM encryption is now implemented. Sensitive fields (webhook_secret, forge_token, gitlab_credentials, forgejo_credentials) are encrypted at rest with automatic migration of existing plaintext values.

## ✅ FIXED: Origin checks for UI WebSockets

**File:** internal/server/ws.go:43-65

A separate `uiUpgrader` now exists with origin validation for UI WebSockets, while `workerUpgrader` remains permissive for worker connections.

## ⚠️ PARTIALLY FIXED: Sensitive metadata exposed via public GET

**File:** internal/server/api.go

Private repo jobs are now filtered in `listJobs` (lines 301-304) and `getJob` checks auth (lines 335-345). However, `listRepos` still returns ALL repos including private ones without filtering.

## ❌ NOT FIXED: Git credential leakage via process list

**File:** internal/worker/clone.go:46-53

Token is still embedded in the clone URL (`u.User = url.UserPassword("x-access-token", token)`). This appears in `ps aux` output and error messages.

## ❌ NOT FIXED: Worker ID collision handling

**File:** internal/server/hub.go:99-109

`hub.Register` still overwrites the map entry without disconnecting prior connections or closing send channels, leading to stale goroutines and potential message routing bugs.

---

# New Issues Found

## [CRITICAL] Origin check bypass in uiUpgrader

**File:** internal/server/ws.go:56
**Type:** Security

**Problem:**
```go
if strings.Contains(origin, host) {
    return true
}
```
This check is bypassable. An attacker hosting `evil-cinch.sh.com` or `cinch.sh.evil.com` would pass the check since both contain "cinch.sh".

**Impact:**
Cross-site WebSocket hijacking. Malicious websites can connect to log streams if user is authenticated.

**Suggested Fix:**
Parse both URLs and compare hosts exactly:
```go
originURL, err := url.Parse(origin)
if err != nil { return false }
return originURL.Host == r.Host
```

## [CRITICAL] SQL injection in GetUserByEmail

**File:** internal/storage/sqlite.go:960
**Type:** Security

**Problem:**
```go
WHERE email = ? OR emails LIKE ?`, email, "%"+email+"%"
```
While `email` is parameterized for the first condition, the LIKE pattern with `%` wildcards allows unintended matching. A user with email `a]%` could match other users' email arrays.

**Impact:**
Account takeover potential if an attacker can register with specially crafted email addresses.

**Suggested Fix:**
Escape `%` and `_` characters in the LIKE pattern, or use JSON functions to properly search the emails array.

## [HIGH] GitHub App missing fork detection and trust model

**File:** internal/server/github_app.go:304-441
**Type:** Security

**Problem:**
The `handlePullRequest` handler in GitHubAppHandler doesn't check whether the PR is from a fork and doesn't set `Author`, `TrustLevel`, or `IsFork` fields on the job. All PR jobs go directly to `pending` status.

**Impact:**
Malicious fork PRs bypass the trust model and run immediately on shared workers without requiring contributor CI approval, potentially executing arbitrary code.

**Suggested Fix:**
Add fork detection (compare `head.repo.full_name` vs `repository.full_name`) and set trust fields on the job, similar to webhook.go's `handlePullRequest`.

## [HIGH] Device code has weak entropy (brute-forceable)

**File:** internal/server/auth.go:931-937
**Type:** Security

**Problem:**
```go
userCode := fmt.Sprintf("CINCH-%04d", int(userCodeBytes[0])<<8|int(userCodeBytes[1])%10000)
```
The user code only has ~10,000 possibilities (CINCH-0000 to CINCH-9999), not the 65,536 the bit manipulation suggests (due to the `%10000`).

**Impact:**
An attacker can brute-force device codes to hijack authentication. With 15-minute expiry and no rate limiting, ~10,000 guesses is trivial.

**Suggested Fix:**
Use a longer alphanumeric code with higher entropy (e.g., 8 characters from A-Z0-9 = 36^8 = 2.8 trillion possibilities), or add rate limiting per IP.

## [HIGH] listRepos exposes all repos including private

**File:** internal/server/api.go:979-1022
**Type:** Security

**Problem:**
`listRepos` returns ALL repos without any authentication or privacy filtering. Private repo names, clone URLs, and metadata are exposed.

**Impact:**
Information disclosure of private repository existence and CI activity.

**Suggested Fix:**
Either require authentication and filter by user access, or only return public repos for unauthenticated requests.

## [HIGH] listTokens returns all system tokens

**File:** internal/server/api.go:1338-1358
**Type:** Security

**Problem:**
`listTokens` returns ALL tokens in the system. There's no filtering by user/owner - any authenticated user can see all tokens.

**Impact:**
Information disclosure. While token hashes aren't useful directly, token names and creation times reveal infrastructure information. Combined with worker IDs, this could aid targeted attacks.

**Suggested Fix:**
Filter tokens by the authenticated user's ownership, or add an `owner_id` field to tokens.

## [MEDIUM] Token exposed in WebSocket URL

**File:** internal/server/ws.go:151, internal/worker/worker.go:241-242
**Type:** Security

**Problem:**
Worker authentication token is passed in the URL query string:
```go
token := r.URL.Query().Get("token")
q.Set("token", w.config.Token)
```

**Impact:**
Tokens appear in server access logs, browser history, proxy logs, and referrer headers. Long-lived tokens could be exposed through log aggregation.

**Suggested Fix:**
Use WebSocket subprotocol for auth, or send token in first message after connection.

## [MEDIUM] No job ownership verification on completion

**File:** internal/server/ws.go:533-598
**Type:** Security

**Problem:**
When a worker sends `JOB_COMPLETE` or `JOB_ERROR`, the server doesn't verify that this worker was actually assigned the job. Any worker could claim to complete any job.

**Impact:**
A malicious worker could mark other workers' jobs as failed/completed, disrupting CI builds.

**Suggested Fix:**
Track job-to-worker assignment in `inflight` or database and verify before accepting completion messages.

## [MEDIUM] Hub.List returns mutable pointers

**File:** internal/server/hub.go:134-142
**Type:** Bug

**Problem:**
```go
func (h *Hub) List() []*WorkerConn {
    h.mu.RLock()
    defer h.mu.RUnlock()
    workers := make([]*WorkerConn, 0, len(h.workers))
    for _, w := range h.workers {
        workers = append(workers, w)
    }
    return workers
}
```
Returns pointers to the actual WorkerConn structs. Callers can modify them outside the mutex lock.

**Impact:**
Race conditions when multiple goroutines access worker state. Could cause data corruption or panics.

**Suggested Fix:**
Return copies of the structs, not pointers, or use a separate mutex for each WorkerConn.

## [MEDIUM] parseEmailsJSON is fragile

**File:** internal/storage/sqlite.go:1072-1092
**Type:** Bug

**Problem:**
Custom JSON parser using string splitting:
```go
parts := strings.Split(s, ",")
```
Breaks if an email contains a comma (valid in quoted local-part) or if the JSON has whitespace.

**Impact:**
User account issues if emails have edge-case characters.

**Suggested Fix:**
Use `encoding/json` to parse the array properly:
```go
var emails []string
json.Unmarshal([]byte(s), &emails)
```

## [MEDIUM] formatEmailsJSON doesn't escape

**File:** internal/storage/sqlite.go:1094-1103
**Type:** Bug

**Problem:**
```go
parts = append(parts, "\""+e+"\"")
```
If an email contains a quote character, the resulting JSON is malformed.

**Impact:**
Database corruption for edge-case email addresses.

**Suggested Fix:**
Use `json.Marshal` instead of manual string construction.

## [LOW] Race condition in device code verification

**File:** internal/server/auth.go:977-1007
**Type:** Bug

**Problem:**
Device code lookup and authorization happen in separate lock acquisitions:
```go
h.deviceCodesMu.Lock()
// find code
h.deviceCodesMu.Unlock()
// ... do auth check ...
h.deviceCodesMu.Lock()
dc.Authorized = true
h.deviceCodesMu.Unlock()
```

**Impact:**
Unlikely but possible race between lookup and authorization if user submits twice quickly.

**Suggested Fix:**
Keep lock held during the full verification flow.

## [LOW] No rate limiting on device code verification

**File:** internal/server/auth.go:967-1016
**Type:** Security

**Problem:**
No rate limiting on POST to `/auth/device/verify` allows unlimited brute-force attempts.

**Impact:**
Combined with weak device code entropy, enables account hijacking.

**Suggested Fix:**
Add rate limiting per IP or exponential backoff on failed attempts.

## [LOW] Cleanup goroutine spawned on every device auth request

**File:** internal/server/auth.go:948
**Type:** Performance

**Problem:**
```go
go h.cleanupExpiredDeviceCodes()
```
A new goroutine is spawned for every device auth request to clean up expired codes.

**Impact:**
Under load, many redundant cleanup goroutines running. Minor resource waste.

**Suggested Fix:**
Use a single background ticker for cleanup, or only clean up when map exceeds threshold.

---

**Questions Answered**

1) Would you trust this to run on your production servers?
- Getting closer. The critical issues from the first review are mostly fixed. The remaining CRITICAL issues (origin bypass, SQL-ish injection, missing fork detection in GitHub App) need to be addressed, but the codebase is significantly more secure than before.

2) Top 3 issues to fix before public launch
- Fix origin check in uiUpgrader (exact host match, not substring)
- Add fork detection to GitHub App webhook handler
- Increase device code entropy and add rate limiting

3) Architectural concerns that would be painful to fix later
- Job ownership verification (need to track worker-to-job assignment properly)
- Token in URL pattern (hard to change protocol once workers deployed)
- The Hub returning mutable pointers (requires careful refactoring)

4) What's surprisingly good about this codebase?
- Encryption at rest was implemented correctly with AES-256-GCM
- The trust model for personal vs shared workers is well thought out
- Good separation of concerns between webhook handling, dispatch, and worker management
- JWT handling is solid with proper signing method validation
- The fix for webhook signature verification is clean and correct

**Quick Fix Checklist**

- [ ] Fix origin check: exact host match, not substring contains
- [ ] Add fork detection to github_app.go handlePullRequest
- [ ] Increase device code entropy (alphanumeric, longer)
- [ ] Add rate limiting to device verification
- [ ] Filter listRepos by authentication/privacy
- [ ] Filter listTokens by owner
- [ ] Verify job ownership before accepting completion
- [ ] Fix parseEmailsJSON to use json.Unmarshal
- [ ] Fix formatEmailsJSON to use json.Marshal
- [ ] Consider moving token from URL to subprotocol
- [ ] Add worker collision handling in hub.Register
