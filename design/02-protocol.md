# Protocol Specification

## Overview

Communication between server and workers uses WebSocket with JSON messages. Workers connect outbound to the server (NAT-friendly, no firewall holes needed).

## Connection Lifecycle

```
Worker                                    Server
  │                                         │
  │──────── WebSocket Connect ─────────────►│
  │         /ws?token=xxx                   │
  │                                         │
  │◄─────── AUTH_OK ───────────────────────│
  │         {worker_id, server_version}     │
  │                                         │
  │──────── REGISTER ──────────────────────►│
  │         {labels, capabilities}          │
  │                                         │
  │◄─────── REGISTERED ────────────────────│
  │                                         │
  │         ... connection established ...  │
  │                                         │
  │◄─────── JOB_ASSIGN ────────────────────│
  │         {job_id, repo, commit, cmd}     │
  │                                         │
  │──────── JOB_ACK ───────────────────────►│
  │         {job_id}                        │
  │                                         │
  │──────── LOG_CHUNK ─────────────────────►│
  │         {job_id, data, stream}          │  (repeats)
  │                                         │
  │──────── JOB_COMPLETE ──────────────────►│
  │         {job_id, exit_code, duration}   │
  │                                         │
  │◄─────── ACK ───────────────────────────│
  │                                         │
  │         ... wait for next job ...       │
  │                                         │
  │──────── PING ──────────────────────────►│
  │◄─────── PONG ──────────────────────────│
  │                                         │
```

## Message Format

All messages are JSON with a `type` field:

```json
{
  "type": "MESSAGE_TYPE",
  "payload": { ... }
}
```

## Message Types

### Server → Worker

#### `AUTH_OK`

Sent after successful WebSocket connection with valid token.

```json
{
  "type": "AUTH_OK",
  "payload": {
    "worker_id": "w_abc123",
    "server_version": "0.1.0"
  }
}
```

#### `AUTH_FAIL`

Sent if token is invalid. Connection closed after.

```json
{
  "type": "AUTH_FAIL",
  "payload": {
    "error": "invalid or expired token"
  }
}
```

#### `REGISTERED`

Acknowledges worker registration.

```json
{
  "type": "REGISTERED",
  "payload": {
    "worker_id": "w_abc123"
  }
}
```

#### `JOB_ASSIGN`

Assigns a job to the worker.

```json
{
  "type": "JOB_ASSIGN",
  "payload": {
    "job_id": "j_xyz789",
    "repo": {
      "clone_url": "https://github.com/user/repo.git",
      "clone_token": "ghs_shortlived...",
      "commit": "abc1234567890",
      "branch": "main",
      "is_pr": false,
      "pr_number": null
    },
    "config": {
      "command": "make ci",
      "timeout": "30m",
      "env": {
        "CI": "true",
        "CINCH": "true"
      }
    }
  }
}
```

#### `JOB_CANCEL`

Cancels a running job.

```json
{
  "type": "JOB_CANCEL",
  "payload": {
    "job_id": "j_xyz789",
    "reason": "user requested cancellation"
  }
}
```

#### `PONG`

Response to worker's PING.

```json
{
  "type": "PONG",
  "payload": {
    "timestamp": 1705312800
  }
}
```

#### `ACK`

Generic acknowledgment.

```json
{
  "type": "ACK",
  "payload": {
    "ref": "j_xyz789"
  }
}
```

### Worker → Server

#### `REGISTER`

Sent after AUTH_OK to register worker capabilities.

```json
{
  "type": "REGISTER",
  "payload": {
    "labels": ["linux", "amd64", "docker"],
    "capabilities": {
      "docker": true,
      "concurrency": 2
    },
    "version": "0.1.0",
    "hostname": "build-server-1"
  }
}
```

#### `JOB_ACK`

Acknowledges receipt of job assignment.

```json
{
  "type": "JOB_ACK",
  "payload": {
    "job_id": "j_xyz789"
  }
}
```

#### `JOB_REJECT`

Worker rejects a job (e.g., busy, missing capability).

```json
{
  "type": "JOB_REJECT",
  "payload": {
    "job_id": "j_xyz789",
    "reason": "worker at max concurrency"
  }
}
```

#### `LOG_CHUNK`

Streams log output from running job.

```json
{
  "type": "LOG_CHUNK",
  "payload": {
    "job_id": "j_xyz789",
    "timestamp": 1705312800,
    "stream": "stdout",
    "data": "Running tests...\n"
  }
}
```

`stream` is `stdout` or `stderr`.

#### `JOB_STARTED`

