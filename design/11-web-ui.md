# Cinch Web UI Design

**Status:** Draft
**Last Updated:** 2025-01-24

---

## Philosophy

Cinch is "CI that's a cinch" - the web UI should embody this. Every interaction should feel effortless, every page should have a clear purpose, and the user should never feel lost or confused.

**Design Principles:**
1. **Obvious** - No guessing what things do
2. **Fast** - Instant feedback, no unnecessary loading
3. **Focused** - Show what matters, hide what doesn't
4. **Cohesive** - Consistent patterns throughout

---

## Current Issues

### Critical
- [ ] **No URL routing** - Back button doesn't work, can't bookmark pages, can't share links to jobs
- [ ] **Badges page broken** - Shows "CI CI" (logo + label both say CI)
- [ ] **Settings page empty** - Just says "coming soon"
- [ ] **No onboarding flow** - New users land on empty jobs list with no guidance

### UX Problems
- [ ] **Landing hero unclear** - Terminal output showing worker isn't compelling; users care about config simplicity
- [ ] **Empty states unhelpful** - "No jobs yet" doesn't tell you what to do
- [ ] **No error handling** - API failures show nothing
- [ ] **Jobs table is basic** - No filtering, no relative timestamps, no repo grouping
- [ ] **Workers page sparse** - No way to add workers, no connection status details
- [ ] **Auth state confusing** - Landing shows for both unauth AND when clicking "home"

### Visual Issues
- [ ] **Inconsistent spacing** - Landing page vs dashboard feel different
- [ ] **Header styles differ** - Landing header vs dashboard header
- [ ] **No visual hierarchy** - Everything same weight in dashboard

---

## Information Architecture

```
/                   Landing page (marketing)
/login              GitHub OAuth redirect
/dashboard          Redirect to /jobs (default authenticated view)
/jobs               Job list (default view when logged in)
/jobs/:id           Job detail with logs
/workers            Worker list + setup instructions
/settings           Account settings, tokens, danger zone
/badges             Badge generator
/docs               Link to external docs (GitHub wiki or separate site)
```

---

## Page Designs

### 1. Landing Page (`/`)

**Purpose:** Convert visitors to users

**Hero Section:**
Show BOTH the cinch config AND the Makefile side-by-side:

```
┌───────────────────────────────────────────────────────────────────────┐
│                                                                       │
│                      CI that's a Cinch                                │
│                                                                       │
│      The exact `make build` you run locally. That's your CI.         │
│                                                                       │
│      ┌──────────────────────┐     ┌────────────────────────────┐     │
│      │  .cinch.yaml         │     │  Makefile                  │     │
│      │  ─────────────       │     │  ────────                  │     │
│      │  build: make build   │     │  build:                    │     │
│      │  release: make release     │      go build -o bin/app   │     │
│      │                      │     │                            │     │
│      └──────────────────────┘     │  release:                  │     │
│                                   │      cinch release dist/*  │     │
│                                   └────────────────────────────┘     │
│                                                                       │
│      Your Makefile already works. We just run it on push.            │
│                                                                       │
│      [Get Started]                                                    │
│                                                                       │
└───────────────────────────────────────────────────────────────────────┘
```

**Key insight from competitor analysis:**
- Dagger/Earthly nail "local = CI" but require learning new syntax
- Cinch: "local = CI" using your EXISTING Makefile - zero new syntax

**Why this works:**
- Shows the REAL config format (`build:` not `run:`)
- Emphasizes "same command locally and in CI"
- Shows the Makefile to prove you don't learn anything new
- Implicit contrast: 3-line config vs 50-line GitHub Actions YAML

**Messaging:**
- NOT: "We simplified CI" (implies we invented something)
- YES: "Your Makefile already works. We just run it on push."

**Sections:**
1. Hero (config + Makefile side-by-side)
2. How it works (3 steps, visual)
3. Features (multi-forge, your hardware, simple)
4. Pricing (free/pro/enterprise)
5. Footer

### 2. Jobs Page (`/jobs`)

**Purpose:** See build status at a glance, drill into failures

