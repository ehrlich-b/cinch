# Multi-Node Architecture

Cinch supports horizontal scaling via the "login node" pattern from tunn. One node owns the SQLite database; other nodes proxy DB operations to it via gRPC.

## Why This Pattern

**Problem:** SQLite doesn't support multiple writers. Postgres would solve this but adds operational complexity.

**Solution:** Embrace single-writer. One "login/db node" owns SQLite. Scale by adding stateless web nodes that proxy DB calls.

**Scaling path:**
1. Start with 1 node (login node) - handles everything
2. Add web nodes as traffic grows - they proxy DB to login node
3. Stop sending web traffic to login node - it becomes pure DB
4. If you outgrow this, swap in PostgresStorage (or hire someone)

This scales to thousands of concurrent workers before you need anything fancier.

## Database Flexibility

The `Storage` interface abstracts the database. Implementations:

| Backend | Status | Use case |
|---------|--------|----------|
| `SQLiteStorage` | v0.1 (free) | Self-hosted, single-node, zero config |
| `ProxyStorage` | v0.2 | Multi-node (proxies to login node) |
| `PostgresStorage` | Paid support | Multi-writer, hosted/enterprise |
| `MySQLStorage` | Paid support | If someone pays for it |

SQLite is the default and ships free. Other backends are a paid support/consulting conversation - just implement the interface.

## Architecture

```
                              Load Balancer
                                   │
                +──────────────────┼──────────────────+
                │                  │                  │
           ┌─────────┐        ┌─────────┐        ┌─────────┐
           │ Node 1  │        │ Node 2  │        │ Node 3  │
           │ (login) │◄─gRPC──│ (web)   │        │ (web)   │
           │ SQLite  │◄─gRPC──│         │        │         │
           └─────────┘        └─────────┘        └─────────┘
                │                  │                  │
                │ WebSocket        │ WebSocket        │ WebSocket
                │                  │                  │
           [Workers A]        [Workers B]        [Workers C]
```

## Storage Interface

All database operations go through a unified `Storage` interface:

```go
// internal/storage/storage.go
type Storage interface {
    // Jobs
    CreateJob(ctx context.Context, job *Job) error
    GetJob(ctx context.Context, id string) (*Job, error)
    ListJobs(ctx context.Context, opts ListJobsOpts) ([]*Job, error)
    UpdateJobStatus(ctx context.Context, id string, status JobStatus) error

    // Workers (persistent registration, not live connections)
    CreateWorker(ctx context.Context, worker *Worker) error
    GetWorker(ctx context.Context, id string) (*Worker, error)
    ListWorkers(ctx context.Context) ([]*Worker, error)
    UpdateWorkerLastSeen(ctx context.Context, id string) error

    // Repos
    CreateRepo(ctx context.Context, repo *Repo) error
    GetRepo(ctx context.Context, id string) (*Repo, error)
    GetRepoByCloneURL(ctx context.Context, url string) (*Repo, error)

    // Tokens
    CreateToken(ctx context.Context, token *Token) error
    ValidateToken(ctx context.Context, hash string) (*Token, error)
    RevokeToken(ctx context.Context, id string) error

    // Logs
    AppendLog(ctx context.Context, jobID string, stream string, data []byte) error
    GetLogs(ctx context.Context, jobID string) ([]LogChunk, error)

    // Health
    Available() bool
}
```

Two implementations:
- `LocalStorage` - direct SQLite (login node)
- `ProxyStorage` - gRPC proxy to login node (web nodes)

## Worker Registry (Live Connections)

The Storage interface tracks persistent worker info (labels, tokens). But *live* WebSocket connections are tracked separately in memory:

```go
// internal/server/hub.go
type Hub struct {
    mu       sync.RWMutex
    workers  map[string]*WorkerConn  // worker_id -> connection
    storage  Storage                  // for DB operations
}

type WorkerConn struct {
    ID       string
    Labels   []string
    Conn     *websocket.Conn
    Jobs     map[string]*RunningJob  // active jobs on this worker
    LastPing time.Time
}
```

**Single-node:** Hub is the source of truth for live workers.

**Multi-node:** Each node has its own Hub tracking local connections. Job dispatch asks all nodes for available workers via gRPC.

## Multi-Node Worker Discovery

When dispatching a job, the server needs to find workers across all nodes:

```protobuf
// proto/internal.proto
service InternalService {
    // Find workers with matching labels across the cluster
    rpc GetAvailableWorkers(GetWorkersRequest) returns (GetWorkersResponse);

    // Route a job to a specific worker on this node
    rpc RouteJob(RouteJobRequest) returns (RouteJobResponse);

    // Node info (is this the login node?)
    rpc GetNodeInfo(NodeInfoRequest) returns (NodeInfoResponse);
}

message GetWorkersRequest {
    repeated string labels = 1;  // required labels (AND)
}

message GetWorkersResponse {
    repeated WorkerInfo workers = 1;
}

message WorkerInfo {
    string worker_id = 1;
    repeated string labels = 2;
    int32 active_jobs = 3;
    int32 max_jobs = 4;
    string node_address = 5;  // which node has this worker's WebSocket
}
```

**Dispatch algorithm:**
1. Server receives webhook, creates job
2. Server calls `GetAvailableWorkers` on all nodes (including itself)
3. Collects all workers matching job's label requirements
4. Picks least-loaded worker
5. If worker is local: send job via local WebSocket
6. If worker is remote: call `RouteJob` on that node

## Login Node Determination

```go
func IsLoginNode() bool {
    // Explicit env var
    if os.Getenv("CINCH_LOGIN_NODE") == "true" {
        return true
    }
    // Fly.io process group
    if os.Getenv("FLY_PROCESS_GROUP") == "login" {
        return true
    }
    // Single-node default
    if os.Getenv("CINCH_NODE_ADDRESSES") == "" {
        return true  // no other nodes configured
    }
    return false
}
```

## Node-to-Node Authentication

All internal gRPC requires a shared secret:

```bash
export CINCH_NODE_SECRET=your-secret-here
```

Passed as gRPC metadata, verified on every call.

## Graceful Degradation

If login node is unreachable:
- **Job dispatch:** Continue with locally-connected workers
- **Log writes:** Buffer locally, flush when connection restored
- **Status updates:** Queue and retry
- **New repos/tokens:** Fail (admin operations can wait)

Workers keep running. Jobs keep executing. Only admin/dashboard features degrade.

## v0.1 Scope

For v0.1, implement single-node only:
- `LocalStorage` with SQLite
- In-memory `Hub` for worker connections
- No gRPC internal service
- No `ProxyStorage`

The interface is designed so multi-node can be added later without changing the rest of the codebase.

## Configuration

**Single-node (v0.1):**
```bash
./cinch server --addr :8080 --data-dir /var/lib/cinch
```

**Multi-node (future):**
```bash
# Login node
CINCH_LOGIN_NODE=true \
CINCH_NODE_SECRET=secret \
CINCH_NODE_ADDRESSES=node2:50051,node3:50051 \
./cinch server

# Web nodes
CINCH_NODE_SECRET=secret \
CINCH_NODE_ADDRESSES=node1:50051,node3:50051 \
./cinch server
```
