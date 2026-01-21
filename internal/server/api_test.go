package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ehrlich-b/cinch/internal/storage"
)

func TestAPIListJobs(t *testing.T) {
	store, _ := storage.NewSQLite(":memory:")
	defer store.Close()

	// Create repo for foreign key
	repo := &storage.Repo{
		ID:        "r_1",
		ForgeType: storage.ForgeTypeGitHub,
		CloneURL:  "https://github.com/test/repo.git",
		CreatedAt: time.Now(),
	}
	_ = store.CreateRepo(t.Context(), repo)

	// Create some jobs
	for i := 0; i < 3; i++ {
		job := &storage.Job{
			ID:        "j_" + string(rune('a'+i)),
			RepoID:    "r_1",
			Commit:    "abc123",
			Branch:    "main",
			Status:    storage.JobStatusPending,
			CreatedAt: time.Now(),
		}
		_ = store.CreateJob(t.Context(), job)
	}

	api := NewAPIHandler(store, nil, nil)

	req := httptest.NewRequest("GET", "/api/jobs", nil)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Jobs []jobResponse `json:"jobs"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(resp.Jobs) != 3 {
		t.Errorf("len(jobs) = %d, want 3", len(resp.Jobs))
	}
}

func TestAPIListJobsWithFilters(t *testing.T) {
	store, _ := storage.NewSQLite(":memory:")
	defer store.Close()

	// Create repo
	repo := &storage.Repo{
		ID:        "r_1",
		ForgeType: storage.ForgeTypeGitHub,
		CloneURL:  "https://github.com/test/repo.git",
		CreatedAt: time.Now(),
	}
	_ = store.CreateRepo(t.Context(), repo)

	// Create jobs with different statuses
	job1 := &storage.Job{
		ID:        "j_1",
		RepoID:    "r_1",
		Branch:    "main",
		Status:    storage.JobStatusSuccess,
		CreatedAt: time.Now(),
	}
	job2 := &storage.Job{
		ID:        "j_2",
		RepoID:    "r_1",
		Branch:    "main",
		Status:    storage.JobStatusFailed,
		CreatedAt: time.Now(),
	}
	_ = store.CreateJob(t.Context(), job1)
	_ = store.CreateJob(t.Context(), job2)

	api := NewAPIHandler(store, nil, nil)

	// Filter by status
	req := httptest.NewRequest("GET", "/api/jobs?status=success", nil)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)

	var resp struct {
		Jobs []jobResponse `json:"jobs"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.Jobs) != 1 {
		t.Errorf("len(jobs) = %d, want 1", len(resp.Jobs))
	}
	if len(resp.Jobs) > 0 && resp.Jobs[0].Status != "success" {
		t.Errorf("status = %s, want success", resp.Jobs[0].Status)
	}
}

func TestAPIGetJob(t *testing.T) {
	store, _ := storage.NewSQLite(":memory:")
	defer store.Close()

	// Create repo
	_ = store.CreateRepo(t.Context(), &storage.Repo{
		ID:        "r_1",
		ForgeType: storage.ForgeTypeGitHub,
		CloneURL:  "https://github.com/test/repo.git",
		CreatedAt: time.Now(),
	})

	exitCode := 0
	job := &storage.Job{
		ID:        "j_test",
		RepoID:    "r_1",
		Commit:    "abc123def",
		Branch:    "feature",
		Status:    storage.JobStatusSuccess,
		ExitCode:  &exitCode,
		CreatedAt: time.Now(),
	}
	_ = store.CreateJob(t.Context(), job)

	api := NewAPIHandler(store, nil, nil)

	req := httptest.NewRequest("GET", "/api/jobs/j_test", nil)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var got jobResponse
	_ = json.NewDecoder(w.Body).Decode(&got)

	if got.ID != "j_test" {
		t.Errorf("ID = %s, want j_test", got.ID)
	}
	if got.Commit != "abc123def" {
		t.Errorf("Commit = %s, want abc123def", got.Commit)
	}
	if got.Branch != "feature" {
		t.Errorf("Branch = %s, want feature", got.Branch)
	}
}

