package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"

	"github.com/ehrlich-b/cinch/internal/worker"
)

// Server is the daemon's Unix socket server.
type Server struct {
	socketPath string
	worker     *worker.Worker
	log        *slog.Logger

	// Active client connections subscribed to job events
	mu      sync.RWMutex
	clients map[*clientConn]struct{}

	listener net.Listener
	ctx      context.Context
	cancel   context.CancelFunc
}

// clientConn represents a connected client.
type clientConn struct {
	conn        net.Conn
	jobID       string // which job to follow (empty = oldest)
	includeLogs bool   // whether to include log chunks
	eventChan   chan []byte
	done        chan struct{}
}

// NewServer creates a new daemon server.
func NewServer(socketPath string, w *worker.Worker, log *slog.Logger) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		socketPath: socketPath,
		worker:     w,
		log:        log,
		clients:    make(map[*clientConn]struct{}),
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start begins listening on the Unix socket.
func (s *Server) Start() error {
	// Remove existing socket file if it exists
	if _, err := os.Stat(s.socketPath); err == nil {
		if err := os.Remove(s.socketPath); err != nil {
			return fmt.Errorf("remove existing socket: %w", err)
		}
	}

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("listen on socket: %w", err)
	}
	s.listener = listener

	// Set socket permissions (readable/writable by owner only)
	if err := os.Chmod(s.socketPath, 0600); err != nil {
		s.log.Warn("failed to set socket permissions", "error", err)
	}

	s.log.Info("daemon listening", "socket", s.socketPath)

	go s.acceptLoop()
	return nil
}

// Stop shuts down the server.
func (s *Server) Stop() {
	s.cancel()
	if s.listener != nil {
		s.listener.Close()
	}

	// Close all client connections
	s.mu.Lock()
	for c := range s.clients {
		close(c.done)
		c.conn.Close()
	}
	s.clients = make(map[*clientConn]struct{})
	s.mu.Unlock()

	// Remove socket file
	os.Remove(s.socketPath)
}

// acceptLoop accepts new client connections.
func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				s.log.Warn("accept error", "error", err)
				continue
			}
		}

		go s.handleClient(conn)
	}
}

// handleClient handles a single client connection.
func (s *Server) handleClient(conn net.Conn) {
	client := &clientConn{
		conn:      conn,
		eventChan: make(chan []byte, 100),
		done:      make(chan struct{}),
	}

	// Read messages from client
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	for scanner.Scan() {
		select {
		case <-s.ctx.Done():
			return
		case <-client.done:
			return
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		msgType, payload, err := Decode(line)
		if err != nil {
			s.log.Warn("decode error", "error", err)
			continue
		}

		switch msgType {
		case TypeStatusRequest:
			s.handleStatusRequest(client)
		case TypeStreamRequest:
			s.handleStreamRequest(client, payload)
		case TypeStreamStop:
			s.handleStreamStop(client)
		}
	}

	// Client disconnected
	s.unsubscribe(client)
	conn.Close()
}

// handleStatusRequest returns the daemon's current status.
func (s *Server) handleStatusRequest(client *clientConn) {
	jobs := s.worker.GetRunningJobs()

	resp := StatusResponse{
		SlotsTotal:  s.worker.Concurrency(),
		SlotsBusy:   len(jobs),
		RunningJobs: make([]JobInfo, 0, len(jobs)),
	}

	for _, j := range jobs {
		resp.RunningJobs = append(resp.RunningJobs, JobInfo{
			JobID:     j.ID,
			Repo:      j.Repo,
			Branch:    j.Branch,
			Tag:       j.Tag,
			Commit:    j.Commit,
			Command:   j.Command,
			Mode:      j.Mode,
			Forge:     j.Forge,
			StartedAt: j.StartedAt.Unix(),
		})
	}

	s.sendToClient(client, TypeStatusResponse, resp)
}

// handleStreamRequest starts streaming events to the client.
func (s *Server) handleStreamRequest(client *clientConn, payload json.RawMessage) {
	var req StreamRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		s.sendError(client, "invalid stream request")
		return
	}

	client.jobID = req.JobID
	client.includeLogs = req.IncludeLogs

	s.subscribe(client)

	// Start goroutine to write events to client
	go s.writeEvents(client)
}

