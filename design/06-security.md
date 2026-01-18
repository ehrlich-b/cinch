# Security Model

## Threat Model

### Assets to Protect

1. **Source code** - Cloned to workers for builds
2. **Secrets** - Environment variables on worker machines
3. **Forge credentials** - Tokens for posting status checks
4. **Build logs** - May contain sensitive output
5. **Control plane** - Coordinates everything

### Trust Boundaries

```
┌─────────────────────────────────────────────────────────────────────┐
│                         TRUSTED ZONE                                 │
│  ┌─────────────────┐      ┌─────────────────┐                       │
│  │     Worker      │      │     Worker      │  (User's machines)    │
│  │  • Source code  │      │  • Source code  │                       │
│  │  • Secrets      │      │  • Secrets      │                       │
│  │  • Build output │      │  • Build output │                       │
│  └────────┬────────┘      └────────┬────────┘                       │
│           │                        │                                 │
└───────────┼────────────────────────┼─────────────────────────────────┘
            │ WebSocket (TLS)        │
            │                        │
┌───────────┼────────────────────────┼─────────────────────────────────┐
│           │   SEMI-TRUSTED ZONE    │                                 │
│           ▼                        ▼                                 │
│  ┌─────────────────────────────────────────────┐                    │
│  │            Control Plane (Server)            │                    │
│  │  • Job metadata                              │                    │
│  │  • Logs (streamed through)                   │                    │
│  │  • Forge tokens (for status posting)         │                    │
│  └──────────────────────────────────────────────┘                    │
│                        │                                             │
└────────────────────────┼─────────────────────────────────────────────┘
                         │ HTTPS
                         ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      UNTRUSTED ZONE                                  │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐                           │
│  │  GitHub  │  │  GitLab  │  │ Forgejo  │  (Forges - send webhooks) │
│  └──────────┘  └──────────┘  └──────────┘                           │
└─────────────────────────────────────────────────────────────────────┘
```

### Threat Actors

1. **Malicious webhook sender** - Sends fake webhooks to trigger builds
2. **Compromised forge** - Forge sends malicious payloads
3. **Compromised control plane** - Attacker gains server access
4. **Malicious repository** - Repo contains code that tries to escape sandbox
5. **Network attacker** - MITM, traffic analysis

## Security Measures

### 1. Webhook Verification

Every webhook must be cryptographically verified:

```go
func (h *WebhookHandler) verifyGitHub(r *http.Request, secret string) error {
    sig := r.Header.Get("X-Hub-Signature-256")
    body, _ := io.ReadAll(r.Body)

    expected := "sha256=" + hmacSHA256(body, []byte(secret))
    if !hmac.Equal([]byte(sig), []byte(expected)) {
        return ErrInvalidSignature
    }
    return nil
}
```

- Each repo has unique webhook secret
- Secrets stored hashed or encrypted
- Reject requests without valid signatures

### 2. Clone Token Security

Workers never store forge credentials. Instead:

1. Server generates short-lived clone token (1 hour expiry)
2. Token scoped to specific repo when possible
3. Token transmitted over TLS WebSocket
4. Worker uses token once, then discards

```go
type CloneCredentials struct {
    URL       string    // https://x-access-token:TOKEN@github.com/user/repo.git
    ExpiresAt time.Time // 1 hour from now
}
```

### 3. Secrets Never Transit Control Plane

Build secrets (API keys, passwords) are environment variables on worker machines.

```
Worker machine:
  export AWS_ACCESS_KEY_ID=xxx
  export DATABASE_URL=xxx

cinch.toml:
  command = "make deploy"  # Uses $AWS_ACCESS_KEY_ID
```

The control plane never sees these values. It only sends:
- Clone URL (with short-lived token)
- Command to execute
- Repo-defined env vars from cinch.toml (non-sensitive)

### 4. Worker Authentication

Workers connect with bearer tokens:

```
WebSocket: wss://server/ws?token=tok_xxxxx
```

Token security:
- Tokens hashed with bcrypt in database
- Tokens can be scoped (single repo, all repos)
- Tokens can be revoked
- Tokens can have expiry dates

### 5. Build Isolation (Optional)

For untrusted code, workers can run builds in isolation:

#### Docker Isolation

```bash
cinch worker --docker
```

Builds run in ephemeral Docker containers:
- Fresh filesystem each build
- Network restrictions (optional)
- Resource limits (CPU, memory)
- Non-root user inside container

```go
func (e *DockerExecutor) Run(job *Job) error {
    return exec.Command("docker", "run",
        "--rm",
        "--network", "none",      // No network access
        "--memory", "2g",         // Memory limit
        "--cpus", "2",            // CPU limit
        "--user", "1000:1000",    // Non-root
        "-v", workdir+":/build",
        "-w", "/build",
        "cinch-builder:latest",
        "sh", "-c", job.Command,
    ).Run()
}
```

#### Bubblewrap Isolation (Linux)

