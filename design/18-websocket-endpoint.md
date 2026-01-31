# WebSocket Endpoint Configuration

## Problem

The managed service (cinch.sh) needs to split HTTP and WebSocket traffic:
- HTTP API at `https://cinch.sh` - proxied through Cloudflare for CDN/DDoS protection
- WebSocket at `wss://ws.cinch.sh` - direct to Fly (DNS only, no Cloudflare proxy)

This split is needed because:
1. Cloudflare has WebSocket connection limits on non-Enterprise plans
2. WebSocket connections are long-lived and mostly idle - perfect for direct connection
3. HTTP traffic benefits from Cloudflare caching/protection

But self-hosted users should NOT need this complexity. One domain, one DNS record.

## Solution

Server tells clients where to connect for WebSocket.

### Server Config

New optional environment variable:

```bash
# Managed service (fly.toml)
CINCH_WS_BASE_URL=wss://ws.cinch.sh

# Self-hosted (nothing needed - defaults to same as BASE_URL)
```

### API Response

`/api/whoami` and login endpoints include the WebSocket URL:

```json
{
  "user": "alice@example.com",
  "ws_url": "wss://ws.cinch.sh"
}
```

If `CINCH_WS_BASE_URL` is not set, server returns the base URL converted to `wss://`.

### Client Behavior

1. `cinch login` saves both `url` and `ws_url` to `~/.cinch/config`
2. `cinch worker` connects to `ws_url` (not derived from `url`)
3. If `ws_url` missing (old config), fall back to deriving from `url`

### Config File

```toml
[servers.default]
url = "https://cinch.sh"
ws_url = "wss://ws.cinch.sh"
token = "..."
```

## Deployment (Managed)

1. **Fly custom domain:**
   ```bash
   fly certs create ws.cinch.sh
   ```

2. **Cloudflare DNS:**
   - `cinch.sh` → Proxied (orange cloud) → Fly
   - `ws.cinch.sh` → DNS only (gray cloud) → Fly

3. **Fly secrets:**
   ```bash
   fly secrets set CINCH_WS_BASE_URL=wss://ws.cinch.sh
   ```

## Self-Hosted

Nothing changes. Single domain works as before:

```bash
cinch server --base-url https://ci.example.com
# HTTP: https://ci.example.com
# WebSocket: wss://ci.example.com/ws/worker (automatic)
```

## Security

Direct WebSocket exposure means:
- No Cloudflare DDoS protection on WebSocket endpoint
- Fly's built-in protection is the only layer
- If DDoS becomes a problem: upgrade to Cloudflare Enterprise or add rate limiting

This is acceptable because:
- WebSocket requires valid auth token
- Connection rate is low (workers connect once, stay connected)
- Actual attack surface is small
