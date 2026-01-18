# Tunnel + Cinch Synergy Analysis

## Context

Bryan has an LLC building a portfolio of micro-SaaS products with the same vibe. One product is a tunnel (ngrok competitor). Cinch is a CI tool. They might work together.

## The Webhook Problem

CI tools need to receive webhooks from forges (GitHub, GitLab, etc). Your laptop doesn't have a public IP.

```
GitHub ──POST /webhook──► ??? ──► your laptop
                          │
                    (how does this work?)
```

## Three Architectural Options

### Option A: Pure Local (Tunnel-Powered)

```
GitHub ──webhook──► tunnel ──► laptop (runs everything)
```

- Cinch is pure software, no hosted component
- User runs: tunnel + control plane + worker
- Cinch revenue: $0
- Tunnel revenue: whatever tunnel charges
- Lifestyle factor: Maximum (nothing to operate for Cinch)
- Problem: Where's the CI money? Is it just tunnel marketing?

### Option B: Minimal Webhook Relay

```
GitHub ──webhook──► cinch.sh (tiny queue) ◄──websocket──► laptop
```

- Hosted component is ~50 lines of code, stateless queue
- User runs: control plane + worker locally
- Revenue: $9/month for "I don't want to run a tunnel"
- Lifestyle factor: High (queue basically can't break)
- Problem: Is webhook relay worth $9/month?

### Option C: Full Hosted Control Plane

```
GitHub ──webhook──► cinch.sh (full service) ◄──► laptop (worker only)
```

- Hosted component is a real service (webhooks, job dispatch, log storage, web UI)
- User runs: worker only
- Revenue: $9/month for real convenience
- Lifestyle factor: Medium (more surface area, more support)
- Problem: You're operating software for strangers

## The Lifestyle Business Constraint

Goal: ~1,759 paying customers at $9/month = ~$190k/year

Anti-goal: 1M free users expecting support, 100K paid users demanding features, hiring employees, getting acquired by Harness

This means:
- Self-hosted users are GOOD (free marketing, zero cost)
- Free hosted tier might be BAD (support cost, no revenue)
- Simplicity is survival (fewer features = fewer support tickets)

## Portfolio Synergy Options

### Synergy Model 1: Tunnel Powers Cinch

- Cinch is OSS-only, no hosted component
- Cinch docs say "use [tunnel product] to receive webhooks"
- Tunnel gets CI users, Cinch gets reputation
- Revenue comes from tunnel, not CI

### Synergy Model 2: Cinch Uses Tunnel Under the Hood

- cinch.sh hosted service uses tunnel tech internally
- User doesn't know or care
- They pay $9/month for "webhooks just work"
- Tunnel is implementation detail, not separate product

### Synergy Model 3: Separate Products, Same Vibe

- Tunnel is tunnel, Cinch is CI
- They happen to work well together
- Cross-promote but don't couple
- Each stands alone financially

## Open Questions

1. Is tunnel currently revenue-generating?
2. Is the goal to make Cinch revenue-generating, or is it marketing/reputation?
3. Would bundling hurt or help? (Complexity vs convenience)
4. What's the support burden tolerance? (0 employees forever?)

## The $9 Question

What is someone actually paying $9/month for?

| Option | What $9 buys |
|--------|--------------|
| Webhook relay only | "I don't want to expose a port" |
| + Log storage | "I want to see logs from my phone" |
| + Web UI | "I want a dashboard" |
| + Full control plane | "I don't want to run anything but the worker" |

The more value, the more justified the $9, but also the more operational burden.

## Recommendation (TBD)

Leaning toward: **Option B (webhook relay) + log storage + simple web UI**

Rationale:
- Minimal operational surface
- Clear value prop ("webhooks work, logs viewable anywhere")
- Can use tunnel tech under the hood
- Doesn't preclude self-hosted (just use your tunnel directly)
- $9/month feels fair for the convenience

But this needs more thought. The tunnel synergy could change the calculus entirely if tunnel is already successful.
