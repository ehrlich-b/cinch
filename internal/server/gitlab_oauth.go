package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ehrlich-b/cinch/internal/storage"
	"github.com/golang-jwt/jwt/v4"
)

// GitLabOAuthConfig holds GitLab OAuth configuration.
type GitLabOAuthConfig struct {
	ClientID     string // GitLab OAuth Application ID
	ClientSecret string // GitLab OAuth Application Secret
	BaseURL      string // "https://gitlab.com" or self-hosted
}

// GitLabOAuthHandler handles GitLab OAuth and repo setup.
type GitLabOAuthHandler struct {
	config     GitLabOAuthConfig
	appBaseURL string // Cinch app base URL for callbacks
	jwtSecret  []byte
	storage    storage.Storage
	log        *slog.Logger

	// Temporary storage for OAuth tokens (keyed by username)
	// In production, use Redis or database
	oauthTokens   map[string]*gitlabOAuthToken
	oauthTokensMu sync.RWMutex
}

type gitlabOAuthToken struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	GitLabURL    string // Base URL (for self-hosted support)
}

// GitLabOAuthCredentials is stored in repo.ForgeToken when using OAuth fallback.
// The forge code checks if ForgeToken is JSON with Type="oauth" to know it needs refresh handling.
type GitLabOAuthCredentials struct {
	Type         string    `json:"type"` // Always "oauth"
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	BaseURL      string    `json:"base_url"`
}

// NewGitLabOAuthHandler creates a new GitLab OAuth handler.
func NewGitLabOAuthHandler(cfg GitLabOAuthConfig, appBaseURL string, jwtSecret []byte, store storage.Storage, log *slog.Logger) *GitLabOAuthHandler {
	if log == nil {
		log = slog.Default()
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://gitlab.com"
	}
	return &GitLabOAuthHandler{
		config:      cfg,
		appBaseURL:  strings.TrimSuffix(appBaseURL, "/"),
		jwtSecret:   jwtSecret,
		storage:     store,
		log:         log,
		oauthTokens: make(map[string]*gitlabOAuthToken),
	}
}

// IsConfigured returns true if GitLab OAuth is configured.
func (h *GitLabOAuthHandler) IsConfigured() bool {
	return h.config.ClientID != "" && h.config.ClientSecret != ""
}

