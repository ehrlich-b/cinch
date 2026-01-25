package worker

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
	colorGreen  = "\033[32m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
)

// Terminal provides formatted output for the worker terminal.
type Terminal struct {
	out   io.Writer
	width int
}

// NewTerminal creates a terminal output helper.
func NewTerminal(out io.Writer) *Terminal {
	return &Terminal{
		out:   out,
		width: 80, // Default width
	}
}

// PrintJobStart prints the job start banner.
func (t *Terminal) PrintJobStart(repo, branch, tag, commit, command, mode, forge string) {
	// Parse owner/repo from clone URL
	repoShort := parseRepoShort(repo)

	// Determine ref display (branch or tag)
	refDisplay := branch
	if tag != "" {
		refDisplay = tag
	}

	// Short commit
	commitShort := commit
	if len(commit) > 8 {
		commitShort = commit[:8]
	}

	// Format forge name for display
	forgeDisplay := strings.ToUpper(forge)
	if forgeDisplay == "" {
		forgeDisplay = "BUILD"
	}

	fmt.Fprintln(t.out)
	t.printLine("━")
	fmt.Fprintf(t.out, "%s%s  %s STARTED%s\n", colorBold, colorCyan, forgeDisplay, colorReset)
	t.printLine("─")
	fmt.Fprintf(t.out, "  %s%s%s @ %s (%s)\n", colorBold, repoShort, colorReset, refDisplay, commitShort)
	fmt.Fprintf(t.out, "  %s$ %s%s\n", colorDim, command, colorReset)
	if mode != "" {
		fmt.Fprintf(t.out, "  %s[%s]%s\n", colorDim, mode, colorReset)
	}
	t.printLine("━")
	fmt.Fprintln(t.out)
}

// PrintJobComplete prints the job completion banner.
func (t *Terminal) PrintJobComplete(exitCode int, duration time.Duration) {
	fmt.Fprintln(t.out)
	t.printLine("━")

	durationStr := formatDuration(duration)

	if exitCode == 0 {
		fmt.Fprintf(t.out, "%s%s  ✓ BUILD PASSED%s  %s%s%s\n",
			colorBold, colorGreen, colorReset,
			colorDim, durationStr, colorReset)
	} else {
		fmt.Fprintf(t.out, "%s%s  ✗ BUILD FAILED%s  %s(exit %d)  %s%s\n",
			colorBold, colorRed, colorReset,
			colorDim, exitCode, durationStr, colorReset)
	}

	t.printLine("━")
	fmt.Fprintln(t.out)
}

// PrintJobError prints a job error banner.
func (t *Terminal) PrintJobError(phase, errMsg string) {
	fmt.Fprintln(t.out)
	t.printLine("━")
	fmt.Fprintf(t.out, "%s%s  ✗ BUILD ERROR%s  %s(%s)%s\n",
		colorBold, colorRed, colorReset,
		colorDim, phase, colorReset)
	fmt.Fprintf(t.out, "  %s%s%s\n", colorRed, errMsg, colorReset)
	t.printLine("━")
	fmt.Fprintln(t.out)
}

// printLine prints a horizontal line of the given character.
func (t *Terminal) printLine(char string) {
	fmt.Fprintln(t.out, colorDim+strings.Repeat(char, t.width)+colorReset)
}

// PrintConnected prints a connection success message.
func (t *Terminal) PrintConnected(user, server string) {
	fmt.Fprintf(t.out, "%s%s✓ Connected%s as %s to %s\n", colorBold, colorGreen, colorReset, user, server)
	fmt.Fprintf(t.out, "%sWaiting for jobs...%s\n", colorDim, colorReset)
}

// parseRepoShort extracts "owner/repo" from a clone URL.
func parseRepoShort(cloneURL string) string {
	// Handle various URL formats:
	// https://github.com/owner/repo.git
	// git@github.com:owner/repo.git
	// https://gitlab.example.com/owner/repo.git

	// Remove .git suffix
	url := strings.TrimSuffix(cloneURL, ".git")

	// Try to extract owner/repo
	// Pattern for https URLs
	httpsRe := regexp.MustCompile(`https?://[^/]+/(.+)`)
	if matches := httpsRe.FindStringSubmatch(url); len(matches) > 1 {
		return matches[1]
	}

	// Pattern for SSH URLs (git@host:owner/repo)
	sshRe := regexp.MustCompile(`git@[^:]+:(.+)`)
	if matches := sshRe.FindStringSubmatch(url); len(matches) > 1 {
		return matches[1]
	}

	// Fallback to original URL
	return cloneURL
}

// formatDuration formats a duration in human-readable form.
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", mins, secs)
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", hours, mins)
}
