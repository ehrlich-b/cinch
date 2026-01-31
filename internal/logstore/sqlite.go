package logstore

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/ehrlich-b/cinch/internal/storage"
)

// SQLiteLogStore wraps the existing SQLite storage for logs.
// This provides backward compatibility for development and gradual migration.
type SQLiteLogStore struct {
	storage storage.Storage
}

// NewSQLiteLogStore creates a log store backed by SQLite.
func NewSQLiteLogStore(store storage.Storage) *SQLiteLogStore {
	return &SQLiteLogStore{storage: store}
}

// AppendChunk writes log data directly to SQLite (no buffering needed).
func (s *SQLiteLogStore) AppendChunk(ctx context.Context, jobID, stream string, data []byte) error {
	return s.storage.AppendLog(ctx, jobID, stream, string(data))
}

// Finalize is a no-op for SQLite (logs are written immediately).
// Returns 0 for size since SQLite logs aren't compressed.
func (s *SQLiteLogStore) Finalize(ctx context.Context, jobID string) (int64, error) {
	// For SQLite, we could calculate size from the logs, but it's not compressed
	// so we return 0 (SQLite is only for dev/self-hosted where quota doesn't apply)
	return 0, nil
}

// GetLogs returns logs as newline-delimited JSON.
func (s *SQLiteLogStore) GetLogs(ctx context.Context, jobID string) (io.ReadCloser, error) {
	logs, err := s.storage.GetLogs(ctx, jobID)
	if err != nil {
		return nil, err
	}

	// Convert to NDJSON format
	var buf bytes.Buffer
	for _, l := range logs {
		entry := LogEntry{
			Time:   l.CreatedAt,
			Stream: l.Stream,
			Data:   l.Data,
		}
		data, _ := json.Marshal(entry)
		buf.Write(data)
		buf.WriteByte('\n')
	}

	return io.NopCloser(&buf), nil
}

// Delete is a no-op for SQLite (logs are deleted with job via FK cascade).
func (s *SQLiteLogStore) Delete(ctx context.Context, jobID string) error {
	return nil
}

// Close is a no-op for SQLite.
func (s *SQLiteLogStore) Close() error {
	return nil
}
