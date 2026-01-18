# Web UI Design

## Philosophy

- Embedded in binary (no separate deployment)
- Minimal dependencies (vanilla JS or tiny framework)
- Real-time updates via WebSocket
- Mobile-friendly

## Pages

### Dashboard (`/`)

Overview of recent activity.

```
┌────────────────────────────────────────────────────────────────────────────┐
│  cinch                                              [Workers: 3 online]    │
├────────────────────────────────────────────────────────────────────────────┤
│                                                                            │
│  Recent Builds                                                    [Filter] │
│  ──────────────────────────────────────────────────────────────────────── │
│                                                                            │
│  ✓  user/frontend        main      abc1234   2m 34s   3 min ago           │
│  ✓  user/api             main      def5678   45s      5 min ago           │
│  ✗  user/frontend        feat/xyz  789abc    1m 12s   10 min ago          │
│  ◐  org/backend          PR #42    bcd4567   running  started 30s ago     │
│  ◷  org/backend          main      efg7890   pending  queued              │
│                                                                            │
│  [Load more]                                                               │
│                                                                            │
└────────────────────────────────────────────────────────────────────────────┘
```

Status icons:
- `✓` success (green)
- `✗` failure (red)
- `◐` running (blue, animated)
- `◷` pending (gray)
- `⚠` error (yellow)

### Job Detail (`/jobs/{id}`)

Single job with streaming logs.

```
┌────────────────────────────────────────────────────────────────────────────┐
│  cinch                                              [Workers: 3 online]    │
├────────────────────────────────────────────────────────────────────────────┤
│                                                                            │
│  ← Back                                                                    │
│                                                                            │
│  Job #j_abc123                                              ✓ SUCCESS      │
│  ──────────────────────────────────────────────────────────────────────── │
│                                                                            │
│  Repository:  user/frontend                                                │
│  Branch:      main                                                         │
│  Commit:      abc1234 "Fix login button alignment"                        │
│  Worker:      macbook-pro (linux/amd64)                                   │
│  Duration:    2m 34s                                                       │
│  Command:     make ci                                                      │
│                                                                            │
│  ──────────────────────────────────────────────────────────────────────── │
│                                                                            │
│  Logs                                                        [Raw] [Copy] │
│  ┌────────────────────────────────────────────────────────────────────┐  │
│  │ $ git clone https://github.com/user/frontend.git                   │  │
│  │ Cloning into 'frontend'...                                         │  │
│  │ $ cd frontend && make ci                                           │  │
│  │ npm install                                                         │  │
│  │ npm test                                                            │  │
│  │                                                                     │  │
│  │ PASS src/App.test.js                                               │  │
│  │ PASS src/components/Button.test.js                                 │  │
│  │                                                                     │  │
│  │ Tests:       42 passed                                             │  │
│  │ Time:        12.34s                                                │  │
│  │                                                                     │  │
│  │ npm run build                                                       │  │
│  │ Build completed successfully.                                       │  │
│  │                                                                     │  │
│  │ ✓ Exit code: 0                                                     │  │
│  └────────────────────────────────────────────────────────────────────┘  │
│                                                                            │
└────────────────────────────────────────────────────────────────────────────┘
```

Features:
- Real-time log streaming (WebSocket)
- ANSI color support in logs
- Auto-scroll (toggleable)
- Download raw logs
- Copy to clipboard

### Workers (`/workers`)

List of connected workers.

```
┌────────────────────────────────────────────────────────────────────────────┐
│  cinch                                              [Workers: 3 online]    │
├────────────────────────────────────────────────────────────────────────────┤
│                                                                            │
│  Workers                                                   [+ Add Worker]  │
│  ──────────────────────────────────────────────────────────────────────── │
│                                                                            │
│  ● macbook-pro          online    linux/amd64     0 jobs   last: 2s ago   │
│  ● build-server-1       online    linux/amd64     1 job    last: 5s ago   │
│  ● raspberry-pi         online    linux/arm64     0 jobs   last: 10s ago  │
│  ○ old-laptop           offline   darwin/amd64    -        last: 2h ago   │
│                                                                            │
│  ──────────────────────────────────────────────────────────────────────── │
│                                                                            │
│  + Add Worker                                                              │
│  ┌────────────────────────────────────────────────────────────────────┐   │
│  │  Name: [_______________]                                           │   │
│  │  Labels: [linux, amd64___]                                         │   │
│  │                                                        [Create]    │   │
│  └────────────────────────────────────────────────────────────────────┘   │
│                                                                            │
│  After creating, run:                                                      │
│  cinch worker --server https://cinch.sh --token <token>                   │
│                                                                            │
└────────────────────────────────────────────────────────────────────────────┘
```

### Repo Settings (`/repos/{owner}/{name}`)

Per-repo configuration.

