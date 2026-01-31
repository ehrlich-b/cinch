package worker

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ehrlich-b/cinch/internal/protocol"
)

// GitCloner handles git clone operations.
type GitCloner struct {
	// BaseDir is the base directory for clones.
	// If empty, uses ~/.cinch/work (for Docker mount compatibility).
	BaseDir string
}

// Clone clones a repository and checks out the specified commit.
// Returns the path to the cloned repository.
func (c *GitCloner) Clone(ctx context.Context, repo protocol.JobRepo) (string, error) {
	baseDir := c.BaseDir
	if baseDir == "" {
		// Use ~/.cinch/work instead of os.TempDir() because:
		// - macOS temp dirs (/var/folders/...) can't be mounted by Docker/Colima
		// - Home directory is always mountable
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory: %w", err)
		}
		baseDir = filepath.Join(home, ".cinch", "work")
		if err := os.MkdirAll(baseDir, 0755); err != nil {
			return "", fmt.Errorf("create work dir: %w", err)
		}
	}

	// Create work directory
	workDir, err := os.MkdirTemp(baseDir, "cinch-*")
	if err != nil {
		return "", fmt.Errorf("create work dir: %w", err)
	}

	// Build clone URL - use credential helper to avoid token in process list
	cloneURL := repo.CloneURL
	var askpassScript string
	if repo.CloneToken != "" {
		// Create a temporary askpass script that provides the token
		// This avoids exposing the token in the command line (visible in `ps`)
		askpassScript, err = createAskpassScript(repo.CloneToken)
		if err != nil {
			os.RemoveAll(workDir)
			return "", fmt.Errorf("create askpass script: %w", err)
		}
		defer os.Remove(askpassScript)

		// Add username to URL (password will come from askpass script)
		cloneURL, err = injectUsername(repo.CloneURL)
		if err != nil {
			os.RemoveAll(workDir)
			return "", fmt.Errorf("inject username: %w", err)
		}
	}

	// Clone with shallow depth
	// Use tag name for tag pushes, branch name for branch pushes
	refToClone := repo.Branch
	if repo.Tag != "" {
		refToClone = repo.Tag
	}
	args := []string{"clone", "--depth=1", "--branch", refToClone, cloneURL, workDir}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0", // Don't prompt for credentials
	)
	if askpassScript != "" {
		cmd.Env = append(cmd.Env, "GIT_ASKPASS="+askpassScript)
	}

	if output, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(workDir)
		return "", fmt.Errorf("git clone failed: %w\n%s", err, output)
	}

	// If commit is specified and different from branch HEAD, fetch and checkout
	if repo.Commit != "" {
		// First try to checkout the commit (might be the branch head)
		checkoutCmd := exec.CommandContext(ctx, "git", "checkout", repo.Commit)
		checkoutCmd.Dir = workDir

		if err := checkoutCmd.Run(); err != nil {
			// Commit not in shallow clone, need to fetch it
			fetchCmd := exec.CommandContext(ctx, "git", "fetch", "--depth=1", "origin", repo.Commit)
			fetchCmd.Dir = workDir
			if output, err := fetchCmd.CombinedOutput(); err != nil {
				os.RemoveAll(workDir)
				return "", fmt.Errorf("git fetch commit failed: %w\n%s", err, output)
			}

			// Now checkout
			checkoutCmd = exec.CommandContext(ctx, "git", "checkout", repo.Commit)
			checkoutCmd.Dir = workDir
			if output, err := checkoutCmd.CombinedOutput(); err != nil {
				os.RemoveAll(workDir)
				return "", fmt.Errorf("git checkout failed: %w\n%s", err, output)
			}
		}
	}

	return workDir, nil
}

// createAskpassScript creates a temporary executable script that outputs the token.
// This is used with GIT_ASKPASS to avoid putting tokens in command-line arguments
// where they would be visible in `ps` output.
func createAskpassScript(token string) (string, error) {
	// Create temp file with executable permissions
	f, err := os.CreateTemp("", "git-askpass-*.sh")
	if err != nil {
		return "", err
	}
	path := f.Name()

	// Write script that echoes the token
	// The script is simple: when git calls it asking for password, it outputs the token
	script := fmt.Sprintf("#!/bin/sh\necho '%s'\n", token)
	if _, err := f.WriteString(script); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	f.Close()

	// Make executable
	if err := os.Chmod(path, 0700); err != nil {
		os.Remove(path)
		return "", err
	}

	return path, nil
}

// injectUsername adds just the username to a clone URL (password comes from askpass).
func injectUsername(cloneURL string) (string, error) {
	u, err := url.Parse(cloneURL)
	if err != nil {
		return "", err
	}

	// Set just the username - password will be provided by GIT_ASKPASS
	u.User = url.User("x-access-token")

	return u.String(), nil
}

// CloneLocal clones a local repository (for testing).
func (c *GitCloner) CloneLocal(ctx context.Context, srcDir, branch string) (string, error) {
	baseDir := c.BaseDir
	if baseDir == "" {
		baseDir = os.TempDir()
	}

	workDir, err := os.MkdirTemp(baseDir, "cinch-*")
	if err != nil {
		return "", fmt.Errorf("create work dir: %w", err)
	}

	// Clone from local path
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", "--branch", branch, srcDir, workDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(workDir)
		return "", fmt.Errorf("git clone failed: %w\n%s", err, output)
	}

	return workDir, nil
}

// EnsureGit checks that git is available.
func EnsureGit() error {
	return CheckCommand("git")
}

// GetRepoRoot finds the root of the current git repository.
func GetRepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not in a git repository")
	}
	return filepath.Clean(string(output[:len(output)-1])), nil
}

// GetCurrentBranch returns the current git branch.
func GetCurrentBranch() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get branch: %w", err)
	}
	return string(output[:len(output)-1]), nil
}

// GetCurrentCommit returns the current git commit SHA.
func GetCurrentCommit() (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get commit: %w", err)
	}
	return string(output[:len(output)-1]), nil
}
