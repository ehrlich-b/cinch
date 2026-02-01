package protocol

import (
	"encoding/json"
	"fmt"
	"time"
)

// Message types for server → worker communication
const (
	TypeAuthOK      = "AUTH_OK"
	TypeAuthFail    = "AUTH_FAIL"
	TypeRegistered  = "REGISTERED"
	TypeJobAssign   = "JOB_ASSIGN"
	TypeJobCancel   = "JOB_CANCEL"
	TypePong        = "PONG"
	TypeAck         = "ACK"
	TypeWorkerDrain = "WORKER_DRAIN" // Graceful shutdown
	TypeWorkerKill  = "WORKER_KILL"  // Force disconnect
)

// Message types for worker → server communication
const (
	TypeRegister     = "REGISTER"
	TypeJobAck       = "JOB_ACK"
	TypeJobReject    = "JOB_REJECT"
	TypeLogChunk     = "LOG_CHUNK"
	TypeJobStarted   = "JOB_STARTED"
	TypeJobComplete  = "JOB_COMPLETE"
	TypeJobError     = "JOB_ERROR"
	TypePing         = "PING"
	TypeStatusUpdate = "STATUS_UPDATE"
)

// Message types for relay communication (self-hosted servers ↔ cinch.sh)
const (
	TypeRelayRequest  = "RELAY_REQUEST"  // cinch.sh → self-hosted: forwarded webhook
	TypeRelayResponse = "RELAY_RESPONSE" // self-hosted → cinch.sh: webhook response
	TypeRelayReady    = "RELAY_READY"    // cinch.sh → self-hosted: connection confirmed
)

// Log stream types
const (
	StreamStdout = "stdout"
	StreamStderr = "stderr"
)

// Job error phases
const (
	PhaseClone   = "clone"
	PhaseSetup   = "setup"
	PhaseExecute = "execute"
	PhaseCleanup = "cleanup"
)

