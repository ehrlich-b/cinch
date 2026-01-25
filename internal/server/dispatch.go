package server

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/ehrlich-b/cinch/internal/protocol"
	"github.com/ehrlich-b/cinch/internal/storage"
)

// Dispatcher manages job queue and worker assignment.
type Dispatcher struct {
	hub     *Hub
	storage storage.Storage
	ws      *WSHandler
	log     *slog.Logger

	// Job queue
	mu       sync.Mutex
	queue    []*QueuedJob
	inflight map[string]*QueuedJob // jobs dispatched but not completed
	queueCh  chan struct{}         // signals new jobs in queue

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// QueuedJob represents a job waiting for a worker.
type QueuedJob struct {
	Job            *storage.Job
	Repo           *storage.Repo // Storage repo (for forge token, webhook secret)
	Labels         []string
	Config         protocol.JobConfig // Command, env vars, etc.
	CloneURL       string             // Clone URL
	Ref            string             // Full ref (refs/heads/main or refs/tags/v1.0.0)
	Branch         string             // Branch to checkout (empty for tags)
	Tag            string             // Tag name (empty for branches)
	CloneToken     string             // Token for cloning private repos
	Forge          interface{}        // Forge implementation (for status posting)
	InstallationID int64              // GitHub App installation ID
	QueuedAt       time.Time
	Attempts       int
	MaxRetries     int
}

// NewDispatcher creates a new job dispatcher.
func NewDispatcher(hub *Hub, store storage.Storage, ws *WSHandler, log *slog.Logger) *Dispatcher {
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Dispatcher{
		hub:      hub,
		storage:  store,
		ws:       ws,
		log:      log,
		queue:    make([]*QueuedJob, 0),
		inflight: make(map[string]*QueuedJob),
		queueCh:  make(chan struct{}, 1),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start begins the dispatch loop.
func (d *Dispatcher) Start() {
	d.wg.Add(2)
	go d.dispatchLoop()
	go d.timeoutLoop()
}

// Stop stops the dispatcher and waits for goroutines.
func (d *Dispatcher) Stop() {
	d.cancel()
	d.wg.Wait()
}

// Enqueue adds a job to the queue.
func (d *Dispatcher) Enqueue(job *QueuedJob) {
	d.mu.Lock()
	defer d.mu.Unlock()

	job.QueuedAt = time.Now()
	d.queue = append(d.queue, job)

	// Update job status to queued
	ctx := context.Background()
	if err := d.storage.UpdateJobStatus(ctx, job.Job.ID, storage.JobStatusQueued, nil); err != nil {
		d.log.Error("failed to update job status to queued", "job_id", job.Job.ID, "error", err)
	}

	// Signal dispatch loop
	select {
	case d.queueCh <- struct{}{}:
	default:
	}

	d.log.Info("job enqueued", "job_id", job.Job.ID, "labels", job.Labels)
}

// dispatchLoop continuously attempts to assign queued jobs to workers.
func (d *Dispatcher) dispatchLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-d.queueCh:
			d.tryDispatch()
		case <-ticker.C:
			d.tryDispatch()
		}
	}
}

// tryDispatch attempts to assign queued jobs to available workers.
func (d *Dispatcher) tryDispatch() {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Process queue from front
	remaining := make([]*QueuedJob, 0, len(d.queue))
	for _, qj := range d.queue {
		if d.tryAssign(qj) {
			d.log.Info("job dispatched", "job_id", qj.Job.ID)
		} else {
			remaining = append(remaining, qj)
		}
	}
	d.queue = remaining
}

// tryAssign attempts to assign a job to an available worker.
// Returns true if successful.
func (d *Dispatcher) tryAssign(qj *QueuedJob) bool {
	worker := d.hub.SelectWorker(qj.Labels)
	if worker == nil {
		return false
	}

	// Update job with worker assignment
	ctx := context.Background()
	if err := d.storage.UpdateJobWorker(ctx, qj.Job.ID, worker.ID); err != nil {
		d.log.Error("failed to update job worker", "job_id", qj.Job.ID, "error", err)
		return false
	}

	// Build job assignment
	assign := protocol.JobAssign{
		JobID: qj.Job.ID,
		Repo: protocol.JobRepo{
			CloneURL:   qj.CloneURL,
			Ref:        qj.Ref,
			Branch:     qj.Branch,
			Tag:        qj.Tag,
			Commit:     qj.Job.Commit,
			CloneToken: qj.CloneToken,
			ForgeType:  string(qj.Repo.ForgeType),
		},
		Config: qj.Config,
	}

	// Mark worker as busy BEFORE sending (prevents over-dispatch)
	d.hub.AddActiveJob(worker.ID, qj.Job.ID)

	// Track in-flight job for potential re-queue
	d.inflight[qj.Job.ID] = qj

	// Send to worker
	if err := d.ws.SendJob(worker.ID, assign); err != nil {
		d.log.Error("failed to send job to worker", "job_id", qj.Job.ID, "worker_id", worker.ID, "error", err)
		d.hub.RemoveActiveJob(worker.ID, qj.Job.ID)
		delete(d.inflight, qj.Job.ID)
		return false
	}

	d.log.Info("job assigned", "job_id", qj.Job.ID, "worker_id", worker.ID)
	return true
}

