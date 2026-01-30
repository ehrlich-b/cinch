# Design 17: Billing & Team Onboarding

## Status: Draft

## Problem

A team lead with a credit card wants to onboard their team to Cinch. Today:
- Individual users sign up
- No way to pay for a "team"
- No billing entity that spans multiple users

The worker visibility design (16) solves who can see/use workers. This design solves who pays.

## Core Insight

**Pro status belongs to the USER, not the repo or job.**

The only thing Pro unlocks is: **private repos**. That's it. Workers are BYOW (bring your own worker).

If you have Pro status, you can use private repos - both at work AND at home. Your employer doesn't see your personal repos, they just pay for your seat.

## Pricing Tiers

### Free ($0 forever)

For open source and self-hosters.

- Public repos only
- Self-hosted control plane option
- BYOW (bring your own workers)
- 7-day log retention

### Personal Pro ($4/mo yearly, $5/mo monthly)

For indie devs with private projects.

```
You pay $4-5/month
  ↓
Your Cinch account has Pro status
  ↓
You can use private repos (yours, or any you have access to)
```

- Private repos
- 30-day log retention

### Team Pro ($10/seat yearly, $12/seat monthly)

For organizations. Gives Pro status to all org members.

```
Org admin sets up Team Pro for github.com/acme
  ↓
Cinch checks: who is a member of `acme` org on GitHub?
  ↓
All members get Pro status on their Cinch accounts
  ↓
Org pays per seat for each member who uses Cinch
```

**Yearly commitment:**
- Commit to N seats at $10/seat/mo ($120/seat/year)
- Pay upfront or monthly, locked rate
- Overage (more than N seats): $12/seat/mo

**Monthly (no commitment):**
- Pure metered at $12/seat/mo
- Can go to $0 if no one uses it

**Features:**
- Private repos for all org members
- 90-day log retention
- Priority support

### Managed Runners (Future)

For teams who don't want BYOW. Separate pricing TBD.

- Cinch-hosted runners
- Per runner-hour or per runner/month
- This is where the real margin is

### Competitive Comparison

| Service | Per Seat | Notes |
|---------|----------|-------|
| Buildkite | $15/mo | + compute costs |
| CircleCI | $15/mo | + compute costs |
| **Cinch Team (yearly)** | $10/mo | BYOW, no compute costs |
| **Cinch Team (monthly)** | $12/mo | BYOW, no compute costs |

**Key insight:** Team Pro is just "Personal Pro for everyone in the org, billed to one card."

## How Pro Status Works

```go
func hasPro(user *User) bool {
    // Has personal Pro subscription?
    if hasSubscription(user.ID, "personal_pro") {
        return true
    }

    // Member of an org with Team Pro?
    for _, org := range getUserForgeOrgs(user) {
        if hasSubscription(org.ID, "team_pro") {
            return true
        }
    }

    return false
}
```

When a job is created for a private repo:
```go
func canRunPrivateRepo(user *User) bool {
    return hasPro(user)
}
```

## What a "Seat" Is

A seat is a unique org member who triggers at least one job in a billing period.