// HandleLogin initiates GitLab OAuth flow.
// GET /auth/gitlab
func (h *GitLabOAuthHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if !h.IsConfigured() {
		http.Error(w, "GitLab OAuth not configured", http.StatusServiceUnavailable)
		return
	}

	// Create signed state JWT with CSRF token
	state, err := h.createOAuthState()
	if err != nil {
		h.log.Error("failed to create OAuth state", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Build GitLab authorization URL
	// Scopes: api (for webhook creation), read_user (for profile)
	authURL := fmt.Sprintf("%s/oauth/authorize?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=%s",
		h.config.BaseURL,
		url.QueryEscape(h.config.ClientID),
		url.QueryEscape(h.appBaseURL+"/auth/gitlab/callback"),
		url.QueryEscape("api read_user"),
		url.QueryEscape(state))

	h.log.Info("redirecting to GitLab OAuth", "url", h.config.BaseURL)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// HandleCallback handles GitLab OAuth callback.
// GET /auth/gitlab/callback
func (h *GitLabOAuthHandler) HandleCallback(w http.ResponseWriter, r *http.Request, username string) {
	// Validate state parameter
	stateToken := r.URL.Query().Get("state")
	if err := h.validateOAuthState(stateToken); err != nil {
		h.log.Error("invalid OAuth state", "error", err)
		http.Error(w, "Invalid state parameter", http.StatusBadRequest)
		return
	}

	// Check for error
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		errDesc := r.URL.Query().Get("error_description")
		h.log.Warn("GitLab OAuth error", "error", errParam, "description", errDesc)
		// Redirect to app with error
		http.Redirect(w, r, "/?error=gitlab_oauth_denied", http.StatusFound)
		return
	}

	// Get authorization code
	code := r.URL.Query().Get("code")
	if code == "" {
		h.log.Error("missing authorization code")
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}

	// Exchange code for token
	token, err := h.exchangeCode(code)
	if err != nil {
		h.log.Error("failed to exchange code", "error", err)
		http.Error(w, "Failed to exchange authorization code", http.StatusInternalServerError)
		return
	}

	// Store token temporarily for this user (for immediate web UI use)
	h.oauthTokensMu.Lock()
	h.oauthTokens[username] = token
	h.oauthTokensMu.Unlock()

	// Also persist to database (for CLI use later)
	ctx := r.Context()
	user, err := h.storage.GetOrCreateUser(ctx, username)
	if err != nil {
		h.log.Error("failed to get/create user", "error", err, "user", username)
	} else {
		creds := GitLabOAuthCredentials{
			Type:         "oauth",
			AccessToken:  token.AccessToken,
			RefreshToken: token.RefreshToken,
			ExpiresAt:    token.ExpiresAt,
			BaseURL:      token.GitLabURL,
		}
		credsJSON, _ := json.Marshal(creds)
		if err := h.storage.UpdateUserGitLabCredentials(ctx, user.ID, string(credsJSON)); err != nil {
			h.log.Error("failed to save GitLab credentials", "error", err, "user", username)
		} else {
			h.log.Info("GitLab credentials saved to database", "user", username)
		}
	}

	h.log.Info("GitLab OAuth successful", "user", username)

	// Redirect to project selector
	http.Redirect(w, r, "/gitlab/onboard", http.StatusFound)
}

// HandleProjects lists user's GitLab projects.
// GET /api/gitlab/projects
func (h *GitLabOAuthHandler) HandleProjects(w http.ResponseWriter, r *http.Request, username string) {
	token := h.getOAuthToken(username)
	if token == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "GitLab not connected"})
		return
	}

	// Fetch user's projects from GitLab
	projects, err := h.fetchProjects(token)
	if err != nil {
		h.log.Error("failed to fetch projects", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to fetch projects"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(projects)
}

// HandleSetup creates webhook and attempts PAT creation.
// POST /api/gitlab/setup
func (h *GitLabOAuthHandler) HandleSetup(w http.ResponseWriter, r *http.Request, username string) {
	token := h.getOAuthToken(username)
	if token == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "GitLab not connected"})
		return
	}

	var req struct {
		ProjectID   int    `json:"project_id"`
		ProjectPath string `json:"project_path"` // "owner/name"
		ManualToken string `json:"manual_token"` // User-provided PAT (fallback)
		UseOAuth    bool   `json:"use_oauth"`    // Use OAuth token instead of PAT
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Parse project path
	parts := strings.SplitN(req.ProjectPath, "/", 2)
	if len(parts) != 2 {
		http.Error(w, "Invalid project path", http.StatusBadRequest)
		return
	}
	owner, name := parts[0], parts[1]

	// Generate webhook secret
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		h.log.Error("failed to generate webhook secret", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	webhookSecret := base64.URLEncoding.EncodeToString(secretBytes)

	// Create webhook using OAuth token
	webhookURL := h.appBaseURL + "/webhooks"
	if err := h.createWebhook(token, req.ProjectID, webhookURL, webhookSecret); err != nil {
		h.log.Error("failed to create webhook", "error", err, "project", req.ProjectPath)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to create webhook: %v", err)})
		return
	}

	h.log.Info("webhook created", "project", req.ProjectPath)

	// Determine the token for status posting
	var forgeToken string
	var tokenType string // "pat", "manual", or "oauth"
	var needsFallback bool

	if req.ManualToken != "" {
		// User provided a manual token
		forgeToken = req.ManualToken
		tokenType = "manual"
	} else if req.UseOAuth {
		// User chose to use OAuth token (acts as them)
		oauthCreds := GitLabOAuthCredentials{
			Type:         "oauth",
			AccessToken:  token.AccessToken,
			RefreshToken: token.RefreshToken,
			ExpiresAt:    token.ExpiresAt,
			BaseURL:      token.GitLabURL,
		}
		credJSON, err := json.Marshal(oauthCreds)
		if err != nil {
			h.log.Error("failed to marshal OAuth credentials", "error", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		forgeToken = string(credJSON)
		tokenType = "oauth"
		h.log.Info("using OAuth token (acts as user)", "project", req.ProjectPath)
	} else {
		// Try to create a Project Access Token
		pat, err := h.createProjectAccessToken(token, req.ProjectID)
		if err != nil {
			// Check if it's a 403 (free tier limitation)
			if strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "not allowed") {
				h.log.Info("PAT creation not available (free tier), offering fallback options", "project", req.ProjectPath)
				needsFallback = true
			} else {
				h.log.Error("failed to create PAT", "error", err, "project", req.ProjectPath)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to create access token: %v", err)})
				return
			}
		} else {
			forgeToken = pat
			tokenType = "pat"
			h.log.Info("Project Access Token created automatically", "project", req.ProjectPath)
		}
	}

	// If we need fallback, respond with options
	if needsFallback {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted) // 202 = webhook created, but needs more info
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":          "needs_token",
			"message":         "Webhook created! We couldn't create a bot token (requires GitLab Premium or self-hosted).",
			"webhook_created": true,
			"options": []map[string]any{
				{
					"id":          "manual",
					"label":       "Create a token manually (recommended)",
					"description": "You control the credential. Releases will show as published by you.",
					"token_url":   fmt.Sprintf("%s/%s/-/settings/access_tokens", token.GitLabURL, req.ProjectPath),
					"scopes":      []string{"api"},
					"expiry_hint": "1 year recommended",
				},
				{
					"id":          "oauth",
					"label":       "Use my current session",
					"description": "We'll act on your behalf. Releases will show as published by you. You can revoke access anytime in GitLab settings.",
				},
			},
		})
		return
	}

	// Create repo in storage
	cloneURL := fmt.Sprintf("%s/%s.git", token.GitLabURL, req.ProjectPath)
	htmlURL := fmt.Sprintf("%s/%s", token.GitLabURL, req.ProjectPath)

	repo := &storage.Repo{
		ID:            fmt.Sprintf("r_%d", time.Now().UnixNano()),
		ForgeType:     storage.ForgeTypeGitLab,
		Owner:         owner,
		Name:          name,
		CloneURL:      cloneURL,
		HTMLURL:       htmlURL,
		WebhookSecret: webhookSecret,
		ForgeToken:    forgeToken,
		CreatedAt:     time.Now(),
	}

	if err := h.storage.CreateRepo(ctx, repo); err != nil {
		h.log.Error("failed to create repo", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to save repository"})
		return
	}

	// Clean up OAuth token (no longer needed)
	h.oauthTokensMu.Lock()
	delete(h.oauthTokens, username)
	h.oauthTokensMu.Unlock()

	h.log.Info("GitLab repo setup complete", "repo", req.ProjectPath, "token_type", tokenType)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":          "success",
		"repo_id":         repo.ID,
		"webhook_created": true,
		"token_type":      tokenType, // "pat", "manual", or "oauth"
	})
}

