package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadYAML(t *testing.T) {
	dir := t.TempDir()
	content := `command: make test
timeout: 10m
workers:
  - linux
  - arm64
`
	if err := os.WriteFile(filepath.Join(dir, ".cinch.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, filename, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if filename != ".cinch.yaml" {
		t.Errorf("expected .cinch.yaml, got %s", filename)
	}
	if cfg.Command != "make test" {
		t.Errorf("expected 'make test', got %q", cfg.Command)
	}
	if cfg.Timeout.Duration() != 10*time.Minute {
		t.Errorf("expected 10m, got %v", cfg.Timeout.Duration())
	}
	if len(cfg.Workers) != 2 || cfg.Workers[0] != "linux" {
		t.Errorf("expected [linux arm64], got %v", cfg.Workers)
	}
}

func TestLoadTOML(t *testing.T) {
	dir := t.TempDir()
	content := `command = "cargo build"
timeout = "5m"
`
	if err := os.WriteFile(filepath.Join(dir, ".cinch.toml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, filename, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if filename != ".cinch.toml" {
		t.Errorf("expected .cinch.toml, got %s", filename)
	}
	if cfg.Command != "cargo build" {
		t.Errorf("expected 'cargo build', got %q", cfg.Command)
	}
	if cfg.Timeout.Duration() != 5*time.Minute {
		t.Errorf("expected 5m, got %v", cfg.Timeout.Duration())
	}
}

func TestLoadJSON(t *testing.T) {
	dir := t.TempDir()
	content := `{"command": "npm test", "timeout": "2m"}`
	if err := os.WriteFile(filepath.Join(dir, ".cinch.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, filename, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if filename != ".cinch.json" {
		t.Errorf("expected .cinch.json, got %s", filename)
	}
	if cfg.Command != "npm test" {
		t.Errorf("expected 'npm test', got %q", cfg.Command)
	}
}

func TestLoadPriority(t *testing.T) {
	// .cinch.yaml should take priority over cinch.yaml
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".cinch.yaml"), []byte("command: first"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cinch.yaml"), []byte("command: second"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, filename, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filename != ".cinch.yaml" {
		t.Errorf("expected .cinch.yaml priority, got %s", filename)
	}
	if cfg.Command != "first" {
		t.Errorf("expected 'first', got %q", cfg.Command)
	}
}

func TestLoadWithServices(t *testing.T) {
	dir := t.TempDir()
	content := `command: pytest
services:
  postgres:
    image: postgres:16-alpine
    env:
      POSTGRES_PASSWORD: secret
    healthcheck:
      cmd: pg_isready -U postgres
      timeout: 30s
  redis:
    image: redis:7-alpine
`
	if err := os.WriteFile(filepath.Join(dir, ".cinch.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(cfg.Services))
	}

	pg := cfg.Services["postgres"]
	if pg.Image != "postgres:16-alpine" {
		t.Errorf("expected postgres:16-alpine, got %s", pg.Image)
	}
	if pg.Env["POSTGRES_PASSWORD"] != "secret" {
		t.Errorf("expected POSTGRES_PASSWORD=secret, got %v", pg.Env)
	}
	if pg.Healthcheck == nil {
		t.Fatal("expected healthcheck")
	}
	if pg.Healthcheck.Cmd != "pg_isready -U postgres" {
		t.Errorf("expected pg_isready, got %s", pg.Healthcheck.Cmd)
	}
	if pg.Healthcheck.Timeout.Duration() != 30*time.Second {
		t.Errorf("expected 30s, got %v", pg.Healthcheck.Timeout.Duration())
	}

	redis := cfg.Services["redis"]
	if redis.Image != "redis:7-alpine" {
		t.Errorf("expected redis:7-alpine, got %s", redis.Image)
	}
}

func TestValidateNoCommand(t *testing.T) {
	cfg := &Config{}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for missing command")
	}
}

func TestValidateBooleanFootgun(t *testing.T) {
	// YAML will parse `on` or `yes` as true
	cfg := &Config{Command: "true"}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for boolean footgun")
	}
}

func TestValidateServiceNoImage(t *testing.T) {
	cfg := &Config{
		Command: "test",
		Services: map[string]Service{
			"db": {Image: ""},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for service without image")
	}
}

func TestDefaults(t *testing.T) {
	dir := t.TempDir()
	content := `command: test`
	if err := os.WriteFile(filepath.Join(dir, ".cinch.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Default timeout is 30 minutes
	if cfg.Timeout.Duration() != 30*time.Minute {
		t.Errorf("expected default timeout 30m, got %v", cfg.Timeout.Duration())
	}
}

func TestNoConfigError(t *testing.T) {
	dir := t.TempDir()
	_, _, err := Load(dir)
	if err != ErrNoConfig {
		t.Errorf("expected ErrNoConfig, got %v", err)
	}
}

func TestIsBareMetalContainer(t *testing.T) {
	tests := []struct {
		container string
		want      bool
	}{
		{"", false},
		{"ubuntu:22.04", false},
		{"none", true},
	}

	for _, tt := range tests {
		cfg := &Config{Container: tt.container}
		if got := cfg.IsBareMetalContainer(); got != tt.want {
			t.Errorf("IsBareMetalContainer(%q) = %v, want %v", tt.container, got, tt.want)
		}
	}
}

func TestMultilineCommand(t *testing.T) {
	dir := t.TempDir()
	content := `command: |
  set -e
  make build
  make test
`
	if err := os.WriteFile(filepath.Join(dir, ".cinch.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	expected := "set -e\nmake build\nmake test\n"
	if cfg.Command != expected {
		t.Errorf("expected multiline command, got %q", cfg.Command)
	}
}