**Layout:**
```
┌─────────────────────────────────────────────────────────────┐
│ Cinch          [Jobs] Workers Settings Badges    user ▼    │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Recent Builds                              [Filter ▼]     │
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ ✓  ehrlich-b/cinch          main    abc1234   12s   │   │
│  │    2 minutes ago                                     │   │
│  ├─────────────────────────────────────────────────────┤   │
│  │ ✓  ehrlich-b/cinch          main    def5678   45s   │   │
│  │    15 minutes ago                                    │   │
│  ├─────────────────────────────────────────────────────┤   │
│  │ ✗  ehrlich-b/other-repo     feat    ghi9012   1m    │   │
│  │    1 hour ago                                        │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

**Empty State (new user):**
```
┌─────────────────────────────────────────────────────────────┐
│                                                             │
│                    No builds yet                            │
│                                                             │
│     Let's get your first build running:                    │
│                                                             │
│     1. Install a worker                                     │
│        curl -sSL https://cinch.sh/install.sh | sh          │
│        cinch worker --token=YOUR_TOKEN                      │
│        [Copy Token]                                         │
│                                                             │
│     2. Add the GitHub App                                   │
│        [Install GitHub App]                                 │
│                                                             │
│     3. Add .cinch.yaml to your repo                        │
│        run: make ci                                         │
│                                                             │
│     4. Push!                                                │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

**Features:**
- Relative timestamps ("2 minutes ago")
- Click row to see logs
- Status icon with color (green checkmark, red X, yellow spinner)
- Filter by repo, status, branch
- Group by repo optionally
- Pagination or infinite scroll

### 3. Job Detail (`/jobs/:id`)

**Purpose:** Debug build failures, see what happened

**Layout:**
```
┌─────────────────────────────────────────────────────────────┐
│ ← Back to Jobs                                              │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ✓ ehrlich-b/cinch @ main                                  │
│  Commit abc1234 · 12 seconds · 2 minutes ago               │
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ $ make ci                                            │   │
│  │ go build -o bin/cinch ./cmd/cinch                   │   │
│  │ go test ./...                                        │   │
│  │ ok   cinch/internal/config  0.012s                  │   │
│  │ ok   cinch/internal/worker  0.034s                  │   │
│  │                                                      │   │
│  │ Build succeeded                                      │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│  [Re-run] [View on GitHub]                                 │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

**Features:**
- Back button that works (proper routing)
- Full log viewer with ANSI color support
- Auto-scroll for running builds
- Copy log button
- Re-run button
- Link to commit on forge

### 4. Workers Page (`/workers`)

**Purpose:** See connected workers, add new ones

**Layout with workers:**
```
┌─────────────────────────────────────────────────────────────┐
│ Cinch          Jobs [Workers] Settings Badges    user ▼    │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Workers                                    [Add Worker]   │
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ ● macbook-pro                                        │   │
│  │   Online · Idle · darwin/arm64                       │   │
│  │   Connected 2 hours ago                              │   │
│  ├─────────────────────────────────────────────────────┤   │
│  │ ● linux-server                                       │   │
│  │   Online · Running job abc123 · linux/amd64         │   │
│  │   Connected 1 day ago                                │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

**Empty state:**
```
┌─────────────────────────────────────────────────────────────┐
│                                                             │
│                  No workers connected                       │
│                                                             │
│     Workers run your builds on your hardware.              │
│                                                             │
│     1. Install the CLI                                      │
│        curl -sSL https://cinch.sh/install.sh | sh          │
│                                                             │
│     2. Start a worker                                       │
│        cinch worker --token=xxxxxxxx                        │
│        [Copy Command]                                       │
│                                                             │
│     Your token: [Show Token]                                │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

**Features:**
- Online/offline status with color indicator
- Current activity (idle, running job X)
- Platform info (os/arch)
- Connection duration
- Add worker flow with token

### 5. Settings Page (`/settings`)

**Purpose:** Manage account, tokens, connections

**Layout:**
```
┌─────────────────────────────────────────────────────────────┐
│ Cinch          Jobs Workers [Settings] Badges    user ▼    │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Settings                                                   │
│                                                             │
│  ─────────────────────────────────────────────────────────  │
│  Account                                                    │
│  ─────────────────────────────────────────────────────────  │
│                                                             │
│  Logged in as: ehrlich-b (GitHub)                          │
│  Plan: Pro ✓                                                │
│                                                             │
│  ─────────────────────────────────────────────────────────  │
│  Worker Token                                               │
│  ─────────────────────────────────────────────────────────  │
│                                                             │
│  Your worker token: ●●●●●●●●●●●●  [Show] [Regenerate]      │
│                                                             │
│  ─────────────────────────────────────────────────────────  │
│  Connected Repos                                            │
│  ─────────────────────────────────────────────────────────  │
│                                                             │
│  • ehrlich-b/cinch (GitHub App)                            │
│  • ehrlich-b/other-repo (GitHub App)                       │
│                                                             │
│  [Manage GitHub App Installation]                          │
│                                                             │
│  ─────────────────────────────────────────────────────────  │
│  Danger Zone                                                │
│  ─────────────────────────────────────────────────────────  │
│                                                             │
│  [Delete Account]                                           │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### 6. Badges Page (`/badges`)

