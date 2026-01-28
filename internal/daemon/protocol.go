package daemon

import (
	"encoding/json"
	"fmt"
)

// Message types for daemon RPC
const (
	TypeStatusRequest  = "STATUS_REQUEST"
	TypeStatusResponse = "STATUS_RESPONSE"
	TypeStreamRequest  = "STREAM_REQUEST"
	TypeStreamStop     = "STREAM_STOP"
	TypeJobStarted     = "JOB_STARTED"
	TypeLogChunk       = "LOG_CHUNK"
	TypeJobCompleted   = "JOB_COMPLETED"
	TypeError          = "ERROR"
)

// Message is the envelope for all daemon messages.
type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Encode creates a Message with the given type and payload.
func Encode(msgType string, payload any) ([]byte, error) {
	var payloadBytes []byte
	var err error

	if payload != nil {
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}
	}

	msg := Message{
		Type:    msgType,
		Payload: payloadBytes,
	}
	return json.Marshal(msg)
}

// Decode parses a raw message and returns the type and payload.
func Decode(data []byte) (msgType string, payload json.RawMessage, err error) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return "", nil, fmt.Errorf("unmarshal message: %w", err)
	}
	return msg.Type, msg.Payload, nil
}

// DecodePayload unmarshals the payload into the given type.
func DecodePayload[T any](payload json.RawMessage) (T, error) {
	var v T
	if err := json.Unmarshal(payload, &v); err != nil {
		return v, fmt.Errorf("unmarshal payload: %w", err)
	}
	return v, nil
}

// StatusRequest requests the daemon's current status.
type StatusRequest struct{}

// StatusResponse returns the daemon's current status.
type StatusResponse struct {
	SlotsTotal  int       `json:"slots_total"`
	SlotsBusy   int       `json:"slots_busy"`
	RunningJobs []JobInfo `json:"running_jobs"`
}

// JobInfo contains information about a running job.
type JobInfo struct {
	JobID     string `json:"job_id"`
	Repo      string `json:"repo"`
	Branch    string `json:"branch,omitempty"`
	Tag       string `json:"tag,omitempty"`
	Commit    string `json:"commit"`
	Command   string `json:"command,omitempty"`
	Mode      string `json:"mode,omitempty"`
	Forge     string `json:"forge,omitempty"`
	StartedAt int64  `json:"started_at"`
}

// StreamRequest requests to stream events for a job.
type StreamRequest struct {
	JobID       string `json:"job_id,omitempty"` // empty = oldest running job
	IncludeLogs bool   `json:"include_logs"`     // include log chunks (-v flag)
}

// StreamStop requests to stop streaming.
type StreamStop struct{}

// JobStarted event when a job starts.
type JobStarted struct {
	JobID   string `json:"job_id"`
	Repo    string `json:"repo"`
	Branch  string `json:"branch,omitempty"`
	Tag     string `json:"tag,omitempty"`
	Commit  string `json:"commit"`
	Command string `json:"command"`
	Mode    string `json:"mode"`
	Forge   string `json:"forge"`
}

// LogChunk event for log output.
type LogChunk struct {
	JobID  string `json:"job_id"`
	Stream string `json:"stream"` // "stdout" or "stderr"
	Data   string `json:"data"`
}

// JobCompleted event when a job finishes.
type JobCompleted struct {
	JobID      string `json:"job_id"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
}

// Error is sent when an error occurs.
type Error struct {
	Message string `json:"message"`
}
