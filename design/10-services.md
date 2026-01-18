# Service Containers

## Overview

Many builds need backing services: databases, caches, message queues. Instead of requiring users to pre-provision these, cinch spins up service containers alongside the build.

## The Config

```yaml
# .cinch.yaml
command: make test

services:
  postgres:
    image: postgres:16
    env:
      POSTGRES_PASSWORD: postgres
      POSTGRES_DB: test
    ports:
      - 5432:5432
    healthcheck:
      cmd: pg_isready
      interval: 5s
      timeout: 3s
      retries: 5

  redis:
    image: redis:7-alpine
    ports:
      - 6379:6379

  minio:
    image: minio/minio
    command: server /data
    env:
      MINIO_ROOT_USER: minioadmin
      MINIO_ROOT_PASSWORD: minioadmin
    ports:
      - 9000:9000
```

## How It Works

### Lifecycle

```
Job starts
    │
    ▼
1. Create network for this job (isolated)
    │
    ▼
2. Start service containers (in parallel)
    │
    ▼
3. Wait for all services healthy (or timeout)
    │
    ▼
4. Start build container (connected to same network)
    │
    ▼
5. Run command
    │
    ▼
6. Command exits
    │
    ▼
7. Stop and remove all containers
    │
    ▼
8. Remove network
```

### Networking

All containers (services + build) share a Docker network created for the job:

```
┌─────────────────────────────────────────────────────────────┐
│                  cinch-job-{job_id} network                  │
│                                                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │   postgres   │  │    redis     │  │    build     │       │
│  │  :5432       │  │   :6379      │  │  (your cmd)  │       │
│  └──────────────┘  └──────────────┘  └──────────────┘       │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

Services are accessible by name: `postgres:5432`, `redis:6379`.

### Health Checks

Services must be healthy before the build starts. Options:

**Built-in healthcheck (from image):**
```yaml
services:
  postgres:
    image: postgres:16
    # Uses image's HEALTHCHECK instruction
```

**Custom healthcheck:**
```yaml
services:
  postgres:
    image: postgres:16
    healthcheck:
      cmd: pg_isready -U postgres
      interval: 5s
      timeout: 3s
      retries: 10
```

**No healthcheck (start immediately):**
```yaml
services:
  redis:
    image: redis:7-alpine
    # Redis starts fast, no healthcheck needed
```

### Timeout

If services don't become healthy within timeout, job fails:

```yaml
services:
  postgres:
    image: postgres:16
    healthcheck:
      cmd: pg_isready
      timeout: 30s  # Give up after 30s total
```

Default timeout: 60 seconds for all services combined.

## Implementation

### Service Container Creation

```go
type Service struct {
    Name        string
    Image       string
    Env         map[string]string
    Ports       []string
    Command     string
    Healthcheck *Healthcheck
}

func (w *Worker) startServices(job *Job, network string) ([]string, error) {
    var containerIDs []string
    var wg sync.WaitGroup
    errChan := make(chan error, len(job.Services))

    for name, svc := range job.Services {
        wg.Add(1)
        go func(name string, svc Service) {
            defer wg.Done()

            id, err := w.docker.CreateContainer(ContainerConfig{
                Name:    fmt.Sprintf("cinch-%s-%s", job.ID, name),
                Image:   svc.Image,
                Env:     svc.Env,
                Network: network,
                Aliases: []string{name}, // Accessible as "postgres", "redis", etc.
            })
            if err != nil {
                errChan <- err
                return
            }

            containerIDs = append(containerIDs, id)

            if err := w.waitHealthy(id, svc.Healthcheck); err != nil {
                errChan <- fmt.Errorf("service %s: %w", name, err)
            }
        }(name, svc)
    }

    wg.Wait()
    close(errChan)

    for err := range errChan {
        return containerIDs, err // Return first error
    }

    return containerIDs, nil
}
```

### Health Check Polling

```go
func (w *Worker) waitHealthy(containerID string, hc *Healthcheck) error {
    if hc == nil {
        return nil // No healthcheck, assume ready
    }

    deadline := time.Now().Add(hc.Timeout)
    ticker := time.NewTicker(hc.Interval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            if time.Now().After(deadline) {
                return errors.New("healthcheck timeout")
            }

            healthy, err := w.checkHealth(containerID, hc.Cmd)
            if err != nil {
                log.Debug("healthcheck failed: %v", err)
                continue
            }
            if healthy {
                return nil
            }

        case <-w.ctx.Done():
            return w.ctx.Err()
        }
    }
}

