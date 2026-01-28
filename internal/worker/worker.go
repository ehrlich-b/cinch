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
	Docker      bool
	Hostname    string
	Verbose     bool   // Show job output in terminal (default: only banners)
	Concurrency int    // Number of concurrent jobs (default 1)
	SocketPath  string // Unix socket path for daemon mode
}

// JobInfo holds information about a running job.
type JobInfo struct {
	ID        string
	Repo      string
	Branch    string
	Tag       string
	Commit    string
	Command   string
	Mode      string
	Forge     string
	StartedAt time.Time
	Cancel    context.CancelFunc
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
	activeJobs map[string]*JobInfo

	// Control
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	draining bool // When true, reject new jobs but let existing ones finish

	// Callbacks
	OnJobStart    func(jobID string)
	OnJobComplete func(jobID string, exitCode int, duration time.Duration)
	OnJobError    func(jobID string, phase, err string)

	// Event broadcasting for daemon mode
	eventBroadcaster EventBroadcaster
}

// EventBroadcaster receives job events for streaming to clients.
type EventBroadcaster interface {
	BroadcastJobStarted(jobID, repo, branch, tag, commit, command, mode, forge string)
	BroadcastLogChunk(jobID, stream string, data []byte)
	BroadcastJobCompleted(jobID string, exitCode int, durationMs int64)
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
		activeJobs: make(map[string]*JobInfo),
		ctx:        ctx,
		cancel:     cancel,
	}
}

// SetEventBroadcaster sets the event broadcaster for daemon mode.
func (w *Worker) SetEventBroadcaster(eb EventBroadcaster) {
	w.eventBroadcaster = eb
}

// GetRunningJobs returns information about all currently running jobs.
func (w *Worker) GetRunningJobs() []*JobInfo {
	w.jobsLock.Lock()
	defer w.jobsLock.Unlock()

	jobs := make([]*JobInfo, 0, len(w.activeJobs))
	for _, info := range w.activeJobs {
		jobs = append(jobs, info)
	}
	return jobs
}

// OldestRunningJob returns the ID of the oldest running job, or empty string if none.
func (w *Worker) OldestRunningJob() string {
	w.jobsLock.Lock()
	defer w.jobsLock.Unlock()

	var oldest *JobInfo
	for _, info := range w.activeJobs {
		if oldest == nil || info.StartedAt.Before(oldest.StartedAt) {
			oldest = info
		}
	}
	if oldest == nil {
		return ""
	}
	return oldest.ID
}

// Concurrency returns the worker's concurrency limit.
func (w *Worker) Concurrency() int {
	return w.config.Concurrency
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

// Drain stops accepting new jobs and waits for running jobs to complete.
// Returns the number of jobs that were running when drain started.
func (w *Worker) Drain(timeout time.Duration) int {
	w.jobsLock.Lock()
	w.draining = true
	initialCount := len(w.activeJobs)
	w.jobsLock.Unlock()

	if initialCount == 0 {
		return 0
	}

	w.log.Info("draining", "jobs", initialCount)

	// Wait for active jobs to complete
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
		w.log.Info("drain complete")
	case <-time.After(timeout):
		w.log.Warn("drain timeout", "remaining", w.ActiveJobCount())
	}

	return initialCount
}

// IsDraining returns true if the worker is draining (not accepting new jobs).
func (w *Worker) IsDraining() bool {
	w.jobsLock.Lock()
	defer w.jobsLock.Unlock()
	return w.draining
}

