package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ehrlich-b/cinch/internal/storage"
	"github.com/golang-jwt/jwt/v4"
)

const (
	githubAuthorizeURL = "https://github.com/login/oauth/authorize"
	githubTokenURL     = "https://github.com/login/oauth/access_token"
	githubUserURL      = "https://api.github.com/user"
	githubEmailsURL    = "https://api.github.com/user/emails"

	authCookieName     = "cinch_auth"
	authCookieLifetime = 7 * 24 * time.Hour

	// Device auth settings
	deviceCodeExpiry    = 15 * time.Minute
	deviceCodePollDelay = 5 // seconds
)

// deviceCode represents a pending device authorization request.
type deviceCode struct {
	UserCode   string    // Human-readable code (e.g., "CINCH-1234")
	ExpiresAt  time.Time // When this code expires
	Authorized bool      // Whether the user has authorized
	Email      string    // User's email, set when authorized
}

// AuthConfig holds GitHub OAuth configuration.
type AuthConfig struct {
	GitHubClientID     string
	GitHubClientSecret string
	JWTSecret          string
	BaseURL            string // e.g., "https://cinch.sh"
}

// AuthHandler handles authentication routes.
type AuthHandler struct {
	config  AuthConfig
	log     *slog.Logger
	storage AuthStorage // For user lookups/creation

	// Device auth state (in-memory, keyed by device_code)
	deviceCodes   map[string]*deviceCode
	deviceCodesMu sync.RWMutex
}

// AuthStorage interface for auth (subset of full storage.Storage)
type AuthStorage interface {
	GetUserByEmail(ctx context.Context, email string) (*storage.User, error)
	GetOrCreateUserByEmail(ctx context.Context, email, name string) (*storage.User, error)
	UpdateUserGitHubConnected(ctx context.Context, userID string) error
}

// NewAuthHandler creates a new auth handler.
func NewAuthHandler(cfg AuthConfig, store AuthStorage, log *slog.Logger) *AuthHandler {
	if log == nil {
		log = slog.Default()
	}
	return &AuthHandler{
		config:      cfg,
		storage:     store,
		log:         log,
		deviceCodes: make(map[string]*deviceCode),
	}
}

