# Multi-Forge Presence

**Status:** Planning
**Goal:** Cinch dogfoods its own multi-forge support by hosting source on GitHub, GitLab, AND Codeberg simultaneously.

## Why

1. **Proves the thesis** - "One config, every forge" isn't just marketing if we actually do it
2. **Catches bugs** - We'll find GitLab/Forgejo issues before users do
3. **Credibility** - Shows we're not just another GitHub-only tool
4. **Resilience** - If GitHub goes down, development continues elsewhere

## Strategy: True Multi-Primary

All three forges are "real" upstreams:
- Accept PRs from any forge
- Releases publish to ALL forges simultaneously
- Same commit SHAs across all three

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   GitHub    │     │   GitLab    │     │  Codeberg   │
│ ehrlich-b/  │     │ ehrlich-b/  │     │  ehrlich/   │
│   cinch     │     │   cinch     │     │   cinch     │
└──────┬──────┘     └──────┬──────┘     └──────┬──────┘
       │                   │                   │
       │    webhooks       │                   │
       └───────────┬───────┴───────────────────┘
                   │
                   ▼
            ┌─────────────┐
            │  cinch.sh   │
            │  (control   │
            │   plane)    │
            └─────────────┘
                   │
                   ▼
            ┌─────────────┐
            │   worker    │
            │ (builds &   │
            │  releases)  │
            └─────────────┘
```

## Git Mechanics

### Setup (one-time)

```bash
# Add all remotes
git remote add github git@github.com:ehrlich-b/cinch.git
git remote add gitlab git@gitlab.com:ehrlich-b/cinch.git
git remote add codeberg git@codeberg.org:ehrlich/cinch.git

# Or use HTTPS with tokens
git remote add github https://github.com/ehrlich-b/cinch.git
git remote add gitlab https://gitlab.com/ehrlich-b/cinch.git
git remote add codeberg https://codeberg.org/ehrlich/cinch.git
```

### Daily Workflow

```bash
# Push to all forges
make push

# Or manually
git push github main
git push gitlab main
git push codeberg main

# Push tags to all (triggers releases on all three)
git tag v1.0.0
make push-tags
# Or: git push github v1.0.0 && git push gitlab v1.0.0 && git push codeberg v1.0.0
```

### Accepting PRs from Other Forges

Someone opens PR on GitLab:

```bash
# Fetch their changes
git fetch gitlab

# Review, merge locally
git checkout -b feature-from-gitlab gitlab/feature-branch
# ... review ...
git checkout main
git merge feature-from-gitlab

# Push merged result to all forges
make push
```

Or merge via GitLab's UI, then sync:

```bash
git pull gitlab main
make push  # syncs to GitHub and Codeberg
```

## Makefile Targets

```makefile
# Push to all forges
push:
	git push github main
	git push gitlab main
	git push codeberg main

# Push tags to all forges (triggers releases everywhere)
push-tags:
	git push github --tags
	git push gitlab --tags
	git push codeberg --tags

# Full release: tag + push everywhere
release-tag:
	@if [ -z "$(VERSION)" ]; then echo "Usage: make release-tag VERSION=v1.0.0"; exit 1; fi
	git tag $(VERSION)
	$(MAKE) push
	$(MAKE) push-tags
```

## Release Flow

When a tag is pushed:

1. **GitHub** receives tag push → sends webhook to cinch.sh
2. **GitLab** receives tag push → sends webhook to cinch.sh
3. **Codeberg** receives tag push → sends webhook to cinch.sh

Each webhook creates a separate job:

```
Job 1: github/ehrlich-b/cinch @ v1.0.0
  → make release
  → cinch release dist/* (detects CINCH_FORGE=github)
  → Uploads to GitHub Releases

Job 2: gitlab/ehrlich-b/cinch @ v1.0.0
  → make release
  → cinch release dist/* (detects CINCH_FORGE=gitlab)
  → Uploads to GitLab Releases

Job 3: forgejo/ehrlich/cinch @ v1.0.0
  → make release
  → cinch release dist/* (detects CINCH_FORGE=forgejo)
  → Uploads to Codeberg Releases
```

All three run in parallel (if workers available) or sequentially. Same binaries built three times, uploaded to three places.

**Alternative:** Build once, upload to all three from a single job. More complex, requires storing tokens for all forges. Current approach is simpler and more resilient.

## Cinch.sh Repo Configuration

Register all three repos with the control plane:

```bash
# GitHub (already done)
cinch repo add ehrlich-b/cinch --forge github

# GitLab
cinch repo add ehrlich-b/cinch --forge gitlab --token glpat-xxx

# Codeberg
cinch repo add ehrlich/cinch --forge forgejo --url https://codeberg.org --token xxx
```

Each repo has its own webhook secret. Configure webhooks on each forge pointing to `https://cinch.sh/webhooks`.

## Install Script

The install script should work regardless of which forge hosts the release:

```bash
#!/bin/sh
# Try each forge in order until one works

GITHUB_URL="https://github.com/ehrlich-b/cinch/releases/download/${VERSION}/cinch-${OS}-${ARCH}"
GITLAB_URL="https://gitlab.com/ehrlich-b/cinch/-/releases/${VERSION}/downloads/cinch-${OS}-${ARCH}"
CODEBERG_URL="https://codeberg.org/ehrlich/cinch/releases/download/${VERSION}/cinch-${OS}-${ARCH}"

for url in "$GITHUB_URL" "$GITLAB_URL" "$CODEBERG_URL"; do
    if curl -fsSL "$url" -o cinch; then
        break
    fi
done
```

Or just pick one as the "download primary" (GitHub has best CDN) while keeping all three as source-of-truth.

## Which Forge is "Primary"?

**For development:** Whichever you prefer. All are equal.

**For issues/discussions:** Pick one (probably GitHub for discoverability) and link from others.

**For releases/downloads:** GitHub has the best CDN, so install script defaults there.

**For credibility:** Point FOSS-minded users to Codeberg, enterprise to GitLab.

## Open Questions

1. **Issue tracking** - Centralize on one forge or accept issues everywhere?
   - Recommendation: GitHub primary (most users), note in other repos "file issues on GitHub"

2. **CI status** - Show badge from which forge?
   - Recommendation: GitHub badge in README, but all three run CI

3. **Mirror vs. multi-primary** - Are PRs really accepted everywhere?
   - Recommendation: Yes, but document the sync process clearly

## Setup Checklist

- [ ] Create GitLab repo: `gitlab.com/ehrlich-b/cinch`
- [ ] Create Codeberg repo: `codeberg.org/ehrlich/cinch`
- [ ] Add remotes locally
- [ ] Push existing history to both
- [ ] Create GitLab Project Access Token (for status posting)
- [ ] Create Codeberg access token
- [ ] Register both repos with cinch.sh
- [ ] Configure webhooks on both forges
- [ ] Add `push` and `push-tags` targets to Makefile
- [ ] Update install.sh with fallback URLs
- [ ] First multi-forge release (verify all three get artifacts)
