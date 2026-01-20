package server

import (
	"testing"
	"time"
)

func TestHubRegisterUnregister(t *testing.T) {
	hub := NewHub()

	worker := &WorkerConn{
		ID:     "w_1",
		Labels: []string{"linux"},
		Send:   make(chan []byte, 10),
	}

	hub.Register(worker)

	if hub.Count() != 1 {
		t.Errorf("Count() = %d, want 1", hub.Count())
	}

	got := hub.Get("w_1")
	if got == nil {
		t.Fatal("Get() returned nil")
	}
	if got.ID != "w_1" {
		t.Errorf("ID = %q, want %q", got.ID, "w_1")
	}

	hub.Unregister("w_1")

	if hub.Count() != 0 {
		t.Errorf("Count() = %d, want 0", hub.Count())
	}
	if hub.Get("w_1") != nil {
		t.Error("Get() should return nil after unregister")
	}
}

func TestHubList(t *testing.T) {
	hub := NewHub()

	hub.Register(&WorkerConn{ID: "w_1", Send: make(chan []byte, 1)})
	hub.Register(&WorkerConn{ID: "w_2", Send: make(chan []byte, 1)})

	workers := hub.List()
	if len(workers) != 2 {
		t.Errorf("len(List()) = %d, want 2", len(workers))
	}
}

func TestHubFindAvailable(t *testing.T) {
	hub := NewHub()

	// Worker with 2 slots, 1 used
	hub.Register(&WorkerConn{
		ID:           "w_1",
		Labels:       []string{"linux", "amd64"},
		Capabilities: Capabilities{Concurrency: 2},
		ActiveJobs:   []string{"j_1"},
		Send:         make(chan []byte, 1),
	})

	// Worker with 1 slot, fully busy
	hub.Register(&WorkerConn{
		ID:           "w_2",
		Labels:       []string{"linux"},
		Capabilities: Capabilities{Concurrency: 1},
		ActiveJobs:   []string{"j_2"},
		Send:         make(chan []byte, 1),
	})

	// Worker with 3 slots, all free
	hub.Register(&WorkerConn{
		ID:           "w_3",
		Labels:       []string{"linux", "arm64"},
		Capabilities: Capabilities{Concurrency: 3},
		ActiveJobs:   []string{},
		Send:         make(chan []byte, 1),
	})

	// Find all available linux workers
	available := hub.FindAvailable([]string{"linux"})
	if len(available) != 2 {
		t.Fatalf("len(FindAvailable) = %d, want 2", len(available))
	}
	// Should be sorted by available slots (w_3 first with 3, then w_1 with 1)
	if available[0].ID != "w_3" {
		t.Errorf("First available = %q, want w_3", available[0].ID)
	}

	// Find available amd64 workers
	available = hub.FindAvailable([]string{"amd64"})
	if len(available) != 1 {
		t.Fatalf("len(FindAvailable) = %d, want 1", len(available))
	}
	if available[0].ID != "w_1" {
		t.Errorf("Available = %q, want w_1", available[0].ID)
	}

	// Find available arm64 workers
	available = hub.FindAvailable([]string{"arm64"})
	if len(available) != 1 {
		t.Fatalf("len(FindAvailable) = %d, want 1", len(available))
	}
	if available[0].ID != "w_3" {
		t.Errorf("Available = %q, want w_3", available[0].ID)
	}

	// Find with no labels - all available
	available = hub.FindAvailable(nil)
	if len(available) != 2 {
		t.Errorf("len(FindAvailable) = %d, want 2", len(available))
	}
}

func TestHubSelectWorker(t *testing.T) {
	hub := NewHub()

	hub.Register(&WorkerConn{
		ID:           "w_1",
		Labels:       []string{"linux"},
		Capabilities: Capabilities{Concurrency: 2},
		ActiveJobs:   []string{"j_1"},
		Send:         make(chan []byte, 1),
	})

	hub.Register(&WorkerConn{
		ID:           "w_2",
		Labels:       []string{"linux"},
		Capabilities: Capabilities{Concurrency: 2},
		ActiveJobs:   []string{},
		Send:         make(chan []byte, 1),
	})

	// Should select w_2 (has more available slots)
	worker := hub.SelectWorker([]string{"linux"})
	if worker == nil {
		t.Fatal("SelectWorker returned nil")
	}
	if worker.ID != "w_2" {
		t.Errorf("Selected = %q, want w_2", worker.ID)
	}

	// No windows workers available
	worker = hub.SelectWorker([]string{"windows"})
	if worker != nil {
		t.Error("SelectWorker should return nil for windows")
	}
}

