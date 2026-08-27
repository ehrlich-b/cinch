package daemon

import (
	"encoding/json"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		msgType string
		payload any
	}{
		{
			name:    "StatusRequest",
			msgType: TypeStatusRequest,
			payload: StatusRequest{},
		},
		{
			name:    "StatusResponse",
			msgType: TypeStatusResponse,
			payload: StatusResponse{
				SlotsTotal: 4,
				SlotsBusy:  2,
				RunningJobs: []JobInfo{
					{JobID: "j_slow", Command: "make build", StartedAt: 1705312800},
				},
			},
		},
		{
			name:    "StreamRequest",
			msgType: TypeStreamRequest,
			payload: StreamRequest{JobID: "j_1", IncludeLogs: true},
		},
		{
			name:    "StreamStop",
			msgType: TypeStreamStop,
			payload: StreamStop{},
		},
		{
			name:    "JobStarted",
			msgType: TypeJobStarted,
			payload: JobStarted{
				JobID:   "j_1",
				Repo:    "https://github.com/acme/app.git",
				Branch:  "main",
				Commit:  "deadbeef",
				Command: "make build",
				Mode:    "container",
				Forge:   "github",
			},
		},
		{
			name:    "LogChunk",
			msgType: TypeLogChunk,
			payload: LogChunk{JobID: "j_1", Stream: "stdout", Data: "compiling...\n"},
		},
		{
			name:    "JobCompleted",
			msgType: TypeJobCompleted,
			payload: JobCompleted{JobID: "j_1", ExitCode: 0, DurationMs: 5230},
		},
		{
			name:    "Error",
			msgType: TypeError,
			payload: Error{Message: "worker offline"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Encode(tt.msgType, tt.payload)
			if err != nil {
				t.Fatalf("Encode failed: %v", err)
			}

			var msg Message
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("Invalid JSON: %v", err)
			}

			gotType, gotPayload, err := Decode(data)
			if err != nil {
				t.Fatalf("Decode failed: %v", err)
			}

			if gotType != tt.msgType {
				t.Errorf("type = %q, want %q", gotType, tt.msgType)
			}

			if len(gotPayload) == 0 {
				t.Error("payload is empty")
			}
		})
	}
}

func TestEncodeNilPayloadOmitted(t *testing.T) {
	data, err := Encode(TypeStatusRequest, nil)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	if len(msg.Payload) != 0 {
		t.Errorf("payload = %q, want empty/absent", msg.Payload)
	}

	gotType, gotPayload, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if gotType != TypeStatusRequest {
		t.Errorf("type = %q, want %q", gotType, TypeStatusRequest)
	}
	if len(gotPayload) != 0 {
		t.Errorf("decoded payload = %q, want empty", gotPayload)
	}
}

func TestEncodeEmptyStructPayloadNotEmpty(t *testing.T) {
	data, err := Encode(TypeStatusRequest, StatusRequest{})
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	if string(msg.Payload) != "{}" {
		t.Errorf("payload = %q, want %q", msg.Payload, "{}")
	}
}

