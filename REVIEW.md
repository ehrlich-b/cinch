# Cinch Code Review (Security-First)

Scope reviewed: core server/worker/storage/auth/webhook/relay paths, plus API/CLI integration points and major frontend/API coupling. Priority order below is **P1 security blockers**, then **P2 egregious AI/code smell / functional correctness**.

---

## [CRITICAL] Private repo logs are accessible to any authenticated user via WebSocket

**File:** `internal/server/logstream.go:81-91`  
**Type:** Security

**Problem:**
For private repos, `/ws/logs/{job_id}` only checks that the caller is authenticated, then explicitly leaves authorization as TODO. There is no owner/collaborator check before streaming historical + live logs.

**Impact:**
Any logged-in user can read logs from other users' private repos. Since logs often contain secrets/tokens, this is a direct cross-tenant secret exposure.

**Suggested Fix:**
Use the same repo access policy as REST (`canAccessRepo`/owner check) before WS upgrade. Deny with 403 when user cannot access that repo.

---

## [CRITICAL] Reflected/stored XSS in auth pages

**File:** `internal/server/auth.go:744-748`, `internal/server/auth.go:1221-1223`, `internal/server/auth.go:1133-1139`  
**Type:** Security

**Problem:**
Untrusted strings are interpolated directly into HTML:
- email options in `renderEmailSelector` are inserted without escaping.
- `userCode` (from query/form input) is inserted directly into an input value.
- `message` is inserted via `fmt.Sprintf` into HTML blocks.

**Impact:**
Attackers can inject script into auth-domain pages and perform authenticated actions (session riding, device authorization abuse, data extraction through same-origin API calls).

**Suggested Fix:**
Render via `html/template` (auto-escaping), or at minimum use proper HTML escaping for all untrusted values. Add CSP as defense-in-depth.

---

## [CRITICAL] Worker identity is client-asserted (owner/mode spoofing)

**File:** `internal/server/ws.go:404-413`, `internal/server/ws.go:442-453`, `internal/server/ws.go:245-252`  
**Type:** Security

**Problem:**
The worker sends `OwnerName`/`Mode` in REGISTER and server trusts it. Token validation returns worker ID but does not bind owner identity from token ownership. A malicious worker can claim another username as owner.

**Impact:**
Trust model can be bypassed: jobs can be routed to attacker-controlled workers by spoofing owner metadata, especially around personal/shared worker selection and fork-PR gating behavior.

**Suggested Fix:**
Derive owner identity server-side from token owner (`tokens.owner_user_id -> users.name`) and ignore/validate client-provided `OwnerName`/`Mode` unless explicitly authorized.

---

## [HIGH] Account deletion leaves worker tokens valid (orphaned credentials)

**File:** `internal/storage/sqlite.go:1520-1528`, `internal/storage/postgres.go:1472-1474`  
**Type:** Security

**Problem:**
`DeleteUser` deletes only the user row; tokens owned by that user are not revoked/deleted.

**Impact:**
Deleted accounts can still have active worker tokens that authenticate WebSocket workers. This is a credential lifecycle failure.

**Suggested Fix:**
On user deletion: revoke/delete all owned tokens, repos, relay IDs, and any other auth artifacts in a transaction.

---

## [HIGH] Public endpoint leaks worker job metadata without auth

**File:** `internal/server/api.go:833-859`  
**Type:** Security

**Problem:**
`GET /api/workers/{id}/jobs` has no auth or visibility checks and returns recent jobs + repo names for that worker.

**Impact:**
Unauthenticated clients can enumerate build/job activity and repo metadata (including private repo names if reachable by worker ID discovery).

**Suggested Fix:**
Require auth and apply same worker visibility + repo access policy used elsewhere.

---

## [HIGH] Unbounded request body reads on webhook/relay paths (DoS risk)

**File:** `internal/server/webhook.go:85`, `internal/server/github_app.go:105`, `internal/server/relay_http.go:61`  
**Type:** Security

**Problem:**
Handlers call `io.ReadAll(r.Body)` without a size cap.

**Impact:**
Attackers can send very large bodies and force memory pressure/OOM.

**Suggested Fix:**
Wrap body with `http.MaxBytesReader` and enforce sane limits per endpoint (e.g., 1-5 MB).

---

## [HIGH] Log stream completion path can deadlock

**File:** `internal/server/logstream.go:290-304`  
**Type:** Bug / Reliability

**Problem:**
`BroadcastJobComplete` holds `RLock` (deferred unlock) and then tries `Lock` before returning.

**Impact:**
Potential deadlock in production when job completion broadcasts occur.

**Suggested Fix:**
Do not upgrade lock while read lock is held. Copy subscribers under `RLock`, release, then mutate map under `Lock`.

---

## [HIGH] Username/email identity confusion breaks account + worker control APIs

**File:** `internal/server/api.go:1808-1815`, `internal/server/api.go:1871-1878`, `internal/server/api.go:2023-2030`, `internal/server/api.go:864-883`  
**Type:** Bug / AI Smell

