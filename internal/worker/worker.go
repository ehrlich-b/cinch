package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/ehrlich-b/cinch/internal/config"
	"github.com/ehrlich-b/cinch/internal/protocol"
	"github.com/ehrlich-b/cinch/internal/worker/container"
	"github.com/gorilla/websocket"
)

const (
	// Worker version
	workerVersion = "0.1.0"

	// Ping interval
	pingInterval = 30 * time.Second

	// Reconnect backoff
	minReconnectDelay = 1 * time.Second
	maxReconnectDelay = 60 * time.Second
)

// WorkerConfig holds configuration for a worker.
type WorkerConfig struct {
	ServerURL   string
	Token       string
	Labels      []string
	Concurrency int
	Docker      bool
	Hostname    string
}

// Worker connects to the server and executes jobs.
type Worker struct {
	config WorkerConfig
	log    *slog.Logger

	// Connection
	conn     *websocket.Conn
	connLock sync.Mutex
	workerID string

	// Active jobs
	jobsLock   sync.Mutex
	activeJobs map[string]context.CancelFunc

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Callbacks
	OnJobStart    func(jobID string)
	OnJobComplete func(jobID string, exitCode int, duration time.Duration)
	OnJobError    func(jobID string, phase, err string)
}

// NewWorker creates a new worker.
func NewWorker(cfg WorkerConfig, log *slog.Logger) *Worker {
	if log == nil {
		log = slog.Default()
	}
	if cfg.Hostname == "" {
		cfg.Hostname, _ = os.Hostname()
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Worker{
		config:     cfg,
		log:        log,
		activeJobs: make(map[string]context.CancelFunc),
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start connects to the server and begins processing jobs.
func (w *Worker) Start() error {
	if err := w.connect(); err != nil {
		return fmt.Errorf("initial connect: %w", err)
	}

	w.wg.Add(2)
	go w.readLoop()
	go w.pingLoop()

	return nil
}

// Stop gracefully shuts down the worker.
func (w *Worker) Stop() {
	w.cancel()

	// Wait for active jobs to complete (with timeout)
	done := make(chan struct{})
	go func() {
		for {
			w.jobsLock.Lock()
			count := len(w.activeJobs)
			w.jobsLock.Unlock()
			if count == 0 {
				close(done)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		w.log.Warn("timeout waiting for jobs to complete")
	}

	w.connLock.Lock()
	if w.conn != nil {
		w.conn.Close()
	}
	w.connLock.Unlock()

	w.wg.Wait()
}

// connect establishes WebSocket connection to server.
func (w *Worker) connect() error {
	u, err := url.Parse(w.config.ServerURL)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}

	// Add token to query
	q := u.Query()
	q.Set("token", w.config.Token)
	u.RawQuery = q.Encode()

	// Connect
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	w.connLock.Lock()
	w.conn = conn
	w.connLock.Unlock()

	// Wait for AUTH_OK
	_, msg, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return fmt.Errorf("read auth response: %w", err)
	}

	msgType, payload, err := protocol.Decode(msg)
	if err != nil {
		conn.Close()
		return fmt.Errorf("decode auth response: %w", err)
	}

	if msgType == protocol.TypeAuthFail {
		authFail, _ := protocol.DecodePayload[protocol.AuthFail](payload)
		conn.Close()
		return fmt.Errorf("auth failed: %s", authFail.Error)
	}

	if msgType != protocol.TypeAuthOK {
		conn.Close()
		return fmt.Errorf("unexpected message type: %s", msgType)
	}

	authOK, err := protocol.DecodePayload[protocol.AuthOK](payload)
	if err != nil {
		conn.Close()
		return fmt.Errorf("decode AUTH_OK: %w", err)
	}

	w.workerID = authOK.WorkerID
	w.log.Info("authenticated", "worker_id", w.workerID, "server_version", authOK.ServerVersion)

	// Send REGISTER
	if err := w.sendRegister(); err != nil {
		conn.Close()
		return fmt.Errorf("send register: %w", err)
	}

	// Wait for REGISTERED
	_, msg, err = conn.ReadMessage()
	if err != nil {
		conn.Close()
		return fmt.Errorf("read registered: %w", err)
	}

	msgType, _, err = protocol.Decode(msg)
	if err != nil {
		conn.Close()
		return fmt.Errorf("decode registered: %w", err)
	}

	if msgType != protocol.TypeRegistered {
		conn.Close()
		return fmt.Errorf("expected REGISTERED, got %s", msgType)
	}

	w.log.Info("registered with server")
	return nil
}

// reconnect attempts to reconnect with exponential backoff.
func (w *Worker) reconnect() error {
	delay := minReconnectDelay

	for {
		select {
		case <-w.ctx.Done():
			return w.ctx.Err()
		default:
		}

		w.log.Info("attempting reconnect", "delay", delay)
		time.Sleep(delay)

		if err := w.connect(); err != nil {
			w.log.Warn("reconnect failed", "error", err)
			delay = delay * 2
			if delay > maxReconnectDelay {
				delay = maxReconnectDelay
			}
			continue
		}

		return nil
	}
}

// sendRegister sends registration message.
func (w *Worker) sendRegister() error {
	reg := protocol.Register{
		Labels: w.config.Labels,
		Capabilities: protocol.Capabilities{
			Docker:      w.config.Docker,
			Concurrency: w.config.Concurrency,
		},
		Version:  workerVersion,
		Hostname: w.config.Hostname,
	}

	return w.send(protocol.TypeRegister, reg)
}

// send encodes and sends a message.
func (w *Worker) send(msgType string, payload any) error {
	msg, err := protocol.Encode(msgType, payload)
	if err != nil {
		return err
	}

	w.connLock.Lock()
	defer w.connLock.Unlock()

	if w.conn == nil {
		return errors.New("not connected")
	}

	return w.conn.WriteMessage(websocket.TextMessage, msg)
}

// readLoop reads and handles messages from server.
func (w *Worker) readLoop() {
	defer w.wg.Done()

	for {
		select {
		case <-w.ctx.Done():
			return
		default:
		}

		w.connLock.Lock()
		conn := w.conn
		w.connLock.Unlock()

		if conn == nil {
			if err := w.reconnect(); err != nil {
				return
			}
			continue
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				w.log.Info("connection closed")
			} else {
				w.log.Warn("read error", "error", err)
			}

			w.connLock.Lock()
			w.conn = nil
			w.connLock.Unlock()

			// Try to reconnect
			if err := w.reconnect(); err != nil {
				return
			}
			continue
		}

		w.handleMessage(msg)
	}
}

// handleMessage processes an incoming message.
func (w *Worker) handleMessage(msg []byte) {
	msgType, payload, err := protocol.Decode(msg)
	if err != nil {
		w.log.Warn("failed to decode message", "error", err)
		return
	}

	switch msgType {
	case protocol.TypeJobAssign:
		w.handleJobAssign(payload)
	case protocol.TypeJobCancel:
		w.handleJobCancel(payload)
	case protocol.TypePong:
		// Ignore pong
	case protocol.TypeAck:
		// Ignore ack
	default:
		w.log.Warn("unknown message type", "type", msgType)
	}
}

// handleJobAssign processes a job assignment.
func (w *Worker) handleJobAssign(payload []byte) {
	assign, err := protocol.DecodePayload[protocol.JobAssign](payload)
	if err != nil {
		w.log.Warn("failed to decode JOB_ASSIGN", "error", err)
		return
	}

	// Check capacity
	w.jobsLock.Lock()
	if len(w.activeJobs) >= w.config.Concurrency {
		w.jobsLock.Unlock()
		w.log.Warn("at capacity, rejecting job", "job_id", assign.JobID)
		if err := w.send(protocol.TypeJobReject, protocol.JobReject{
			JobID:  assign.JobID,
			Reason: "worker at max concurrency",
		}); err != nil {
			w.log.Warn("failed to send JOB_REJECT", "job_id", assign.JobID, "error", err)
		}
		return
	}

	// Create cancellable context for this job
	jobCtx, jobCancel := context.WithCancel(w.ctx)
	w.activeJobs[assign.JobID] = jobCancel
	w.jobsLock.Unlock()

	// Acknowledge
	if err := w.send(protocol.TypeJobAck, protocol.JobAck{JobID: assign.JobID}); err != nil {
		w.log.Warn("failed to send JOB_ACK", "job_id", assign.JobID, "error", err)
	}

	w.log.Info("job assigned", "job_id", assign.JobID)

	// Execute job in goroutine
	go w.executeJob(jobCtx, assign)
}

// handleJobCancel processes a job cancellation.
func (w *Worker) handleJobCancel(payload []byte) {
	cancel, err := protocol.DecodePayload[protocol.JobCancel](payload)
	if err != nil {
		w.log.Warn("failed to decode JOB_CANCEL", "error", err)
		return
	}

	w.jobsLock.Lock()
	if cancelFn, ok := w.activeJobs[cancel.JobID]; ok {
		cancelFn()
		delete(w.activeJobs, cancel.JobID)
	}
	w.jobsLock.Unlock()

	w.log.Info("job cancelled", "job_id", cancel.JobID, "reason", cancel.Reason)
}

// executeJob runs a job and reports results.
func (w *Worker) executeJob(ctx context.Context, assign protocol.JobAssign) {
	jobID := assign.JobID
	start := time.Now()

	defer func() {
		w.jobsLock.Lock()
		delete(w.activeJobs, jobID)
		w.jobsLock.Unlock()
	}()

	// Notify start
	if err := w.send(protocol.TypeJobStarted, protocol.NewJobStarted(jobID)); err != nil {
		w.log.Warn("failed to send JOB_STARTED", "job_id", jobID, "error", err)
	}
	if w.OnJobStart != nil {
		w.OnJobStart(jobID)
	}

	// Clone repository
	workDir, err := w.cloneRepo(ctx, assign.Repo)
	if err != nil {
		w.reportError(jobID, protocol.PhaseClone, err.Error())
		return
	}
	defer os.RemoveAll(workDir)

	// Load config from repo (overrides server-provided config)
	command := assign.Config.Command
	cfg, _, err := config.Load(workDir)
	if err == nil {
		// Select build or release based on whether this is a tag push
		isTag := assign.Repo.Tag != ""
		configCommand := cfg.CommandForEvent(isTag)
		if configCommand != "" {
			command = configCommand
			w.log.Debug("using command from .cinch.yaml", "command", command, "is_tag", isTag)
		}
	}
	if command == "" {
		command = "make check" // Default fallback
		w.log.Debug("using default command", "command", command)
	}

	// Create log streamer
	streamer := NewLogStreamer(jobID, func(jobID, stream, data string) {
		if err := w.send(protocol.TypeLogChunk, protocol.NewLogChunk(jobID, stream, data)); err != nil {
			w.log.Warn("failed to send log chunk", "job_id", jobID, "error", err)
		}
	})

	// Build environment with Cinch variables
	env := make(map[string]string)
	for k, v := range assign.Config.Env {
		env[k] = v
	}
	env["CINCH_JOB_ID"] = jobID
	env["CINCH_REF"] = assign.Repo.Ref
	env["CINCH_BRANCH"] = assign.Repo.Branch
	env["CINCH_TAG"] = assign.Repo.Tag
	env["CINCH_COMMIT"] = assign.Repo.Commit
	env["CINCH_REPO"] = assign.Repo.CloneURL
	env["CINCH_FORGE"] = assign.Repo.ForgeType

	// Set forge-specific token env var for API access (releases, comments, etc.)
	if assign.Repo.CloneToken != "" {
		switch assign.Repo.ForgeType {
		case "github":
			env["GITHUB_TOKEN"] = assign.Repo.CloneToken
		case "gitlab":
			env["GITLAB_TOKEN"] = assign.Repo.CloneToken
			env["CI_JOB_TOKEN"] = assign.Repo.CloneToken // GitLab compat
		case "forgejo", "gitea":
			env["GITEA_TOKEN"] = assign.Repo.CloneToken
		}
		env["CINCH_FORGE_TOKEN"] = assign.Repo.CloneToken
	}

	// Resolve container configuration
	effectiveCfg := cfg
	if effectiveCfg == nil {
		effectiveCfg = &config.Config{}
	}

	// Determine execution mode
	var exitCode int
	stdout := io.MultiWriter(streamer.Stdout(), os.Stdout)
	stderr := io.MultiWriter(streamer.Stderr(), os.Stderr)

	if w.config.Docker && !effectiveCfg.IsBareMetalContainer() {
		// Container mode
		source, err := container.ResolveContainer(effectiveCfg, workDir)
		if err != nil {
			w.reportError(jobID, protocol.PhaseExecute, fmt.Sprintf("resolve container: %v", err))
			return
		}

		if source.Type == "bare-metal" {
			// Config says bare-metal
			exitCode, err = w.runBareMetal(ctx, command, workDir, env, stdout, stderr)
		} else {
			// Run in container
			w.log.Info("executing job",
				"job_id", jobID,
				"repo", assign.Repo.CloneURL,
				"branch", assign.Repo.Branch,
				"commit", assign.Repo.Commit[:8],
				"command", command,
				"mode", "container",
				"container_type", source.Type,
			)

			exitCode, err = w.runInContainer(ctx, jobID, source, command, workDir, env, stdout, stderr)
		}
	} else {
		// Bare-metal mode
		w.log.Info("executing job",
			"job_id", jobID,
			"repo", assign.Repo.CloneURL,
			"branch", assign.Repo.Branch,
			"commit", assign.Repo.Commit[:8],
			"command", command,
			"mode", "bare-metal",
		)

		exitCode, err = w.runBareMetal(ctx, command, workDir, env, stdout, stderr)
	}
	if err != nil && ctx.Err() != nil {
		// Context cancelled
		w.reportError(jobID, protocol.PhaseExecute, "job cancelled")
		return
	}

	// Flush any remaining logs
	streamer.Flush()

	duration := time.Since(start)

	// Report completion
	if err := w.send(protocol.TypeJobComplete, protocol.NewJobComplete(jobID, exitCode, duration)); err != nil {
		w.log.Warn("failed to send JOB_COMPLETE", "job_id", jobID, "error", err)
	}

	if w.OnJobComplete != nil {
		w.OnJobComplete(jobID, exitCode, duration)
	}

	w.log.Info("job completed", "job_id", jobID, "exit_code", exitCode, "duration", duration)
}

// reportError sends a job error message.
func (w *Worker) reportError(jobID, phase, errMsg string) {
	if err := w.send(protocol.TypeJobError, protocol.JobError{
		JobID: jobID,
		Error: errMsg,
		Phase: phase,
	}); err != nil {
		w.log.Warn("failed to send JOB_ERROR", "job_id", jobID, "error", err)
	}

	if w.OnJobError != nil {
		w.OnJobError(jobID, phase, errMsg)
	}

	w.log.Error("job error", "job_id", jobID, "phase", phase, "error", errMsg)
}

// cloneRepo clones the repository and returns the working directory.
func (w *Worker) cloneRepo(ctx context.Context, repo protocol.JobRepo) (string, error) {
	cloner := &GitCloner{}
	return cloner.Clone(ctx, repo)
}

// pingLoop sends periodic pings to the server.
func (w *Worker) pingLoop() {
	defer w.wg.Done()

	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.jobsLock.Lock()
			activeJobs := make([]string, 0, len(w.activeJobs))
			for jobID := range w.activeJobs {
				activeJobs = append(activeJobs, jobID)
			}
			w.jobsLock.Unlock()

			if err := w.send(protocol.TypePing, protocol.NewPing(activeJobs)); err != nil {
				w.log.Warn("failed to send ping", "error", err)
			}
		}
	}
}

