package server

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ehrlich-b/cinch/internal/logstore"
	"github.com/ehrlich-b/cinch/internal/storage"
	"github.com/gorilla/websocket"
)

// LogStreamHandler handles WebSocket connections for log streaming to UI clients.
type LogStreamHandler struct {
	storage  storage.Storage
	logStore logstore.LogStore
	auth     *AuthHandler
	log      *slog.Logger

	// Subscriptions: jobID -> set of connections
	mu          sync.RWMutex
	subscribers map[string]map[*websocket.Conn]bool
}

// NewLogStreamHandler creates a new log stream handler.
func NewLogStreamHandler(store storage.Storage, auth *AuthHandler, log *slog.Logger) *LogStreamHandler {
	if log == nil {
		log = slog.Default()
	}
	return &LogStreamHandler{
		storage:     store,
		auth:        auth,
		log:         log,
		subscribers: make(map[string]map[*websocket.Conn]bool),
	}
}

// SetLogStore sets the log store for retrieving historical logs.
func (h *LogStreamHandler) SetLogStore(ls logstore.LogStore) {
	h.logStore = ls
}

// ServeHTTP handles log stream WebSocket requests.
// Expected path: /ws/logs/{job_id}
func (h *LogStreamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract job ID from path
	path := strings.TrimPrefix(r.URL.Path, "/ws/logs/")
	jobID := strings.TrimSuffix(path, "/")

	if jobID == "" {
		http.Error(w, "missing job_id", http.StatusBadRequest)
		return
	}

	// Verify job exists
	ctx := r.Context()
	job, err := h.storage.GetJob(ctx, jobID)
	if err != nil {
		if err == storage.ErrNotFound {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		h.log.Error("failed to get job", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Authorization: require auth for private repo logs
	repo, err := h.storage.GetRepo(ctx, job.RepoID)
	if err != nil {
		h.log.Error("failed to get repo for log auth", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if repo.Private {
		var email string
		if h.auth != nil {
			email = h.auth.GetUser(r)
		}
		if email == "" {
			http.Error(w, "authentication required for private repo logs", http.StatusUnauthorized)
			return
		}
		// Check if user owns this repo
		user, err := h.storage.GetUserByEmail(ctx, email)
		if err != nil || user == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if repo.OwnerUserID != user.ID {
			http.Error(w, "forbidden: you don't have access to this repo", http.StatusForbidden)
			return
		}
	}

	// Upgrade to WebSocket (use UI upgrader with origin checks)
	conn, err := uiUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Error("websocket upgrade failed", "error", err)
		return
	}

	h.log.Debug("log stream client connected", "job_id", jobID)

	// Send existing logs first
	if err := h.sendExistingLogs(conn, jobID); err != nil {
		h.log.Warn("failed to send existing logs", "job_id", jobID, "error", err)
		conn.Close()
		return
	}

	// If job is already complete, send completion and close
	if job.Status == storage.JobStatusSuccess ||
		job.Status == storage.JobStatusFailed ||
		job.Status == storage.JobStatusError ||
		job.Status == storage.JobStatusCancelled {
		h.sendJobStatus(conn, job)
		conn.Close()
		return
	}

	// Subscribe for new logs
	h.subscribe(jobID, conn)

	// Read pump (just for close detection)
	go h.readPump(conn, jobID)
}

// sendExistingLogs sends all existing logs for a job.
func (h *LogStreamHandler) sendExistingLogs(conn *websocket.Conn, jobID string) error {
	// Use logStore if available
	if h.logStore != nil {
		reader, err := h.logStore.GetLogs(context.Background(), jobID)
		if err != nil {
			return err
		}
		defer reader.Close()

		// Read NDJSON and send each entry
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			var entry logstore.LogEntry
			if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
				continue
			}
			msg := logMessage{
				Type:   "log",
				Stream: entry.Stream,
				Data:   entry.Data,
				Time:   entry.Time,
			}
			if err := conn.WriteJSON(msg); err != nil {
				return err
			}
		}
		return scanner.Err()
	}

	// Fallback to direct storage access
	logs, err := h.storage.GetLogs(context.Background(), jobID)
	if err != nil {
		return err
	}

	for _, l := range logs {
		msg := logMessage{
			Type:   "log",
			Stream: l.Stream,
			Data:   l.Data,
			Time:   l.CreatedAt,
		}
		if err := conn.WriteJSON(msg); err != nil {
			return err
		}
	}

	return nil
}

// sendJobStatus sends job completion status.
func (h *LogStreamHandler) sendJobStatus(conn *websocket.Conn, job *storage.Job) {
	msg := statusMessage{
		Type:     "status",
		Status:   string(job.Status),
		ExitCode: job.ExitCode,
	}
	if err := conn.WriteJSON(msg); err != nil {
		h.log.Warn("failed to send job status", "job_id", job.ID, "error", err)
	}
}

// subscribe adds a connection to the subscribers for a job.
func (h *LogStreamHandler) subscribe(jobID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.subscribers[jobID] == nil {
		h.subscribers[jobID] = make(map[*websocket.Conn]bool)
	}
	h.subscribers[jobID][conn] = true
}

// unsubscribe removes a connection from the subscribers.
func (h *LogStreamHandler) unsubscribe(jobID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if subs, ok := h.subscribers[jobID]; ok {
		delete(subs, conn)
		if len(subs) == 0 {
			delete(h.subscribers, jobID)
		}
	}
}

// readPump handles reading from the WebSocket (for close detection).
func (h *LogStreamHandler) readPump(conn *websocket.Conn, jobID string) {
	defer func() {
		h.unsubscribe(jobID, conn)
		conn.Close()
		h.log.Debug("log stream client disconnected", "job_id", jobID)
	}()

	conn.SetReadLimit(512)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			return
		}
	}
}

