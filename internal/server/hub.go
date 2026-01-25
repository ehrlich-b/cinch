package server

import (
	"sync"
	"time"
)

// WorkerConn represents a connected worker.
type WorkerConn struct {
	ID           string
	Labels       []string
	Capabilities Capabilities
	Hostname     string
	Version      string

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

// Hub manages connected workers.
type Hub struct {
	mu      sync.RWMutex
	workers map[string]*WorkerConn
}

// NewHub creates a new Hub.
func NewHub() *Hub {
	return &Hub{
		workers: make(map[string]*WorkerConn),
	}
}

// Register adds a worker to the hub.
func (h *Hub) Register(worker *WorkerConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	worker.LastPing = time.Now()
	h.workers[worker.ID] = worker
}

// Unregister removes a worker from the hub.
func (h *Hub) Unregister(workerID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if w, ok := h.workers[workerID]; ok {
		close(w.Send)
		delete(h.workers, workerID)
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
func (h *Hub) SelectWorker(labels []string) *WorkerConn {
	available := h.FindAvailable(labels)
	if len(available) == 0 {
		return nil
	}
	return available[0]
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
	defer h.mu.Unlock()
	if w, ok := h.workers[workerID]; ok {
		w.ActiveJobs = append(w.ActiveJobs, jobID)
	}
}

// RemoveActiveJob removes a job from a worker's active list.
func (h *Hub) RemoveActiveJob(workerID, jobID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if w, ok := h.workers[workerID]; ok {
		for i, id := range w.ActiveJobs {
			if id == jobID {
				w.ActiveJobs = append(w.ActiveJobs[:i], w.ActiveJobs[i+1:]...)
				break
			}
		}
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