func TestAPIGetJobNotFound(t *testing.T) {
	store, _ := storage.NewSQLite(":memory:")
	defer store.Close()

	api := NewAPIHandler(store, nil, nil)

	req := httptest.NewRequest("GET", "/api/jobs/nonexistent", nil)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestAPIGetJobLogs(t *testing.T) {
	store, _ := storage.NewSQLite(":memory:")
	defer store.Close()

	// Create repo and job
	_ = store.CreateRepo(t.Context(), &storage.Repo{
		ID:        "r_1",
		ForgeType: storage.ForgeTypeGitHub,
		CloneURL:  "https://github.com/test/repo.git",
		CreatedAt: time.Now(),
	})
	_ = store.CreateJob(t.Context(), &storage.Job{
		ID:        "j_1",
		RepoID:    "r_1",
		Status:    storage.JobStatusRunning,
		CreatedAt: time.Now(),
	})

	// Add logs
	_ = store.AppendLog(t.Context(), "j_1", "stdout", "Hello world\n")
	_ = store.AppendLog(t.Context(), "j_1", "stderr", "Warning\n")

	api := NewAPIHandler(store, nil, nil)

	req := httptest.NewRequest("GET", "/api/jobs/j_1/logs", nil)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var logs []struct {
		Stream string `json:"stream"`
		Data   string `json:"data"`
	}
	_ = json.NewDecoder(w.Body).Decode(&logs)

	if len(logs) != 2 {
		t.Errorf("len(logs) = %d, want 2", len(logs))
	}
}

func TestAPIListWorkers(t *testing.T) {
	store, _ := storage.NewSQLite(":memory:")
	defer store.Close()

	// Create workers
	_ = store.CreateWorker(t.Context(), &storage.Worker{
		ID:        "w_1",
		Name:      "worker-1",
		Labels:    []string{"linux", "docker"},
		Status:    storage.WorkerStatusOnline,
		LastSeen:  time.Now(),
		CreatedAt: time.Now(),
	})

	hub := NewHub()
	api := NewAPIHandler(store, hub, nil)

	req := httptest.NewRequest("GET", "/api/workers", nil)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Workers []workerResponse `json:"workers"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.Workers) != 1 {
		t.Errorf("len(workers) = %d, want 1", len(resp.Workers))
	}
	if len(resp.Workers) > 0 && resp.Workers[0].Name != "worker-1" {
		t.Errorf("Name = %s, want worker-1", resp.Workers[0].Name)
	}
}

func TestAPICreateRepo(t *testing.T) {
	store, _ := storage.NewSQLite(":memory:")
	defer store.Close()

	api := NewAPIHandler(store, nil, nil)

	body := `{
		"forge_type": "github",
		"owner": "myorg",
		"name": "myrepo",
		"clone_url": "https://github.com/myorg/myrepo.git",
		"html_url": "https://github.com/myorg/myrepo",
		"forge_token": "ghp_xxxx",
		"command": "make test"
	}`

	req := httptest.NewRequest("POST", "/api/repos", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var repo repoResponse
	_ = json.NewDecoder(w.Body).Decode(&repo)

	if repo.Owner != "myorg" {
		t.Errorf("Owner = %s, want myorg", repo.Owner)
	}
	if repo.Name != "myrepo" {
		t.Errorf("Name = %s, want myrepo", repo.Name)
	}
	if repo.Command != "make test" {
		t.Errorf("Command = %s, want make test", repo.Command)
	}
	if repo.WebhookSecret == "" {
		t.Error("WebhookSecret should be generated")
	}
}

func TestAPICreateRepoMissingFields(t *testing.T) {
	store, _ := storage.NewSQLite(":memory:")
	defer store.Close()

	api := NewAPIHandler(store, nil, nil)

	body := `{"forge_type": "github"}`

	req := httptest.NewRequest("POST", "/api/repos", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAPIListRepos(t *testing.T) {
	store, _ := storage.NewSQLite(":memory:")
	defer store.Close()

	// Create repos
	_ = store.CreateRepo(t.Context(), &storage.Repo{
		ID:        "r_1",
		ForgeType: storage.ForgeTypeGitHub,
		Owner:     "org1",
		Name:      "repo1",
		CloneURL:  "https://github.com/org1/repo1.git",
		CreatedAt: time.Now(),
	})
	_ = store.CreateRepo(t.Context(), &storage.Repo{
		ID:        "r_2",
		ForgeType: storage.ForgeTypeForgejo,
		Owner:     "org2",
		Name:      "repo2",
		CloneURL:  "https://forgejo.example.com/org2/repo2.git",
		CreatedAt: time.Now(),
	})

	api := NewAPIHandler(store, nil, nil)

	req := httptest.NewRequest("GET", "/api/repos", nil)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var repos []repoResponse
	_ = json.NewDecoder(w.Body).Decode(&repos)

	if len(repos) != 2 {
		t.Errorf("len(repos) = %d, want 2", len(repos))
	}
}

func TestAPIGetRepo(t *testing.T) {
	store, _ := storage.NewSQLite(":memory:")
	defer store.Close()

	_ = store.CreateRepo(t.Context(), &storage.Repo{
		ID:            "r_1",
		ForgeType:     storage.ForgeTypeGitHub,
		Owner:         "myorg",
		Name:          "myrepo",
		CloneURL:      "https://github.com/myorg/myrepo.git",
		WebhookSecret: "secret123",
		CreatedAt:     time.Now(),
	})

	api := NewAPIHandler(store, nil, nil)

	req := httptest.NewRequest("GET", "/api/repos/r_1", nil)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var repo repoResponse
	_ = json.NewDecoder(w.Body).Decode(&repo)

	if repo.Owner != "myorg" {
		t.Errorf("Owner = %s, want myorg", repo.Owner)
	}
	// Webhook secret should be included in single repo view
	if repo.WebhookSecret != "secret123" {
		t.Errorf("WebhookSecret = %s, want secret123", repo.WebhookSecret)
	}
}

func TestAPIDeleteRepo(t *testing.T) {
	store, _ := storage.NewSQLite(":memory:")
	defer store.Close()

	_ = store.CreateRepo(t.Context(), &storage.Repo{
		ID:        "r_1",
		ForgeType: storage.ForgeTypeGitHub,
		CloneURL:  "https://github.com/test/repo.git",
		CreatedAt: time.Now(),
	})

	api := NewAPIHandler(store, nil, nil)

	req := httptest.NewRequest("DELETE", "/api/repos/r_1", nil)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}

	// Verify deleted
	_, err := store.GetRepo(t.Context(), "r_1")
	if err != storage.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestAPICreateToken(t *testing.T) {
	store, _ := storage.NewSQLite(":memory:")
	defer store.Close()

	api := NewAPIHandler(store, nil, nil)

	body := `{"name": "my-worker-token"}`

	req := httptest.NewRequest("POST", "/api/tokens", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp createTokenResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp.Name != "my-worker-token" {
		t.Errorf("Name = %s, want my-worker-token", resp.Name)
	}
	if resp.Token == "" {
		t.Error("Token should be returned")
	}
	if len(resp.Token) != 64 { // 32 bytes hex encoded
		t.Errorf("Token length = %d, want 64", len(resp.Token))
	}
}

func TestAPIListTokens(t *testing.T) {
	store, _ := storage.NewSQLite(":memory:")
	defer store.Close()

	_ = store.CreateToken(t.Context(), &storage.Token{
		ID:        "t_1",
		Name:      "token-1",
		Hash:      "hash1",
		CreatedAt: time.Now(),
	})

	api := NewAPIHandler(store, nil, nil)

	req := httptest.NewRequest("GET", "/api/tokens", nil)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var tokens []tokenResponse
	_ = json.NewDecoder(w.Body).Decode(&tokens)

	if len(tokens) != 1 {
		t.Errorf("len(tokens) = %d, want 1", len(tokens))
	}
	if len(tokens) > 0 && tokens[0].Name != "token-1" {
		t.Errorf("Name = %s, want token-1", tokens[0].Name)
	}
}

func TestAPIRevokeToken(t *testing.T) {
	store, _ := storage.NewSQLite(":memory:")
	defer store.Close()

	_ = store.CreateToken(t.Context(), &storage.Token{
		ID:        "t_1",
		Name:      "token-1",
		Hash:      "hash1",
		CreatedAt: time.Now(),
	})

	api := NewAPIHandler(store, nil, nil)

	req := httptest.NewRequest("DELETE", "/api/tokens/t_1", nil)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}

	// Verify revoked
	tokens, _ := store.ListTokens(t.Context())
	for _, tok := range tokens {
		if tok.ID == "t_1" && tok.RevokedAt == nil {
			t.Error("token should be revoked")
		}
	}
}

func TestAPINotFound(t *testing.T) {
	store, _ := storage.NewSQLite(":memory:")
	defer store.Close()

	api := NewAPIHandler(store, nil, nil)

	req := httptest.NewRequest("GET", "/api/unknown", nil)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestAPIMethodNotAllowed(t *testing.T) {
	store, _ := storage.NewSQLite(":memory:")
	defer store.Close()

	api := NewAPIHandler(store, nil, nil)

	req := httptest.NewRequest("PUT", "/api/jobs", nil)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestGenerateSecret(t *testing.T) {
	secret1, err := generateSecret(32)
	if err != nil {
		t.Fatalf("generateSecret failed: %v", err)
	}

	secret2, err := generateSecret(32)
	if err != nil {
		t.Fatalf("generateSecret failed: %v", err)
	}

	if len(secret1) != 64 { // 32 bytes hex encoded
		t.Errorf("secret length = %d, want 64", len(secret1))
	}

	if secret1 == secret2 {
		t.Error("secrets should be unique")
	}
}

func TestAPIInvalidJSON(t *testing.T) {
	store, _ := storage.NewSQLite(":memory:")
	defer store.Close()

	api := NewAPIHandler(store, nil, nil)

	req := httptest.NewRequest("POST", "/api/repos", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
