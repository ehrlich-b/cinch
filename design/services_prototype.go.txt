// services_prototype.go - Prototype implementation for review
// This would live in internal/worker/services.go
//
// ~250 lines including comments. Core logic is ~150 lines.

package worker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// ServiceConfig is what users write in .cinch.yaml
type ServiceConfig struct {
	Image       string            `yaml:"image"`
	Env         map[string]string `yaml:"env"`
	Command     string            `yaml:"command"`
	Healthcheck *HealthcheckConfig `yaml:"healthcheck"`
}

type HealthcheckConfig struct {
	Cmd      string        `yaml:"cmd"`
	Interval time.Duration `yaml:"interval"` // default: 5s
	Timeout  time.Duration `yaml:"timeout"`  // default: 60s
	Retries  int           `yaml:"retries"`  // default: 12
}

// ServiceOrchestrator manages service containers for a build
type ServiceOrchestrator struct {
	docker     *client.Client
	jobID      string
	networkID  string
	containers []string // container IDs to cleanup
}

// NewServiceOrchestrator creates an orchestrator for a specific job
func NewServiceOrchestrator(docker *client.Client, jobID string) *ServiceOrchestrator {
	return &ServiceOrchestrator{
		docker:     docker,
		jobID:      jobID,
		containers: make([]string, 0),
	}
}

// Start brings up all services and waits for them to be healthy
// Returns the network ID that the build container should join
func (s *ServiceOrchestrator) Start(ctx context.Context, services map[string]ServiceConfig) (string, error) {
	if len(services) == 0 {
		return "", nil // No services, no network needed
	}

	// 1. Create isolated network for this build
	networkName := fmt.Sprintf("cinch-%s", s.jobID)
	resp, err := s.docker.NetworkCreate(ctx, networkName, network.CreateOptions{
		Driver: "bridge",
		Labels: map[string]string{"cinch.job": s.jobID},
	})
	if err != nil {
		return "", fmt.Errorf("create network: %w", err)
	}
	s.networkID = resp.ID

	// 2. Start each service container
	for name, svc := range services {
		containerID, err := s.startService(ctx, name, svc)
		if err != nil {
			return "", fmt.Errorf("start service %s: %w", name, err)
		}
		s.containers = append(s.containers, containerID)
	}

	// 3. Wait for all services to be healthy
	for name, svc := range services {
		if err := s.waitHealthy(ctx, name, svc); err != nil {
			return "", fmt.Errorf("service %s unhealthy: %w", name, err)
		}
	}

	return s.networkID, nil
}

// Cleanup stops all containers and removes the network
// Always call this, even on build failure
func (s *ServiceOrchestrator) Cleanup(ctx context.Context) {
	// Use background context - we want cleanup even if parent ctx is cancelled
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop and remove containers
	for _, id := range s.containers {
		timeout := 10
		_ = s.docker.ContainerStop(cleanupCtx, id, container.StopOptions{Timeout: &timeout})
		_ = s.docker.ContainerRemove(cleanupCtx, id, container.RemoveOptions{Force: true})
	}

	// Remove network
	if s.networkID != "" {
		_ = s.docker.NetworkRemove(cleanupCtx, s.networkID)
	}
}

func (s *ServiceOrchestrator) startService(ctx context.Context, name string, svc ServiceConfig) (string, error) {
	// Pull image if needed
	// (In production, add image pull logic here)

	// Convert env map to slice
	env := make([]string, 0, len(svc.Env))
	for k, v := range svc.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Parse command if provided
	var cmd []string
	if svc.Command != "" {
		cmd = strings.Fields(svc.Command)
	}

	// Create container
	containerName := fmt.Sprintf("cinch-%s-%s", s.jobID, name)
	resp, err := s.docker.ContainerCreate(ctx,
		&container.Config{
			Image: svc.Image,
			Env:   env,
			Cmd:   cmd,
			Labels: map[string]string{
				"cinch.job":     s.jobID,
				"cinch.service": name,
			},
		},
		&container.HostConfig{
			NetworkMode: container.NetworkMode(s.networkID),
		},
		&network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				s.networkID: {
					Aliases: []string{name}, // Service reachable as "postgres", "redis", etc.
				},
			},
		},
		nil, // platform
		containerName,
	)
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}

	// Start container
	if err := s.docker.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("start container: %w", err)
	}

	return resp.ID, nil
}

func (s *ServiceOrchestrator) waitHealthy(ctx context.Context, name string, svc ServiceConfig) error {
	hc := svc.Healthcheck
	if hc == nil {
		// No healthcheck configured - assume ready after small delay
		time.Sleep(2 * time.Second)
		return nil
	}

	// Apply defaults
	interval := hc.Interval
	if interval == 0 {
		interval = 5 * time.Second
	}
	timeout := hc.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	// Find container ID by name
	containerName := fmt.Sprintf("cinch-%s-%s", s.jobID, name)

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("healthcheck timeout after %v", timeout)
			}

			healthy, err := s.checkHealth(ctx, containerName, hc.Cmd)
			if err != nil {
				// Log but don't fail - container might still be starting
				continue
			}
			if healthy {
				return nil
			}
		}
	}
}

func (s *ServiceOrchestrator) checkHealth(ctx context.Context, containerName, cmd string) (bool, error) {
	// Create exec to run healthcheck command
	exec, err := s.docker.ContainerExecCreate(ctx, containerName, container.ExecOptions{
		Cmd:          []string{"sh", "-c", cmd},
		AttachStdout: false,
		AttachStderr: false,
	})
	if err != nil {
		return false, err
	}

	// Run the healthcheck
	if err := s.docker.ContainerExecStart(ctx, exec.ID, container.ExecStartOptions{}); err != nil {
		return false, err
	}

	// Check exit code
	inspect, err := s.docker.ContainerExecInspect(ctx, exec.ID)
	if err != nil {
		return false, err
	}

	return inspect.ExitCode == 0, nil
}

// --- Usage in job execution ---

func (w *Worker) runJobWithServices(ctx context.Context, job *Job) error {
	// Create orchestrator
	orch := NewServiceOrchestrator(w.docker, job.ID)
	defer orch.Cleanup(ctx) // Always cleanup

	// Start services (if any)
	networkID, err := orch.Start(ctx, job.Config.Services)
	if err != nil {
		return fmt.Errorf("services failed: %w", err)
	}

	// Run build container on the same network
	return w.runBuildContainer(ctx, job, networkID)
}

// --- That's it. ---
//
// What this gives you:
// - Isolated network per build (no port conflicts)
// - Services accessible by name (postgres:5432, redis:6379)
// - Health checks with configurable timeout
// - Automatic cleanup even on failure
//
// What this doesn't do:
// - Volume mounts for services (add if needed)
// - Resource limits (add in v0.2)
// - Service logs (they go to container logs, not cinch logs)
// - Caching service images (docker handles this)
