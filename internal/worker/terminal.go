package worker

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"golang.org/x/term"
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
// When isTTY is true, outputs ANSI-decorated banners.
// When isTTY is false, outputs plain prefixed text suitable for pipes/logs.
type Terminal struct {
	out   io.Writer
	width int
	isTTY bool
}

// NewTerminal creates a terminal output helper with auto-detected TTY mode.
// If out is os.Stdout, checks if it's a terminal.
func NewTerminal(out io.Writer) *Terminal {
	isTTY := false
	if f, ok := out.(*os.File); ok {
		isTTY = term.IsTerminal(int(f.Fd()))
	}
	return &Terminal{
		out:   out,
		width: 80,
		isTTY: isTTY,
	}
}

// NewTerminalWithTTY creates a terminal output helper with explicit TTY mode.
func NewTerminalWithTTY(out io.Writer, isTTY bool) *Terminal {
	return &Terminal{
		out:   out,
		width: 80,
		isTTY: isTTY,
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

	if !t.isTTY {
		// Plain output for pipes/logs
		fmt.Fprintf(t.out, "[job] %s@%s (%s)\n", repoShort, refDisplay, commitShort)
		fmt.Fprintf(t.out, "[job] running: %s\n", command)
		return
	}

	// Format forge name for display based on hostname
	forgeDisplay := forgeDisplayName(repo, forge)

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
	durationStr := formatDuration(duration)

	if !t.isTTY {
		// Plain output for pipes/logs
		if exitCode == 0 {
			fmt.Fprintf(t.out, "[job] exit 0 (%s)\n", durationStr)
		} else {
			fmt.Fprintf(t.out, "[job] exit %d (%s)\n", exitCode, durationStr)
		}
		return
	}

	fmt.Fprintln(t.out)
	t.printLine("━")

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
	if !t.isTTY {
		// Plain output for pipes/logs
		fmt.Fprintf(t.out, "[job] error (%s): %s\n", phase, errMsg)
		return
	}

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
	if !t.isTTY {
		// Plain output for pipes/logs
		fmt.Fprintf(t.out, "[worker] connected to %s as %s\n", server, user)
		fmt.Fprintf(t.out, "[worker] waiting for jobs\n")
		return
	}

	fmt.Fprintf(t.out, "%s%s✓ Connected%s as %s to %s\n", colorBold, colorGreen, colorReset, user, server)
	fmt.Fprintf(t.out, "%sWaiting for jobs...%s\n", colorDim, colorReset)
}

// PrintJobClaimed prints when a job is claimed (non-TTY only, TTY uses PrintJobStart).
func (t *Terminal) PrintJobClaimed(jobID string) {
	if !t.isTTY {
		fmt.Fprintf(t.out, "[worker] claiming job %s\n", jobID)
	}
	// TTY mode: job start banner handles this
}

// PrintCloning prints when cloning starts (non-TTY only).
func (t *Terminal) PrintCloning(repo, ref string) {
	if !t.isTTY {
		repoShort := parseRepoShort(repo)
		fmt.Fprintf(t.out, "[job] cloning %s@%s\n", repoShort, ref)
	}
	// TTY mode: included in job start banner
}

// PrintJobWaiting prints the waiting message after a job completes.
func (t *Terminal) PrintJobWaiting() {
	if !t.isTTY {
		fmt.Fprintf(t.out, "[worker] job complete, waiting for next\n")
	}
	// TTY mode: job complete banner is sufficient
}

// PrintShutdown prints the shutdown message.
func (t *Terminal) PrintShutdown() {
	if !t.isTTY {
		fmt.Fprintf(t.out, "[worker] shutting down\n")
	} else {
		fmt.Fprintln(t.out, "\nShutting down...")
	}
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

// forgeDisplayName returns a user-friendly forge name based on the clone URL and forge type.
// Known hosted services get their brand name, self-hosted instances show the forge type.
func forgeDisplayName(cloneURL, forgeType string) string {
	// Extract hostname from clone URL
	host := parseHostFromURL(cloneURL)

	// Map known hosted services to friendly names
	switch host {
	case "github.com":
		return "GITHUB"
	case "gitlab.com":
		return "GITLAB"
	case "codeberg.org":
		return "CODEBERG"
	case "gitea.com":
		return "GITEA"
	}

	// For self-hosted or unknown hosts, use the forge type
	if forgeType != "" {
		return strings.ToUpper(forgeType)
	}
	return "BUILD"
}

// parseHostFromURL extracts the hostname from a clone URL.
func parseHostFromURL(cloneURL string) string {
	// Handle https://github.com/owner/repo.git
	httpsRe := regexp.MustCompile(`https?://([^/]+)`)
	if matches := httpsRe.FindStringSubmatch(cloneURL); len(matches) > 1 {
		return strings.ToLower(matches[1])
	}

	// Handle git@github.com:owner/repo.git
	sshRe := regexp.MustCompile(`git@([^:]+):`)
	if matches := sshRe.FindStringSubmatch(cloneURL); len(matches) > 1 {
		return strings.ToLower(matches[1])
	}

	return ""
}
