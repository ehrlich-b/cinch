package logstore

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestStore creates a FilesystemLogStore rooted at a fresh t.TempDir()
// and ensures its handles are closed at test teardown.
func newTestStore(t *testing.T) *FilesystemLogStore {
	t.Helper()
	store, err := NewFilesystemLogStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewFilesystemLogStore failed: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func numOpenHandles(t *testing.T, store *FilesystemLogStore) int {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	return len(store.files)
}

func assertLogFilesAbsent(t *testing.T, logDir, jobID string) {
	t.Helper()
	for _, ext := range []string{".log", ".log.gz"} {
		if _, err := os.Stat(filepath.Join(logDir, jobID+ext)); !os.IsNotExist(err) {
			t.Errorf("%s%s still exists, want it removed", jobID, ext)
		}
	}
}

func decodeLogLines(t *testing.T, raw []byte) []LogEntry {
	t.Helper()
	trimmed := strings.TrimSuffix(string(raw), "\n")
	if trimmed == "" {
		return nil
	}
	var entries []LogEntry
	for _, line := range strings.Split(trimmed, "\n") {
		var entry LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("line %q is not valid NDJSON: %v", line, err)
		}
		entries = append(entries, entry)
	}
	return entries
}

func TestAppendChunkProducesNDJSON(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	jobID := "job-1"

	chunks := []struct{ stream, data string }{
		{"stdout", "hello"},
		{"stderr", "warning"},
		{"stdout", "done"},
	}
	for _, c := range chunks {
		if err := store.AppendChunk(ctx, jobID, c.stream, []byte(c.data)); err != nil {
			t.Fatalf("AppendChunk(%q) failed: %v", c.data, err)
		}
	}

	// All three appends reuse the same cached file handle.
	if n := numOpenHandles(t, store); n != 1 {
		t.Fatalf("expected 1 cached open handle, got %d", n)
	}

	raw, err := os.ReadFile(filepath.Join(store.logDir, jobID+".log"))
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	entries := decodeLogLines(t, raw)
	if len(entries) != 3 {
		t.Fatalf("got %d NDJSON lines, want 3 (raw: %q)", len(entries), raw)
	}

	for i, want := range chunks {
		entry := entries[i]
		if entry.Stream != want.stream {
			t.Errorf("line %d stream = %q, want %q", i, entry.Stream, want.stream)
		}
		if entry.Data != want.data {
			t.Errorf("line %d data = %q, want %q", i, entry.Data, want.data)
		}
		if entry.Time.IsZero() {
			t.Errorf("line %d has zero timestamp", i)
		}
	}
}

func TestAppendChunkSeparateJobs(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	interleaved := []struct{ jobID, stream, data string }{
		{"job-A", "stdout", "A1"},
		{"job-B", "stdout", "B1"},
		{"job-A", "stderr", "A2"},
		{"job-B", "stdout", "B2"},
	}
	for _, c := range interleaved {
		if err := store.AppendChunk(ctx, c.jobID, c.stream, []byte(c.data)); err != nil {
			t.Fatalf("AppendChunk(%s/%s) failed: %v", c.jobID, c.data, err)
		}
	}

	// Two independent files, each with only its own content in call order.
	for _, tc := range []struct{ jobID, wantData string }{
		{"job-A", "A1A2"},
		{"job-B", "B1B2"},
	} {
		raw, err := os.ReadFile(filepath.Join(store.logDir, tc.jobID+".log"))
		if err != nil {
			t.Fatalf("ReadFile(%s) failed: %v", tc.jobID, err)
		}
		var got strings.Builder
		for _, entry := range decodeLogLines(t, raw) {
			got.WriteString(entry.Data)
		}
		if got.String() != tc.wantData {
			t.Errorf("%s concatenated data = %q, want %q", tc.jobID, got.String(), tc.wantData)
		}
		if strings.Contains(string(raw), "B") && tc.jobID == "job-A" {
			t.Errorf("%s file contains data from job-B: %q", tc.jobID, raw)
		}
		if strings.Contains(string(raw), "A") && tc.jobID == "job-B" {
			t.Errorf("%s file contains data from job-A: %q", tc.jobID, raw)
		}
	}
}

