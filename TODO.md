# Cinch TODO

**Goal:** First external user by Sunday 2026-02-02

---

## Launch Blockers

- [x] **Remove services from README** - Services syntax is shown but not wired up. Confuses first-time users. Services are v2 consideration.
- [x] **Add `cinch secrets` CLI** - `cinch secrets list`, `cinch secrets set KEY=VALUE`, `cinch secrets delete KEY`
- [x] **Deep documentation** - Self-hosting guide has webhook ingress options (Cloudflare Tunnel, ngrok, Tailscale Funnel, VPS proxy)
- [x] **README missing `repo add`** - Usage section says `login → worker` but should be `login → repo add → worker` (landing page is correct)

---

## Self-Hosting (DONE)

The self-hosting overhaul is complete:

- [x] **Webhook Relay** - `cinch server --relay` connects outbound to cinch.sh, webhooks forwarded over WebSocket
- [x] **Token-Based Auth** - `CINCH_URL` + `CINCH_TOKEN` env vars for workers, server prints admin token on startup
- [x] **Org Tokens** - `CINCH_GITHUB_TOKEN` / `CINCH_GITLAB_TOKEN` / `CINCH_FORGEJO_TOKEN` on server auto-creates webhooks
- [x] **Per-user JWTs** - Self-hosted server issues its own JWTs signed with `CINCH_SECRET_KEY`

**Self-hosted flow:**
```bash
# Coordinator machine
cinch login                      # Login to cinch.sh (reserves relay ID)
cinch server --relay             # Connects to relay, prints admin token

# Worker machine
export CINCH_URL=http://coordinator:8080
export CINCH_TOKEN=<admin-token>
cinch worker                     # Connects to self-hosted server only
```

---

## Self-Hosting Docs Gaps

From simulated r/selfhosted feedback:

- [ ] **Security / Threat Model** - What's isolated, what's not, fork PR handling, worker trust
- [ ] **Resource requirements** - RAM, CPU, disk estimates (it's minimal, just say so)
- [ ] **Upgrade procedure** - "Replace binary, restart" - document explicitly
- [ ] **Backup procedure** - Beyond "copy .db" - mention `sqlite3 .backup`, what else to save
- [ ] **No telemetry statement** - Important to this audience, add to docs
- [ ] **What `repo add` actually does** - Creates webhook? Requires OAuth? Make explicit

---

## Security (Low Priority)

- [ ] **Token in WebSocket URL** - v2 protocol change
- [ ] **Worker ID collision** - Close old connection on re-register (tracked in REVIEW.md)

---

## Target Audience

**Primary:** Forgejo/Gitea self-hosters (underserved by CI tools)
**Secondary:** r/selfhosted generalists
**Tertiary:** GitHub users who want their own hardware

**Why Forgejo first:**
- GitLab has GitLab CI built-in
- GitHub has Actions built-in
- Forgejo has... Woodpecker? Gitea Actions (rough)
- Cinch can own this niche

**Positioning:**
- r/forgejo: "Native Forgejo CI, 5 minutes to first build, no OAuth apps needed"
- r/selfhosted: Lead with Forgejo story, "also works with GitHub/GitLab"

---

## Billing (Post-Launch)

**Status:** Free during beta. Infrastructure ready, Stripe stubbed.

- [ ] Storage quota exceeded → usage breakdown UI
- [ ] Seat limit reached → "Team at capacity" message
- [ ] Log retention warning → "logs expire in X days" for free tier
- [ ] Stripe integration (products, checkout, webhook handler)

---

## Bugs

- [ ] **Device login only offers GitHub** - `cinch login` device flow is hardcoded to GitHub OAuth. Should show forge picker (GitHub/GitLab/Codeberg) or accept `cinch login --forge gitlab`
- [ ] **Invalid JWT shows "connection closed" instead of clear error** - Worker disconnects with generic "event stream error: connection closed" when JWT is invalid/expired. Should show "invalid credentials" or "token expired, run `cinch login`"
- [ ] **Device login flow redirects twice** - Goes device code page → GitHub login → device code page again. Should go straight to success after GitHub auth
- [ ] Container config errors should fail with helpful message, not default to ubuntu:22.04
- [ ] Missing Docker should fail with "install Docker or set `container: none`"

---

## After Launch

**User Acquisition (order matters):**
1. [ ] r/forgejo post - underserved audience, most likely to try it
2. [ ] r/selfhosted post - broader reach, lead with Forgejo story
3. [ ] awesome-selfhosted PR
4. [ ] Hacker News Show HN
5. [ ] Badge on someone else's repo

**Self-Hosting Polish:**
- [ ] Prometheus metrics endpoint (`/metrics`) - job counts, worker counts, durations
- [ ] Log retention (`CINCH_LOG_RETENTION_DAYS` env var, background cleanup job)
- [ ] Auto-prompt PAT setup via relay if server has no org token configured

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
