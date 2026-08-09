package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRenderEmailSelectorEscapesEmails verifies that untrusted email options are
// HTML-escaped when rendered in the select-email page (previously XSS).
func TestRenderEmailSelectorEscapesEmails(t *testing.T) {
	h := NewAuthHandler(AuthConfig{JWTSecret: testJWTSecret}, nil, nil)

	w := httptest.NewRecorder()
	h.renderEmailSelector(w, []string{`<script>alert(1)</script>`, "user@example.com"}, "octocat", "/")

	body := w.Body.String()

	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("body does not contain escaped script tag, got:\n%s", body)
	}
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Errorf("body contains raw script tag, got:\n%s", body)
	}
}

// TestRenderEmailSelectorRenders verifies the select-email page still renders
// correctly for benign input.
func TestRenderEmailSelectorRenders(t *testing.T) {
	h := NewAuthHandler(AuthConfig{JWTSecret: testJWTSecret}, nil, nil)

	w := httptest.NewRecorder()
	h.renderEmailSelector(w, []string{"user@example.com"}, "octocat", "/")

	body := w.Body.String()

	for _, want := range []string{"Which email should we use?", "user@example.com", `action="/auth/select-email"`, `name="token"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q, got:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<script>") {
		t.Errorf("body unexpectedly contains script tag, got:\n%s", body)
	}
}

// TestDeviceVerifyEscapesUserCode verifies that a userCode supplied via the
// query parameter is HTML-escaped when rendered into the input value.
func TestDeviceVerifyEscapesUserCode(t *testing.T) {
	h := NewAuthHandler(AuthConfig{JWTSecret: testJWTSecret}, nil, nil)

	req := httptest.NewRequest("GET", "/auth/device/verify?code=%3Cscript%3Ealert(1)%3C/script%3E", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	body := w.Body.String()

	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("body does not contain escaped script tag, got:\n%s", body)
	}
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Errorf("body contains raw script tag, got:\n%s", body)
	}
}

// TestDeviceVerifyRendersUserCode verifies the device verify page still renders
// the user-provided code for benign input.
func TestDeviceVerifyRendersUserCode(t *testing.T) {
	h := NewAuthHandler(AuthConfig{JWTSecret: testJWTSecret}, nil, nil)

	req := httptest.NewRequest("GET", "/auth/device/verify?code=ABCD-1234", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	body := w.Body.String()

	for _, want := range []string{"Verify Device", "ABCD-1234", `name="code"`, "Authorize Device"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q, got:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<script>") {
		t.Errorf("body unexpectedly contains script tag, got:\n%s", body)
	}
}

// TestDeviceVerifyEscapesMessage verifies that a message is HTML-escaped when
// rendered on the device verify page.
func TestDeviceVerifyEscapesMessage(t *testing.T) {
	h := NewAuthHandler(AuthConfig{JWTSecret: testJWTSecret}, nil, nil)

	w := httptest.NewRecorder()
	h.renderDeviceVerifyPage(w, "ABCD-1234", `<script>alert(1)</script>`)

	body := w.Body.String()

	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("body does not contain escaped script tag, got:\n%s", body)
	}
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Errorf("body contains raw script tag, got:\n%s", body)
	}
}

// TestDeviceVerifyRendersMessage verifies the device verify page still renders
// a benign message.
func TestDeviceVerifyRendersMessage(t *testing.T) {
	h := NewAuthHandler(AuthConfig{JWTSecret: testJWTSecret}, nil, nil)

	w := httptest.NewRecorder()
	h.renderDeviceVerifyPage(w, "ABCD-1234", "Invalid or expired code")

	body := w.Body.String()

	for _, want := range []string{"Verify Device", "Invalid or expired code", `class="error"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q, got:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<script>") {
		t.Errorf("body unexpectedly contains script tag, got:\n%s", body)
	}
}
