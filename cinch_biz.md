# Cinch Business Philosophy

## The Opportunity

GitHub announced they're charging **$0.002/minute for self-hosted runners** starting March 2026. Read that again: they're billing you to use *your own hardware*.

At 50,000 minutes/month that's $100. At 100,000 minutes, $200. For the privilege of GitHub's orchestration layer touching jobs that run on machines you own, power, and maintain.

This isn't a bug. This is platform capitalism working as intended. First you get hooked on free. Then they charge.

## The Philosophy: Fail Open and Free

Cinch operates on a different principle:

**"When I die, you'll have to host the control plane."**

This means:
- **MIT licensed forever** - The entire codebase is yours. Fork it. Run it. Modify it.
- **Self-hostable** - `cinch server` runs anywhere. Your laptop. A $5 VPS. A Raspberry Pi.
- **Fail open** - If cinch.sh (the hosted service) disappears tomorrow, your CI keeps working. Download the binary, run `cinch server`, point your workers at it.

No VC means no board meetings where someone asks "how do we monetize the free tier?" No "strategic pivot" where your CI becomes a data product. No acqui-hire that sunsets the service.

Solo dev means decisions are fast and values don't drift. It also means when I'm gone, you're on your own. That's the deal.

## Competitive Analysis

### The Problem with Every Alternative

| Product | Model | The Catch |
|---------|-------|-----------|
| **GitHub Actions** | Per-minute + control plane fee | Now charging for your own hardware. $0.002/min on self-hosted. Lock-in to GitHub. |
| **GitLab CI** | Bundled with GitLab ($29/user Premium) | Tied to GitLab. Not multi-forge. Complex YAML DSL. |
| **CircleCI** | Usage-based credits | Enterprise pricing. Complex. Another vendor to trust with secrets. |
| **Buildkite** | Per-seat | Enterprise-focused. $29+/user/month. Great product, wrong customer (me). |
| **Jenkins** | Free (self-hosted) | Java. Groovy DSLs. Plugin hell. DevOps complexity. Remembers the Obama administration. |
| **Woodpecker CI** | Free (self-hosted only) | No hosted option. You run everything yourself. Fork of Drone, community-maintained. |
| **Drone CI** | Free/Enterprise split | License shenanigans. Harness acquired it. |

### Where Cinch Fits

Cinch is **Woodpecker with a hosted control plane option**.

Same philosophy: simple, container-native, one config file. But with a key addition: you don't *have* to run the control plane yourself if you don't want to.

**Free tier (self-hosted)**: Run everything yourself. MIT licensed. No limits, ever.

**Free tier (hosted)**: Public repos, unlimited builds, unlimited repos. Your dev machine is the runner - you bring the compute, we bring the coordination. This is the Travis model, resurrected. Cost to operate: ~$0.01/repo/month (webhook relay, log storage, WebSocket coordination). No account required beyond forge connection.

**Paid tier (Pro)**: $5/seat/month. Unlimited private repos, unlimited builds. The paywall is privacy, not build count.

### What's a Seat?

A **seat** is a unique identity that triggered a build on a private repo within the billing period. That's it.

- Detected automatically from webhook payload (`pusher.name` on GitHub, `user_username` on GitLab, equivalent on other forges)
- No cinch user accounts required — identity comes from the forge
- Bots/service accounts that push code count as seats

One SKU in Stripe: `cinch_pro_seat` @ $5/month, quantity-based. Customers purchase a quantity of seats. No tiers in the billing system — just `qty × $5`.

### Pricing Page Presentation

Present as suggested bundles for psychological anchoring, but actual billing is linear:

| | Solo | Team | Business |
|--|------|------|----------|
| Suggested for | Just you | Small teams | Larger orgs |
| Starting at | $5/mo | $25/mo | $75/mo |
| Seats included | 1 | 5 | 15 |
| Additional seats | +$5/seat | +$5/seat | +$5/seat |
| Private repos | Unlimited | Unlimited | Unlimited |
| Builds | Unlimited | Unlimited | Unlimited |

These bundles are marketing — checkout is just "how many seats do you need?" and Stripe multiplies.

### Usage Tracking & Enforcement

**Dashboard shows:**
- Unique contributors to private repos this billing period
- Current seat count purchased
- Soft warning if usage exceeds purchased seats

**Enforcement approach (soft):**
- Builds continue even if over seat count
- Dashboard warns: "You had 12 unique contributors this month but are paying for 5 seats. Please adjust your seat count."
- Grace period for occasional overages (contractor pushed once, etc.)
- Reserve right to enforce for egregious/persistent abuse

**Not doing (for now):**
- Auto-upgrade
- Build blocking
- Hard enforcement

**Why per-seat?**
- Per-seat scales naturally with team size
- Self-serve: no sales conversations, just adjust your seat count
- A solo dev pays $5. A 15-person team pays $75. Both feel fair.
- No metering compute, no counting repos, no complexity

## Pricing: Why $5/seat

### The Per-Seat Math

At $5/seat, revenue scales with team size naturally:

| Team Size | Monthly | Annual |
|-----------|---------|--------|
| Solo dev | $5 | $60 |
| Small team (5) | $25 | $300 |
| Medium team (15) | $75 | $900 |
| Larger team (50) | $250 | $3,000 |

This is meaningful revenue without requiring enterprise sales conversations.

### Competitive Positioning

