package worker

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestParseRepoShort(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://github.com/owner/repo.git", "owner/repo"},
		{"https://github.com/owner/repo", "owner/repo"},
		{"https://gitlab.example.com/org/project.git", "org/project"},
		{"git@github.com:owner/repo.git", "owner/repo"},
		{"git@gitlab.example.com:org/project.git", "org/project"},
		{"simple", "simple"}, // fallback
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseRepoShort(tt.input)
			if result != tt.expected {
				t.Errorf("parseRepoShort(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d        time.Duration
		expected string
	}{
		{500 * time.Millisecond, "500ms"},
		{1 * time.Second, "1.0s"},
		{12*time.Second + 345*time.Millisecond, "12.3s"},
		{59 * time.Second, "59.0s"},
		{60 * time.Second, "1m0s"},
		{2*time.Minute + 15*time.Second, "2m15s"},
		{1*time.Hour + 5*time.Minute, "1h5m"},
	}

	for _, tt := range tests {
		t.Run(tt.d.String(), func(t *testing.T) {
			result := formatDuration(tt.d)
			if result != tt.expected {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.d, result, tt.expected)
			}
		})
	}
}

func TestForgeDisplayName(t *testing.T) {
	tests := []struct {
		cloneURL  string
		forgeType string
		expected  string
	}{
		// Known hosted services
		{"https://github.com/owner/repo.git", "github", "GITHUB"},
		{"https://gitlab.com/owner/repo.git", "gitlab", "GITLAB"},
		{"https://codeberg.org/owner/repo.git", "forgejo", "CODEBERG"},
		{"https://gitea.com/owner/repo.git", "gitea", "GITEA"},
		// Self-hosted instances show forge type
		{"https://gitlab.mycompany.com/owner/repo.git", "gitlab", "GITLAB"},
		{"https://git.example.org/owner/repo.git", "forgejo", "FORGEJO"},
		{"https://gitea.internal/owner/repo.git", "gitea", "GITEA"},
		// SSH URLs
		{"git@github.com:owner/repo.git", "github", "GITHUB"},
		{"git@codeberg.org:owner/repo.git", "forgejo", "CODEBERG"},
		// Fallback
		{"", "", "BUILD"},
		{"", "github", "GITHUB"},
	}

	for _, tt := range tests {
		t.Run(tt.cloneURL+"/"+tt.forgeType, func(t *testing.T) {
			result := forgeDisplayName(tt.cloneURL, tt.forgeType)
			if result != tt.expected {
				t.Errorf("forgeDisplayName(%q, %q) = %q, want %q", tt.cloneURL, tt.forgeType, result, tt.expected)
			}
		})
	}
}

func TestTerminalPrintJobStart(t *testing.T) {
	var buf bytes.Buffer
	term := NewTerminalWithTTY(&buf, true) // Force TTY mode for ANSI output

	term.PrintJobStart("https://github.com/owner/repo.git", "main", "", "abc1234567890", "make build", "bare-metal", "github")

	output := buf.String()

	// Check key content is present
	if !strings.Contains(output, "GITHUB STARTED") {
		t.Error("missing GITHUB STARTED")
	}
	if !strings.Contains(output, "owner/repo") {
		t.Error("missing owner/repo")
	}
	if !strings.Contains(output, "main") {
		t.Error("missing branch")
	}
	if !strings.Contains(output, "abc12345") {
		t.Error("missing short commit")
	}
	if !strings.Contains(output, "make build") {
		t.Error("missing command")
	}
	if !strings.Contains(output, "bare-metal") {
		t.Error("missing mode")
	}
}

func TestTerminalPrintJobStartNonTTY(t *testing.T) {
	var buf bytes.Buffer
	term := NewTerminalWithTTY(&buf, false) // Non-TTY mode

	term.PrintJobStart("https://github.com/owner/repo.git", "main", "", "abc1234567890", "make build", "bare-metal", "github")

	output := buf.String()

	// Non-TTY output should be plain prefixed text
	if !strings.Contains(output, "[job] owner/repo@main") {
		t.Error("missing [job] owner/repo@main - got: " + output)
	}
	if !strings.Contains(output, "[job] running: make build") {
		t.Error("missing [job] running: make build")
	}
	// Should NOT contain ANSI decorations
	if strings.Contains(output, "GITHUB STARTED") {
		t.Error("TTY banner should not appear in non-TTY mode")
	}
}

