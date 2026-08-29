package worker

import (
	"testing"
	"time"

	"github.com/ehrlich-b/cinch/internal/protocol"
)

func addTestJob(w *Worker, id string, startedAt time.Time) {
	w.jobsLock.Lock()
	defer w.jobsLock.Unlock()
	w.activeJobs[id] = &JobInfo{ID: id, StartedAt: startedAt}
}

func TestNewWorkerNoJobs(t *testing.T) {
	w := NewWorker(WorkerConfig{}, nil)

	if jobs := w.GetRunningJobs(); len(jobs) != 0 {
		t.Errorf("GetRunningJobs() on fresh worker = %d jobs, want 0", len(jobs))
	}
	if id := w.OldestRunningJob(); id != "" {
		t.Errorf("OldestRunningJob() on fresh worker = %q, want empty", id)
	}
	if n := w.ActiveJobCount(); n != 0 {
		t.Errorf("ActiveJobCount() on fresh worker = %d, want 0", n)
	}
}

func TestRunningJobsWithFixtures(t *testing.T) {
	w := NewWorker(WorkerConfig{}, nil)

	base := time.Now()
	addTestJob(w, "job-c", base.Add(3*time.Second))
	addTestJob(w, "job-a", base.Add(1*time.Second))
	addTestJob(w, "job-b", base.Add(2*time.Second))

	jobs := w.GetRunningJobs()
	if len(jobs) != 3 {
		t.Fatalf("GetRunningJobs() = %d jobs, want 3", len(jobs))
	}
	got := make(map[string]bool)
	for _, j := range jobs {
		got[j.ID] = true
	}
	for _, want := range []string{"job-a", "job-b", "job-c"} {
		if !got[want] {
			t.Errorf("GetRunningJobs() missing job %q", want)
		}
	}

	if id := w.OldestRunningJob(); id != "job-a" {
		t.Errorf("OldestRunningJob() = %q, want %q (earliest StartedAt)", id, "job-a")
	}
	if n := w.ActiveJobCount(); n != 3 {
		t.Errorf("ActiveJobCount() = %d, want 3", n)
	}
}

func TestConcurrency(t *testing.T) {
	w := NewWorker(WorkerConfig{Concurrency: 4}, nil)
	if n := w.Concurrency(); n != 4 {
		t.Errorf("Concurrency() with Concurrency:4 = %d, want 4", n)
	}

	w = NewWorker(WorkerConfig{Concurrency: 0}, nil)
	if n := w.Concurrency(); n != 1 {
		t.Errorf("Concurrency() with Concurrency:0 = %d, want default 1", n)
	}

	w = NewWorker(WorkerConfig{Concurrency: -3}, nil)
	if n := w.Concurrency(); n != 1 {
		t.Errorf("Concurrency() with Concurrency:-3 = %d, want default 1", n)
	}
}

func TestIsCriticalMessage(t *testing.T) {
	tests := []struct {
		name    string
		msgType string
		want    bool
	}{
		{"job_complete", protocol.TypeJobComplete, true},
		{"job_error", protocol.TypeJobError, true},
		{"ping", protocol.TypePing, false},
		{"status_update", protocol.TypeStatusUpdate, false},
		{"log_chunk", protocol.TypeLogChunk, false},
		{"job_started", protocol.TypeJobStarted, false},
		{"unrecognized", "NOT_A_REAL_TYPE", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCriticalMessage(tt.msgType); got != tt.want {
				t.Errorf("isCriticalMessage(%q) = %v, want %v", tt.msgType, got, tt.want)
			}
		})
	}
}

func TestDrainNoJobs(t *testing.T) {
	w := NewWorker(WorkerConfig{}, nil)

	start := time.Now()
	n := w.Drain(2 * time.Second)
	elapsed := time.Since(start)

	if n != 0 {
		t.Errorf("Drain() with no jobs = %d, want 0", n)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("Drain() with no jobs took %v, want near-instant (early return, no polling)", elapsed)
	}
}

func TestDrainJobsComplete(t *testing.T) {
	w := NewWorker(WorkerConfig{}, nil)

	base := time.Now()
	addTestJob(w, "job-1", base)
	addTestJob(w, "job-2", base.Add(time.Second))

	// Simulate both jobs completing shortly after drain starts.
	go func() {
		time.Sleep(150 * time.Millisecond)
		w.jobsLock.Lock()
		delete(w.activeJobs, "job-1")
		delete(w.activeJobs, "job-2")
		w.jobsLock.Unlock()
	}()

	start := time.Now()
	n := w.Drain(2 * time.Second)
	elapsed := time.Since(start)

	if n != 2 {
		t.Errorf("Drain() with 2 jobs = %d, want 2 (initialCount at drain start)", n)
	}
	if elapsed >= 2*time.Second {
		t.Errorf("Drain() waited %v, want it to return once jobs cleared (completion path, not timeout)", elapsed)
	}
}

func TestDrainTimeout(t *testing.T) {
	w := NewWorker(WorkerConfig{}, nil)

	addTestJob(w, "job-1", time.Now())

	timeout := 200 * time.Millisecond
	start := time.Now()
	n := w.Drain(timeout)
	elapsed := time.Since(start)

	if n != 1 {
		t.Errorf("Drain() with 1 never-finishing job = %d, want 1 (initialCount)", n)
	}
	if elapsed < 150*time.Millisecond {
		t.Errorf("Drain() returned after %v, want ~timeout (%v) to fire since job never completes", elapsed, timeout)
	}

	// Clean up so the polling goroutine can exit.
	w.jobsLock.Lock()
	delete(w.activeJobs, "job-1")
	w.jobsLock.Unlock()
}

func TestIsDraining(t *testing.T) {
	w := NewWorker(WorkerConfig{}, nil)

	if w.IsDraining() {
		t.Error("IsDraining() on fresh worker = true, want false")
	}

	// Zero-job Drain takes the early-return path but must still set draining.
	if n := w.Drain(time.Second); n != 0 {
		t.Errorf("Drain() with no jobs = %d, want 0", n)
	}
	if !w.IsDraining() {
		t.Error("IsDraining() after Drain() = false, want true (early return still sets draining)")
	}

	// And it stays true after a drain that ran to timeout.
	w2 := NewWorker(WorkerConfig{}, nil)
	addTestJob(w2, "job-1", time.Now())
	w2.Drain(100 * time.Millisecond)
	if !w2.IsDraining() {
		t.Error("IsDraining() after timed-out Drain() = false, want true (never reset)")
	}
}