func (w *Worker) checkHealth(containerID string, cmd string) (bool, error) {
    exec, err := w.docker.CreateExec(containerID, []string{"sh", "-c", cmd})
    if err != nil {
        return false, err
    }

    exitCode, err := w.docker.StartExec(exec)
    if err != nil {
        return false, err
    }

    return exitCode == 0, nil
}
```

### Cleanup

Always clean up, even on failure:

```go
func (w *Worker) runJob(job *Job) error {
    // Create network
    network, err := w.docker.CreateNetwork(fmt.Sprintf("cinch-%s", job.ID))
    if err != nil {
        return err
    }
    defer w.docker.RemoveNetwork(network)

    // Start services
    serviceIDs, err := w.startServices(job, network)
    defer func() {
        for _, id := range serviceIDs {
            w.docker.StopContainer(id, 10*time.Second)
            w.docker.RemoveContainer(id)
        }
    }()
    if err != nil {
        return fmt.Errorf("services failed: %w", err)
    }

    // Run build
    return w.runBuild(job, network)
}
```

## Resource Limits

Services can have resource limits:

```yaml
services:
  postgres:
    image: postgres:16
    resources:
      memory: 2g
      cpus: 2
      shm_size: 1g  # Shared memory (important for postgres)
```

```go
hostConfig := &container.HostConfig{
    Resources: container.Resources{
        Memory:   2 * 1024 * 1024 * 1024,
        NanoCPUs: 2 * 1000000000,
    },
    ShmSize: 1 * 1024 * 1024 * 1024,
}
```

## Port Mapping

Ports are mapped to localhost for the build container:

```yaml
services:
  postgres:
    ports:
      - 5432:5432  # host:container
```

Since all containers are on the same network, the build can access `postgres:5432` directly. Port mapping is optional but useful for:
- Debugging (connect from worker host)
- Compatibility with code expecting `localhost:5432`

## Environment Variables

Cinch injects service connection info as env vars:

```bash
# Automatically set based on services config
CINCH_SERVICE_POSTGRES_HOST=postgres
CINCH_SERVICE_POSTGRES_PORT=5432
CINCH_SERVICE_REDIS_HOST=redis
CINCH_SERVICE_REDIS_PORT=6379
```

Or the build can just use the service name directly since they're on the same network.

## Common Patterns

### PostgreSQL with Custom Config

```yaml
services:
  postgres:
    image: postgres:16
    env:
      POSTGRES_PASSWORD: postgres
      POSTGRES_DB: myapp_test
      POSTGRES_INITDB_ARGS: >-
        -c max_connections=100
        -c shared_buffers=256MB
    resources:
      shm_size: 256m
    healthcheck:
      cmd: pg_isready -U postgres
```

### MySQL

```yaml
services:
  mysql:
    image: mysql:8
    env:
      MYSQL_ROOT_PASSWORD: root
      MYSQL_DATABASE: test
    healthcheck:
      cmd: mysqladmin ping -h localhost
```

### Redis with Persistence

```yaml
services:
  redis:
    image: redis:7-alpine
    command: redis-server --appendonly yes
```

### Elasticsearch

```yaml
services:
  elasticsearch:
    image: elasticsearch:8.11.0
    env:
      discovery.type: single-node
      xpack.security.enabled: false
    resources:
      memory: 2g
    healthcheck:
      cmd: curl -f http://localhost:9200/_cluster/health
      interval: 10s
      timeout: 60s
```

### Docker-in-Docker

```yaml
services:
  docker:
    image: docker:24-dind
    privileged: true
    env:
      DOCKER_TLS_CERTDIR: ""
```

Note: Requires `privileged: true` which is a security consideration.

## Limitations

1. **No persistent data** - Service containers are ephemeral. Data is lost after job completes.

2. **No external network** - Services can't reach the internet by default (security). Use `network: host` in service config to allow.

3. **No volume sharing between services** - Each service is isolated. Use the build container as the coordination point.

4. **No GPU support** - Services can't access GPUs. Use bare metal mode if services need GPU.

## Future Considerations

### Service Caching

Pre-warm common services to reduce startup time:

```yaml
services:
  postgres:
    image: postgres:16
    cache: warm  # Keep container running between builds
```

### External Services

Connect to existing services instead of spinning up new ones:

```yaml
services:
  postgres:
    external: true
    host: db.example.com
    port: 5432
```

### Service Sidecars

Long-running services that persist across builds on the same worker:

```yaml
# Worker config, not repo config
sidecars:
  postgres:
    image: postgres:16
    # Always running on this worker
```
