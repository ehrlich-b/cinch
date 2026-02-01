package storage

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPostgresStorage(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping Postgres tests")
	}

	store, err := NewPostgres(dsn, "test-encryption-secret-32chars!", "")
	if err != nil {
		t.Fatalf("failed to create postgres storage: %v", err)
	}
	defer store.Close()

	// Clean up tables before tests
	cleanupPostgres(t, store)

	t.Run("Jobs", func(t *testing.T) {
		testPostgresJobs(t, store)
	})

	t.Run("Workers", func(t *testing.T) {
		testPostgresWorkers(t, store)
	})

	t.Run("Repos", func(t *testing.T) {
		testPostgresRepos(t, store)
	})

	t.Run("Tokens", func(t *testing.T) {
		testPostgresTokens(t, store)
	})

	t.Run("Users", func(t *testing.T) {
		testPostgresUsers(t, store)
	})

	t.Run("Logs", func(t *testing.T) {
		testPostgresLogs(t, store)
	})
}

func cleanupPostgres(t *testing.T, store *PostgresStorage) {
	t.Helper()
	// Delete in order due to foreign key constraints (job_logs references jobs)
	_, _ = store.db.Exec("DELETE FROM job_logs")
	_, _ = store.db.Exec("DELETE FROM jobs")
	_, _ = store.db.Exec("DELETE FROM tokens")
	_, _ = store.db.Exec("DELETE FROM workers")
	_, _ = store.db.Exec("DELETE FROM repos")
	_, _ = store.db.Exec("DELETE FROM users")
}

func testPostgresJobs(t *testing.T, store *PostgresStorage) {
	ctx := context.Background()

	// Create a repo first (foreign key)
	repo := &Repo{
		ID:            "r_test_job",
		ForgeType:     ForgeTypeGitHub,
		Owner:         "testowner",
		Name:          "testrepo",
		CloneURL:      "https://github.com/testowner/testrepo.git",
		HTMLURL:       "https://github.com/testowner/testrepo",
		WebhookSecret: "secret123",
		ForgeToken:    "token123",
		Build:         "make build",
		CreatedAt:     time.Now(),
	}
	if err := store.CreateRepo(ctx, repo); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	// Create job
	job := &Job{
		ID:         "j_test1",
		RepoID:     repo.ID,
		Commit:     "abc123",
		Branch:     "main",
		Status:     JobStatusPending,
		Author:     "testuser",
		TrustLevel: TrustOwner,
		CreatedAt:  time.Now(),
	}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	// Get job
	got, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.ID != job.ID {
		t.Errorf("GetJob ID = %q, want %q", got.ID, job.ID)
	}
	if got.Status != JobStatusPending {
		t.Errorf("GetJob Status = %q, want %q", got.Status, JobStatusPending)
	}

	// Update status
	if err := store.UpdateJobStatus(ctx, job.ID, JobStatusRunning, nil); err != nil {
		t.Fatalf("UpdateJobStatus: %v", err)
	}
	got, _ = store.GetJob(ctx, job.ID)
	if got.Status != JobStatusRunning {
		t.Errorf("Status after update = %q, want %q", got.Status, JobStatusRunning)
	}
	if got.StartedAt == nil {
		t.Error("StartedAt should be set after running")
	}

	// List jobs
	jobs, err := store.ListJobs(ctx, JobFilter{RepoID: repo.ID})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("ListJobs len = %d, want 1", len(jobs))
	}

	// Cleanup
	_, _ = store.db.Exec("DELETE FROM jobs WHERE id = $1", job.ID)
	_, _ = store.db.Exec("DELETE FROM repos WHERE id = $1", repo.ID)
}