// Message is the envelope for all protocol messages.
type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Encode creates a Message with the given type and payload.
func Encode(msgType string, payload any) ([]byte, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
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

// --- Server → Worker Messages ---

// AuthOK is sent after successful WebSocket connection.
type AuthOK struct {
	WorkerID      string `json:"worker_id"`
	ServerVersion string `json:"server_version"`
}

// AuthFail is sent when token validation fails.
type AuthFail struct {
	Error string `json:"error"`
}

// Registered acknowledges worker registration.
type Registered struct {
	WorkerID string `json:"worker_id"`
}

// JobRepo contains repository info for a job.
type JobRepo struct {
	CloneURL   string `json:"clone_url"`
	CloneToken string `json:"clone_token,omitempty"`
	Commit     string `json:"commit"`
	Ref        string `json:"ref"`              // Full ref (refs/heads/main or refs/tags/v1.0.0)
	Branch     string `json:"branch,omitempty"` // Branch name (empty for tag pushes)
	Tag        string `json:"tag,omitempty"`    // Tag name (empty for branch pushes)
	ForgeType  string `json:"forge_type"`       // github, gitlab, forgejo, gitea
	IsPR       bool   `json:"is_pr"`
	PRNumber   int    `json:"pr_number,omitempty"`
}

// JobConfig contains the command and execution config.
type JobConfig struct {
	Command string            `json:"command"`
	Timeout string            `json:"timeout,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// JobAssign assigns a job to a worker.
type JobAssign struct {
	JobID  string    `json:"job_id"`
	Repo   JobRepo   `json:"repo"`
	Config JobConfig `json:"config"`
}

// JobCancel cancels a running job.
type JobCancel struct {
	JobID  string `json:"job_id"`
	Reason string `json:"reason,omitempty"`
}

// Pong is the response to a Ping.
type Pong struct {
	Timestamp int64 `json:"timestamp"`
}

// Ack is a generic acknowledgment.
type Ack struct {
	Ref string `json:"ref"`
}

// WorkerDrain requests graceful worker shutdown.
type WorkerDrain struct {
	Reason       string `json:"reason,omitempty"`
	DrainTimeout int    `json:"drain_timeout"` // Seconds to wait for jobs
}

// WorkerKill requests immediate worker disconnect.
type WorkerKill struct {
	Reason string `json:"reason,omitempty"`
}

// --- Worker → Server Messages ---

// Capabilities describes what a worker can do.
type Capabilities struct {
	Docker bool `json:"docker,omitempty"`
}

// WorkerMode determines which jobs a worker will accept.
type WorkerMode string

const (
	// ModePersonal (default) - only runs jobs authored by the worker's owner
	ModePersonal WorkerMode = "personal"
	// ModeShared - runs jobs from any collaborator, defers to author's personal worker if online
	ModeShared WorkerMode = "shared"
)

// Register is sent after AUTH_OK to register worker.
type Register struct {
	Labels       []string     `json:"labels,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
	Version      string       `json:"version"`
	Hostname     string       `json:"hostname,omitempty"`
	Concurrency  int          `json:"concurrency,omitempty"` // Max concurrent jobs (default 1)
	Mode         WorkerMode   `json:"mode,omitempty"`        // personal (default) or shared
	OwnerID      string       `json:"owner_id,omitempty"`    // User ID of the worker's owner
	OwnerName    string       `json:"owner_name,omitempty"`  // Username of the worker's owner
}

// JobAck acknowledges receipt of job assignment.
type JobAck struct {
	JobID string `json:"job_id"`
}

// JobReject is sent when worker rejects a job.
type JobReject struct {
	JobID  string `json:"job_id"`
	Reason string `json:"reason"`
}

// LogChunk streams log output from a running job.
type LogChunk struct {
	JobID     string `json:"job_id"`
	Timestamp int64  `json:"timestamp"`
	Stream    string `json:"stream"` // "stdout" or "stderr"
	Data      string `json:"data"`
}

// NewLogChunk creates a LogChunk with current timestamp.
func NewLogChunk(jobID, stream, data string) LogChunk {
	return LogChunk{
		JobID:     jobID,
		Timestamp: time.Now().Unix(),
		Stream:    stream,
		Data:      data,
	}
}

// JobStarted indicates job execution has begun.
type JobStarted struct {
	JobID     string `json:"job_id"`
	Timestamp int64  `json:"timestamp"`
}

// NewJobStarted creates a JobStarted with current timestamp.
func NewJobStarted(jobID string) JobStarted {
	return JobStarted{
		JobID:     jobID,
		Timestamp: time.Now().Unix(),
	}
}

// JobComplete indicates job finished.
type JobComplete struct {
	JobID      string `json:"job_id"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	Timestamp  int64  `json:"timestamp"`
}

// NewJobComplete creates a JobComplete with current timestamp.
func NewJobComplete(jobID string, exitCode int, duration time.Duration) JobComplete {
	return JobComplete{
		JobID:      jobID,
		ExitCode:   exitCode,
		DurationMs: duration.Milliseconds(),
		Timestamp:  time.Now().Unix(),
	}
}

// JobError indicates infrastructure failure (not command failure).
type JobError struct {
	JobID string `json:"job_id"`
	Error string `json:"error"`
	Phase string `json:"phase"` // "clone", "setup", "execute", "cleanup"
}

// Ping is a heartbeat from worker.
type Ping struct {
	Timestamp  int64    `json:"timestamp"`
	ActiveJobs []string `json:"active_jobs,omitempty"`
}

// NewPing creates a Ping with current timestamp.
func NewPing(activeJobs []string) Ping {
	return Ping{
		Timestamp:  time.Now().Unix(),
		ActiveJobs: activeJobs,
	}
}

// StatusUpdate reports worker's current status.
type StatusUpdate struct {
	ActiveJobs int     `json:"active_jobs"`
	MaxJobs    int     `json:"max_jobs"`
	Available  bool    `json:"available"`
	Load       float64 `json:"load,omitempty"`
}

// --- Relay Messages (for webhook forwarding to self-hosted servers) ---

// RelayRequest is sent from cinch.sh to a self-hosted server to forward a webhook.
type RelayRequest struct {
	RequestID string            `json:"request_id"`
	Method    string            `json:"method"`
	Path      string            `json:"path"`
	Headers   map[string]string `json:"headers"`
	Body      string            `json:"body"` // base64 encoded
}

// RelayResponse is sent from a self-hosted server back to cinch.sh with the webhook response.
type RelayResponse struct {
	RequestID  string            `json:"request_id"`
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"` // base64 encoded
}

// RelayReady is sent from cinch.sh to confirm the relay connection.
type RelayReady struct {
	RelayID  string `json:"relay_id"`  // Unique ID for this relay (used in webhook URLs)
	RelayURL string `json:"relay_url"` // Full URL for webhooks (e.g., https://cinch.sh/relay/x7k9m/webhooks/github)
}