**Problem:**
`auth.GetUser()` returns email, but several API handlers treat it as username and query `GetUserByName`, or compare it to `worker.OwnerName` (GitHub username).

**Impact:**
`/api/user`, disconnect forge, delete user, and worker drain/disconnect can fail or reject legitimate users.

**Suggested Fix:**
Standardize identity type at boundaries (email vs username). Prefer resolving current user via `getCurrentUser()` everywhere.

---

## [HIGH] CLI/server contract drift: cancel command targets nonexistent endpoint

**File:** `cmd/cinch/main.go:1692`, `internal/server/api.go:83-241`  
**Type:** Functionality / AI Smell

**Problem:**
CLI sends `POST /api/jobs/{id}/cancel`, but API router has no cancel route.

**Impact:**
`cinch cancel` is effectively broken despite being exposed as a first-class command.

**Suggested Fix:**
Either implement cancel endpoint + worker cancel propagation, or remove/hide command until supported.

---

## [MEDIUM] Login flow pings nonexistent `/api/whoami`

**File:** `cmd/cinch/main.go:1893`  
**Type:** Functionality / AI Smell

**Problem:**
Login session reuse probes `/api/whoami`, but server router does not define it.

**Impact:**
Always falls through to fresh login flow; confusing and unnecessary network calls.

**Suggested Fix:**
Add `/api/whoami` or switch probe to existing authenticated endpoint.

---

## [MEDIUM] Badge status ignores forge in lookup (cross-forge mix-up)

**File:** `internal/server/badge.go:142`, `internal/server/badge.go:151-154`  
**Type:** Bug

**Problem:**
`getRepoStatus` ignores forge parameter and matches only owner/repo.

**Impact:**
Wrong status may be shown when same owner/repo exists across multiple forges.

**Suggested Fix:**
Include forge type/domain in repo lookup.

---

## [MEDIUM] Token cache in WS auth can keep revoked tokens usable briefly

**File:** `internal/server/ws.go:36-38`, `internal/server/ws.go:236-241`, `internal/server/ws.go:253-257`  
**Type:** Security

**Problem:**
Token hash -> workerID cache has 5-minute TTL and no revocation invalidation hook.

**Impact:**
Recently revoked tokens may continue to authenticate until cache expiry.

**Suggested Fix:**
Invalidate cache entry on revoke, or shorten TTL + include revocation timestamp/version checks.

---

## [LOW] AI-smell/stale comments indicate drift from actual schema

**File:** `internal/storage/sqlite.go:1525`  
**Type:** Code Quality / AI Smell

**Problem:**
Comment says tokens have no `user_id`, but schema and code already use `owner_user_id`.

**Impact:**
Misleading for maintainers and directly tied to missed security cleanup path.

**Suggested Fix:**
Update comments + implement intended token cleanup behavior.

---

## Fix Progress

### ✅ Fixed

1. **[CRITICAL] Private repo logs access** - `logstream.go` now checks repo ownership before streaming logs
2. **[CRITICAL] XSS in auth pages** - All user input escaped with `html.EscapeString()` in `auth.go`
3. **[CRITICAL] Worker identity spoofing** - Owner now derived from token's `owner_user_id`, not client-provided values
4. **[HIGH] Account deletion orphans tokens** - `DeleteUser` now deletes tokens and repos in transaction
5. **[HIGH] /api/workers/{id}/jobs leaks data** - Now requires auth and checks worker ownership
6. **[HIGH] Unbounded request bodies** - Added `MaxBytesReader` (5MB limit) to webhooks and relay
7. **[HIGH] Log stream deadlock** - Fixed RLock/Lock upgrade issue in `BroadcastJobComplete`
8. **[HIGH] Username/email confusion** - All handlers now use `getCurrentUser()` helper
9. **[HIGH] Cancel command missing** - Added `POST /api/jobs/{id}/cancel` endpoint
10. **[MEDIUM] /api/whoami missing** - Added endpoint for CLI session validation
11. **[MEDIUM] Badge ignores forge** - Now matches forge domain in repo lookup
12. **[LOW] Stale comments** - Cleaned up

### 🚧 Remaining

1. **[MEDIUM] Token cache doesn't invalidate on revoke** - 5-minute TTL means revoked tokens work briefly

---

## Testing Notes

- All tests pass: `go test ./...`

---

## Direct Answers

1. **Would I trust this on production servers today?**
   Yes, with the fixes above applied. Core authz, XSS, and trust-model issues are resolved.

2. **Top 3 issues that were fixed:**
   1) Private log access authz gap - now checks repo ownership
   2) Worker identity spoofing - owner derived from token, not client
   3) XSS in auth pages - all user input escaped

3. **Architectural concerns painful later:**  
   - Identity model inconsistency (email vs username) across auth/API/worker trust logic.  
   - Trust model depending on client-supplied metadata instead of server-bound identities.  
   - Route/contract drift between CLI and API (commands existing before backend support).

4. **What’s surprisingly good:**  
   - Webhook signature verification is present and generally done before state mutation in core webhook handler.  
   - Secrets/tokens are designed for encryption-at-rest with migration support.  
   - Tests are reasonably broad across server/storage/worker modules.
