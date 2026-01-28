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

// ForgejoOAuthConfig holds Forgejo/Gitea OAuth configuration.
type ForgejoOAuthConfig struct {
	ClientID     string // OAuth Application ID
	ClientSecret string // OAuth Application Secret
	BaseURL      string // "https://codeberg.org" or self-hosted
}

// ForgejoOAuthHandler handles Forgejo/Gitea OAuth and repo setup.
type ForgejoOAuthHandler struct {
	config     ForgejoOAuthConfig
	appBaseURL string // Cinch app base URL for callbacks
	jwtSecret  []byte
	storage    storage.Storage
	log        *slog.Logger

	// Temporary storage for OAuth tokens (keyed by username)
	oauthTokens   map[string]*forgejoOAuthToken
	oauthTokensMu sync.RWMutex
}

type forgejoOAuthToken struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	ForgejoURL   string // Base URL (for self-hosted support)
}

// forgejoUser represents Forgejo/Gitea user info from API.
type forgejoUser struct {
	ID       int    `json:"id"`
	Login    string `json:"login"`
	Email    string `json:"email"`
	FullName string `json:"full_name"`
}

// NewForgejoOAuthHandler creates a new Forgejo OAuth handler.
func NewForgejoOAuthHandler(cfg ForgejoOAuthConfig, appBaseURL string, jwtSecret []byte, store storage.Storage, log *slog.Logger) *ForgejoOAuthHandler {
	if log == nil {
		log = slog.Default()
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://codeberg.org"
	}
	return &ForgejoOAuthHandler{
		config:      cfg,
		appBaseURL:  strings.TrimSuffix(appBaseURL, "/"),
		jwtSecret:   jwtSecret,
		storage:     store,
		log:         log,
		oauthTokens: make(map[string]*forgejoOAuthToken),
	}
}

// IsConfigured returns true if Forgejo OAuth is configured.
func (h *ForgejoOAuthHandler) IsConfigured() bool {
	return h.config.ClientID != "" && h.config.ClientSecret != ""
}

