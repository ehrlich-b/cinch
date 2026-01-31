package logstore

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	flushSize     = 256 * 1024       // 256KB - flush buffer when exceeded
	flushInterval = 30 * time.Second // flush stale buffers every 30s
	flushLoopTick = 5 * time.Second  // check for stale buffers every 5s
)

// R2Config contains configuration for R2 storage.
type R2Config struct {
	AccountID       string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
}

// R2LogStore stores logs in Cloudflare R2.
type R2LogStore struct {
	client  *s3.Client
	bucket  string
	buffers map[string]*jobBuffer
	mu      sync.RWMutex
	log     *slog.Logger

	// Shutdown
	done chan struct{}
	wg   sync.WaitGroup
}

// jobBuffer holds buffered log entries for a job.
type jobBuffer struct {
	entries   []LogEntry
	size      int       // current buffer size in bytes
	lastFlush time.Time // when we last flushed
	chunkIdx  int       // next chunk number for R2
	mu        sync.Mutex
}

// NewR2LogStore creates a new R2-backed log store.
func NewR2LogStore(cfg R2Config, log *slog.Logger) (*R2LogStore, error) {
	if log == nil {
		log = slog.Default()
	}

	// Build endpoint URL for R2
	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.AccountID)

	// Create AWS config with R2 endpoint
	awsCfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID,
			cfg.SecretAccessKey,
			"",
		)),
		config.WithRegion("auto"),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	// Create S3 client with R2 endpoint
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
	})

	store := &R2LogStore{
		client:  client,
		bucket:  cfg.Bucket,
		buffers: make(map[string]*jobBuffer),
		log:     log,
		done:    make(chan struct{}),
	}

	// Start background flush loop
	store.wg.Add(1)
	go store.flushLoop()

	return store, nil
}

// flushLoop periodically flushes stale buffers.
func (s *R2LogStore) flushLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(flushLoopTick)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.flushStale()
		case <-s.done:
			return
		}
	}
}

// flushStale flushes buffers that haven't been flushed recently.
func (s *R2LogStore) flushStale() {
	s.mu.RLock()
	var staleJobs []string
	now := time.Now()
	for jobID, buf := range s.buffers {
		buf.mu.Lock()
		if now.Sub(buf.lastFlush) > flushInterval && len(buf.entries) > 0 {
			staleJobs = append(staleJobs, jobID)
		}
		buf.mu.Unlock()
	}
	s.mu.RUnlock()

	// Flush stale buffers
	for _, jobID := range staleJobs {
		if err := s.flush(context.Background(), jobID); err != nil {
			s.log.Warn("failed to flush stale buffer", "job_id", jobID, "error", err)
		}
	}
}

// AppendChunk adds log data to the buffer, flushing if threshold exceeded.
func (s *R2LogStore) AppendChunk(ctx context.Context, jobID, stream string, data []byte) error {
	entry := LogEntry{
		Time:   time.Now(),
		Stream: stream,
		Data:   string(data),
	}

	s.mu.Lock()
	buf, ok := s.buffers[jobID]
	if !ok {
		buf = &jobBuffer{
			lastFlush: time.Now(),
		}
		s.buffers[jobID] = buf
	}
	s.mu.Unlock()

	buf.mu.Lock()
	buf.entries = append(buf.entries, entry)
	buf.size += len(data) + 50 // approximate JSON overhead
	shouldFlush := buf.size >= flushSize
	buf.mu.Unlock()

	if shouldFlush {
		return s.flush(ctx, jobID)
	}
	return nil
}

// flush writes buffered entries to R2 as a chunk.
func (s *R2LogStore) flush(ctx context.Context, jobID string) error {
	s.mu.RLock()
	buf, ok := s.buffers[jobID]
	s.mu.RUnlock()
	if !ok {
		return nil
	}

	buf.mu.Lock()
	if len(buf.entries) == 0 {
		buf.mu.Unlock()
		return nil
	}

	// Take current entries
	entries := buf.entries
	chunkIdx := buf.chunkIdx
	buf.entries = nil
	buf.size = 0
	buf.chunkIdx++
	buf.lastFlush = time.Now()
	buf.mu.Unlock()

	// Build NDJSON
	var content bytes.Buffer
	for _, e := range entries {
		data, _ := json.Marshal(e)
		content.Write(data)
		content.WriteByte('\n')
	}

	// Upload to R2
	key := fmt.Sprintf("logs/%s/chunk_%03d.log", jobID, chunkIdx)
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(content.Bytes()),
		ContentType: aws.String("application/x-ndjson"),
	})
	if err != nil {
		s.log.Error("failed to upload chunk", "job_id", jobID, "chunk", chunkIdx, "error", err)
		return fmt.Errorf("upload chunk: %w", err)
	}

	s.log.Debug("flushed log chunk", "job_id", jobID, "chunk", chunkIdx, "size", content.Len())
	return nil
}