func TestHubActiveJobs(t *testing.T) {
	hub := NewHub()

	hub.Register(&WorkerConn{
		ID:           "w_1",
		Capabilities: Capabilities{Concurrency: 2},
		Send:         make(chan []byte, 1),
	})

	hub.AddActiveJob("w_1", "j_1")
	hub.AddActiveJob("w_1", "j_2")

	worker := hub.Get("w_1")
	if len(worker.ActiveJobs) != 2 {
		t.Errorf("ActiveJobs = %d, want 2", len(worker.ActiveJobs))
	}
	if worker.AvailableSlots() != 0 {
		t.Errorf("AvailableSlots = %d, want 0", worker.AvailableSlots())
	}

	hub.RemoveActiveJob("w_1", "j_1")
	if len(worker.ActiveJobs) != 1 {
		t.Errorf("ActiveJobs = %d, want 1", len(worker.ActiveJobs))
	}
	if worker.ActiveJobs[0] != "j_2" {
		t.Errorf("ActiveJobs[0] = %q, want j_2", worker.ActiveJobs[0])
	}
}

func TestHubUpdateLastPing(t *testing.T) {
	hub := NewHub()

	hub.Register(&WorkerConn{
		ID:   "w_1",
		Send: make(chan []byte, 1),
	})

	time.Sleep(10 * time.Millisecond)
	hub.UpdateLastPing("w_1", []string{"j_new"})

	worker := hub.Get("w_1")
	if time.Since(worker.LastPing) > time.Second {
		t.Error("LastPing should be updated")
	}
	if len(worker.ActiveJobs) != 1 || worker.ActiveJobs[0] != "j_new" {
		t.Errorf("ActiveJobs = %v, want [j_new]", worker.ActiveJobs)
	}
}

func TestHubFindStale(t *testing.T) {
	hub := NewHub()

	// Fresh worker
	hub.Register(&WorkerConn{
		ID:   "w_1",
		Send: make(chan []byte, 1),
	})

	// Stale worker (manually set old LastPing)
	hub.Register(&WorkerConn{
		ID:   "w_2",
		Send: make(chan []byte, 1),
	})
	hub.mu.Lock()
	hub.workers["w_2"].LastPing = time.Now().Add(-2 * time.Minute)
	hub.mu.Unlock()

	stale := hub.FindStale(90 * time.Second)
	if len(stale) != 1 {
		t.Fatalf("len(FindStale) = %d, want 1", len(stale))
	}
	if stale[0].ID != "w_2" {
		t.Errorf("Stale = %q, want w_2", stale[0].ID)
	}
}

func TestWorkerConnHasLabel(t *testing.T) {
	w := &WorkerConn{
		Labels: []string{"linux", "amd64", "docker"},
	}

	if !w.HasLabel("linux") {
		t.Error("HasLabel(linux) should be true")
	}
	if !w.HasLabel("docker") {
		t.Error("HasLabel(docker) should be true")
	}
	if w.HasLabel("windows") {
		t.Error("HasLabel(windows) should be false")
	}
}

func TestWorkerConnAvailableSlots(t *testing.T) {
	tests := []struct {
		name        string
		concurrency int
		activeJobs  int
		want        int
	}{
		{"default concurrency", 0, 0, 1},
		{"default concurrency with job", 0, 1, 0},
		{"explicit concurrency", 3, 1, 2},
		{"fully loaded", 2, 2, 0},
		{"over capacity", 2, 3, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &WorkerConn{
				Capabilities: Capabilities{Concurrency: tt.concurrency},
				ActiveJobs:   make([]string, tt.activeJobs),
			}
			if got := w.AvailableSlots(); got != tt.want {
				t.Errorf("AvailableSlots() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestHubBroadcast(t *testing.T) {
	hub := NewHub()

	ch1 := make(chan []byte, 10)
	ch2 := make(chan []byte, 10)

	hub.Register(&WorkerConn{ID: "w_1", Send: ch1})
	hub.Register(&WorkerConn{ID: "w_2", Send: ch2})

	msg := []byte("test message")
	hub.Broadcast(msg)

	select {
	case got := <-ch1:
		if string(got) != string(msg) {
			t.Errorf("ch1 received %q, want %q", got, msg)
		}
	default:
		t.Error("ch1 should have received message")
	}

	select {
	case got := <-ch2:
		if string(got) != string(msg) {
			t.Errorf("ch2 received %q, want %q", got, msg)
		}
	default:
		t.Error("ch2 should have received message")
	}
}
