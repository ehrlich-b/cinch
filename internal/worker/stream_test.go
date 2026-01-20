package worker

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLogStreamerBasic(t *testing.T) {
	var mu sync.Mutex
	var chunks []struct {
		jobID, stream, data string
	}

	callback := func(jobID, stream, data string) {
		mu.Lock()
		chunks = append(chunks, struct{ jobID, stream, data string }{jobID, stream, data})
		mu.Unlock()
	}

	streamer := NewLogStreamer("j_1", callback)
	defer streamer.Close()

	// Write to stdout
	if _, err := streamer.Stdout().Write([]byte("hello stdout\n")); err != nil {
		t.Fatalf("stdout write failed: %v", err)
	}
	if _, err := streamer.Stderr().Write([]byte("hello stderr\n")); err != nil {
		t.Fatalf("stderr write failed: %v", err)
	}

	// Flush to ensure delivery
	streamer.Flush()

	mu.Lock()
	defer mu.Unlock()

	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2", len(chunks))
	}

	if chunks[0].stream != "stdout" {
		t.Errorf("chunks[0].stream = %s, want stdout", chunks[0].stream)
	}
	if chunks[0].data != "hello stdout\n" {
		t.Errorf("chunks[0].data = %q, want %q", chunks[0].data, "hello stdout\n")
	}

	if chunks[1].stream != "stderr" {
		t.Errorf("chunks[1].stream = %s, want stderr", chunks[1].stream)
	}
}

func TestLogStreamerLargeChunk(t *testing.T) {
	var mu sync.Mutex
	var chunks []string

	callback := func(jobID, stream, data string) {
		mu.Lock()
		chunks = append(chunks, data)
		mu.Unlock()
	}

	streamer := NewLogStreamer("j_1", callback)
	defer streamer.Close()

	// Write data larger than maxChunkSize
	largeData := strings.Repeat("x", maxChunkSize+1000)
	if _, err := streamer.Stdout().Write([]byte(largeData)); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Flush remaining
	streamer.Flush()

	mu.Lock()
	defer mu.Unlock()

	// Should be split into multiple chunks
	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks, got %d", len(chunks))
	}

	// First chunk should be maxChunkSize
	if len(chunks[0]) != maxChunkSize {
		t.Errorf("first chunk size = %d, want %d", len(chunks[0]), maxChunkSize)
	}

	// Total data should be preserved
	total := 0
	for _, c := range chunks {
		total += len(c)
	}
	if total != len(largeData) {
		t.Errorf("total data = %d, want %d", total, len(largeData))
	}
}

func TestLogStreamerAutoFlush(t *testing.T) {
	var mu sync.Mutex
	var chunks []string

	callback := func(jobID, stream, data string) {
		mu.Lock()
		chunks = append(chunks, data)
		mu.Unlock()
	}

	streamer := NewLogStreamer("j_1", callback)
	defer streamer.Close()

	// Write enough data to trigger auto-flush
	data := strings.Repeat("x", minFlushSize+10)
	if _, err := streamer.Stdout().Write([]byte(data)); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Wait for auto-flush
	time.Sleep(flushInterval * 2)

	mu.Lock()
	defer mu.Unlock()

	if len(chunks) == 0 {
		t.Error("expected auto-flush to send data")
	}
}

func TestPrefixWriter(t *testing.T) {
	var buf strings.Builder
	pw := NewPrefixWriter(&buf, ">>> ")

	if _, err := pw.Write([]byte("line1\nline2\nline3")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if _, err := pw.Write([]byte("\n")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	expected := ">>> line1\n>>> line2\n>>> line3\n"
	if buf.String() != expected {
		t.Errorf("output = %q, want %q", buf.String(), expected)
	}
}

func TestPrefixWriterNoNewline(t *testing.T) {
	var buf strings.Builder
	pw := NewPrefixWriter(&buf, "> ")

	if _, err := pw.Write([]byte("partial")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if _, err := pw.Write([]byte(" more")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if _, err := pw.Write([]byte("\n")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	expected := "> partial more\n"
	if buf.String() != expected {
		t.Errorf("output = %q, want %q", buf.String(), expected)
	}
}

func TestCombinedWriter(t *testing.T) {
	var buf strings.Builder
	cw := NewCombinedWriter(&buf)

	if _, err := cw.Write([]byte("test output\n")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if buf.String() != "test output\n" {
		t.Errorf("output = %q, want %q", buf.String(), "test output\n")
	}
}
