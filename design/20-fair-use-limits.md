# Design Doc 20: Fair Use Limits

**Status:** MVP
**Author:** Claude
**Date:** 2026-01-30

## Problem

Cinch uses Cloudflare R2 for log storage. Without limits, users could abuse it as free cloud storage.

## Relationship to Billing

See `design/17-billing-and-teams.md` for the full billing model. This doc covers enforcement.

**Key insight:** Quota follows repo owner, not the user who triggered the build.

| Repo Type | Quota Source |
|-----------|--------------|
| Org repo (`acme/backend`) | Org pool: `seat_limit × 10GB` |
| Personal repo (`alice/crypto`) | User's personal: `10GB` |

## Limits by Tier

| Resource | Free | Pro | Self-Hosted |
|----------|------|-----|-------------|
| Private repos | ❌ | ✅ | ✅ |
| Storage quota | 100 MB | 10 GB (or seats×10GB) | Unlimited |
| Log retention | 7 days | 90 days | Configurable |
| Max workers | 10 | 1000 | Unlimited |
| Concurrent jobs | 5 | 100 | Unlimited |
| Job timeout max | 1 hour | 6 hours | Unlimited |

**Free tier can only use public repos**, so storage abuse is limited. 100MB covers ~2000 builds at 50KB average.

## Infrastructure (Done)

- [x] **Log compression** - Gzip on finalize (~10-30x savings)
- [x] **User tier model** - `tier` field: free/pro
- [x] **Storage tracking fields** - `user.storage_used_bytes`, `job.log_size_bytes`
- [x] **Storage interface methods** - `UpdateJobLogSize`, `UpdateUserStorageUsed`
- [x] **Size tracking on finalize** - `Finalize()` returns compressed size

## Enforcement (With Billing)

These require the billing tables from design/17:

- [ ] **Org billing schema** - `org_billing`, `org_seats` tables
- [ ] **Pro status check** - `HasPro()` checks personal + team
- [ ] **Private repo gate** - Block private repos without Pro
- [ ] **Seat tracking** - Consume seats on job trigger
- [ ] **Storage quota check** - Block if org/user over quota
- [ ] **Storage tracking on complete** - Update org or user storage
- [ ] **Log retention cleanup** - Background job deletes old logs
- [ ] **Worker limit check** - Reject registration when at limit

## Self-Hosted: No Artificial Limits

Self-hosted deployments have **no quotas**. Only system health guards:

- WebSocket message size: 1MB
- Log chunk size: 64KB
- Stale worker timeout: 90s
- Job queue timeout: 30min

These prevent pathological behavior, not fair use enforcement.

## Error Messages

```
❌ Seat limit reached (5/5). Add seats at cinch.sh/billing

❌ Storage quota exceeded (48.2 GB / 50 GB)
   Your team's quota is 10GB per seat. Add seats or delete old jobs.

❌ Private repos require Pro. Upgrade at cinch.sh/billing

❌ Worker limit reached (10/10)
   Free accounts can register 10 workers. Upgrade to Pro for 1000.
```

## Implementation Order

1. Billing schema + Stripe integration (design/17)
2. Pro status check in job dispatch
3. Storage quota check in job dispatch
4. Storage tracking on job complete
5. Worker limit check on registration
6. Log retention cleanup (background job)
7. Usage API endpoint (`GET /api/account/usage`)
