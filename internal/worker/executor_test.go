package worker

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecutorRun(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exec := &Executor{
		Stdout: &stdout,
		Stderr: &stderr,
	}

	exitCode, err := exec.Run(context.Background(), "echo hello")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
	if got := strings.TrimSpace(stdout.String()); got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestExecutorExitCode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exec := &Executor{
		Stdout: &stdout,
		Stderr: &stderr,
	}

	exitCode, err := exec.Run(context.Background(), "exit 42")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if exitCode != 42 {
		t.Errorf("expected exit code 42, got %d", exitCode)
	}
}

func TestExecutorEnv(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exec := &Executor{
		Env: map[string]string{
			"CINCH_TEST_VAR": "test_value",
		},
		Stdout: &stdout,
		Stderr: &stderr,
	}

	exitCode, err := exec.Run(context.Background(), "echo $CINCH_TEST_VAR")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
	if got := strings.TrimSpace(stdout.String()); got != "test_value" {
		t.Errorf("expected 'test_value', got %q", got)
	}
}

func TestExecutorWorkDir(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	exec := &Executor{
		WorkDir: dir,
		Stdout:  &stdout,
		Stderr:  &stderr,
	}

	exitCode, err := exec.Run(context.Background(), "cat test.txt")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
	if got := strings.TrimSpace(stdout.String()); got != "content" {
		t.Errorf("expected 'content', got %q", got)
	}
}

func TestExecutorStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exec := &Executor{
		Stdout: &stdout,
		Stderr: &stderr,
	}

	exitCode, err := exec.Run(context.Background(), "echo error >&2")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
	if got := strings.TrimSpace(stderr.String()); got != "error" {
		t.Errorf("expected 'error' on stderr, got %q", got)
	}
}

func TestExecutorContextCancel(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exec := &Executor{
		Stdout: &stdout,
		Stderr: &stderr,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	exitCode, err := exec.Run(ctx, "sleep 10")
	elapsed := time.Since(start)

	// Should return quickly, not after 10 seconds
	if elapsed > 2*time.Second {
		t.Errorf("command took too long: %v", elapsed)
	}

	// Context cancelled commands return non-zero
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exitCode == 0 {
		t.Error("expected non-zero exit code for cancelled command")
	}
}

func TestExecutorMultilineCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exec := &Executor{
		Stdout: &stdout,
		Stderr: &stderr,
	}

	command := `set -e
echo line1
echo line2
`
	exitCode, err := exec.Run(context.Background(), command)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
	output := stdout.String()
	if !strings.Contains(output, "line1") || !strings.Contains(output, "line2") {
		t.Errorf("expected multiline output, got %q", output)
	}
}

func TestExecutorFailFast(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exec := &Executor{
		Stdout: &stdout,
		Stderr: &stderr,
	}

	// With set -e, should fail on first error
	command := `set -e
false
echo should_not_print
`
	exitCode, err := exec.Run(context.Background(), command)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if exitCode == 0 {
		t.Error("expected non-zero exit code")
	}
	if strings.Contains(stdout.String(), "should_not_print") {
		t.Error("command should have stopped at 'false'")
	}
}

func TestCheckCommand(t *testing.T) {
	// sh should exist everywhere
	if err := CheckCommand("sh"); err != nil {
		t.Errorf("sh should exist: %v", err)
	}

	// Made up command shouldn't exist
	if err := CheckCommand("definitely_not_a_real_command_12345"); err == nil {
		t.Error("expected error for non-existent command")
	}
}
