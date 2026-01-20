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

	// If no command provided, load from config
	if command == "" {
		cfg, configFile, err := config.Load(workDir)
		if err != nil {
			if errors.Is(err, config.ErrNoConfig) {
				fmt.Fprintln(os.Stderr, "Error: no command provided and no config file found")
				fmt.Fprintln(os.Stderr, "Usage: cinch run \"make test\"")
				fmt.Fprintln(os.Stderr, "   or: create .cinch.yaml with 'command: make test'")
			} else {
				fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			}
			return 1
		}
		fmt.Printf("Loaded config from %s\n", configFile)
		command = cfg.Command

		// Check if config specifies bare metal
		if cfg.IsBareMetalContainer() {
			bareMetal = true
		}
	}

	// Bare metal mode - just run the command
	if bareMetal {
		return runBareMetal(ctx, command, workDir, opts.Env)
	}

	// Container mode
	return runContainer(ctx, command, workDir, opts.Env)
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

func runContainer(ctx context.Context, command, workDir string, env map[string]string) int {
	// Check docker is available
	if err := container.CheckAvailable(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintln(os.Stderr, "Hint: use --bare-metal to run without containers")
		return 1
	}

	// Detect image source
	fmt.Printf("Detecting container image...\n")
	source, err := container.DetectImage(workDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error detecting image: %v\n", err)
		return 1
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

	// Run in container
	fmt.Printf("\nRunning: %s\n", command)
	fmt.Printf("Working directory: /workspace (mounted from %s)\n\n", workDir)

	docker := &container.Docker{
		WorkDir:      workDir,
		Image:        image,
		Env:          env,
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
