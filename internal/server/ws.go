package server

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ehrlich-b/cinch/internal/logstore"
	"github.com/ehrlich-b/cinch/internal/protocol"
	"github.com/ehrlich-b/cinch/internal/storage"
	"github.com/ehrlich-b/cinch/internal/version"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/sha3"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 90 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = 30 * time.Second

	// Maximum message size allowed from peer.
	maxMessageSize = 1 << 20 // 1MB
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for workers
	},
}

// StatusPoster posts job status to forges.
type StatusPoster interface {
	PostJobStatus(ctx context.Context, jobID string, state string, description string) error
}

// LogBroadcaster broadcasts logs and job completion to UI clients.
type LogBroadcaster interface {
	BroadcastLog(jobID, stream, data string)
	BroadcastJobComplete(jobID string, status string, exitCode *int)
}

// JWTValidator validates JWT tokens.
type JWTValidator interface {
	ValidateUserToken(tokenString string) string
}

// WorkerAvailableNotifier is called when a worker becomes available.
type WorkerAvailableNotifier interface {
	NotifyWorkerAvailable()
	Requeue(jobID string)
	CompleteJob(jobID string)
	RequeueWorkerJobs(jobIDs []string) // re-queue all jobs when worker disconnects
}

// WSHandler handles WebSocket connections from workers.
type WSHandler struct {
	hub            *Hub
	storage        storage.Storage
	logStore       logstore.LogStore
	log            *slog.Logger
	statusPoster   StatusPoster
	logBroadcaster LogBroadcaster
	jwtValidator   JWTValidator
	githubApp      *GitHubAppHandler
	workerNotifier WorkerAvailableNotifier
}

// NewWSHandler creates a new WebSocket handler.
func NewWSHandler(hub *Hub, store storage.Storage, log *slog.Logger) *WSHandler {
	if log == nil {
		log = slog.Default()
	}
	return &WSHandler{
		hub:     hub,
		storage: store,
		log:     log,
	}
}

// SetStatusPoster sets the status poster for reporting job status to forges.
func (h *WSHandler) SetStatusPoster(sp StatusPoster) {
	h.statusPoster = sp
}

// SetLogBroadcaster sets the log broadcaster for streaming logs to UI clients.
func (h *WSHandler) SetLogBroadcaster(lb LogBroadcaster) {
	h.logBroadcaster = lb
}

// SetJWTValidator sets the JWT validator for user token authentication.
func (h *WSHandler) SetJWTValidator(v JWTValidator) {
	h.jwtValidator = v
}

// SetGitHubApp sets the GitHub App handler for status posting.
func (h *WSHandler) SetGitHubApp(app *GitHubAppHandler) {
	h.githubApp = app
}

// SetWorkerNotifier sets the notifier to call when a worker becomes available.
func (h *WSHandler) SetWorkerNotifier(n WorkerAvailableNotifier) {
	h.workerNotifier = n
}

// SetLogStore sets the log store for persisting job logs.
func (h *WSHandler) SetLogStore(ls logstore.LogStore) {
	h.logStore = ls
}

// ServeHTTP handles WebSocket upgrade requests.
func (h *WSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	// Validate token
	ctx := r.Context()
	workerID, err := h.validateToken(ctx, token)
	if err != nil {
		h.log.Warn("token validation failed", "error", err)
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	// Upgrade connection
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Error("websocket upgrade failed", "error", err)
		return
	}

	// Create worker connection
	worker := &WorkerConn{
		ID:         workerID,
		Send:       make(chan []byte, 256),
		ActiveJobs: []string{},
		LastPing:   time.Now(),
	}

	h.log.Info("worker connected", "worker_id", workerID)

	// Send AUTH_OK
	authOK, err := protocol.Encode(protocol.TypeAuthOK, protocol.AuthOK{
		WorkerID:      workerID,
		ServerVersion: version.Version,
	})
	if err != nil {
		h.log.Error("failed to encode AUTH_OK", "error", err)
		conn.Close()
		return
	}
	if err := conn.WriteMessage(websocket.TextMessage, authOK); err != nil {
		h.log.Error("failed to send AUTH_OK", "error", err)
		conn.Close()
		return
	}

	// Start read/write pumps
	go h.writePump(conn, worker)
	go h.readPump(conn, worker)
}

