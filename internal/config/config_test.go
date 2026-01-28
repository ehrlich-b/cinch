package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadYAML(t *testing.T) {
	dir := t.TempDir()
	content := `build: make test
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
	if cfg.Build != "make test" {
		t.Errorf("expected 'make test', got %q", cfg.Build)
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
	content := `build = "cargo build"
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
	if cfg.Build != "cargo build" {
		t.Errorf("expected 'cargo build', got %q", cfg.Build)
	}
	if cfg.Timeout.Duration() != 5*time.Minute {
		t.Errorf("expected 5m, got %v", cfg.Timeout.Duration())
	}
}

func TestLoadJSON(t *testing.T) {
	dir := t.TempDir()
	content := `{"build": "npm test", "timeout": "2m"}`
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
	if cfg.Build != "npm test" {
		t.Errorf("expected 'npm test', got %q", cfg.Build)
	}
}

func TestLoadPriority(t *testing.T) {
	// .cinch.yaml should take priority over cinch.yaml
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".cinch.yaml"), []byte("build: first"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cinch.yaml"), []byte("build: second"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, filename, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filename != ".cinch.yaml" {
		t.Errorf("expected .cinch.yaml priority, got %s", filename)
	}
	if cfg.Build != "first" {
		t.Errorf("expected 'first', got %q", cfg.Build)
	}
}

func TestLoadWithServices(t *testing.T) {
	dir := t.TempDir()
	content := `build: pytest
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

func TestValidateNoBuild(t *testing.T) {
	cfg := &Config{}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for missing build")
	}
}

func TestValidateBooleanFootgun(t *testing.T) {
	// YAML will parse `on` or `yes` as true
	cfg := &Config{Build: "true"}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for boolean footgun")
	}

	// Also test release field
	cfg = &Config{Build: "make test", Release: "true"}
	err = cfg.Validate()
	if err == nil {
		t.Error("expected error for boolean footgun in release")
	}
}

func TestValidateServiceNoImage(t *testing.T) {
	cfg := &Config{
		Build: "test",
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
	content := `build: test`
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

func TestMultilineBuild(t *testing.T) {
	dir := t.TempDir()
	content := `build: |
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
	if cfg.Build != expected {
		t.Errorf("expected multiline build, got %q", cfg.Build)
	}
}

func TestCommandForEvent(t *testing.T) {
	tests := []struct {
		name    string
		build   string
		release string
		isTag   bool
		want    string
	}{
		{"branch with no release", "make check", "", false, "make check"},
		{"branch with release", "make check", "make release", false, "make check"},
		{"tag with no release", "make check", "", true, "make check"},
		{"tag with release", "make check", "make release", true, "make release"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Build: tt.build, Release: tt.release}
			got := cfg.CommandForEvent(tt.isTag)
			if got != tt.want {
				t.Errorf("CommandForEvent(%v) = %q, want %q", tt.isTag, got, tt.want)
			}
		})
	}
}

func TestLoadWithRelease(t *testing.T) {
	dir := t.TempDir()
	content := `build: make check
release: make release
`
	if err := os.WriteFile(filepath.Join(dir, ".cinch.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Build != "make check" {
		t.Errorf("expected 'make check', got %q", cfg.Build)
	}
	if cfg.Release != "make release" {
		t.Errorf("expected 'make release', got %q", cfg.Release)
	}
}

func TestDevcontainerOptionYAML(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantPath string
		disabled bool
	}{
		{
			name:     "not specified uses default",
			content:  "build: test",
			wantPath: DefaultDevcontainerPath,
			disabled: false,
		},
		{
			name:     "explicit path",
			content:  "build: test\ndevcontainer: custom/devcontainer.json",
			wantPath: "custom/devcontainer.json",
			disabled: false,
		},
		{
			name:     "disabled with false",
			content:  "build: test\ndevcontainer: false",
			wantPath: "",
			disabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, ".cinch.yaml"), []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			cfg, _, err := Load(dir)
			if err != nil {
				t.Fatalf("Load failed: %v", err)
			}

			if cfg.Devcontainer.Disabled != tt.disabled {
				t.Errorf("Disabled = %v, want %v", cfg.Devcontainer.Disabled, tt.disabled)
			}
			if got := cfg.Devcontainer.EffectivePath(); got != tt.wantPath {
				t.Errorf("EffectivePath() = %q, want %q", got, tt.wantPath)
			}
		})
	}
}

func TestDevcontainerOptionJSON(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantPath string
		disabled bool
	}{
		{
			name:     "explicit path",
			content:  `{"build": "test", "devcontainer": "my/path.json"}`,
			wantPath: "my/path.json",
			disabled: false,
		},
		{
			name:     "disabled with false",
			content:  `{"build": "test", "devcontainer": false}`,
			wantPath: "",
			disabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, ".cinch.json"), []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			cfg, _, err := Load(dir)
			if err != nil {
				t.Fatalf("Load failed: %v", err)
			}

			if cfg.Devcontainer.Disabled != tt.disabled {
				t.Errorf("Disabled = %v, want %v", cfg.Devcontainer.Disabled, tt.disabled)
			}
			if got := cfg.Devcontainer.EffectivePath(); got != tt.wantPath {
				t.Errorf("EffectivePath() = %q, want %q", got, tt.wantPath)
			}
		})
	}
}

func TestImageConfig(t *testing.T) {
	dir := t.TempDir()
	content := `build: npm test
image: node:20
`
	if err := os.WriteFile(filepath.Join(dir, ".cinch.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Image != "node:20" {
		t.Errorf("expected image 'node:20', got %q", cfg.Image)
	}
}

func TestDockerfileConfig(t *testing.T) {
	dir := t.TempDir()
	content := `build: make test
dockerfile: docker/Dockerfile.ci
`
	if err := os.WriteFile(filepath.Join(dir, ".cinch.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Dockerfile != "docker/Dockerfile.ci" {
		t.Errorf("expected dockerfile 'docker/Dockerfile.ci', got %q", cfg.Dockerfile)
	}
}

func TestPRSupportVerification(t *testing.T) {
	// This test intentionally fails to verify PR gating works
	t.Fatal("PR support verification: this build should be blocked from merging")
}
