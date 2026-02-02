# Self-Hosting Cinch

This guide covers running your own Cinch control plane. You'll have full control over your CI infrastructure with no dependency on cinch.sh.

## Quick Start (with Relay)

The easiest way to self-host. Your server connects *outbound* to cinch.sh for webhook forwarding - no port forwarding or tunnels needed.

```bash
# 1. Generate a secret key (CRITICAL - keep this safe, you'll need it for key rotation)
export CINCH_SECRET_KEY=$(openssl rand -hex 32)

# 2. Set org-level PAT for your forge (used for webhooks + status checks)
export CINCH_GITHUB_TOKEN=ghp_xxx      # GitHub PAT with repo scope
# OR: export CINCH_GITLAB_TOKEN=glpat-xxx
# OR: export CINCH_FORGEJO_TOKEN=xxx

# 3. Login to cinch.sh (reserves your relay ID)
cinch login

# 4. Start the server with relay
cinch server --relay
# Output:
#   Relay URL: https://cinch.sh/relay/x7k9m
#   Admin token: cinch_xxx

# 5. Add a repo (webhook auto-created via org token)
cinch repo add owner/repo

# 6. On worker machines, connect via env vars
export CINCH_URL=http://your-server:8080
export CINCH_TOKEN=<admin-token-from-step-4>
cinch worker
```

Workers talk only to your server. Webhook secrets are validated locally. cinch.sh is just a dumb pipe for webhooks. The relay is free—it costs us nearly nothing to operate.

## Quick Start (Fully Independent)

For zero cinch.sh dependency, you need your own webhook ingress (public IP, tunnel, or VPS).

```bash
# 1. Generate a secret key
export CINCH_SECRET_KEY=$(openssl rand -hex 32)

# 2. Set org-level PAT for your forge
export CINCH_GITHUB_TOKEN=ghp_xxx

# 3. Start the server (no --relay)
cinch server --port 8080

# 4. Add a repo (webhook auto-created via org token)
cinch repo add owner/repo

# 5. Workers connect via env vars
export CINCH_URL=http://your-server:8080
export CINCH_TOKEN=<admin-token>
cinch worker
```

For production, you'll want to run behind a reverse proxy with TLS.

## Resource Requirements

Cinch is lightweight. Observed usage (not rigorous benchmarks):

| Component | RAM (idle) | CPU (idle) | Notes |
|-----------|------------|------------|-------|
| Control plane | ~15-20 MB | ~0% | Spikes briefly on webhook/dispatch |
| Worker | ~10-15 MB | ~0% | Build processes use their own resources |
| SQLite | ~10 KB/job | - | Job metadata, repos, tokens |
| Logs | ~10 KB - 1 MB/job | - | Plain text, one file per job |

A Raspberry Pi can run a worker. A Pi 4 can run the full stack.

## Privacy

Cinch does not collect telemetry. No analytics, no usage tracking, no phone-home.

Self-hosted deployments have zero communication with cinch.sh unless you explicitly enable the webhook relay. Even with the relay, cinch.sh only sees HTTP headers and encrypted webhook payloads—it never sees your code, credentials, or build logs.

## Upgrades

```bash
sudo systemctl stop cinch-server
cinch install
sudo systemctl start cinch-server
```

Workers auto-reconnect. No database migrations needed.

## Environment Variables

### Core Server Config

| Variable | Default | Description |
|----------|---------|-------------|
| `CINCH_ADDR` | `:8080` | Listen address (host:port) |
| `CINCH_DATA_DIR` | `./data` | Directory for SQLite database and local logs |
| `CINCH_BASE_URL` | Auto-detect | Public URL for webhooks (e.g., `https://ci.example.com`) |
| `CINCH_WS_BASE_URL` | Same as BASE_URL | WebSocket URL for workers (usually same host, `wss://`) |
| `CINCH_SECRET_KEY` | **Required** | Secret for JWT signing and data encryption. Generate with `openssl rand -hex 32`. **Save this - you need it for key rotation.** |
| `CINCH_LOG_DIR` | `$CINCH_DATA_DIR/logs` | Directory for job log storage |