func TestTerminalPrintJobStartCodeberg(t *testing.T) {
	var buf bytes.Buffer
	term := NewTerminalWithTTY(&buf, true) // Force TTY mode for ANSI output

	term.PrintJobStart("https://codeberg.org/owner/repo.git", "main", "", "abc1234567890", "make build", "bare-metal", "forgejo")

	output := buf.String()

	// Should show CODEBERG, not FORGEJO
	if !strings.Contains(output, "CODEBERG STARTED") {
		t.Error("missing CODEBERG STARTED - got: " + output)
	}
}

func TestTerminalPrintJobComplete(t *testing.T) {
	t.Run("success TTY", func(t *testing.T) {
		var buf bytes.Buffer
		term := NewTerminalWithTTY(&buf, true)
		term.PrintJobComplete(0, 12*time.Second)

		output := buf.String()
		if !strings.Contains(output, "BUILD PASSED") {
			t.Error("missing BUILD PASSED")
		}
		if !strings.Contains(output, "12.0s") {
			t.Error("missing duration")
		}
	})

	t.Run("failure TTY", func(t *testing.T) {
		var buf bytes.Buffer
		term := NewTerminalWithTTY(&buf, true)
		term.PrintJobComplete(1, 5*time.Second)

		output := buf.String()
		if !strings.Contains(output, "BUILD FAILED") {
			t.Error("missing BUILD FAILED")
		}
		if !strings.Contains(output, "exit 1") {
			t.Error("missing exit code")
		}
	})

	t.Run("success non-TTY", func(t *testing.T) {
		var buf bytes.Buffer
		term := NewTerminalWithTTY(&buf, false)
		term.PrintJobComplete(0, 12*time.Second)

		output := buf.String()
		if !strings.Contains(output, "[job] exit 0 (12.0s)") {
			t.Error("expected [job] exit 0 (12.0s) - got: " + output)
		}
	})

	t.Run("failure non-TTY", func(t *testing.T) {
		var buf bytes.Buffer
		term := NewTerminalWithTTY(&buf, false)
		term.PrintJobComplete(1, 5*time.Second)

		output := buf.String()
		if !strings.Contains(output, "[job] exit 1 (5.0s)") {
			t.Error("expected [job] exit 1 (5.0s) - got: " + output)
		}
	})
}

func TestTerminalPrintJobError(t *testing.T) {
	t.Run("TTY", func(t *testing.T) {
		var buf bytes.Buffer
		term := NewTerminalWithTTY(&buf, true)
		term.PrintJobError("clone", "permission denied")

		output := buf.String()
		if !strings.Contains(output, "BUILD ERROR") {
			t.Error("missing BUILD ERROR")
		}
		if !strings.Contains(output, "clone") {
			t.Error("missing phase")
		}
		if !strings.Contains(output, "permission denied") {
			t.Error("missing error message")
		}
	})

	t.Run("non-TTY", func(t *testing.T) {
		var buf bytes.Buffer
		term := NewTerminalWithTTY(&buf, false)
		term.PrintJobError("clone", "permission denied")

		output := buf.String()
		if !strings.Contains(output, "[job] error (clone): permission denied") {
			t.Error("expected plain error format - got: " + output)
		}
	})
}

func TestTerminalPrintConnected(t *testing.T) {
	t.Run("TTY", func(t *testing.T) {
		var buf bytes.Buffer
		term := NewTerminalWithTTY(&buf, true)
		term.PrintConnected("user@example.com", "https://cinch.sh")

		output := buf.String()
		if !strings.Contains(output, "Connected") {
			t.Error("missing Connected")
		}
		if !strings.Contains(output, "user@example.com") {
			t.Error("missing user")
		}
	})

	t.Run("non-TTY", func(t *testing.T) {
		var buf bytes.Buffer
		term := NewTerminalWithTTY(&buf, false)
		term.PrintConnected("user@example.com", "https://cinch.sh")

		output := buf.String()
		if !strings.Contains(output, "[worker] connected to https://cinch.sh as user@example.com") {
			t.Error("expected plain connected format - got: " + output)
		}
		if !strings.Contains(output, "[worker] waiting for jobs") {
			t.Error("expected waiting message")
		}
	})
}
