# Cinch Competitive Analysis

Deep dive into the CI/CD landscape. Know thy enemy.

## The Big Picture

| Tier | Players | Revenue Model | Target |
|------|---------|---------------|--------|
| **Big Tech** | GitHub Actions, GitLab CI, Bitbucket Pipelines | Platform lock-in, compute fees | Everyone (bundled) |
| **Enterprise** | CircleCI, Buildkite, TeamCity, Bamboo | Per-seat, usage-based | Large teams, enterprises |
| **Self-Hosted OSS** | Jenkins, Woodpecker, Drone, Dagger | Free / enterprise upsell | DIY teams, privacy-focused |
| **Niche** | Semaphore, Codefresh, Earthly, Depot | Various | Specific use cases |
| **Cinch** | ? | Per-repo, BYOC | Weekend warriors, multi-forge, simplicity zealots |

---

## Tier 1: Big Tech (The Bundled Giants)

### GitHub Actions

**What it is:** CI/CD built into GitHub. YAML workflows, marketplace of actions, hosted runners.

**Pricing:**
- Public repos: Free, unlimited minutes
- Private repos: 2,000 free minutes/month, then $0.008/min (Linux)
- Self-hosted runners: **$0.002/min starting March 2026** (the opening for Cinch)

**Strengths:**
- Zero setup for GitHub users
- Massive ecosystem (20,000+ actions in marketplace)
- Deep GitHub integration (issues, PRs, packages)
- Free for open source

**Weaknesses:**
- GitHub lock-in (worthless if you use GitLab/Forgejo)
- YAML complexity spirals out of control
- Cold caches on every build (unless you fight the actions/cache action)
- The March 2026 self-hosted runner fee is pissing people off
- Debugging is painful (no SSH into runner)
- Minutes-based billing is unpredictable

