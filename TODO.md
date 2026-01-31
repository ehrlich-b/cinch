# Cinch TODO

**Last Updated:** 2026-01-30

**Goal:** Someone besides me uses this by Sunday (2026-02-02)

---

## Launch Blockers

### Landing Page - DONE

- ✅ **Hero section** - Headline + subhead + install command + forge logos
- ✅ **How it works** - 3 visual steps with arrows
- ✅ **Pricing section** - Free / Pro / Self-hosted
- ✅ **Why Cinch** - 4 value props + config example
- ✅ **Social proof** - Badge from cinch repo

### Remaining Blockers

- [ ] **Make GitHub App public** - Currently private, strangers can't install
- [ ] **Public webhook ingress guidance** - Clear setup docs for self-hosted

### Security (remaining)

- [ ] **Git token in clone URL** - Token visible in process list. Use git credential helper.
- [ ] **Worker ID collision** - Duplicate IDs overwrite without cleanup.

---

## Billing & Fair Use

**Status:** Infrastructure done, Stripe stubbed, free during beta.

See `design/17-billing-and-teams.md` for full model.

**Pricing (post-beta):** Free (public only, 100MB) / Pro $5/seat/mo (private, 10GB×seats) / Self-hosted (free, unlimited)

**Org Purchasing Flow (when Stripe ready):**
```
1. Account → "Add Organization" → select GitHub/GitLab org you admin
2. Set seat count (5/10/25/custom)
3. Stripe Checkout → webhook creates org_billing record
4. Adjust seats anytime → Stripe quantity update
```

**Graceful Error Handling (MVP):**
- ✅ **Private repo → visible error** - Creates job, shows error in GitHub/GitLab status
- [ ] **Storage quota exceeded** - Usage breakdown, link to manage
- [ ] **Seat limit reached** - "Team at capacity (5/5 seats). Add seats at cinch.sh/billing"
- ✅ **Worker limit reached** - Free: 10, Pro: 1000, shows error on connect
- [ ] **Log retention warning** - Show "logs expire in X days" for free tier

**Stripe Integration (post-launch):**
- [ ] Products/prices, checkout flow, webhook handler, seat tracking

**Enforcement (post-launch):**
- [ ] Storage quota check, storage tracking on complete, log retention cleanup, worker limit

---

## Bugs to Fix

- [ ] **No container config should fail** - Error with helpful message instead of defaulting to ubuntu:22.04
- [ ] **Container-first clarity** - If Docker missing, fail with "install Docker or set `container: none`"

---

## After Launch

### User Acquisition
- [ ] r/selfhosted post
- [ ] awesome-selfhosted PR
- [ ] Hacker News Show HN
- [ ] One badge on someone else's repo

### Install Flow Polish
- [ ] Zero to green checkmark without questions
- [ ] Error messages that help

---

## Ready When Needed

### Postgres
SQLite handles launch. Postgres implemented and tested, ready if needed.
- [ ] Wire DATABASE_URL in main.go
- [ ] Deploy Fly Postgres
- [ ] Data migration

---

## Polish (Post-Launch)

- [ ] Visual polish, badge design, light/dark toggle, loading skeletons
- [ ] Branch protection docs (GitHub rulesets, GitLab/Forgejo)
- [ ] `cinch daemon scale N`, Windows support
- [ ] Artifacts feature

---

## Done

### Infrastructure (2026-01-30)
- ✅ WebSocket endpoint split (ws.cinch.sh)
- ✅ Worker resilience (reconnect, pending results, heartbeat)
- ✅ Self-hosting (filesystem logs, secrets, labels)
- ✅ Security review (all CRITICAL/HIGH fixed, encryption at rest)
- ✅ Billing infrastructure (tier model, org schema, private repo gate, "Give Me Pro" UI)
- ✅ Postgres implementation (ready to deploy)

### MVP 1.9 - Polish (2026-01-29)
- ✅ Retry jobs, `cinch status`, `cinch logs -f`, worker list, remote control, visibility model

### MVP 1.8 - Worker Trust (2026-01-28)
- ✅ Personal/shared modes, trust levels, fork PR approval

### MVP 1.7 - PR Support (2026-01-28)
- ✅ GitHub/GitLab/Forgejo PRs, status checks, UI

### MVP 1.6 - Logs → R2 (2026-01-28)
- ✅ R2 storage, streaming, retrieval

### MVP 1.5 - Daemon (2026-01-28)
- ✅ launchd/systemd, parallelism, graceful shutdown

### MVP 1.4 - Forge Expansion (2026-01-27)
- ✅ GitLab, Forgejo/Gitea, multi-forge push

### MVP 1.0-1.3 - Core (2026-01-23-26)
- ✅ Single binary, webhooks, GitHub App, OAuth, job queue, log streaming, badges, Fly.io

---

## Architecture Reference

```bash
# Job environment variables
CINCH_REF=refs/heads/main
CINCH_BRANCH=main
CINCH_COMMIT=abc1234567890
CINCH_JOB_ID=j_abc123
CINCH_FORGE=github
CINCH_FORGE_TOKEN=xxx
```

```
Cloudflare (CDN + R2) → Fly.io (single node) → SQLite/Postgres
Workers connect via WebSocket, retry on disconnect.
```
