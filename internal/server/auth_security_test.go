package server

import (
	"log/slog"
	"testing"
	"time"
)

func TestSanitizeReturnTo(t *testing.T) {
	tests := []struct {
		name     string
		returnTo string
		baseURL  string
		want     string
	}{
		{
			name:     "empty return path falls back to root",
			returnTo: "",
			baseURL:  "https://cinch.sh",
			want:     "/",
		},
		{
			name:     "plain relative path returned unchanged",
			returnTo: "/jobs/123",
			baseURL:  "https://cinch.sh",
			want:     "/jobs/123",
		},
		{
			// Classic open-redirect bypass: protocol-relative URL that strips the
			// scheme and would redirect to an attacker-controlled host.
			name:     "protocol-relative URL rejected",
			returnTo: "//evil.com/phish",
			baseURL:  "https://cinch.sh",
			want:     "/",
		},
		{
			name:     "absolute URL with exact matching host returned unchanged",
			returnTo: "https://cinch.sh/account",
			baseURL:  "https://cinch.sh",
			want:     "https://cinch.sh/account",
		},
		{
			name:     "absolute URL on a different host rejected",
			returnTo: "https://evil.com/phish",
			baseURL:  "https://cinch.sh",
			want:     "/",
		},
		{
			// Exact host match must reject a same-prefix subdomain; a naive
			// suffix/prefix check would let cinch.sh.evil.com through.
			name:     "subdomain with matching prefix rejected",
			returnTo: "https://cinch.sh.evil.com/x",
			baseURL:  "https://cinch.sh",
			want:     "/",
		},
		{
			name:     "absolute URL with no base URL to validate against rejected",
			returnTo: "https://evil.com/phish",
			baseURL:  "",
			want:     "/",
		},
		{
			// Control character makes url.Parse fail, so the absolute-URL branch
			// cannot validate it and must reject.
			name:     "malformed URL failing url.Parse rejected",
			returnTo: "http://evil.com/\x00x",
			baseURL:  "https://cinch.sh",
			want:     "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeReturnTo(tt.returnTo, tt.baseURL); got != tt.want {
				t.Errorf("sanitizeReturnTo(%q, %q) = %q, want %q", tt.returnTo, tt.baseURL, got, tt.want)
			}
		})
	}
}