Lighter than Docker:

```go
func (e *BubblewrapExecutor) Run(job *Job) error {
    return exec.Command("bwrap",
        "--unshare-net",          // No network
        "--unshare-pid",          // PID namespace
        "--ro-bind", "/usr", "/usr",
        "--ro-bind", "/lib", "/lib",
        "--bind", workdir, "/build",
        "--chdir", "/build",
        "--", "sh", "-c", job.Command,
    ).Run()
}
```

### 6. Transport Security

All connections use TLS:
- Server terminates TLS (or behind reverse proxy)
- Workers verify server certificate
- WebSocket uses `wss://`

### 7. Database Encryption

Sensitive fields encrypted at rest:

```go
// Encrypt with AES-256-GCM
func encryptField(key, plaintext []byte) ([]byte, error) {
    block, _ := aes.NewCipher(key)
    gcm, _ := cipher.NewGCM(block)
    nonce := make([]byte, gcm.NonceSize())
    io.ReadFull(rand.Reader, nonce)
    return gcm.Seal(nonce, nonce, plaintext, nil), nil
}
```

Encrypted fields:
- Forge access tokens
- Webhook secrets

Encryption key from environment variable, not database.

### 8. Rate Limiting

Prevent abuse:

```go
var webhookLimiter = rate.NewLimiter(rate.Limit(100), 200) // 100/sec, burst 200

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    if !webhookLimiter.Allow() {
        http.Error(w, "rate limited", http.StatusTooManyRequests)
        return
    }
    // ...
}
```

Rate limits:
- Webhooks: 100/sec per IP
- API: 1000/min per token
- WebSocket connections: 10/sec per IP

### 9. Input Validation

Validate all inputs:

```go
func validateConfig(c *Config) error {
    if c.Command == "" {
        return errors.New("command required")
    }
    if c.Timeout > 24*time.Hour {
        return errors.New("timeout too long")
    }
    // Don't allow shell metacharacters in certain fields
    if strings.ContainsAny(c.Command, "|;&`$") && !c.AllowShell {
        return errors.New("shell operators not allowed without allow_shell=true")
    }
    return nil
}
```

### 10. Logging and Audit

Log security-relevant events:

```go
type AuditEvent struct {
    Time     time.Time
    Event    string   // webhook_received, job_started, token_created, etc.
    Actor    string   // IP, token ID, or worker ID
    Resource string   // repo, job, worker
    Details  map[string]any
}
```

Events to log:
- Webhook received (valid/invalid)
- Job started/completed
- Worker connected/disconnected
- Token created/revoked
- Failed authentication attempts

## Attack Scenarios

### Scenario 1: Fake Webhook

**Attack:** Attacker sends POST to `/webhook/github` with forged payload.

**Mitigation:** Webhook signature verification. Without the secret, attacker cannot forge valid HMAC.

### Scenario 2: Stolen Worker Token

**Attack:** Attacker steals worker token, connects rogue worker.

**Mitigation:**
- Rogue worker can only receive jobs, not access secrets
- Secrets are on real worker's machine, not in job payload
- Monitor for multiple workers with same token (alert)
- Token revocation available

### Scenario 3: Malicious Repo Code

**Attack:** Repo contains code that reads `/etc/passwd` or installs rootkit.

**Mitigation:**
- Docker/bubblewrap isolation (optional)
- Builds run as non-root user
- Worker operator controls what runs (their machine, their responsibility)

### Scenario 4: Compromised Control Plane

**Attack:** Attacker gains access to server database.

**Mitigation:**
- Forge tokens encrypted at rest
- Worker tokens hashed (bcrypt)
- Build secrets never stored (they're on worker machines)
- Logs may leak info, but no credentials

### Scenario 5: Log Injection

**Attack:** Build output contains malicious content (XSS in web UI, or misleading log lines).

**Mitigation:**
- HTML-escape all log output in web UI
- Consider log sanitization (remove ANSI abuse)

## Deployment Recommendations

### Self-Hosted

1. Run server behind reverse proxy (nginx, caddy) with TLS
2. Use strong webhook secrets (32+ random bytes)
3. Restrict network access to server (VPN or IP allowlist)
4. Enable Docker isolation for untrusted repos
5. Regular backups of database

### Hosted Service

1. Multi-tenant isolation (database row-level security)
2. Rate limiting per account
3. SOC 2 Type II compliance (future)
4. Penetration testing (periodic)
5. Bug bounty program (future)

## Security Checklist

- [ ] All webhooks verified with HMAC
- [ ] Clone tokens short-lived (1 hour)
- [ ] Worker tokens hashed with bcrypt
- [ ] Forge tokens encrypted at rest
- [ ] TLS everywhere
- [ ] Rate limiting enabled
- [ ] Input validation on all fields
- [ ] Audit logging enabled
- [ ] Docker isolation documented
- [ ] Security headers on web UI (CSP, etc.)