**Why people would switch to Cinch:**
- Multi-forge (they use Forgejo or GitLab)
- Warm caches (their builds are slow)
- Simplicity (they hate YAML spaghetti)
- Principle (they don't want to pay for self-hosted runner orchestration)

**Market share:** ~40% of CI market. The default choice.

---

### GitLab CI

**What it is:** CI/CD built into GitLab. `.gitlab-ci.yml`, tightly integrated with GitLab features.

**Pricing:**
- Free tier: 400 CI/CD minutes/month on shared runners
- Premium: $29/user/month (includes 10,000 minutes)
- Ultimate: $99/user/month
- Self-hosted runners: Free (no orchestration fee... yet)

**Strengths:**
- Best-in-class DevOps integration (issues, MRs, security scanning, deployment)
- Auto DevOps (zero-config CI for standard stacks)
- Good self-hosted story (run your own GitLab + runners)
- DAG pipelines, includes, extends (powerful if you need it)

**Weaknesses:**
- GitLab lock-in
- Complexity rivals GitHub Actions
- Slow UI, bloated product
- Per-user pricing hurts large teams
- If you're not all-in on GitLab, it's overkill

**Why people would switch to Cinch:**
- They don't use GitLab (Cinch works with anything)
- They want simplicity (one command, not a YAML DSL)
- Per-repo pricing is cheaper than per-user for small teams with many repos

**Market share:** ~15% of CI market. Strong in enterprises.

---

### Bitbucket Pipelines

**What it is:** CI/CD built into Bitbucket. YAML config, integrated with Atlassian ecosystem.

**Pricing:**
- Free: 50 build minutes/month
- Standard: $3/user/month (2,500 minutes)
- Premium: $6/user/month (3,500 minutes)
- Additional minutes: $10/1,000 minutes

**Strengths:**
- Cheap entry point
- Good Jira integration
- Simple for basic use cases

**Weaknesses:**
- Atlassian ecosystem lock-in
- Limited marketplace/ecosystem
- Fewer features than GitHub/GitLab
- Atlassian's reputation for slow, bloated software
- Nobody's first choice - it's "we're already on Bitbucket"

**Why people would switch to Cinch:**
- Escape Atlassian
- Multi-forge support
- Simpler pricing

**Market share:** ~10% of CI market. Declining.

---

## Tier 2: Enterprise CI

### CircleCI

**What it is:** Standalone CI/CD platform. Config-as-code, Docker-native, orbs (reusable config).

**Pricing:**
- Free: 6,000 build minutes/month (limited concurrency)
- Performance: $15/user/month + usage
- Scale: Custom pricing
- Self-hosted runners: Available on paid plans

**Strengths:**
- Fast builds (good caching, parallelism)
- Orbs ecosystem (reusable config packages)
- Good Docker support
- SSH debugging into failed builds
- Insights/analytics

**Weaknesses:**
- January 2023 security breach (secrets leaked, trust damaged)
- Complex pricing (credits, resource classes, concurrency)
- Config complexity rivals GitHub Actions
- Another vendor to trust with your secrets
- Enterprise-focused sales motion

**Why people would switch to Cinch:**
- Trust issues post-breach
- Simpler pricing ($9/repo vs credits calculator)
- BYOC means secrets never leave their machine
- Don't need enterprise features

**Market share:** ~8% of CI market. Recovering from breach.

---

### Buildkite

**What it is:** CI/CD with hosted orchestration, self-hosted agents. The "best of both worlds" pitch.

**Pricing:**
- Free: Up to 3 users
- Pro: $29/user/month (billed annually)
- Enterprise: Custom
- Compute (hosted runners): Usage-based on top

**Strengths:**
- Excellent self-hosted agent experience
- Fast, reliable, well-engineered
- Good plugin ecosystem
- Scales well (handles huge monorepos)
- Loved by developers who've used it

**Weaknesses:**
- Per-seat pricing is brutal for large teams
- $29/user = $2,900/month for 100 engineers
- Enterprise-focused (not for solo devs)
- Still requires their orchestration layer (not fully self-hosted)

**Why people would switch to Cinch:**
- Per-repo pricing vs per-seat
- 10 devs × $29 = $290/month vs 10 repos × $9 = $90/month
- Cinch is fully self-hostable (MIT, no orchestration fee)
- Simpler for smaller teams

**Cinch's relationship to Buildkite:** Buildkite is the "good" enterprise CI. Cinch is "Buildkite for people who can't afford Buildkite."

**Market share:** ~5% of CI market. Growing. Indie darling at enterprise scale.

---

### TeamCity (JetBrains)

**What it is:** JetBrains' CI/CD server. Kotlin DSL, deep IDE integration.

**Pricing:**
- Free: 3 build agents, 100 build configs
- Cloud: $45/month for 3 agents
- Server: $299/year for 3 agents (self-hosted)

**Strengths:**
- JetBrains quality (polished, powerful)
- Kotlin DSL (type-safe config)
- Good for JVM projects
- On-prem option for enterprises

**Weaknesses:**
- JetBrains ecosystem lock-in
- Dated UI
- Complex setup
- Not cloud-native

**Why people would switch to Cinch:**
- Simpler setup
- Not tied to JetBrains
- Modern architecture (WebSocket, containers)

**Market share:** ~3% of CI market. Niche but stable.

---

### Bamboo (Atlassian)

**What it is:** Atlassian's CI/CD server. Integrates with Jira, Bitbucket.

**Pricing:**
- Cloud: Discontinued (migrating to Bitbucket Pipelines)
- Data Center: $1,200/year for 1 agent

**Strengths:**
- Deep Atlassian integration
- On-prem for regulated industries

**Weaknesses:**
- Being sunset (cloud version discontinued)
- Expensive
- Atlassian's reputation
- Nobody would choose this today

**Why people would switch to Cinch:**
- Bamboo is dying
- Anything is better

**Market share:** ~2% of CI market. Dying.

---

## Tier 3: Self-Hosted OSS

### Jenkins

**What it is:** The OG CI server. Java, plugins, Groovy DSLs. Been around since 2011.

**Pricing:** Free (open source, MIT-ish license)

**Strengths:**
- Free forever
- Plugin for literally everything (1,800+ plugins)
- Battle-tested at scale
- You control everything

**Weaknesses:**
- Java (resource hog, slow startup)
- Groovy DSL is a nightmare
- Plugin compatibility hell
- Security vulnerabilities (constant patching)
- Dated UI
- DevOps complexity (you're running a Jenkins cluster)
- "Jenkins admin" is a full-time job at some companies

**Why people would switch to Cinch:**
- Single binary vs Java plugin hell
- Modern architecture
- Less operational burden
- Config is YAML, not Groovy

**Cinch's relationship to Jenkins:** Jenkins is "free but costs you your sanity." Cinch is "free and simple."

**Market share:** ~15% of CI market. Declining but still huge in enterprises.

---

### Woodpecker CI

**What it is:** Community fork of Drone after Harness acquisition. Container-native, simple YAML.

**Pricing:** Free (Apache 2.0 license)

**Strengths:**
- Simple, clean design
- Container-native
- Active community (post-Drone fork energy)
- Truly self-hosted (no phone-home)
- Multi-platform support

**Weaknesses:**
- No hosted option (you run everything)
- Smaller ecosystem than Drone was
- Community-maintained (slower development)
- Less mature than established players

**Why people would switch to Cinch:**
- Hosted control plane option ("I don't want to run the server")
- Similar philosophy but with a SaaS option
- Multi-forge support

**Cinch's relationship to Woodpecker:** Cinch is "Woodpecker with a hosted control plane option." Same philosophy, different deployment model.

**Market share:** <1% of CI market. Growing in self-hosted community.

---

### Drone CI

**What it is:** Container-native CI. Was popular, then Harness bought it and things got weird.

**Pricing:**
- Community: Free (limited features)
- Enterprise: Contact Harness

**Strengths:**
- Clean design
- Docker-native
- Was beloved before acquisition

**Weaknesses:**
- Harness acquisition created licensing confusion
- Community forked to Woodpecker
- Trust damaged
- Enterprise focus post-acquisition

**Why people would switch to Cinch:**
- Trust (Cinch is MIT, solo dev, no acquisition risk... well, different risk)
- Active development
- No license ambiguity

**Market share:** ~2% of CI market. Declining post-acquisition.

---

### Dagger

**What it is:** CI/CD pipelines as code (Go, Python, TypeScript SDKs). Runs anywhere.

**Pricing:** Free (Apache 2.0) + Dagger Cloud (usage-based)

**Strengths:**
- Pipelines in real programming languages (not YAML)
- Portable (runs locally, on any CI)
- Modern architecture (BuildKit-based)
- Good caching story
- VC-backed, well-funded

**Weaknesses:**
- Learning curve (new paradigm)
- Requires code changes (not just a config file)
- Still maturing
- Another layer of abstraction

**Why people would switch to Cinch:**
- Simpler (just a Makefile, not a Go SDK)
- No new paradigm to learn
- Different philosophy (your Makefile is the pipeline, not Dagger pipelines)

**Cinch's relationship to Dagger:** Different philosophies. Dagger says "write pipelines in code." Cinch says "your Makefile is the pipeline."

**Market share:** <1% of CI market. Growing, VC darling.

---

## Tier 4: Niche Players

### Semaphore CI

**What it is:** Fast CI/CD with good caching. Developer-focused.

**Pricing:**
- Free: $10/month credit for open source
- Startup: $20/month + usage
- Scale: Usage-based

**Strengths:**
- Fast builds (good parallelism, caching)
- Clean UI
- Good monorepo support

**Weaknesses:**
- Smaller ecosystem
- Usage-based pricing is unpredictable
- Less known

**Why people would switch to Cinch:**
- Flat pricing vs usage-based
- Self-hosted option

---

### Codefresh

**What it is:** CI/CD focused on Kubernetes and GitOps.

**Pricing:**
- Free: Limited builds
- Pro: $75/month
- Enterprise: Custom

**Strengths:**
- Great for Kubernetes deployments
- GitOps-native (Argo integration)
- Good Docker/Helm support

**Weaknesses:**
- Kubernetes-focused (overkill if you don't use k8s)
- Complex for simple projects
- Enterprise pricing

**Why people would switch to Cinch:**
- Don't need Kubernetes features
- Simpler, cheaper

---

### Depot

**What it is:** Fast Docker builds. Remote BuildKit builders.

**Pricing:**
- Pay-per-use: $0.02/minute
- Teams: $20/user/month

**Strengths:**
- Very fast Docker builds
- Good caching
- Simple integration (drop-in for docker build)

**Weaknesses:**
- Docker builds only (not general CI)
- Niche use case

**Why people would switch to Cinch:**
- Need general CI, not just Docker builds
- Different use case (Depot is complementary, not competitive)

---

### Earthly

**What it is:** Makefile + Dockerfile hybrid. Reproducible builds.

**Pricing:**
- Free (open source)
- Earthly Cloud: Usage-based

**Strengths:**
- Reproducible builds
- Makefile-like syntax
- Good caching
- Runs anywhere

**Weaknesses:**
- New syntax to learn (Earthfile)
- Another abstraction layer
- Smaller community

**Why people would switch to Cinch:**
- Use actual Makefiles, not Earthfiles
- Simpler mental model

---

## The Graveyard

### Travis CI

**What it was:** The original "CI that just works." Free for open source. Beautiful badges. Everyone loved it.

**What happened:**
1. Acquired by Idera (2019)
2. Layoffs, service degradation
3. Security incidents
4. Free tier gutted
5. Users fled to GitHub Actions

**Lessons for Cinch:**
- Don't get acquired by private equity
- Free tier is marketing, treat it that way
- Trust is easy to lose, hard to regain
- Badges matter (people loved Travis badges)

---

### Codeship

**What it was:** Simple CI/CD, acquired by CloudBees.

**What happened:** Absorbed into CloudBees, effectively dead.

**Lessons for Cinch:**
- Big company acquisitions kill indie products
- Stay small, stay independent

---

## Where Cinch Fits

### The Positioning

```
                    Complexity
                        ↑
                        │
       Jenkins ●        │         ● GitHub Actions
                        │         ● GitLab CI
                        │
       Drone/Wood- ●    │    ● CircleCI
       pecker           │    ● Buildkite
                        │
                        │         ● Semaphore
                 Cinch ●│
                        │
                        └──────────────────────→ Hosted
                    Self-hosted
```

### Cinch's Unique Position

| Attribute | Cinch | Nearest Competitor |
|-----------|-------|-------------------|
| Multi-forge | GitHub, GitLab, Forgejo, Gitea, Bitbucket | Drone/Woodpecker (partial) |
| Pricing model | Per-repo, public free | Buildkite (per-seat), GHA (per-minute) |
| Self-hosted option | Full MIT, no phone-home | Woodpecker (no hosted), Buildkite (orchestration fee) |
| Config complexity | One command + services | Everyone else (YAML DSLs) |
| Compute model | BYOC (your machine) | Everyone else (their runners or your runners with fees) |

### Target Users

1. **Forgejo/Gitea users** - Underserved, hate GitHub, will love multi-forge
2. **Simplicity zealots** - Hate YAML complexity, want "make ci" and done
3. **Cost-conscious teams** - Per-repo cheaper than per-seat for many shapes
4. **Privacy-focused** - Code never leaves their network
5. **Travis nostalgics** - Want the old magic back, with badges

### NOT Target Users

1. **Enterprises needing SOC2, SSO, audit logs** - Use Buildkite
2. **Kubernetes-native shops** - Use Codefresh or Argo
3. **Complex pipeline needs (DAGs, matrices)** - Use GitHub Actions
4. **"We already use GitHub for everything"** - Probably stay on GHA

---

## Competitive Threats

### Threat 1: GitHub drops the self-hosted runner fee

**Probability:** Low (they announced it, they'll do it)
**Impact:** Reduces urgency for Cinch's pitch
**Mitigation:** Multi-forge and simplicity are still differentiators

### Threat 2: Woodpecker adds hosted option

**Probability:** Medium (community might build this)
**Impact:** Direct competition
**Mitigation:** Move fast, build brand, focus on simplicity

### Threat 3: Buildkite drops prices

**Probability:** Low (they're VC-funded, need revenue)
**Impact:** Medium
**Mitigation:** Per-repo vs per-seat is still different model

### Threat 4: Someone forks Cinch

**Probability:** High (it's MIT)
**Impact:** Low (execution matters more than code)
**Mitigation:** Build brand, community, trust

---

## Key Takeaways

1. **The opening is real** - GitHub's self-hosted runner fee + complexity fatigue + Travis nostalgia
2. **Multi-forge is underserved** - Forgejo/Gitea community has no good options
3. **Per-repo pricing is differentiated** - Nobody else does this
4. **Simplicity is the wedge** - "Your Makefile is the pipeline" is a strong message
5. **BYOC is unique** - Cost structure is fundamentally different
6. **Trust matters** - CircleCI breach, Travis collapse, Drone acquisition - there's appetite for trustworthy indie
7. **Badges matter** - Bring back the Travis badge culture

---

## Cinch Taglines to Test

- "CI that's a cinch"
- "Your Makefile is the pipeline"
- "Travis CI, resurrected"
- "Push code, get green checks, keep your money"
- "The last CI tool you'll ever need"
- "CI for people who hate CI"
- "Free for open source. Forever."