func testPostgresWorkers(t *testing.T, store *PostgresStorage) {
	ctx := context.Background()

	worker := &Worker{
		ID:        "w_test1",
		Name:      "test-worker",
		Labels:    []string{"linux", "amd64"},
		Status:    WorkerStatusOnline,
		OwnerName: "testuser@example.com",
		Mode:      "personal",
		LastSeen:  time.Now(),
		CreatedAt: time.Now(),
	}

	if err := store.CreateWorker(ctx, worker); err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}

	got, err := store.GetWorker(ctx, worker.ID)
	if err != nil {
		t.Fatalf("GetWorker: %v", err)
	}
	if got.Name != worker.Name {
		t.Errorf("GetWorker Name = %q, want %q", got.Name, worker.Name)
	}
	if len(got.Labels) != 2 {
		t.Errorf("GetWorker Labels len = %d, want 2", len(got.Labels))
	}

	// Update status
	if err := store.UpdateWorkerStatus(ctx, worker.ID, WorkerStatusOffline); err != nil {
		t.Fatalf("UpdateWorkerStatus: %v", err)
	}
	got, _ = store.GetWorker(ctx, worker.ID)
	if got.Status != WorkerStatusOffline {
		t.Errorf("Status after update = %q, want %q", got.Status, WorkerStatusOffline)
	}

	// List workers
	workers, err := store.ListWorkers(ctx)
	if err != nil {
		t.Fatalf("ListWorkers: %v", err)
	}
	if len(workers) < 1 {
		t.Error("ListWorkers should return at least 1 worker")
	}

	// Delete
	if err := store.DeleteWorker(ctx, worker.ID); err != nil {
		t.Fatalf("DeleteWorker: %v", err)
	}
	_, err = store.GetWorker(ctx, worker.ID)
	if err != ErrNotFound {
		t.Errorf("GetWorker after delete: got %v, want ErrNotFound", err)
	}
}

func testPostgresRepos(t *testing.T, store *PostgresStorage) {
	ctx := context.Background()

	repo := &Repo{
		ID:            "r_test1",
		ForgeType:     ForgeTypeGitHub,
		Owner:         "owner1",
		Name:          "repo1",
		CloneURL:      "https://github.com/owner1/repo1.git",
		HTMLURL:       "https://github.com/owner1/repo1",
		WebhookSecret: "supersecret",
		ForgeToken:    "ghp_token123",
		Build:         "make check",
		Release:       "make release",
		Workers:       []string{"linux-amd64", "linux-arm64"},
		Secrets:       map[string]string{"API_KEY": "secret123"},
		Private:       true,
		CreatedAt:     time.Now(),
	}

	if err := store.CreateRepo(ctx, repo); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	// Get by ID
	got, err := store.GetRepo(ctx, repo.ID)
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if got.Owner != repo.Owner {
		t.Errorf("GetRepo Owner = %q, want %q", got.Owner, repo.Owner)
	}
	if got.WebhookSecret != repo.WebhookSecret {
		t.Errorf("GetRepo WebhookSecret = %q, want %q (decryption failed?)", got.WebhookSecret, repo.WebhookSecret)
	}
	if got.ForgeToken != repo.ForgeToken {
		t.Errorf("GetRepo ForgeToken = %q, want %q (decryption failed?)", got.ForgeToken, repo.ForgeToken)
	}
	if len(got.Workers) != 2 {
		t.Errorf("GetRepo Workers len = %d, want 2", len(got.Workers))
	}
	if got.Secrets["API_KEY"] != "secret123" {
		t.Errorf("GetRepo Secrets[API_KEY] = %q, want %q", got.Secrets["API_KEY"], "secret123")
	}
	if !got.Private {
		t.Error("GetRepo Private should be true")
	}

	// Get by clone URL
	got, err = store.GetRepoByCloneURL(ctx, repo.CloneURL)
	if err != nil {
		t.Fatalf("GetRepoByCloneURL: %v", err)
	}
	if got.ID != repo.ID {
		t.Errorf("GetRepoByCloneURL ID = %q, want %q", got.ID, repo.ID)
	}

	// Get by owner/name
	got, err = store.GetRepoByOwnerName(ctx, "github", "owner1", "repo1")
	if err != nil {
		t.Fatalf("GetRepoByOwnerName: %v", err)
	}
	if got.ID != repo.ID {
		t.Errorf("GetRepoByOwnerName ID = %q, want %q", got.ID, repo.ID)
	}

	// Update secrets
	newSecrets := map[string]string{"NEW_KEY": "newvalue"}
	if err := store.UpdateRepoSecrets(ctx, repo.ID, newSecrets); err != nil {
		t.Fatalf("UpdateRepoSecrets: %v", err)
	}
	got, _ = store.GetRepo(ctx, repo.ID)
	if got.Secrets["NEW_KEY"] != "newvalue" {
		t.Errorf("UpdateRepoSecrets: Secrets[NEW_KEY] = %q, want %q", got.Secrets["NEW_KEY"], "newvalue")
	}

	// List repos
	repos, err := store.ListRepos(ctx)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) < 1 {
		t.Error("ListRepos should return at least 1 repo")
	}

	// Delete
	if err := store.DeleteRepo(ctx, repo.ID); err != nil {
		t.Fatalf("DeleteRepo: %v", err)
	}
	_, err = store.GetRepo(ctx, repo.ID)
	if err != ErrNotFound {
		t.Errorf("GetRepo after delete: got %v, want ErrNotFound", err)
	}
}

