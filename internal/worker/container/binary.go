package container

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// version is set at build time via ldflags
var version = "dev"

// SetVersion allows main.go to pass the version at runtime
func SetVersion(v string) {
	version = v
}

// GetLinuxBinary returns the path to a Linux cinch binary for container injection.
// It checks:
// 1. ~/.cinch/bin/cinch-linux-{arch} (from install.sh)
// 2. Downloads from GitHub releases if not present or version mismatch
func GetLinuxBinary() (string, error) {
	arch := runtime.GOARCH
	return GetBinaryForPlatform("linux", arch)
}

// GetBinaryForPlatform returns the path to a cinch binary for the given OS/arch.
func GetBinaryForPlatform(targetOS, targetArch string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}

	binDir := filepath.Join(home, ".cinch", "bin")
	binaryName := fmt.Sprintf("cinch-%s-%s", targetOS, targetArch)
	binaryPath := filepath.Join(binDir, binaryName)

	// Check if binary exists and matches version
	if _, err := os.Stat(binaryPath); err == nil {
		// Binary exists - check version
		versionFile := filepath.Join(binDir, ".version")
		if data, err := os.ReadFile(versionFile); err == nil {
			installedVersion := strings.TrimSpace(string(data))
			if installedVersion == version {
				return binaryPath, nil
			}
			// Version mismatch - need to download
		}
		// No version file or mismatch - check if current version is "dev"
		if version == "dev" {
			// In dev mode, use whatever is there
			return binaryPath, nil
		}
	}

	// Binary doesn't exist or version mismatch - download it
	if version == "dev" {
		return "", fmt.Errorf("cinch-%s-%s not found in %s (run install.sh to download)", targetOS, targetArch, binDir)
	}

	if err := downloadBinary(binaryPath, targetOS, targetArch, version); err != nil {
		return "", err
	}

	// Update version file
	versionFile := filepath.Join(binDir, ".version")
	_ = os.WriteFile(versionFile, []byte(version), 0644)

	return binaryPath, nil
}

// downloadBinary downloads a cinch binary from GitHub releases.
func downloadBinary(dest, targetOS, targetArch, tag string) error {
	url := fmt.Sprintf("https://github.com/ehrlich-b/cinch/releases/download/%s/cinch-%s-%s",
		tag, targetOS, targetArch)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("create bin dir: %w", err)
	}

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("binary not found: %s (release may not exist)", url)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s returned %d", url, resp.StatusCode)
	}

	// Write to temp file first
	tmpFile := dest + ".tmp"
	f, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmpFile)
		return fmt.Errorf("write binary: %w", err)
	}
	f.Close()

	// Make executable and rename
	if err := os.Chmod(tmpFile, 0755); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("chmod: %w", err)
	}

	if err := os.Rename(tmpFile, dest); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("rename: %w", err)
	}

	return nil
}

// LatestRelease fetches the latest release tag from GitHub.
func LatestRelease() (string, error) {
	resp, err := http.Get("https://api.github.com/repos/ehrlich-b/cinch/releases/latest")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}

	return release.TagName, nil
}