func TestFinalizeCompressesAndRemovesLog(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	jobID := "finalize-job"

	orig := []byte("line one\nline two\n")
	if err := store.AppendChunk(ctx, jobID, "stdout", orig); err != nil {
		t.Fatalf("AppendChunk failed: %v", err)
	}

	expectedNDJSON, err := os.ReadFile(filepath.Join(store.logDir, jobID+".log"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	size, err := store.Finalize(ctx, jobID)
	if err != nil {
		t.Fatalf("Finalize failed: %v", err)
	}
	if size <= 0 {
		t.Errorf("finalize returned size %d, want > 0", size)
	}

	// .log gone, .log.gz present, returned size matches on-disk size.
	if _, err := os.Stat(filepath.Join(store.logDir, jobID+".log")); !os.IsNotExist(err) {
		t.Errorf(".log file still exists after Finalize")
	}
	gzInfo, err := os.Stat(filepath.Join(store.logDir, jobID+".log.gz"))
	if err != nil {
		t.Fatalf(".log.gz missing after Finalize: %v", err)
	}
	if gzInfo.Size() != size {
		t.Errorf("returned size %d != on-disk compressed size %d", size, gzInfo.Size())
	}

	// GetLogs transparently gunzips back to the original NDJSON.
	rc, err := store.GetLogs(ctx, jobID)
	if err != nil {
		t.Fatalf("GetLogs failed: %v", err)
	}
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Errorf("Close on gzip-backed reader failed: %v", err)
	}
	if !bytes.Equal(got, expectedNDJSON) {
		t.Errorf("GetLogs content mismatch:\n got %q\nwant %q", got, expectedNDJSON)
	}

	// Manual gunzip of the .log.gz reproduces the exact same NDJSON bytes.
	gzFile, err := os.Open(filepath.Join(store.logDir, jobID+".log.gz"))
	if err != nil {
		t.Fatalf("open .log.gz: %v", err)
	}
	gr, err := gzip.NewReader(gzFile)
	if err != nil {
		gzFile.Close()
		t.Fatalf("gzip.NewReader: %v", err)
	}
	gunzipped, err := io.ReadAll(gr)
	gr.Close()
	gzFile.Close()
	if err != nil {
		t.Fatalf("manual gunzip: %v", err)
	}
	if !bytes.Equal(gunzipped, expectedNDJSON) {
		t.Errorf("manual gunzip mismatch:\n got %q\nwant %q", gunzipped, expectedNDJSON)
	}
}

func TestFinalizeNeverAppended(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	size, err := store.Finalize(ctx, "never-appended")
	if err != nil {
		t.Fatalf("Finalize failed: %v", err)
	}
	if size != 0 {
		t.Errorf("finalize size = %d, want 0", size)
	}

	entries, err := os.ReadDir(store.logDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no files created, got %d: %v", len(entries), entries)
	}
}

func TestFinalizeTwice(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	jobID := "twice"

	if err := store.AppendChunk(ctx, jobID, "stdout", []byte("payload")); err != nil {
		t.Fatalf("AppendChunk failed: %v", err)
	}

	firstSize, err := store.Finalize(ctx, jobID)
	if err != nil {
		t.Fatalf("first Finalize failed: %v", err)
	}
	if firstSize <= 0 {
		t.Fatalf("first Finalize size = %d, want > 0", firstSize)
	}

	gzPath := filepath.Join(store.logDir, jobID+".log.gz")
	before, err := os.ReadFile(gzPath)
	if err != nil {
		t.Fatalf("read .log.gz: %v", err)
	}

	// Second call finds no .log (first consumed it) and must not touch the .log.gz.
	secondSize, err := store.Finalize(ctx, jobID)
	if err != nil {
		t.Fatalf("second Finalize failed: %v", err)
	}
	if secondSize != 0 {
		t.Errorf("second Finalize size = %d, want 0", secondSize)
	}

	after, err := os.ReadFile(gzPath)
	if err != nil {
		t.Fatalf("read .log.gz after second Finalize: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf(".log.gz was modified by second Finalize")
	}
	if !bytes.Equal(after, before) {
		t.Errorf(".log.gz content changed: %q -> %q", before, after)
	}
	if _, err := os.Stat(filepath.Join(store.logDir, jobID+".log")); !os.IsNotExist(err) {
		t.Errorf(".log file should not exist after second Finalize")
	}
}

func TestGetLogsFallback(t *testing.T) {
	ctx := context.Background()

	t.Run("raw log only", func(t *testing.T) {
		store := newTestStore(t)
		jobID := "in-progress"
		if err := store.AppendChunk(ctx, jobID, "stdout", []byte("raw-line")); err != nil {
			t.Fatalf("AppendChunk failed: %v", err)
		}
		expected, err := os.ReadFile(filepath.Join(store.logDir, jobID+".log"))
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}

		rc, err := store.GetLogs(ctx, jobID)
		if err != nil {
			t.Fatalf("GetLogs failed: %v", err)
		}
		got, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("ReadAll failed: %v", err)
		}
		if err := rc.Close(); err != nil {
			t.Errorf("Close on raw reader failed: %v", err)
		}
		if !bytes.Equal(got, expected) {
			t.Errorf("raw fallback mismatch:\n got %q\nwant %q", got, expected)
		}
	})

	t.Run("gzip only", func(t *testing.T) {
		store := newTestStore(t)
		jobID := "finalized"
		orig := []byte("compressed line")
		if err := store.AppendChunk(ctx, jobID, "stdout", orig); err != nil {
			t.Fatalf("AppendChunk failed: %v", err)
		}
		expectedNDJSON, err := os.ReadFile(filepath.Join(store.logDir, jobID+".log"))
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if _, err := store.Finalize(ctx, jobID); err != nil {
			t.Fatalf("Finalize failed: %v", err)
		}

		rc, err := store.GetLogs(ctx, jobID)
		if err != nil {
			t.Fatalf("GetLogs failed: %v", err)
		}
		got, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("ReadAll failed: %v", err)
		}
		if err := rc.Close(); err != nil {
			t.Fatalf("Close on gzip-backed reader failed: %v", err)
		}
		if !bytes.Equal(got, expectedNDJSON) {
			t.Errorf("gzip fallback mismatch:\n got %q\nwant %q", got, expectedNDJSON)
		}

		// Close released the underlying file, so Delete succeeds cleanly.
		if err := store.Delete(ctx, jobID); err != nil {
			t.Fatalf("Delete after reader Close failed: %v", err)
		}
		assertLogFilesAbsent(t, store.logDir, jobID)
	})

	t.Run("neither present", func(t *testing.T) {
		store := newTestStore(t)
		rc, err := store.GetLogs(ctx, "unknown-job")
		if err != nil {
			t.Fatalf("GetLogs failed: %v", err)
		}
		got, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("ReadAll failed: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected empty read, got %q", got)
		}
		if err := rc.Close(); err != nil {
			t.Errorf("Close on empty reader failed: %v", err)
		}
	})
}