// ServeHTTP routes auth requests.
func (h *AuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/auth")

	switch path {
	case "/login", "/login/":
		h.handleLogin(w, r)
	case "/github", "/github/":
		h.handleGitHubLogin(w, r)
	case "/callback", "/callback/":
		h.handleCallback(w, r)
	case "/select-email", "/select-email/":
		h.handleSelectEmail(w, r)
	case "/logout", "/logout/":
		h.handleLogout(w, r)
	case "/me", "/me/":
		h.handleMe(w, r)
	case "/device", "/device/":
		h.handleDevice(w, r)
	case "/device/verify", "/device/verify/":
		h.handleDeviceVerify(w, r)
	case "/device/token", "/device/token/":
		h.handleDeviceToken(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handleLogin shows a simple login page.
func (h *AuthHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	returnTo := sanitizeReturnTo(r.URL.Query().Get("return_to"), h.config.BaseURL)

	// If already logged in, redirect
	if email, ok := h.getAuthFromCookie(r); ok {
		h.log.Info("already authenticated", "email", email)
		http.Redirect(w, r, returnTo, http.StatusFound)
		return
	}

	// Check if GitHub OAuth is configured
	if h.config.GitHubClientID == "" {
		http.Error(w, "GitHub OAuth not configured", http.StatusInternalServerError)
		return
	}

	// Simple login page
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	oauthURL := fmt.Sprintf("/auth/github?return_to=%s", url.QueryEscape(returnTo))

	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Sign in - Cinch</title>
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
  background: #0d1117;
  color: #c9d1d9;
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
}
.container {
  text-align: center;
  padding: 2rem;
}
h1 {
  font-size: 1.5rem;
  margin-bottom: 0.5rem;
  color: #f0f6fc;
}
p {
  color: #8b949e;
  margin-bottom: 2rem;
}
.btn {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.75rem 1.5rem;
  background: #238636;
  color: #fff;
  text-decoration: none;
  border-radius: 6px;
  font-weight: 500;
  transition: background 0.2s;
}
.btn:hover { background: #2ea043; }
.btn svg { width: 20px; height: 20px; }
</style>
</head>
<body>
<div class="container">
  <h1>Sign in to Cinch</h1>
  <p>Authenticate to manage repos and workers.</p>
  <a href="%s" class="btn">
    <svg viewBox="0 0 16 16" fill="currentColor"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"/></svg>
    Continue with GitHub
  </a>
</div>
</body>
</html>`, oauthURL)
}

// handleGitHubLogin initiates the GitHub OAuth flow.
func (h *AuthHandler) handleGitHubLogin(w http.ResponseWriter, r *http.Request) {
	if h.config.GitHubClientID == "" {
		h.log.Error("GitHub OAuth not configured")
		http.Error(w, "OAuth not configured", http.StatusInternalServerError)
		return
	}

	returnTo := sanitizeReturnTo(r.URL.Query().Get("return_to"), h.config.BaseURL)

	// Create signed state JWT
	stateToken, err := h.createOAuthState(returnTo)
	if err != nil {
		h.log.Error("failed to create OAuth state", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Build GitHub authorization URL
	// user:email scope gives us access to the user's verified emails
	authURL := fmt.Sprintf("%s?client_id=%s&redirect_uri=%s&scope=%s&state=%s",
		githubAuthorizeURL,
		url.QueryEscape(h.config.GitHubClientID),
		url.QueryEscape(h.config.BaseURL+"/auth/callback"),
		url.QueryEscape("read:user user:email"),
		url.QueryEscape(stateToken))

	h.log.Info("redirecting to GitHub")
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleCallback handles the GitHub OAuth callback.
func (h *AuthHandler) handleCallback(w http.ResponseWriter, r *http.Request) {
	// Parse and validate the state JWT
	stateToken := r.URL.Query().Get("state")
	returnTo, err := h.parseOAuthState(stateToken)
	if err != nil {
		h.log.Error("invalid state parameter", "error", err)
		http.Error(w, "Invalid state parameter", http.StatusBadRequest)
		return
	}

	// Get authorization code
	code := r.URL.Query().Get("code")
	if code == "" {
		h.log.Error("missing authorization code")
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}

	// Exchange code for access token
	accessToken, err := h.exchangeGitHubCode(code)
	if err != nil {
		h.log.Error("failed to exchange code", "error", err)
		http.Error(w, "Failed to exchange code", http.StatusInternalServerError)
		return
	}

	// Get user info from GitHub
	ghUser, err := h.getGitHubUser(accessToken)
	if err != nil {
		h.log.Error("failed to get user info", "error", err)
		http.Error(w, "Failed to get user info", http.StatusInternalServerError)
		return
	}

	// Get all verified emails from GitHub
	emails, err := h.getGitHubEmails(accessToken)
	if err != nil {
		h.log.Error("failed to get user emails", "error", err)
		http.Error(w, "Failed to get user emails", http.StatusInternalServerError)
		return
	}

	h.log.Info("GitHub OAuth callback", "user", ghUser.Login, "emails", emails)

	// Check if user is already logged in (connecting GitHub to existing account)
	if existingEmail, ok := h.getAuthFromCookie(r); ok {
		// Already logged in - just update GitHub connection
		if h.storage != nil {
			user, err := h.storage.GetUserByEmail(r.Context(), existingEmail)
			if err == nil && user != nil {
				_ = h.storage.UpdateUserGitHubConnected(r.Context(), user.ID)
				h.log.Info("GitHub connected to existing account", "email", existingEmail)
			}
		}
		http.Redirect(w, r, returnTo, http.StatusFound)
		return
	}

	// Check if any of the user's emails already exists in our system
	// If so, this is a returning user - log them into their existing account
	if h.storage != nil {
		for _, email := range emails {
			existingUser, err := h.storage.GetUserByEmail(r.Context(), email)
			if err == nil && existingUser != nil {
				// Found existing account - log them in and connect GitHub
				_ = h.storage.UpdateUserGitHubConnected(r.Context(), existingUser.ID)
				if err := h.SetAuthCookie(w, existingUser.Email); err != nil {
					h.log.Error("failed to set auth cookie", "error", err)
					http.Error(w, "Failed to complete login", http.StatusInternalServerError)
					return
				}
				h.log.Info("returning user logged in via GitHub", "email", existingUser.Email)
				http.Redirect(w, r, returnTo, http.StatusFound)
				return
			}
		}
	}

	// No existing account found - this is a new user
	// If only one email, use it directly
	if len(emails) == 1 {
		h.completeLogin(w, r, emails[0], ghUser.Login, returnTo)
		return
	}

	// Multiple emails - show selector
	h.renderEmailSelector(w, emails, ghUser.Login, returnTo)
}

// handleLogout clears the auth cookie.
func (h *AuthHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	h.clearAuthCookie(w)
	http.Redirect(w, r, "/", http.StatusFound)
}

// handleMe returns the current user info as JSON.
func (h *AuthHandler) handleMe(w http.ResponseWriter, r *http.Request) {
	user, ok := h.getAuthFromCookie(r)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"authenticated": false})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"authenticated": true,
		"user":          user,
		"isPro":         true, // Free during beta
	})
}

// RequireAuth is middleware that requires authentication.
// For API routes, returns 401. For browser routes, redirects to login.
func (h *AuthHandler) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, authenticated := h.getAuthFromCookie(r)
		if !authenticated {
			// Check if this is an API request (expects JSON)
			if strings.HasPrefix(r.URL.Path, "/api/") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "authentication required"})
				return
			}

			// Browser request - redirect to login
			returnTo := r.URL.String()
			loginURL := fmt.Sprintf("/auth/login?return_to=%s", url.QueryEscape(returnTo))
			http.Redirect(w, r, loginURL, http.StatusFound)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// IsAuthenticated checks if the request has valid auth (cookie or Bearer token).
func (h *AuthHandler) IsAuthenticated(r *http.Request) bool {
	// Check cookie auth first
	if _, ok := h.getAuthFromCookie(r); ok {
		return true
	}

	// Check Bearer token auth
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if h.ValidateUserToken(token) != "" {
			return true
		}
	}

	return false
}

// GetUser returns the authenticated username, or empty string.
func (h *AuthHandler) GetUser(r *http.Request) string {
	// Check cookie auth first
	if user, ok := h.getAuthFromCookie(r); ok {
		return user
	}

	// Check Bearer token auth
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		return h.ValidateUserToken(token)
	}

	return ""
}

// --- Cookie Management ---

func (h *AuthHandler) SetAuthCookie(w http.ResponseWriter, username string) error {
	claims := jwt.MapClaims{
		"sub": username,
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(authCookieLifetime).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(h.getJWTSigningKey())
	if err != nil {
		return fmt.Errorf("failed to sign JWT: %w", err)
	}

	// Parse base URL to get domain
	domain := ""
	if h.config.BaseURL != "" {
		if u, err := url.Parse(h.config.BaseURL); err == nil {
			domain = u.Hostname()
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    tokenString,
		Path:     "/",
		Domain:   domain,
		MaxAge:   int(authCookieLifetime.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	return nil
}

func (h *AuthHandler) getAuthFromCookie(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(authCookieName)
	if err != nil {
		return "", false
	}

	token, err := jwt.Parse(cookie.Value, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return h.getJWTSigningKey(), nil
	})
	if err != nil || !token.Valid {
		return "", false
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", false
	}

	sub, ok := claims["sub"].(string)
	if !ok || sub == "" {
		return "", false
	}

	return sub, true
}

func (h *AuthHandler) clearAuthCookie(w http.ResponseWriter) {
	domain := ""
	if h.config.BaseURL != "" {
		if u, err := url.Parse(h.config.BaseURL); err == nil {
			domain = u.Hostname()
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    "",
		Path:     "/",
		Domain:   domain,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *AuthHandler) getJWTSigningKey() []byte {
	if h.config.JWTSecret != "" {
		return []byte(h.config.JWTSecret)
	}

	// Check environment
	if secret := os.Getenv("CINCH_JWT_SECRET"); secret != "" {
		return []byte(secret)
	}

	// No secret configured - this is a fatal misconfiguration
	// Panic to prevent the server from running with forgeable tokens
	panic("FATAL: JWT_SECRET not configured. Set config.JWTSecret or CINCH_JWT_SECRET environment variable.")
}

// --- OAuth State ---

func (h *AuthHandler) createOAuthState(returnTo string) (string, error) {
	// Generate random CSRF token
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	claims := jwt.MapClaims{
		"csrf":      base64.URLEncoding.EncodeToString(b),
		"return_to": returnTo,
		"exp":       time.Now().Add(10 * time.Minute).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(h.getJWTSigningKey())
}

func (h *AuthHandler) parseOAuthState(stateToken string) (string, error) {
	token, err := jwt.Parse(stateToken, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return h.getJWTSigningKey(), nil
	})
	if err != nil || !token.Valid {
		return "", fmt.Errorf("invalid state token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", fmt.Errorf("invalid claims")
	}

	returnTo, _ := claims["return_to"].(string)
	return returnTo, nil
}

// --- GitHub API ---

type githubUser struct {
	Login string `json:"login"`
	ID    int    `json:"id"`
}

func (h *AuthHandler) exchangeGitHubCode(code string) (string, error) {
	data := url.Values{}
	data.Set("client_id", h.config.GitHubClientID)
	data.Set("client_secret", h.config.GitHubClientSecret)
	data.Set("code", code)
	data.Set("redirect_uri", h.config.BaseURL+"/auth/callback")

	req, err := http.NewRequest("POST", githubTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token request returned status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	if tokenResp.Error != "" {
		return "", fmt.Errorf("GitHub error: %s - %s", tokenResp.Error, tokenResp.ErrorDesc)
	}

	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("no access token in response")
	}

	return tokenResp.AccessToken, nil
}

func (h *AuthHandler) getGitHubUser(accessToken string) (*githubUser, error) {
	req, err := http.NewRequest("GET", githubUserURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("user request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("user request returned status %d: %s", resp.StatusCode, string(body))
	}

	var user githubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode user response: %w", err)
	}

	if user.Login == "" {
		return nil, fmt.Errorf("no login in response")
	}

	return &user, nil
}

// getGitHubEmails fetches all verified emails from GitHub, with primary first.
func (h *AuthHandler) getGitHubEmails(accessToken string) ([]string, error) {
	req, err := http.NewRequest("GET", githubEmailsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("email request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("email request returned status %d: %s", resp.StatusCode, string(body))
	}

	var ghEmails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&ghEmails); err != nil {
		return nil, fmt.Errorf("failed to decode email response: %w", err)
	}

	// Collect all verified emails, primary first
	var primaryEmail string
	var emails []string
	for _, e := range ghEmails {
		if e.Verified {
			if e.Primary {
				primaryEmail = e.Email
			} else {
				emails = append(emails, e.Email)
			}
		}
	}

	// Put primary first
	if primaryEmail != "" {
		emails = append([]string{primaryEmail}, emails...)
	}

	if len(emails) == 0 {
		return nil, fmt.Errorf("no verified emails found")
	}

	return emails, nil
}

// completeLogin finishes the login process by creating the user and setting the cookie.
func (h *AuthHandler) completeLogin(w http.ResponseWriter, r *http.Request, email, username, returnTo string) {
	// Create user in storage
	if h.storage != nil {
		user, err := h.storage.GetOrCreateUserByEmail(r.Context(), email, username)
		if err != nil {
			h.log.Error("failed to create user", "error", err)
			http.Error(w, "Failed to create user", http.StatusInternalServerError)
			return
		}
		// Mark GitHub as connected
		_ = h.storage.UpdateUserGitHubConnected(r.Context(), user.ID)
	}

	// Set JWT auth cookie with email
	if err := h.SetAuthCookie(w, email); err != nil {
		h.log.Error("failed to set auth cookie", "error", err)
		http.Error(w, "Failed to complete login", http.StatusInternalServerError)
		return
	}

	h.log.Info("user authenticated via GitHub", "email", email, "username", username)

	// Redirect to return_to or dashboard
	if returnTo == "" || returnTo == "/" {
		returnTo = "/dashboard"
	}
	http.Redirect(w, r, returnTo, http.StatusFound)
}

// renderEmailSelector shows a page to choose which email to use.
func (h *AuthHandler) renderEmailSelector(w http.ResponseWriter, emails []string, username, returnTo string) {
	// Create a signed JWT containing the email options and username
	selectionToken, err := h.createEmailSelectionToken(emails, username, returnTo)
	if err != nil {
		h.log.Error("failed to create selection token", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	// Build email options HTML
	var emailOptionsHTML string
	for _, email := range emails {
		emailOptionsHTML += fmt.Sprintf(`
			<button type="submit" name="email" value="%s" class="email-option">
				<span class="email">%s</span>
			</button>`, email, email)
	}

	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Select Email - Cinch</title>
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
  background: #0d1117;
  color: #c9d1d9;
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
}
.container {
  text-align: center;
  padding: 2rem;
  max-width: 500px;
}
h1 {
  font-size: 1.5rem;
  margin-bottom: 0.5rem;
  color: #f0f6fc;
}
p {
  color: #8b949e;
  margin-bottom: 2rem;
}
form { display: flex; flex-direction: column; gap: 0.75rem; }
.email-option {
  display: block;
  width: 100%%;
  padding: 1rem 1.5rem;
  background: #161b22;
  border: 1px solid #30363d;
  color: #c9d1d9;
  border-radius: 6px;
  font-size: 1rem;
  cursor: pointer;
  text-align: left;
  transition: border-color 0.2s, background 0.2s;
}
.email-option:hover {
  border-color: #238636;
  background: #1c2128;
}
.email-option .email { font-weight: 500; }
.hint {
  color: #8b949e;
  font-size: 0.875rem;
  margin-top: 1.5rem;
}
</style>
</head>
<body>
<div class="container">
  <h1>Which email should we use?</h1>
  <p>This will be your Cinch account identity, used for billing and notifications.</p>
  <form method="POST" action="/auth/select-email">
    <input type="hidden" name="token" value="%s">
    %s
  </form>
  <p class="hint">You can change this later in account settings.</p>
</div>
</body>
</html>`, selectionToken, emailOptionsHTML)
}

// handleSelectEmail handles the email selection form submission.
func (h *AuthHandler) handleSelectEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	token := r.FormValue("token")
	selectedEmail := r.FormValue("email")

	if token == "" || selectedEmail == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// Parse and validate the selection token
	emails, username, returnTo, err := h.parseEmailSelectionToken(token)
	if err != nil {
		h.log.Error("invalid selection token", "error", err)
		http.Error(w, "Invalid or expired selection", http.StatusBadRequest)
		return
	}

	// Verify the selected email is in the allowed list
	validEmail := false
	for _, email := range emails {
		if email == selectedEmail {
			validEmail = true
			break
		}
	}
	if !validEmail {
		http.Error(w, "Invalid email selection", http.StatusBadRequest)
		return
	}

	// Check if this email already exists - if so, log them into existing account
	if h.storage != nil {
		existingUser, err := h.storage.GetUserByEmail(r.Context(), selectedEmail)
		if err == nil && existingUser != nil {
			// Found existing account - log them in and connect GitHub
			_ = h.storage.UpdateUserGitHubConnected(r.Context(), existingUser.ID)
			if err := h.SetAuthCookie(w, existingUser.Email); err != nil {
				h.log.Error("failed to set auth cookie", "error", err)
				http.Error(w, "Failed to complete login", http.StatusInternalServerError)
				return
			}
			h.log.Info("returning user logged in via GitHub (email selector)", "email", existingUser.Email)
			http.Redirect(w, r, returnTo, http.StatusFound)
			return
		}
	}

	// Complete the login with the selected email (new user)
	h.completeLogin(w, r, selectedEmail, username, returnTo)
}

// createEmailSelectionToken creates a signed JWT containing email options.
func (h *AuthHandler) createEmailSelectionToken(emails []string, username, returnTo string) (string, error) {
	claims := jwt.MapClaims{
		"emails":    emails,
		"username":  username,
		"return_to": returnTo,
		"exp":       time.Now().Add(10 * time.Minute).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(h.getJWTSigningKey())
}

// parseEmailSelectionToken parses a signed JWT containing email options.
func (h *AuthHandler) parseEmailSelectionToken(tokenString string) (emails []string, username, returnTo string, err error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return h.getJWTSigningKey(), nil
	})
	if err != nil || !token.Valid {
		return nil, "", "", fmt.Errorf("invalid token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, "", "", fmt.Errorf("invalid claims")
	}

	// Extract emails
	if emailsRaw, ok := claims["emails"].([]any); ok {
		for _, e := range emailsRaw {
			if email, ok := e.(string); ok {
				emails = append(emails, email)
			}
		}
	}

	username, _ = claims["username"].(string)
	returnTo, _ = claims["return_to"].(string)

	return emails, username, returnTo, nil
}

// --- Device Authorization Flow ---

// handleDevice initiates a device authorization request (POST /auth/device).
func (h *AuthHandler) handleDevice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Generate unique device code (32 hex chars)
	deviceCodeBytes := make([]byte, 16)
	if _, err := rand.Read(deviceCodeBytes); err != nil {
		h.log.Error("failed to generate device code", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	deviceCodeStr := hex.EncodeToString(deviceCodeBytes)

	// Generate user-friendly code (CINCH-XXXX)
	userCodeBytes := make([]byte, 2)
	if _, err := rand.Read(userCodeBytes); err != nil {
		h.log.Error("failed to generate user code", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	userCode := fmt.Sprintf("CINCH-%04d", int(userCodeBytes[0])<<8|int(userCodeBytes[1])%10000)

	// Store the device code
	h.deviceCodesMu.Lock()
	h.deviceCodes[deviceCodeStr] = &deviceCode{
		UserCode:  userCode,
		ExpiresAt: time.Now().Add(deviceCodeExpiry),
	}
	h.deviceCodesMu.Unlock()

	// Clean up expired codes periodically
	go h.cleanupExpiredDeviceCodes()

	// Build verification URL
	verificationURI := "/auth/device/verify"
	if h.config.BaseURL != "" {
		verificationURI = h.config.BaseURL + verificationURI
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"device_code":      deviceCodeStr,
		"user_code":        userCode,
		"verification_uri": verificationURI,
		"expires_in":       int(deviceCodeExpiry.Seconds()),
		"interval":         deviceCodePollDelay,
	})
}

// handleDeviceVerify shows the browser page for device verification (GET/POST /auth/device/verify).
func (h *AuthHandler) handleDeviceVerify(w http.ResponseWriter, r *http.Request) {
	userCode := r.URL.Query().Get("code")

	if r.Method == http.MethodPost {
		// Handle form submission
		if err := r.ParseForm(); err == nil {
			userCode = r.FormValue("code")
		}

		// Find the device code by user code
		h.deviceCodesMu.Lock()
		var foundDeviceCode string
		for dc, info := range h.deviceCodes {
			if info.UserCode == userCode && time.Now().Before(info.ExpiresAt) {
				foundDeviceCode = dc
				break
			}
		}
		h.deviceCodesMu.Unlock()

		if foundDeviceCode == "" {
			h.renderDeviceVerifyPage(w, userCode, "Invalid or expired code")
			return
		}

		// Check if user is authenticated
		email, authenticated := h.getAuthFromCookie(r)
		if !authenticated {
			// Redirect to login, then back here
			loginURL := fmt.Sprintf("/auth/login?return_to=%s", url.QueryEscape("/auth/device/verify?code="+userCode))
			http.Redirect(w, r, loginURL, http.StatusFound)
			return
		}

		// Authorize the device
		h.deviceCodesMu.Lock()
		if dc, ok := h.deviceCodes[foundDeviceCode]; ok {
			dc.Authorized = true
			dc.Email = email
		}
		h.deviceCodesMu.Unlock()

		h.log.Info("device authorized", "email", email, "code", userCode)
		h.renderDeviceVerifyPage(w, "", "Device authorized! You can close this window.")
		return
	}

	// GET - show the verification page
	h.renderDeviceVerifyPage(w, userCode, "")
}

func (h *AuthHandler) renderDeviceVerifyPage(w http.ResponseWriter, userCode, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	messageHTML := ""
	if message != "" {
		if strings.Contains(message, "authorized") {
			messageHTML = fmt.Sprintf(`<p class="success">%s</p>`, message)
		} else {
			messageHTML = fmt.Sprintf(`<p class="error">%s</p>`, message)
		}
	}

	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Verify Device - Cinch</title>
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
  background: #0d1117;
  color: #c9d1d9;
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
}
.container {
  text-align: center;
  padding: 2rem;
  max-width: 400px;
}
h1 {
  font-size: 1.5rem;
  margin-bottom: 0.5rem;
  color: #f0f6fc;
}
p {
  color: #8b949e;
  margin-bottom: 1.5rem;
}
.code-display {
  font-family: monospace;
  font-size: 2rem;
  font-weight: bold;
  color: #58a6ff;
  margin: 1.5rem 0;
  padding: 1rem;
  background: #161b22;
  border-radius: 6px;
  letter-spacing: 0.1em;
}
form { margin-top: 1rem; }
input[type="text"] {
  padding: 0.75rem 1rem;
  font-size: 1rem;
  background: #161b22;
  border: 1px solid #30363d;
  color: #c9d1d9;
  border-radius: 6px;
  width: 100%%;
  margin-bottom: 1rem;
  text-align: center;
  letter-spacing: 0.1em;
}
input[type="text"]::placeholder { color: #8b949e; }
.btn {
  display: inline-block;
  padding: 0.75rem 1.5rem;
  background: #238636;
  color: #fff;
  text-decoration: none;
  border: none;
  border-radius: 6px;
  font-size: 1rem;
  font-weight: 500;
  cursor: pointer;
  width: 100%%;
}
.btn:hover { background: #2ea043; }
.error { color: #f85149; }
.success { color: #3fb950; }
</style>
</head>
<body>
<div class="container">
  <h1>Verify Device</h1>
  %s
  <p>Enter the code shown in your terminal to authorize the CLI.</p>
  <form method="POST">
    <input type="text" name="code" placeholder="CINCH-0000" value="%s" autocomplete="off" autofocus>
    <button type="submit" class="btn">Authorize Device</button>
  </form>
</div>
</body>
</html>`, messageHTML, userCode)
}

// handleDeviceToken handles polling for the device token (POST /auth/device/token).
func (h *AuthHandler) handleDeviceToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		DeviceCode string `json:"device_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_request"})
		return
	}

	h.deviceCodesMu.RLock()
	dc, exists := h.deviceCodes[req.DeviceCode]
	h.deviceCodesMu.RUnlock()

	if !exists {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_device_code"})
		return
	}

	if time.Now().After(dc.ExpiresAt) {
		// Clean up expired code
		h.deviceCodesMu.Lock()
		delete(h.deviceCodes, req.DeviceCode)
		h.deviceCodesMu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "expired_token"})
		return
	}

	if !dc.Authorized {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK) // 200 with authorization_pending
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
		return
	}

	// Generate a long-lived user token
	token, err := h.createUserToken(dc.Email)
	if err != nil {
		h.log.Error("failed to create user token", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "server_error"})
		return
	}

	// Clean up the device code
	h.deviceCodesMu.Lock()
	delete(h.deviceCodes, req.DeviceCode)
	h.deviceCodesMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"email":        dc.Email,
	})
}

