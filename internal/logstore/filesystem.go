package logstore

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FilesystemLogStore stores logs as files on disk.
// Each job gets one file: {logDir}/{jobID}.log in NDJSON format.
type FilesystemLogStore struct {
	logDir string
	log    *slog.Logger

	// File handles for active jobs
	mu    sync.Mutex
	files map[string]*os.File
}

// NewFilesystemLogStore creates a new filesystem-based log store.
func NewFilesystemLogStore(logDir string, log *slog.Logger) (*FilesystemLogStore, error) {
	if log == nil {
		log = slog.Default()
	}

	// Create log directory if it doesn't exist
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}

	return &FilesystemLogStore{
		logDir: logDir,
		log:    log,
		files:  make(map[string]*os.File),
	}, nil
}

// DefaultLogDir returns the default log directory.
func DefaultLogDir() string {
	if dataDir := os.Getenv("CINCH_DATA_DIR"); dataDir != "" {
		return filepath.Join(dataDir, "logs")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "logs"
	}
	return filepath.Join(home, ".cinch", "logs")
}

// AppendChunk appends log data to the job's log file.
func (s *FilesystemLogStore) AppendChunk(ctx context.Context, jobID, stream string, data []byte) error {
	f, err := s.getOrCreateFile(jobID)
	if err != nil {
		return err
	}

	entry := LogEntry{
		Time:   time.Now(),
		Stream: stream,
		Data:   string(data),
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal log entry: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("write log entry: %w", err)
	}
	if _, err := f.Write([]byte("\n")); err != nil {
		return fmt.Errorf("write newline: %w", err)
	}

	return nil
}

// getOrCreateFile returns the file handle for a job, creating it if needed.
func (s *FilesystemLogStore) getOrCreateFile(jobID string) (*os.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if f, ok := s.files[jobID]; ok {
		return f, nil
	}

	path := filepath.Join(s.logDir, jobID+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}

	s.files[jobID] = f
	return f, nil
}

// Finalize closes the file handle and compresses the log file.
// Returns the final compressed size in bytes for storage tracking.
func (s *FilesystemLogStore) Finalize(ctx context.Context, jobID string) (int64, error) {
	s.mu.Lock()
	if f, ok := s.files[jobID]; ok {
		if err := f.Sync(); err != nil {
			s.log.Warn("failed to sync log file", "job_id", jobID, "error", err)
		}
		if err := f.Close(); err != nil {
			s.log.Warn("failed to close log file", "job_id", jobID, "error", err)
		}
		delete(s.files, jobID)
	}
	s.mu.Unlock()

	// Compress the log file (text logs compress ~10:1)
	srcPath := filepath.Join(s.logDir, jobID+".log")
	dstPath := filepath.Join(s.logDir, jobID+".log.gz")

	// Read uncompressed content
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // No logs written
		}
		return 0, fmt.Errorf("read log file: %w", err)
	}

	// Gzip compress
	var compressed bytes.Buffer
	gw := gzip.NewWriter(&compressed)
	if _, err := gw.Write(raw); err != nil {
		return 0, fmt.Errorf("gzip compress: %w", err)
	}
	if err := gw.Close(); err != nil {
		return 0, fmt.Errorf("gzip close: %w", err)
	}

	compressedSize := int64(compressed.Len())

	// Write compressed file
	if err := os.WriteFile(dstPath, compressed.Bytes(), 0644); err != nil {
		return 0, fmt.Errorf("write compressed log: %w", err)
	}

	// Delete uncompressed file
	if err := os.Remove(srcPath); err != nil {
		s.log.Warn("failed to remove uncompressed log", "job_id", jobID, "error", err)
	}

	s.log.Debug("compressed job logs", "job_id", jobID,
		"raw_size", len(raw), "compressed_size", compressedSize)

	return compressedSize, nil
}

// GetLogs returns the log file as a streaming reader.
// Tries compressed (.log.gz) first, then uncompressed (.log) for in-progress jobs.
func (s *FilesystemLogStore) GetLogs(ctx context.Context, jobID string) (io.ReadCloser, error) {
	// Try compressed file first (finalized jobs)
	gzPath := filepath.Join(s.logDir, jobID+".log.gz")
	if f, err := os.Open(gzPath); err == nil {
		gr, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("gzip reader: %w", err)
		}
		return &fsGzipReadCloser{gr: gr, file: f}, nil
	}

	// Try uncompressed file (in-progress jobs)
	path := filepath.Join(s.logDir, jobID+".log")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty reader if no logs yet
			return io.NopCloser(&emptyReader{}), nil
		}
		return nil, fmt.Errorf("open log file: %w", err)
	}
	return f, nil
}

// fsGzipReadCloser wraps a gzip.Reader and its underlying file.
type fsGzipReadCloser struct {
	gr   *gzip.Reader
	file *os.File
}

func (g *fsGzipReadCloser) Read(p []byte) (int, error) {
	return g.gr.Read(p)
}

func (g *fsGzipReadCloser) Close() error {
	g.gr.Close()
	return g.file.Close()
}

// Delete removes the log file for a job.
func (s *FilesystemLogStore) Delete(ctx context.Context, jobID string) error {
	// Close file handle if open
	s.mu.Lock()
	if f, ok := s.files[jobID]; ok {
		f.Close()
		delete(s.files, jobID)
	}
	s.mu.Unlock()

	// Remove both compressed and uncompressed files
	for _, ext := range []string{".log", ".log.gz"} {
		path := filepath.Join(s.logDir, jobID+ext)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove log file: %w", err)
		}
	}
	return nil
}

// Close closes all open file handles.
func (s *FilesystemLogStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for jobID, f := range s.files {
		if err := f.Close(); err != nil {
			s.log.Warn("failed to close log file", "job_id", jobID, "error", err)
		}
	}
	s.files = make(map[string]*os.File)

	return nil
}

// emptyReader implements io.Reader that returns EOF immediately.
type emptyReader struct{}

func (e *emptyReader) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}
