package container

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Docker runs commands in containers via the docker CLI.
// Works with Docker Desktop, Colima, OrbStack, Podman, etc.
type Docker struct {
	// WorkDir on host to mount as /workspace
	WorkDir string

	// Image to run (e.g., "ubuntu:22.04")
	Image string

	// Env vars to pass to container
	Env map[string]string

	// Network to join (for service containers)
	Network string

	// CacheVolumes maps volume names to container paths
	// e.g., {"cinch-npm": "/root/.npm"}
	CacheVolumes map[string]string

	// Stdout/Stderr for streaming output
	Stdout io.Writer
	Stderr io.Writer
}

// Run executes a command inside a container.
// Returns the exit code.
func (d *Docker) Run(ctx context.Context, command string) (int, error) {
	args := []string{"run", "--rm"}

	// Mount workspace
	if d.WorkDir != "" {
		absPath, err := filepath.Abs(d.WorkDir)
		if err != nil {
			return 1, fmt.Errorf("resolve workdir: %w", err)
		}
		args = append(args, "-v", absPath+":/workspace")
		args = append(args, "-w", "/workspace")
	}

	// Mount cache volumes
	for volName, containerPath := range d.CacheVolumes {
		args = append(args, "-v", volName+":"+containerPath)
	}

	// Environment variables
	for k, v := range d.Env {
		args = append(args, "-e", k+"="+v)
	}

	// Network (for services)
	if d.Network != "" {
		args = append(args, "--network", d.Network)
	}

	// Image and command
	args = append(args, d.Image, "sh", "-c", command)

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = d.Stdout
	cmd.Stderr = d.Stderr

	err := cmd.Run()
	return exitCode(err), nil
}

// Pull fetches an image if not present locally.
func (d *Docker) Pull(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "pull", d.Image)
	cmd.Stdout = d.Stdout
	cmd.Stderr = d.Stderr
	return cmd.Run()
}

// Build builds an image from a Dockerfile.
func Build(ctx context.Context, dockerfile, contextDir, tag string, stdout, stderr io.Writer) error {
	args := []string{"build", "-f", dockerfile, "-t", tag, contextDir}
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// CheckAvailable verifies docker CLI is available and daemon is running.
func CheckAvailable() error {
	cmd := exec.Command("docker", "info")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker not available - install Docker Desktop, Colima, OrbStack, or Podman: %w", err)
	}
	return nil
}

// DefaultCacheVolumes returns the standard cache volume mappings.
func DefaultCacheVolumes() map[string]string {
	return map[string]string{
		"cinch-cache-npm":   "/root/.npm",
		"cinch-cache-cargo": "/root/.cargo",
		"cinch-cache-go":    "/root/go/pkg",
		"cinch-cache-pip":   "/root/.cache/pip",
	}
}

// EnsureCacheDir creates the host cache directory if needed.
func EnsureCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	cacheDir := filepath.Join(home, ".cinch", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", err
	}
	return cacheDir, nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return status.ExitStatus()
		}
	}
	return 1
}