Indicates job execution has begun.

```json
{
  "type": "JOB_STARTED",
  "payload": {
    "job_id": "j_xyz789",
    "timestamp": 1705312800
  }
}
```

#### `JOB_COMPLETE`

Job finished (success or failure).

```json
{
  "type": "JOB_COMPLETE",
  "payload": {
    "job_id": "j_xyz789",
    "exit_code": 0,
    "duration_ms": 45230,
    "timestamp": 1705312845
  }
}
```

#### `JOB_ERROR`

Job failed due to infrastructure error (not command failure).

```json
{
  "type": "JOB_ERROR",
  "payload": {
    "job_id": "j_xyz789",
    "error": "failed to clone: network timeout",
    "phase": "clone"
  }
}
```

`phase` can be: `clone`, `setup`, `execute`, `cleanup`

#### `PING`

Heartbeat from worker.

```json
{
  "type": "PING",
  "payload": {
    "timestamp": 1705312800,
    "active_jobs": ["j_xyz789"]
  }
}
```

#### `STATUS_UPDATE`

Worker reports its current status.

```json
{
  "type": "STATUS_UPDATE",
  "payload": {
    "active_jobs": 1,
    "max_jobs": 2,
    "available": true,
    "load": 0.45
  }
}
```

## Connection Management

### Authentication

1. Worker connects: `wss://server/ws?token=xxx`
2. Server validates token against database
3. If valid: send `AUTH_OK`, wait for `REGISTER`
4. If invalid: send `AUTH_FAIL`, close connection

### Heartbeat

- Worker sends `PING` every 30 seconds
- Server responds with `PONG`
- If no PING for 90 seconds, server marks worker offline
- If no PONG for 60 seconds, worker attempts reconnect

### Reconnection

On disconnect, worker:
1. Waits 1 second
2. Attempts reconnect
3. On failure, exponential backoff (1s, 2s, 4s, 8s, max 60s)
4. On success, re-registers with any in-progress jobs

### Job Recovery

If connection drops during job execution:
1. Worker completes job locally
2. Worker reconnects
3. Worker re-sends `JOB_COMPLETE` or `JOB_ERROR`
4. Server is idempotent (ignores duplicate completions)

## Security Considerations

### Token Scoping

Tokens should have minimal scope:
- Worker tokens: can only receive jobs, send logs
- Admin tokens: can create workers, view all jobs

### Clone Token Security

- Clone tokens are short-lived (1 hour)
- Generated per-job by server
- Scoped to specific repo (where possible)
- Never stored in database (generated on demand)

### Message Size Limits

- Max message size: 1MB
- Log chunks: max 64KB each
- If log chunk exceeds limit, split and send multiple

## Wire Format

### WebSocket Frames

- Text frames only (no binary)
- Each frame is one JSON message
- No compression at protocol level (rely on TLS/WebSocket compression)

### Example Session

```
C: [WebSocket Connect /ws?token=tok_abc123]
S: {"type":"AUTH_OK","payload":{"worker_id":"w_1","server_version":"0.1.0"}}
C: {"type":"REGISTER","payload":{"labels":["linux"],"capabilities":{},"version":"0.1.0"}}
S: {"type":"REGISTERED","payload":{"worker_id":"w_1"}}
... time passes ...
S: {"type":"JOB_ASSIGN","payload":{"job_id":"j_1","repo":{...},"config":{...}}}
C: {"type":"JOB_ACK","payload":{"job_id":"j_1"}}
C: {"type":"JOB_STARTED","payload":{"job_id":"j_1","timestamp":1705312800}}
C: {"type":"LOG_CHUNK","payload":{"job_id":"j_1","stream":"stdout","data":"$ make ci\n"}}
C: {"type":"LOG_CHUNK","payload":{"job_id":"j_1","stream":"stdout","data":"Running tests...\n"}}
C: {"type":"LOG_CHUNK","payload":{"job_id":"j_1","stream":"stdout","data":"OK\n"}}
C: {"type":"JOB_COMPLETE","payload":{"job_id":"j_1","exit_code":0,"duration_ms":5230}}
S: {"type":"ACK","payload":{"ref":"j_1"}}
```

## Future Considerations

### Compression

If log volume becomes problematic:
- Add `LOG_CHUNK_COMPRESSED` message type
- Use gzip for large chunks
- Negotiate compression in `REGISTER`

### Binary Protocol

If JSON overhead becomes problematic:
- Consider MessagePack or Protocol Buffers
- Negotiate in initial handshake
- Maintain JSON as fallback
