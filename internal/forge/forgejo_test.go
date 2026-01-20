package forge

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestForgejoIdentify(t *testing.T) {
	fg := &Forgejo{}

	tests := []struct {
		name    string
		headers map[string]string
		want    bool
	}{
		{
			name:    "forgejo push event",
			headers: map[string]string{"X-Forgejo-Event": "push"},
			want:    true,
		},
		{
			name:    "gitea push event",
			headers: map[string]string{"X-Gitea-Event": "push"},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/webhook", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			if got := fg.Identify(req); got != tt.want {
				t.Errorf("Identify() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestForgejoParsePush(t *testing.T) {
	fg := &Forgejo{}

	payload := `{
		"ref": "refs/heads/develop",
		"after": "def456abc789",
		"repository": {
			"name": "coolproject",
			"full_name": "devteam/coolproject",
			"private": true,
			"html_url": "https://forgejo.example.com/devteam/coolproject",
			"clone_url": "https://forgejo.example.com/devteam/coolproject.git",
			"owner": {
				"username": "devteam"
			}
		},
		"sender": {
			"username": "developer"
		}
	}`

	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Forgejo-Event", "push")

	event, err := fg.ParsePush(req, "")
	if err != nil {
		t.Fatalf("ParsePush failed: %v", err)
	}

	if event.Commit != "def456abc789" {
		t.Errorf("Commit = %s, want def456abc789", event.Commit)
	}
	if event.Branch != "develop" {
		t.Errorf("Branch = %s, want develop", event.Branch)
	}
	if event.Sender != "developer" {
		t.Errorf("Sender = %s, want developer", event.Sender)
	}
	if event.Repo.Owner != "devteam" {
		t.Errorf("Repo.Owner = %s, want devteam", event.Repo.Owner)
	}
	if event.Repo.Name != "coolproject" {
		t.Errorf("Repo.Name = %s, want coolproject", event.Repo.Name)
	}
	if event.Repo.ForgeType != "forgejo" {
		t.Errorf("Repo.ForgeType = %s, want forgejo", event.Repo.ForgeType)
	}
	if !event.Repo.Private {
		t.Error("Repo.Private = false, want true")
	}
}

func TestForgejoParsePushGitea(t *testing.T) {
	fg := &Forgejo{IsGitea: true}

	payload := `{
		"ref": "refs/heads/main",
		"after": "abc123",
		"repository": {
			"name": "repo",
			"private": false,
			"clone_url": "https://gitea.example.com/user/repo.git",
			"owner": {"username": "user"}
		},
		"sender": {"username": "user"}
	}`

	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Gitea-Event", "push")

	event, err := fg.ParsePush(req, "")
	if err != nil {
		t.Fatalf("ParsePush failed: %v", err)
	}

	if event.Repo.ForgeType != "gitea" {
		t.Errorf("Repo.ForgeType = %s, want gitea", event.Repo.ForgeType)
	}
}

func TestForgejoParsePushWithSignature(t *testing.T) {
	fg := &Forgejo{}
	secret := "webhook-secret"

	payload := `{
		"ref": "refs/heads/main",
		"after": "abc123",
		"repository": {
			"name": "repo",
			"owner": {"username": "user"},
			"clone_url": "https://forgejo.example.com/user/repo.git"
		},
		"sender": {"username": "user"}
	}`

	// Calculate signature (Forgejo uses raw hex, no prefix)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Forgejo-Event", "push")
	req.Header.Set("X-Forgejo-Signature", sig)

	_, err := fg.ParsePush(req, secret)
	if err != nil {
		t.Fatalf("ParsePush with valid signature failed: %v", err)
	}
}

func TestForgejoParsePushInvalidSignature(t *testing.T) {
	fg := &Forgejo{}

	payload := `{"ref": "refs/heads/main"}`

	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Forgejo-Event", "push")
	req.Header.Set("X-Forgejo-Signature", "invalidsig")

	_, err := fg.ParsePush(req, "secret")
	if err == nil {
		t.Fatal("expected error for invalid signature")
	}
}

func TestForgejoCloneToken(t *testing.T) {
	fg := &Forgejo{Token: "forgejo-token"}

	ctx := context.Background()

	// Private repo should return token
	token, _, err := fg.CloneToken(ctx, &Repo{Private: true})
	if err != nil {
		t.Fatalf("CloneToken failed: %v", err)
	}
	if token != "forgejo-token" {
		t.Errorf("token = %s, want forgejo-token", token)
	}

	// Public repo should return empty token
	token, _, err = fg.CloneToken(ctx, &Repo{Private: false})
	if err != nil {
		t.Fatalf("CloneToken failed: %v", err)
	}
	if token != "" {
		t.Errorf("token = %s, want empty", token)
	}
}

func TestForgejoName(t *testing.T) {
	fg := &Forgejo{}
	if fg.Name() != "forgejo" {
		t.Errorf("Name() = %s, want forgejo", fg.Name())
	}

	fg.IsGitea = true
	if fg.Name() != "gitea" {
		t.Errorf("Name() = %s, want gitea", fg.Name())
	}
}

func TestRepoFullName(t *testing.T) {
	repo := &Repo{Owner: "myorg", Name: "myproject"}
	if repo.FullName() != "myorg/myproject" {
		t.Errorf("FullName() = %s, want myorg/myproject", repo.FullName())
	}
}
