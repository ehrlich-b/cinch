# Cinch TODO

**Goal:** First external user by Sunday 2026-02-02

---

## Launch Blockers

- [x] **Remove services from README** - Services syntax is shown but not wired up. Confuses first-time users. Services are v2 consideration.
- [x] **Add `cinch secrets` CLI** - `cinch secrets list`, `cinch secrets set KEY=VALUE`, `cinch secrets delete KEY`
- [x] **Deep documentation** - Self-hosting guide has webhook ingress options (Cloudflare Tunnel, ngrok, Tailscale Funnel, VPS proxy)
- [x] **README missing `repo add`** - Usage section says `login → worker` but should be `login → repo add → worker` (landing page is correct)

---

## NEXT: Self-Hosting Overhaul

**Problem:** Current "self-hosting" isn't really self-hosted. OAuth apps require creating GitHub Apps (20+ steps) or users implicitly depend on cinch.sh's OAuth. The origin story is "GitHub tried to charge for your own hardware" - we need to actually deliver on that.

### Webhook Relay (Differentiator)

Self-hosters behind NAT can't receive webhooks. Current solutions (Cloudflare Tunnel, ngrok, Tailscale) are extra moving parts. **Cinch should solve this natively.**

**Flow:**
```
1. cinch login                    # Login to cinch.sh (reserves relay ID)
2. cinch server --relay           # Connects outbound to cinch.sh
3. GitHub → cinch.sh/relay/x7k9m  # Webhook forwarded over WebSocket
4. Your server validates locally  # Webhook secret never leaves your machine
```

**Implementation:**
- [ ] Relay server on cinch.sh (~200 LOC) - accepts WebSocket, forwards HTTP
- [ ] `--relay` flag for `cinch server` (~150 LOC) - connects to relay, handles forwarded requests
- [ ] Relay ID persistence - stored in SQLite, tied to user account
- [ ] Protocol: JSON envelope `{method, path, headers, body}` → `{status, headers, body}`

**Security:** Relay is a dumb pipe. Webhook secrets validated locally. Relay URL is random/unguessable.

### Token-Based Auth for Self-Hosting

**Problem:** Workers need to auth to self-hosted server, but current flow uses OAuth which requires creating apps.

**Solution:** Environment variables for context, token-based auth.

- [ ] `CINCH_URL` env var - URL of Cinch server (e.g., `http://localhost:8080` or `https://cinch.sh`)
- [ ] `CINCH_TOKEN` env var - auth token for that server
- [ ] Server generates admin token on first run, stores in SQLite
- [x] JWTs signed with server's `CINCH_SECRET_KEY` (independent of cinch.sh)

**Key insight:** `cinch login` to cinch.sh reserves a relay ID (tied to user's GitHub/etc account). The self-hosted server then uses that relay but issues its **own** JWTs. Workers never talk to cinch.sh - only to the self-hosted server.

**Flow:**
```bash
# On coordinator machine
cinch login                      # Login to cinch.sh with GitHub OAuth
                                 # This reserves a rotatable relay token

cinch server --relay             # Connects to cinch.sh relay
                                 # Prints: relay URL + admin token
                                 # Server issues its own JWTs

# On worker machine (never talks to cinch.sh)
export CINCH_URL=http://coordinator:8080
export CINCH_TOKEN=cinch_adm_xxx
cinch worker                     # Connects to self-hosted server only
```

### Manual Mode (Skip OAuth Apps on Forges)

**Problem:** `cinch repo add` uses OAuth to auto-create webhooks. But OAuth requires creating apps.

**Solution:** Manual mode - user adds webhook themselves, provides token via CLI.

- [ ] `cinch repo add --manual` - prints webhook URL + secret, user adds in forge UI
- [ ] `--token` flag or `CINCH_FORGE_TOKEN` - PAT for posting status checks
- [ ] Skip OAuth dance entirely for single-user setups

**Flow:**
```bash
cinch repo add owner/repo --manual --token ghp_xxx
# Output:
# Add this webhook to your repo:
#   URL: https://cinch.sh/relay/x7k9m/webhooks/github
#   Secret: whsec_abc123
#   Events: Push, Pull Request, Create
```

### Fully Independent Path (No Relay)

For users who want zero cinch.sh dependency:

- [ ] Document the "bring your own ingress" path clearly
- [ ] Works today, just needs better docs
- [ ] User manages: webhook ingress (tunnel/VPS), OAuth apps if multi-user

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
