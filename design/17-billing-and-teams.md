# Design 17: Billing & Team Onboarding

## Status: MVP

## Problem

A team lead with a credit card wants to onboard their team to Cinch. Today:
- Individual users sign up
- No way to pay for a "team"
- No billing entity that spans multiple users

## Core Insights

1. **Pro status belongs to the USER, not the repo.** If your employer pays for your seat, you get Pro everywhere—work AND home.

2. **High Water Mark (HWM), not metered.** You set your seat limit. If you exceed it, builds block until you add seats. No surprise bills.

3. **Quota follows repo owner.** Org repos use org quota. Personal repos use personal quota. Pro status just unlocks the door.

## Pricing

| Tier | Price | Storage | Log Retention |
|------|-------|---------|---------------|
| Free | $0 | 100 MB | 7 days |
| Personal Pro | $5/mo or $48/yr | 10 GB | 90 days |
| Team Pro | $5/seat/mo or $48/seat/yr | 10 GB × seats | 90 days |

**Yearly = 20% off, requires commitment.** You set seat count, pay upfront. Stripe handles proration if you add seats mid-year.

**Monthly = flexibility.** Change seats anytime. No commitment.

## How It Works

### Team Pro Flow

```
1. Team lead goes to cinch.sh/billing
2. Selects org: github.com/acme
3. Sets seat limit: 5
4. Pays: $25/mo or $240/yr
5. Done.

When Alice (acme member) pushes to acme/backend:
├── Is Alice already a seat this period?
│   └── Yes → build runs
│   └── No → seats_used < seat_limit?
│       └── Yes → Alice becomes seat, build runs
│       └── No → BLOCKED "Seat limit reached. Add seats."

Alice now has Pro status everywhere:
└── alice-c/personal-project (private) → works!
```

### Storage Quota

| Repo | Quota Source |
|------|--------------|
| `acme/backend` | Org pool: `seat_limit × 10GB` |
| `alice-c/crypto` | Alice's personal: `10GB` |

Pro status (however acquired) unlocks private repos. Quota is based on **repo owner**.

### Personal Pro Flow

```
1. Solo dev goes to cinch.sh/billing
2. Subscribes: $5/mo or $48/yr
3. Done.

All their private repos work. 10GB storage.
```

## Stripe Integration

**No metered billing.** Just quantity-based subscriptions.

### Products & Prices

```
Product: Cinch Pro
├── price_personal_monthly  → $5/mo, quantity=1
├── price_personal_yearly   → $48/yr, quantity=1
├── price_team_monthly      → $5/seat/mo, quantity=N
└── price_team_yearly       → $48/seat/yr, quantity=N
```

### Checkout Session

```go
// Team Pro checkout
session, _ := stripe.CheckoutSessions.Create(&stripe.CheckoutSessionParams{
    Mode: stripe.String("subscription"),
    LineItems: []*stripe.CheckoutSessionLineItemParams{{
        Price:    stripe.String("price_team_monthly"),
        Quantity: stripe.Int64(seatLimit),  // HWM
    }},
    SuccessURL: stripe.String("https://cinch.sh/billing?success=1"),
    CancelURL:  stripe.String("https://cinch.sh/billing"),
    Metadata: map[string]string{
        "forge_type": "github",
        "forge_org":  "acme",
        "user_id":    user.ID,
    },
})
```

### Updating Seats

```go
// User bumps from 5 to 6 seats
stripe.Subscriptions.Update(subID, &stripe.SubscriptionParams{
    Items: []*stripe.SubscriptionItemsParams{{
        ID:       stripe.String(itemID),
        Quantity: stripe.Int64(6),
    }},
    ProrationBehavior: stripe.String("create_prorations"),
})
```

Stripe handles proration automatically.

### Webhooks

```go
switch event.Type {
case "checkout.session.completed":
    // Create org_billing or update user.stripe_subscription_id

case "invoice.paid":
    // Mark subscription active, reset seats_used for new period

case "customer.subscription.updated":
    // Sync seat_limit from Stripe quantity

case "customer.subscription.deleted":
    // Mark inactive, users lose Pro status
}
```

## Data Model

```sql
-- Org billing (Team Pro)
CREATE TABLE org_billing (
    id TEXT PRIMARY KEY,
    forge_type TEXT NOT NULL,           -- 'github', 'gitlab', 'forgejo'
    forge_org TEXT NOT NULL,            -- 'acme'
    owner_user_id TEXT NOT NULL,        -- who manages billing
    stripe_customer_id TEXT,
    stripe_subscription_id TEXT,
    stripe_subscription_item_id TEXT,   -- for quantity updates
    seat_limit INT NOT NULL DEFAULT 5,  -- HWM
    seats_used INT NOT NULL DEFAULT 0,  -- current period
    storage_used_bytes BIGINT DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'active',  -- 'active', 'past_due', 'canceled'
    period_start TIMESTAMP,             -- for seat reset
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(forge_type, forge_org)
);

-- Track who's consumed a seat this billing period
CREATE TABLE org_seats (
    org_billing_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    forge_username TEXT NOT NULL,       -- for admin display
    consumed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (org_billing_id, user_id),
    FOREIGN KEY (org_billing_id) REFERENCES org_billing(id)
);

-- Personal Pro (add to existing users table)
ALTER TABLE users ADD COLUMN stripe_customer_id TEXT;
ALTER TABLE users ADD COLUMN stripe_subscription_id TEXT;
ALTER TABLE users ADD COLUMN storage_used_bytes BIGINT DEFAULT 0;
-- tier already exists: 'free' or 'pro'
```

