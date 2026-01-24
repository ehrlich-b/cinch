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
	// If empty, uses os.TempDir.
	BaseDir string
}

// Clone clones a repository and checks out the specified commit.
// Returns the path to the cloned repository.
func (c *GitCloner) Clone(ctx context.Context, repo protocol.JobRepo) (string, error) {
	baseDir := c.BaseDir
	if baseDir == "" {
		baseDir = os.TempDir()
	}

	// Create work directory
	workDir, err := os.MkdirTemp(baseDir, "cinch-*")
	if err != nil {
		return "", fmt.Errorf("create work dir: %w", err)
	}

	// Build clone URL with token if provided
	cloneURL := repo.CloneURL
	if repo.CloneToken != "" {
		cloneURL, err = injectToken(repo.CloneURL, repo.CloneToken)
		if err != nil {
			os.RemoveAll(workDir)
			return "", fmt.Errorf("inject token: %w", err)
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

// injectToken adds authentication token to a clone URL.
func injectToken(cloneURL, token string) (string, error) {
	u, err := url.Parse(cloneURL)
	if err != nil {
		return "", err
	}

	// Set user info with token
	u.User = url.UserPassword("x-access-token", token)

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
