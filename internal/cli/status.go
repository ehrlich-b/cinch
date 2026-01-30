package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

// StatusOptions configures the status command.
type StatusOptions struct {
	ServerURL string
	Token     string
	Limit     int
}

// JobStatus represents a job's status from the API.
type JobStatus struct {
	ID           string    `json:"id"`
	Status       string    `json:"status"`
	Repo         string    `json:"repo"`
	Branch       string    `json:"branch"`
	Tag          string    `json:"tag,omitempty"`
	Commit       string    `json:"commit"`
	PRNumber     *int      `json:"pr_number,omitempty"`
	PRBaseBranch string    `json:"pr_base_branch,omitempty"`
	ExitCode     *int      `json:"exit_code,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	StartedAt    *string   `json:"started_at,omitempty"`
	FinishedAt   *string   `json:"finished_at,omitempty"`
	Forge        string    `json:"-"` // Set locally, not from API
	Owner        string    `json:"-"` // Parsed from Repo field
}

// RepoInfo parsed from git remote URL.
type RepoInfo struct {
	Forge string
	Owner string
	Name  string
}

// Status shows the build status for the current repo across all forges.
func Status(opts StatusOptions) ([]JobStatus, error) {
	// Get all repo remotes
	repos, err := detectAllRepos()
	if err != nil {
		return nil, err
	}

	if len(repos) == 0 {
		return nil, fmt.Errorf("no git remotes configured")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	var allJobs []JobStatus

	for _, info := range repos {
		jobs, err := fetchJobsForRepo(client, opts, info)
		if err != nil {
			// Skip repos that aren't on cinch, but continue with others
			continue
		}
		allJobs = append(allJobs, jobs...)
	}

	// Sort by created_at descending
	sort.Slice(allJobs, func(i, j int) bool {
		return allJobs[i].CreatedAt.After(allJobs[j].CreatedAt)
	})

	// Limit total results
	if len(allJobs) > opts.Limit {
		allJobs = allJobs[:opts.Limit]
	}

	return allJobs, nil
}

func fetchJobsForRepo(client *http.Client, opts StatusOptions, info *RepoInfo) ([]JobStatus, error) {
	apiURL := fmt.Sprintf("%s/api/repos/%s/%s/%s/jobs?limit=%d",
		opts.ServerURL, info.Forge, info.Owner, info.Name, opts.Limit)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+opts.Token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("repo not found")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Jobs []JobStatus `json:"jobs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Add forge/owner info to each job for display
	for i := range result.Jobs {
		result.Jobs[i].Forge = info.Forge
		// Parse owner from "owner/repo" format
		if parts := strings.SplitN(result.Jobs[i].Repo, "/", 2); len(parts) >= 1 {
			result.Jobs[i].Owner = parts[0]
		}
	}

	return result.Jobs, nil
}

// DetectRepos gets repo info from all git remotes. Exported for use by repo add command.
func DetectRepos() ([]*RepoInfo, error) {
	return detectAllRepos()
}

// detectAllRepos gets repo info from all git remotes.
func detectAllRepos() ([]*RepoInfo, error) {
	cmd := exec.Command("git", "remote")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("not in a git repository")
	}

	remoteNames := strings.Fields(string(out))
	if len(remoteNames) == 0 {
		return nil, fmt.Errorf("no git remotes configured")
	}

	var repos []*RepoInfo
	seen := make(map[string]bool)

	for _, name := range remoteNames {
		cmd := exec.Command("git", "remote", "get-url", name)
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		remoteURL := strings.TrimSpace(string(out))

		info, err := parseRemoteURL(remoteURL)
		if err != nil {
			continue
		}

		// Dedupe by forge (same repo on different forges)
		key := info.Forge + "/" + info.Owner + "/" + info.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		repos = append(repos, info)
	}

	return repos, nil
}

// parseRemoteURL extracts forge/owner/repo from a git remote URL.
func parseRemoteURL(remoteURL string) (*RepoInfo, error) {
	// Handle SSH URLs: git@github.com:owner/repo.git
	if strings.HasPrefix(remoteURL, "git@") {
		re := regexp.MustCompile(`git@([^:]+):([^/]+)/(.+?)(?:\.git)?$`)
		matches := re.FindStringSubmatch(remoteURL)
		if matches == nil {
			return nil, fmt.Errorf("cannot parse SSH remote URL: %s", remoteURL)
		}
		return &RepoInfo{
			Forge: hostToForgeDomain(matches[1]),
			Owner: matches[2],
			Name:  strings.TrimSuffix(matches[3], ".git"),
		}, nil
	}

	// Handle HTTPS URLs: https://github.com/owner/repo.git
	u, err := url.Parse(remoteURL)
	if err != nil {
		return nil, fmt.Errorf("cannot parse remote URL: %s", remoteURL)
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("cannot parse remote URL path: %s", remoteURL)
	}

	return &RepoInfo{
		Forge: hostToForgeDomain(u.Host),
		Owner: parts[0],
		Name:  strings.TrimSuffix(parts[1], ".git"),
	}, nil
}

// hostToForgeDomain converts host to forge domain used in API.
func hostToForgeDomain(host string) string {
	switch host {
	case "github.com":
		return "github.com"
	case "gitlab.com":
		return "gitlab.com"
	case "codeberg.org":
		return "codeberg.org"
	default:
		return host
	}
}

// FormatDuration formats a duration nicely.
func FormatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

// RelativeTime formats a time as relative to now.
func RelativeTime(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1 day ago"
	}
	return fmt.Sprintf("%d days ago", days)
}

// StatusSymbol returns a terminal-friendly status symbol.
func StatusSymbol(status string) string {
	switch status {
	case "success":
		return "\033[32m✓\033[0m" // green check
	case "failed", "error":
		return "\033[31m✗\033[0m" // red X
	case "running":
		return "\033[33m●\033[0m" // yellow dot
	case "pending", "queued":
		return "\033[90m○\033[0m" // gray circle
	case "pending_contributor":
		return "\033[35m⏳\033[0m" // purple hourglass
	case "cancelled":
		return "\033[90m⊘\033[0m" // gray cancel
	default:
		return "?"
	}
}
