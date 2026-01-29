package server

import (
	"sync"
	"time"

	"github.com/ehrlich-b/cinch/internal/protocol"
	"github.com/ehrlich-b/cinch/internal/storage"
)

// WorkerConn represents a connected worker.
type WorkerConn struct {
	ID           string
	Name         string // Display name from database
	Labels       []string
	Capabilities Capabilities
	Hostname     string
	Version      string

	// Worker trust model
	Mode      protocol.WorkerMode // personal or shared
	OwnerID   string              // User ID of the worker's owner
	OwnerName string              // Username of the worker's owner

	// Connection state
	ActiveJobs []string
	LastPing   time.Time

	// Send is used to send messages to this worker.
	// The actual WebSocket connection is managed separately.
	Send chan []byte
}

// Capabilities describes what a worker can do.
type Capabilities struct {
	Docker bool
}

// AvailableSlots returns how many more jobs this worker can accept.
// One worker = one job. Want more parallelism? Run more workers.
func (w *WorkerConn) AvailableSlots() int {
	if len(w.ActiveJobs) > 0 {
		return 0
	}
	return 1
}

// HasLabel returns true if the worker has the given label.
func (w *WorkerConn) HasLabel(label string) bool {
	for _, l := range w.Labels {
		if l == label {
			return true
		}
	}
	return false
}

// WorkerEventCallback is the function signature for worker event callbacks.
type WorkerEventCallback func(*WorkerConn)

// WorkerJobEventCallback is the function signature for job event callbacks.
type WorkerJobEventCallback func(workerID, jobID string)

// Hub manages connected workers.
type Hub struct {
	mu      sync.RWMutex
	workers map[string]*WorkerConn

	// Event callbacks for UI streaming
	onWorkerConnected    WorkerEventCallback
	onWorkerDisconnected func(workerID string)
	onWorkerJobStarted   WorkerJobEventCallback
	onWorkerJobFinished  WorkerJobEventCallback
}

// NewHub creates a new Hub.
func NewHub() *Hub {
	return &Hub{
		workers: make(map[string]*WorkerConn),
	}
}

// SetEventCallbacks configures callbacks for worker events.
func (h *Hub) SetEventCallbacks(
	onConnected WorkerEventCallback,
	onDisconnected func(workerID string),
	onJobStarted WorkerJobEventCallback,
	onJobFinished WorkerJobEventCallback,
) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onWorkerConnected = onConnected
	h.onWorkerDisconnected = onDisconnected
	h.onWorkerJobStarted = onJobStarted
	h.onWorkerJobFinished = onJobFinished
}

// Register adds a worker to the hub.
func (h *Hub) Register(worker *WorkerConn) {
	h.mu.Lock()
	worker.LastPing = time.Now()
	h.workers[worker.ID] = worker
	callback := h.onWorkerConnected
	h.mu.Unlock()

	if callback != nil {
		callback(worker)
	}
}

// Unregister removes a worker from the hub.
func (h *Hub) Unregister(workerID string) {
	h.mu.Lock()
	if w, ok := h.workers[workerID]; ok {
		close(w.Send)
		delete(h.workers, workerID)
	}
	callback := h.onWorkerDisconnected
	h.mu.Unlock()

	if callback != nil {
		callback(workerID)
	}
}

// Get returns a worker by ID, or nil if not found.
func (h *Hub) Get(workerID string) *WorkerConn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.workers[workerID]
}

// List returns all connected workers.
func (h *Hub) List() []*WorkerConn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	workers := make([]*WorkerConn, 0, len(h.workers))
	for _, w := range h.workers {
		workers = append(workers, w)
	}
	return workers
}

// Count returns the number of connected workers.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.workers)
}

// FindAvailable returns available workers matching the given labels.
// If labels is empty, all available workers are returned.
// Workers are sorted by available slots (most available first).
func (h *Hub) FindAvailable(labels []string) []*WorkerConn {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var available []*WorkerConn
	for _, w := range h.workers {
		if w.AvailableSlots() <= 0 {
			continue
		}
		if len(labels) > 0 && !h.matchesLabels(w, labels) {
			continue
		}
		available = append(available, w)
	}

	// Sort by available slots descending (least loaded first)
	for i := 0; i < len(available)-1; i++ {
		for j := i + 1; j < len(available); j++ {
			if available[j].AvailableSlots() > available[i].AvailableSlots() {
				available[i], available[j] = available[j], available[i]
			}
		}
	}

	return available
}

// SelectWorker returns the best available worker for the given labels.
// Returns nil if no worker is available.
// Deprecated: Use SelectWorkerForJob for trust-aware dispatch.
func (h *Hub) SelectWorker(labels []string) *WorkerConn {
	available := h.FindAvailable(labels)
	if len(available) == 0 {
		return nil
	}
	return available[0]
}

