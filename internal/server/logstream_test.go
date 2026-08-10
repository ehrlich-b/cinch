package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ehrlich-b/cinch/internal/storage"
	"github.com/gorilla/websocket"
)

// setupPrivateLogStream seeds the given store with a PRIVATE repo owned by
// ownerID, a running job in it, and a distinctive secret line in the job logs.
// It returns the job and the secret log text.
func setupPrivateLogStream(t *testing.T, store storage.Storage, ownerID string) (*storage.Job, string) {
	t.Helper()
	ctx := context.Background()

	repo := &storage.Repo{
		ID:          "r_private",
		ForgeType:   storage.ForgeTypeGitHub,
		Owner:       "alice",
		Name:        "secret-repo",
		CloneURL:    "https://github.com/alice/secret-repo.git",
		Private:     true,
		OwnerUserID: ownerID,
		CreatedAt:   time.Now(),
	}
	if err := store.CreateRepo(ctx, repo); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	job := &storage.Job{
		ID:        "j_private",
		RepoID:    repo.ID,
		Commit:    "abc123",
		Branch:    "main",
		Status:    storage.JobStatusRunning,
		CreatedAt: time.Now(),
	}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	const secretLog = "super-secret-token-placeholder\n"
	if err := store.AppendLog(ctx, job.ID, "stdout", secretLog); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}

	return job, strings.TrimSpace(secretLog)
}

// serveLogStream starts an httptest server exposing the log stream handler.
func serveLogStream(t *testing.T, h *LogStreamHandler) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(h.ServeHTTP))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/logs/"
}

// authCookieHeader produces a Cookie header value for the given auth handler.
func authCookieHeader(t *testing.T, auth *AuthHandler, email string) string {
	t.Helper()
	w := httptest.NewRecorder()
	if err := auth.SetAuthCookie(w, email); err != nil {
		t.Fatalf("SetAuthCookie: %v", err)
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("SetAuthCookie produced no cookies")
	}
	return cookies[0].String()
}

// TestLogStreamOwnerStreamsOwnRun verifies the repo owner can stream a private
// run's logs over the WebSocket endpoint.
func TestLogStreamOwnerStreamsOwnRun(t *testing.T) {
	store, _ := storage.NewSQLite(":memory:", "", "")
	t.Cleanup(func() { store.Close() })

	auth, owner := setupTestAuth(t, store)
	job, secretLog := setupPrivateLogStream(t, store, owner.ID)

	logStream := NewLogStreamHandler(store, auth, nil)
	wsBase := serveLogStream(t, logStream)

	hdr := http.Header{}
	hdr.Add("Cookie", authCookieHeader(t, auth, owner.Email))
	conn, resp, err := websocket.DefaultDialer.Dial(wsBase+job.ID, hdr)
	if err != nil {
		t.Fatalf("owner dial failed: %v", err)
	}
	defer conn.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("handshake status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
	}

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for i := 0; i < 10; i++ {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("owner read failed before receiving logs: %v", err)
		}
		if strings.Contains(string(data), secretLog) {
			return // owner received their own run's log bytes
		}
	}
	t.Error("owner never received log bytes for their own private run")
}

// TestLogStreamCrossTenantDenied verifies an authenticated user of tenant B
// is denied (403, matching the repo's cross-tenant convention) with NO log
// bytes leaked when requesting tenant A's private run.
func TestLogStreamCrossTenantDenied(t *testing.T) {
	store, _ := storage.NewSQLite(":memory:", "", "")
	t.Cleanup(func() { store.Close() })

	auth, owner := setupTestAuth(t, store)

	// Tenant B is authenticated, but does not own tenant A's repo.
	if _, err := store.GetOrCreateUserByEmail(context.Background(), "tenant-b@example.com", "tenant-b"); err != nil {
		t.Fatalf("create tenant B user: %v", err)
	}

	job, secretLog := setupPrivateLogStream(t, store, owner.ID)

	logStream := NewLogStreamHandler(store, auth, nil)
	wsBase := serveLogStream(t, logStream)

	hdr := http.Header{}
	hdr.Add("Cookie", authCookieHeader(t, auth, "tenant-b@example.com"))
	_, resp, err := websocket.DefaultDialer.Dial(wsBase+job.ID, hdr)
	if err == nil {
		t.Fatal("cross-tenant dial unexpectedly succeeded")
	}
	if resp == nil {
		t.Fatalf("cross-tenant dial returned no response: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-tenant status = %d, want %d (forbidden)", resp.StatusCode, http.StatusForbidden)
	}

	// The denial response must not contain any of the run's log bytes.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read denial body: %v", err)
	}
	if strings.Contains(string(body), secretLog) {
		t.Error("log bytes leaked to a non-owner tenant in the denial response")
	}
}

// TestLogStreamUnauthenticatedDenied verifies an unauthenticated request for a
// private run is denied (401) with NO log bytes leaked.
func TestLogStreamUnauthenticatedDenied(t *testing.T) {
	store, _ := storage.NewSQLite(":memory:", "", "")
	t.Cleanup(func() { store.Close() })

	_, owner := setupTestAuth(t, store)
	job, secretLog := setupPrivateLogStream(t, store, owner.ID)

	// No auth handler configured is equivalent to no credentials being present.
	logStream := NewLogStreamHandler(store, nil, nil)
	wsBase := serveLogStream(t, logStream)

	_, resp, err := websocket.DefaultDialer.Dial(wsBase+job.ID, nil)
	if err == nil {
		t.Fatal("unauthenticated dial unexpectedly succeeded")
	}
	if resp == nil {
		t.Fatalf("unauthenticated dial returned no response: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d (unauthorized)", resp.StatusCode, http.StatusUnauthorized)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read denial body: %v", err)
	}
	if strings.Contains(string(body), secretLog) {
		t.Error("log bytes leaked to an unauthenticated caller")
	}
}
