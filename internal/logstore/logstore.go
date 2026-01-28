// Package logstore provides log storage for CI job output.
// It supports both SQLite (for development) and R2 (for production).
package logstore

import (
	"context"
	"io"
	"time"
)

// LogEntry represents a log line with metadata.
type LogEntry struct {
	Time   time.Time `json:"t"`
	Stream string    `json:"s"` // "stdout" or "stderr"
	Data   string    `json:"d"`
}

// LogStore provides log storage and retrieval.
type LogStore interface {
	// AppendChunk buffers log data. Flushes to storage when threshold hit.
	AppendChunk(ctx context.Context, jobID, stream string, data []byte) error

	// Finalize flushes remaining buffer and marks job logs as complete.
	// For R2: concatenates chunks into final.log
	Finalize(ctx context.Context, jobID string) error

	// GetLogs returns logs as a streaming reader.
	// Returns newline-delimited JSON log entries.
	GetLogs(ctx context.Context, jobID string) (io.ReadCloser, error)

	// Delete removes all logs for a job (for retention cleanup).
	Delete(ctx context.Context, jobID string) error

	// Close shuts down the log store (stops flush loop, etc).
	Close() error
}
