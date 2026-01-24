package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunBareMetal(t *testing.T) {
	dir := t.TempDir()

	// Create a test file
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	exitCode := Run(RunOptions{
		Command:   "cat test.txt && echo ' world'",
		WorkDir:   dir,
		BareMetal: true,
	})

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
}

func TestRunBareMetalWithConfig(t *testing.T) {
	dir := t.TempDir()

	// Create config that specifies bare metal
	configContent := `build: echo "from config"
container: none
`
	if err := os.WriteFile(filepath.Join(dir, ".cinch.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	exitCode := Run(RunOptions{
		WorkDir: dir,
	})

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
}

func TestRunBareMetalFailure(t *testing.T) {
	exitCode := Run(RunOptions{
		Command:   "exit 42",
		BareMetal: true,
	})

	if exitCode != 42 {
		t.Errorf("expected exit code 42, got %d", exitCode)
	}
}

func TestRunBareMetalEnv(t *testing.T) {
	dir := t.TempDir()

	// Create script that checks env var
	script := `#!/bin/sh
if [ "$MY_VAR" = "my_value" ]; then
    exit 0
else
    echo "MY_VAR is '$MY_VAR', expected 'my_value'"
    exit 1
fi
`
	scriptFile := filepath.Join(dir, "check_env.sh")
	if err := os.WriteFile(scriptFile, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	exitCode := Run(RunOptions{
		Command:   "sh check_env.sh",
		WorkDir:   dir,
		BareMetal: true,
		Env: map[string]string{
			"MY_VAR": "my_value",
		},
	})

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
}

func TestRunNoCommand(t *testing.T) {
	dir := t.TempDir()

	exitCode := Run(RunOptions{
		WorkDir:   dir,
		BareMetal: true,
	})

	// Should fail with exit code 1 (no command)
	if exitCode != 1 {
		t.Errorf("expected exit code 1 for no command, got %d", exitCode)
	}
}

// TestRunContainer tests container execution.
// Skip if docker is not available.
func TestRunContainer(t *testing.T) {
	if os.Getenv("CINCH_TEST_DOCKER") == "" {
		t.Skip("CINCH_TEST_DOCKER not set, skipping container test")
	}

	dir := t.TempDir()

	// Create a test file
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello from container"), 0644); err != nil {
		t.Fatal(err)
	}

	exitCode := Run(RunOptions{
		Command: "cat test.txt",
		WorkDir: dir,
	})

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
}

// TestRunContainerWithServices tests service containers.
// Skip if docker is not available.
func TestRunContainerWithServices(t *testing.T) {
	if os.Getenv("CINCH_TEST_DOCKER") == "" {
		t.Skip("CINCH_TEST_DOCKER not set, skipping container test")
	}

	dir := t.TempDir()

	// Create config with redis service
	configContent := `build: |
  apt-get update -qq && apt-get install -yqq redis-tools > /dev/null
  redis-cli -h redis PING
services:
  redis:
    image: redis:7-alpine
`
	if err := os.WriteFile(filepath.Join(dir, ".cinch.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	exitCode := Run(RunOptions{
		WorkDir: dir,
	})

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
}

func TestRunMultilineBuild(t *testing.T) {
	dir := t.TempDir()

	configContent := `build: |
  set -e
  echo "line1"
  echo "line2"
  echo "line3"
container: none
`
	if err := os.WriteFile(filepath.Join(dir, ".cinch.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	exitCode := Run(RunOptions{
		WorkDir: dir,
	})

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
}