func testPostgresTokens(t *testing.T, store *PostgresStorage) {
	ctx := context.Background()

	token := &Token{
		ID:        "t_test1",
		Name:      "test-token",
		Hash:      "hash123",
		CreatedAt: time.Now(),
	}

	if err := store.CreateToken(ctx, token); err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	got, err := store.GetTokenByHash(ctx, token.Hash)
	if err != nil {
		t.Fatalf("GetTokenByHash: %v", err)
	}
	if got.ID != token.ID {
		t.Errorf("GetTokenByHash ID = %q, want %q", got.ID, token.ID)
	}

	// List tokens
	tokens, err := store.ListTokens(ctx)
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	if len(tokens) < 1 {
		t.Error("ListTokens should return at least 1 token")
	}

	// Revoke
	if err := store.RevokeToken(ctx, token.ID); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	_, err = store.GetTokenByHash(ctx, token.Hash)
	if err != ErrNotFound {
		t.Errorf("GetTokenByHash after revoke: got %v, want ErrNotFound", err)
	}

	// Cleanup
	_, _ = store.db.Exec("DELETE FROM tokens WHERE id = $1", token.ID)
}

func testPostgresUsers(t *testing.T, store *PostgresStorage) {
	ctx := context.Background()

	// GetOrCreateUser
	user, err := store.GetOrCreateUser(ctx, "testuser")
	if err != nil {
		t.Fatalf("GetOrCreateUser: %v", err)
	}
	if user.Name != "testuser" {
		t.Errorf("GetOrCreateUser Name = %q, want %q", user.Name, "testuser")
	}

	// Get same user again
	user2, err := store.GetOrCreateUser(ctx, "testuser")
	if err != nil {
		t.Fatalf("GetOrCreateUser (second): %v", err)
	}
	if user2.ID != user.ID {
		t.Errorf("GetOrCreateUser should return same user, got %q want %q", user2.ID, user.ID)
	}

	// Update email
	if err := store.UpdateUserEmail(ctx, user.ID, "test@example.com"); err != nil {
		t.Fatalf("UpdateUserEmail: %v", err)
	}
	got, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if got.ID != user.ID {
		t.Errorf("GetUserByEmail returned wrong user: got %q, want %q", got.ID, user.ID)
	}

	// Add email
	if err := store.AddUserEmail(ctx, user.ID, "test2@example.com"); err != nil {
		t.Fatalf("AddUserEmail: %v", err)
	}
	got, _ = store.GetUserByName(ctx, "testuser")
	if len(got.Emails) != 1 || got.Emails[0] != "test2@example.com" {
		t.Errorf("AddUserEmail: Emails = %v, want [test2@example.com]", got.Emails)
	}

	// Update GitLab credentials
	if err := store.UpdateUserGitLabCredentials(ctx, user.ID, `{"token":"gitlab123"}`); err != nil {
		t.Fatalf("UpdateUserGitLabCredentials: %v", err)
	}
	got, _ = store.GetUserByName(ctx, "testuser")
	if got.GitLabCredentials != `{"token":"gitlab123"}` {
		t.Errorf("GitLabCredentials = %q, want encrypted/decrypted value", got.GitLabCredentials)
	}

	// Delete
	if err := store.DeleteUser(ctx, user.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	_, err = store.GetUserByName(ctx, "testuser")
	if err != ErrNotFound {
		t.Errorf("GetUserByName after delete: got %v, want ErrNotFound", err)
	}
}

func testPostgresLogs(t *testing.T, store *PostgresStorage) {
	ctx := context.Background()

	// Create repo and job first
	repo := &Repo{
		ID:            "r_test_logs",
		ForgeType:     ForgeTypeGitHub,
		Owner:         "logowner",
		Name:          "logrepo",
		CloneURL:      "https://github.com/logowner/logrepo.git",
		WebhookSecret: "secret",
		CreatedAt:     time.Now(),
	}
	if err := store.CreateRepo(ctx, repo); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	job := &Job{
		ID:         "j_test_logs",
		RepoID:     repo.ID,
		Commit:     "def456",
		Branch:     "main",
		Status:     JobStatusRunning,
		Author:     "loguser",
		TrustLevel: TrustOwner,
		CreatedAt:  time.Now(),
	}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	// Append logs
	if err := store.AppendLog(ctx, job.ID, "stdout", "line 1\n"); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}
	if err := store.AppendLog(ctx, job.ID, "stderr", "error 1\n"); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}
	if err := store.AppendLog(ctx, job.ID, "stdout", "line 2\n"); err != nil {
		t.Fatalf("AppendLog: %v", err)
	}

	// Get logs
	logs, err := store.GetLogs(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetLogs: %v", err)
	}
	if len(logs) != 3 {
		t.Errorf("GetLogs len = %d, want 3", len(logs))
	}
	if logs[0].Stream != "stdout" || logs[0].Data != "line 1\n" {
		t.Errorf("First log: stream=%q data=%q", logs[0].Stream, logs[0].Data)
	}

	// Cleanup
	_, _ = store.db.Exec("DELETE FROM job_logs WHERE job_id = $1", job.ID)
	_, _ = store.db.Exec("DELETE FROM jobs WHERE id = $1", job.ID)
	_, _ = store.db.Exec("DELETE FROM repos WHERE id = $1", repo.ID)
}

