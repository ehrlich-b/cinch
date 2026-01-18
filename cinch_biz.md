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

**Free tier (hosted)**: Public repos, your dev machine as the runner. 50 builds/month/repo, 7 day log retention, no scheduled builds. Push code, your laptop builds it, green check appears. Cost to operate: ~$0.01/repo/month.

**Paid tier**: $9/month/repo. Private repos, unlimited builds, 30 day log retention, scheduled builds. Natural upgrade when you need more.

| | Free (hosted) | Paid |
|---|---|---|
| Public repos | ✓ | ✓ |
| Private repos | ✗ | ✓ |
| Builds/month | 50/repo | Unlimited |
| Log retention | 7 days | 30 days |
| Scheduled builds | ✗ | ✓ |
| Price | $0 | $9/repo/month |

## Pricing: $5 vs $10 vs $9

### The Case for Not Going Too Low

Research says founders who price at $5/month enter the "underpricing trap":
- Thin margins
- Attracts bargain hunters who churn fast and demand support
- Hard to raise prices later (15-30% churn when you do)
- Creates a hamster wheel: lots of customers, no money

A [study in Quantitative Marketing and Economics](https://link.springer.com/journal/11129) found prices ending in "9" increase purchases by up to 24%. The psychological difference between $9 and $10 is significant - $9 feels "in the single digits" while $10 crosses into "teens."

### What Does $5 Actually Buy?

At $5/month:
- 100 customers = $500/month = hobby project
- 1000 customers = $5000/month = "ramen profitable" at best

At $9/month:
- 100 customers = $900/month
- 1000 customers = $9000/month = sustainable solo business

The effort to support 100 vs 1000 customers is roughly the same until you hit scale issues. Might as well charge what makes the math work.

### But $10 is Cleaner

$10/month:
- Clean number
- Easy mental math
- "One Hamilton a month"
- 100 customers = $1000/month
- 1000 customers = $10,000/month

The question: does the $1 delta matter?

For a CI product targeting developers who understand costs, probably not. Developers do math. They'll compare $10/month to GitHub's per-minute billing or Buildkite's per-seat pricing and see it's cheap either way.

### Recommendation: $9/month

Go with $9. It's:
- Psychologically "single digits"
- Charts better than $10 in SaaS pricing research
- Still meaningful revenue per customer
- Clearly cheaper than alternatives at a glance
- Low enough that nobody needs manager approval

If it doesn't matter to the customer (and it probably doesn't), take the behavioral economics win.

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

**Sustainability threshold**: $5000 MRR (556 customers at $9)
- Covers hosting costs
- Covers my time as "worth it"
- Proves product-market fit

**"Real business" threshold**: $10,000 MRR (1,112 customers at $9)
- Sustainable solo income
- Enough buffer for growth investments
- Enough that I'd turn down job offers

**"Dream" threshold**: $25,000 MRR (2,778 customers at $9)
- Hire help for support
- Actually take vacations
- Comfortable indefinitely

These are small numbers by VC standards. That's the point. I'm not building a unicorn. I'm building a tool that works, charges fairly, and doesn't betray its users.

## The Moat

There is no moat. It's a CI tool. The "moat" is:

1. **First to market with this specific positioning** - multi-forge, flat-rate, fail-open
2. **Trust** - years of not screwing users builds reputation
3. **Switching cost** - once CI works, people don't touch it

The real protection is staying small enough that it's not worth acquiring or competing with. A $300k/year business is invisible to GitHub. A $30M/year business is a threat to be crushed.

## Future Upsells (Not v1)

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

Low margin but high convenience. Users who want managed everything can get it. Users who want BYOW pay $9 flat. Everyone's happy.

## Conclusion

Cinch is a bet that there's a market for:
- CI that works
- Pricing that's predictable
- A vendor who won't sell out

It's not a bet on explosive growth. It's a bet on sustainable operation.

The GitHub pricing change opened a window. People are Googling "GitHub Actions alternatives" right now. The goal is to be there when they look, with a product that's simple, honest, and priced fairly.

$9/month. Flat. Forever. MIT licensed.

**Run your commands. Get green checks. Keep your money.**
