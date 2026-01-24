package container

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ehrlich-b/cinch/internal/config"
)

// DefaultImage is used when no devcontainer or Dockerfile is found.
const DefaultImage = "ubuntu:22.04"

// ImageSource describes where the container image comes from.
type ImageSource struct {
	// Type is "image", "dockerfile", "devcontainer", or "bare-metal"
	Type string

	// Image is the image name (for Type="image") or tag (for built images)
	Image string

	// Dockerfile path (for Type="dockerfile" or "devcontainer")
	Dockerfile string

	// Context directory for docker build
	Context string
}

// ResolveContainer figures out what container image to use based on config.
// Priority: image > dockerfile > container:none > devcontainer:false > devcontainer:path
func ResolveContainer(cfg *config.Config, repoDir string) (*ImageSource, error) {
	// 1. Explicit image specified
	if cfg.Image != "" {
		return &ImageSource{
			Type:  "image",
			Image: cfg.Image,
		}, nil
	}

	// 2. Explicit dockerfile specified
	if cfg.Dockerfile != "" {
		dockerfilePath := cfg.Dockerfile
		if !filepath.IsAbs(dockerfilePath) {
			dockerfilePath = filepath.Join(repoDir, dockerfilePath)
		}
		return &ImageSource{
			Type:       "dockerfile",
			Dockerfile: dockerfilePath,
			Context:    filepath.Dir(dockerfilePath),
		}, nil
	}

	// 3. Bare metal escape hatch
	if cfg.Container == "none" {
		return &ImageSource{
			Type: "bare-metal",
		}, nil
	}

	// 4. Devcontainer disabled - use default image
	if cfg.Devcontainer.Disabled {
		return &ImageSource{
			Type:  "image",
			Image: DefaultImage,
		}, nil
	}

	// 5. Devcontainer path (default or custom)
	devcontainerPath := cfg.Devcontainer.EffectivePath()
	if !filepath.IsAbs(devcontainerPath) {
		devcontainerPath = filepath.Join(repoDir, devcontainerPath)
	}

	// Check if devcontainer.json exists
	if _, err := os.Stat(devcontainerPath); err == nil {
		return parseDevcontainer(devcontainerPath, repoDir)
	}

	// No devcontainer.json found - use default image
	return &ImageSource{
		Type:  "image",
		Image: DefaultImage,
	}, nil
}

// DetectImage figures out what container image to use for a repo.
// Deprecated: Use ResolveContainer with config instead.
// Priority: .devcontainer/devcontainer.json > .devcontainer/Dockerfile > Dockerfile > default
func DetectImage(repoDir string) (*ImageSource, error) {
	// Check for devcontainer.json
	devcontainerJSON := filepath.Join(repoDir, ".devcontainer", "devcontainer.json")
	if _, err := os.Stat(devcontainerJSON); err == nil {
		return parseDevcontainer(devcontainerJSON, repoDir)
	}

	// Check for .devcontainer/Dockerfile
	devcontainerDockerfile := filepath.Join(repoDir, ".devcontainer", "Dockerfile")
	if _, err := os.Stat(devcontainerDockerfile); err == nil {
		return &ImageSource{
			Type:       "dockerfile",
			Dockerfile: devcontainerDockerfile,
			Context:    filepath.Join(repoDir, ".devcontainer"),
		}, nil
	}

	// Check for root Dockerfile
	rootDockerfile := filepath.Join(repoDir, "Dockerfile")
	if _, err := os.Stat(rootDockerfile); err == nil {
		return &ImageSource{
			Type:       "dockerfile",
			Dockerfile: rootDockerfile,
			Context:    repoDir,
		}, nil
	}

	// Default image
	return &ImageSource{
		Type:  "image",
		Image: DefaultImage,
	}, nil
}

// devcontainerConfig is a minimal parse of devcontainer.json
type devcontainerConfig struct {
	Image string `json:"image"`
	Build *struct {
		Dockerfile string `json:"dockerfile"`
		Context    string `json:"context"`
	} `json:"build"`
	DockerFile string `json:"dockerFile"` // legacy field
}

func parseDevcontainer(jsonPath, repoDir string) (*ImageSource, error) {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("read devcontainer.json: %w", err)
	}

	var config devcontainerConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse devcontainer.json: %w", err)
	}

	devcontainerDir := filepath.Join(repoDir, ".devcontainer")

	// Direct image reference
	if config.Image != "" {
		return &ImageSource{
			Type:  "devcontainer",
			Image: config.Image,
		}, nil
	}

	// Build with dockerfile
	if config.Build != nil && config.Build.Dockerfile != "" {
		context := devcontainerDir
		if config.Build.Context != "" {
			context = filepath.Join(devcontainerDir, config.Build.Context)
		}
		return &ImageSource{
			Type:       "devcontainer",
			Dockerfile: filepath.Join(devcontainerDir, config.Build.Dockerfile),
			Context:    context,
		}, nil
	}

	// Legacy dockerFile field
	if config.DockerFile != "" {
		return &ImageSource{
			Type:       "devcontainer",
			Dockerfile: filepath.Join(devcontainerDir, config.DockerFile),
			Context:    devcontainerDir,
		}, nil
	}

	// devcontainer.json exists but no image specified - check for Dockerfile
	devcontainerDockerfile := filepath.Join(devcontainerDir, "Dockerfile")
	if _, err := os.Stat(devcontainerDockerfile); err == nil {
		return &ImageSource{
			Type:       "devcontainer",
			Dockerfile: devcontainerDockerfile,
			Context:    devcontainerDir,
		}, nil
	}

	return nil, fmt.Errorf("devcontainer.json found but no image or dockerfile specified")
}

// PrepareImage ensures the image is ready to use.
// For direct images, pulls if needed. For dockerfiles, builds.
// For bare-metal, returns empty string (no container).
func PrepareImage(ctx context.Context, source *ImageSource, jobID string, stdout, stderr io.Writer) (string, error) {
	switch source.Type {
	case "image":
		// Pull image (docker will skip if cached)
		cmd := fmt.Sprintf("docker pull %s", source.Image)
		fmt.Fprintf(stdout, "$ %s\n", cmd)
		d := &Docker{Image: source.Image, Stdout: stdout, Stderr: stderr}
		if err := d.Pull(ctx); err != nil {
			return "", fmt.Errorf("pull image: %w", err)
		}
		return source.Image, nil

	case "dockerfile", "devcontainer":
		// Build image with job-specific tag
		tag := fmt.Sprintf("cinch-build-%s", jobID)
		fmt.Fprintf(stdout, "$ docker build -f %s -t %s %s\n", source.Dockerfile, tag, source.Context)
		if err := Build(ctx, source.Dockerfile, source.Context, tag, stdout, stderr); err != nil {
			return "", fmt.Errorf("build image: %w", err)
		}
		return tag, nil

	case "bare-metal":
		// No container preparation needed
		fmt.Fprintf(stdout, "Running in bare-metal mode (no container)\n")
		return "", nil

	default:
		return "", fmt.Errorf("unknown image source type: %s", source.Type)
	}
}
