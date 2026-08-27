package cli

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"zero", 0, "0s"},
		{"a few seconds", 5 * time.Second, "5s"},
		{"exactly 59s", 59 * time.Second, "59s"},
		{"exactly 60s boundary", time.Minute, "1m0s"},
		{"a few minutes with nonzero seconds", 2*time.Minute + 30*time.Second, "2m30s"},
		{"just under an hour", 3599 * time.Second, "59m59s"},
		{"exactly an hour boundary", time.Hour, "1h0m"},
		{"multi-hour with nonzero minutes", 2*time.Hour + 30*time.Minute + 5*time.Second, "2h30m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatDuration(tt.d); got != tt.want {
				t.Errorf("FormatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestRelativeTime(t *testing.T) {
	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"just under a minute", time.Now().Add(-10 * time.Second), "just now"},
		{"exactly one minute", time.Now().Add(-time.Minute), "1 minute ago"},
		{"just over one minute", time.Now().Add(-61 * time.Second), "1 minute ago"},
		{"a few minutes", time.Now().Add(-5 * time.Minute), "5 minutes ago"},
		{"just under an hour", time.Now().Add(-59 * time.Minute), "59 minutes ago"},
		{"exactly one hour", time.Now().Add(-time.Hour), "1 hour ago"},
		{"just over one hour", time.Now().Add(-61 * time.Minute), "1 hour ago"},
		{"a few hours", time.Now().Add(-3 * time.Hour), "3 hours ago"},
		{"just under a day", time.Now().Add(-23 * time.Hour), "23 hours ago"},
		{"exactly one day", time.Now().Add(-24 * time.Hour), "1 day ago"},
		{"just over one day", time.Now().Add(-25 * time.Hour), "1 day ago"},
		{"several days", time.Now().Add(-72 * time.Hour), "3 days ago"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RelativeTime(tt.t); got != tt.want {
				t.Errorf("RelativeTime(%v) = %q, want %q", tt.t, got, tt.want)
			}
		})
	}
}

func TestStatusSymbol(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   string
	}{
		{"success", "success", "\033[32m✓\033[0m"},
		{"failed", "failed", "\033[31m✗\033[0m"},
		{"error", "error", "\033[31m✗\033[0m"},
		{"running", "running", "\033[33m●\033[0m"},
		{"pending", "pending", "\033[90m○\033[0m"},
		{"queued", "queued", "\033[90m○\033[0m"},
		{"pending_contributor", "pending_contributor", "\033[35m⏳\033[0m"},
		{"cancelled", "cancelled", "\033[90m⊘\033[0m"},
		{"empty", "", "?"},
		{"unrecognized", "bogus", "?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StatusSymbol(tt.status); got != tt.want {
				t.Errorf("StatusSymbol(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}
