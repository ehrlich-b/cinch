package container

import (
	"os"
	"path/filepath"
	"testing"
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