// SelectWorkerForJob returns the best available worker for a job, considering trust model.
// Priority:
// 1. Author's personal worker (if online)
// 2. Shared worker (if author is collaborator/owner and no personal worker)
// 3. nil (for fork PRs without author's worker, or if no worker available)
func (h *Hub) SelectWorkerForJob(labels []string, job *storage.Job) *WorkerConn {
	available := h.FindAvailable(labels)
	if len(available) == 0 {
		return nil
	}

	// Legacy mode: if job has no author info, use simple label-based selection
	// This maintains backward compatibility with jobs created before trust model
	if job.Author == "" {
		return available[0]
	}

	// First, try to find author's personal worker
	for _, w := range available {
		if (w.Mode == "" || w.Mode == protocol.ModePersonal) && w.OwnerName == job.Author {
			return w
		}
	}

	// For fork PRs from external contributors, only author's personal worker can run
	// (unless explicitly approved)
	if job.IsFork && job.TrustLevel == storage.TrustExternal && job.ApprovedBy == nil {
		return nil // Must wait for author's worker or approval
	}

	// For collaborators/owners, check if author has a personal worker online
	// If so, defer to their worker
	if h.hasPersonalWorkerOnline(job.Author) {
		return nil // Defer to author's personal worker
	}

	// Find a shared worker that can run this job
	for _, w := range available {
		if w.Mode == protocol.ModeShared {
			return w
		}
	}

	// Fallback to any personal worker with empty owner (legacy workers)
	for _, w := range available {
		if w.Mode == "" || w.Mode == protocol.ModePersonal {
			if w.OwnerName == "" {
				// If OwnerName is empty (legacy worker), allow it
				return w
			}
		}
	}

	return nil
}

// hasPersonalWorkerOnline returns true if the given user has a personal worker online.
func (h *Hub) hasPersonalWorkerOnline(username string) bool {
	for _, w := range h.workers {
		if (w.Mode == "" || w.Mode == protocol.ModePersonal) && w.OwnerName == username {
			return true
		}
	}
	return false
}

// CanWorkerRunJob checks if a specific worker can run a job based on trust model.
func (h *Hub) CanWorkerRunJob(w *WorkerConn, job *storage.Job) bool {
	// Personal worker: only author's own code
	if w.Mode == "" || w.Mode == protocol.ModePersonal {
		return job.Author == w.OwnerName || w.OwnerName == ""
	}

	// Shared worker
	if w.Mode == protocol.ModeShared {
		// Check trust level
		switch job.TrustLevel {
		case storage.TrustOwner, storage.TrustCollaborator:
			// Check if author has their own worker online - defer to them
			if h.hasPersonalWorkerOnline(job.Author) {
				return false
			}
			return true
		case storage.TrustExternal:
			// External PRs must be explicitly approved
			return job.ApprovedBy != nil
		}
	}

	return false
}

// matchesLabels returns true if the worker has all the required labels.
func (h *Hub) matchesLabels(w *WorkerConn, required []string) bool {
	for _, label := range required {
		if !w.HasLabel(label) {
			return false
		}
	}
	return true
}

// AddActiveJob marks a job as active on a worker.
func (h *Hub) AddActiveJob(workerID, jobID string) {
	h.mu.Lock()
	if w, ok := h.workers[workerID]; ok {
		w.ActiveJobs = append(w.ActiveJobs, jobID)
	}
	callback := h.onWorkerJobStarted
	h.mu.Unlock()

	if callback != nil {
		callback(workerID, jobID)
	}
}

// RemoveActiveJob removes a job from a worker's active list.
func (h *Hub) RemoveActiveJob(workerID, jobID string) {
	h.mu.Lock()
	if w, ok := h.workers[workerID]; ok {
		for i, id := range w.ActiveJobs {
			if id == jobID {
				w.ActiveJobs = append(w.ActiveJobs[:i], w.ActiveJobs[i+1:]...)
				break
			}
		}
	}
	callback := h.onWorkerJobFinished
	h.mu.Unlock()

	if callback != nil {
		callback(workerID, jobID)
	}
}

// UpdateLastPing updates the last ping time for a worker.
func (h *Hub) UpdateLastPing(workerID string, activeJobs []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if w, ok := h.workers[workerID]; ok {
		w.LastPing = time.Now()
		w.ActiveJobs = activeJobs
	}
}

// FindStale returns workers that haven't pinged since the given duration.
func (h *Hub) FindStale(timeout time.Duration) []*WorkerConn {
	h.mu.RLock()
	defer h.mu.RUnlock()

	cutoff := time.Now().Add(-timeout)
	var stale []*WorkerConn
	for _, w := range h.workers {
		if w.LastPing.Before(cutoff) {
			stale = append(stale, w)
		}
	}
	return stale
}

// Broadcast sends a message to all connected workers.
func (h *Hub) Broadcast(msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, w := range h.workers {
		select {
		case w.Send <- msg:
		default:
			// Worker's send buffer is full, skip
		}
	}
}
