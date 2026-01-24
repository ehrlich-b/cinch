package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ehrlich-b/cinch/internal/config"
	"github.com/ehrlich-b/cinch/internal/worker"
	"github.com/ehrlich-b/cinch/internal/worker/container"
)

// RunOptions configures the run command.
type RunOptions struct {
	Command   string
	WorkDir   string
	BareMetal bool
	Env       map[string]string
}

// Run executes a command locally, simulating what CI would do.
func Run(opts RunOptions) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\nInterrupted, stopping...")
		cancel()
	}()

	// Determine working directory
	workDir := opts.WorkDir
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot get working directory: %v\n", err)
			return 1
		}
	}

	command := opts.Command
	bareMetal := opts.BareMetal
	var cfg *config.Config

	// Try to load config (for services, timeout, etc.)
	loadedCfg, configFile, err := config.Load(workDir)
	if err == nil {
		cfg = loadedCfg
		fmt.Printf("Loaded config from %s\n", configFile)

		// Use build command from config if not provided
		if command == "" {
			command = cfg.Build
		}

		// Check if config specifies bare metal
		if cfg.IsBareMetalContainer() {
			bareMetal = true
		}
	} else if !errors.Is(err, config.ErrNoConfig) {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		return 1
	}

	// Still no command?
	if command == "" {
		fmt.Fprintln(os.Stderr, "Error: no command provided and no config file found")
		fmt.Fprintln(os.Stderr, "Usage: cinch run \"make test\"")
		fmt.Fprintln(os.Stderr, "   or: create .cinch.yaml with 'build: make test'")
		return 1
	}

	// Bare metal mode - just run the command
	if bareMetal {
		return runBareMetal(ctx, command, workDir, opts.Env)
	}

	// Container mode (with optional services)
	return runContainer(ctx, command, workDir, opts.Env, cfg)
}

func runBareMetal(ctx context.Context, command, workDir string, env map[string]string) int {
	fmt.Printf("Running (bare metal): %s\n", command)
	fmt.Printf("Working directory: %s\n\n", workDir)

	exec := &worker.Executor{
		WorkDir: workDir,
		Env:     env,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	}

	exitCode, err := exec.Run(ctx, command)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	fmt.Printf("\nExit code: %d\n", exitCode)
	return exitCode
}

func runContainer(ctx context.Context, command, workDir string, env map[string]string, cfg *config.Config) int {
	// Check docker is available
	if err := container.CheckAvailable(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintln(os.Stderr, "Hint: use --bare-metal to run without containers")
		return 1
	}

	// Resolve container image from config
	fmt.Printf("Resolving container image...\n")
	effectiveCfg := cfg
	if effectiveCfg == nil {
		// No config file, use defaults
		effectiveCfg = &config.Config{}
	}
	source, err := container.ResolveContainer(effectiveCfg, workDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving container: %v\n", err)
		return 1
	}

	// Handle bare-metal case (shouldn't happen if runContainer was called, but be safe)
	if source.Type == "bare-metal" {
		return runBareMetal(ctx, command, workDir, env)
	}

	switch source.Type {
	case "image":
		fmt.Printf("Using image: %s\n", source.Image)
	case "dockerfile":
		fmt.Printf("Building from: %s\n", source.Dockerfile)
	case "devcontainer":
		if source.Image != "" {
			fmt.Printf("Using devcontainer image: %s\n", source.Image)
		} else {
			fmt.Printf("Building devcontainer from: %s\n", source.Dockerfile)
		}
	}

	// Prepare image (pull or build)
	jobID := "local"
	image, err := container.PrepareImage(ctx, source, jobID, os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error preparing image: %v\n", err)
		return 1
	}

	// Start services if configured
	var svcManager *container.ServiceManager
	var network string
	if cfg != nil && len(cfg.Services) > 0 {
		fmt.Printf("\nStarting %d service(s)...\n", len(cfg.Services))
		svcManager = container.NewServiceManager(jobID, os.Stdout, os.Stderr)
		defer svcManager.Cleanup(ctx)

		if err := svcManager.Setup(ctx, cfg.Services); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting services: %v\n", err)
			return 1
		}
		network = svcManager.Network
		fmt.Println()
	}

	// Run in container
	fmt.Printf("Running: %s\n", command)
	fmt.Printf("Working directory: /workspace (mounted from %s)\n\n", workDir)

	docker := &container.Docker{
		WorkDir:      workDir,
		Image:        image,
		Env:          env,
		Network:      network,
		CacheVolumes: container.DefaultCacheVolumes(),
		Stdout:       os.Stdout,
		Stderr:       os.Stderr,
	}

	exitCode, err := docker.Run(ctx, command)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	fmt.Printf("\nExit code: %d\n", exitCode)
	return exitCode
}
