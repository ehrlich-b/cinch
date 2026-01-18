# cinch.sh

**CI that's a Cinch.**

One config. Every forge. Your hardware.

---

## The Problem

You push code. You want a green checkmark. Simple, right?

Except now you need to:

- Learn GitHub's proprietary YAML workflow syntax
- Trust Microsoft with your source code, secrets, and build patterns
- Pay per-minute for compute (or use slow shared runners)
- Rewrite everything when you move to GitLab, Forgejo, or self-hosted git

And then [this happened](https://github.com/orgs/community/discussions/182089):

> **GitHub will charge $0.002/minute for self-hosted runners starting March 2026.**

That's right. Microsoft wants to charge you to use **your own CPU**. Your own electricity. Your own hardware sitting under your own desk.

The community response was immediate:

> *"Per minute charges for self hosted runners? Wtf? Those minutes are MY processing time."*

> *"How are you charging me to use MY hardware?"*

> *"This is genuinely one of the worst decisions I've seen from GitHub."*

They walked it back (for now). But they showed their hand. Your CI is their leverage.

**cinch will never charge you to use your own CPU.** That's absurd.

---

## The Solution

**cinch** is CI stripped to its essence:

```yaml
# .cinch.yaml - the entire config file
command: make ci
```

That's it. That's your CI configuration.

**cinch** receives webhooks from your forge, tells your machine to run a command, and posts the result back as a status check. No pipeline DSL. No action marketplace. No 500-line YAML workflows.

Your `Makefile` (or `package.json` scripts, or shell script, or whatever) is the pipeline. We just run it.

---

## How It Works

```
YOUR MACHINE                          cinch.sh (hosted or self-hosted)
════════════════                      ══════════════════════════════════

┌──────────────┐                      ┌────────────────────────┐
│    cinch     │◄───── websocket ────►│    Control Plane       │
│   worker     │                      │                        │
│              │                      │  • Receives webhooks   │
│  • Connects  │                      │  • Dispatches jobs     │
│  • Clones    │                      │  • Posts status checks │
│  • Containers│                      │  • Streams logs to UI  │
│  • Runs cmd  │                      │                        │
│  • Streams   │                      │                        │
└──────────────┘                      └────────────────────────┘
       │                                        ▲
       │                                        │
       ▼                                        │

 Your CPU                              GitHub / GitLab / Forgejo
 Your cache                            Gitea / Bitbucket / etc.
 Your secrets
 Your network                          (Any forge with webhooks)
```

1. Install `cinch` on any machine (your laptop, a VPS, a Raspberry Pi, a beefy server)
2. Point it at the control plane (ours at $9/mo, or self-host for free)
3. Add a webhook to your repo
4. Push code
5. Green checkmarks appear on GitHub, GitLab, Forgejo—wherever your repo lives

---

## Quick Start

**Self-hosted (free forever):**

```bash
# Install cinch (single binary, does everything)
curl -sSL https://cinch.sh/install.sh | sh

# Run the control plane
cinch server

# In another terminal, run a worker
cinch worker --server http://localhost:8080 --token <your-token>
```

**Hosted ($9/mo):**

```bash
# Just install and connect
curl -sSL https://cinch.sh/install.sh | sh
cinch worker --server https://cinch.sh --token <your-token>
```

Add `.cinch.yaml` to your repo:

```yaml
command: make ci
```

Add a webhook to your forge. Push code. Get green checks.

---

## The Config

```yaml
# .cinch.yaml

# Required: what to run
command: make ci

# Optional: container (defaults to auto-detect devcontainer/Dockerfile)
container:
  image: node:20-alpine     # explicit image
  # OR
  # devcontainer: true      # use .devcontainer/ (default if exists)
  # OR
  # dockerfile: ./Dockerfile.ci

# Optional: persistent caches (mounted into container)
cache:
  - name: deps
    path: ./node_modules

# Optional: when to run
trigger:
  branches: [main, develop]
  pull_requests: true
  paths: ["src/**", "tests/**"]       # only trigger on these paths
  paths_ignore: ["docs/**", "*.md"]   # skip these

# Optional: runner selection (if you have multiple machines)
runner:
  labels: [linux, amd64]

# Optional: limits
timeout: 30m
```

**That's the entire schema.** There's no `jobs:`, no `steps:`, no `uses:`, no `with:`, no `matrix:`, no `needs:`, no `if:`, no `runs-on:`.

Your build tool handles the complexity. We just invoke it.

Prefer JSON? Use `.cinch.json` instead. We support both.

---

## CLI Commands

`cinch` is a single binary that does everything:

```bash
cinch server              # Run the control plane
cinch worker              # Run a worker (connects to control plane)
cinch run "make test"     # Run a command locally (test your CI)
cinch status              # Check status of current repo's builds
cinch logs <job-id>       # Stream logs from a job
cinch config              # Validate your config
```

---

## Why Your Machine?

**Reproducible builds with warm caches.**

Builds run in containers by default. Got a `.devcontainer/` in your repo? cinch uses it automatically. Same environment in dev, same environment in CI.

But unlike GitHub Actions, your caches persist across builds. Node modules, cargo registry, pip packages—all mounted as volumes that survive container restarts. First build downloads dependencies. Every build after? Instant.

**Your secrets stay yours.**

With hosted CI, your AWS keys, npm tokens, and database credentials live on someone else's server. With **cinch**, secrets are environment variables on your machine. They never leave.

**Your hardware is faster and cheaper.**

That M4 Max sitting on your desk? It's faster than GitHub's shared runners. That $20/mo VPS? It's yours 24/7, not metered by the minute.

**You're not locked in.**

Move from GitHub to GitLab? Your `.cinch.yaml` doesn't change. Move from GitLab to Forgejo? Still works. The same config posts green checks everywhere.

---

## Multi-Forge Support

Most CI tools are married to one forge:

- GitHub Actions → GitHub only
- GitLab CI → GitLab only
- Forgejo Runner → Forgejo only

**cinch** posts status checks to all of them:

| Forge | Webhooks | Status Checks |
|-------|----------|---------------|
| GitHub | ✓ | ✓ |
| GitLab | ✓ | ✓ |
| Forgejo | ✓ | ✓ |
| Gitea | ✓ | ✓ |
| Bitbucket | ✓ | ✓ |

**Same repo mirrored to GitHub and Codeberg?** One config, green checks on both.

---

## What We Don't Do (By Design)

| Feature | GitHub Actions | cinch | Why |
|---------|---------------|-------|-----|
| Pipeline DSL | 500-line YAML | `command: make ci` | Your Makefile is the pipeline |
| Action marketplace | 15,000+ actions | None | Your devcontainer has your tools |
| Secrets management | Encrypted in their cloud | Your env vars | Your secrets, your machine |
| Caching service | `actions/cache@v4` | Persistent volumes | Automatic, no config needed |
| Matrix builds | `strategy.matrix` | Run the command twice | KISS |
| Artifact storage | GitHub's servers | Your filesystem (or your S3) | We're coordination, not storage |

**We are aggressively simple.** Every feature we don't have is a feature that can't break, can't have security vulnerabilities, and can't lock you in.

---

## Pricing

| Tier | Price | What You Get |
|------|-------|--------------|
| **Self-Hosted** | $0 forever | Everything. Host the control plane yourself. MIT licensed. |
| **Hosted** | $9/month flat | We run the control plane. Unlimited repos, unlimited workers, unlimited builds. |

No per-seat pricing. No per-minute metering. No "contact sales." No charging you to use your own hardware.

---

## The Tech

**Single Binary:** Go, compiles to one executable. Contains: control plane server, worker daemon, CLI tools, web UI assets. ~15MB. No dependencies.

**Containers:** Builds run in Docker by default. Auto-detects your devcontainer or Dockerfile. Persistent cache volumes keep npm/cargo/pip warm. `container: none` in config if you really want bare metal.

**Database:** SQLite for self-hosted, Postgres for hosted. That's it.

**Protocol:** WebSocket + JSON. Workers connect outbound (no firewall holes). Jobs are: clone, run in container, stream logs, report status.

**Security:**
- Workers connect outbound (no open ports required)
- Clone URLs use short-lived tokens (1 hour expiry)
- Secrets never transit the control plane
- Container isolation by default

---

## License

MIT. Forever.

If cinch.sh disappears, you self-host. Your CI keeps working. The code is yours.

---

**Your build system is the product. We're just the messenger.**

[cinch.sh](https://cinch.sh)