### Log Storage (R2)

For cloud log storage instead of local filesystem:

| Variable | Description |
|----------|-------------|
| `CINCH_R2_ACCOUNT_ID` | Cloudflare account ID |
| `CINCH_R2_ACCESS_KEY_ID` | R2 access key |
| `CINCH_R2_SECRET_ACCESS_KEY` | R2 secret key |
| `CINCH_R2_BUCKET` | R2 bucket name |

### GitHub App

| Variable | Description |
|----------|-------------|
| `CINCH_GITHUB_APP_ID` | GitHub App ID (numeric) |
| `CINCH_GITHUB_APP_PRIVATE_KEY` | Private key (PEM format, can include newlines) |
| `CINCH_GITHUB_APP_WEBHOOK_SECRET` | Webhook secret for signature verification |
| `CINCH_GITHUB_APP_CLIENT_ID` | OAuth client ID (for user login) |
| `CINCH_GITHUB_APP_CLIENT_SECRET` | OAuth client secret |

### GitLab OAuth

| Variable | Default | Description |
|----------|---------|-------------|
| `CINCH_GITLAB_CLIENT_ID` | | OAuth application ID |
| `CINCH_GITLAB_CLIENT_SECRET` | | OAuth application secret |
| `CINCH_GITLAB_URL` | `https://gitlab.com` | GitLab instance URL |

### Forgejo/Gitea OAuth

| Variable | Default | Description |
|----------|---------|-------------|
| `CINCH_FORGEJO_CLIENT_ID` | | OAuth application ID |
| `CINCH_FORGEJO_CLIENT_SECRET` | | OAuth application secret |
| `CINCH_FORGEJO_URL` | `https://codeberg.org` | Forgejo/Gitea instance URL |

## Forge Setup

### GitHub

**Option A: Org token (recommended)**—webhooks auto-created via `cinch repo add`:
```bash
export CINCH_GITHUB_TOKEN=ghp_xxx  # PAT with repo scope
```

**Option B: GitHub App**—for multi-user login and fine-grained permissions:

1. Create the App: GitHub → Settings → Developer settings → GitHub Apps
   - Callback URL: `https://ci.example.com/auth/github/callback`
   - Webhook URL: `https://ci.example.com/webhooks/github`
   - Webhook secret: `openssl rand -hex 20`
2. Permissions: Contents (read/write), Metadata (read), Commit statuses (read/write), Pull requests (read/write), Email addresses (read)
3. Events: Push, Pull request, Create
4. Generate and download private key
5. Configure Cinch:
   ```bash
   export CINCH_GITHUB_APP_ID=123456
   export CINCH_GITHUB_APP_PRIVATE_KEY="$(cat /path/to/private-key.pem)"
   export CINCH_GITHUB_APP_WEBHOOK_SECRET=your-webhook-secret
   export CINCH_GITHUB_APP_CLIENT_ID=Iv1.xxxxxxxx
   export CINCH_GITHUB_APP_CLIENT_SECRET=xxxxxxxx
   ```
6. Install the app on repositories that should use Cinch.

### GitLab

**Option A: Org token (recommended)**—webhooks auto-created via `cinch repo add`:
```bash
export CINCH_GITLAB_TOKEN=glpat-xxx  # PAT with api scope
export CINCH_GITLAB_URL=https://gitlab.yourcompany.com  # for self-hosted
```

**Option B: OAuth**—for multi-user login (like cinch.sh):
1. Create OAuth Application: GitLab → Settings → Applications
   - Redirect URI: `https://ci.example.com/auth/gitlab/callback`
   - Scopes: `api`, `read_user`, `read_repository`
2. Configure Cinch:
   ```bash
   export CINCH_GITLAB_CLIENT_ID=your-client-id
   export CINCH_GITLAB_CLIENT_SECRET=your-client-secret
   ```

### Forgejo/Codeberg/Gitea

