package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WorkerEvent represents an event broadcast to UI clients.
type WorkerEvent struct {
	Type     string      `json:"type"` // connected, disconnected, job_started, job_finished
	WorkerID string      `json:"worker_id"`
	Worker   *WorkerInfo `json:"worker,omitempty"`
	JobID    string      `json:"job_id,omitempty"`
}

// WorkerInfo contains worker information for UI display.
type WorkerInfo struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Hostname   string   `json:"hostname"`
	Labels     []string `json:"labels"`
	Mode       string   `json:"mode"`
	OwnerName  string   `json:"owner_name,omitempty"`
	Version    string   `json:"version,omitempty"`
	Connected  bool     `json:"connected"`
	ActiveJobs []string `json:"active_jobs"`
	LastSeen   int64    `json:"last_seen"`
}

// workerSubscriber holds a WebSocket connection and the authenticated user.
type workerSubscriber struct {
	conn     *websocket.Conn
	username string // authenticated user's email
}

// WorkerStreamHandler handles WebSocket connections for worker event streaming.
type WorkerStreamHandler struct {
	hub  *Hub
	auth *AuthHandler
	log  *slog.Logger

	mu          sync.RWMutex
	subscribers map[*websocket.Conn]*workerSubscriber
}

// NewWorkerStreamHandler creates a new worker stream handler.
func NewWorkerStreamHandler(hub *Hub, auth *AuthHandler, log *slog.Logger) *WorkerStreamHandler {
	if log == nil {
		log = slog.Default()
	}
	h := &WorkerStreamHandler{
		hub:         hub,
		auth:        auth,
		log:         log,
		subscribers: make(map[*websocket.Conn]*workerSubscriber),
	}

	// Set up hub callbacks
	hub.SetEventCallbacks(
		h.handleWorkerConnected,
		h.handleWorkerDisconnected,
		h.handleWorkerJobStarted,
		h.handleWorkerJobFinished,
	)

	return h
}

// ServeHTTP handles WebSocket upgrade requests for /ws/workers.
func (h *WorkerStreamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Require authentication - anonymous access to worker telemetry leaks operational metadata
	username := ""
	if h.auth != nil {
		username = h.auth.GetUser(r)
	}
	if username == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := uiUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Error("websocket upgrade failed", "error", err)
		return
	}

	h.log.Debug("worker stream client connected", "user", username)

	// Send current worker list as initial state (filtered by visibility)
	if err := h.sendInitialState(conn, username); err != nil {
		h.log.Warn("failed to send initial state", "error", err)
		conn.Close()
		return
	}

	// Subscribe for updates
	h.subscribe(conn, username)

	// Read pump for close detection
	go h.readPump(conn)
}

// sendInitialState sends the current worker list to a new client (filtered by visibility).
func (h *WorkerStreamHandler) sendInitialState(conn *websocket.Conn, username string) error {
	workers := h.hub.List()

	for i := range workers {
		w := &workers[i]
		// Visibility filtering: personal workers only visible to owner
		if !h.canUserSeeWorker(username, w) {
			continue
		}

		event := WorkerEvent{
			Type:     "connected",
			WorkerID: w.ID,
			Worker:   workerConnToInfo(w),
		}
		if err := conn.WriteJSON(event); err != nil {
			return err
		}
	}

	return nil
}

// canUserSeeWorker returns true if the user can see the worker.
func (h *WorkerStreamHandler) canUserSeeWorker(username string, w *WorkerConn) bool {
	mode := string(w.Mode)
	if mode == "" {
		mode = "personal"
	}

	// Personal workers: only visible to owner
	if mode == "personal" && w.OwnerName != username {
		return false
	}

	// Shared workers: visible to all authenticated users
	return true
}

// subscribe adds a connection to subscribers.
func (h *WorkerStreamHandler) subscribe(conn *websocket.Conn, username string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subscribers[conn] = &workerSubscriber{conn: conn, username: username}
}

// unsubscribe removes a connection from subscribers.
func (h *WorkerStreamHandler) unsubscribe(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subscribers, conn)
}

// readPump handles reading from WebSocket for close detection.
func (h *WorkerStreamHandler) readPump(conn *websocket.Conn) {
	defer func() {
		h.unsubscribe(conn)
		conn.Close()
		h.log.Debug("worker stream client disconnected")
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

// broadcastForWorker sends an event to subscribers who can see the specified worker.
func (h *WorkerStreamHandler) broadcastForWorker(event WorkerEvent, worker *WorkerConn) {
	msgBytes, err := json.Marshal(event)
	if err != nil {
		h.log.Error("failed to marshal worker event", "error", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, sub := range h.subscribers {
		// Filter by visibility
		if worker != nil && !h.canUserSeeWorker(sub.username, worker) {
			continue
		}
		if err := sub.conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
			h.log.Warn("failed to broadcast worker event", "error", err)
		}
	}
}

// handleWorkerConnected is called when a worker connects.
func (h *WorkerStreamHandler) handleWorkerConnected(worker *WorkerConn) {
	h.broadcastForWorker(WorkerEvent{
		Type:     "connected",
		WorkerID: worker.ID,
		Worker:   workerConnToInfo(worker),
	}, worker)
}

// handleWorkerDisconnected is called when a worker disconnects.
func (h *WorkerStreamHandler) handleWorkerDisconnected(workerID string) {
	// For disconnect, we need to look up the worker to check visibility
	// If the worker is already gone from hub, broadcast to everyone
	// (they'll just ignore it if they didn't see that worker)
	worker := h.hub.Get(workerID)
	h.broadcastForWorker(WorkerEvent{
		Type:     "disconnected",
		WorkerID: workerID,
	}, worker)
}

// handleWorkerJobStarted is called when a job starts on a worker.
func (h *WorkerStreamHandler) handleWorkerJobStarted(workerID, jobID string) {
	worker := h.hub.Get(workerID)
	h.broadcastForWorker(WorkerEvent{
		Type:     "job_started",
		WorkerID: workerID,
		JobID:    jobID,
	}, worker)
}

// handleWorkerJobFinished is called when a job finishes on a worker.
func (h *WorkerStreamHandler) handleWorkerJobFinished(workerID, jobID string) {
	worker := h.hub.Get(workerID)
	h.broadcastForWorker(WorkerEvent{
		Type:     "job_finished",
		WorkerID: workerID,
		JobID:    jobID,
	}, worker)
}

// workerConnToInfo converts a WorkerConn to WorkerInfo for JSON serialization.
func workerConnToInfo(w *WorkerConn) *WorkerInfo {
	mode := string(w.Mode)
	if mode == "" {
		mode = "personal"
	}

	return &WorkerInfo{
		ID:         w.ID,
		Name:       w.Hostname,
		Hostname:   w.Hostname,
		Labels:     w.Labels,
		Mode:       mode,
		OwnerName:  w.OwnerName,
		Version:    w.Version,
		Connected:  true,
		ActiveJobs: w.ActiveJobs,
		LastSeen:   w.LastPing.Unix(),
	}
}
