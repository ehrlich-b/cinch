# Code Review Prompt

You are reviewing Cinch, a CI system. This codebase was rapidly developed and needs a thorough review.

## What Cinch Does

- Control plane receives webhooks from GitHub/GitLab/Forgejo
- Dispatches jobs to workers via WebSocket
- Workers run commands in containers on user hardware
- Posts status checks back to the forge

## Review Focus Areas

### 1. Security (Critical)

**Authentication & Authorization:**
- JWT token generation and validation (`internal/server/auth.go`)
- Worker authentication via tokens (`internal/server/workerstream.go`)
- Cookie security (httpOnly, secure, sameSite)
- OAuth flows for GitHub, GitLab, Forgejo

**Webhook Security:**
- Signature validation for each forge type (`internal/server/webhook.go`)
- Are all webhook endpoints properly validated?
- Can an attacker forge a webhook to run arbitrary code?

**Worker Trust Model:**
- Personal workers only run owner's code
- Shared workers run collaborators' code
- Fork PR handling - does untrusted code run on maintainer's worker?
- Review `internal/server/hub.go` SelectWorker logic

**Secrets & Tokens:**
- Forge tokens passed to jobs - are they scoped correctly?
- Token storage in database - encrypted?
- Token exposure in logs or error messages?

**Input Validation:**
- Webhook payloads
- API request bodies
- WebSocket messages
- Config file parsing (.cinch.yaml)

**Container Escape:**
- How are containers invoked? (`internal/worker/container/`)
- Volume mounts - can job escape to host?
- Network isolation?

### 2. Functionality Bugs

**Race Conditions:**
- Job dispatch to multiple workers
- Worker connection/disconnection during job
- Concurrent webhook processing

**State Management:**
- Job status transitions (pending -> running -> success/failed)
- Worker online/offline tracking
- What happens if server restarts mid-job?

**Error Handling:**
- Webhook processing failures - are they retried?
- Worker disconnection during job - is job re-queued?
- Database errors - graceful degradation?

**Edge Cases:**
- Very long running jobs
- Very large log output
- Simultaneous pushes to same branch
- Worker reconnection with same ID

### 3. Data Integrity

**Database:**
- Foreign key constraints
- Transaction usage for multi-table operations
- SQLite concurrent access patterns

**Log Storage:**
- R2 upload reliability
- What if R2 is unavailable?
- Log truncation for huge outputs?

### 4. API Review

**REST API (`internal/server/api.go`):**
- Authentication on all endpoints that need it
- Authorization checks (can user X do action Y?)
- Input validation
- Error response consistency

**WebSocket Protocol:**
- Message validation
- Connection lifecycle
- Heartbeat/timeout handling

### 5. Code Quality

- Dead code
- Unused variables/imports
- Error handling patterns (are errors logged? returned? swallowed?)
- Consistent naming
- Comments that are wrong or outdated

## Key Files to Review

```
internal/server/
  auth.go           # JWT, OAuth, cookies
  webhook.go        # Forge webhook handling
  api.go            # REST API endpoints
  hub.go            # Worker management, job dispatch
  workerstream.go   # WebSocket worker connections
  ws.go             # WebSocket handling

internal/worker/
  worker.go         # Job execution
  container/        # Docker/Podman execution

internal/forge/
  github.go         # GitHub API client
  gitlab.go         # GitLab API client
  forgejo.go        # Forgejo/Gitea API client
```

## Output Format

For each issue found:

```
## [SEVERITY] Short description

**File:** path/to/file.go:123
**Type:** Security | Bug | Code Quality | Performance

**Problem:**
Explain what's wrong.

**Impact:**
What could go wrong? Who is affected?

**Suggested Fix:**
How to fix it (code snippet if helpful).
```

Severity levels:
- **CRITICAL**: Security vulnerability, data loss, or complete failure
- **HIGH**: Significant bug affecting functionality
- **MEDIUM**: Bug with workaround or minor security issue
- **LOW**: Code quality, minor issues

## Questions to Answer

After review, answer:

1. Would you trust this to run on your production servers?
2. What are the top 3 issues to fix before public launch?
3. Any architectural concerns that would be painful to fix later?
4. What's surprisingly good about this codebase?

## Context

- Single developer, rapid development
- Currently in beta, ~1 user (the author)
- Planning public launch soon
- MIT licensed, self-hostable

Be thorough but practical. Focus on issues that matter for a CI system handling code and secrets.
