package worker

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
)

// Executor runs commands and streams output.
type Executor struct {
	// WorkDir is the working directory for commands.
	WorkDir string

	// Env is additional environment variables.
	Env map[string]string

	// Stdout/Stderr for streaming output.
	Stdout io.Writer
	Stderr io.Writer
}

// Run executes a command in bare metal (no container).
// Returns the exit code.
func (e *Executor) Run(ctx context.Context, command string) (int, error) {
	// Don't use CommandContext - we handle cancellation ourselves with process groups
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = e.WorkDir
	cmd.Env = e.buildEnv()
	cmd.Stdout = e.Stdout
	cmd.Stderr = e.Stderr

	// Create a new process group so we can kill all child processes
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return 1, err
	}

	// Wait for either completion or context cancellation
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		return exitCode(err), nil
	case <-ctx.Done():
		// Kill the entire process group (negative PID)
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		// Wait for it to actually exit
		<-done
		return 137, nil // 128 + 9 (SIGKILL)
	}
}

func (e *Executor) buildEnv() []string {
	// Start with current environment
	env := os.Environ()

	// Add custom env vars
	for k, v := range e.Env {
		env = append(env, k+"="+v)
	}

	return env
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

// CheckCommand verifies a command exists in PATH.
func CheckCommand(name string) error {
	_, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s not found in PATH", name)
	}
	return nil
}