// Stop gracefully shuts down the worker.
// It first drains (waits for running jobs), then cancels and cleans up.
func (w *Worker) Stop() {
	// Drain first - let running jobs complete
	w.Drain(5 * time.Minute)

	// Now cancel context and cleanup
	w.cancel()

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
			Docker: w.config.Docker,
		},
		Version:     workerVersion,
		Hostname:    w.config.Hostname,
		Concurrency: w.config.Concurrency,
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

	// Check if draining or at capacity
	w.jobsLock.Lock()
	if w.draining {
		w.jobsLock.Unlock()
		w.log.Info("draining, rejecting new job", "job_id", assign.JobID)
		if err := w.send(protocol.TypeJobReject, protocol.JobReject{
			JobID:  assign.JobID,
			Reason: "worker draining",
		}); err != nil {
			w.log.Warn("failed to send JOB_REJECT", "job_id", assign.JobID, "error", err)
		}
		return
	}
	if len(w.activeJobs) >= w.config.Concurrency {
		w.jobsLock.Unlock()
		w.log.Warn("at capacity, rejecting", "job_id", assign.JobID, "current", len(w.activeJobs), "max", w.config.Concurrency)
		if err := w.send(protocol.TypeJobReject, protocol.JobReject{
			JobID:  assign.JobID,
			Reason: "worker at capacity",
		}); err != nil {
			w.log.Warn("failed to send JOB_REJECT", "job_id", assign.JobID, "error", err)
		}
		return
	}

	// Create cancellable context for this job
	jobCtx, jobCancel := context.WithCancel(w.ctx)

	// Create job info
	jobInfo := &JobInfo{
		ID:        assign.JobID,
		Repo:      assign.Repo.CloneURL,
		Branch:    assign.Repo.Branch,
		Tag:       assign.Repo.Tag,
		Commit:    assign.Repo.Commit,
		Forge:     assign.Repo.ForgeType,
		StartedAt: time.Now(),
		Cancel:    jobCancel,
	}
	w.activeJobs[assign.JobID] = jobInfo
	w.jobsLock.Unlock()

	// Acknowledge
	if err := w.send(protocol.TypeJobAck, protocol.JobAck{JobID: assign.JobID}); err != nil {
		w.log.Warn("failed to send JOB_ACK", "job_id", assign.JobID, "error", err)
	}

	w.log.Info("job assigned", "job_id", assign.JobID, "active", len(w.activeJobs), "max", w.config.Concurrency)

	// Execute job in goroutine
	go w.executeJob(jobCtx, assign, jobInfo)
}

// handleJobCancel processes a job cancellation.
func (w *Worker) handleJobCancel(payload []byte) {
	cancel, err := protocol.DecodePayload[protocol.JobCancel](payload)
	if err != nil {
		w.log.Warn("failed to decode JOB_CANCEL", "error", err)
		return
	}

	w.jobsLock.Lock()
	if jobInfo, ok := w.activeJobs[cancel.JobID]; ok {
		jobInfo.Cancel()
		delete(w.activeJobs, cancel.JobID)
	}
	w.jobsLock.Unlock()

	w.log.Info("job cancelled", "job_id", cancel.JobID, "reason", cancel.Reason)
}

