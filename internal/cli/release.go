package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// ReleaseOptions configures the release command.
type ReleaseOptions struct {
	Forge   string   // Override forge detection
	Tag     string   // Override tag
	Repo    string   // Override repository (owner/name)
	Token   string   // Override token
	Files   []string // Files to upload
	Draft   bool
	Prerelease bool
}

// Release creates a release on the detected forge and uploads assets.
func Release(opts ReleaseOptions) error {
	// Auto-detect from environment
	forge := opts.Forge
	if forge == "" {
		forge = os.Getenv("CINCH_FORGE")
	}
	if forge == "" {
		return fmt.Errorf("cannot detect forge: set CINCH_FORGE or use --forge flag")
	}

	tag := opts.Tag
	if tag == "" {
		tag = os.Getenv("CINCH_TAG")
	}
	if tag == "" {
		return fmt.Errorf("cannot detect tag: set CINCH_TAG or use --tag flag")
	}

	token := opts.Token
	if token == "" {
		token = os.Getenv("CINCH_FORGE_TOKEN")
	}
	if token == "" {
		// Try forge-specific token
		switch forge {
		case "github":
			token = os.Getenv("GITHUB_TOKEN")
		case "gitlab":
			token = os.Getenv("GITLAB_TOKEN")
		case "gitea", "forgejo":
			token = os.Getenv("GITEA_TOKEN")
		}
	}
	if token == "" {
		return fmt.Errorf("cannot detect token: set CINCH_FORGE_TOKEN or use --token flag")
	}

	repo := opts.Repo
	if repo == "" {
		// Parse from CINCH_REPO (clone URL)
		cloneURL := os.Getenv("CINCH_REPO")
		if cloneURL != "" {
			repo = parseRepoFromURL(cloneURL)
		}
	}
	if repo == "" {
		return fmt.Errorf("cannot detect repository: set CINCH_REPO or use --repo flag")
	}

	// Expand file globs
	var files []string
	for _, pattern := range opts.Files {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("invalid glob pattern %q: %w", pattern, err)
		}
		if len(matches) == 0 {
			return fmt.Errorf("no files match pattern %q", pattern)
		}
		files = append(files, matches...)
	}

	if len(files) == 0 {
		return fmt.Errorf("no files to upload")
	}

	fmt.Printf("Creating %s release %s for %s\n", forge, tag, repo)
	fmt.Printf("Uploading %d files...\n", len(files))

	switch forge {
	case "github":
		return releaseGitHub(repo, tag, token, files, opts.Draft, opts.Prerelease)
	case "gitlab":
		return fmt.Errorf("GitLab releases not yet implemented")
	case "gitea", "forgejo":
		return releaseGitea(repo, tag, token, files, opts.Draft, opts.Prerelease)
	default:
		return fmt.Errorf("unknown forge: %s", forge)
	}
}

// parseRepoFromURL extracts owner/repo from a clone URL.
func parseRepoFromURL(cloneURL string) string {
	// Handle https://github.com/owner/repo.git
	// Handle git@github.com:owner/repo.git
	u, err := url.Parse(cloneURL)
	if err != nil {
		return ""
	}

	path := strings.TrimPrefix(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	return path
}

// --- GitHub ---

type githubRelease struct {
	ID      int64  `json:"id"`
	TagName string `json:"tag_name"`
	UploadURL string `json:"upload_url"`
}

func releaseGitHub(repo, tag, token string, files []string, draft, prerelease bool) error {
	// Create release
	payload := map[string]any{
		"tag_name":               tag,
		"name":                   tag,
		"draft":                  draft,
		"prerelease":             prerelease,
		"generate_release_notes": true,
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", fmt.Sprintf("https://api.github.com/repos/%s/releases", repo), bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("create release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create release failed: %s - %s", resp.Status, string(body))
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return fmt.Errorf("decode release response: %w", err)
	}

	fmt.Printf("Created release: %s\n", tag)

	// Upload assets
	// upload_url looks like: https://uploads.github.com/repos/owner/repo/releases/123/assets{?name,label}
	uploadBase := strings.Split(release.UploadURL, "{")[0]

	for _, file := range files {
		if err := uploadGitHubAsset(uploadBase, token, file); err != nil {
			return fmt.Errorf("upload %s: %w", file, err)
		}
	}

	fmt.Printf("Release %s complete!\n", tag)
	return nil
}

func uploadGitHubAsset(uploadBase, token, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return err
	}

	name := filepath.Base(filePath)
	uploadURL := fmt.Sprintf("%s?name=%s", uploadBase, url.QueryEscape(name))

	// Detect content type
	contentType := mime.TypeByExtension(filepath.Ext(filePath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	req, _ := http.NewRequest("POST", uploadURL, f)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", contentType)
	req.ContentLength = stat.Size()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s - %s", resp.Status, string(body))
	}

	fmt.Printf("  Uploaded: %s\n", name)
	return nil
}

// --- Gitea/Forgejo ---

type giteaRelease struct {
	ID      int64  `json:"id"`
	TagName string `json:"tag_name"`
}

func releaseGitea(repo, tag, token string, files []string, draft, prerelease bool) error {
	// Gitea needs the base URL - try to get from CINCH_REPO
	baseURL := "https://codeberg.org" // Default for Forgejo
	if cloneURL := os.Getenv("CINCH_REPO"); cloneURL != "" {
		if u, err := url.Parse(cloneURL); err == nil {
			baseURL = fmt.Sprintf("%s://%s", u.Scheme, u.Host)
		}
	}

	// Create release
	payload := map[string]any{
		"tag_name":   tag,
		"name":       tag,
		"draft":      draft,
		"prerelease": prerelease,
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/repos/%s/releases", baseURL, repo), bytes.NewReader(body))
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("create release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create release failed: %s - %s", resp.Status, string(body))
	}

	var release giteaRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return fmt.Errorf("decode release response: %w", err)
	}

	fmt.Printf("Created release: %s\n", tag)

	// Upload assets
	for _, file := range files {
		if err := uploadGiteaAsset(baseURL, repo, release.ID, token, file); err != nil {
			return fmt.Errorf("upload %s: %w", file, err)
		}
	}

	fmt.Printf("Release %s complete!\n", tag)
	return nil
}

func uploadGiteaAsset(baseURL, repo string, releaseID int64, token, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	name := filepath.Base(filePath)
	uploadURL := fmt.Sprintf("%s/api/v1/repos/%s/releases/%d/assets?name=%s", baseURL, repo, releaseID, url.QueryEscape(name))

	// Detect content type
	contentType := mime.TypeByExtension(filepath.Ext(filePath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	req, _ := http.NewRequest("POST", uploadURL, f)
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s - %s", resp.Status, string(body))
	}

	fmt.Printf("  Uploaded: %s\n", name)
	return nil
}