// Finalize flushes remaining buffer, concatenates chunks into final.log, and cleans up.
// Returns the final compressed size in bytes for storage tracking.
func (s *R2LogStore) Finalize(ctx context.Context, jobID string) (int64, error) {
	// Flush any remaining buffer
	if err := s.flush(ctx, jobID); err != nil {
		return 0, err
	}

	// Get chunk count
	s.mu.Lock()
	buf, ok := s.buffers[jobID]
	var chunkCount int
	if ok {
		buf.mu.Lock()
		chunkCount = buf.chunkIdx
		buf.mu.Unlock()
		delete(s.buffers, jobID)
	}
	s.mu.Unlock()

	if chunkCount == 0 {
		// No chunks written, nothing to finalize
		return 0, nil
	}

	// Collect all chunk content
	var rawContent bytes.Buffer
	for i := 0; i < chunkCount; i++ {
		key := fmt.Sprintf("logs/%s/chunk_%03d.log", jobID, i)
		resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(s.bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			s.log.Warn("failed to read chunk during finalize", "job_id", jobID, "chunk", i, "error", err)
			continue
		}
		_, _ = io.Copy(&rawContent, resp.Body)
		resp.Body.Close()
	}

	// Gzip compress before storing (text logs compress ~10:1)
	var compressed bytes.Buffer
	gw := gzip.NewWriter(&compressed)
	if _, err := gw.Write(rawContent.Bytes()); err != nil {
		return 0, fmt.Errorf("gzip compress: %w", err)
	}
	if err := gw.Close(); err != nil {
		return 0, fmt.Errorf("gzip close: %w", err)
	}

	compressedSize := int64(compressed.Len())

	// Upload compressed final.log with Content-Encoding for transparent browser decompression
	finalKey := fmt.Sprintf("logs/%s/final.log", jobID)
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:          aws.String(s.bucket),
		Key:             aws.String(finalKey),
		Body:            bytes.NewReader(compressed.Bytes()),
		ContentType:     aws.String("application/x-ndjson"),
		ContentEncoding: aws.String("gzip"),
	})
	if err != nil {
		return 0, fmt.Errorf("upload final.log: %w", err)
	}

	// Delete chunks
	for i := 0; i < chunkCount; i++ {
		key := fmt.Sprintf("logs/%s/chunk_%03d.log", jobID, i)
		_, _ = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(s.bucket),
			Key:    aws.String(key),
		})
	}

	s.log.Debug("finalized job logs", "job_id", jobID, "chunks", chunkCount,
		"raw_size", rawContent.Len(), "compressed_size", compressedSize)
	return compressedSize, nil
}

// GetLogs returns logs as a streaming reader.
// If job is complete (final.log exists), returns that (decompressed if gzipped).
// Otherwise, concatenates available chunks (for in-progress jobs).
func (s *R2LogStore) GetLogs(ctx context.Context, jobID string) (io.ReadCloser, error) {
	// Try final.log first
	finalKey := fmt.Sprintf("logs/%s/final.log", jobID)
	resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(finalKey),
	})
	if err == nil {
		// Check if content is gzip-compressed (new format)
		// Content-Encoding header is set on compressed files
		if resp.ContentEncoding != nil && *resp.ContentEncoding == "gzip" {
			gr, err := gzip.NewReader(resp.Body)
			if err != nil {
				resp.Body.Close()
				return nil, fmt.Errorf("gzip reader: %w", err)
			}
			return &gzipReadCloser{gr: gr, underlying: resp.Body}, nil
		}
		// Uncompressed (legacy format) - return as-is
		return resp.Body, nil
	}

	// No final.log, read available chunks (job still running or error case)
	prefix := fmt.Sprintf("logs/%s/chunk_", jobID)
	listResp, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("list chunks: %w", err)
	}

	if len(listResp.Contents) == 0 {
		// No chunks, check buffer
		s.mu.RLock()
		buf, ok := s.buffers[jobID]
		s.mu.RUnlock()

		if !ok || len(buf.entries) == 0 {
			// No logs at all
			return io.NopCloser(strings.NewReader("")), nil
		}

		// Return buffered entries
		buf.mu.Lock()
		var content bytes.Buffer
		for _, e := range buf.entries {
			data, _ := json.Marshal(e)
			content.Write(data)
			content.WriteByte('\n')
		}
		buf.mu.Unlock()

		return io.NopCloser(&content), nil
	}

	// Sort chunks by key (chunk_000, chunk_001, ...)
	sort.Slice(listResp.Contents, func(i, j int) bool {
		return *listResp.Contents[i].Key < *listResp.Contents[j].Key
	})

	// Concatenate chunks
	var content bytes.Buffer
	for _, obj := range listResp.Contents {
		resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(s.bucket),
			Key:    obj.Key,
		})
		if err != nil {
			s.log.Warn("failed to read chunk", "key", *obj.Key, "error", err)
			continue
		}
		_, _ = io.Copy(&content, resp.Body)
		resp.Body.Close()
	}

	// Add any buffered entries not yet flushed
	s.mu.RLock()
	buf, ok := s.buffers[jobID]
	s.mu.RUnlock()
	if ok {
		buf.mu.Lock()
		for _, e := range buf.entries {
			data, _ := json.Marshal(e)
			content.Write(data)
			content.WriteByte('\n')
		}
		buf.mu.Unlock()
	}

	return io.NopCloser(&content), nil
}

// Delete removes all logs for a job.
func (s *R2LogStore) Delete(ctx context.Context, jobID string) error {
	// Remove from buffers
	s.mu.Lock()
	delete(s.buffers, jobID)
	s.mu.Unlock()

	// List all objects for this job
	prefix := fmt.Sprintf("logs/%s/", jobID)
	listResp, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return fmt.Errorf("list objects: %w", err)
	}

	// Delete all objects
	for _, obj := range listResp.Contents {
		_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(s.bucket),
			Key:    obj.Key,
		})
		if err != nil {
			s.log.Warn("failed to delete log object", "key", *obj.Key, "error", err)
		}
	}

	return nil
}

// Close shuts down the log store.
func (s *R2LogStore) Close() error {
	close(s.done)
	s.wg.Wait()
	return nil
}

// gzipReadCloser wraps a gzip.Reader and its underlying io.ReadCloser.
type gzipReadCloser struct {
	gr         *gzip.Reader
	underlying io.ReadCloser
}

func (g *gzipReadCloser) Read(p []byte) (int, error) {
	return g.gr.Read(p)
}

func (g *gzipReadCloser) Close() error {
	g.gr.Close()
	return g.underlying.Close()
}
