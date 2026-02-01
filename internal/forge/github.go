package forge

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// GitHub implements the Forge interface for GitHub.
type GitHub struct {
	// Token is a personal access token or installation token.
	// Needs repo:status scope for status posting.
	Token string

	// Client is the HTTP client to use. If nil, http.DefaultClient is used.
	Client *http.Client
}

// Name returns "github".
func (g *GitHub) Name() string {
	return "github"
}

// Identify returns true if the request has GitHub webhook headers.
func (g *GitHub) Identify(r *http.Request) bool {
	return r.Header.Get("X-GitHub-Event") != ""
}

// ParsePush parses a GitHub push webhook.
func (g *GitHub) ParsePush(r *http.Request, secret string) (*PushEvent, error) {
	// Check event type
	event := r.Header.Get("X-GitHub-Event")
	if event != "push" {
		return nil, fmt.Errorf("unexpected event type: %s", event)
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	// Verify signature
	if secret != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if err := g.verifySignature(body, sig, secret); err != nil {
			return nil, err
		}
	}

	// Parse payload
	var payload githubPushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}

	// Skip deletions
	if payload.Deleted {
		return nil, errors.New("ref deletion event")
	}

	// Parse ref to determine if branch or tag
	var branch, tag string
	if strings.HasPrefix(payload.Ref, "refs/tags/") {
		tag = strings.TrimPrefix(payload.Ref, "refs/tags/")
	} else {
		branch = strings.TrimPrefix(payload.Ref, "refs/heads/")
	}

	return &PushEvent{
		Repo: &Repo{
			ForgeType: "github",
			Owner:     payload.Repository.Owner.Login,
			Name:      payload.Repository.Name,
			CloneURL:  payload.Repository.CloneURL,
			HTMLURL:   payload.Repository.HTMLURL,
			Private:   payload.Repository.Private,
		},
		Commit: payload.After,
		Ref:    payload.Ref,
		Branch: branch,
		Tag:    tag,
		Sender: payload.Sender.Login,
	}, nil
}

func (g *GitHub) verifySignature(body []byte, signature, secret string) error {
	if signature == "" {
		return errors.New("missing signature header")
	}

	// Expected format: sha256=<hex>
	if !strings.HasPrefix(signature, "sha256=") {
		return errors.New("invalid signature format")
	}

	sig, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return errors.New("invalid signature encoding")
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)

	if !hmac.Equal(sig, expected) {
		return errors.New("signature mismatch")
	}

	return nil
}

// PostStatus posts a commit status to GitHub.
func (g *GitHub) PostStatus(ctx context.Context, repo *Repo, commit string, status *Status) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/statuses/%s",
		repo.Owner, repo.Name, commit)

	// Map our status state to GitHub's
	state := string(status.State)
	if status.State == StatusRunning {
		state = "pending" // GitHub doesn't have "running"
	}

	payload := githubStatusPayload{
		State:       state,
		Context:     status.Context,
		Description: status.Description,
		TargetURL:   status.TargetURL,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+g.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := g.Client
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github api error: %s - %s", resp.Status, string(respBody))
	}

	return nil
}

// CloneToken returns the token for cloning.
// For GitHub, we use the same token configured for API access.
func (g *GitHub) CloneToken(ctx context.Context, repo *Repo) (string, time.Time, error) {
	if !repo.Private {
		return "", time.Time{}, nil
	}
	// Token doesn't expire (PAT) or has ~1hr life (installation token)
	// Return 1 hour from now as a safe assumption
	return g.Token, time.Now().Add(time.Hour), nil
}

// CreateWebhook creates a webhook for the repository.
func (g *GitHub) CreateWebhook(ctx context.Context, repo *Repo, webhookURL, secret string) (int64, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/hooks",
		repo.Owner, repo.Name)

	payload := githubWebhookPayload{
		Name:   "web",
		Active: true,
		Events: []string{"push", "pull_request", "create"},
		Config: githubWebhookConfig{
			URL:         webhookURL,
			ContentType: "json",
			Secret:      secret,
			InsecureSSL: "0",
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}

	req.Header.Set("Authorization", "Bearer "+g.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := g.Client
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("github api error: %s - %s", resp.Status, string(respBody))
	}

	var result struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}

	return result.ID, nil
}

// ParsePullRequest parses a GitHub pull_request webhook.
func (g *GitHub) ParsePullRequest(r *http.Request, secret string) (*PullRequestEvent, error) {
	// Check event type
	event := r.Header.Get("X-GitHub-Event")
	if event != "pull_request" {
		return nil, fmt.Errorf("unexpected event type: %s", event)
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	// Verify signature
	if secret != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if err := g.verifySignature(body, sig, secret); err != nil {
			return nil, err
		}
	}

	// Parse payload
	var payload githubPRPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}

	// Only trigger on actionable events
	switch payload.Action {
	case "opened", "synchronize", "reopened":
		// These are the events we want to build
	default:
		return nil, fmt.Errorf("ignoring PR action: %s", payload.Action)
	}

	// Check if this is from a fork
	isFork := payload.PullRequest.Head.Repo.FullName != payload.Repository.FullName

	return &PullRequestEvent{
		Repo: &Repo{
			ForgeType: "github",
			Owner:     payload.Repository.Owner.Login,
			Name:      payload.Repository.Name,
			CloneURL:  payload.Repository.CloneURL,
			HTMLURL:   payload.Repository.HTMLURL,
			Private:   payload.Repository.Private,
		},
		Number:     payload.PullRequest.Number,
		Action:     payload.Action,
		Commit:     payload.PullRequest.Head.SHA,
		HeadBranch: payload.PullRequest.Head.Ref,
		BaseBranch: payload.PullRequest.Base.Ref,
		Title:      payload.PullRequest.Title,
		Sender:     payload.Sender.Login,
		IsFork:     isFork,
	}, nil
}

// GitHub webhook payload types

type githubPushPayload struct {
	Ref        string `json:"ref"`
	Before     string `json:"before"`
	After      string `json:"after"`
	Deleted    bool   `json:"deleted"`
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Private  bool   `json:"private"`
		HTMLURL  string `json:"html_url"`
		CloneURL string `json:"clone_url"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
}

type githubStatusPayload struct {
	State       string `json:"state"`
	Context     string `json:"context"`
	Description string `json:"description,omitempty"`
	TargetURL   string `json:"target_url,omitempty"`
}

type githubWebhookPayload struct {
	Name   string              `json:"name"`
	Active bool                `json:"active"`
	Events []string            `json:"events"`
	Config githubWebhookConfig `json:"config"`
}

type githubWebhookConfig struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Secret      string `json:"secret"`
	InsecureSSL string `json:"insecure_ssl"`
}

type githubPRPayload struct {
	Action      string `json:"action"`
	PullRequest struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Head   struct {
			SHA  string `json:"sha"`
			Ref  string `json:"ref"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	} `json:"pull_request"`
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Private  bool   `json:"private"`
		HTMLURL  string `json:"html_url"`
		CloneURL string `json:"clone_url"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
}