- Alice pushes to `acme/backend` → Alice is a seat
- Alice pushes again → still 1 seat
- Bob pushes to `acme/frontend` → now 2 seats
- Alice pushes to `alice/personal-project` → still 2 seats (personal repos don't count toward org)

Counting happens at end of billing period:
```go
func countSeats(org *Org, period BillingPeriod) int {
    members := getOrgMembers(org)  // Query GitHub/GitLab API

    activeCount := 0
    for _, member := range members {
        if hasJobsInPeriod(member, period) {
            activeCount++
        }
    }
    return activeCount
}
```

## User Stories

### Solo Developer

```
1. Visit cinch.sh, login with GitHub
2. Try to add private repo → "Upgrade to Pro for private repos"
3. Click upgrade → choose $4/mo yearly or $5/mo monthly
4. Done. Private repos work.
```

### Team Lead Onboarding

```
1. Visit cinch.sh, login with GitHub
2. Go to Billing → "Set up Team Pro"
3. Select org: github.com/acme
4. Add payment method
5. Done. All acme org members now have Pro.
```

### Team Member Experience

```
1. Run `cinch login` → authenticates with GitHub
2. Run `cinch worker` → worker starts
3. Push to acme/backend (private) → job runs
4. Push to personal/side-project (private) → job runs (you have Pro!)

Bob never sees billing UI. Acme pays for his seat.
```

### Multi-Forge Individual

```
1. Login, link GitHub + GitLab + Codeberg
2. Subscribe to Personal Pro ($4/mo yearly or $5/mo)
3. All your private repos on all forges work
4. One Cinch account, one bill, all forges
```

## What Org Admins See vs Don't See

**Org admins CAN see:**
- Seat count (12 users this month)
- List of usernames (alice, bob, charlie)
- Total jobs on org repos
- Monthly cost

**Org admins CANNOT see:**
- Your personal repos
- Your personal job history
- Your home worker
- Anything outside org repos

The org just pays for your Pro status. They don't manage you.

## Stripe Integration

### Products & Prices

```
Product: Cinch Pro
├── Price: personal_pro_monthly
│   └── $5/month, fixed
├── Price: personal_pro_yearly
│   └── $48/year ($4/mo effective), fixed
├── Price: team_pro_seat_yearly
│   └── $120/seat/year ($10/mo), licensed (committed quantity)
├── Price: team_pro_seat_monthly
│   └── $12/seat/month, metered (no commitment)
└── Price: team_pro_seat_overage
    └── $12/seat/month, metered (for yearly plans exceeding commitment)
```

### Personal Pro Subscription

```go
// Monthly
subscription, _ := stripe.Subscriptions.Create(&stripe.SubscriptionParams{
    Customer: user.StripeCustomerID,
    Items: []*stripe.SubscriptionItemsParams{{
        Price: "price_personal_pro_monthly",  // $5/month
    }},
})

// Yearly
subscription, _ := stripe.Subscriptions.Create(&stripe.SubscriptionParams{
    Customer: user.StripeCustomerID,
    Items: []*stripe.SubscriptionItemsParams{{
        Price: "price_personal_pro_yearly",  // $48/year
    }},
})
```

### Team Pro Subscription (Yearly Commitment)

```go
// Org commits to N seats for the year
subscription, _ := stripe.Subscriptions.Create(&stripe.SubscriptionParams{
    Customer: org.StripeCustomerID,
    Items: []*stripe.SubscriptionItemsParams{
        {
            Price:    "price_team_pro_seat_yearly",  // $120/seat/year
            Quantity: committedSeats,                 // e.g., 10
        },
        {
            Price: "price_team_pro_seat_overage",    // $12/seat/mo for extras
        },
    },
})
```

### Team Pro Subscription (Monthly, No Commitment)

```go
// Pure metered, can go to $0
subscription, _ := stripe.Subscriptions.Create(&stripe.SubscriptionParams{
    Customer: org.StripeCustomerID,
    Items: []*stripe.SubscriptionItemsParams{{
        Price: "price_team_pro_seat_monthly",  // $12/seat/month metered
    }},
})
```

### Reporting Seat Usage

```go
func reportTeamUsage(org *Org) {
    period := getCurrentBillingPeriod(org)
    actualSeats := countSeats(org, period)

    if org.Plan == "team_yearly" {
        // Only report overage beyond committed seats
        overage := max(0, actualSeats - org.CommittedSeats)
        if overage > 0 {
            stripe.UsageRecords.Create(&stripe.UsageRecordParams{
                SubscriptionItem: org.OverageSubscriptionItemID,
                Quantity:         overage,
                Timestamp:        period.End,
                Action:           "set",
            })
        }
    } else {
        // Monthly plan: report all seats
        stripe.UsageRecords.Create(&stripe.UsageRecordParams{
            SubscriptionItem: org.StripeSubscriptionItemID,
            Quantity:         actualSeats,
            Timestamp:        period.End,
            Action:           "set",
        })
    }
}
```

## Data Model

### Tables

```sql
-- Personal Pro subscriptions
CREATE TABLE subscriptions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    plan TEXT NOT NULL,  -- 'personal_pro'
    stripe_subscription_id TEXT,
    status TEXT NOT NULL,  -- 'active', 'canceled', 'past_due'
    current_period_start TIMESTAMP,
    current_period_end TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Team Pro billing accounts (one per org)
CREATE TABLE org_billing (
    id TEXT PRIMARY KEY,
    forge_type TEXT NOT NULL,  -- 'github', 'gitlab', 'forgejo'
    forge_org_id TEXT NOT NULL,
    forge_org_name TEXT NOT NULL,
    owner_user_id TEXT NOT NULL,  -- Cinch user who manages billing
    stripe_customer_id TEXT,
    stripe_subscription_id TEXT,
    status TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(forge_type, forge_org_id)
);

-- Track seats per billing period
CREATE TABLE org_billing_seats (
    org_billing_id TEXT NOT NULL,
    period_start DATE NOT NULL,
    user_id TEXT NOT NULL,
    forge_username TEXT NOT NULL,
    first_job_at TIMESTAMP,
    last_job_at TIMESTAMP,
    job_count INTEGER DEFAULT 0,
    PRIMARY KEY (org_billing_id, period_start, user_id)
);
```

## API

### GET /api/billing

```json
{
  "pro": true,
  "pro_source": "team",  // or "personal" or null
  "personal_subscription": null,
  "team_memberships": [
    {
      "org": "acme",
      "forge": "github",
      "has_billing": true,
      "you_are_admin": false
    }
  ],
  "managed_orgs": [
    {
      "id": "ob_123",
      "org": "acme",
      "forge": "github",
      "plan": "team_yearly",
      "committed_seats": 10,
      "seats_this_month": 12,
      "estimated_charge": "$124.00"
    }
  ]
}
```

### POST /api/billing/personal

Subscribe to Personal Pro:

```json
{}
```

Returns Stripe Checkout session URL.

### POST /api/billing/team

Set up Team Pro for an org:

```json
{
  "forge_type": "github",
  "forge_org": "acme"
}
```

Requires: user is admin of org on forge.
Returns Stripe Checkout session URL.

### GET /api/billing/team/{id}/usage

```json
{
  "org": "acme",
  "period": "2024-01",
  "plan": "team_yearly",
  "committed_seats": 10,
  "committed_charge": "$100.00",
  "seats": [
    {"username": "alice", "jobs": 145},
    {"username": "bob", "jobs": 89},
    {"username": "charlie", "jobs": 67}
  ],
  "total_seats": 12,
  "total_jobs": 847,
  "overage_seats": 2,
  "overage_charge": "$24.00",
  "total_charge": "$124.00"
}
```

## Edge Cases

### User Has Both Personal + Team Pro

Alice pays for Personal Pro AND is in an org with Team Pro.

```go
func hasPro(user) bool {
    // Check both - either grants Pro
    return hasPersonalPro(user) || hasTeamPro(user)
}
```

Alice's personal subscription is wasted money. Show a warning:
"You have Personal Pro but you're also covered by Acme's Team Pro. Consider canceling your personal subscription."

### User in Multiple Orgs with Team Pro

Alice is in `acme` (Team Pro) and `widgets` (Team Pro).

- She has Pro status (either org grants it)
- She's counted as a seat in BOTH orgs if she triggers jobs in both
- Both orgs pay for her

This is fine. She's using both orgs' CI.

### Org Member Who Never Uses Cinch

Bob is in `acme` org but never pushes code or triggers jobs.

- Bob is NOT counted as a seat
- Acme doesn't pay for Bob
- If Bob ever uses Cinch, he automatically has Pro (and becomes a seat)

### Personal Repos of Team Members

Alice is in `acme` (Team Pro). She also has `alice/personal-project` (private).

- Alice has Pro status (from Acme)
- She can use her personal private repo
- Personal repo jobs don't count toward Acme's seat usage
- Acme only pays for jobs on `acme/*` repos

Wait, that's tricky. Let me reconsider...

Actually, simpler: **Seats are org members who use Cinch at all.**

- Alice is in `acme` and uses Cinch → she's a seat, Acme pays
- Doesn't matter if she uses personal repos or org repos
- Acme is paying for her Pro status, period

This is how GitHub seats work - you pay per user, not per repo usage.

### Org Removes Team Pro

Acme cancels their Team Pro subscription.

- All Acme members lose Pro status (unless they have personal Pro)
- Private repo jobs start failing: "Private repos require Pro"
- Grace period? Maybe 7 days before hard cutoff

### Admin Leaves Org

Alice (billing admin) leaves Acme.

- Another org admin can claim billing management
- If no admins claim it, billing continues (Stripe has the card)
- Eventually: send warning emails, then suspend

## Web UI

### Billing Page (Has Pro via Team)

```
Account

Pro Status: ✓ Active
  via Acme Corp Team Pro

──────────────────────────────

Team Billing (you manage)

┌────────────────────────────────────┐
│ Acme Corp          Team Pro Yearly │
│ github.com/acme                    │
│                                    │
│ Committed: 10 seats @ $10/mo       │
│ Active: 12 seats (+2 overage)      │
│ Est. charge: $124.00               │
│                                    │
│ [View Usage] [Manage Payment]      │
└────────────────────────────────────┘
```

### Billing Page (No Pro)

```
Account

Pro Status: ✗ Free
  Private repos unavailable
  7-day log retention

──────────────────────────────

[Upgrade to Personal Pro - $4/month yearly, $5/month]

or

Set up Team Pro for an organization:
  [github.com/acme] [Set Up - $10/seat yearly, $12/seat monthly]
```

### Billing Page (Personal Pro)

```
Account

Pro Status: ✓ Active
  Personal Pro - $4/month (yearly)

[Manage Subscription] [Cancel]
```

## Implementation Order

1. **Database schema** - subscriptions, org_billing, org_billing_seats
2. **hasPro() logic** - check personal + team subscriptions
3. **Private repo gate** - require Pro for private repos
4. **Stripe integration** - personal monthly/yearly, team monthly/yearly
5. **Seat counting** - query forge API for org members
6. **Usage reporting** - report overage seats to Stripe monthly
7. **Commitment management** - UI to set/change committed seats
8. **Billing UI** - status, upgrade, usage dashboard
9. **Warnings** - redundant subscriptions, payment failures, approaching commitment

## Open Questions

### 1. Free Trial?

- 14-day Pro trial for new users?
- Or just let them use public repos free forever?

Proposal: No trial. Public repos are the trial.

### 2. Minimum Commitment for Yearly Teams?

Require minimum commitment for yearly discount?
- e.g., "Yearly requires at least 5 seats"

Proposal: No minimum. 1 seat = $10/mo yearly. Small teams shouldn't be punished.

### 3. Enterprise?

Custom pricing for large orgs (100+ seats)?

Defer to later. Start with self-serve.

### 4. Refunds?

What if someone pays for Personal Pro, then their org gets Team Pro?

Proposal: Prorate and refund remaining personal subscription automatically.

### 5. Yearly Commitment Changes?

Can you increase committed seats mid-year? Decrease?

Proposal:
- Increase: yes, prorated at yearly rate
- Decrease: no, wait until renewal (or pay early termination)

### 6. Log Retention by Tier?

Current proposal:
- Free: 7 days
- Personal Pro: 30 days
- Team Pro: 90 days

Is this the right differentiation? Or should everyone get 30 days?