// timeoutLoop checks for stale workers and timed-out jobs.
func (d *Dispatcher) timeoutLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.checkStaleWorkers()
			d.checkJobTimeouts()
		}
	}
}

// checkStaleWorkers finds and removes workers that haven't pinged recently.
func (d *Dispatcher) checkStaleWorkers() {
	stale := d.hub.FindStale(90 * time.Second)
	for _, w := range stale {
		d.log.Warn("removing stale worker", "worker_id", w.ID, "last_ping", w.LastPing)

		// Mark any active jobs as error
		for _, jobID := range w.ActiveJobs {
			ctx := context.Background()
			if err := d.storage.UpdateJobStatus(ctx, jobID, storage.JobStatusError, nil); err != nil {
				d.log.Error("failed to update job status", "job_id", jobID, "error", err)
			}
		}

		// Update worker status in storage
		ctx := context.Background()
		if err := d.storage.UpdateWorkerStatus(ctx, w.ID, storage.WorkerStatusOffline); err != nil {
			d.log.Error("failed to update worker status", "worker_id", w.ID, "error", err)
		}

		d.hub.Unregister(w.ID)
	}
}

// checkJobTimeouts marks jobs that have been queued too long.
func (d *Dispatcher) checkJobTimeouts() {
	d.mu.Lock()
	defer d.mu.Unlock()

	timeout := 30 * time.Minute // Max time in queue
	now := time.Now()

	remaining := make([]*QueuedJob, 0, len(d.queue))
	for _, qj := range d.queue {
		if now.Sub(qj.QueuedAt) > timeout {
			d.log.Warn("job timed out in queue", "job_id", qj.Job.ID, "queued_at", qj.QueuedAt)
			ctx := context.Background()
			if err := d.storage.UpdateJobStatus(ctx, qj.Job.ID, storage.JobStatusError, nil); err != nil {
				d.log.Error("failed to update job status", "job_id", qj.Job.ID, "error", err)
			}
		} else {
			remaining = append(remaining, qj)
		}
	}
	d.queue = remaining
}

// QueueLength returns the current queue size.
func (d *Dispatcher) QueueLength() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.queue)
}

// NotifyWorkerAvailable signals that a worker has become available.
// This triggers an immediate dispatch attempt for queued jobs.
func (d *Dispatcher) NotifyWorkerAvailable() {
	select {
	case d.queueCh <- struct{}{}:
	default:
	}
}

// PendingJobs returns jobs currently in the queue.
func (d *Dispatcher) PendingJobs() []*QueuedJob {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make([]*QueuedJob, len(d.queue))
	copy(result, d.queue)
	return result
}

// Requeue puts a rejected/failed job back in the queue.
func (d *Dispatcher) Requeue(jobID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	qj, ok := d.inflight[jobID]
	if !ok {
		d.log.Warn("cannot requeue: job not found in inflight", "job_id", jobID)
		return
	}

	delete(d.inflight, jobID)
	qj.Attempts++

	// Check max retries
	if qj.MaxRetries > 0 && qj.Attempts >= qj.MaxRetries {
		d.log.Warn("job exceeded max retries", "job_id", jobID, "attempts", qj.Attempts)
		ctx := context.Background()
		if err := d.storage.UpdateJobStatus(ctx, jobID, storage.JobStatusError, nil); err != nil {
			d.log.Error("failed to update job status", "job_id", jobID, "error", err)
		}
		return
	}

	// Put back at front of queue for immediate retry
	d.queue = append([]*QueuedJob{qj}, d.queue...)

	// Update status back to queued
	ctx := context.Background()
	if err := d.storage.UpdateJobStatus(ctx, jobID, storage.JobStatusQueued, nil); err != nil {
		d.log.Error("failed to update job status to queued", "job_id", jobID, "error", err)
	}

	d.log.Info("job requeued", "job_id", jobID, "attempt", qj.Attempts)

	// Signal dispatch loop
	select {
	case d.queueCh <- struct{}{}:
	default:
	}
}

// CompleteJob removes a job from inflight tracking.
func (d *Dispatcher) CompleteJob(jobID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.inflight, jobID)
}

// RequeueWorkerJobs re-queues all jobs that were assigned to a disconnected worker.
func (d *Dispatcher) RequeueWorkerJobs(jobIDs []string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, jobID := range jobIDs {
		qj, ok := d.inflight[jobID]
		if !ok {
			continue
		}

		delete(d.inflight, jobID)

		// Put back at front of queue
		d.queue = append([]*QueuedJob{qj}, d.queue...)

		// Update status back to queued
		ctx := context.Background()
		if err := d.storage.UpdateJobStatus(ctx, jobID, storage.JobStatusQueued, nil); err != nil {
			d.log.Error("failed to update job status to queued", "job_id", jobID, "error", err)
		}

		d.log.Info("job requeued (worker disconnected)", "job_id", jobID)
	}

	// Signal dispatch loop to try assigning to other workers
	if len(jobIDs) > 0 {
		select {
		case d.queueCh <- struct{}{}:
		default:
		}
	}
}
