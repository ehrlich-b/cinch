package server

import (
	"testing"

	"github.com/ehrlich-b/cinch/internal/protocol"
	"github.com/ehrlich-b/cinch/internal/storage"
)

// TestSelectWorkerForJobLegacyAuthorless covers the legacy path: a job with no
// author info bypasses all trust logic and just takes the first available
// worker. Both workers are deliberately configured so that trust-aware
// selection would find nothing (no shared worker, no empty-owner fallback),
// proving mode/owner are ignored on this path.
func TestSelectWorkerForJobLegacyAuthorless(t *testing.T) {
	hub := NewHub()
	hub.Register(&WorkerConn{ID: "w1", Mode: protocol.ModePersonal, OwnerName: "bob", Send: make(chan []byte, 1)})
	hub.Register(&WorkerConn{ID: "w2", Mode: protocol.ModePersonal, OwnerName: "carol", Send: make(chan []byte, 1)})

	job := &storage.Job{Author: ""} // legacy job, no author metadata
	got := hub.SelectWorkerForJob(nil, job)
	if got == nil {
		t.Fatal("SelectWorkerForJob returned nil, want an available worker")
	}
	if got.ID != "w1" && got.ID != "w2" {
		t.Errorf("SelectWorkerForJob = %q, want one of {w1, w2}", got.ID)
	}
}

// TestSelectWorkerForJobAuthorPersonalWins: the author's own personal worker
// always wins even when a shared worker is also available.
func TestSelectWorkerForJobAuthorPersonalWins(t *testing.T) {
	hub := NewHub()
	hub.Register(&WorkerConn{ID: "shared1", Mode: protocol.ModeShared, Send: make(chan []byte, 1)})
	hub.Register(&WorkerConn{ID: "alice1", Mode: protocol.ModePersonal, OwnerName: "alice", Send: make(chan []byte, 1)})

	job := &storage.Job{Author: "alice", TrustLevel: storage.TrustCollaborator}
	got := hub.SelectWorkerForJob(nil, job)
	if got == nil || got.ID != "alice1" {
		t.Fatalf("SelectWorkerForJob = %v, want alice1", got)
	}
}

// TestSelectWorkerForJobForkExternalUnapproved: an unapproved fork PR from an
// external author with no worker of their own may not be dispatched to anyone.
func TestSelectWorkerForJobForkExternalUnapproved(t *testing.T) {
	hub := NewHub()
	hub.Register(&WorkerConn{ID: "shared1", Mode: protocol.ModeShared, Send: make(chan []byte, 1)})

	job := &storage.Job{Author: "mallory", TrustLevel: storage.TrustExternal, IsFork: true}
	got := hub.SelectWorkerForJob(nil, job)
	if got != nil {
		t.Fatalf("SelectWorkerForJob = %v, want nil", got)
	}
}

// TestSelectWorkerForJobForkExternalApproved: explicit approval unlocks shared
// dispatch even for a fork PR.
func TestSelectWorkerForJobForkExternalApproved(t *testing.T) {
	hub := NewHub()
	hub.Register(&WorkerConn{ID: "shared1", Mode: protocol.ModeShared, Send: make(chan []byte, 1)})

	approved := "alice"
	job := &storage.Job{Author: "mallory", TrustLevel: storage.TrustExternal, IsFork: true, ApprovedBy: &approved}
	got := hub.SelectWorkerForJob(nil, job)
	if got == nil || got.ID != "shared1" {
		t.Fatalf("SelectWorkerForJob = %v, want shared1", got)
	}
}

// TestSelectWorkerForJobDeferToBusyPersonalWorker: the author's personal
// worker exists in the hub but is busy, and a shared worker is available. The
// selector still returns nil because hasPersonalWorkerOnline scans all
// registered workers regardless of availability.
//
// BUG (not fixed, per task): this starves the job whenever the author's
// personal worker is merely busy (or lacks the required labels) — the job is
// deferred to a worker that cannot take it, and waits until that worker frees
// up. A shared worker that is ready and available is passed over. Product may
// or may not want this; it is pinned here and intentionally NOT fixed.
func TestSelectWorkerForJobDeferToBusyPersonalWorker(t *testing.T) {
	hub := NewHub()
	hub.Register(&WorkerConn{ID: "alice_busy", Mode: protocol.ModePersonal, OwnerName: "alice", ActiveJobs: []string{"j1"}, Send: make(chan []byte, 1)})
	hub.Register(&WorkerConn{ID: "shared1", Mode: protocol.ModeShared, Send: make(chan []byte, 1)})

	job := &storage.Job{Author: "alice", TrustLevel: storage.TrustCollaborator}
	got := hub.SelectWorkerForJob(nil, job)
	if got != nil {
		t.Fatalf("SelectWorkerForJob = %v, want nil (deferred to busy personal worker)", got)
	}
}