// executeJob runs a job and reports results.
func (w *Worker) executeJob(ctx context.Context, assign protocol.JobAssign, jobInfo *JobInfo) {
	jobID := assign.JobID
	start := time.Now()
	term := NewTerminal(os.Stdout)

	defer func() {
		w.jobsLock.Lock()
		delete(w.activeJobs, jobID)
		w.jobsLock.Unlock()
	}()

	// Print job claimed (non-TTY mode)
	term.PrintJobClaimed(jobID)

	// Notify start
	if err := w.send(protocol.TypeJobStarted, protocol.NewJobStarted(jobID)); err != nil {
		w.log.Warn("failed to send JOB_STARTED", "job_id", jobID, "error", err)
	}
	if w.OnJobStart != nil {
		w.OnJobStart(jobID)
	}

	// Print cloning (non-TTY mode)
	ref := assign.Repo.Branch
	if assign.Repo.Tag != "" {
		ref = assign.Repo.Tag
	}
	term.PrintCloning(assign.Repo.CloneURL, ref)

	// Clone repository
	workDir, err := w.cloneRepo(ctx, assign.Repo)
	if err != nil {
		term.PrintJobError(protocol.PhaseClone, err.Error())
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
		// Also broadcast to daemon clients
		if w.eventBroadcaster != nil {
			w.eventBroadcaster.BroadcastLogChunk(jobID, stream, []byte(data))
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

	// Determine execution mode and prepare for running
	var exitCode int
	var runErr error
	var execMode string

	// In verbose mode, output goes to terminal AND server
	// In quiet mode, output only goes to server (for log streaming)
	var stdout, stderr io.Writer
	if w.config.Verbose {
		stdout = io.MultiWriter(streamer.Stdout(), os.Stdout)
		stderr = io.MultiWriter(streamer.Stderr(), os.Stderr)
	} else {
		stdout = streamer.Stdout()
		stderr = streamer.Stderr()
	}

	if w.config.Docker && !effectiveCfg.IsBareMetalContainer() {
		// Container mode
		source, err := container.ResolveContainer(effectiveCfg, workDir)
		if err != nil {
			term.PrintJobError(protocol.PhaseExecute, fmt.Sprintf("resolve container: %v", err))
			w.reportError(jobID, protocol.PhaseExecute, fmt.Sprintf("resolve container: %v", err))
			return
		}

		if source.Type == "bare-metal" {
			// Config says bare-metal
			execMode = "bare-metal"
		} else {
			// Run in container
			execMode = fmt.Sprintf("container: %s", source.Type)
			if source.Image != "" {
				execMode = fmt.Sprintf("container: %s", source.Image)
			}
		}

		// Update job info and print start banner
		jobInfo.Command = command
		jobInfo.Mode = execMode
		term.PrintJobStart(assign.Repo.CloneURL, assign.Repo.Branch, assign.Repo.Tag, assign.Repo.Commit, command, execMode, assign.Repo.ForgeType)

		// Broadcast job start to daemon clients
		if w.eventBroadcaster != nil {
			w.eventBroadcaster.BroadcastJobStarted(jobID, assign.Repo.CloneURL, assign.Repo.Branch, assign.Repo.Tag, assign.Repo.Commit, command, execMode, assign.Repo.ForgeType)
		}

		if source.Type == "bare-metal" {
			exitCode, runErr = w.runBareMetal(ctx, command, workDir, env, stdout, stderr)
		} else {
			w.log.Info("executing job",
				"job_id", jobID,
				"repo", assign.Repo.CloneURL,
				"branch", assign.Repo.Branch,
				"commit", assign.Repo.Commit[:8],
				"command", command,
				"mode", "container",
				"container_type", source.Type,
			)

			exitCode, runErr = w.runInContainer(ctx, jobID, source, command, workDir, env, stdout, stderr)
		}
	} else {
		// Bare-metal mode
		execMode = "bare-metal"

		// Update job info and print start banner
		jobInfo.Command = command
		jobInfo.Mode = execMode
		term.PrintJobStart(assign.Repo.CloneURL, assign.Repo.Branch, assign.Repo.Tag, assign.Repo.Commit, command, execMode, assign.Repo.ForgeType)

		// Broadcast job start to daemon clients
		if w.eventBroadcaster != nil {
			w.eventBroadcaster.BroadcastJobStarted(jobID, assign.Repo.CloneURL, assign.Repo.Branch, assign.Repo.Tag, assign.Repo.Commit, command, execMode, assign.Repo.ForgeType)
		}

		w.log.Info("executing job",
			"job_id", jobID,
			"repo", assign.Repo.CloneURL,
			"branch", assign.Repo.Branch,
			"commit", assign.Repo.Commit[:8],
			"command", command,
			"mode", "bare-metal",
		)

		exitCode, runErr = w.runBareMetal(ctx, command, workDir, env, stdout, stderr)
	}
	if runErr != nil && ctx.Err() != nil {
		// Context cancelled
		term.PrintJobError(protocol.PhaseExecute, "job cancelled")
		w.reportError(jobID, protocol.PhaseExecute, "job cancelled")
		return
	}

	// Flush any remaining logs
	streamer.Flush()

	duration := time.Since(start)

	// Print job completion banner
	term.PrintJobComplete(exitCode, duration)

	// Print waiting message (non-TTY mode)
	term.PrintJobWaiting()

	// Report completion
	if err := w.send(protocol.TypeJobComplete, protocol.NewJobComplete(jobID, exitCode, duration)); err != nil {
		w.log.Warn("failed to send JOB_COMPLETE", "job_id", jobID, "error", err)
	}

	// Broadcast to daemon clients
	if w.eventBroadcaster != nil {
		w.eventBroadcaster.BroadcastJobCompleted(jobID, exitCode, duration.Milliseconds())
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

	return docker.Run(ctx, command)
}
