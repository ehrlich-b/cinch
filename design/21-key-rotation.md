# Key Rotation

## Overview

Cinch uses a single root secret (`CINCH_SECRET_KEY`) for:
1. **JWT signing** - User sessions, worker auth tokens
2. **Encryption at rest** - Forge tokens, webhook secrets, user credentials
3. **Admin token derivation** - Deterministic admin token for self-hosted servers

This document describes how to rotate this key without losing encrypted data.

## Environment Variables

```bash
CINCH_SECRET_KEY          # Primary key (required)
CINCH_SECRET_KEY_SECONDARY  # Secondary key for rotation (optional)

# Deprecated (backwards compatible)
CINCH_JWT_SECRET          # Falls back if CINCH_SECRET_KEY not set
```

## Key Canary

A "canary" value validates that the encryption key is correct before any decryption attempts. This prevents garbage output from wrong-key decryption.

```sql
CREATE TABLE key_canary (
    id INTEGER PRIMARY KEY CHECK (id = 1),  -- Only one row allowed
    encrypted_value TEXT NOT NULL,          -- encrypt("cinch-canary-v1")
    key_version INTEGER NOT NULL DEFAULT 1, -- Which key encrypted this
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

**Canary plaintext:** `cinch-canary-v1` (constant, never changes)

On startup:
1. If no canary exists → create one encrypted with primary key
2. If canary exists → decrypt and verify it equals `cinch-canary-v1`
3. If decryption fails → try secondary key (rotation mode)

## Rotation State Machine

```
┌─────────────────────────────────────────────────────────────┐
│                     NORMAL OPERATION                        │
│  CINCH_SECRET_KEY = "key_A"                                 │
│  CINCH_SECRET_KEY_SECONDARY = (not set)                     │
│                                                             │
│  • Decrypt with key_A                                       │
│  • Encrypt with key_A                                       │
│  • Sign JWTs with key_A                                     │
└─────────────────────────────────────────────────────────────┘
                            │
                            │ User sets CINCH_SECRET_KEY_SECONDARY="key_B"
                            │ (App restart)
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                     ROTATION MODE                           │
│  CINCH_SECRET_KEY = "key_A"                                 │
│  CINCH_SECRET_KEY_SECONDARY = "key_B"                       │
│                                                             │
│  Startup:                                                   │
│  1. Try decrypt canary with key_A → SUCCESS                 │
│  2. Detect secondary key is set → ROTATING                  │
│  3. Re-encrypt all secrets with key_B                       │
│  4. Update canary to key_B                                  │
│  5. Log: "Rotation complete, update env vars"               │
│                                                             │
│  Runtime:                                                   │
│  • Decrypt: try key_B first, fall back to key_A             │
│  • Encrypt: always use key_B                                │
│  • Sign JWTs: use key_B                                     │
│  • Validate JWTs: accept both key_A and key_B               │
└─────────────────────────────────────────────────────────────┘
                            │
                            │ User swaps keys:
                            │   CINCH_SECRET_KEY="key_B"
                            │   CINCH_SECRET_KEY_SECONDARY=(unset)
                            │ (App restart)
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                     POST-ROTATION                           │
│  CINCH_SECRET_KEY = "key_B"                                 │
│  CINCH_SECRET_KEY_SECONDARY = (not set)                     │
│                                                             │
│  Startup:                                                   │
│  1. Try decrypt canary with key_B → SUCCESS                 │
│  2. No secondary key → normal operation                     │
│                                                             │
│  Back to normal operation with new key.                     │
└─────────────────────────────────────────────────────────────┘
```

## Recovery Mode

If the primary key is lost (our current situation):

```
┌─────────────────────────────────────────────────────────────┐
│                     RECOVERY MODE                           │
│  CINCH_SECRET_KEY = "key_A" (unknown/lost)                  │
│  CINCH_SECRET_KEY_SECONDARY = "key_B" (new, known)          │
│                                                             │
│  Startup:                                                   │
│  1. Try decrypt canary with key_A → SUCCESS                 │
│     (key_A still works even if we don't know it)            │
│  2. Detect secondary key is set → ROTATING                  │
│  3. Re-encrypt all secrets with key_B                       │
│  4. Update canary to key_B                                  │
│  5. Log: "Rotation complete"                                │
│                                                             │
│  After restart with swapped keys, key_B is primary.         │
│  The unknown key_A is no longer needed.                     │
└─────────────────────────────────────────────────────────────┘
```

## What Gets Re-encrypted

During rotation, the following are re-encrypted with the new key:

| Table | Column | Description |
|-------|--------|-------------|
| `repos` | `webhook_secret` | Webhook signature validation |
| `repos` | `forge_token` | Org-level PAT for API calls |
| `repos` | `secrets` | User-defined CI secrets (JSON) |
| `users` | `gitlab_credentials` | GitLab OAuth tokens |
| `users` | `forgejo_credentials` | Forgejo OAuth tokens |
| `key_canary` | `encrypted_value` | Canary value |

## What Gets Invalidated

These are **not** re-encrypted and will require users to re-authenticate:

- **JWTs** - User sessions, worker tokens
- **Admin token** - Derived from secret, changes automatically

This is acceptable because:
- Users can re-login (minor inconvenience)
- Workers reconnect automatically
- No data is lost

## Implementation

### Startup Sequence

```go
func (s *Storage) validateAndRotateKeys() error {
    // 1. Try primary key
    canary, err := s.getCanary()
    if err == ErrNotFound {
        // No canary - create one with primary key
        return s.createCanary()
    }

    primaryWorks := s.tryDecryptCanary(canary, s.primaryCipher)

    if primaryWorks {
        if s.secondaryCipher != nil {
            // Rotation mode: re-encrypt everything
            return s.rotateToSecondary()
        }
        // Normal operation
        return nil
    }

    // Primary failed - try secondary
    if s.secondaryCipher != nil {
        secondaryWorks := s.tryDecryptCanary(canary, s.secondaryCipher)
        if secondaryWorks {
            // Secondary is now primary (rotation already happened)
            s.primaryCipher = s.secondaryCipher
            s.secondaryCipher = nil
            s.log.Warn("using secondary key as primary - update your env vars")
            return nil
        }
    }

    return errors.New("neither primary nor secondary key can decrypt data")
}
```

### Re-encryption

```go
func (s *Storage) rotateToSecondary() error {
    s.log.Info("starting key rotation")

    // Re-encrypt repos
    repos, _ := s.listReposRaw() // Get encrypted values
    for _, repo := range repos {
        // Decrypt with primary
        webhookSecret, _ := s.primaryCipher.Decrypt(repo.WebhookSecret)
        forgeToken, _ := s.primaryCipher.Decrypt(repo.ForgeToken)
        secrets, _ := s.primaryCipher.Decrypt(repo.Secrets)

        // Re-encrypt with secondary
        repo.WebhookSecret, _ = s.secondaryCipher.Encrypt(webhookSecret)
        repo.ForgeToken, _ = s.secondaryCipher.Encrypt(forgeToken)
        repo.Secrets, _ = s.secondaryCipher.Encrypt(secrets)

        s.updateRepoSecrets(repo)
    }

    // Re-encrypt users
    users, _ := s.listUsersRaw()
    for _, user := range users {
        // ... same pattern
    }

    // Update canary
    s.updateCanary(s.secondaryCipher)

    s.log.Info("key rotation complete - swap CINCH_SECRET_KEY and remove CINCH_SECRET_KEY_SECONDARY")
    return nil
}
```

## Operator Runbook

### Scheduled Rotation

```bash
# 1. Generate new key and SAVE IT
NEW_KEY=$(openssl rand -hex 32)
echo "NEW KEY: $NEW_KEY"  # Save this somewhere safe!

# 2. Set as secondary (triggers rotation on restart)
fly secrets set CINCH_SECRET_KEY_SECONDARY=$NEW_KEY

# 3. Watch logs for "key rotation complete"
fly logs

# 4. Swap keys
fly secrets set CINCH_SECRET_KEY=$NEW_KEY
fly secrets unset CINCH_SECRET_KEY_SECONDARY
```

### Emergency Recovery (Lost Primary)

```bash
# 1. Generate new key and SAVE IT
NEW_KEY=$(openssl rand -hex 32)
echo "NEW KEY: $NEW_KEY"  # Save this somewhere safe!

# 2. Set as secondary
# The old primary is still in CINCH_SECRET_KEY (we just don't know it)
fly secrets set CINCH_SECRET_KEY_SECONDARY=$NEW_KEY

# 3. App restarts, rotation happens automatically
fly logs  # Watch for "key rotation complete"

# 4. Swap keys
fly secrets set CINCH_SECRET_KEY=$NEW_KEY
fly secrets unset CINCH_SECRET_KEY_SECONDARY
```

## Security Considerations

1. **Never log keys** - Only log that rotation happened, never the key values
2. **Atomic operations** - Re-encryption should be all-or-nothing per row
3. **Backup before rotation** - Operators should backup the database first
4. **Key storage** - Operators are responsible for storing keys securely
5. **No key in database** - The key is never stored, only used to derive encryption

## Future Improvements

1. **Key versioning** - Store which key version encrypted each value (currently we try both)
2. **Automatic rotation** - Scheduled rotation without manual intervention
3. **HSM support** - For high-security deployments
