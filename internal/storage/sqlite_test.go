package storage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ehrlich-b/cinch/internal/crypto"
)

func newTestStorage(t *testing.T) *SQLiteStorage {
	t.Helper()
	s, err := NewSQLite(":memory:", "", "")
	if err != nil {
		t.Fatalf("NewSQLite failed: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestWorkerCRUD(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	worker := &Worker{
		ID:        "w_test1",
		Name:      "test-worker",
		Labels:    []string{"linux", "amd64"},
		Status:    WorkerStatusOnline,
		LastSeen:  time.Now(),
		CreatedAt: time.Now(),
	}

	// Create
	if err := s.CreateWorker(ctx, worker); err != nil {
		t.Fatalf("CreateWorker failed: %v", err)
	}

	// Get
	got, err := s.GetWorker(ctx, worker.ID)
	if err != nil {
		t.Fatalf("GetWorker failed: %v", err)
	}
	if got.ID != worker.ID {
		t.Errorf("ID = %q, want %q", got.ID, worker.ID)
	}
	if got.Name != worker.Name {
		t.Errorf("Name = %q, want %q", got.Name, worker.Name)
	}
	if len(got.Labels) != 2 || got.Labels[0] != "linux" {
		t.Errorf("Labels = %v, want [linux amd64]", got.Labels)
	}

	// List
	workers, err := s.ListWorkers(ctx)
	if err != nil {
		t.Fatalf("ListWorkers failed: %v", err)
	}
	if len(workers) != 1 {
		t.Errorf("len(workers) = %d, want 1", len(workers))
	}

	// Update status
	if err := s.UpdateWorkerStatus(ctx, worker.ID, WorkerStatusOffline); err != nil {
		t.Fatalf("UpdateWorkerStatus failed: %v", err)
	}
	got, _ = s.GetWorker(ctx, worker.ID)
	if got.Status != WorkerStatusOffline {
		t.Errorf("Status = %q, want %q", got.Status, WorkerStatusOffline)
	}

	// Delete
	if err := s.DeleteWorker(ctx, worker.ID); err != nil {
		t.Fatalf("DeleteWorker failed: %v", err)
	}
	_, err = s.GetWorker(ctx, worker.ID)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRepoCRUD(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	repo := &Repo{
		ID:            "r_test1",
		ForgeType:     ForgeTypeGitHub,
		CloneURL:      "https://github.com/user/repo.git",
		WebhookSecret: "secret123",
		CreatedAt:     time.Now(),
	}

	// Create
	if err := s.CreateRepo(ctx, repo); err != nil {
		t.Fatalf("CreateRepo failed: %v", err)
	}

	// Get by ID
	got, err := s.GetRepo(ctx, repo.ID)
	if err != nil {
		t.Fatalf("GetRepo failed: %v", err)
	}
	if got.CloneURL != repo.CloneURL {
		t.Errorf("CloneURL = %q, want %q", got.CloneURL, repo.CloneURL)
	}

	// Get by clone URL
	got, err = s.GetRepoByCloneURL(ctx, repo.CloneURL)
	if err != nil {
		t.Fatalf("GetRepoByCloneURL failed: %v", err)
	}
	if got.ID != repo.ID {
		t.Errorf("ID = %q, want %q", got.ID, repo.ID)
	}

	// List
	repos, err := s.ListRepos(ctx)
	if err != nil {
		t.Fatalf("ListRepos failed: %v", err)
	}
	if len(repos) != 1 {
		t.Errorf("len(repos) = %d, want 1", len(repos))
	}

	// Delete
	if err := s.DeleteRepo(ctx, repo.ID); err != nil {
		t.Fatalf("DeleteRepo failed: %v", err)
	}
	_, err = s.GetRepo(ctx, repo.ID)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestJobCRUD(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Create repo first (foreign key)
	repo := &Repo{
		ID:            "r_test",
		ForgeType:     ForgeTypeGitHub,
		CloneURL:      "https://github.com/test/repo.git",
		WebhookSecret: "secret",
		CreatedAt:     time.Now(),
	}
	if err := s.CreateRepo(ctx, repo); err != nil {
		t.Fatalf("CreateRepo failed: %v", err)
	}

	job := &Job{
		ID:        "j_test1",
		RepoID:    repo.ID,
		Commit:    "abc123",
		Branch:    "main",
		Status:    JobStatusPending,
		CreatedAt: time.Now(),
	}

	// Create
	if err := s.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob failed: %v", err)
	}

	// Get
	got, err := s.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob failed: %v", err)
	}
	if got.Commit != job.Commit {
		t.Errorf("Commit = %q, want %q", got.Commit, job.Commit)
	}
	if got.Status != JobStatusPending {
		t.Errorf("Status = %q, want %q", got.Status, JobStatusPending)
	}

	// Update status to running
	if err := s.UpdateJobStatus(ctx, job.ID, JobStatusRunning, nil); err != nil {
		t.Fatalf("UpdateJobStatus failed: %v", err)
	}
	got, _ = s.GetJob(ctx, job.ID)
	if got.Status != JobStatusRunning {
		t.Errorf("Status = %q, want %q", got.Status, JobStatusRunning)
	}
	if got.StartedAt == nil {
		t.Error("StartedAt should be set")
	}

	// Update status to success with exit code
	exitCode := 0
	if err := s.UpdateJobStatus(ctx, job.ID, JobStatusSuccess, &exitCode); err != nil {
		t.Fatalf("UpdateJobStatus failed: %v", err)
	}
	got, _ = s.GetJob(ctx, job.ID)
	if got.Status != JobStatusSuccess {
		t.Errorf("Status = %q, want %q", got.Status, JobStatusSuccess)
	}
	if got.ExitCode == nil || *got.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", got.ExitCode)
	}
	if got.FinishedAt == nil {
		t.Error("FinishedAt should be set")
	}

	// List with filter
	jobs, err := s.ListJobs(ctx, JobFilter{RepoID: repo.ID})
	if err != nil {
		t.Fatalf("ListJobs failed: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("len(jobs) = %d, want 1", len(jobs))
	}

	// List with status filter
	jobs, err = s.ListJobs(ctx, JobFilter{Status: JobStatusSuccess})
	if err != nil {
		t.Fatalf("ListJobs failed: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("len(jobs) = %d, want 1", len(jobs))
	}
}

func TestTokenCRUD(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	token := &Token{
		ID:        "t_test1",
		Name:      "test-token",
		Hash:      "hash123",
		CreatedAt: time.Now(),
	}

	// Create
	if err := s.CreateToken(ctx, token); err != nil {
		t.Fatalf("CreateToken failed: %v", err)
	}

	// Get by hash
	got, err := s.GetTokenByHash(ctx, token.Hash)
	if err != nil {
		t.Fatalf("GetTokenByHash failed: %v", err)
	}
	if got.ID != token.ID {
		t.Errorf("ID = %q, want %q", got.ID, token.ID)
	}
	if got.Name != token.Name {
		t.Errorf("Name = %q, want %q", got.Name, token.Name)
	}

	// List
	tokens, err := s.ListTokens(ctx)
	if err != nil {
		t.Fatalf("ListTokens failed: %v", err)
	}
	if len(tokens) != 1 {
		t.Errorf("len(tokens) = %d, want 1", len(tokens))
	}

	// Revoke
	if err := s.RevokeToken(ctx, token.ID); err != nil {
		t.Fatalf("RevokeToken failed: %v", err)
	}

	// Get by hash should fail (revoked)
	_, err = s.GetTokenByHash(ctx, token.Hash)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound for revoked token, got %v", err)
	}
}

func TestLogAppendGet(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Create repo and job first
	repo := &Repo{
		ID:            "r_test",
		ForgeType:     ForgeTypeGitHub,
		CloneURL:      "https://github.com/test/repo.git",
		WebhookSecret: "secret",
		CreatedAt:     time.Now(),
	}
	if err := s.CreateRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}

	job := &Job{
		ID:        "j_test",
		RepoID:    repo.ID,
		Commit:    "abc",
		Branch:    "main",
		Status:    JobStatusRunning,
		CreatedAt: time.Now(),
	}
	if err := s.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}

	// Append logs
	if err := s.AppendLog(ctx, job.ID, "stdout", "line1\n"); err != nil {
		t.Fatalf("AppendLog failed: %v", err)
	}
	if err := s.AppendLog(ctx, job.ID, "stderr", "error1\n"); err != nil {
		t.Fatalf("AppendLog failed: %v", err)
	}
	if err := s.AppendLog(ctx, job.ID, "stdout", "line2\n"); err != nil {
		t.Fatalf("AppendLog failed: %v", err)
	}

	// Get logs
	logs, err := s.GetLogs(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("len(logs) = %d, want 3", len(logs))
	}

	// Verify order
	if logs[0].Data != "line1\n" {
		t.Errorf("logs[0].Data = %q, want %q", logs[0].Data, "line1\n")
	}
	if logs[1].Stream != "stderr" {
		t.Errorf("logs[1].Stream = %q, want %q", logs[1].Stream, "stderr")
	}
	if logs[2].Data != "line2\n" {
		t.Errorf("logs[2].Data = %q, want %q", logs[2].Data, "line2\n")
	}
}