// --- Helper methods ---

func (h *GitLabOAuthHandler) getOAuthToken(username string) *gitlabOAuthToken {
	// First check in-memory cache
	h.oauthTokensMu.RLock()
	token := h.oauthTokens[username]
	h.oauthTokensMu.RUnlock()
	if token != nil {
		return token
	}

	// Fall back to database
	ctx := context.Background()
	user, err := h.storage.GetUserByName(ctx, username)
	if err != nil || user == nil || user.GitLabCredentials == "" {
		return nil
	}

	// Parse stored credentials
	var creds GitLabOAuthCredentials
	if err := json.Unmarshal([]byte(user.GitLabCredentials), &creds); err != nil {
		h.log.Error("failed to parse stored GitLab credentials", "error", err, "user", username)
		return nil
	}

	// Convert to token struct and cache in memory
	token = &gitlabOAuthToken{
		AccessToken:  creds.AccessToken,
		RefreshToken: creds.RefreshToken,
		ExpiresAt:    creds.ExpiresAt,
		GitLabURL:    creds.BaseURL,
	}

	h.oauthTokensMu.Lock()
	h.oauthTokens[username] = token
	h.oauthTokensMu.Unlock()

	return token
}

func (h *GitLabOAuthHandler) createOAuthState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	claims := jwt.MapClaims{
		"csrf": base64.URLEncoding.EncodeToString(b),
		"exp":  time.Now().Add(10 * time.Minute).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(h.jwtSecret)
}

func (h *GitLabOAuthHandler) validateOAuthState(stateToken string) error {
	token, err := jwt.Parse(stateToken, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return h.jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return fmt.Errorf("invalid state token: %w", err)
	}
	return nil
}

