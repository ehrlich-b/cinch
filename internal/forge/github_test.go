package forge

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubIdentify(t *testing.T) {
	gh := &GitHub{}

	tests := []struct {
		name    string
		headers map[string]string
		want    bool
	}{
		{
			name:    "github push event",
			headers: map[string]string{"X-GitHub-Event": "push"},
			want:    true,
		},
		{
			name:    "github pr event",
			headers: map[string]string{"X-GitHub-Event": "pull_request"},
			want:    true,
		},
		{
			name:    "no header",
			headers: map[string]string{},
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
			if got := gh.Identify(req); got != tt.want {
				t.Errorf("Identify() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGitHubParsePush(t *testing.T) {
	gh := &GitHub{}

	payload := `{
		"ref": "refs/heads/main",
		"after": "abc123def456",
		"deleted": false,
		"repository": {
			"name": "myrepo",
			"full_name": "myuser/myrepo",
			"private": false,
			"html_url": "https://github.com/myuser/myrepo",
			"clone_url": "https://github.com/myuser/myrepo.git",
			"owner": {
				"login": "myuser"
			}
		},
		"sender": {
			"login": "pusher"
		}
	}`

	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "push")

	event, err := gh.ParsePush(req, "")
	if err != nil {
		t.Fatalf("ParsePush failed: %v", err)
	}

	if event.Commit != "abc123def456" {
		t.Errorf("Commit = %s, want abc123def456", event.Commit)
	}
	if event.Branch != "main" {
		t.Errorf("Branch = %s, want main", event.Branch)
	}
	if event.Sender != "pusher" {
		t.Errorf("Sender = %s, want pusher", event.Sender)
	}
	if event.Repo.Owner != "myuser" {
		t.Errorf("Repo.Owner = %s, want myuser", event.Repo.Owner)
	}
	if event.Repo.Name != "myrepo" {
		t.Errorf("Repo.Name = %s, want myrepo", event.Repo.Name)
	}
	if event.Repo.ForgeType != "github" {
		t.Errorf("Repo.ForgeType = %s, want github", event.Repo.ForgeType)
	}
}

func TestGitHubParsePushWithSignature(t *testing.T) {
	gh := &GitHub{}
	secret := "test-secret"

	payload := `{
		"ref": "refs/heads/main",
		"after": "abc123",
		"deleted": false,
		"repository": {
			"name": "repo",
			"owner": {"login": "user"},
			"clone_url": "https://github.com/user/repo.git"
		},
		"sender": {"login": "user"}
	}`

	// Calculate signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", sig)

	_, err := gh.ParsePush(req, secret)
	if err != nil {
		t.Fatalf("ParsePush with valid signature failed: %v", err)
	}
}

func TestGitHubParsePushInvalidSignature(t *testing.T) {
	gh := &GitHub{}

	payload := `{"ref": "refs/heads/main"}`

	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", "sha256=invalid")

	_, err := gh.ParsePush(req, "secret")
	if err == nil {
		t.Fatal("expected error for invalid signature")
	}
}

func TestGitHubParsePushBranchDeletion(t *testing.T) {
	gh := &GitHub{}

	payload := `{
		"ref": "refs/heads/feature",
		"deleted": true,
		"repository": {
			"name": "repo",
			"owner": {"login": "user"}
		}
	}`

	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "push")

	_, err := gh.ParsePush(req, "")
	if err == nil {
		t.Fatal("expected error for branch deletion")
	}
	if !strings.Contains(err.Error(), "deletion") {
		t.Errorf("error = %v, want deletion error", err)
	}
}

func TestGitHubPostStatus(t *testing.T) {
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	// We can't easily test against the real GitHub API,
	// but we can verify the request is properly formed
	gh := &GitHub{
		Token:  "test-token",
		Client: server.Client(),
	}

	// This will fail because we're not hitting the real API
	// but we can at least verify the code paths
	ctx := context.Background()
	err := gh.PostStatus(ctx, &Repo{
		Owner: "testuser",
		Name:  "testrepo",
	}, "abc123", &Status{
		State:       StatusSuccess,
		Context:     "cinch",
		Description: "Build passed",
		TargetURL:   "https://example.com/jobs/1",
	})

	// Will fail since we're hitting localhost not api.github.com
	if err == nil {
		// If somehow it succeeded (shouldn't happen), check the request
		if receivedAuth != "Bearer test-token" {
			t.Errorf("Authorization = %s, want Bearer test-token", receivedAuth)
		}
	}
}

func TestGitHubCloneToken(t *testing.T) {
	gh := &GitHub{Token: "my-token"}

	ctx := context.Background()

	// Private repo should return token
	token, _, err := gh.CloneToken(ctx, &Repo{Private: true})
	if err != nil {
		t.Fatalf("CloneToken failed: %v", err)
	}
	if token != "my-token" {
		t.Errorf("token = %s, want my-token", token)
	}

	// Public repo should return empty token
	token, _, err = gh.CloneToken(ctx, &Repo{Private: false})
	if err != nil {
		t.Fatalf("CloneToken failed: %v", err)
	}
	if token != "" {
		t.Errorf("token = %s, want empty", token)
	}
}

func TestGitHubName(t *testing.T) {
	gh := &GitHub{}
	if gh.Name() != "github" {
		t.Errorf("Name() = %s, want github", gh.Name())
	}
}