// createUserToken creates a long-lived JWT for CLI use.
func (h *AuthHandler) createUserToken(email string) (string, error) {
	claims := jwt.MapClaims{
		"sub":  email,
		"type": "user",
		"iat":  time.Now().Unix(),
		"exp":  time.Now().Add(90 * 24 * time.Hour).Unix(), // 90 days
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(h.getJWTSigningKey())
}

// ValidateUserToken validates a Bearer token from the CLI.
// Returns the username if valid, empty string if not.
func (h *AuthHandler) ValidateUserToken(tokenString string) string {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return h.getJWTSigningKey(), nil
	})
	if err != nil || !token.Valid {
		return ""
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return ""
	}

	// Check token type
	tokenType, _ := claims["type"].(string)
	if tokenType != "user" {
		return ""
	}

	sub, ok := claims["sub"].(string)
	if !ok || sub == "" {
		return ""
	}

	return sub
}

func (h *AuthHandler) cleanupExpiredDeviceCodes() {
	h.deviceCodesMu.Lock()
	defer h.deviceCodesMu.Unlock()

	now := time.Now()
	for code, info := range h.deviceCodes {
		if now.After(info.ExpiresAt) {
			delete(h.deviceCodes, code)
		}
	}
}

// --- Helpers ---

// sanitizeReturnTo validates and sanitizes a return_to URL parameter.
func sanitizeReturnTo(returnTo, baseURL string) string {
	if returnTo == "" {
		return "/"
	}

	// Allow relative paths
	if strings.HasPrefix(returnTo, "/") && !strings.HasPrefix(returnTo, "//") {
		return returnTo
	}

	// Allow absolute URLs on our domain
	if baseURL != "" && strings.HasPrefix(returnTo, baseURL) {
		return returnTo
	}

	return "/"
}