func (h *GitLabOAuthHandler) exchangeCode(code string) (*gitlabOAuthToken, error) {
	data := url.Values{}
	data.Set("client_id", h.config.ClientID)
	data.Set("client_secret", h.config.ClientSecret)
	data.Set("code", code)
	data.Set("grant_type", "authorization_code")
	data.Set("redirect_uri", h.appBaseURL+"/auth/gitlab/callback")

	req, err := http.NewRequest("POST", h.config.BaseURL+"/oauth/token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token request returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("no access token in response")
	}

	return &gitlabOAuthToken{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
		GitLabURL:    h.config.BaseURL,
	}, nil
}

type gitlabProject struct {
	ID                int    `json:"id"`
	Name              string `json:"name"`
	PathWithNamespace string `json:"path_with_namespace"`
	WebURL            string `json:"web_url"`
	Visibility        string `json:"visibility"`
	DefaultBranch     string `json:"default_branch"`
}

func (h *GitLabOAuthHandler) fetchProjects(token *gitlabOAuthToken) ([]gitlabProject, error) {
	req, err := http.NewRequest("GET", token.GitLabURL+"/api/v4/projects?membership=true&per_page=100&order_by=last_activity_at", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("projects request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("projects request returned %d: %s", resp.StatusCode, string(body))
	}

	var projects []gitlabProject
	if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
		return nil, fmt.Errorf("failed to decode projects: %w", err)
	}

	return projects, nil
}

func (h *GitLabOAuthHandler) createWebhook(token *gitlabOAuthToken, projectID int, webhookURL, secret string) error {
	client := &http.Client{Timeout: 10 * time.Second}

	// First, delete any existing webhooks pointing to our URL (for idempotent re-onboarding)
	listReq, err := http.NewRequest("GET",
		fmt.Sprintf("%s/api/v4/projects/%d/hooks", token.GitLabURL, projectID),
		nil)
	if err == nil {
		listReq.Header.Set("Authorization", "Bearer "+token.AccessToken)
		listResp, err := client.Do(listReq)
		if err == nil && listResp.StatusCode == http.StatusOK {
			var hooks []struct {
				ID  int    `json:"id"`
				URL string `json:"url"`
			}
			if json.NewDecoder(listResp.Body).Decode(&hooks) == nil {
				for _, hook := range hooks {
					if hook.URL == webhookURL {
						// Delete this webhook
						delReq, _ := http.NewRequest("DELETE",
							fmt.Sprintf("%s/api/v4/projects/%d/hooks/%d", token.GitLabURL, projectID, hook.ID),
							nil)
						delReq.Header.Set("Authorization", "Bearer "+token.AccessToken)
						delResp, _ := client.Do(delReq)
						if delResp != nil {
							delResp.Body.Close()
						}
						h.log.Info("deleted existing webhook", "project_id", projectID, "hook_id", hook.ID)
					}
				}
			}
			listResp.Body.Close()
		}
	}

	// Now create the new webhook
	data := url.Values{}
	data.Set("url", webhookURL)
	data.Set("token", secret)
	data.Set("push_events", "true")
	data.Set("tag_push_events", "true")
	data.Set("enable_ssl_verification", "true")

	req, err := http.NewRequest("POST",
		fmt.Sprintf("%s/api/v4/projects/%d/hooks", token.GitLabURL, projectID),
		strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook creation returned %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (h *GitLabOAuthHandler) createProjectAccessToken(token *gitlabOAuthToken, projectID int) (string, error) {
	// GitLab Project Access Tokens API
	// POST /api/v4/projects/:id/access_tokens
	// Requires maintainer+ access and Premium/Ultimate (or self-hosted)

	data := map[string]any{
		"name":         "cinch-ci",
		"scopes":       []string{"api"},
		"access_level": 40,                                               // Maintainer level (required for commit status)
		"expires_at":   time.Now().AddDate(1, 0, 0).Format("2006-01-02"), // 1 year
	}
	body, _ := json.Marshal(data)

	req, err := http.NewRequest("POST",
		fmt.Sprintf("%s/api/v4/projects/%d/access_tokens", token.GitLabURL, projectID),
		strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("PAT request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		// Free tier doesn't allow PAT creation
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("403 Forbidden: %s", string(respBody))
	}

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("PAT creation returned %d: %s", resp.StatusCode, string(respBody))
	}

	var patResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&patResp); err != nil {
		return "", fmt.Errorf("failed to decode PAT response: %w", err)
	}

	if patResp.Token == "" {
		return "", fmt.Errorf("no token in PAT response")
	}

	return patResp.Token, nil
}
