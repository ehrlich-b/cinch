package logstore_test

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ehrlich-b/cinch/internal/logstore"
	"github.com/ehrlich-b/cinch/internal/storage"
)

func TestSQLiteLogStore(t *testing.T) {
	store, err := storage.NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("NewSQLite failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create repo and job for foreign key constraint
	repo := &storage.Repo{
		ID:            "r_1",
		ForgeType:     storage.ForgeTypeGitHub,
		Owner:         "test",
		Name:          "test",
		CloneURL:      "https://github.com/test/test.git",
		WebhookSecret: "secret",
		Build:         "make",
		CreatedAt:     time.Now(),
	}
	if err := store.CreateRepo(ctx, repo); err != nil {
		t.Fatalf("CreateRepo failed: %v", err)
	}

	jobID := "test-job-1"
	job := &storage.Job{
		ID:        jobID,
		RepoID:    repo.ID,
		Commit:    "abc123",
		Branch:    "main",
		Status:    storage.JobStatusRunning,
		CreatedAt: time.Now(),
	}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob failed: %v", err)
	}

	ls := logstore.NewSQLiteLogStore(store)

	// Append some log chunks
	if err := ls.AppendChunk(ctx, jobID, "stdout", []byte("Hello ")); err != nil {
		t.Fatalf("AppendChunk failed: %v", err)
	}
	if err := ls.AppendChunk(ctx, jobID, "stdout", []byte("World\n")); err != nil {
		t.Fatalf("AppendChunk failed: %v", err)
	}
	if err := ls.AppendChunk(ctx, jobID, "stderr", []byte("warning\n")); err != nil {
		t.Fatalf("AppendChunk failed: %v", err)
	}

	// Finalize (no-op for SQLite)
	if err := ls.Finalize(ctx, jobID); err != nil {
		t.Fatalf("Finalize failed: %v", err)
	}

	// Get logs
	reader, err := ls.GetLogs(ctx, jobID)
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	// Parse NDJSON
	var entries []logstore.LogEntry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var entry logstore.LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		entries = append(entries, entry)
	}

	if len(entries) != 3 {
		t.Errorf("got %d entries, want 3", len(entries))
	}

	// Check content
	if entries[0].Data != "Hello " {
		t.Errorf("got %q, want %q", entries[0].Data, "Hello ")
	}
	if entries[0].Stream != "stdout" {
		t.Errorf("got %q, want %q", entries[0].Stream, "stdout")
	}
	if entries[2].Stream != "stderr" {
		t.Errorf("got %q, want %q", entries[2].Stream, "stderr")
	}
}

func TestLogEntry_JSON(t *testing.T) {
	entry := logstore.LogEntry{
		Time:   time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		Stream: "stdout",
		Data:   "test output\n",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Should use short field names
	if !strings.Contains(string(data), `"t":`) {
		t.Errorf("expected short field name 't', got: %s", data)
	}
	if !strings.Contains(string(data), `"s":`) {
		t.Errorf("expected short field name 's', got: %s", data)
	}
	if !strings.Contains(string(data), `"d":`) {
		t.Errorf("expected short field name 'd', got: %s", data)
	}

	// Unmarshal back
	var decoded logstore.LogEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if decoded.Stream != "stdout" {
		t.Errorf("got %q, want %q", decoded.Stream, "stdout")
	}
	if decoded.Data != "test output\n" {
		t.Errorf("got %q, want %q", decoded.Data, "test output\n")
	}
}
