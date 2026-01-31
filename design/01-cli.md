# CLI Design

## Philosophy

One binary. Swiss army knife. Every subcommand is a different tool.

```
cinch <subcommand> [flags] [args]
```

## Subcommands

### `cinch server`

Run the control plane.

```bash
cinch server [flags]

Flags:
  --port, -p        Port to listen on (default: 8080)
  --host            Host to bind to (default: 0.0.0.0)
  --db              Database path/URL (default: ./cinch.db for SQLite)
  --db-type         Database type: sqlite, postgres (default: sqlite)
  --base-url        Public URL for webhooks (default: auto-detect)
  --log-level       Log level: debug, info, warn, error (default: info)
```

**Examples:**
```bash
# Development
cinch server

# Production (self-hosted)
cinch server --port 443 --base-url https://ci.example.com

# Production (Postgres)
cinch server --db-type postgres --db "postgres://user:pass@localhost/cinch"
```

### `cinch worker`

Run a worker that connects to a server.

```bash
cinch worker [flags]

Flags:
  --server, -s      Server URL (required, or set CINCH_SERVER)
  --token, -t       Auth token (required, or set CINCH_TOKEN)
  --labels, -l      Comma-separated labels (default: os/arch auto-detected)
  --workdir         Working directory for builds (default: ~/.cinch/builds)
  --concurrency     Max concurrent jobs (default: 1)
  --bare-metal      Run builds directly on host instead of containers (default: false)
  --log-level       Log level: debug, info, warn, error (default: info)
```

**Examples:**
```bash
# Basic
cinch worker --server https://cinch.sh --token abc123

# With labels
cinch worker -s https://cinch.sh -t abc123 -l linux,amd64,gpu

# Bare metal (no container isolation)
cinch worker -s https://cinch.sh -t abc123 --bare-metal

# Multiple concurrent jobs
cinch worker -s https://cinch.sh -t abc123 --concurrency 4
```

### `cinch run`

Run a command locally, simulating CI. Useful for testing your CI config.

```bash
cinch run [command] [flags]

Flags:
  --config, -c      Config file (default: ./.cinch.yaml)
  --env, -e         Additional env vars (can repeat)
```

**Examples:**
```bash
# Run command from .cinch.yaml
cinch run

# Run specific command
cinch run "make test"

# With extra env vars
cinch run -e DEBUG=1 -e VERBOSE=true
```

**Behavior:**
- Reads `.cinch.yaml` if no command specified
- Sets `CI=true` and `CINCH=true` env vars
- Streams output to terminal
- Exits with command's exit code

### `cinch status`

Check build status for a repo.

```bash
cinch status [flags]

Flags:
  --server, -s      Server URL (default: CINCH_SERVER env)
  --repo, -r        Repo (default: current git remote)
  --branch, -b      Branch (default: current branch)
  --json            Output as JSON
```

**Examples:**
```bash
# Current repo/branch
cinch status

# Specific branch
cinch status -b main

# JSON output for scripting
cinch status --json
```

**Output:**
```
repo:   github.com/user/myproject
branch: feature/cool-thing
commit: abc1234

latest build:
  id:      42
  status:  success
  time:    2m 34s
  build: make check
```

### `cinch logs`

Stream or view logs from a build.

```bash
cinch logs <job-id> [flags]

Flags:
  --server, -s      Server URL (default: CINCH_SERVER env)
  --follow, -f      Follow log output (default: false)
  --tail, -n        Number of lines from end (default: all)
```

**Examples:**
```bash
# View all logs
cinch logs 42

# Follow in real-time
cinch logs 42 -f

# Last 100 lines
cinch logs 42 -n 100
```

### `cinch config`

Validate and display parsed config.

```bash
cinch config [flags]

Flags:
  --file, -f        Config file (default: ./.cinch.yaml)
```

**Examples:**
```bash
# Validate current config
cinch config

# Validate specific file
cinch config -f /path/to/.cinch.yaml
```

**Output (success):**
```
.cinch.yaml is valid

build: make check
timeout: 30m
triggers:
  branches: [main, develop]
  pull_requests: true
runner:
  labels: [linux, amd64]
```

**Output (error):**
```
.cinch.yaml:12: unknown field 'biuld' (did you mean 'build'?)
```

### `cinch token`

Manage authentication tokens.

```bash
cinch token <subcommand>

Subcommands:
  create      Create a new worker token
  list        List tokens
  revoke      Revoke a token
```

**Examples:**
```bash
# Create token (when running server)
cinch token create --name "macbook-worker"

# List tokens
cinch token list

# Revoke
cinch token revoke abc123
```

### `cinch version`

Print version info.

```bash
cinch version

Output:
cinch v0.1.0
commit: abc1234
built:  2024-01-15T10:30:00Z
go:     go1.22.0
```

## Global Flags

```bash
--help, -h          Show help
--version, -v       Show version
--config            Global config file (default: ~/.cinch/config.toml)
```

## Environment Variables

See `CLAUDE.md` for the complete list of server and job environment variables.

**Note:** The CLI uses `~/.cinch/config` for credentials (saved by `cinch login`), not environment variables.

## Config File

Optional global config at `~/.cinch/config.toml`:

```toml
# Default server for CLI commands
server = "https://cinch.sh"

# Default token (careful with permissions!)
# token = "xxx"

[log]
level = "info"
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Config error |
| 3 | Connection error |
| 4 | Auth error |
| 5+ | Command exit code (for `cinch run`) |

## Implementation Notes

### Subcommand Routing

Use cobra or similar:

```go
func main() {
    root := &cobra.Command{Use: "cinch"}
    root.AddCommand(
        serverCmd(),
        workerCmd(),
        runCmd(),
        statusCmd(),
        logsCmd(),
        configCmd(),
        tokenCmd(),
        versionCmd(),
    )
    root.Execute()
}
```

### Shared Client

CLI commands that talk to server share an HTTP/WebSocket client:

```go
// internal/client/client.go
type Client struct {
    baseURL string
    token   string
    http    *http.Client
}

func (c *Client) GetJob(id int) (*Job, error)
func (c *Client) StreamLogs(id int, w io.Writer) error
func (c *Client) GetStatus(repo, branch string) (*Status, error)
```

### Output Formatting

- Default: human-readable, colored (if TTY)
- `--json`: machine-readable JSON
- Respect `NO_COLOR` env var
