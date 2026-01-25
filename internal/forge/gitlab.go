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
	"os"
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
	// Extract just scheme://host from BaseURL or HTMLURL
	// (BaseURL might be the full project URL like https://gitlab.com/owner/repo)
	var baseURL string
	urlToParse := g.BaseURL
	if urlToParse == "" {
		urlToParse = repo.HTMLURL
	}
	if u, err := url.Parse(urlToParse); err == nil {
		baseURL = u.Scheme + "://" + u.Host
	} else {
		return errors.New("base URL not configured")
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

	// Get effective token (handles OAuth refresh if needed)
	token, isOAuth, err := g.getEffectiveToken()
	if err != nil {
		return fmt.Errorf("get token: %w", err)
	}

	// Use appropriate auth header
	if isOAuth {
		req.Header.Set("Authorization", "Bearer "+token)
	} else {
		req.Header.Set("PRIVATE-TOKEN", token)
	}
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

	// Get effective token (handles OAuth refresh if needed)
	token, _, err := g.getEffectiveToken()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("get token: %w", err)
	}

	// Token validity - OAuth tokens expire, PATs typically don't
	return token, time.Now().Add(1 * time.Hour), nil
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

// gitlabOAuthCredentials matches the JSON stored in repo.ForgeToken for OAuth fallback.
type gitlabOAuthCredentials struct {
	Type         string    `json:"type"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	BaseURL      string    `json:"base_url"`
}

// getEffectiveToken returns the token to use and whether it's OAuth (needs Bearer auth).
// If the stored token is OAuth credentials, it handles refresh if needed.
func (g *GitLab) getEffectiveToken() (token string, isOAuth bool, err error) {
	// Check if token is OAuth JSON
	if !strings.HasPrefix(g.Token, "{") {
		// Plain PAT
		return g.Token, false, nil
	}

	var creds gitlabOAuthCredentials
	if err := json.Unmarshal([]byte(g.Token), &creds); err != nil {
		// Not valid JSON, treat as plain token
		return g.Token, false, nil
	}

	if creds.Type != "oauth" {
		// Unknown type, treat as plain token
		return g.Token, false, nil
	}

	// Check if token is expired (with 5 minute buffer)
	if time.Now().Add(5 * time.Minute).After(creds.ExpiresAt) {
		// Need to refresh
		newCreds, err := g.refreshOAuthToken(creds)
		if err != nil {
			return "", true, fmt.Errorf("refresh OAuth token: %w", err)
		}
		return newCreds.AccessToken, true, nil
	}

	return creds.AccessToken, true, nil
}

// refreshOAuthToken refreshes an expired OAuth token.
func (g *GitLab) refreshOAuthToken(creds gitlabOAuthCredentials) (*gitlabOAuthCredentials, error) {
	clientID := os.Getenv("CINCH_GITLAB_CLIENT_ID")
	clientSecret := os.Getenv("CINCH_GITLAB_CLIENT_SECRET")

	if clientID == "" || clientSecret == "" {
		return nil, errors.New("CINCH_GITLAB_CLIENT_ID and CINCH_GITLAB_CLIENT_SECRET required for OAuth token refresh")
	}

	baseURL := creds.BaseURL
	if baseURL == "" {
		baseURL = "https://gitlab.com"
	}

	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)
	data.Set("refresh_token", creds.RefreshToken)
	data.Set("grant_type", "refresh_token")

	req, err := http.NewRequest("POST", baseURL+"/oauth/token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := g.Client
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("refresh failed: %s - %s", resp.Status, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decode refresh response: %w", err)
	}

	return &gitlabOAuthCredentials{
		Type:         "oauth",
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
		BaseURL:      baseURL,
	}, nil
}