// BroadcastLog sends a log chunk to all subscribers for a job.
func (h *LogStreamHandler) BroadcastLog(jobID, stream, data string) {
	h.mu.RLock()
	subs := h.subscribers[jobID]
	h.mu.RUnlock()

	if len(subs) == 0 {
		return
	}

	msg := logMessage{
		Type:   "log",
		Stream: stream,
		Data:   data,
		Time:   time.Now(),
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		h.log.Error("failed to marshal log message", "error", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for conn := range subs {
		if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
			h.log.Warn("failed to broadcast log", "job_id", jobID, "error", err)
		}
	}
}

// BroadcastJobComplete sends job completion to all subscribers.
func (h *LogStreamHandler) BroadcastJobComplete(jobID string, status string, exitCode *int) {
	// Copy subscribers under read lock, then release before mutating
	h.mu.RLock()
	subs := h.subscribers[jobID]
	if len(subs) == 0 {
		h.mu.RUnlock()
		return
	}
	// Copy the connections to avoid holding lock during I/O
	conns := make([]*websocket.Conn, 0, len(subs))
	for conn := range subs {
		conns = append(conns, conn)
	}
	h.mu.RUnlock()

	msg := statusMessage{
		Type:     "status",
		Status:   status,
		ExitCode: exitCode,
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		h.log.Error("failed to marshal status message", "error", err)
		return
	}

	// Send to all connections (no lock held)
	for _, conn := range conns {
		if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
			h.log.Warn("failed to broadcast status", "job_id", jobID, "error", err)
		}
		// Close connection after sending completion
		conn.Close()
	}

	// Clean up subscribers for this job
	h.mu.Lock()
	delete(h.subscribers, jobID)
	h.mu.Unlock()
}

// Message types for log streaming

type logMessage struct {
	Type   string    `json:"type"`
	Stream string    `json:"stream"`
	Data   string    `json:"data"`
	Time   time.Time `json:"time"`
}

type statusMessage struct {
	Type     string `json:"type"`
	Status   string `json:"status"`
	ExitCode *int   `json:"exit_code,omitempty"`
}