// HandleLogin initiates Forgejo OAuth flow.
// GET /auth/forgejo
func (h *ForgejoOAuthHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if !h.IsConfigured() {
		http.Error(w, "Forgejo OAuth not configured", http.StatusServiceUnavailable)
		return
	}

	// Create signed state JWT with CSRF token
	state, err := h.createOAuthState()
	if err != nil {
		h.log.Error("failed to create OAuth state", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Build Forgejo authorization URL
	// Scopes: write:repository (webhooks + status), read:user (profile)
	authURL := fmt.Sprintf("%s/login/oauth/authorize?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=%s",
		h.config.BaseURL,
		url.QueryEscape(h.config.ClientID),
		url.QueryEscape(h.appBaseURL+"/auth/forgejo/callback"),
		url.QueryEscape("write:repository read:user"),
		url.QueryEscape(state))

	h.log.Info("redirecting to Forgejo OAuth", "url", h.config.BaseURL)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// HandleCallback handles Forgejo OAuth callback.
// GET /auth/forgejo/callback
// Now supports both onboarding (new users) and connecting (existing users).
func (h *ForgejoOAuthHandler) HandleCallback(w http.ResponseWriter, r *http.Request, authHelper ForgeAuthHelper) {
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
		h.log.Warn("Forgejo OAuth error", "error", errParam, "description", errDesc)
		http.Redirect(w, r, "/?error=forgejo_oauth_denied", http.StatusFound)
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

	// Get user info from Forgejo (includes email)
	fjUser, err := h.getForgejoUser(token)
	if err != nil {
		h.log.Error("failed to get Forgejo user", "error", err)
		http.Error(w, "Failed to get user info from Forgejo", http.StatusInternalServerError)
		return
	}

	h.log.Info("Forgejo OAuth callback", "user", fjUser.Login, "email", fjUser.Email)

	ctx := r.Context()

	// Check if user is already logged in (connecting Forgejo to existing account)
	if existingEmail := authHelper.GetUser(r); existingEmail != "" {
		// Already logged in - just update Forgejo connection
		user, err := h.storage.GetUserByEmail(ctx, existingEmail)
		if err == nil && user != nil {
			// Store OAuth token for this user (keyed by email now)
			h.oauthTokensMu.Lock()
			h.oauthTokens[existingEmail] = token
			h.oauthTokensMu.Unlock()

			// Persist credentials
			creds := map[string]any{
				"type":          "oauth",
				"access_token":  token.AccessToken,
				"refresh_token": token.RefreshToken,
				"expires_at":    token.ExpiresAt,
				"base_url":      token.ForgejoURL,
			}
			credsJSON, _ := json.Marshal(creds)
			if err := h.storage.UpdateUserForgejoCredentials(ctx, user.ID, string(credsJSON)); err != nil {
				h.log.Error("failed to save Forgejo credentials", "error", err)
			}
			_ = h.storage.UpdateUserForgejoConnected(ctx, user.ID)
			h.log.Info("Forgejo connected to existing account", "email", existingEmail)
		}
		http.Redirect(w, r, "/forgejo/onboard", http.StatusFound)
		return
	}

	// Not logged in - find or create account by email
	email := fjUser.Email
	if email == "" {
		h.log.Error("Forgejo user has no email")
		http.Error(w, "Forgejo account has no email configured", http.StatusBadRequest)
		return
	}

	// Find existing account or create new one
	// If email exists, this is a returning user - log them in and connect Forgejo
	user, err := h.storage.GetOrCreateUserByEmail(ctx, email, fjUser.Login)
	if err != nil {
		h.log.Error("failed to get/create user", "error", err)
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	// Store OAuth token for this user
	h.oauthTokensMu.Lock()
	h.oauthTokens[email] = token
	h.oauthTokensMu.Unlock()

	// Persist credentials and mark Forgejo as connected
	creds := map[string]any{
		"type":          "oauth",
		"access_token":  token.AccessToken,
		"refresh_token": token.RefreshToken,
		"expires_at":    token.ExpiresAt,
		"base_url":      token.ForgejoURL,
	}
	credsJSON, _ := json.Marshal(creds)
	if err := h.storage.UpdateUserForgejoCredentials(ctx, user.ID, string(credsJSON)); err != nil {
		h.log.Error("failed to save Forgejo credentials", "error", err)
	}
	_ = h.storage.UpdateUserForgejoConnected(ctx, user.ID)

	// Set auth cookie
	if err := authHelper.SetAuthCookie(w, email); err != nil {
		h.log.Error("failed to set auth cookie", "error", err)
		http.Error(w, "Failed to complete login", http.StatusInternalServerError)
		return
	}

	h.log.Info("user authenticated via Forgejo", "email", email, "username", fjUser.Login)

	// Redirect to project selector
	http.Redirect(w, r, "/forgejo/onboard", http.StatusFound)
}

// HandleProjects lists user's Forgejo repositories.
// GET /api/forgejo/repos
func (h *ForgejoOAuthHandler) HandleProjects(w http.ResponseWriter, r *http.Request, userEmail string) {
	token := h.getOAuthToken(userEmail)
	if token == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Forgejo not connected"})
		return
	}

	// Fetch user's repos from Forgejo
	repos, err := h.fetchRepos(token)
	if err != nil {
		h.log.Error("failed to fetch repos", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to fetch repositories"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(repos)
}

// HandleSetup creates webhook and prompts for manual PAT.
// POST /api/forgejo/setup
func (h *ForgejoOAuthHandler) HandleSetup(w http.ResponseWriter, r *http.Request, userEmail string) {
	token := h.getOAuthToken(userEmail)
	if token == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Forgejo not connected"})
		return
	}

	var req struct {
		Owner       string `json:"owner"`
		Name        string `json:"name"`
		ManualToken string `json:"manual_token"` // User-provided PAT
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

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
	if err := h.createWebhook(token, req.Owner, req.Name, webhookURL, webhookSecret); err != nil {
		h.log.Error("failed to create webhook", "error", err, "repo", req.Owner+"/"+req.Name)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Failed to create webhook: %v", err)})
		return
	}

	h.log.Info("webhook created", "repo", req.Owner+"/"+req.Name)

	// If no manual token yet, respond asking for it
	if req.ManualToken == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted) // 202 = webhook created, needs token
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":          "needs_token",
			"message":         "Webhook created! Now we need a token for posting build status.",
			"webhook_created": true,
			"token_url":       fmt.Sprintf("%s/user/settings/applications", token.ForgejoURL),
			"instructions": []string{
				"Click 'Generate New Token'",
				"Name it 'Cinch CI' (or whatever you like)",
				"Select scope: 'repository' (read & write)",
				"Copy the token and paste it below",
			},
		})
		return
	}

	// Create repo in storage with the manual token
	cloneURL := fmt.Sprintf("%s/%s/%s.git", token.ForgejoURL, req.Owner, req.Name)
	htmlURL := fmt.Sprintf("%s/%s/%s", token.ForgejoURL, req.Owner, req.Name)

	repo := &storage.Repo{
		ID:            fmt.Sprintf("r_%d", time.Now().UnixNano()),
		ForgeType:     storage.ForgeTypeForgejo,
		Owner:         req.Owner,
		Name:          req.Name,
		CloneURL:      cloneURL,
		HTMLURL:       htmlURL,
		WebhookSecret: webhookSecret,
		ForgeToken:    req.ManualToken,
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
	delete(h.oauthTokens, userEmail)
	h.oauthTokensMu.Unlock()

	h.log.Info("Forgejo repo setup complete", "repo", req.Owner+"/"+req.Name)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":          "success",
		"repo_id":         repo.ID,
		"webhook_created": true,
	})
}

// --- Helper methods ---

// getForgejoUser fetches user info from Forgejo/Gitea API.
func (h *ForgejoOAuthHandler) getForgejoUser(token *forgejoOAuthToken) (*forgejoUser, error) {
	req, err := http.NewRequest("GET", token.ForgejoURL+"/api/v1/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+token.AccessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("user request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("user request returned %d: %s", resp.StatusCode, string(body))
	}

	var user forgejoUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode user: %w", err)
	}

	return &user, nil
}