// TestPostgresStorageNoEncryption tests that storage works without encryption configured.
func TestPostgresStorageNoEncryption(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping Postgres tests")
	}

	// Create storage without encryption
	store, err := NewPostgres(dsn, "", "")
	if err != nil {
		t.Fatalf("failed to create postgres storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create a repo with secrets (should store plaintext when no encryption)
	repo := &Repo{
		ID:            "r_no_enc",
		ForgeType:     ForgeTypeGitHub,
		Owner:         "noenc",
		Name:          "noenc",
		CloneURL:      "https://github.com/noenc/noenc.git",
		WebhookSecret: "plaintext_secret",
		ForgeToken:    "plaintext_token",
		Secrets:       map[string]string{"KEY": "VALUE"},
		CreatedAt:     time.Now(),
	}
	if err := store.CreateRepo(ctx, repo); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	got, err := store.GetRepo(ctx, repo.ID)
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if got.WebhookSecret != "plaintext_secret" {
		t.Errorf("WebhookSecret = %q, want %q", got.WebhookSecret, "plaintext_secret")
	}
	if got.Secrets["KEY"] != "VALUE" {
		t.Errorf("Secrets[KEY] = %q, want %q", got.Secrets["KEY"], "VALUE")
	}

	// Cleanup
	_, _ = store.db.Exec("DELETE FROM repos WHERE id = $1", repo.ID)
}
