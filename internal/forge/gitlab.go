package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GitLab implements the Forge interface for GitLab (both gitlab.com and self-hosted).
type GitLab struct {
	// BaseURL is the GitLab instance URL (e.g., "https://gitlab.com" or self-hosted)
	BaseURL string

	// Token is a Project Access Token (glpat-xxx) with api scope.
	Token string

	// ProjectID is the numeric project ID for status API calls.
	// Required for PostStatus. Can be left empty for webhook parsing.
	ProjectID int

	// Client is the HTTP client to use. If nil, http.DefaultClient is used.
	Client *http.Client
}

// Name returns "gitlab".
func (g *GitLab) Name() string {
	return "gitlab"
}

// Identify returns true if the request has GitLab webhook headers.
func (g *GitLab) Identify(r *http.Request) bool {
	return r.Header.Get("X-Gitlab-Event") != ""
}

// ParsePush parses a GitLab push webhook.
// GitLab uses a simple token comparison (X-Gitlab-Token header) instead of HMAC.
func (g *GitLab) ParsePush(r *http.Request, secret string) (*PushEvent, error) {
	// Check event type
	event := r.Header.Get("X-Gitlab-Event")
	if event != "Push Hook" && event != "Tag Push Hook" {
		return nil, fmt.Errorf("unexpected event type: %s", event)
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	// Verify token (GitLab uses simple token comparison, not HMAC)
	if secret != "" {
		token := r.Header.Get("X-Gitlab-Token")
		if token != secret {
			return nil, errors.New("token mismatch")
		}
	}

	// Parse payload
	var payload gitlabPushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}

	// Skip deletions (when after is all zeros)
	if payload.After == "0000000000000000000000000000000000000000" {
		return nil, errors.New("ref deletion event")
	}

	// Parse ref to determine if branch or tag
	var branch, tag string
	if strings.HasPrefix(payload.Ref, "refs/tags/") {
		tag = strings.TrimPrefix(payload.Ref, "refs/tags/")
	} else {
		branch = strings.TrimPrefix(payload.Ref, "refs/heads/")
	}

	// Build clone URL from project URL
	cloneURL := payload.Project.GitHTTPURL

	return &PushEvent{
		Repo: &Repo{
			ForgeType: "gitlab",
			Owner:     payload.Project.Namespace,
			Name:      payload.Project.Name,
			CloneURL:  cloneURL,
			HTMLURL:   payload.Project.WebURL,
			Private:   payload.Project.VisibilityLevel != 20, // 20 = public
		},
		Commit: payload.After,
		Ref:    payload.Ref,
		Branch: branch,
		Tag:    tag,
		Sender: payload.UserUsername,
	}, nil
}

// PostStatus posts a commit status to GitLab.
// Uses the project ID for the API call.
func (g *GitLab) PostStatus(ctx context.Context, repo *Repo, commit string, status *Status) error {
	baseURL := g.BaseURL
	if baseURL == "" {
		// Try to extract from repo HTMLURL
		if u, err := url.Parse(repo.HTMLURL); err == nil {
			baseURL = u.Scheme + "://" + u.Host
		} else {
			return errors.New("base URL not configured")
		}
	}

	// GitLab status API uses project ID or URL-encoded path
	// We'll use URL-encoded path if ProjectID is not set
	var apiURL string
	if g.ProjectID != 0 {
		apiURL = fmt.Sprintf("%s/api/v4/projects/%d/statuses/%s",
			strings.TrimSuffix(baseURL, "/"), g.ProjectID, commit)
	} else {
		// Use URL-encoded path: owner%2Fname
		projectPath := url.PathEscape(repo.Owner + "/" + repo.Name)
		apiURL = fmt.Sprintf("%s/api/v4/projects/%s/statuses/%s",
			strings.TrimSuffix(baseURL, "/"), projectPath, commit)
	}

	// Map our status state to GitLab's
	// GitLab states: pending, running, success, failed, canceled
	state := string(status.State)
	switch status.State {
	case StatusFailure:
		state = "failed"
	case StatusError:
		state = "failed"
	}

	payload := gitlabStatusPayload{
		State:       state,
		Context:     status.Context,
		Description: status.Description,
		TargetURL:   status.TargetURL,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("PRIVATE-TOKEN", g.Token)
	req.Header.Set("Content-Type", "application/json")

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
		return fmt.Errorf("gitlab api error: %s - %s", resp.Status, string(respBody))
	}

	return nil
}

// CloneToken returns the token for cloning.
func (g *GitLab) CloneToken(ctx context.Context, repo *Repo) (string, time.Time, error) {
	if !repo.Private {
		return "", time.Time{}, nil
	}
	// GitLab PATs don't expire (unless configured)
	return g.Token, time.Now().Add(24 * time.Hour), nil
}

// GitLab webhook payload types

type gitlabPushPayload struct {
	Ref          string `json:"ref"`
	Before       string `json:"before"`
	After        string `json:"after"`
	UserUsername string `json:"user_username"`
	Project      struct {
		ID              int    `json:"id"`
		Name            string `json:"name"`
		Namespace       string `json:"namespace"`
		WebURL          string `json:"web_url"`
		GitHTTPURL      string `json:"git_http_url"`
		VisibilityLevel int    `json:"visibility_level"` // 0=private, 10=internal, 20=public
	} `json:"project"`
}

type gitlabStatusPayload struct {
	State       string `json:"state"`
	Context     string `json:"name"` // GitLab uses "name" for status context
	Description string `json:"description,omitempty"`
	TargetURL   string `json:"target_url,omitempty"`
}
