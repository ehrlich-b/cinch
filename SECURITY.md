# Security Surface

This document catalogs Cinch's security boundaries and attack surfaces. It's a living checklist for security reviews, not a certification of security.

## Threat Model

**What we're protecting:**
- Workers execute commands from `.cinch.yaml` in repos they're configured to trust
- The control plane must never trick a worker into cloning/executing untrusted repos
- User credentials (forge tokens, webhook secrets) stored in the control plane

**What we're NOT protecting against:**
- Malicious repo owners attacking their own workers (repos are "friendly")
- Users running `cinch run` locally on untrusted code (same as running `make` locally)

The core value prop is "run your own CI on your own hardware" - like running tests before pushing. Workers trust their configured repos.

## Security Boundaries

### 1. Control Plane ↔ Worker

**The critical boundary.** Workers execute whatever the control plane tells them.

| Attack | Vector | Mitigation |
|--------|--------|------------|
| Fake job assignment | Compromise control plane, MITM | Worker token auth, TLS |
| Malicious clone URL | Inject repo URL in job | Webhook signature verification |
| Token theft | Intercept WebSocket | TLS, token hashing in storage |

**Things to poke at:**
- [ ] Can a worker be tricked into cloning a repo not in its configured scope?
- [ ] What happens if WebSocket TLS is downgraded?
- [ ] Are worker tokens rotatable? Revocable?
- [ ] Token in query string visible in logs/referer headers?

### 2. Forge → Control Plane (Webhooks)

Webhooks trigger job creation. Forged webhooks = unauthorized builds.

| Attack | Vector | Mitigation |
|--------|--------|------------|
| Forged webhook | POST to webhook endpoint | HMAC-SHA256 signature verification |
| Replay attack | Resend old webhook | (Not currently mitigated - no timestamp check) |
| Missing secret | Misconfiguration | Reject unsigned webhooks |

**Things to poke at:**
- [ ] What if webhook secret is empty string vs not configured?
- [ ] Replay attacks - should we check webhook timestamps?
- [ ] Are webhook secrets generated with sufficient entropy?
- [x] GitHub App handler rejects webhooks when secret not configured

### 3. User Authentication

Browser sessions and CLI authentication.

| Component | Mechanism | Storage |
|-----------|-----------|---------|
| Web UI | JWT in HttpOnly cookie | HS256, 7-day expiry |
| CLI | Device authorization flow | Token in `~/.config/cinch/config.json` |
| OAuth | GitHub OAuth with state parameter | State is signed JWT |

**Things to poke at:**
- [ ] JWT algorithm confusion (we check for HMAC, but verify)
- [x] JWT secret must be configured (panic on missing)
- [ ] Device code brute force (8-char user code = ~2B combinations)
- [ ] OAuth state CSRF token entropy
- [ ] Session fixation after OAuth callback
- [ ] CLI config file permissions (currently 0700)

### 4. API Authentication

`/api/*` endpoints for managing repos, tokens, workers.

**Things to poke at:**
- [ ] Are all API endpoints authenticated?
- [ ] Token scope - can a worker token access admin APIs?
- [ ] Rate limiting on token creation/validation
- [ ] CORS policy on API endpoints

### 5. Secrets at Rest

Data stored in SQLite/Postgres.

| Secret | Storage | Exposure |
|--------|---------|----------|
| Worker tokens | SHA3-256 hash | Never retrievable |
| Webhook secrets | Plaintext | API responses to admins |
| Forge tokens | Plaintext | Used for clone URLs, status posts |
| JWT signing key | Config/env var | Not in database |

**Things to poke at:**
- [ ] Database compromise exposes forge tokens (GitHub PATs, etc.)
- [ ] Should webhook secrets be encrypted at rest?
- [ ] Backup security - are DB dumps protected?

### 6. Secrets in Transit

Credentials flowing through the system.

| Secret | Where | Risk |
|--------|-------|------|
| Clone token | Protocol message to worker | Logged? In memory? |
| Clone URL with token | Git command argument | Visible in `ps`, error messages |
| Webhook secret | HTTP header verification | Timing attacks (mitigated with constant-time compare) |

**Things to poke at:**
- [ ] Clone tokens in git command args visible in process list
- [ ] Error messages containing URLs with embedded tokens
- [ ] Log sanitization - are tokens redacted?
- [ ] Memory handling - are secrets zeroed after use?

### 7. Command Execution

Workers execute shell commands from config files.

| Mode | Isolation | Notes |
|------|-----------|-------|
| Container (default) | Docker | Network access, volume mounts |
| Bare metal | None | Full system access |

**Things to poke at:**
- [ ] Container escape via privileged mode
- [ ] Volume mount exposing host filesystem
- [ ] Network access from container to control plane
- [ ] Environment variable injection
- [ ] Service containers (postgres, redis) - network exposure

**Note:** Command injection from `.cinch.yaml` is BY DESIGN. The repo is trusted. The question is: are we certain the job came from the expected repo?

### 8. WebSocket Security

Worker connections to control plane.

| Setting | Value | Notes |
|---------|-------|-------|
| Origin check | Permissive (all origins) | Mitigated by token auth |
| Message size | 1 MB max | DoS protection |
| Ping/pong | 30s interval | Connection health |

**Things to poke at:**
- [ ] WebSocket hijacking from browser (CSRF-like)
- [ ] Message flooding / DoS
- [ ] Reconnection storms after control plane restart

## Attack Scenarios to Test

### Scenario A: Malicious Webhook
Attacker sends crafted webhook to trigger build of attacker-controlled repo on victim's worker.

- Requires: Guessing/leaking webhook secret, or finding signature bypass
- Test: Fuzz webhook parser with malformed payloads

### Scenario B: Credential Harvesting
Attacker compromises control plane database, extracts forge tokens.

- Impact: Access to user's GitHub/GitLab repos
- Mitigation: Encrypt tokens at rest? Use short-lived tokens?

### Scenario C: Worker Impersonation
Attacker steals worker token, connects fake worker, receives job details (repo URLs, clone tokens).

- Requires: Token theft (logs, memory, network)
- Test: Token rotation, revocation, audit logging

### Scenario D: Supply Chain via Config
Attacker with repo write access modifies `.cinch.yaml` to exfiltrate secrets.

- Note: This is EXPECTED - repo owners control their CI config
- Not a vulnerability in Cinch's threat model

## Security Controls Checklist

| Control | Status | Notes |
|---------|--------|-------|
| TLS everywhere | Required | Workers must connect over WSS |
| Webhook signatures | Required | HMAC-SHA256, constant-time compare |
| Token hashing | Done | SHA3-256 for stored tokens |
| JWT validation | Done | Algorithm check, expiry |
| CSRF protection | Done | OAuth state parameter |
| Open redirect | Mitigated | return_to validated against BaseURL |
| Rate limiting | TODO | Auth endpoints need limits |
| Audit logging | TODO | Token operations, admin actions |
| Secret rotation | TODO | No mechanism for rotating secrets |

## Reporting Security Issues

Email security@cinch.sh (TODO: set up) or open a GitHub security advisory.