| Service | Model | 15-person team cost |
|---------|-------|---------------------|
| **cinch.sh** | $5/seat | **$75/mo** |
| Buildkite | $15/seat | $225/mo |
| CircleCI | Credit-based | $50-150+/mo |
| GitHub Actions | Free* (*for now) | $0 but lock-in risk |

**Positioning statement:** "Buildkite is a full CI platform. Cinch is a webhook and a green checkmark. One-third the features, one-third the price."

### Why $5 Works Now

The old concern about $5 being too low assumed per-repo pricing. At $5/repo, you need thousands of repos to make meaningful revenue.

Per-seat changes the math:
- Average team has 5-15 engineers
- Average revenue per customer: $25-75/month (not $5)
- $5 is the *unit price*, not the customer price
- Still low enough that nobody needs manager approval

### Underpricing Risk Addressed

The risk with low prices is attracting bargain hunters who churn fast. But:
- CI is sticky — once it works, people don't touch it
- Free tier absorbs the price-sensitive hobbyists
- Paying customers have private repos = real projects = real teams
- Per-seat naturally filters for customers worth having

## Why "Yet Another" CI Tool?

The honest answer: **because this one is the one I own**.

Every other CI tool is one acquisition, one board meeting, one "strategic refocus" away from ruining your day. GitHub was fine until Microsoft needed to show Actions revenue. Drone was fine until Harness bought it.

The only CI tool that won't turn on you is the one you control.

With Cinch:
- **I control the hosted version** - my servers, my decisions
- **You can control the self-hosted version** - MIT licensed, run it forever
- **Nobody else is in the room** - no investors, no acquirers, no product managers

Is the world crying out for another CI tool? No. But the world isn't crying out for any SaaS product. People build them because they want to own their tools and their income. This is mine.

## The Numbers to Hit

**Target**: $180K ARR = $15K MRR = **3,000 seats at $5/month**

Assuming average team size of 10, that's ~300 paying customers. Here's the path:

| Stage | Seats | Avg Team | Customers | MRR | ARR |
|-------|-------|----------|-----------|-----|-----|
| Launched | 100 | 5 | 20 | $500 | $6,000 |
| Product-market fit | 400 | 8 | 50 | $2,000 | $24,000 |
| Word of mouth working | 1,000 | 10 | 100 | $5,000 | $60,000 |
| Real business | 2,000 | 10 | 200 | $10,000 | $120,000 |
| **Target** | **3,000** | **10** | **300** | **$15,000** | **$180,000** |

The free tier (public repos) isn't charity - it's marketing. Users start with public repos, their team grows, they add private repos, they pay. The big OSS projects using Cinch for free are paying in credibility, not cash.

These are small numbers by VC standards. That's the point. I'm not building a unicorn. I'm building a tool that works, charges fairly, and doesn't betray its users.

## The Moat

There is no moat. It's a CI tool. The "moat" is:

1. **First to market with this specific positioning** - multi-forge, flat-rate, fail-open
2. **Trust** - years of not screwing users builds reputation
3. **Switching cost** - once CI works, people don't touch it

The real protection is staying small enough that it's not worth acquiring or competing with. A $300k/year business is invisible to GitHub. A $30M/year business is a threat to be crushed.

## Future Upsells (Not v1)

### Pricing Expansions

- **Volume discount at 50+ seats**: $4/seat (20% off)
- **Annual billing**: 2 months free ($50/seat/year instead of $60)
- **Enterprise tier for 100+ seats**: Custom pricing, SLA, priority support

### Add-on Services

If this takes off, there's room for pay-as-you-go add-ons:

**Managed Workers**
- Spin up Fly.io (or DO, or whatever) workers on demand
- User pays compute at cost + small margin
- "I don't want to run my own worker" → we run it for you
- Natural upgrade from "my laptop is the runner"

**Artifact/Cache Storage**
- S3-compatible storage for build artifacts and caches
- User pays storage at cost + small margin
- "I want my artifacts hosted somewhere" → we host them

These are "oh shit this is working" expansions, not v1 scope. The core product is control plane + BYOW. Add-ons are for users who want to throw money at convenience.

**The math:**
- Fly.io compute: ~$0.01-0.05/min depending on size
- Charge: cost + 20%?
- S3 storage: ~$0.023/GB/month
- Charge: cost + 20%?

Low margin but high convenience. Users who want managed everything can get it. Users who want BYOW pay $5/seat flat. Everyone's happy.

## Implementation Notes

- Stripe Checkout with quantity selector for seats
- Webhook payload parsing already captures pusher identity — store and count unique values per billing period per customer
- Dashboard needs: current seat count, unique pushers this period, upgrade/adjust CTA
- No forge API integration needed for seat counting — data flows through existing webhook path

## Conclusion

Cinch is a bet that there's a market for:
- CI that works
- Pricing that's predictable
- A vendor who won't sell out

It's not a bet on explosive growth. It's a bet on sustainable operation.

The GitHub pricing change opened a window. People are Googling "GitHub Actions alternatives" right now. The goal is to be there when they look, with a product that's simple, honest, and priced fairly.

**Public repos: Free. Unlimited. The Travis magic, resurrected.**

**Private repos: $5/seat/month. Unlimited repos. Unlimited builds.**

**Self-hosted: MIT licensed forever. When I die, you keep your CI.**

Run your commands. Get green checks. Keep your money.