// handleStreamStop stops streaming events to the client.
func (s *Server) handleStreamStop(client *clientConn) {
	s.unsubscribe(client)
}

// subscribe adds a client to the subscribers list.
func (s *Server) subscribe(client *clientConn) {
	s.mu.Lock()
	s.clients[client] = struct{}{}
	s.mu.Unlock()
}

// unsubscribe removes a client from the subscribers list.
func (s *Server) unsubscribe(client *clientConn) {
	s.mu.Lock()
	if _, ok := s.clients[client]; ok {
		delete(s.clients, client)
		select {
		case <-client.done:
		default:
			close(client.done)
		}
	}
	s.mu.Unlock()
}

// writeEvents writes events from the channel to the client.
func (s *Server) writeEvents(client *clientConn) {
	for {
		select {
		case <-client.done:
			return
		case data := <-client.eventChan:
			if _, err := client.conn.Write(append(data, '\n')); err != nil {
				s.unsubscribe(client)
				return
			}
		}
	}
}

// sendToClient sends a message to a client.
func (s *Server) sendToClient(client *clientConn, msgType string, payload any) {
	data, err := Encode(msgType, payload)
	if err != nil {
		s.log.Warn("encode error", "error", err)
		return
	}
	if _, err := client.conn.Write(append(data, '\n')); err != nil {
		s.log.Warn("write error", "error", err)
	}
}

// sendError sends an error message to a client.
func (s *Server) sendError(client *clientConn, message string) {
	s.sendToClient(client, TypeError, Error{Message: message})
}

// BroadcastJobStarted broadcasts a job start event to all subscribers.
func (s *Server) BroadcastJobStarted(jobID, repo, branch, tag, commit, command, mode, forge string) {
	event := JobStarted{
		JobID:   jobID,
		Repo:    repo,
		Branch:  branch,
		Tag:     tag,
		Commit:  commit,
		Command: command,
		Mode:    mode,
		Forge:   forge,
	}

	s.broadcast(jobID, TypeJobStarted, event)
}

// BroadcastLogChunk broadcasts a log chunk to all subscribers.
func (s *Server) BroadcastLogChunk(jobID, stream string, data []byte) {
	event := LogChunk{
		JobID:  jobID,
		Stream: stream,
		Data:   string(data),
	}

	s.broadcastLogs(jobID, TypeLogChunk, event)
}

// BroadcastJobCompleted broadcasts a job completion event to all subscribers.
func (s *Server) BroadcastJobCompleted(jobID string, exitCode int, durationMs int64) {
	event := JobCompleted{
		JobID:      jobID,
		ExitCode:   exitCode,
		DurationMs: durationMs,
	}

	s.broadcast(jobID, TypeJobCompleted, event)
}

// broadcast sends an event to all clients subscribed to a job.
func (s *Server) broadcast(jobID string, msgType string, payload any) {
	data, err := Encode(msgType, payload)
	if err != nil {
		s.log.Warn("encode error", "error", err)
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	oldestJob := s.worker.OldestRunningJob()

	for client := range s.clients {
		// Check if client is interested in this job
		if client.jobID != "" && client.jobID != jobID {
			continue
		}
		// If no specific job requested, only send events for oldest job
		if client.jobID == "" && jobID != oldestJob {
			continue
		}

		select {
		case client.eventChan <- data:
		default:
			// Channel full, drop message
		}
	}
}

// broadcastLogs is like broadcast but only sends to clients that want logs.
func (s *Server) broadcastLogs(jobID string, msgType string, payload any) {
	data, err := Encode(msgType, payload)
	if err != nil {
		s.log.Warn("encode error", "error", err)
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	oldestJob := s.worker.OldestRunningJob()

	for client := range s.clients {
		// Check if client wants logs
		if !client.includeLogs {
			continue
		}
		// Check if client is interested in this job
		if client.jobID != "" && client.jobID != jobID {
			continue
		}
		// If no specific job requested, only send events for oldest job
		if client.jobID == "" && jobID != oldestJob {
			continue
		}

		select {
		case client.eventChan <- data:
		default:
			// Channel full, drop message
		}
	}
}
