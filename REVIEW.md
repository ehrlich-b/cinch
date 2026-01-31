# Security Review

**Date:** 2026-01-31
**Status:** Production-ready

---

## Summary

All HIGH and MEDIUM priority security issues addressed. Codebase is clean (~28k LOC Go). Ready for production use.

**Remaining:** Token in WebSocket URL (low priority, requires v2 protocol change)

---

## Fixed Issues

| Priority | Issue | Status |
|----------|-------|--------|
| HIGH | Rate limiting on device auth | ✅ 10 attempts/min per IP, 5-min block |
| HIGH | Job ownership verification | ✅ Workers can only complete assigned jobs |
| HIGH | Git credential leakage | ✅ Uses GIT_ASKPASS instead of URL-embedded tokens |
| MEDIUM | Hub.List mutable pointers | ✅ Returns copies, not pointers |
| MEDIUM | Worker ID collision | ✅ Closes old connection before replacing |
| MEDIUM | Origin check bypass | ✅ Exact host matching |
| MEDIUM | Device code entropy | ✅ 32^8 combinations (was 10,000) |
| MEDIUM | Private repo filtering | ✅ Unauth users can't see private repos/jobs |
| MEDIUM | Open redirect | ✅ Exact hostname validation |
| MEDIUM | Fork detection | ✅ Trust levels for fork PRs |

---

## Architecture Strengths

- Trust model complete (personal vs shared workers, fork detection)
- Encryption at rest (AES-256-GCM with migration)
- Clean control plane / worker / storage separation
- WebSocket reconnection and job re-queuing
- Structured logging throughout

---

## Production Readiness

| Area | Status |
|------|--------|
| Authentication | ✅ JWT, OAuth, device auth with rate limiting |
| Authorization | ✅ Private repo filtering, fork trust model, job ownership |
| Data integrity | ✅ Encryption at rest, proper transactions |
| Reliability | ✅ Job re-queuing, worker collision handling |
| Security | ✅ Rate limiting, entropy, ownership checks |
| Scalability | ✅ SQLite default, Postgres ready |

---

## Low Priority / Future

- **Token in URL** (`ws.go:179`) - Appears in logs. Requires coordinated worker update for v2.
- **Cleanup goroutine per device auth** (`auth.go:955`) - Minor, redundant goroutines under load.
- **Tokens table ownership** - No user_id FK, would need migration.
