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
	"net/url"
	"strings"
	"time"
)

// Forgejo implements the Forge interface for Forgejo and Gitea.
// These forges have nearly identical APIs.
type Forgejo struct {
	// BaseURL is the Forgejo/Gitea instance URL (e.g., "https://forgejo.example.com")
	BaseURL string

	// Token is an access token with repo and status permissions.
	Token string

	// Client is the HTTP client to use. If nil, http.DefaultClient is used.
	Client *http.Client

	// IsGitea indicates this is a Gitea instance (affects header names).
	IsGitea bool
}

// Name returns "forgejo" or "gitea".
func (f *Forgejo) Name() string {
	if f.IsGitea {
		return "gitea"
	}
	return "forgejo"
}

// Identify returns true if the request has Forgejo or Gitea webhook headers.
func (f *Forgejo) Identify(r *http.Request) bool {
	return r.Header.Get("X-Forgejo-Event") != "" || r.Header.Get("X-Gitea-Event") != ""
}

// ParsePush parses a Forgejo/Gitea push webhook.
func (f *Forgejo) ParsePush(r *http.Request, secret string) (*PushEvent, error) {
	// Check event type (try both headers)
	event := r.Header.Get("X-Forgejo-Event")
	if event == "" {
		event = r.Header.Get("X-Gitea-Event")
	}
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
		sig := r.Header.Get("X-Forgejo-Signature")
		if sig == "" {
			sig = r.Header.Get("X-Gitea-Signature")
		}
		if err := f.verifySignature(body, sig, secret); err != nil {
			return nil, err
		}
	}

	// Parse payload
	var payload forgejoPushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}

	// Parse ref to determine if branch or tag
	var branch, tag string
	if strings.HasPrefix(payload.Ref, "refs/tags/") {
		tag = strings.TrimPrefix(payload.Ref, "refs/tags/")
	} else {
		branch = strings.TrimPrefix(payload.Ref, "refs/heads/")
	}

	// Determine forge type
	forgeType := "forgejo"
	if f.IsGitea {
		forgeType = "gitea"
	}

	return &PushEvent{
		Repo: &Repo{
			ForgeType: forgeType,
			Owner:     payload.Repository.Owner.Username,
			Name:      payload.Repository.Name,
			CloneURL:  payload.Repository.CloneURL,
			HTMLURL:   payload.Repository.HTMLURL,
			Private:   payload.Repository.Private,
		},
		Commit: payload.After,
		Ref:    payload.Ref,
		Branch: branch,
		Tag:    tag,
		Sender: payload.Sender.Username,
	}, nil
}

func (f *Forgejo) verifySignature(body []byte, signature, secret string) error {
	if signature == "" {
		return errors.New("missing signature header")
	}

	sig, err := hex.DecodeString(signature)
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

// PostStatus posts a commit status to Forgejo/Gitea.
func (f *Forgejo) PostStatus(ctx context.Context, repo *Repo, commit string, status *Status) error {
	baseURL := f.BaseURL
	if baseURL == "" {
		// Try to extract from repo HTMLURL
		if u, err := url.Parse(repo.HTMLURL); err == nil {
			baseURL = u.Scheme + "://" + u.Host
		} else {
			return errors.New("base URL not configured")
		}
	}

	apiURL := fmt.Sprintf("%s/api/v1/repos/%s/%s/statuses/%s",
		strings.TrimSuffix(baseURL, "/"), repo.Owner, repo.Name, commit)

	// Map our status state to Forgejo/Gitea's
	// Valid states: pending, success, error, failure, warning
	state := string(status.State)
	if status.State == StatusRunning {
		state = "pending"
	}

	payload := forgejoStatusPayload{
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

	req.Header.Set("Authorization", "token "+f.Token)
	req.Header.Set("Content-Type", "application/json")

	client := f.Client
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
		return fmt.Errorf("forgejo api error: %s - %s", resp.Status, string(respBody))
	}

	return nil
}

// CloneToken returns the token for cloning.
func (f *Forgejo) CloneToken(ctx context.Context, repo *Repo) (string, time.Time, error) {
	if !repo.Private {
		return "", time.Time{}, nil
	}
	// Forgejo/Gitea tokens don't expire
	return f.Token, time.Now().Add(24 * time.Hour), nil
}

// Forgejo/Gitea webhook payload types

type forgejoPushPayload struct {
	Ref        string `json:"ref"`
	Before     string `json:"before"`
	After      string `json:"after"`
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Private  bool   `json:"private"`
		HTMLURL  string `json:"html_url"`
		CloneURL string `json:"clone_url"`
		Owner    struct {
			Username string `json:"username"`
		} `json:"owner"`
	} `json:"repository"`
	Sender struct {
		Username string `json:"username"`
	} `json:"sender"`
}

type forgejoStatusPayload struct {
	State       string `json:"state"`
	Context     string `json:"context"`
	Description string `json:"description,omitempty"`
	TargetURL   string `json:"target_url,omitempty"`
}
