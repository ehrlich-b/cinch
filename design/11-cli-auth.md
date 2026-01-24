# CLI Authentication & Self-Registration

## Overview

The CLI needs to authenticate with the server for two purposes:
1. **Worker registration** - Workers self-register and receive jobs
2. **Admin operations** - `cinch token`, `cinch repo add`, `cinch status`, etc.

This design uses the device authorization flow (like `gh auth login`) where the CLI opens a browser for the user to authenticate, then receives credentials.

## Device Authorization Flow

```
┌─────────────────┐                  ┌─────────────────┐                  ┌─────────────────┐
│     CLI         │                  │     Server      │                  │     Browser     │
└────────┬────────┘                  └────────┬────────┘                  └────────┬────────┘
         │                                    │                                    │
         │  POST /auth/device                 │                                    │
         │  (request device code)             │                                    │
         ├───────────────────────────────────>│                                    │
         │                                    │                                    │
         │  { device_code, user_code,         │                                    │
         │    verification_uri, expires_in }  │                                    │
         │<───────────────────────────────────│                                    │
         │                                    │                                    │
         │  Opens browser to verification_uri │                                    │
         │────────────────────────────────────┼───────────────────────────────────>│
         │                                    │                                    │
         │                                    │  GET /auth/device/verify?code=XXX  │
         │                                    │<───────────────────────────────────│
         │                                    │                                    │
         │                                    │  (user logs in via GitHub OAuth)   │
         │                                    │<──────────────────────────────────>│
         │                                    │                                    │
         │                                    │  (user confirms device code)       │
         │                                    │<───────────────────────────────────│
         │                                    │                                    │
         │  POST /auth/device/token           │                                    │
         │  (poll for token)                  │                                    │
         ├───────────────────────────────────>│                                    │
         │                                    │                                    │
         │  { access_token, token_type }      │                                    │
         │<───────────────────────────────────│                                    │
         │                                    │                                    │
         │  (store token in ~/.cinch/config)  │                                    │
         │                                    │                                    │
```

## API Endpoints

### POST /auth/device

Request a device code for CLI authentication.

**Request:**
```json
{}
```

**Response:**
```json
{
  "device_code": "abc123...",
  "user_code": "CINCH-1234",
  "verification_uri": "https://cinch.sh/auth/device/verify",
  "expires_in": 900,
  "interval": 5
}
```

### GET /auth/device/verify

Browser page where user enters/confirms the device code. Shows:
1. The user code to confirm
2. Login button (if not authenticated)
3. Authorize button (if authenticated)

### POST /auth/device/token

Poll for the access token after user authorizes.

**Request:**
```json
{
  "device_code": "abc123..."
}
```

**Response (pending):**
```json
{
  "error": "authorization_pending"
}
```

**Response (success):**
```json
{
  "access_token": "cinch_xxx...",
  "token_type": "Bearer",
  "user": "ehrlich-b"
}
```

## CLI Commands

### cinch login

Authenticate the CLI with the server.

```bash
$ cinch login --server https://cinch.sh

Opening browser to authenticate...
Enter the code shown: CINCH-1234

Waiting for authorization...
Logged in as ehrlich-b
Credentials saved to ~/.cinch/config
```

### cinch logout

Remove stored credentials.

```bash
$ cinch logout
Logged out of https://cinch.sh
```

### cinch whoami

Show current authentication status.

```bash
$ cinch whoami
Logged in as ehrlich-b at https://cinch.sh
```

## Worker Self-Registration

Once the CLI is authenticated, the worker can self-register:

```bash
$ cinch worker --server https://cinch.sh

Using credentials from ~/.cinch/config
Registering worker...
Worker registered: w_abc123
Connected, waiting for jobs...
```

The worker flow:
1. Read credentials from `~/.cinch/config`
2. Connect to WebSocket with Bearer token in header
3. Server validates token, creates worker record, accepts connection
4. Worker receives jobs as normal

## Config File

`~/.cinch/config` (TOML):

```toml
[servers.default]
url = "https://cinch.sh"
token = "cinch_xxx..."
user = "ehrlich-b"
```

Multiple servers can be configured:

```toml
[servers.default]
url = "https://cinch.sh"
token = "cinch_xxx..."

[servers.work]
url = "https://cinch.internal.corp"
token = "cinch_yyy..."
```

## Token Types

1. **User tokens** - Issued via device auth flow, tied to GitHub user, used for CLI operations
2. **Worker tokens** - Created via API (or CLI), used for worker connections, can be revoked

User tokens can create worker tokens. Worker tokens can only be used for worker connections.

## CLI Repo Management

With CLI auth, adding repos becomes:

```bash
$ cinch repo add ehrlich-b/cinch
Added repo ehrlich-b/cinch
Webhook URL: https://cinch.sh/webhooks
Webhook secret: whsec_xxx...

Configure webhook in GitHub:
  URL: https://cinch.sh/webhooks
  Secret: whsec_xxx...
  Events: push
```

The server uses the user's GitHub OAuth token (from login) to:
1. Verify repo access
2. Optionally create the webhook automatically

## Implementation Plan

### Phase 1: Device Auth (MVP)
- [ ] `internal/server/device.go` - Device code storage and endpoints
- [ ] `POST /auth/device` - Generate device code
- [ ] `GET /auth/device/verify` - Browser verification page
- [ ] `POST /auth/device/token` - Token polling endpoint
- [ ] `cmd/cinch/login.go` - `cinch login` command
- [ ] Config file handling (`~/.cinch/config`)

### Phase 2: Worker Self-Registration
- [ ] Update `cinch worker` to use config credentials
- [ ] Bearer token auth on WebSocket connection
- [ ] Auto-create worker record on first connection

### Phase 3: CLI Repo Management
- [ ] `cinch repo add <owner/name>` - Add repo
- [ ] `cinch repo list` - List repos
- [ ] `cinch repo remove <owner/name>` - Remove repo
- [ ] Store user's GitHub token for API operations

## Security Considerations

1. **Device codes expire** - 15 minute TTL, single use
2. **Tokens are scoped** - User tokens vs worker tokens
3. **Config file permissions** - `chmod 600 ~/.cinch/config`
4. **Token revocation** - Users can revoke tokens via web UI
5. **Short user codes** - 8 chars, easy to type, hard to guess (rate limited)
