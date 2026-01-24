package container

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ehrlich-b/cinch/internal/config"
)

func TestDetectImageDefault(t *testing.T) {
	dir := t.TempDir()

	source, err := DetectImage(dir)
	if err != nil {
		t.Fatalf("DetectImage failed: %v", err)
	}
	if source.Type != "image" {
		t.Errorf("expected type 'image', got %q", source.Type)
	}
	if source.Image != DefaultImage {
		t.Errorf("expected %q, got %q", DefaultImage, source.Image)
	}
}

func TestDetectImageRootDockerfile(t *testing.T) {
	dir := t.TempDir()
	dockerfile := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(dockerfile, []byte("FROM alpine"), 0644); err != nil {
		t.Fatal(err)
	}

	source, err := DetectImage(dir)
	if err != nil {
		t.Fatalf("DetectImage failed: %v", err)
	}
	if source.Type != "dockerfile" {
		t.Errorf("expected type 'dockerfile', got %q", source.Type)
	}
	if source.Dockerfile != dockerfile {
		t.Errorf("expected dockerfile %q, got %q", dockerfile, source.Dockerfile)
	}
	if source.Context != dir {
		t.Errorf("expected context %q, got %q", dir, source.Context)
	}
}

func TestDetectImageDevcontainerDockerfile(t *testing.T) {
	dir := t.TempDir()
	devcontainerDir := filepath.Join(dir, ".devcontainer")
	if err := os.MkdirAll(devcontainerDir, 0755); err != nil {
		t.Fatal(err)
	}
	dockerfile := filepath.Join(devcontainerDir, "Dockerfile")
	if err := os.WriteFile(dockerfile, []byte("FROM node:20"), 0644); err != nil {
		t.Fatal(err)
	}

	source, err := DetectImage(dir)
	if err != nil {
		t.Fatalf("DetectImage failed: %v", err)
	}
	if source.Type != "dockerfile" {
		t.Errorf("expected type 'dockerfile', got %q", source.Type)
	}
	if source.Dockerfile != dockerfile {
		t.Errorf("expected dockerfile %q, got %q", dockerfile, source.Dockerfile)
	}
	if source.Context != devcontainerDir {
		t.Errorf("expected context %q, got %q", devcontainerDir, source.Context)
	}
}