func TestDeviceVerifyRateLimit(t *testing.T) {
	h := &AuthHandler{
		deviceVerifyAttempts: make(map[string]*deviceVerifyAttempt),
		log:                  slog.Default(),
	}

	t.Run("unknown IP not blocked", func(t *testing.T) {
		blocked, retryAfter := h.checkDeviceVerifyRateLimit("93.184.216.34")
		if blocked {
			t.Fatalf("unknown IP reported blocked")
		}
		if retryAfter != 0 {
			t.Fatalf("unknown IP retryAfter = %d, want 0", retryAfter)
		}
	})

	ip := "198.51.100.1"

	t.Run("below threshold not blocked", func(t *testing.T) {
		for i := 0; i < deviceVerifyMaxAttempts-1; i++ {
			h.recordDeviceVerifyAttempt(ip, false)
		}
		blocked, retryAfter := h.checkDeviceVerifyRateLimit(ip)
		if blocked {
			t.Fatalf("IP with %d failures reported blocked", deviceVerifyMaxAttempts-1)
		}
		if retryAfter != 0 {
			t.Fatalf("retryAfter = %d, want 0", retryAfter)
		}
	})

	t.Run("threshold attempt blocks", func(t *testing.T) {
		h.recordDeviceVerifyAttempt(ip, false)
		blocked, retryAfter := h.checkDeviceVerifyRateLimit(ip)
		if !blocked {
			t.Fatalf("IP with %d failures not blocked", deviceVerifyMaxAttempts)
		}
		if retryAfter <= 0 {
			t.Fatalf("retryAfter = %d, want > 0", retryAfter)
		}
	})

	blockedIP := "198.51.100.2"

	t.Run("still blocked after further failures", func(t *testing.T) {
		for i := 0; i < deviceVerifyMaxAttempts; i++ {
			h.recordDeviceVerifyAttempt(blockedIP, false)
		}
		blocked, retryAfter := h.checkDeviceVerifyRateLimit(blockedIP)
		if !blocked {
			t.Fatalf("blocked IP reported as not blocked")
		}
		if retryAfter <= 0 {
			t.Fatalf("retryAfter = %d, want > 0", retryAfter)
		}
		h.recordDeviceVerifyAttempt(blockedIP, false)
		blocked, retryAfter = h.checkDeviceVerifyRateLimit(blockedIP)
		if !blocked {
			t.Fatalf("blocked IP no longer blocked after further failures")
		}
		if retryAfter <= 0 {
			t.Fatalf("retryAfter = %d, want > 0", retryAfter)
		}
	})

	resetIP := "198.51.100.3"

	t.Run("successful attempt resets counter", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			h.recordDeviceVerifyAttempt(resetIP, false)
		}
		h.recordDeviceVerifyAttempt(resetIP, true)
		for i := 0; i < deviceVerifyMaxAttempts-1; i++ {
			h.recordDeviceVerifyAttempt(resetIP, false)
		}
		blocked, retryAfter := h.checkDeviceVerifyRateLimit(resetIP)
		if blocked {
			t.Fatalf("IP reset by success reported blocked")
		}
		if retryAfter != 0 {
			t.Fatalf("retryAfter = %d, want 0", retryAfter)
		}
		h.recordDeviceVerifyAttempt(resetIP, false)
		if blocked, _ := h.checkDeviceVerifyRateLimit(resetIP); !blocked {
			t.Fatalf("IP should block again after threshold reached past reset")
		}
	})

	maxedIP := "198.51.100.4"
	independentIP := "198.51.100.5"

	t.Run("IPs tracked independently", func(t *testing.T) {
		for i := 0; i < deviceVerifyMaxAttempts; i++ {
			h.recordDeviceVerifyAttempt(maxedIP, false)
		}
		if blocked, _ := h.checkDeviceVerifyRateLimit(maxedIP); !blocked {
			t.Fatalf("maxed IP not blocked")
		}
		blocked, retryAfter := h.checkDeviceVerifyRateLimit(independentIP)
		if blocked {
			t.Fatalf("unrelated IP reported blocked")
		}
		if retryAfter != 0 {
			t.Fatalf("retryAfter = %d, want 0", retryAfter)
		}
		h.recordDeviceVerifyAttempt(independentIP, false)
		if blocked, _ := h.checkDeviceVerifyRateLimit(independentIP); blocked {
			t.Fatalf("unrelated IP blocked after a single failure")
		}
	})

	t.Run("block expiry clears state", func(t *testing.T) {
		attempt, ok := h.deviceVerifyAttempts[blockedIP]
		if !ok {
			t.Fatalf("no attempt record for blocked IP")
		}
		if attempt.BlockedAt.IsZero() || !attempt.BlockedAt.After(time.Now()) {
			t.Fatalf("expected %q to be currently blocked, BlockedAt=%v", blockedIP, attempt.BlockedAt)
		}
		attempt.BlockedAt = time.Now().Add(-time.Second)

		blocked, retryAfter := h.checkDeviceVerifyRateLimit(blockedIP)
		if blocked {
			t.Fatalf("IP reported blocked after block expiry")
		}
		if retryAfter != 0 {
			t.Fatalf("retryAfter = %d, want 0", retryAfter)
		}
		if attempt.Count != 0 {
			t.Fatalf("Count = %d, want 0 after block expiry", attempt.Count)
		}
		if !attempt.ResetAt.After(time.Now()) {
			t.Fatalf("ResetAt = %v, want future", attempt.ResetAt)
		}
		if !attempt.BlockedAt.IsZero() {
			t.Fatalf("BlockedAt = %v, want zero after block expiry", attempt.BlockedAt)
		}
	})
}