```
┌────────────────────────────────────────────────────────────────────────────┐
│  cinch                                              [Workers: 3 online]    │
├────────────────────────────────────────────────────────────────────────────┤
│                                                                            │
│  ← Back                                                                    │
│                                                                            │
│  user/frontend                                                             │
│  ──────────────────────────────────────────────────────────────────────── │
│                                                                            │
│  Webhook URL:                                                              │
│  https://cinch.sh/webhook/github?repo=user/frontend                       │
│  [Copy]                                                                    │
│                                                                            │
│  Webhook Secret:                                                           │
│  ●●●●●●●●●●●●●●●●                                           [Regenerate]  │
│                                                                            │
│  Recent Builds                                                             │
│  ✓  main      abc1234   2m 34s   3 min ago                                │
│  ✗  feat/xyz  789abc    1m 12s   10 min ago                               │
│                                                                            │
│  [Delete Repo]                                                             │
│                                                                            │
└────────────────────────────────────────────────────────────────────────────┘
```

## Tech Stack

### Option A: Vanilla JS (Recommended for v0.1)

**Pros:**
- No build step
- Tiny bundle size
- Easy to embed
- Works forever (no framework churn)

**Cons:**
- More boilerplate
- No reactivity magic

**Structure:**
```
web/
├── static/
│   ├── index.html
│   ├── app.js
│   ├── style.css
│   └── favicon.ico
└── embed.go          # go:embed directive
```

### Option B: Preact/Alpine.js

**Pros:**
- Reactive updates
- Component model
- Still tiny (~3KB)

**Cons:**
- Needs build step (or use CDN)

### Decision: Vanilla JS for v0.1

Keep it simple. If UI complexity grows, migrate to Preact later.

## Embedding

```go
//go:embed static/*
var staticFiles embed.FS

func (s *Server) setupRoutes() {
    // API routes
    s.mux.HandleFunc("/api/jobs", s.handleJobs)
    s.mux.HandleFunc("/api/workers", s.handleWorkers)

    // WebSocket for real-time
    s.mux.HandleFunc("/ws/logs", s.handleLogStream)

    // Static files (fallback to index.html for SPA routing)
    s.mux.Handle("/", http.FileServer(http.FS(staticFiles)))
}
```

## API Endpoints

### Jobs

```
GET  /api/jobs              List jobs (paginated)
GET  /api/jobs/{id}         Get job details
GET  /api/jobs/{id}/logs    Get job logs (or WebSocket for streaming)
POST /api/jobs/{id}/cancel  Cancel running job
```

### Workers

```
GET    /api/workers         List workers
POST   /api/workers         Create worker (returns token)
DELETE /api/workers/{id}    Delete worker
```

### Repos

```
GET    /api/repos           List repos
GET    /api/repos/{id}      Get repo details
POST   /api/repos/{id}/webhook-secret  Regenerate webhook secret
DELETE /api/repos/{id}      Delete repo
```

## WebSocket Streams

### Log Streaming

```javascript
const ws = new WebSocket(`wss://${host}/ws/logs?job=${jobId}`);

ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  // msg: { type: "log", data: "line of output\n", stream: "stdout" }
  // msg: { type: "status", status: "running" }
  // msg: { type: "complete", exit_code: 0 }
  appendToLog(msg);
};
```

### Dashboard Updates

```javascript
const ws = new WebSocket(`wss://${host}/ws/events`);

ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  // msg: { type: "job_created", job: {...} }
  // msg: { type: "job_updated", job: {...} }
  // msg: { type: "worker_status", worker: {...} }
  updateUI(msg);
};
```

## Styling

### Colors

```css
:root {
  --bg: #0d1117;
  --bg-secondary: #161b22;
  --text: #c9d1d9;
  --text-muted: #8b949e;
  --border: #30363d;

  --success: #3fb950;
  --failure: #f85149;
  --pending: #8b949e;
  --running: #58a6ff;
  --warning: #d29922;
}
```

Dark mode by default (developers live in dark mode).

### Typography

```css
body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif;
  font-size: 14px;
}

.log-output {
  font-family: "SF Mono", "Fira Code", Consolas, monospace;
  font-size: 12px;
}
```

## ANSI Color Support

Logs may contain ANSI escape codes. Parse and convert to HTML:

```javascript
function ansiToHtml(text) {
  // Convert \x1b[32m to <span class="ansi-green">
  // Convert \x1b[0m to </span>
  // Handle: bold, colors (8 + 8 bright), reset
}
```

Libraries: `ansi-to-html` (npm) or simple regex replacement.

## Mobile Support

- Responsive layout (flexbox/grid)
- Touch-friendly buttons (44px min)
- Log viewer scrolls horizontally
- Hamburger menu for nav on small screens

## Authentication

For hosted version:
- Login page (`/login`)
- OAuth with GitHub/GitLab
- Session cookie
- API key for CLI tools

For self-hosted:
- Optional auth (single-user mode)
- Basic auth or token-based
- Configure via env vars

## Future Enhancements

- **Dark/light theme toggle**
- **Keyboard shortcuts** (j/k navigation, r to retry)
- **Search/filter jobs**
- **Build badges** (`/badge/{repo}.svg`)
- **Notifications** (browser push for failures)
