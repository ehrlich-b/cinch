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
	Type     string      `json:"type"`      // connected, disconnected, job_started, job_finished
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

// WorkerStreamHandler handles WebSocket connections for worker event streaming.
type WorkerStreamHandler struct {
	hub *Hub
	log *slog.Logger

	mu          sync.RWMutex
	subscribers map[*websocket.Conn]bool
}

// NewWorkerStreamHandler creates a new worker stream handler.
func NewWorkerStreamHandler(hub *Hub, log *slog.Logger) *WorkerStreamHandler {
	if log == nil {
		log = slog.Default()
	}
	h := &WorkerStreamHandler{
		hub:         hub,
		log:         log,
		subscribers: make(map[*websocket.Conn]bool),
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
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Error("websocket upgrade failed", "error", err)
		return
	}

	h.log.Debug("worker stream client connected")

	// Send current worker list as initial state
	if err := h.sendInitialState(conn); err != nil {
		h.log.Warn("failed to send initial state", "error", err)
		conn.Close()
		return
	}

	// Subscribe for updates
	h.subscribe(conn)

	// Read pump for close detection
	go h.readPump(conn)
}

// sendInitialState sends the current worker list to a new client.
func (h *WorkerStreamHandler) sendInitialState(conn *websocket.Conn) error {
	workers := h.hub.List()

	for _, w := range workers {
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

// subscribe adds a connection to subscribers.
func (h *WorkerStreamHandler) subscribe(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subscribers[conn] = true
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

// broadcast sends an event to all subscribers.
func (h *WorkerStreamHandler) broadcast(event WorkerEvent) {
	msgBytes, err := json.Marshal(event)
	if err != nil {
		h.log.Error("failed to marshal worker event", "error", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for conn := range h.subscribers {
		if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
			h.log.Warn("failed to broadcast worker event", "error", err)
		}
	}
}

// handleWorkerConnected is called when a worker connects.
func (h *WorkerStreamHandler) handleWorkerConnected(worker *WorkerConn) {
	h.broadcast(WorkerEvent{
		Type:     "connected",
		WorkerID: worker.ID,
		Worker:   workerConnToInfo(worker),
	})
}

// handleWorkerDisconnected is called when a worker disconnects.
func (h *WorkerStreamHandler) handleWorkerDisconnected(workerID string) {
	h.broadcast(WorkerEvent{
		Type:     "disconnected",
		WorkerID: workerID,
	})
}

// handleWorkerJobStarted is called when a job starts on a worker.
func (h *WorkerStreamHandler) handleWorkerJobStarted(workerID, jobID string) {
	h.broadcast(WorkerEvent{
		Type:     "job_started",
		WorkerID: workerID,
		JobID:    jobID,
	})
}

// handleWorkerJobFinished is called when a job finishes on a worker.
func (h *WorkerStreamHandler) handleWorkerJobFinished(workerID, jobID string) {
	h.broadcast(WorkerEvent{
		Type:     "job_finished",
		WorkerID: workerID,
		JobID:    jobID,
	})
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