**Option A: Org token (recommended)**—webhooks auto-created via `cinch repo add`:
```bash
export CINCH_FORGEJO_TOKEN=xxx  # PAT with repo scope
export CINCH_FORGEJO_URL=https://git.yourcompany.com  # for self-hosted
```

**Option B: OAuth**—for multi-user login:
1. Create OAuth Application: Settings → Applications → Create new OAuth2 Application
   - Redirect URI: `https://ci.example.com/auth/forgejo/callback`
2. Configure Cinch:
   ```bash
   export CINCH_FORGEJO_CLIENT_ID=your-client-id
   export CINCH_FORGEJO_CLIENT_SECRET=your-client-secret
   ```

## Webhook Ingress

Webhooks are auto-created when you run `cinch repo add`. The only question is: can your forge reach your server?

| Forge | Webhook Endpoint |
|-------|------------------|
| GitHub | `/webhooks/github` |
| GitLab | `/webhooks/gitlab` |
| Forgejo/Gitea | `/webhooks/forgejo` |

If you're behind a firewall or NAT, you have several options:

### Option 1: Built-in Relay (Recommended)

Cinch has a built-in webhook relay. Your server connects *outbound* to cinch.sh, and webhooks are forwarded over WebSocket. No port forwarding, no extra services. The relay is free—idle WebSocket connections cost us nearly nothing to maintain.

```bash
cinch login                      # Login to cinch.sh (reserves your relay ID)
cinch server --relay             # Connects outbound to cinch.sh relay
# Output:
# Relay URL: https://cinch.sh/relay/x7k9m
# Admin token: cinch_xxx

cinch repo add owner/repo        # Webhook auto-created, points to relay URL
```

Webhook secrets are validated locally - cinch.sh is just a dumb pipe.

Workers connect to your self-hosted server, not cinch.sh:
```bash
export CINCH_URL=http://your-server:8080
export CINCH_TOKEN=<admin-token-from-above>
cinch worker
```

### Option 2: Public IP / Port Forwarding
If your server has a public IP or you can configure port forwarding on your router, expose port 443 (or your chosen port) and point your domain's DNS at it.

### Option 3: Tunnel Services
For development or home setups without a public IP:

- **Cloudflare Tunnel** (free): `cloudflared tunnel --url http://localhost:8080`
- **ngrok** (free tier available): `ngrok http 8080`
- **Tailscale Funnel** (free): `tailscale funnel 8080`

These create a public URL that forwards to your local Cinch server.

### Option 4: VPS Reverse Proxy
Run a small VPS (e.g., $5/month DigitalOcean droplet) as a reverse proxy. Your home server connects outbound to the VPS, and webhooks hit the VPS's public IP.

## Reverse Proxy

### nginx

```nginx
server {
    listen 443 ssl http2;
    server_name ci.example.com;

    ssl_certificate /etc/ssl/certs/ci.example.com.crt;
    ssl_certificate_key /etc/ssl/private/ci.example.com.key;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSocket support
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";

        # Timeouts for long-running connections
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }
}
```

### Caddy

```caddyfile
ci.example.com {
    reverse_proxy localhost:8080
}
```

Caddy automatically handles TLS and WebSocket upgrades.

## Database

### SQLite (Default)

SQLite is the default and works well for most deployments:

```bash
export CINCH_DATA_DIR=/var/lib/cinch
# Database will be at /var/lib/cinch/cinch.db
```

**Backup:** Simply copy the `.db` file (ensure no writes during copy, or use `sqlite3 .backup`).

### PostgreSQL

For high-availability deployments:

```bash
cinch server --db-type postgres --db "postgres://user:pass@localhost/cinch"
```

## Log Storage

### Filesystem (Default)

Logs are stored in `$CINCH_LOG_DIR` (defaults to `$CINCH_DATA_DIR/logs`).

```bash
export CINCH_LOG_DIR=/var/log/cinch
```

Logs are stored as plain text files, one per job.

### Cloudflare R2