// validateToken checks the token and returns the worker ID.
func (h *WSHandler) validateToken(ctx context.Context, token string) (string, error) {
	// First try database token lookup (for worker tokens)
	hash := hashToken(token)
	tok, err := h.storage.GetTokenByHash(ctx, hash)
	if err == nil {
		// Return worker ID if bound, or token ID as worker ID
		if tok.WorkerID != nil {
			return *tok.WorkerID, nil
		}
		return tok.ID, nil
	}

	// If database lookup failed and we have a JWT validator, try JWT
	if h.jwtValidator != nil {
		if email := h.jwtValidator.ValidateUserToken(token); email != "" {
			// Use email as worker ID prefix for user tokens
			return "user:" + email, nil
		}
	}

	return "", err
}

// hashToken creates a SHA3-256 hash of the token.
func hashToken(token string) string {
	h := sha3.New256()
	h.Write([]byte(token))
	return hex.EncodeToString(h.Sum(nil))
}

// TokensEqual compares two tokens in constant time.
func TokensEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// readPump pumps messages from the WebSocket to the hub.
func (h *WSHandler) readPump(conn *websocket.Conn, worker *WorkerConn) {
	defer func() {
		// Re-queue any active jobs before unregistering
		if h.workerNotifier != nil && len(worker.ActiveJobs) > 0 {
			h.workerNotifier.RequeueWorkerJobs(worker.ActiveJobs)
		}
		h.hub.Unregister(worker.ID)
		conn.Close()
		h.log.Info("worker disconnected", "worker_id", worker.ID)
	}()

	conn.SetReadLimit(maxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				h.log.Warn("websocket read error", "worker_id", worker.ID, "error", err)
			}
			return
		}

		h.handleMessage(worker, message)
	}
}

