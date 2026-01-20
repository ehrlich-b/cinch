package container

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/ehrlich-b/cinch/internal/config"
)

// ServiceManager handles the lifecycle of service containers.
type ServiceManager struct {
	JobID   string
	Network string
	Stdout  io.Writer
	Stderr  io.Writer

	containers []string
	mu         sync.Mutex
}

// NewServiceManager creates a manager for a job's services.
func NewServiceManager(jobID string, stdout, stderr io.Writer) *ServiceManager {
	return &ServiceManager{
		JobID:   jobID,
		Network: fmt.Sprintf("cinch-%s", jobID),
		Stdout:  stdout,
		Stderr:  stderr,
	}
}

// Setup creates the network and starts all services.
// Returns an error if any service fails to start or become healthy.
func (m *ServiceManager) Setup(ctx context.Context, services map[string]config.Service) error {
	if len(services) == 0 {
		return nil
	}

	// Create network
	fmt.Fprintf(m.Stdout, "Creating network %s...\n", m.Network)
	if err := CreateNetwork(ctx, m.Network); err != nil {
		return fmt.Errorf("create network: %w", err)
	}

	// Start services in parallel
	var wg sync.WaitGroup
	errCh := make(chan error, len(services))

	for name, svc := range services {
		wg.Add(1)
		go func(name string, svc config.Service) {
			defer wg.Done()
			if err := m.startService(ctx, name, svc); err != nil {
				errCh <- fmt.Errorf("service %s: %w", name, err)
			}
		}(name, svc)
	}

	wg.Wait()
	close(errCh)

	// Return first error
	for err := range errCh {
		return err
	}

	return nil
}

func (m *ServiceManager) startService(ctx context.Context, name string, svc config.Service) error {
	fmt.Fprintf(m.Stdout, "Starting service %s (%s)...\n", name, svc.Image)

	// Pull image first
	if err := PullImage(ctx, svc.Image, m.Stdout, m.Stderr); err != nil {
		return fmt.Errorf("pull image: %w", err)
	}

	// Start container
	containerName := fmt.Sprintf("cinch-%s-%s", m.JobID, name)
	containerID, err := StartService(ctx, ServiceConfig{
		Name:        containerName,
		Image:       svc.Image,
		Network:     m.Network,
		NetworkName: name, // Service is accessible as "postgres", "redis", etc.
		Env:         svc.Env,
		Command:     svc.Command,
	}, m.Stdout, m.Stderr)
	if err != nil {
		return fmt.Errorf("start container: %w", err)
	}

	// Track for cleanup
	m.mu.Lock()
	m.containers = append(m.containers, containerID)
	m.mu.Unlock()

	// Wait for healthy
	if svc.Healthcheck != nil {
		fmt.Fprintf(m.Stdout, "Waiting for %s to be healthy...\n", name)
		if err := m.waitHealthy(ctx, containerID, svc.Healthcheck); err != nil {
			return fmt.Errorf("healthcheck: %w", err)
		}
		fmt.Fprintf(m.Stdout, "Service %s is ready\n", name)
	} else {
		// No healthcheck - give it a moment to start
		time.Sleep(1 * time.Second)
		fmt.Fprintf(m.Stdout, "Service %s started\n", name)
	}

	return nil
}

func (m *ServiceManager) waitHealthy(ctx context.Context, containerID string, hc *config.Healthcheck) error {
	timeout := hc.Timeout.Duration()
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	deadline := time.Now().Add(timeout)
	interval := 2 * time.Second

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout after %s", timeout)
		}

		exitCode, err := ExecInContainer(ctx, containerID, hc.Cmd)
		if err == nil && exitCode == 0 {
			return nil
		}

		time.Sleep(interval)
	}
}

// Cleanup stops all service containers and removes the network.
// Always attempts to clean up everything, even on errors.
func (m *ServiceManager) Cleanup(ctx context.Context) {
	if m.Network == "" {
		return
	}

	fmt.Fprintf(m.Stdout, "\nCleaning up services...\n")

	// Stop containers (use background context to ensure cleanup happens)
	cleanupCtx := context.Background()
	for _, containerID := range m.containers {
		if err := StopService(cleanupCtx, containerID); err != nil {
			fmt.Fprintf(m.Stderr, "Warning: failed to stop container %s: %v\n", containerID[:12], err)
		}
	}

	// Remove network
	if err := RemoveNetwork(cleanupCtx, m.Network); err != nil {
		fmt.Fprintf(m.Stderr, "Warning: failed to remove network %s: %v\n", m.Network, err)
	}
}
