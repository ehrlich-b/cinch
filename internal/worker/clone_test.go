package worker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ehrlich-b/cinch/internal/protocol"
)

func TestInjectToken(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		token    string
		expected string
	}{
		{
			name:     "github https",
			url:      "https://github.com/user/repo.git",
			token:    "ghs_xxx",
			expected: "https://x-access-token:ghs_xxx@github.com/user/repo.git",
		},
		{
			name:     "custom host",
			url:      "https://git.example.com/user/repo.git",
			token:    "token123",
			expected: "https://x-access-token:token123@git.example.com/user/repo.git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := injectToken(tt.url, tt.token)
			if err != nil {
				t.Fatalf("injectToken failed: %v", err)
			}
			if result != tt.expected {
				t.Errorf("result = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestEnsureGit(t *testing.T) {
	// Git should be available in test environment
	if err := EnsureGit(); err != nil {
		t.Skipf("git not available: %v", err)
	}
}

func TestGetRepoRoot(t *testing.T) {
	// This test runs from within a git repo
	root, err := GetRepoRoot()
	if err != nil {
		t.Skipf("not in git repo: %v", err)
	}

	// Should end in "cinch"
	if filepath.Base(root) != "cinch" {
		t.Errorf("repo root = %s, expected to end in 'cinch'", root)
	}
}

func TestGetCurrentBranch(t *testing.T) {
	branch, err := GetCurrentBranch()
	if err != nil {
		t.Skipf("not in git repo: %v", err)
	}

	// Branch should be non-empty
	if branch == "" {
		t.Error("branch is empty")
	}
}

func TestGetCurrentCommit(t *testing.T) {
	commit, err := GetCurrentCommit()
	if err != nil {
		t.Skipf("not in git repo: %v", err)
	}

	// Commit should be 40 hex chars
	if len(commit) != 40 {
		t.Errorf("commit length = %d, want 40", len(commit))
	}
}

func TestCloneLocal(t *testing.T) {
	if err := EnsureGit(); err != nil {
		t.Skipf("git not available: %v", err)
	}

	// Create a temp repo to clone from
	srcDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = srcDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	// Configure user for commit
	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = srcDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git config email failed: %v", err)
	}

	cmd = exec.Command("git", "config", "user.name", "Test")
	cmd.Dir = srcDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git config name failed: %v", err)
	}

	// Create a file and commit
	if err := os.WriteFile(filepath.Join(srcDir, "test.txt"), []byte("hello"), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = srcDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git add failed: %v", err)
	}

	cmd = exec.Command("git", "commit", "-m", "initial")
	cmd.Dir = srcDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}

	// Clone it
	cloner := &GitCloner{}
	workDir, err := cloner.CloneLocal(context.Background(), srcDir, "master")
	if err != nil {
		// Try main branch
		workDir, err = cloner.CloneLocal(context.Background(), srcDir, "main")
		if err != nil {
			t.Fatalf("CloneLocal failed: %v", err)
		}
	}
	defer os.RemoveAll(workDir)

	// Check file exists
	if _, err := os.Stat(filepath.Join(workDir, "test.txt")); err != nil {
		t.Error("cloned file not found")
	}
}

func TestCloneInvalidURL(t *testing.T) {
	cloner := &GitCloner{}

	_, err := cloner.Clone(context.Background(), protocol.JobRepo{
		CloneURL: "https://github.com/nonexistent/really-nonexistent-repo-12345.git",
		Branch:   "main",
	})

	if err == nil {
		t.Error("expected error for invalid repo")
	}
}