// writePump pumps messages from the hub to the WebSocket.
func (h *WSHandler) writePump(conn *websocket.Conn, worker *WorkerConn) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		conn.Close()
	}()

	for {
		select {
		case message, ok := <-worker.Send:
			if err := conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if !ok {
				// Channel closed
				_ = conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
				h.log.Warn("websocket write error", "worker_id", worker.ID, "error", err)
				return
			}

		case <-ticker.C:
			if err := conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage processes incoming messages from a worker.
func (h *WSHandler) handleMessage(worker *WorkerConn, data []byte) {
	msgType, payload, err := protocol.Decode(data)
	if err != nil {
		h.log.Warn("failed to decode message", "worker_id", worker.ID, "error", err)
		return
	}

	switch msgType {
	case protocol.TypeRegister:
		h.handleRegister(worker, payload)
	case protocol.TypePing:
		h.handlePing(worker, payload)
	case protocol.TypeJobAck:
		h.handleJobAck(worker, payload)
	case protocol.TypeJobReject:
		h.handleJobReject(worker, payload)
	case protocol.TypeJobStarted:
		h.handleJobStarted(worker, payload)
	case protocol.TypeLogChunk:
		h.handleLogChunk(worker, payload)
	case protocol.TypeJobComplete:
		h.handleJobComplete(worker, payload)
	case protocol.TypeJobError:
		h.handleJobError(worker, payload)
	default:
		h.log.Warn("unknown message type", "worker_id", worker.ID, "type", msgType)
	}
}

// handleRegister processes worker registration.
func (h *WSHandler) handleRegister(worker *WorkerConn, payload []byte) {
	reg, err := protocol.DecodePayload[protocol.Register](payload)
	if err != nil {
		h.log.Warn("failed to decode REGISTER", "worker_id", worker.ID, "error", err)
		return
	}

	worker.Labels = reg.Labels
	worker.Capabilities = Capabilities{
		Docker: reg.Capabilities.Docker,
	}
	worker.Hostname = reg.Hostname
	worker.Version = reg.Version

	// For user-authenticated workers, include hostname in ID to allow multiple machines
	// Worker IDs like "user:email" become "user:email:hostname"
	if len(worker.ID) > 5 && worker.ID[:5] == "user:" && worker.Hostname != "" {
		worker.ID = worker.ID + ":" + worker.Hostname
	}

	// Set worker mode (default to personal if not specified)
	worker.Mode = reg.Mode
	if worker.Mode == "" {
		worker.Mode = protocol.ModePersonal
	}

	// Set owner info from registration or extract from worker ID
	worker.OwnerID = reg.OwnerID
	worker.OwnerName = reg.OwnerName

	// If owner info not in registration, try to extract from worker ID
	// Worker IDs for JWT-authenticated workers are "user:email:hostname"
	if worker.OwnerName == "" && len(worker.ID) > 5 && worker.ID[:5] == "user:" {
		// Extract email from "user:email:hostname" format
		rest := worker.ID[5:]
		if idx := strings.LastIndex(rest, ":"); idx > 0 {
			worker.OwnerName = rest[:idx] // Get email part before hostname
		} else {
			worker.OwnerName = rest // Fallback for old format without hostname
		}
	}

	// Ensure worker exists in database (for FK constraint)
	ctx := context.Background()
	dbWorker, err := h.storage.GetWorker(ctx, worker.ID)
	if err != nil {
		// Create worker record with owner info
		dbWorker = &storage.Worker{
			ID:        worker.ID,
			Name:      worker.Hostname,
			Labels:    worker.Labels,
			Status:    storage.WorkerStatusOnline,
			LastSeen:  time.Now(),
			CreatedAt: time.Now(),
			OwnerName: worker.OwnerName,
			Mode:      string(worker.Mode),
		}
		if createErr := h.storage.CreateWorker(ctx, dbWorker); createErr != nil {
			h.log.Warn("failed to create worker record", "worker_id", worker.ID, "error", createErr)
			// Continue anyway - hub registration still works
		}
	} else {
		// Update existing worker status and owner info (in case mode changed)
		_ = h.storage.UpdateWorkerStatus(ctx, worker.ID, storage.WorkerStatusOnline)
		_ = h.storage.UpdateWorkerLastSeen(ctx, worker.ID)
		_ = h.storage.UpdateWorkerOwner(ctx, worker.ID, worker.OwnerName, string(worker.Mode))
	}

	// Use database name for display (preserves original name from first registration)
	worker.Name = dbWorker.Name

	// Register with hub
	h.hub.Register(worker)

	h.log.Info("worker registered",
		"worker_id", worker.ID,
		"labels", worker.Labels,
		"hostname", worker.Hostname,
		"mode", worker.Mode,
		"owner", worker.OwnerName,
	)

	// Send REGISTERED
	msg, err := protocol.Encode(protocol.TypeRegistered, protocol.Registered{
		WorkerID: worker.ID,
	})
	if err != nil {
		h.log.Error("failed to encode REGISTERED", "error", err)
		return
	}
	worker.Send <- msg

	// Notify dispatcher that a worker is available for queued jobs
	if h.workerNotifier != nil {
		h.workerNotifier.NotifyWorkerAvailable()
	}
}

// handlePing processes heartbeat from worker.
func (h *WSHandler) handlePing(worker *WorkerConn, payload []byte) {
	ping, err := protocol.DecodePayload[protocol.Ping](payload)
	if err != nil {
		h.log.Warn("failed to decode PING", "worker_id", worker.ID, "error", err)
		return
	}

	h.hub.UpdateLastPing(worker.ID, ping.ActiveJobs)

	// Send PONG
	msg, err := protocol.Encode(protocol.TypePong, protocol.Pong{
		Timestamp: time.Now().Unix(),
	})
	if err != nil {
		h.log.Error("failed to encode PONG", "error", err)
		return
	}
	worker.Send <- msg
}

// handleJobAck processes job acknowledgment.
func (h *WSHandler) handleJobAck(worker *WorkerConn, payload []byte) {
	ack, err := protocol.DecodePayload[protocol.JobAck](payload)
	if err != nil {
		h.log.Warn("failed to decode JOB_ACK", "worker_id", worker.ID, "error", err)
		return
	}

	// Job already tracked in ActiveJobs when dispatched - ACK is just confirmation
	h.log.Debug("job acknowledged", "worker_id", worker.ID, "job_id", ack.JobID)
}

// handleJobReject processes job rejection.
func (h *WSHandler) handleJobReject(worker *WorkerConn, payload []byte) {
	reject, err := protocol.DecodePayload[protocol.JobReject](payload)
	if err != nil {
		h.log.Warn("failed to decode JOB_REJECT", "worker_id", worker.ID, "error", err)
		return
	}

	h.log.Warn("job rejected", "worker_id", worker.ID, "job_id", reject.JobID, "reason", reject.Reason)

	// Re-queue for another worker
	if h.workerNotifier != nil {
		h.workerNotifier.Requeue(reject.JobID)
	}
}

// handleJobStarted processes job start notification.
func (h *WSHandler) handleJobStarted(worker *WorkerConn, payload []byte) {
	started, err := protocol.DecodePayload[protocol.JobStarted](payload)
	if err != nil {
		h.log.Warn("failed to decode JOB_STARTED", "worker_id", worker.ID, "error", err)
		return
	}

	ctx := context.Background()
	if err := h.storage.UpdateJobStatus(ctx, started.JobID, storage.JobStatusRunning, nil); err != nil {
		h.log.Error("failed to update job status", "job_id", started.JobID, "error", err)
	}
	h.log.Info("job started", "worker_id", worker.ID, "job_id", started.JobID)
}

// handleLogChunk processes log output from worker.
func (h *WSHandler) handleLogChunk(worker *WorkerConn, payload []byte) {
	chunk, err := protocol.DecodePayload[protocol.LogChunk](payload)
	if err != nil {
		h.log.Warn("failed to decode LOG_CHUNK", "worker_id", worker.ID, "error", err)
		return
	}

	ctx := context.Background()
	if h.logStore != nil {
		if err := h.logStore.AppendChunk(ctx, chunk.JobID, chunk.Stream, []byte(chunk.Data)); err != nil {
			h.log.Error("failed to append log", "job_id", chunk.JobID, "error", err)
		}
	}

	// Broadcast to UI clients
	if h.logBroadcaster != nil {
		h.logBroadcaster.BroadcastLog(chunk.JobID, chunk.Stream, chunk.Data)
	}
}

// handleJobComplete processes job completion.
func (h *WSHandler) handleJobComplete(worker *WorkerConn, payload []byte) {
	complete, err := protocol.DecodePayload[protocol.JobComplete](payload)
	if err != nil {
		h.log.Warn("failed to decode JOB_COMPLETE", "worker_id", worker.ID, "error", err)
		return
	}

	ctx := context.Background()

	// Determine status based on exit code
	status := storage.JobStatusSuccess
	forgeState := "success"
	description := formatDuration(complete.DurationMs)
	if complete.ExitCode != 0 {
		status = storage.JobStatusFailed
		forgeState = "failure"
		description = "Build failed - " + description
	} else {
		description = "Build passed - " + description
	}

	exitCode := complete.ExitCode
	if err := h.storage.UpdateJobStatus(ctx, complete.JobID, status, &exitCode); err != nil {
		h.log.Error("failed to update job status", "job_id", complete.JobID, "error", err)
	}

	// Post status to forge
	if h.statusPoster != nil {
		if err := h.statusPoster.PostJobStatus(ctx, complete.JobID, forgeState, description); err != nil {
			h.log.Warn("failed to post status to forge", "job_id", complete.JobID, "error", err)
		}
	}

	// Finalize logs (flush buffers, concatenate chunks)
	if h.logStore != nil {
		if err := h.logStore.Finalize(ctx, complete.JobID); err != nil {
			h.log.Warn("failed to finalize logs", "job_id", complete.JobID, "error", err)
		}
	}

	// Broadcast to UI clients
	if h.logBroadcaster != nil {
		h.logBroadcaster.BroadcastJobComplete(complete.JobID, string(status), &exitCode)
	}

	h.hub.RemoveActiveJob(worker.ID, complete.JobID)
	if h.workerNotifier != nil {
		h.workerNotifier.CompleteJob(complete.JobID)
	}
	h.log.Info("job completed",
		"worker_id", worker.ID,
		"job_id", complete.JobID,
		"exit_code", complete.ExitCode,
		"duration_ms", complete.DurationMs,
	)

	// Send ACK
	msg, err := protocol.Encode(protocol.TypeAck, protocol.Ack{
		Ref: complete.JobID,
	})
	if err != nil {
		h.log.Error("failed to encode ACK", "error", err)
		return
	}
	worker.Send <- msg
}

func formatDuration(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	if d < time.Minute {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(time.Second).String()
}

// handleJobError processes job error.
func (h *WSHandler) handleJobError(worker *WorkerConn, payload []byte) {
	jobErr, err := protocol.DecodePayload[protocol.JobError](payload)
	if err != nil {
		h.log.Warn("failed to decode JOB_ERROR", "worker_id", worker.ID, "error", err)
		return
	}

	ctx := context.Background()
	if err := h.storage.UpdateJobStatus(ctx, jobErr.JobID, storage.JobStatusError, nil); err != nil {
		h.log.Error("failed to update job status", "job_id", jobErr.JobID, "error", err)
	}

	// Post error status to forge
	if h.statusPoster != nil {
		description := "Build error: " + jobErr.Error
		if jobErr.Phase != "" {
			description = "Build error in " + jobErr.Phase + ": " + jobErr.Error
		}
		if err := h.statusPoster.PostJobStatus(ctx, jobErr.JobID, "error", description); err != nil {
			h.log.Warn("failed to post status to forge", "job_id", jobErr.JobID, "error", err)
		}
	}

	// Finalize logs (flush buffers)
	if h.logStore != nil {
		if err := h.logStore.Finalize(ctx, jobErr.JobID); err != nil {
			h.log.Warn("failed to finalize logs", "job_id", jobErr.JobID, "error", err)
		}
	}

	// Broadcast to UI clients
	if h.logBroadcaster != nil {
		h.logBroadcaster.BroadcastJobComplete(jobErr.JobID, string(storage.JobStatusError), nil)
	}

	h.hub.RemoveActiveJob(worker.ID, jobErr.JobID)
	if h.workerNotifier != nil {
		h.workerNotifier.CompleteJob(jobErr.JobID)
	}
	h.log.Error("job error",
		"worker_id", worker.ID,
		"job_id", jobErr.JobID,
		"phase", jobErr.Phase,
		"error", jobErr.Error,
	)

	// Send ACK
	msg, err := protocol.Encode(protocol.TypeAck, protocol.Ack{
		Ref: jobErr.JobID,
	})
	if err != nil {
		h.log.Error("failed to encode ACK", "error", err)
		return
	}
	worker.Send <- msg
}

// SendJob sends a job assignment to a worker.
func (h *WSHandler) SendJob(workerID string, job protocol.JobAssign) error {
	worker := h.hub.Get(workerID)
	if worker == nil {
		return ErrWorkerNotFound
	}

	msg, err := protocol.Encode(protocol.TypeJobAssign, job)
	if err != nil {
		return err
	}

	worker.Send <- msg
	return nil
}

// CancelJob sends a cancellation to a worker.
func (h *WSHandler) CancelJob(workerID string, cancel protocol.JobCancel) error {
	worker := h.hub.Get(workerID)
	if worker == nil {
		return ErrWorkerNotFound
	}

	msg, err := protocol.Encode(protocol.TypeJobCancel, cancel)
	if err != nil {
		return err
	}

	worker.Send <- msg
	return nil
}

// SendDrain sends a drain request to a worker (graceful shutdown).
func (h *WSHandler) SendDrain(workerID string, timeout int, reason string) error {
	worker := h.hub.Get(workerID)
	if worker == nil {
		return ErrWorkerNotFound
	}

	msg, err := protocol.Encode(protocol.TypeWorkerDrain, protocol.WorkerDrain{
		Reason:       reason,
		DrainTimeout: timeout,
	})
	if err != nil {
		return err
	}

	worker.Send <- msg
	return nil
}

// SendKill sends a kill request to a worker (force disconnect).
func (h *WSHandler) SendKill(workerID string, reason string) error {
	worker := h.hub.Get(workerID)
	if worker == nil {
		return ErrWorkerNotFound
	}

	msg, err := protocol.Encode(protocol.TypeWorkerKill, protocol.WorkerKill{
		Reason: reason,
	})
	if err != nil {
		return err
	}

	worker.Send <- msg
	return nil
}