**Purpose:** Get badge markdown for READMEs

**Layout:**
```
┌─────────────────────────────────────────────────────────────┐
│ Cinch          Jobs Workers Settings [Badges]    user ▼    │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Build Badges                                               │
│                                                             │
│  Add a build status badge to your README.                  │
│                                                             │
│  ─────────────────────────────────────────────────────────  │
│  Select Repository                                          │
│  ─────────────────────────────────────────────────────────  │
│                                                             │
│  [ehrlich-b/cinch           ▼]                             │
│                                                             │
│  ─────────────────────────────────────────────────────────  │
│  Preview                                                    │
│  ─────────────────────────────────────────────────────────  │
│                                                             │
│           [=========== passing ===========]                 │
│                                                             │
│  ─────────────────────────────────────────────────────────  │
│  Markdown                                                   │
│  ─────────────────────────────────────────────────────────  │
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ [![CI](https://cinch.sh/badge/...)](https://...)    │   │
│  └─────────────────────────────────────────────────────┘   │
│  [Copy]                                                     │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

**Fix the badge:**
- Current badge shows "CI CI" because logo is "CI" and label is "CI"
- Change to: logo only, message is "passing/failing"
- Or: no logo, just "build: passing"

---

## Technical Requirements

### URL Routing

Use `history.pushState` and `popstate` event:

```typescript
// On navigation
function navigate(path: string) {
  history.pushState({}, '', path)
  // Update state to render correct page
}

// On back/forward
window.addEventListener('popstate', () => {
  // Parse location.pathname and render
})

// On initial load
// Parse location.pathname
```

Routes:
- `/` - Landing (unauth) or redirect to /jobs (auth)
- `/jobs` - Jobs list
- `/jobs/:id` - Job detail
- `/workers` - Workers
- `/settings` - Settings
- `/badges` - Badges

### Error Handling

Every API call needs:
```typescript
try {
  const res = await fetch(url)
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return await res.json()
} catch (e) {
  setError('Failed to load. Please try again.')
}
```

Show error states with retry button.

### Loading States

- Skeleton loaders for lists
- Spinner for actions
- Optimistic updates where safe

---

## Visual Design

### Color Palette

```css
--bg: #0d1117;           /* Main background */
--bg-secondary: #161b22; /* Cards, panels */
--bg-tertiary: #1c2128;  /* Hover states */
--border: #30363d;       /* Borders */
--text: #e6edf3;         /* Primary text */
--text-muted: #8b949e;   /* Secondary text */
--accent: #3fb950;       /* Green - success, CTAs */
--accent-hover: #2ea043; /* Green hover */
--failure: #f85149;      /* Red - errors, failures */
--warning: #d29922;      /* Yellow - warnings */
--running: #58a6ff;      /* Blue - in progress */
```

### Typography

- Headings: System font, bold
- Body: System font, regular
- Code: ui-monospace, SF Mono, Monaco

### Spacing Scale

```css
--space-1: 4px;
--space-2: 8px;
--space-3: 12px;
--space-4: 16px;
--space-5: 24px;
--space-6: 32px;
--space-7: 48px;
--space-8: 64px;
```

### Components

**Card:**
```css
background: var(--bg-secondary);
border: 1px solid var(--border);
border-radius: 8px;
padding: var(--space-5);
```

**Button (primary):**
```css
background: var(--accent);
color: var(--bg);
border: none;
border-radius: 6px;
padding: var(--space-3) var(--space-4);
font-weight: 600;
```

**Button (secondary):**
```css
background: transparent;
color: var(--text);
border: 1px solid var(--border);
border-radius: 6px;
padding: var(--space-3) var(--space-4);
```

---

## Implementation Priority

### Phase 1: Foundation
1. Add URL routing with history API
2. Fix badges page (remove duplicate CI)
3. Add proper error handling
4. Add loading skeletons

### Phase 2: Polish
5. Redesign hero to show config simplicity
6. Add empty states with onboarding guidance
7. Add relative timestamps
8. Implement Settings page properly

### Phase 3: Features
9. Add job filtering
10. Add worker management
11. Add re-run builds
12. Add badge repo selector

---

## Open Questions

1. **Do we need a docs page?** Or link to GitHub wiki?
2. **Should landing be separate from app?** Different React apps or same?
3. **Mobile support priority?** Dashboard used on phone?
4. **Notifications?** Email on build failure? Browser notifications?