func (h *ForgejoOAuthHandler) getOAuthToken(userEmail string) *forgejoOAuthToken {
	// First check in-memory cache
	h.oauthTokensMu.RLock()
	token := h.oauthTokens[userEmail]
	h.oauthTokensMu.RUnlock()
	if token != nil {
		return token
	}

	// Fall back to database (lookup by email now)
	ctx := context.Background()
	user, err := h.storage.GetUserByEmail(ctx, userEmail)
	if err != nil || user == nil || user.ForgejoCredentials == "" {
		return nil
	}

	// Parse stored credentials
	var creds struct {
		Type         string    `json:"type"`
		AccessToken  string    `json:"access_token"`
		RefreshToken string    `json:"refresh_token"`
		ExpiresAt    time.Time `json:"expires_at"`
		BaseURL      string    `json:"base_url"`
	}
	if err := json.Unmarshal([]byte(user.ForgejoCredentials), &creds); err != nil {
		h.log.Error("failed to parse stored Forgejo credentials", "error", err, "email", userEmail)
		return nil
	}

	// Convert to token struct and cache in memory
	token = &forgejoOAuthToken{
		AccessToken:  creds.AccessToken,
		RefreshToken: creds.RefreshToken,
		ExpiresAt:    creds.ExpiresAt,
		ForgejoURL:   creds.BaseURL,
	}

	h.oauthTokensMu.Lock()
	h.oauthTokens[userEmail] = token
	h.oauthTokensMu.Unlock()

	return token
}

func (h *ForgejoOAuthHandler) createOAuthState() (string, error) {
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

func (h *ForgejoOAuthHandler) validateOAuthState(stateToken string) error {
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

func (h *ForgejoOAuthHandler) exchangeCode(code string) (*forgejoOAuthToken, error) {
	data := url.Values{}
	data.Set("client_id", h.config.ClientID)
	data.Set("client_secret", h.config.ClientSecret)
	data.Set("code", code)
	data.Set("grant_type", "authorization_code")
	data.Set("redirect_uri", h.appBaseURL+"/auth/forgejo/callback")

	req, err := http.NewRequest("POST", h.config.BaseURL+"/login/oauth/access_token", strings.NewReader(data.Encode()))
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

	return &forgejoOAuthToken{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
		ForgejoURL:   h.config.BaseURL,
	}, nil
}

type forgejoRepo struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"` // "owner/name"
	HTMLURL  string `json:"html_url"`
	Private  bool   `json:"private"`
	Owner    struct {
		Login string `json:"login"`
	} `json:"owner"`
}

func (h *ForgejoOAuthHandler) fetchRepos(token *forgejoOAuthToken) ([]forgejoRepo, error) {
	// Fetch repos the user has access to (owns or is collaborator)
	req, err := http.NewRequest("GET", token.ForgejoURL+"/api/v1/user/repos?limit=100", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+token.AccessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("repos request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("repos request returned %d: %s", resp.StatusCode, string(body))
	}

	var repos []forgejoRepo
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		return nil, fmt.Errorf("failed to decode repos: %w", err)
	}

	return repos, nil
}

func (h *ForgejoOAuthHandler) createWebhook(token *forgejoOAuthToken, owner, name, webhookURL, secret string) error {
	client := &http.Client{Timeout: 10 * time.Second}

	// First, delete any existing webhooks pointing to our URL (for idempotent re-onboarding)
	listReq, err := http.NewRequest("GET",
		fmt.Sprintf("%s/api/v1/repos/%s/%s/hooks", token.ForgejoURL, owner, name),
		nil)
	if err == nil {
		listReq.Header.Set("Authorization", "token "+token.AccessToken)
		listResp, err := client.Do(listReq)
		if err == nil && listResp.StatusCode == http.StatusOK {
			var hooks []struct {
				ID     int64 `json:"id"`
				Config struct {
					URL string `json:"url"`
				} `json:"config"`
			}
			if json.NewDecoder(listResp.Body).Decode(&hooks) == nil {
				for _, hook := range hooks {
					if hook.Config.URL == webhookURL {
						// Delete this webhook
						delReq, _ := http.NewRequest("DELETE",
							fmt.Sprintf("%s/api/v1/repos/%s/%s/hooks/%d", token.ForgejoURL, owner, name, hook.ID),
							nil)
						delReq.Header.Set("Authorization", "token "+token.AccessToken)
						delResp, _ := client.Do(delReq)
						if delResp != nil {
							delResp.Body.Close()
						}
						h.log.Info("deleted existing webhook", "repo", owner+"/"+name, "hook_id", hook.ID)
					}
				}
			}
			listResp.Body.Close()
		}
	}

	// Now create the new webhook
	payload := map[string]any{
		"type":   "forgejo",
		"active": true,
		"events": []string{"push", "pull_request"},
		"config": map[string]string{
			"url":          webhookURL,
			"content_type": "json",
			"secret":       secret,
		},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST",
		fmt.Sprintf("%s/api/v1/repos/%s/%s/hooks", token.ForgejoURL, owner, name),
		strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+token.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook creation returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
