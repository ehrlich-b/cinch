# Cinch TODO

**Goal:** First external user by Sunday 2026-02-02

---

## Launch Blockers

- [ ] **Remove services from README** - Services syntax is shown but not wired up. Confuses first-time users. Services are v2 consideration.
- [ ] **Add `cinch secrets` CLI** - Currently secrets require raw curl to API. Need `cinch secrets set KEY` and `cinch secrets list`.
- [ ] **Deep documentation** - Self-hosting guide needs webhook ingress details

## Security (remaining)

- [ ] **Token in WebSocket URL** - Low priority, v2 protocol change
- [ ] **Worker ID collision** - Close old connection on re-register (tracked in REVIEW.md)

---

## Billing (Post-Launch)

**Status:** Free during beta. Infrastructure ready, Stripe stubbed.

- [ ] Storage quota exceeded → usage breakdown UI
- [ ] Seat limit reached → "Team at capacity" message
- [ ] Log retention warning → "logs expire in X days" for free tier
- [ ] Stripe integration (products, checkout, webhook handler)

---

## Bugs

- [ ] Container config errors should fail with helpful message, not default to ubuntu:22.04
- [ ] Missing Docker should fail with "install Docker or set `container: none`"

---

## After Launch

**User Acquisition:**
- [ ] r/selfhosted post
- [ ] awesome-selfhosted PR
- [ ] Hacker News Show HN
- [ ] Badge on someone else's repo

**Polish:**
- [ ] Zero to green checkmark without questions
- [ ] Error messages that help

---

## Ready When Needed

- **Postgres:** Implemented and tested. Wire DATABASE_URL when needed.

---

## Done (2026-01-23 → 2026-01-31)

Core: Single binary, webhooks, GitHub App, OAuth, job queue, log streaming, badges, Fly.io deployment.

Forges: GitHub, GitLab, Forgejo/Gitea, Codeberg with multi-forge push.

Infrastructure: WebSocket split (ws.cinch.sh), worker reconnect/heartbeat, self-hosting (filesystem logs, secrets, labels), Postgres ready.

Security: Rate limiting (device auth), job ownership verification, worker collision handling, thread-safe Hub.List(), encryption at rest, fork detection, GIT_ASKPASS for credentials.

UI: Landing page, pricing section, worker list, job retry, `cinch status`, `cinch logs -f`.

Worker: Personal/shared modes, trust levels, fork PR approval, daemon (launchd/systemd).
