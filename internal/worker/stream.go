package worker

import (
	"bytes"
	"io"
	"sync"
	"time"

	"github.com/ehrlich-b/cinch/internal/protocol"
)

const (
	// Maximum chunk size for log messages
	maxChunkSize = 64 * 1024 // 64KB

	// Flush interval for buffered output
	flushInterval = 100 * time.Millisecond

	// Minimum bytes before flushing (to avoid tiny messages)
	minFlushSize = 256
)

// LogCallback is called when log data is ready to send.
type LogCallback func(jobID, stream, data string)

// LogStreamer buffers and streams log output.
type LogStreamer struct {
	jobID    string
	callback LogCallback

	mu     sync.Mutex
	stdout *streamWriter
	stderr *streamWriter
	ticker *time.Ticker
	done   chan struct{}
}

// NewLogStreamer creates a new log streamer.
func NewLogStreamer(jobID string, callback LogCallback) *LogStreamer {
	s := &LogStreamer{
		jobID:    jobID,
		callback: callback,
		done:     make(chan struct{}),
	}

	s.stdout = &streamWriter{
		streamer: s,
		stream:   protocol.StreamStdout,
	}
	s.stderr = &streamWriter{
		streamer: s,
		stream:   protocol.StreamStderr,
	}

	// Start background flusher
	s.ticker = time.NewTicker(flushInterval)
	go s.flushLoop()

	return s
}

// Stdout returns an io.Writer for stdout.
func (s *LogStreamer) Stdout() io.Writer {
	return s.stdout
}

// Stderr returns an io.Writer for stderr.
func (s *LogStreamer) Stderr() io.Writer {
	return s.stderr
}

// Flush sends any buffered data.
func (s *LogStreamer) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stdout.flush()
	s.stderr.flush()
}

// Close stops the streamer and flushes remaining data.
func (s *LogStreamer) Close() {
	close(s.done)
	s.ticker.Stop()
	s.Flush()
}

// flushLoop periodically flushes buffered data.
func (s *LogStreamer) flushLoop() {
	for {
		select {
		case <-s.done:
			return
		case <-s.ticker.C:
			s.mu.Lock()
			s.stdout.maybeFlush()
			s.stderr.maybeFlush()
			s.mu.Unlock()
		}
	}
}

// streamWriter implements io.Writer for a specific stream.
type streamWriter struct {
	streamer *LogStreamer
	stream   string
	buf      bytes.Buffer
}

// Write buffers data and sends when appropriate.
func (w *streamWriter) Write(p []byte) (n int, err error) {
	w.streamer.mu.Lock()
	defer w.streamer.mu.Unlock()

	n, err = w.buf.Write(p)
	if err != nil {
		return n, err
	}

	// Send if buffer is large enough
	for w.buf.Len() >= maxChunkSize {
		data := w.buf.Next(maxChunkSize)
		w.streamer.callback(w.streamer.jobID, w.stream, string(data))
	}

	return n, nil
}

// flush sends all buffered data.
func (w *streamWriter) flush() {
	if w.buf.Len() == 0 {
		return
	}

	// Split into chunks if needed
	for w.buf.Len() > 0 {
		size := w.buf.Len()
		if size > maxChunkSize {
			size = maxChunkSize
		}
		data := w.buf.Next(size)
		w.streamer.callback(w.streamer.jobID, w.stream, string(data))
	}
}

// maybeFlush flushes if buffer has enough data.
func (w *streamWriter) maybeFlush() {
	if w.buf.Len() >= minFlushSize {
		w.flush()
	}
}

// CombinedWriter writes to both stdout and stderr writers.
type CombinedWriter struct {
	stdout io.Writer
	stderr io.Writer
}

// NewCombinedWriter creates a writer that sends everything to stdout.
func NewCombinedWriter(stdout io.Writer) *CombinedWriter {
	return &CombinedWriter{
		stdout: stdout,
		stderr: stdout,
	}
}

func (w *CombinedWriter) Write(p []byte) (n int, err error) {
	return w.stdout.Write(p)
}

// PrefixWriter adds a prefix to each line.
type PrefixWriter struct {
	w       io.Writer
	prefix  string
	atStart bool
}

// NewPrefixWriter creates a writer that prefixes each line.
func NewPrefixWriter(w io.Writer, prefix string) *PrefixWriter {
	return &PrefixWriter{
		w:       w,
		prefix:  prefix,
		atStart: true,
	}
}

func (w *PrefixWriter) Write(p []byte) (n int, err error) {
	total := 0
	for len(p) > 0 {
		if w.atStart {
			if _, err := w.w.Write([]byte(w.prefix)); err != nil {
				return total, err
			}
			w.atStart = false
		}

		// Find newline
		idx := bytes.IndexByte(p, '\n')
		if idx < 0 {
			// No newline, write everything
			n, err := w.w.Write(p)
			return total + n, err
		}

		// Write up to and including newline
		n, err := w.w.Write(p[:idx+1])
		total += n
		if err != nil {
			return total, err
		}

		p = p[idx+1:]
		w.atStart = true
	}
	return total, nil
}