func TestDetectImageDevcontainerJSON_Image(t *testing.T) {
	dir := t.TempDir()
	devcontainerDir := filepath.Join(dir, ".devcontainer")
	if err := os.MkdirAll(devcontainerDir, 0755); err != nil {
		t.Fatal(err)
	}
	jsonFile := filepath.Join(devcontainerDir, "devcontainer.json")
	content := `{"image": "mcr.microsoft.com/devcontainers/go:1.21"}`
	if err := os.WriteFile(jsonFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	source, err := DetectImage(dir)
	if err != nil {
		t.Fatalf("DetectImage failed: %v", err)
	}
	if source.Type != "devcontainer" {
		t.Errorf("expected type 'devcontainer', got %q", source.Type)
	}
	if source.Image != "mcr.microsoft.com/devcontainers/go:1.21" {
		t.Errorf("unexpected image: %q", source.Image)
	}
}

func TestDetectImageDevcontainerJSON_Build(t *testing.T) {
	dir := t.TempDir()
	devcontainerDir := filepath.Join(dir, ".devcontainer")
	if err := os.MkdirAll(devcontainerDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create devcontainer.json with build config
	jsonFile := filepath.Join(devcontainerDir, "devcontainer.json")
	content := `{"build": {"dockerfile": "Dockerfile", "context": ".."}}`
	if err := os.WriteFile(jsonFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Create the referenced Dockerfile
	dockerfile := filepath.Join(devcontainerDir, "Dockerfile")
	if err := os.WriteFile(dockerfile, []byte("FROM alpine"), 0644); err != nil {
		t.Fatal(err)
	}

	source, err := DetectImage(dir)
	if err != nil {
		t.Fatalf("DetectImage failed: %v", err)
	}
	if source.Type != "devcontainer" {
		t.Errorf("expected type 'devcontainer', got %q", source.Type)
	}
	if source.Dockerfile != dockerfile {
		t.Errorf("expected dockerfile %q, got %q", dockerfile, source.Dockerfile)
	}
	// Context should be resolved relative to .devcontainer
	expectedContext := filepath.Join(devcontainerDir, "..")
	if source.Context != expectedContext {
		t.Errorf("expected context %q, got %q", expectedContext, source.Context)
	}
}

func TestDetectImageDevcontainerJSON_LegacyDockerFile(t *testing.T) {
	dir := t.TempDir()
	devcontainerDir := filepath.Join(dir, ".devcontainer")
	if err := os.MkdirAll(devcontainerDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create devcontainer.json with legacy dockerFile field
	jsonFile := filepath.Join(devcontainerDir, "devcontainer.json")
	content := `{"dockerFile": "Dockerfile.dev"}`
	if err := os.WriteFile(jsonFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Create the referenced Dockerfile
	dockerfile := filepath.Join(devcontainerDir, "Dockerfile.dev")
	if err := os.WriteFile(dockerfile, []byte("FROM alpine"), 0644); err != nil {
		t.Fatal(err)
	}

	source, err := DetectImage(dir)
	if err != nil {
		t.Fatalf("DetectImage failed: %v", err)
	}
	if source.Type != "devcontainer" {
		t.Errorf("expected type 'devcontainer', got %q", source.Type)
	}
	if source.Dockerfile != dockerfile {
		t.Errorf("expected dockerfile %q, got %q", dockerfile, source.Dockerfile)
	}
}

func TestDetectImageDevcontainerJSON_ImplicitDockerfile(t *testing.T) {
	dir := t.TempDir()
	devcontainerDir := filepath.Join(dir, ".devcontainer")
	if err := os.MkdirAll(devcontainerDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create devcontainer.json with no image or dockerfile specified
	jsonFile := filepath.Join(devcontainerDir, "devcontainer.json")
	content := `{"name": "My Dev Container"}`
	if err := os.WriteFile(jsonFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Create implicit Dockerfile
	dockerfile := filepath.Join(devcontainerDir, "Dockerfile")
	if err := os.WriteFile(dockerfile, []byte("FROM alpine"), 0644); err != nil {
		t.Fatal(err)
	}

	source, err := DetectImage(dir)
	if err != nil {
		t.Fatalf("DetectImage failed: %v", err)
	}
	if source.Type != "devcontainer" {
		t.Errorf("expected type 'devcontainer', got %q", source.Type)
	}
	if source.Dockerfile != dockerfile {
		t.Errorf("expected dockerfile %q, got %q", dockerfile, source.Dockerfile)
	}
}

func TestDetectImageDevcontainerJSON_NoImageOrDockerfile(t *testing.T) {
	dir := t.TempDir()
	devcontainerDir := filepath.Join(dir, ".devcontainer")
	if err := os.MkdirAll(devcontainerDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create devcontainer.json with no image or dockerfile
	jsonFile := filepath.Join(devcontainerDir, "devcontainer.json")
	content := `{"name": "Broken"}`
	if err := os.WriteFile(jsonFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := DetectImage(dir)
	if err == nil {
		t.Error("expected error for devcontainer.json with no image or dockerfile")
	}
}

func TestDetectImagePriority(t *testing.T) {
	// devcontainer.json should take priority over root Dockerfile
	dir := t.TempDir()

	// Create root Dockerfile
	rootDockerfile := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(rootDockerfile, []byte("FROM ubuntu"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create devcontainer.json
	devcontainerDir := filepath.Join(dir, ".devcontainer")
	if err := os.MkdirAll(devcontainerDir, 0755); err != nil {
		t.Fatal(err)
	}
	jsonFile := filepath.Join(devcontainerDir, "devcontainer.json")
	content := `{"image": "node:20"}`
	if err := os.WriteFile(jsonFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	source, err := DetectImage(dir)
	if err != nil {
		t.Fatalf("DetectImage failed: %v", err)
	}
	// devcontainer.json should win
	if source.Type != "devcontainer" {
		t.Errorf("expected devcontainer to take priority, got type %q", source.Type)
	}
	if source.Image != "node:20" {
		t.Errorf("expected node:20, got %q", source.Image)
	}
}

func TestDetectImagePriority_DevcontainerDockerfile(t *testing.T) {
	// .devcontainer/Dockerfile should take priority over root Dockerfile
	dir := t.TempDir()

	// Create root Dockerfile
	rootDockerfile := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(rootDockerfile, []byte("FROM ubuntu"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create .devcontainer/Dockerfile
	devcontainerDir := filepath.Join(dir, ".devcontainer")
	if err := os.MkdirAll(devcontainerDir, 0755); err != nil {
		t.Fatal(err)
	}
	devDockerfile := filepath.Join(devcontainerDir, "Dockerfile")
	if err := os.WriteFile(devDockerfile, []byte("FROM node"), 0644); err != nil {
		t.Fatal(err)
	}

	source, err := DetectImage(dir)
	if err != nil {
		t.Fatalf("DetectImage failed: %v", err)
	}
	// .devcontainer/Dockerfile should win
	if source.Dockerfile != devDockerfile {
		t.Errorf("expected devcontainer Dockerfile to take priority, got %q", source.Dockerfile)
	}
}

// Tests for ResolveContainer (config-driven resolution)

func TestResolveContainer_ExplicitImage(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Build: "test",
		Image: "node:20",
	}

	source, err := ResolveContainer(cfg, dir)
	if err != nil {
		t.Fatalf("ResolveContainer failed: %v", err)
	}
	if source.Type != "image" {
		t.Errorf("expected type 'image', got %q", source.Type)
	}
	if source.Image != "node:20" {
		t.Errorf("expected 'node:20', got %q", source.Image)
	}
}

func TestResolveContainer_ExplicitDockerfile(t *testing.T) {
	dir := t.TempDir()
	dockerDir := filepath.Join(dir, "docker")
	if err := os.MkdirAll(dockerDir, 0755); err != nil {
		t.Fatal(err)
	}
	dockerfile := filepath.Join(dockerDir, "Dockerfile.ci")
	if err := os.WriteFile(dockerfile, []byte("FROM alpine"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Build:      "test",
		Dockerfile: "docker/Dockerfile.ci",
	}

	source, err := ResolveContainer(cfg, dir)
	if err != nil {
		t.Fatalf("ResolveContainer failed: %v", err)
	}
	if source.Type != "dockerfile" {
		t.Errorf("expected type 'dockerfile', got %q", source.Type)
	}
	if source.Dockerfile != dockerfile {
		t.Errorf("expected dockerfile %q, got %q", dockerfile, source.Dockerfile)
	}
}

func TestResolveContainer_BareMetal(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Build:     "test",
		Container: "none",
	}

	source, err := ResolveContainer(cfg, dir)
	if err != nil {
		t.Fatalf("ResolveContainer failed: %v", err)
	}
	if source.Type != "bare-metal" {
		t.Errorf("expected type 'bare-metal', got %q", source.Type)
	}
}

func TestResolveContainer_DevcontainerDisabled(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Build: "test",
		Devcontainer: config.DevcontainerOption{
			Disabled: true,
			IsSet:    true,
		},
	}

	source, err := ResolveContainer(cfg, dir)
	if err != nil {
		t.Fatalf("ResolveContainer failed: %v", err)
	}
	if source.Type != "image" {
		t.Errorf("expected type 'image', got %q", source.Type)
	}
	if source.Image != DefaultImage {
		t.Errorf("expected default image %q, got %q", DefaultImage, source.Image)
	}
}

func TestResolveContainer_DevcontainerDefault(t *testing.T) {
	dir := t.TempDir()
	devcontainerDir := filepath.Join(dir, ".devcontainer")
	if err := os.MkdirAll(devcontainerDir, 0755); err != nil {
		t.Fatal(err)
	}
	jsonFile := filepath.Join(devcontainerDir, "devcontainer.json")
	content := `{"image": "mcr.microsoft.com/devcontainers/go:1.21"}`
	if err := os.WriteFile(jsonFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Build: "test",
		// No devcontainer option set, should use default path
	}

	source, err := ResolveContainer(cfg, dir)
	if err != nil {
		t.Fatalf("ResolveContainer failed: %v", err)
	}
	if source.Type != "devcontainer" {
		t.Errorf("expected type 'devcontainer', got %q", source.Type)
	}
	if source.Image != "mcr.microsoft.com/devcontainers/go:1.21" {
		t.Errorf("expected devcontainer image, got %q", source.Image)
	}
}

func TestResolveContainer_DevcontainerCustomPath(t *testing.T) {
	dir := t.TempDir()
	customDir := filepath.Join(dir, "custom")
	if err := os.MkdirAll(customDir, 0755); err != nil {
		t.Fatal(err)
	}
	jsonFile := filepath.Join(customDir, "devcontainer.json")
	content := `{"image": "python:3.11"}`
	if err := os.WriteFile(jsonFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Build: "test",
		Devcontainer: config.DevcontainerOption{
			Path:  "custom/devcontainer.json",
			IsSet: true,
		},
	}

	source, err := ResolveContainer(cfg, dir)
	if err != nil {
		t.Fatalf("ResolveContainer failed: %v", err)
	}
	if source.Type != "devcontainer" {
		t.Errorf("expected type 'devcontainer', got %q", source.Type)
	}
	if source.Image != "python:3.11" {
		t.Errorf("expected python:3.11, got %q", source.Image)
	}
}

func TestResolveContainer_NoDevcontainer_DefaultImage(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Build: "test",
		// No devcontainer.json exists, no config options
	}

	source, err := ResolveContainer(cfg, dir)
	if err != nil {
		t.Fatalf("ResolveContainer failed: %v", err)
	}
	if source.Type != "image" {
		t.Errorf("expected type 'image', got %q", source.Type)
	}
	if source.Image != DefaultImage {
		t.Errorf("expected default image %q, got %q", DefaultImage, source.Image)
	}
}

func TestResolveContainer_ImageTakesPriority(t *testing.T) {
	// Even if devcontainer.json exists, explicit image: should win
	dir := t.TempDir()
	devcontainerDir := filepath.Join(dir, ".devcontainer")
	if err := os.MkdirAll(devcontainerDir, 0755); err != nil {
		t.Fatal(err)
	}
	jsonFile := filepath.Join(devcontainerDir, "devcontainer.json")
	content := `{"image": "should-not-use"}`
	if err := os.WriteFile(jsonFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Build: "test",
		Image: "explicit:latest",
	}

	source, err := ResolveContainer(cfg, dir)
	if err != nil {
		t.Fatalf("ResolveContainer failed: %v", err)
	}
	if source.Image != "explicit:latest" {
		t.Errorf("expected explicit image to take priority, got %q", source.Image)
	}
}