func TestDecodePayload(t *testing.T) {
	original := StatusResponse{
		SlotsTotal: 8,
		SlotsBusy:  3,
		RunningJobs: []JobInfo{
			{
				JobID:     "j_build",
				Repo:      "https://github.com/acme/app.git",
				Branch:    "main",
				Commit:    "deadbeef",
				Command:   "make build",
				Mode:      "container",
				Forge:     "github",
				StartedAt: 1705312800,
			},
			{
				JobID:     "j_release",
				Tag:       "v1.2.3",
				Commit:    "cafebabe",
				Command:   "make release",
				Forge:     "gitlab",
				StartedAt: 1705312900,
			},
		},
	}

	data, err := Encode(TypeStatusResponse, original)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	msgType, payload, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if msgType != TypeStatusResponse {
		t.Fatalf("type = %q, want %q", msgType, TypeStatusResponse)
	}

	got, err := DecodePayload[StatusResponse](payload)
	if err != nil {
		t.Fatalf("DecodePayload failed: %v", err)
	}

	if got.SlotsTotal != original.SlotsTotal {
		t.Errorf("SlotsTotal = %d, want %d", got.SlotsTotal, original.SlotsTotal)
	}
	if got.SlotsBusy != original.SlotsBusy {
		t.Errorf("SlotsBusy = %d, want %d", got.SlotsBusy, original.SlotsBusy)
	}
	if len(got.RunningJobs) != len(original.RunningJobs) {
		t.Fatalf("RunningJobs len = %d, want %d", len(got.RunningJobs), len(original.RunningJobs))
	}

	gotFirst := got.RunningJobs[0]
	if gotFirst.JobID != original.RunningJobs[0].JobID {
		t.Errorf("RunningJobs[0].JobID = %q, want %q", gotFirst.JobID, original.RunningJobs[0].JobID)
	}
	if gotFirst.Repo != original.RunningJobs[0].Repo {
		t.Errorf("RunningJobs[0].Repo = %q, want %q", gotFirst.Repo, original.RunningJobs[0].Repo)
	}
	if gotFirst.Branch != original.RunningJobs[0].Branch {
		t.Errorf("RunningJobs[0].Branch = %q, want %q", gotFirst.Branch, original.RunningJobs[0].Branch)
	}
	if gotFirst.Commit != original.RunningJobs[0].Commit {
		t.Errorf("RunningJobs[0].Commit = %q, want %q", gotFirst.Commit, original.RunningJobs[0].Commit)
	}
	if gotFirst.Command != original.RunningJobs[0].Command {
		t.Errorf("RunningJobs[0].Command = %q, want %q", gotFirst.Command, original.RunningJobs[0].Command)
	}
	if gotFirst.Mode != original.RunningJobs[0].Mode {
		t.Errorf("RunningJobs[0].Mode = %q, want %q", gotFirst.Mode, original.RunningJobs[0].Mode)
	}
	if gotFirst.Forge != original.RunningJobs[0].Forge {
		t.Errorf("RunningJobs[0].Forge = %q, want %q", gotFirst.Forge, original.RunningJobs[0].Forge)
	}
	if gotFirst.StartedAt != original.RunningJobs[0].StartedAt {
		t.Errorf("RunningJobs[0].StartedAt = %d, want %d", gotFirst.StartedAt, original.RunningJobs[0].StartedAt)
	}

	gotSecond := got.RunningJobs[1]
	if gotSecond.JobID != original.RunningJobs[1].JobID {
		t.Errorf("RunningJobs[1].JobID = %q, want %q", gotSecond.JobID, original.RunningJobs[1].JobID)
	}
	if gotSecond.Tag != original.RunningJobs[1].Tag {
		t.Errorf("RunningJobs[1].Tag = %q, want %q", gotSecond.Tag, original.RunningJobs[1].Tag)
	}
	if gotSecond.Commit != original.RunningJobs[1].Commit {
		t.Errorf("RunningJobs[1].Commit = %q, want %q", gotSecond.Commit, original.RunningJobs[1].Commit)
	}
	if gotSecond.Forge != original.RunningJobs[1].Forge {
		t.Errorf("RunningJobs[1].Forge = %q, want %q", gotSecond.Forge, original.RunningJobs[1].Forge)
	}
}

func TestDecodeInvalidJSON(t *testing.T) {
	_, _, err := Decode([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestDecodePayloadTypeMismatch(t *testing.T) {
	data, _ := Encode(TypeStatusRequest, StatusRequest{})
	_, payload, _ := Decode(data)

	got, err := DecodePayload[JobCompleted](payload)
	if err != nil {
		return
	}
	if got.JobID != "" {
		t.Error("expected empty JobID for type mismatch")
	}
	if got.ExitCode != 0 {
		t.Error("expected zero ExitCode for type mismatch")
	}
	if got.DurationMs != 0 {
		t.Error("expected zero DurationMs for type mismatch")
	}
}

func TestMessageFormat(t *testing.T) {
	data, _ := Encode(TypeJobStarted, JobStarted{
		JobID:   "j_abc123",
		Commit:  "c0ffee42",
		Command: "make build",
	})

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if raw["type"] != TypeJobStarted {
		t.Errorf("type = %v, want %q", raw["type"], TypeJobStarted)
	}
	payload, ok := raw["payload"].(map[string]any)
	if !ok {
		t.Fatal("payload is not an object")
	}
	if payload["job_id"] != "j_abc123" {
		t.Errorf("job_id = %v, want %q", payload["job_id"], "j_abc123")
	}
	if payload["commit"] != "c0ffee42" {
		t.Errorf("commit = %v, want %q", payload["commit"], "c0ffee42")
	}
}
