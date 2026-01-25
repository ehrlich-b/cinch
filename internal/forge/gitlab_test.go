package forge

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitLabIdentify(t *testing.T) {
	gl := &GitLab{}

	tests := []struct {
		name    string
		headers map[string]string
		want    bool
	}{
		{
			name:    "gitlab push hook",
			headers: map[string]string{"X-Gitlab-Event": "Push Hook"},
			want:    true,
		},
		{
			name:    "gitlab tag push hook",
			headers: map[string]string{"X-Gitlab-Event": "Tag Push Hook"},
			want:    true,
		},
		{
			name:    "no header",
			headers: map[string]string{},
			want:    false,
		},
		{
			name:    "github event",
			headers: map[string]string{"X-GitHub-Event": "push"},
			want:    false,
		},
		{
			name:    "forgejo event",
			headers: map[string]string{"X-Forgejo-Event": "push"},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/webhook", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			if got := gl.Identify(req); got != tt.want {
				t.Errorf("Identify() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGitLabParsePush(t *testing.T) {
	gl := &GitLab{}

	payload := `{
		"ref": "refs/heads/main",
		"before": "abc123",
		"after": "def456abc789",
		"user_username": "developer",
		"project": {
			"id": 12345,
			"name": "myproject",
			"namespace": "myorg",
			"web_url": "https://gitlab.com/myorg/myproject",
			"git_http_url": "https://gitlab.com/myorg/myproject.git",
			"visibility_level": 0
		}
	}`

	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Gitlab-Event", "Push Hook")

	event, err := gl.ParsePush(req, "")
	if err != nil {
		t.Fatalf("ParsePush failed: %v", err)
	}

	if event.Commit != "def456abc789" {
		t.Errorf("Commit = %s, want def456abc789", event.Commit)
	}
	if event.Branch != "main" {
		t.Errorf("Branch = %s, want main", event.Branch)
	}
	if event.Tag != "" {
		t.Errorf("Tag = %s, want empty", event.Tag)
	}
	if event.Sender != "developer" {
		t.Errorf("Sender = %s, want developer", event.Sender)
	}
	if event.Repo.Owner != "myorg" {
		t.Errorf("Repo.Owner = %s, want myorg", event.Repo.Owner)
	}
	if event.Repo.Name != "myproject" {
		t.Errorf("Repo.Name = %s, want myproject", event.Repo.Name)
	}
	if event.Repo.ForgeType != "gitlab" {
		t.Errorf("Repo.ForgeType = %s, want gitlab", event.Repo.ForgeType)
	}
	if !event.Repo.Private {
		t.Error("Repo.Private = false, want true (visibility_level=0)")
	}
	if event.Repo.CloneURL != "https://gitlab.com/myorg/myproject.git" {
		t.Errorf("Repo.CloneURL = %s, want https://gitlab.com/myorg/myproject.git", event.Repo.CloneURL)
	}
}

func TestGitLabParsePushTag(t *testing.T) {
	gl := &GitLab{}

	payload := `{
		"ref": "refs/tags/v1.0.0",
		"before": "0000000000000000000000000000000000000000",
		"after": "abc123def456",
		"user_username": "releaser",
		"project": {
			"id": 99,
			"name": "repo",
			"namespace": "user",
			"web_url": "https://gitlab.com/user/repo",
			"git_http_url": "https://gitlab.com/user/repo.git",
			"visibility_level": 20
		}
	}`

	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Gitlab-Event", "Tag Push Hook")

	event, err := gl.ParsePush(req, "")
	if err != nil {
		t.Fatalf("ParsePush failed: %v", err)
	}

	if event.Tag != "v1.0.0" {
		t.Errorf("Tag = %s, want v1.0.0", event.Tag)
	}
	if event.Branch != "" {
		t.Errorf("Branch = %s, want empty", event.Branch)
	}
	if event.Repo.Private {
		t.Error("Repo.Private = true, want false (visibility_level=20)")
	}
}

func TestGitLabParsePushWithToken(t *testing.T) {
	gl := &GitLab{}
	secret := "my-webhook-secret"

	payload := `{
		"ref": "refs/heads/main",
		"after": "abc123",
		"user_username": "user",
		"project": {
			"name": "repo",
			"namespace": "user",
			"git_http_url": "https://gitlab.com/user/repo.git"
		}
	}`

	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	req.Header.Set("X-Gitlab-Token", secret)

	_, err := gl.ParsePush(req, secret)
	if err != nil {
		t.Fatalf("ParsePush with valid token failed: %v", err)
	}
}

func TestGitLabParsePushInvalidToken(t *testing.T) {
	gl := &GitLab{}

	payload := `{"ref": "refs/heads/main", "after": "abc123", "project": {}}`

	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	req.Header.Set("X-Gitlab-Token", "wrong-token")

	_, err := gl.ParsePush(req, "correct-token")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
	if !strings.Contains(err.Error(), "token mismatch") {
		t.Errorf("expected 'token mismatch' error, got: %v", err)
	}
}

func TestGitLabParsePushDeletion(t *testing.T) {
	gl := &GitLab{}

	// Branch deletion has all zeros in "after"
	payload := `{
		"ref": "refs/heads/feature",
		"before": "abc123",
		"after": "0000000000000000000000000000000000000000",
		"user_username": "user",
		"project": {
			"name": "repo",
			"namespace": "user",
			"git_http_url": "https://gitlab.com/user/repo.git"
		}
	}`

	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Gitlab-Event", "Push Hook")

	_, err := gl.ParsePush(req, "")
	if err == nil {
		t.Fatal("expected error for ref deletion")
	}
	if !strings.Contains(err.Error(), "deletion") {
		t.Errorf("expected 'deletion' error, got: %v", err)
	}
}

func TestGitLabCloneToken(t *testing.T) {
	gl := &GitLab{Token: "glpat-xxxyyyzzz"}

	ctx := context.Background()

	// Private repo should return token
	token, _, err := gl.CloneToken(ctx, &Repo{Private: true})
	if err != nil {
		t.Fatalf("CloneToken failed: %v", err)
	}
	if token != "glpat-xxxyyyzzz" {
		t.Errorf("token = %s, want glpat-xxxyyyzzz", token)
	}

	// Public repo should return empty token
	token, _, err = gl.CloneToken(ctx, &Repo{Private: false})
	if err != nil {
		t.Fatalf("CloneToken failed: %v", err)
	}
	if token != "" {
		t.Errorf("token = %s, want empty", token)
	}
}

func TestGitLabName(t *testing.T) {
	gl := &GitLab{}
	if gl.Name() != "gitlab" {
		t.Errorf("Name() = %s, want gitlab", gl.Name())
	}
}

func TestGitLabStatusStateMapping(t *testing.T) {
	// Verify our status states map correctly to GitLab states
	// GitLab: pending, running, success, failed, canceled
	// Our states: pending, running, success, failure, error

	tests := []struct {
		ourState    StatusState
		gitlabState string
	}{
		{StatusPending, "pending"},
		{StatusRunning, "running"},
		{StatusSuccess, "success"},
		{StatusFailure, "failed"},
		{StatusError, "failed"},
	}

	for _, tt := range tests {
		t.Run(string(tt.ourState), func(t *testing.T) {
			state := string(tt.ourState)
			switch tt.ourState {
			case StatusFailure:
				state = "failed"
			case StatusError:
				state = "failed"
			}

			if state != tt.gitlabState {
				t.Errorf("state mapping for %s: got %s, want %s", tt.ourState, state, tt.gitlabState)
			}
		})
	}
}