## Core Logic

### Check Pro Status

```go
func (u *User) HasPro(ctx context.Context, store Storage) bool {
    // Personal Pro subscription?
    if u.Tier == UserTierPro {
        return true
    }

    // Seat in any active org?
    orgs, _ := store.ListOrgSeatsForUser(ctx, u.ID)
    for _, seat := range orgs {
        if seat.OrgStatus == "active" {
            return true
        }
    }

    return false
}
```

### Job Dispatch Gate

```go
func canRunJob(ctx context.Context, job *Job, triggeredBy *User, repo *Repo) error {
    // Public repos always allowed
    if !repo.Private {
        return nil
    }

    // Private repo - determine quota source
    billing, _ := store.GetOrgBilling(ctx, repo.ForgeType, repo.Owner)

    if billing != nil {
        // Org repo - must have Team Pro
        if billing.Status != "active" {
            return errors.New("org subscription inactive")
        }

        // Check/consume seat
        if !store.IsOrgSeat(ctx, billing.ID, triggeredBy.ID) {
            if billing.SeatsUsed >= billing.SeatLimit {
                return fmt.Errorf("seat limit reached (%d/%d). Add seats at cinch.sh/billing",
                    billing.SeatsUsed, billing.SeatLimit)
            }
            store.AddOrgSeat(ctx, billing.ID, triggeredBy.ID, triggeredBy.Name)
        }

        // Check org storage quota
        quota := int64(billing.SeatLimit) * 10 * 1024 * 1024 * 1024  // 10GB per seat
        if billing.StorageUsedBytes >= quota {
            return errors.New("org storage quota exceeded")
        }

        return nil
    }

    // Personal repo - user must have Pro
    if !triggeredBy.HasPro(ctx, store) {
        return errors.New("private repos require Pro. Upgrade at cinch.sh/billing")
    }

    // Check personal storage quota
    if triggeredBy.StorageUsedBytes >= 10*1024*1024*1024 {
        return errors.New("personal storage quota exceeded")
    }

    return nil
}
```

### Storage Tracking

```go
func trackJobStorage(ctx context.Context, job *Job, logSizeBytes int64) {
    repo, _ := store.GetRepo(ctx, job.RepoID)

    billing, _ := store.GetOrgBilling(ctx, repo.ForgeType, repo.Owner)
    if billing != nil {
        // Org repo - update org storage
        store.UpdateOrgStorageUsed(ctx, billing.ID, logSizeBytes)
    } else {
        // Personal repo - update user storage
        store.UpdateUserStorageUsed(ctx, job.TriggeredByUserID, logSizeBytes)
    }
}
```

## Edge Cases

### Alice Leaves Acme

1. Alice is removed from `acme` org on GitHub
2. Next billing period, she's not a seat
3. She loses Pro status (unless she has Personal Pro)
4. Her personal `storage_used_bytes` stays (data not deleted)
5. New private repo builds → BLOCKED
6. If she subscribes to Personal Pro → she's back

### User in Multiple Orgs

Alice is in `acme` (Team Pro) and `widgets` (Team Pro).
- She has Pro status
- She consumes a seat in BOTH if she triggers builds in both
- Both orgs pay for her
- Her personal repos use her personal 10GB quota

### Seat Reset

At start of each billing period:
1. `seats_used` resets to 0
2. `org_seats` rows deleted (or marked historical)
3. First build by each user re-consumes a seat

## Web UI

### Billing Page (Team Admin)

```
Billing

Team Pro: github.com/acme
├── Status: Active
├── Seats: 4/5 used
├── Storage: 12.3 GB / 50 GB
├── Next billing: Feb 15, 2026
└── [Add Seats] [Manage Payment] [View Usage]

Seat Usage This Period:
├── alice (47 jobs)
├── bob (23 jobs)
├── charlie (12 jobs)
└── dana (8 jobs)
```

### Billing Page (No Pro)

```
Billing

Status: Free
├── Public repos only
├── 7-day log retention

[Upgrade to Personal Pro - $5/mo or $48/yr]

Or set up Team Pro for an organization:
├── github.com/acme [Set Up]
└── github.com/widgets [Set Up]
```

## Implementation Order

1. **Schema** - org_billing, org_seats, user columns
2. **Stripe setup** - Products, prices, webhook endpoint
3. **Checkout flow** - /api/billing/checkout
4. **Webhook handler** - invoice.paid, subscription.updated, etc.
5. **Pro status check** - HasPro() logic
6. **Job gate** - Block private repos without Pro
7. **Seat tracking** - Consume seats on job trigger
8. **Storage tracking** - Update on job complete
9. **Billing UI** - Status, upgrade, seat management
10. **Seat reset** - Cron job at period start

## Open Questions (Deferred)

1. **Grace period when subscription lapses?** - Probably 7 days warning, then block.
2. **Can yearly users decrease seats?** - Yes, but no refund. Credit toward renewal.
3. **Self-hosted billing?** - MIT license, no billing. Honor system for commercial use.