func TestJobListPagination(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Create repo
	repo := &Repo{
		ID:            "r_test",
		ForgeType:     ForgeTypeGitHub,
		CloneURL:      "https://github.com/test/repo.git",
		WebhookSecret: "secret",
		CreatedAt:     time.Now(),
	}
	if err := s.CreateRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}

	// Create 5 jobs
	for i := 0; i < 5; i++ {
		job := &Job{
			ID:        "j_" + string(rune('a'+i)),
			RepoID:    repo.ID,
			Commit:    "abc",
			Branch:    "main",
			Status:    JobStatusPending,
			CreatedAt: time.Now().Add(time.Duration(i) * time.Second),
		}
		if err := s.CreateJob(ctx, job); err != nil {
			t.Fatal(err)
		}
	}

	// List with limit
	jobs, err := s.ListJobs(ctx, JobFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListJobs failed: %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("len(jobs) = %d, want 2", len(jobs))
	}

	// List with offset
	jobs, err = s.ListJobs(ctx, JobFilter{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListJobs failed: %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("len(jobs) = %d, want 2", len(jobs))
	}
}

func TestWorkerEmptyLabels(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	worker := &Worker{
		ID:        "w_test",
		Name:      "test",
		Labels:    []string{}, // empty labels
		Status:    WorkerStatusOnline,
		LastSeen:  time.Now(),
		CreatedAt: time.Now(),
	}

	if err := s.CreateWorker(ctx, worker); err != nil {
		t.Fatalf("CreateWorker failed: %v", err)
	}

	got, err := s.GetWorker(ctx, worker.ID)
	if err != nil {
		t.Fatalf("GetWorker failed: %v", err)
	}
	if len(got.Labels) != 0 {
		t.Errorf("Labels = %v, want empty", got.Labels)
	}
}

func TestNotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	_, err := s.GetWorker(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Errorf("GetWorker: expected ErrNotFound, got %v", err)
	}

	_, err = s.GetRepo(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Errorf("GetRepo: expected ErrNotFound, got %v", err)
	}

	_, err = s.GetJob(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Errorf("GetJob: expected ErrNotFound, got %v", err)
	}

	_, err = s.GetTokenByHash(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Errorf("GetTokenByHash: expected ErrNotFound, got %v", err)
	}
}

func TestEncryptedSecrets(t *testing.T) {
	// Test with encryption enabled
	s, err := NewSQLite(":memory:", "test-encryption-key", "")
	if err != nil {
		t.Fatalf("NewSQLite failed: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	// Create repo with secrets
	repo := &Repo{
		ID:            "r_enc",
		ForgeType:     ForgeTypeGitHub,
		CloneURL:      "https://github.com/test/encrypted.git",
		WebhookSecret: "my-webhook-secret",
		ForgeToken:    "ghp_my-forge-token",
		CreatedAt:     time.Now(),
	}
	if err := s.CreateRepo(ctx, repo); err != nil {
		t.Fatalf("CreateRepo failed: %v", err)
	}

	// Retrieve and verify secrets are decrypted
	got, err := s.GetRepo(ctx, repo.ID)
	if err != nil {
		t.Fatalf("GetRepo failed: %v", err)
	}
	if got.WebhookSecret != repo.WebhookSecret {
		t.Errorf("WebhookSecret = %q, want %q", got.WebhookSecret, repo.WebhookSecret)
	}
	if got.ForgeToken != repo.ForgeToken {
		t.Errorf("ForgeToken = %q, want %q", got.ForgeToken, repo.ForgeToken)
	}

	// Verify data is encrypted in database by reading raw value
	var rawSecret string
	err = s.db.QueryRow("SELECT webhook_secret FROM repos WHERE id = ?", repo.ID).Scan(&rawSecret)
	if err != nil {
		t.Fatalf("raw query failed: %v", err)
	}
	if rawSecret == repo.WebhookSecret {
		t.Error("secret should be encrypted in database, but found plaintext")
	}
	if len(rawSecret) < 10 || rawSecret[:4] != "enc:" {
		t.Errorf("encrypted value should have enc: prefix, got %q", rawSecret[:min(20, len(rawSecret))])
	}
}

func TestMigrationEncryptsExistingSecrets(t *testing.T) {
	// First, create storage without encryption
	s1, err := NewSQLite(":memory:", "", "")
	if err != nil {
		t.Fatalf("NewSQLite failed: %v", err)
	}
	ctx := context.Background()

	// Insert repo with plaintext secrets
	repo := &Repo{
		ID:            "r_migrate",
		ForgeType:     ForgeTypeGitHub,
		CloneURL:      "https://github.com/test/migrate.git",
		WebhookSecret: "plaintext-secret",
		ForgeToken:    "plaintext-token",
		CreatedAt:     time.Now(),
	}
	if err := s1.CreateRepo(ctx, repo); err != nil {
		t.Fatalf("CreateRepo failed: %v", err)
	}

	// Verify plaintext in DB
	var rawSecret string
	err = s1.db.QueryRow("SELECT webhook_secret FROM repos WHERE id = ?", repo.ID).Scan(&rawSecret)
	if err != nil {
		t.Fatalf("raw query failed: %v", err)
	}
	if rawSecret != repo.WebhookSecret {
		t.Errorf("expected plaintext in DB, got %q", rawSecret)
	}

	// Get the underlying db handle
	db := s1.db

	// Create new storage with encryption, pointing to same db
	// This simulates app restart with encryption enabled
	s2 := &SQLiteStorage{db: db}
	cipher, _ := newTestCipher("migration-key")
	s2.cipher = cipher
	s2.log = s1.log

	// Run migration which should encrypt existing secrets
	if err := s2.migrateEncryptSecrets(); err != nil {
		t.Fatalf("migrateEncryptSecrets failed: %v", err)
	}

	// Verify encryption happened
	err = db.QueryRow("SELECT webhook_secret FROM repos WHERE id = ?", repo.ID).Scan(&rawSecret)
	if err != nil {
		t.Fatalf("raw query failed: %v", err)
	}
	if rawSecret == repo.WebhookSecret {
		t.Error("secret should be encrypted after migration")
	}
	if len(rawSecret) < 4 || rawSecret[:4] != "enc:" {
		t.Errorf("encrypted value should have enc: prefix, got %q", rawSecret[:min(20, len(rawSecret))])
	}

	// Verify we can still read the decrypted value
	got, err := s2.GetRepo(ctx, repo.ID)
	if err != nil {
		t.Fatalf("GetRepo failed: %v", err)
	}
	if got.WebhookSecret != repo.WebhookSecret {
		t.Errorf("decrypted WebhookSecret = %q, want %q", got.WebhookSecret, repo.WebhookSecret)
	}

	s1.Close()
}

func newTestCipher(secret string) (*crypto.Cipher, error) {
	return crypto.NewCipher(secret)
}

func TestCanaryValidation(t *testing.T) {
	// Create storage with encryption - this creates the canary
	s1, err := NewSQLite(":memory:", "correct-key", "")
	if err != nil {
		t.Fatalf("NewSQLite failed: %v", err)
	}

	// Verify canary was created
	var encryptedValue string
	err = s1.db.QueryRow(`SELECT encrypted_value FROM key_canary WHERE id = 1`).Scan(&encryptedValue)
	if err != nil {
		t.Fatalf("canary should exist: %v", err)
	}
	if !crypto.IsEncrypted(encryptedValue) {
		t.Error("canary should be encrypted")
	}

	// Get the underlying db handle
	db := s1.db

	// Now try to create new storage with WRONG key, pointing to same db
	// This simulates app restart with wrong key
	wrongCipher, _ := crypto.NewCipher("wrong-key")
	s2 := &SQLiteStorage{db: db, cipher: wrongCipher}

	// Validate canary - should fail
	err = s2.validateOrRotateKeys()
	if err == nil {
		t.Error("validateOrRotateKeys should fail with wrong key")
	}
	if err != nil && !strings.Contains(err.Error(), "cannot decrypt canary") && !strings.Contains(err.Error(), "wrong CINCH_SECRET_KEY") {
		t.Errorf("error should mention wrong key, got: %v", err)
	}

	// Validate with correct key - should succeed
	correctCipher, _ := crypto.NewCipher("correct-key")
	s3 := &SQLiteStorage{db: db, cipher: correctCipher}
	err = s3.validateOrRotateKeys()
	if err != nil {
		t.Errorf("validateOrRotateKeys should succeed with correct key: %v", err)
	}

	s1.Close()
}