// TestSelectWorkerForJobSharedForCollaboratorNoPersonalWorker: with no
// personal worker registered for the author anywhere, a shared worker is used.
func TestSelectWorkerForJobSharedForCollaboratorNoPersonalWorker(t *testing.T) {
	hub := NewHub()
	hub.Register(&WorkerConn{ID: "shared1", Mode: protocol.ModeShared, Send: make(chan []byte, 1)})

	job := &storage.Job{Author: "alice", TrustLevel: storage.TrustCollaborator}
	got := hub.SelectWorkerForJob(nil, job)
	if got == nil || got.ID != "shared1" {
		t.Fatalf("SelectWorkerForJob = %v, want shared1", got)
	}
}

// TestSelectWorkerForJobEmptyOwnerFallback: with no shared workers available,
// a legacy personal worker with an empty OwnerName is used as a fallback.
func TestSelectWorkerForJobEmptyOwnerFallback(t *testing.T) {
	hub := NewHub()
	hub.Register(&WorkerConn{ID: "legacy1", Mode: protocol.ModePersonal, OwnerName: "", Send: make(chan []byte, 1)})

	job := &storage.Job{Author: "alice", TrustLevel: storage.TrustCollaborator}
	got := hub.SelectWorkerForJob(nil, job)
	if got == nil || got.ID != "legacy1" {
		t.Fatalf("SelectWorkerForJob = %v, want legacy1", got)
	}
}

// TestSelectWorkerForJobNoWorkers: nothing available, nothing selected.
func TestSelectWorkerForJobNoWorkers(t *testing.T) {
	hub := NewHub()
	hub.Register(&WorkerConn{ID: "w1", Mode: protocol.ModeShared, ActiveJobs: []string{"j1"}, Send: make(chan []byte, 1)})

	job := &storage.Job{Author: "alice", TrustLevel: storage.TrustCollaborator}
	got := hub.SelectWorkerForJob(nil, job)
	if got != nil {
		t.Fatalf("SelectWorkerForJob = %v, want nil", got)
	}
}

func TestCanWorkerRunJob(t *testing.T) {
	tests := []struct {
		name       string
		worker     *WorkerConn
		job        *storage.Job
		hubWorkers []*WorkerConn // registered so hasPersonalWorkerOnline sees them
		want       bool
	}{
		{
			name:   "personal worker matches author",
			worker: &WorkerConn{Mode: protocol.ModePersonal, OwnerName: "alice"},
			job:    &storage.Job{Author: "alice"},
			want:   true,
		},
		{
			name:   "personal worker differs from author",
			worker: &WorkerConn{Mode: protocol.ModePersonal, OwnerName: "alice"},
			job:    &storage.Job{Author: "bob"},
			want:   false,
		},
		{
			name:   "legacy personal worker empty owner runs anything",
			worker: &WorkerConn{Mode: protocol.ModePersonal, OwnerName: ""},
			job:    &storage.Job{Author: "bob"},
			want:   true,
		},
		{
			name:   "shared worker trust owner no personal online",
			worker: &WorkerConn{Mode: protocol.ModeShared},
			job:    &storage.Job{Author: "alice", TrustLevel: storage.TrustOwner},
			want:   true,
		},
		{
			name:   "shared worker trust owner personal online defer",
			worker: &WorkerConn{Mode: protocol.ModeShared},
			job:    &storage.Job{Author: "alice", TrustLevel: storage.TrustOwner},
			hubWorkers: []*WorkerConn{
				{ID: "alice1", Mode: protocol.ModePersonal, OwnerName: "alice", Send: make(chan []byte, 1)},
			},
			want: false,
		},
		{
			name:   "shared worker trust external unapproved",
			worker: &WorkerConn{Mode: protocol.ModeShared},
			job:    &storage.Job{Author: "mallory", TrustLevel: storage.TrustExternal},
			want:   false,
		},
		{
			name:   "shared worker trust external approved",
			worker: &WorkerConn{Mode: protocol.ModeShared},
			job:    &storage.Job{Author: "mallory", TrustLevel: storage.TrustExternal, ApprovedBy: strptr("alice")},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := NewHub()
			for _, w := range tt.hubWorkers {
				hub.Register(w)
			}
			if got := hub.CanWorkerRunJob(tt.worker, tt.job); got != tt.want {
				t.Errorf("CanWorkerRunJob() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHubHasPersonalWorkerOnline(t *testing.T) {
	t.Run("personal worker with matching name", func(t *testing.T) {
		hub := NewHub()
		hub.Register(&WorkerConn{ID: "alice1", Mode: protocol.ModePersonal, OwnerName: "alice", Send: make(chan []byte, 1)})
		if !hub.hasPersonalWorkerOnline("alice") {
			t.Error("hasPersonalWorkerOnline(alice) = false, want true")
		}
	})

	t.Run("only shared worker with that name", func(t *testing.T) {
		hub := NewHub()
		hub.Register(&WorkerConn{ID: "alice_shared", Mode: protocol.ModeShared, OwnerName: "alice", Send: make(chan []byte, 1)})
		if hub.hasPersonalWorkerOnline("alice") {
			t.Error("hasPersonalWorkerOnline(alice) = true, want false")
		}
	})

	t.Run("no workers", func(t *testing.T) {
		hub := NewHub()
		if hub.hasPersonalWorkerOnline("alice") {
			t.Error("hasPersonalWorkerOnline(alice) = true, want false")
		}
	})
}

func strptr(s string) *string { return &s }
