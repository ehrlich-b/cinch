# Self-Hosting Cinch

This guide covers running your own Cinch control plane. You'll have full control over your CI infrastructure with no dependency on cinch.sh.

## Quick Start

Minimal single-machine setup:

```bash
# 1. Generate a JWT secret (CRITICAL - keep this safe)
export CINCH_JWT_SECRET=$(openssl rand -hex 32)

# 2. Start the server
cinch server --port 8080

# 3. In another terminal, login and start a worker
cinch login --server http://localhost:8080
cinch repo add
cinch worker
```

For production, you'll want to configure forge integrations (GitHub/GitLab/Forgejo) and run behind a reverse proxy with TLS.

## Environment Variables

### Core Server Config

| Variable | Default | Description |
|----------|---------|-------------|
| `CINCH_ADDR` | `:8080` | Listen address (host:port) |
| `CINCH_DATA_DIR` | `./data` | Directory for SQLite database and local logs |
| `CINCH_BASE_URL` | Auto-detect | Public URL for webhooks (e.g., `https://ci.example.com`) |
| `CINCH_WS_BASE_URL` | Same as BASE_URL | WebSocket URL for workers (usually same host, `wss://`) |
| `CINCH_JWT_SECRET` | **Required** | Secret for encrypting tokens. Generate with `openssl rand -hex 32` |
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

### GitHub App

GitHub requires a GitHub App (not OAuth App) for webhook integration and installation tokens.

1. **Create the App:**
   - Go to GitHub → Settings → Developer settings → GitHub Apps → New GitHub App
   - Name: "Cinch CI" (or your preferred name)
   - Homepage URL: Your Cinch server URL
   - Callback URL: `https://ci.example.com/auth/github/callback`
   - Webhook URL: `https://ci.example.com/webhooks/github`
   - Webhook secret: Generate with `openssl rand -hex 20`

2. **Permissions:**
   - Repository permissions:
     - Contents: Read and write (for cloning and releases)
     - Metadata: Read-only (required)
     - Commit statuses: Read and write
     - Pull requests: Read and write (for PR comments)
   - Account permissions:
     - Email addresses: Read-only (for user identification)

3. **Subscribe to events:**
   - Push
   - Pull request
   - Create (for tags)

4. **Generate private key:**
   - After creating the app, scroll to "Private keys" and generate one
   - Download the `.pem` file

5. **Configure Cinch:**
   ```bash
   export CINCH_GITHUB_APP_ID=123456
   export CINCH_GITHUB_APP_PRIVATE_KEY="$(cat /path/to/private-key.pem)"
   export CINCH_GITHUB_APP_WEBHOOK_SECRET=your-webhook-secret
   export CINCH_GITHUB_APP_CLIENT_ID=Iv1.xxxxxxxx
   export CINCH_GITHUB_APP_CLIENT_SECRET=xxxxxxxx
   ```

6. **Install the app** on repositories that should use Cinch.

### GitLab OAuth

1. **Create OAuth Application:**
   - Go to GitLab → Settings → Applications (or Admin → Applications for instance-wide)
   - Name: "Cinch CI"
   - Redirect URI: `https://ci.example.com/auth/gitlab/callback`
   - Scopes: `api`, `read_user`, `read_repository`

2. **Configure webhooks** per-project:
   - URL: `https://ci.example.com/webhooks/gitlab`
   - Secret token: Generate and save for each project
   - Triggers: Push events, Tag push events, Merge request events

3. **Configure Cinch:**
   ```bash
   export CINCH_GITLAB_CLIENT_ID=your-client-id
   export CINCH_GITLAB_CLIENT_SECRET=your-client-secret
   # For self-hosted GitLab:
   export CINCH_GITLAB_URL=https://gitlab.yourcompany.com
   ```

### Forgejo/Codeberg/Gitea

1. **Create OAuth Application:**
   - Go to Settings → Applications → Create a new OAuth2 Application
   - Application name: "Cinch CI"
   - Redirect URI: `https://ci.example.com/auth/forgejo/callback`

2. **Configure webhooks** per-repository:
   - URL: `https://ci.example.com/webhooks/forgejo`
   - HTTP Method: POST
   - Content type: application/json
   - Secret: Generate and save
   - Trigger on: Push, Create (for tags), Pull Request

3. **Configure Cinch:**
   ```bash
   export CINCH_FORGEJO_CLIENT_ID=your-client-id
   export CINCH_FORGEJO_CLIENT_SECRET=your-client-secret
   # For self-hosted Forgejo:
   export CINCH_FORGEJO_URL=https://git.yourcompany.com
   ```

## Webhook Configuration

Each forge sends webhooks to a specific endpoint:

| Forge | Webhook URL |
|-------|-------------|
| GitHub | `https://ci.example.com/webhooks/github` |
| GitLab | `https://ci.example.com/webhooks/gitlab` |
| Forgejo/Gitea | `https://ci.example.com/webhooks/forgejo` |

**Important:** Webhooks must be able to reach your server. If self-hosting behind a firewall, you have several options:

### Option 1: Public IP / Port Forwarding
If your server has a public IP or you can configure port forwarding on your router, expose port 443 (or your chosen port) and point your domain's DNS at it.

### Option 2: Tunnel Services
For development or home setups without a public IP:

- **Cloudflare Tunnel** (free): `cloudflared tunnel --url http://localhost:8080`
- **ngrok** (free tier available): `ngrok http 8080`
- **Tailscale Funnel** (free): `tailscale funnel 8080`

These create a public URL that forwards to your local Cinch server.

### Option 3: VPS Reverse Proxy
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

Create `/etc/systemd/system/cinch.service`:

```ini
[Unit]
Description=Cinch CI Server
After=network.target

[Service]
Type=simple
User=cinch
Group=cinch
WorkingDirectory=/var/lib/cinch
EnvironmentFile=/etc/cinch/env
ExecStart=/usr/local/bin/cinch server
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Create `/etc/cinch/env` with your environment variables:

```bash
CINCH_JWT_SECRET=your-secret-here
CINCH_BASE_URL=https://ci.example.com
CINCH_DATA_DIR=/var/lib/cinch
# ... other variables
```

Enable and start:

```bash
sudo systemctl daemon-reload
sudo systemctl enable cinch
sudo systemctl start cinch
```

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
      - CINCH_JWT_SECRET=${CINCH_JWT_SECRET}
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
