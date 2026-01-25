package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		msgType string
		payload any
	}{
		{
			name:    "AuthOK",
			msgType: TypeAuthOK,
			payload: AuthOK{WorkerID: "w_123", ServerVersion: "0.1.0"},
		},
		{
			name:    "AuthFail",
			msgType: TypeAuthFail,
			payload: AuthFail{Error: "invalid token"},
		},
		{
			name:    "Register",
			msgType: TypeRegister,
			payload: Register{
				Labels:       []string{"linux", "amd64"},
				Capabilities: Capabilities{Docker: true},
				Version:      "0.1.0",
				Hostname:     "build-1",
			},
		},
		{
			name:    "JobAssign",
			msgType: TypeJobAssign,
			payload: JobAssign{
				JobID: "j_abc",
				Repo: JobRepo{
					CloneURL:   "https://github.com/user/repo.git",
					CloneToken: "ghs_xxx",
					Commit:     "abc123",
					Branch:     "main",
					IsPR:       false,
				},
				Config: JobConfig{
					Command: "make test",
					Timeout: "30m",
					Env:     map[string]string{"CI": "true"},
				},
			},
		},
		{
			name:    "LogChunk",
			msgType: TypeLogChunk,
			payload: LogChunk{
				JobID:     "j_abc",
				Timestamp: 1705312800,
				Stream:    StreamStdout,
				Data:      "Running tests...\n",
			},
		},
		{
			name:    "JobComplete",
			msgType: TypeJobComplete,
			payload: JobComplete{
				JobID:      "j_abc",
				ExitCode:   0,
				DurationMs: 5230,
				Timestamp:  1705312845,
			},
		},
		{
			name:    "JobError",
			msgType: TypeJobError,
			payload: JobError{
				JobID: "j_abc",
				Error: "failed to clone",
				Phase: PhaseClone,
			},
		},
		{
			name:    "Ping",
			msgType: TypePing,
			payload: Ping{
				Timestamp:  1705312800,
				ActiveJobs: []string{"j_1", "j_2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			data, err := Encode(tt.msgType, tt.payload)
			if err != nil {
				t.Fatalf("Encode failed: %v", err)
			}

			// Verify it's valid JSON
			var msg Message
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("Invalid JSON: %v", err)
			}

			// Decode
			gotType, gotPayload, err := Decode(data)
			if err != nil {
				t.Fatalf("Decode failed: %v", err)
			}

			if gotType != tt.msgType {
				t.Errorf("type = %q, want %q", gotType, tt.msgType)
			}

			// Verify payload can be unmarshaled
			if len(gotPayload) == 0 {
				t.Error("payload is empty")
			}
		})
	}
}

func TestDecodePayload(t *testing.T) {
	original := JobAssign{
		JobID: "j_test",
		Repo: JobRepo{
			CloneURL: "https://github.com/test/repo.git",
			Commit:   "deadbeef",
			Branch:   "main",
		},
		Config: JobConfig{
			Command: "make ci",
		},
	}

	data, err := Encode(TypeJobAssign, original)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	msgType, payload, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if msgType != TypeJobAssign {
		t.Fatalf("type = %q, want %q", msgType, TypeJobAssign)
	}

	got, err := DecodePayload[JobAssign](payload)
	if err != nil {
		t.Fatalf("DecodePayload failed: %v", err)
	}

	if got.JobID != original.JobID {
		t.Errorf("JobID = %q, want %q", got.JobID, original.JobID)
	}
	if got.Repo.CloneURL != original.Repo.CloneURL {
		t.Errorf("CloneURL = %q, want %q", got.Repo.CloneURL, original.Repo.CloneURL)
	}
	if got.Config.Command != original.Config.Command {
		t.Errorf("Command = %q, want %q", got.Config.Command, original.Config.Command)
	}
}

func TestDecodeInvalidJSON(t *testing.T) {
	_, _, err := Decode([]byte("not valid json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestDecodePayloadTypeMismatch(t *testing.T) {
	data, _ := Encode(TypeAuthOK, AuthOK{WorkerID: "w_1"})
	_, payload, _ := Decode(data)

	// Try to decode as wrong type - should fail or have zero values
	got, err := DecodePayload[JobAssign](payload)
	if err != nil {
		// Error is fine
		return
	}
	// If no error, fields should be zero
	if got.JobID != "" {
		t.Error("expected empty JobID for type mismatch")
	}
}

func TestNewLogChunk(t *testing.T) {
	before := time.Now().Unix()
	chunk := NewLogChunk("j_1", StreamStdout, "hello\n")
	after := time.Now().Unix()

	if chunk.JobID != "j_1" {
		t.Errorf("JobID = %q, want %q", chunk.JobID, "j_1")
	}
	if chunk.Stream != StreamStdout {
		t.Errorf("Stream = %q, want %q", chunk.Stream, StreamStdout)
	}
	if chunk.Data != "hello\n" {
		t.Errorf("Data = %q, want %q", chunk.Data, "hello\n")
	}
	if chunk.Timestamp < before || chunk.Timestamp > after {
		t.Errorf("Timestamp %d not in expected range [%d, %d]", chunk.Timestamp, before, after)
	}
}

func TestNewJobComplete(t *testing.T) {
	duration := 5*time.Second + 230*time.Millisecond
	before := time.Now().Unix()
	complete := NewJobComplete("j_1", 0, duration)
	after := time.Now().Unix()

	if complete.JobID != "j_1" {
		t.Errorf("JobID = %q, want %q", complete.JobID, "j_1")
	}
	if complete.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want %d", complete.ExitCode, 0)
	}
	if complete.DurationMs != 5230 {
		t.Errorf("DurationMs = %d, want %d", complete.DurationMs, 5230)
	}
	if complete.Timestamp < before || complete.Timestamp > after {
		t.Errorf("Timestamp %d not in expected range", complete.Timestamp)
	}
}

func TestNewPing(t *testing.T) {
	activeJobs := []string{"j_1", "j_2"}
	before := time.Now().Unix()
	ping := NewPing(activeJobs)
	after := time.Now().Unix()

	if len(ping.ActiveJobs) != 2 {
		t.Errorf("ActiveJobs len = %d, want 2", len(ping.ActiveJobs))
	}
	if ping.Timestamp < before || ping.Timestamp > after {
		t.Errorf("Timestamp %d not in expected range", ping.Timestamp)
	}
}

func TestMessageFormat(t *testing.T) {
	// Verify the wire format matches the spec
	data, _ := Encode(TypeAuthOK, AuthOK{
		WorkerID:      "w_abc123",
		ServerVersion: "0.1.0",
	})

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Check top-level structure
	if raw["type"] != TypeAuthOK {
		t.Errorf("type = %v, want %q", raw["type"], TypeAuthOK)
	}
	payload, ok := raw["payload"].(map[string]any)
	if !ok {
		t.Fatal("payload is not an object")
	}
	if payload["worker_id"] != "w_abc123" {
		t.Errorf("worker_id = %v, want %q", payload["worker_id"], "w_abc123")
	}
	if payload["server_version"] != "0.1.0" {
		t.Errorf("server_version = %v, want %q", payload["server_version"], "0.1.0")
	}
}