func TestDelete(t *testing.T) {
	ctx := context.Background()

	t.Run("both files", func(t *testing.T) {
		store := newTestStore(t)
		jobID := "both"
		if err := store.AppendChunk(ctx, jobID, "stdout", []byte("x")); err != nil {
			t.Fatalf("AppendChunk failed: %v", err)
		}
		gzPath := filepath.Join(store.logDir, jobID+".log.gz")
		if err := os.WriteFile(gzPath, []byte("dummy-gz"), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		if err := store.Delete(ctx, jobID); err != nil {
			t.Fatalf("Delete failed: %v", err)
		}
		assertLogFilesAbsent(t, store.logDir, jobID)
	})

	t.Run("neither file", func(t *testing.T) {
		store := newTestStore(t)
		if err := store.Delete(ctx, "nothing-here"); err != nil {
			t.Fatalf("Delete on missing job failed: %v", err)
		}
	})

	t.Run("only gzip", func(t *testing.T) {
		store := newTestStore(t)
		jobID := "finalized"
		if err := store.AppendChunk(ctx, jobID, "stdout", []byte("x")); err != nil {
			t.Fatalf("AppendChunk failed: %v", err)
		}
		if _, err := store.Finalize(ctx, jobID); err != nil {
			t.Fatalf("Finalize failed: %v", err)
		}

		if err := store.Delete(ctx, jobID); err != nil {
			t.Fatalf("Delete failed: %v", err)
		}
		assertLogFilesAbsent(t, store.logDir, jobID)
	})

	t.Run("closes cached handle", func(t *testing.T) {
		store := newTestStore(t)
		jobID := "open-handle"
		if err := store.AppendChunk(ctx, jobID, "stdout", []byte("first")); err != nil {
			t.Fatalf("AppendChunk failed: %v", err)
		}
		if err := store.Delete(ctx, jobID); err != nil {
			t.Fatalf("Delete failed: %v", err)
		}
		if n := numOpenHandles(t, store); n != 0 {
			t.Fatalf("cached handle not dropped after Delete: %d open", n)
		}

		// A subsequent AppendChunk must create a fresh file, not reuse a stale
		// handle pointing at the removed inode.
		if err := store.AppendChunk(ctx, jobID, "stdout", []byte("second")); err != nil {
			t.Fatalf("AppendChunk after Delete failed: %v", err)
		}
		if n := numOpenHandles(t, store); n != 1 {
			t.Fatalf("expected 1 fresh handle, got %d", n)
		}
		raw, err := os.ReadFile(filepath.Join(store.logDir, jobID+".log"))
		if err != nil {
			t.Fatalf("fresh .log missing: %v", err)
		}
		if !bytes.Contains(raw, []byte("second")) {
			t.Errorf("re-appended content missing from fresh file: %q", raw)
		}
		if bytes.Contains(raw, []byte("first")) {
			t.Errorf("stale content leaked into fresh file: %q", raw)
		}
	})
}

func TestClose(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	for _, jobID := range []string{"job-1", "job-2"} {
		if err := store.AppendChunk(ctx, jobID, "stdout", []byte("open")); err != nil {
			t.Fatalf("AppendChunk(%s) failed: %v", jobID, err)
		}
	}
	if n := numOpenHandles(t, store); n != 2 {
		t.Fatalf("expected 2 open handles, got %d", n)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if n := numOpenHandles(t, store); n != 0 {
		t.Errorf("map not cleared after Close: %d handles remain", n)
	}

	// A later AppendChunk creates a brand-new handle without error, proving the
	// bookkeeping was cleared, not just the files closed.
	if err := store.AppendChunk(ctx, "job-1", "stdout", []byte("after-close")); err != nil {
		t.Fatalf("AppendChunk after Close failed: %v", err)
	}
	if n := numOpenHandles(t, store); n != 1 {
		t.Errorf("expected 1 handle after re-append, got %d", n)
	}
	raw, err := os.ReadFile(filepath.Join(store.logDir, "job-1.log"))
	if err != nil {
		t.Fatalf("fresh .log missing after Close: %v", err)
	}
	if !bytes.Contains(raw, []byte("after-close")) {
		t.Errorf("re-appended content not written: %q", raw)
	}

	// Closing with zero open handles is a no-op.
	if err := store.Close(); err != nil {
		t.Errorf("Close with no open handles failed: %v", err)
	}
}