// ActiveJobCount returns the number of jobs currently running.
func (w *Worker) ActiveJobCount() int {
	w.jobsLock.Lock()
	defer w.jobsLock.Unlock()
	return len(w.activeJobs)
}

// WorkerID returns the assigned worker ID.
func (w *Worker) WorkerID() string {
	return w.workerID
}

// runBareMetal executes a command directly on the host.
func (w *Worker) runBareMetal(ctx context.Context, command, workDir string, env map[string]string, stdout, stderr io.Writer) (int, error) {
	executor := &Executor{
		WorkDir: workDir,
		Env:     env,
		Stdout:  stdout,
		Stderr:  stderr,
	}
	return executor.Run(ctx, command)
}

// runInContainer executes a command inside a container.
func (w *Worker) runInContainer(ctx context.Context, jobID string, source *container.ImageSource, command, workDir string, env map[string]string, stdout, stderr io.Writer) (int, error) {
	// Prepare image (pull or build)
	image, err := container.PrepareImage(ctx, source, jobID, stdout, stderr)
	if err != nil {
		return 1, fmt.Errorf("prepare image: %w", err)
	}

	// Run command in container
	docker := &container.Docker{
		WorkDir:      workDir,
		Image:        image,
		Env:          env,
		CacheVolumes: container.DefaultCacheVolumes(),
		Stdout:       stdout,
		Stderr:       stderr,
	}

	// Debug: show what's in the workspace
	fmt.Fprintf(stdout, "--- workspace: %s ---\n", workDir)
	debugDocker := &container.Docker{
		WorkDir: workDir,
		Image:   image,
		Stdout:  stdout,
		Stderr:  stderr,
	}
	_, _ = debugDocker.Run(ctx, "ls -la")
	fmt.Fprintf(stdout, "--- running: %s ---\n", command)

	return docker.Run(ctx, command)
}
