package server

import (
	"testing"
	"time"

	"github.com/ehrlich-b/cinch/internal/protocol"
	"github.com/ehrlich-b/cinch/internal/storage"
)

func TestDispatcherEnqueue(t *testing.T) {
	hub := NewHub()
	store, _ := storage.NewSQLite(":memory:")
	defer store.Close()

	// Create repo for foreign key
	err := store.CreateRepo(t.Context(), &storage.Repo{
		ID:        "r_1",
		ForgeType: storage.ForgeTypeGitHub,
		CloneURL:  "https://github.com/test/repo.git",
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("CreateRepo failed: %v", err)
	}

	dispatcher := NewDispatcher(hub, store, nil, nil)

	job := &storage.Job{
		ID:        "j_1",
		RepoID:    "r_1",
		Commit:    "abc123",
		Branch:    "main",
		Status:    storage.JobStatusPending,
		CreatedAt: time.Now(),
	}
	if err := store.CreateJob(t.Context(), job); err != nil {
		t.Fatalf("CreateJob failed: %v", err)
	}

	repo := &storage.Repo{
		ID:        "r_1",
		ForgeType: storage.ForgeTypeGitHub,
		CloneURL:  "https://github.com/test/repo.git",
	}
	queued := &QueuedJob{
		Job:      job,
		Repo:     repo,
		Labels:   []string{"linux"},
		Config:   protocol.JobConfig{Command: "make test"},
		CloneURL: "https://github.com/test/repo.git",
		Branch:   "main",
	}

	dispatcher.Enqueue(queued)

	if dispatcher.QueueLength() != 1 {
		t.Errorf("QueueLength = %d, want 1", dispatcher.QueueLength())
	}

	// Verify job status updated to queued
	gotJob, _ := store.GetJob(t.Context(), "j_1")
	if gotJob.Status != storage.JobStatusQueued {
		t.Errorf("job status = %s, want %s", gotJob.Status, storage.JobStatusQueued)
	}
}

func TestDispatcherAssignment(t *testing.T) {
	hub := NewHub()
	store, _ := storage.NewSQLite(":memory:")
	defer store.Close()

	// Create repo
	err := store.CreateRepo(t.Context(), &storage.Repo{
		ID:        "r_1",
		ForgeType: storage.ForgeTypeGitHub,
		CloneURL:  "https://github.com/test/repo.git",
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("CreateRepo failed: %v", err)
	}

	// Create worker in storage (for foreign key)
	err = store.CreateWorker(t.Context(), &storage.Worker{
		ID:        "w_1",
		Name:      "test-worker",
		Labels:    []string{"linux"},
		Status:    storage.WorkerStatusOnline,
		LastSeen:  time.Now(),
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("CreateWorker failed: %v", err)
	}

	// Register a worker in hub
	workerSend := make(chan []byte, 10)
	hub.Register(&WorkerConn{
		ID:           "w_1",
		Labels:       []string{"linux"},
		Capabilities: Capabilities{Concurrency: 2},
		Send:         workerSend,
	})

	ws := &WSHandler{hub: hub, storage: store}
	dispatcher := NewDispatcher(hub, store, ws, nil)
	dispatcher.Start()
	defer dispatcher.Stop()

	// Create and enqueue job
	job := &storage.Job{
		ID:        "j_1",
		RepoID:    "r_1",
		Commit:    "abc123",
		Branch:    "main",
		Status:    storage.JobStatusPending,
		CreatedAt: time.Now(),
	}
	if err := store.CreateJob(t.Context(), job); err != nil {
		t.Fatalf("CreateJob failed: %v", err)
	}

	repo := &storage.Repo{
		ID:        "r_1",
		ForgeType: storage.ForgeTypeGitHub,
		CloneURL:  "https://github.com/test/repo.git",
	}
	queued := &QueuedJob{
		Job:      job,
		Repo:     repo,
		Labels:   []string{"linux"},
		Config:   protocol.JobConfig{Command: "make test"},
		CloneURL: "https://github.com/test/repo.git",
		Branch:   "main",
	}

	dispatcher.Enqueue(queued)

	// Wait for dispatch
	select {
	case msg := <-workerSend:
		msgType, _, err := protocol.Decode(msg)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if msgType != protocol.TypeJobAssign {
			t.Errorf("message type = %s, want %s", msgType, protocol.TypeJobAssign)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for job assignment")
	}

	// Queue should be empty
	time.Sleep(100 * time.Millisecond)
	if dispatcher.QueueLength() != 0 {
		t.Errorf("QueueLength = %d, want 0", dispatcher.QueueLength())
	}
}

func TestDispatcherNoMatchingWorker(t *testing.T) {
	hub := NewHub()
	store, _ := storage.NewSQLite(":memory:")
	defer store.Close()

	// Create repo
	err := store.CreateRepo(t.Context(), &storage.Repo{
		ID:        "r_1",
		ForgeType: storage.ForgeTypeGitHub,
		CloneURL:  "https://github.com/test/repo.git",
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("CreateRepo failed: %v", err)
	}

	// Register a linux worker
	hub.Register(&WorkerConn{
		ID:           "w_1",
		Labels:       []string{"linux"},
		Capabilities: Capabilities{Concurrency: 1},
		Send:         make(chan []byte, 10),
	})

	ws := &WSHandler{hub: hub, storage: store}
	dispatcher := NewDispatcher(hub, store, ws, nil)

	// Create job requiring windows
	job := &storage.Job{
		ID:        "j_1",
		RepoID:    "r_1",
		Commit:    "abc123",
		Branch:    "main",
		Status:    storage.JobStatusPending,
		CreatedAt: time.Now(),
	}
	if err := store.CreateJob(t.Context(), job); err != nil {
		t.Fatalf("CreateJob failed: %v", err)
	}

	repo := &storage.Repo{
		ID:        "r_1",
		ForgeType: storage.ForgeTypeGitHub,
		CloneURL:  "https://github.com/test/repo.git",
	}
	queued := &QueuedJob{
		Job:    job,
		Repo:   repo,
		Labels: []string{"windows"},
		Config: protocol.JobConfig{Command: "make test"},
	}

	dispatcher.Enqueue(queued)

	// Trigger dispatch
	dispatcher.tryDispatch()

	// Job should still be in queue (no matching worker)
	if dispatcher.QueueLength() != 1 {
		t.Errorf("QueueLength = %d, want 1", dispatcher.QueueLength())
	}
}

func TestDispatcherPendingJobs(t *testing.T) {
	hub := NewHub()
	store, _ := storage.NewSQLite(":memory:")
	defer store.Close()

	dispatcher := NewDispatcher(hub, store, nil, nil)

	// Enqueue multiple jobs
	for i := 0; i < 3; i++ {
		job := &storage.Job{ID: string(rune('a' + i))}
		dispatcher.Enqueue(&QueuedJob{Job: job})
	}

	pending := dispatcher.PendingJobs()
	if len(pending) != 3 {
		t.Errorf("len(PendingJobs) = %d, want 3", len(pending))
	}
}