For distributed or cloud deployments:

```bash
export CINCH_R2_ACCOUNT_ID=your-account-id
export CINCH_R2_ACCESS_KEY_ID=your-access-key
export CINCH_R2_SECRET_ACCESS_KEY=your-secret-key
export CINCH_R2_BUCKET=cinch-logs
```

R2 is S3-compatible, so other S3-compatible storage may work (untested).

## Security Checklist

### Critical

- [ ] **Generate a strong JWT secret** - `openssl rand -hex 32`
- [ ] **Never commit secrets** - Use environment variables or secret management
- [ ] **Use HTTPS** - Webhooks contain sensitive data
- [ ] **Verify webhook signatures** - Cinch does this automatically when secrets are configured

### Recommended

- [ ] **Run as non-root user** - Create a dedicated `cinch` user
- [ ] **Limit worker permissions** - Workers execute arbitrary code; isolate appropriately
- [ ] **Set up log rotation** - For filesystem log storage
- [ ] **Monitor disk space** - Logs and build artifacts accumulate
- [ ] **Regular backups** - Back up the SQLite database and any persistent data

### Network

- [ ] **Firewall** - Only expose ports 80/443 (or your chosen port)
- [ ] **Rate limiting** - Consider rate limiting webhook endpoints
- [ ] **Internal network** - Workers can connect from internal network; only webhooks need public access

## Systemd Service

The easiest way to install as a system service:

```bash
# Set your env vars first
export CINCH_SECRET_KEY=$(openssl rand -hex 32)
export CINCH_GITHUB_TOKEN=ghp_xxx
export CINCH_BASE_URL=https://ci.example.com

# Install (captures current CINCH_* env vars)
sudo -E cinch server install

# Start
sudo systemctl enable cinch-server
sudo systemctl start cinch-server
```

This creates:
- `/etc/systemd/system/cinch-server.service`
- `/etc/cinch/env` (your env vars, mode 0600)
- `/var/lib/cinch` (data directory)

To view logs: `journalctl -u cinch-server -f`

To uninstall: `sudo cinch server uninstall`

## Docker

```dockerfile
FROM golang:1.22 AS builder
WORKDIR /app
COPY . .
RUN make build

FROM debian:bookworm-slim
COPY --from=builder /app/cinch /usr/local/bin/
EXPOSE 8080
CMD ["cinch", "server"]
```

Docker Compose example:

```yaml
version: '3.8'
services:
  cinch:
    build: .
    ports:
      - "8080:8080"
    environment:
      - CINCH_SECRET_KEY=${CINCH_SECRET_KEY}
      - CINCH_BASE_URL=https://ci.example.com
      - CINCH_GITHUB_APP_ID=${CINCH_GITHUB_APP_ID}
      # ... other variables
    volumes:
      - cinch-data:/var/lib/cinch

volumes:
  cinch-data:
```

## Health Check

Cinch exposes a `/health` endpoint for monitoring and container orchestration:

```bash
curl https://ci.example.com/health
# {"status":"ok"}
```

Use this for:
- Docker healthchecks
- Load balancer health probes
- Uptime monitoring (UptimeRobot, etc.)

## Troubleshooting

### Workers not connecting

1. Check that `CINCH_BASE_URL` and `CINCH_WS_BASE_URL` are correct
2. Verify WebSocket upgrade is working through your reverse proxy
3. Check worker logs: `cinch worker --verbose` (after `cinch login --server URL`)

### Webhooks not received

1. Verify the webhook URL is publicly accessible
2. Check webhook delivery logs in your forge (GitHub/GitLab/Forgejo)
3. Ensure webhook secret matches between forge and Cinch config

### Jobs stuck in pending

1. Check that at least one worker is connected
2. Verify worker labels match job requirements (if using `workers:` in config)
3. Check server logs for dispatch errors

### Authentication failures

1. Verify OAuth redirect URLs match exactly (including trailing slashes)
2. Check that client ID/secret are correct
3. For GitHub, ensure the app is installed on the repository
