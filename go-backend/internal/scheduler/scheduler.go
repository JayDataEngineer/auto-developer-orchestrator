package scheduler

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// ScheduleType defines how a job is scheduled.
type ScheduleType string

const (
	ScheduleCron   ScheduleType = "cron"
	ScheduleEvery  ScheduleType = "every"
	ScheduleAt     ScheduleType = "at"
	ScheduleManual ScheduleType = "manual" // No schedule, runs on trigger only
)

// DeliveryMode defines how job output is delivered.
type DeliveryMode string

const (
	DeliveryStore   DeliveryMode = "store"    // Save to run log only (default)
	DeliveryWebhook DeliveryMode = "webhook"  // POST JSON to URL
	DeliverySession DeliveryMode = "session"  // Inject into main agent session
)

// DefaultBackoffSchedule is the exponential backoff schedule for consecutive errors.
var DefaultBackoffSchedule = []time.Duration{
	30 * time.Second,
	1 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	60 * time.Minute,
}

const (
	maxConsecutiveErrors = 5
	maxMissedOnStartup  = 5
	missedJobStaggerMs  = 5000
	stuckRunThreshold    = 2 * time.Hour
)

// JobStatus tracks the runtime state of a job.
type JobStatus string

const (
	StatusIdle    JobStatus = "idle"
	StatusRunning JobStatus = "running"
	StatusError   JobStatus = "error"
	StatusDisabled JobStatus = "disabled"
)

// Job represents a scheduled job that sends prompts to Pi agents.
type Job struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Project     string       `json:"project"`
	AgentID     string       `json:"agentId,omitempty"`
	Message     string       `json:"message"`
	Model       string       `json:"model,omitempty"`
	Schedule    ScheduleType `json:"scheduleType"`
	// Cron expression (e.g. "0 9 * * *" for daily at 9am)
	CronExpr string `json:"cronExpr,omitempty"`
	// Timezone for cron expression (e.g. "America/New_York", default: UTC)
	Timezone string `json:"timezone,omitempty"`
	// Fixed interval in seconds
	EverySeconds int64 `json:"everySeconds,omitempty"`
	// One-shot time (RFC3339)
	AtTime string `json:"atTime,omitempty"`
	// Execution settings
	AutoBranch bool `json:"autoBranch,omitempty"`
	AutoMerge  bool `json:"autoMerge,omitempty"`
	Enabled    bool `json:"enabled"`
	// Delivery
	DeliveryMode     DeliveryMode `json:"deliveryMode,omitempty"`
	DeliveryWebhookURL string     `json:"deliveryWebhookUrl,omitempty"`
	DeliveryBestEffort bool       `json:"deliveryBestEffort,omitempty"`
	// Failure alerts
	FailureAlertAfter      int    `json:"failureAlertAfter,omitempty"`
	FailureAlertWebhookURL string `json:"failureAlertWebhookUrl,omitempty"`
	// Runtime state
	Status           JobStatus `json:"status"`
	LastRunAt        time.Time `json:"lastRunAt,omitempty"`
	LastRunStatus    string    `json:"lastRunStatus,omitempty"`
	LastError        string    `json:"lastError,omitempty"`
	NextRunAt        time.Time `json:"nextRunAt,omitempty"`
	ConsecutiveErrors int      `json:"consecutiveErrors"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
	// Metrics (from execution results)
	InputTokens  int   `json:"inputTokens,omitempty"`
	OutputTokens int   `json:"outputTokens,omitempty"`
	DurationMs   int64 `json:"durationMs,omitempty"`
	// Dependencies (task-like)
	Blocks   []string `json:"blocks,omitempty"`   // job IDs this blocks
	BlockedBy []string `json:"blockedBy,omitempty"` // job IDs blocking this

	// Inbound webhook token — POST /api/scheduler/webhook/{token} triggers this job
	WebhookToken string `json:"webhookToken,omitempty"`

	// Internal: cron entry ID
	cronEntryID cron.EntryID
}

// JobExecution records a single run of a job.
type JobExecution struct {
	ID        string    `json:"id"`
	JobID     string    `json:"jobId"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt,omitempty"`
	Status    string    `json:"status"` // "running", "success", "error"
	Error     string    `json:"error,omitempty"`
	Output    string    `json:"output,omitempty"`
}

// PromptSender is a function that sends a prompt to a Pi agent and returns the response text.
type PromptSender func(ctx context.Context, project, agentID, message, model string, autoBranch, autoMerge bool) (string, error)

// SessionInjector injects text into the main agent session.
type SessionInjector func(project, agentID, text string) error

// Scheduler manages scheduled jobs.
type Scheduler struct {
	jobs          map[string]*Job
	executions    []*JobExecution
	cronScheduler *cron.Cron
	promptSender  PromptSender
	logger        *zap.Logger
	storePath     string
	mu            sync.RWMutex
	stopCh        chan struct{}

	// Phase 1: Isolated execution + run logs
	isolated    *IsolatedExecutor
	runLogMgr   *RunLogManager
	projectRoot string

	// Phase 4: Session delivery
	sessionInjector SessionInjector
}

// NewScheduler creates a new scheduler.
func NewScheduler(storePath string, sender PromptSender, logger *zap.Logger) *Scheduler {
	return &Scheduler{
		jobs:          make(map[string]*Job),
		executions:    make([]*JobExecution, 0),
		cronScheduler: cron.New(cron.WithSeconds()),
		promptSender:  sender,
		logger:        logger,
		storePath:     storePath,
		stopCh:        make(chan struct{}),
	}
}

// SetIsolatedExecutor configures the scheduler to use isolated Pi subprocesses
// for job execution instead of the main agent pool.
func (s *Scheduler) SetIsolatedExecutor(executor *IsolatedExecutor, runLogMgr *RunLogManager, projectRoot string) {
	s.isolated = executor
	s.runLogMgr = runLogMgr
	s.projectRoot = projectRoot
}

// SetSessionInjector configures the scheduler to inject job output into the
// main agent session (delivery mode "session").
func (s *Scheduler) SetSessionInjector(injector SessionInjector) {
	s.sessionInjector = injector
}

// Start loads persisted jobs and starts the scheduler.
func (s *Scheduler) Start(ctx context.Context) error {
	if err := s.load(); err != nil {
		s.logger.Warn("failed to load scheduler state, starting fresh", zap.Error(err))
	}

	s.cronScheduler.Start()

	// Start interval/at timers
	go s.runIntervalLoop(ctx)

	s.logger.Info("scheduler started", zap.Int("jobs", len(s.jobs)))
	return nil
}

// Stop gracefully stops the scheduler and persists state.
func (s *Scheduler) Stop() {
	close(s.stopCh)
	s.cronScheduler.Stop()
	s.save()
	s.logger.Info("scheduler stopped")
}

// CreateJob creates and schedules a new job.
func (s *Scheduler) CreateJob(job *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if job.ID == "" {
		job.ID = fmt.Sprintf("job-%d", time.Now().UnixMilli())
	}
	if job.WebhookToken == "" {
		b := make([]byte, 16)
		rand.Read(b)
		job.WebhookToken = hex.EncodeToString(b)
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	job.UpdatedAt = time.Now()
	if job.Enabled {
		job.Status = StatusIdle
	} else {
		job.Status = StatusDisabled
	}

	// Validate schedule
	if err := s.validateJob(job); err != nil {
		return err
	}

	// Compute next run
	s.computeNextRun(job)

	// Schedule cron jobs
	if job.Schedule == ScheduleCron && job.Enabled {
		entryID, err := s.cronScheduler.AddFunc(job.CronExpr, func() {
			s.executeJob(job.ID)
		})
		if err != nil {
			return fmt.Errorf("invalid cron expression: %w", err)
		}
		job.cronEntryID = entryID
	}

	s.jobs[job.ID] = job
	s.save()

	s.logger.Info("job created",
		zap.String("id", job.ID),
		zap.String("name", job.Name),
		zap.String("schedule", string(job.Schedule)),
	)
	return nil
}

// UpdateJob updates an existing job.
func (s *Scheduler) UpdateJob(jobID string, updates *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.jobs[jobID]
	if !ok {
		return fmt.Errorf("job %s not found", jobID)
	}

	// Remove existing cron entry if present
	if existing.cronEntryID != 0 {
		s.cronScheduler.Remove(existing.cronEntryID)
		existing.cronEntryID = 0
	}

	// Apply updates
	if updates.Name != "" {
		existing.Name = updates.Name
	}
	if updates.Description != "" {
		existing.Description = updates.Description
	}
	if updates.Message != "" {
		existing.Message = updates.Message
	}
	if updates.Model != "" {
		existing.Model = updates.Model
	}
	if updates.Project != "" {
		existing.Project = updates.Project
	}
	if updates.AgentID != "" {
		existing.AgentID = updates.AgentID
	}
	if updates.Schedule != "" {
		existing.Schedule = updates.Schedule
	}
	if updates.CronExpr != "" {
		existing.CronExpr = updates.CronExpr
	}
	if updates.EverySeconds > 0 {
		existing.EverySeconds = updates.EverySeconds
	}
	if updates.AtTime != "" {
		existing.AtTime = updates.AtTime
	}
	existing.AutoBranch = updates.AutoBranch
	existing.AutoMerge = updates.AutoMerge
	existing.UpdatedAt = time.Now()

	// Validate
	if err := s.validateJob(existing); err != nil {
		return err
	}

	// Re-schedule if enabled
	if updates.Enabled || existing.Enabled {
		existing.Enabled = updates.Enabled
		if existing.Enabled {
			existing.Status = StatusIdle
			s.computeNextRun(existing)
			if existing.Schedule == ScheduleCron {
				entryID, err := s.cronScheduler.AddFunc(existing.CronExpr, func() {
					s.executeJob(existing.ID)
				})
				if err != nil {
					return fmt.Errorf("invalid cron expression: %w", err)
				}
				existing.cronEntryID = entryID
			}
		} else {
			existing.Status = StatusDisabled
		}
	}

	s.save()
	return nil
}

// DeleteJob removes a job.
func (s *Scheduler) DeleteJob(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return fmt.Errorf("job %s not found", jobID)
	}

	if job.cronEntryID != 0 {
		s.cronScheduler.Remove(job.cronEntryID)
	}

	delete(s.jobs, jobID)
	s.save()

	s.logger.Info("job deleted", zap.String("id", jobID))
	return nil
}

// GetJob returns a single job.
func (s *Scheduler) GetJob(jobID string) (*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return nil, fmt.Errorf("job %s not found", jobID)
	}
	return job, nil
}

// ListJobs returns all jobs.
func (s *Scheduler) ListJobs() []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		result = append(result, j)
	}
	return result
}

// FindJobByWebhookToken looks up a job by its webhook token.
func (s *Scheduler) FindJobByWebhookToken(token string) (*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, j := range s.jobs {
		if j.WebhookToken == token {
			return j, nil
		}
	}
	return nil, fmt.Errorf("no job found for webhook token")
}

// ListExecutions returns recent executions, optionally filtered by jobID.
func (s *Scheduler) ListExecutions(jobID string, limit int) []*JobExecution {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	result := make([]*JobExecution, 0, limit)
	for i := len(s.executions) - 1; i >= 0 && len(result) < limit; i-- {
		if jobID == "" || s.executions[i].JobID == jobID {
			result = append(result, s.executions[i])
		}
	}
	return result
}

// ListRuns returns persistent run log entries for a job.
func (s *Scheduler) ListRuns(jobID string, limit int, statusFilter string) ([]RunLogEntry, error) {
	if s.runLogMgr == nil {
		return nil, nil
	}
	return s.runLogMgr.Read(jobID, limit)
}

// ListAllRuns returns run log entries across all jobs.
func (s *Scheduler) ListAllRuns(limit int, statusFilter, jobIDFilter string) ([]RunLogEntry, error) {
	if s.runLogMgr == nil {
		return nil, nil
	}
	return s.runLogMgr.ReadAll(limit, statusFilter, jobIDFilter)
}

// TriggerJob manually triggers a job execution.
// For manual tasks (scheduleType "manual"), the job is temporarily enabled
// so executeJob will run it.
func (s *Scheduler) TriggerJob(jobID string) error {
	s.mu.RLock()
	job, ok := s.jobs[jobID]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("job %s not found", jobID)
	}

	// For manual tasks, temporarily enable so executeJob proceeds
	if job.Schedule == ScheduleManual && !job.Enabled {
		s.mu.Lock()
		job.Enabled = true
		job.Status = StatusIdle
		s.mu.Unlock()
	}

	go s.executeJob(jobID)
	return nil
}

// executeJob runs a single job execution.
func (s *Scheduler) executeJob(jobID string) {
	s.mu.Lock()
	job, ok := s.jobs[jobID]
	if !ok || !job.Enabled {
		s.mu.Unlock()
		return
	}

	// Skip if already running
	if job.Status == StatusRunning {
		s.mu.Unlock()
		s.logger.Debug("job already running, skipping", zap.String("id", jobID))
		return
	}

	job.Status = StatusRunning
	s.mu.Unlock()

	execution := &JobExecution{
		ID:        fmt.Sprintf("exec-%d", time.Now().UnixMilli()),
		JobID:     jobID,
		StartedAt: time.Now(),
		Status:    "running",
	}

	ctx := context.Background()

	s.logger.Info("executing job",
		zap.String("id", jobID),
		zap.String("name", job.Name),
		zap.String("project", job.Project),
	)

	// Use isolated execution if configured
	var output string
	var err error
	var result *JobResult
	if s.isolated != nil {
		projectPath := s.resolveProjectPath(job.Project)
		if projectPath == "" {
			err = fmt.Errorf("project %s not found", job.Project)
		} else {
			result = s.isolated.Execute(ctx, jobID, job.Name, projectPath, job.Message, job.Model, 0)
			if result.Error != "" {
				err = fmt.Errorf("%s", result.Error)
			}
			output = result.Output
			job.InputTokens = result.InputTokens
			job.OutputTokens = result.OutputTokens
		}
	} else {
		// Fallback to main agent pool
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		output, err = s.promptSender(ctx, job.Project, job.AgentID, job.Message, job.Model, job.AutoBranch, job.AutoMerge)
	}

	execution.EndedAt = time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Re-fetch job (may have been modified)
	job, ok = s.jobs[jobID]
	if !ok {
		return
	}

	if err != nil {
		execution.Status = "error"
		execution.Error = truncateClip(err.Error(), 2000)
		job.LastRunStatus = "error"
		job.LastError = truncateClip(err.Error(), 500)
		job.ConsecutiveErrors++
		job.DurationMs = execution.EndedAt.Sub(execution.StartedAt).Milliseconds()

		// Error backoff
		endedAt := time.Now()
		if backoff := s.errorBackoff(job.ConsecutiveErrors); backoff > 0 {
			nextRun := endedAt.Add(backoff)
			s.logger.Info("applying error backoff",
				zap.String("id", jobID),
				zap.Int("errors", job.ConsecutiveErrors),
				zap.Duration("backoff", backoff),
			)
			if job.NextRunAt.Before(nextRun) {
				job.NextRunAt = nextRun
			}
		}

		if job.ConsecutiveErrors >= maxConsecutiveErrors {
			job.Status = StatusError
			job.Enabled = false
			s.logger.Error("job disabled after consecutive errors",
				zap.String("id", jobID),
				zap.Int("errors", job.ConsecutiveErrors),
			)
			s.sendFailureAlert(job, execution.Error)
		} else if job.Schedule == ScheduleManual {
			// Manual tasks return to disabled after failure
			job.Status = StatusDisabled
			job.Enabled = false
		} else {
			job.Status = StatusIdle
		}
	} else {
		execution.Status = "success"
		execution.Output = truncateClip(output, 5000)
		job.LastRunStatus = "success"
		job.LastError = ""
		job.ConsecutiveErrors = 0
		job.DurationMs = execution.EndedAt.Sub(execution.StartedAt).Milliseconds()

		// Deliver output
		s.deliverOutput(job, output)

		if job.Schedule == ScheduleManual {
			// Manual tasks return to disabled after successful run
			job.Status = StatusDisabled
			job.Enabled = false
		} else {
			job.Status = StatusIdle
		}
	}

	job.LastRunAt = time.Now()
	s.computeNextRun(job)

	// Keep last 200 executions
	s.executions = append(s.executions, execution)
	if len(s.executions) > 200 {
		s.executions = s.executions[len(s.executions)-200:]
	}

	// Write to persistent run log (Phase 1)
	if s.runLogMgr != nil {
		status := execution.Status
		if status == "running" {
			status = "ok"
		}
		nextRunMs := int64(0)
		if !job.NextRunAt.IsZero() {
			nextRunMs = job.NextRunAt.UnixMilli()
		}
		s.runLogMgr.Append(RunLogEntry{
			JobID:       jobID,
			Status:      status,
			Error:       execution.Error,
			Summary:     truncateClip(output, 500),
			RunAtMs:     execution.StartedAt.UnixMilli(),
			DurationMs:  execution.EndedAt.Sub(execution.StartedAt).Milliseconds(),
			NextRunAtMs: nextRunMs,
			Model:       job.Model,
		})
	}

	s.save()
}

// CanStart returns true if a job's dependencies are all completed.
func (s *Scheduler) CanStart(jobID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return false, fmt.Errorf("job %s not found", jobID)
	}

	for _, depID := range job.BlockedBy {
		dep, ok := s.jobs[depID]
		if !ok {
			return false, nil // dependency doesn't exist yet
		}
		if dep.LastRunStatus != "success" {
			return false, nil // dependency not completed successfully
		}
	}
	return true, nil
}

// SetDependencies configures job dependencies.
func (s *Scheduler) SetDependencies(jobID string, blocks, blockedBy []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return fmt.Errorf("job %s not found", jobID)
	}

	job.Blocks = blocks
	job.BlockedBy = blockedBy
	job.UpdatedAt = time.Now()

	// Validate: check for cycles
	if err := s.validateNoCycleLocked(jobID); err != nil {
		job.Blocks = nil
		job.BlockedBy = nil
		return err
	}

	s.save()
	return nil
}

// validateNoCycleLocked checks for dependency cycles using DFS.
func (s *Scheduler) validateNoCycleLocked(startID string) error {
	visited := make(map[string]bool)
	inStack := make(map[string]bool)

	var dfs func(id string) error
	dfs = func(id string) error {
		visited[id] = true
		inStack[id] = true

		job, ok := s.jobs[id]
		if !ok {
			return nil
		}
		for _, depID := range job.BlockedBy {
			if inStack[depID] {
				return fmt.Errorf("dependency cycle detected: %s → %s", id, depID)
			}
			if !visited[depID] {
				if err := dfs(depID); err != nil {
					return err
				}
			}
		}

		inStack[id] = false
		return nil
	}

	return dfs(startID)
}

// resolveProjectPath resolves a project name to its filesystem path.
func (s *Scheduler) resolveProjectPath(project string) string {
	if project == "" {
		return ""
	}
	// Try relative to project root
	candidate := filepath.Join(s.projectRoot, project)
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate
	}
	// Try as absolute path
	if info, err := os.Stat(project); err == nil && info.IsDir() {
		return project
	}
	return ""
}

// runIntervalLoop handles "every" and "at" schedule types.
func (s *Scheduler) runIntervalLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.checkIntervalJobs()
		}
	}
}

// checkIntervalJobs checks and executes "every" and "at" jobs that are due.
func (s *Scheduler) checkIntervalJobs() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	for _, job := range s.jobs {
		if !job.Enabled || job.Status == StatusRunning {
			continue
		}

		switch job.Schedule {
		case ScheduleEvery:
			if job.EverySeconds > 0 && !job.NextRunAt.IsZero() && now.After(job.NextRunAt) {
				go s.executeJob(job.ID)
				// Compute next run immediately
				job.NextRunAt = now.Add(time.Duration(job.EverySeconds) * time.Second)
			}
		case ScheduleAt:
			if !job.NextRunAt.IsZero() && now.After(job.NextRunAt) {
				go s.executeJob(job.ID)
				// One-shot: disable after execution
				job.Enabled = false
				job.Status = StatusDisabled
			}
		}
	}
}

// computeNextRun sets the NextRunAt field based on schedule type.
func (s *Scheduler) computeNextRun(job *Job) {
	now := time.Now()

	switch job.Schedule {
	case ScheduleCron:
		// Use parser with seconds support (matches cron.WithSeconds() used above)
		parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
		schedule, err := parser.Parse(job.CronExpr)
		if err == nil {
			job.NextRunAt = schedule.Next(now)
		}
	case ScheduleEvery:
		if job.EverySeconds > 0 {
			if job.LastRunAt.IsZero() {
				job.NextRunAt = now.Add(time.Duration(job.EverySeconds) * time.Second)
			} else {
				job.NextRunAt = job.LastRunAt.Add(time.Duration(job.EverySeconds) * time.Second)
				if job.NextRunAt.Before(now) {
					job.NextRunAt = now.Add(time.Duration(job.EverySeconds) * time.Second)
				}
			}
		}
	case ScheduleAt:
		if t, err := time.Parse(time.RFC3339, job.AtTime); err == nil {
			job.NextRunAt = t
		}
	case ScheduleManual:
		// No next run for manual tasks
	}
}

// validateJob checks that a job has all required fields.
func (s *Scheduler) validateJob(job *Job) error {
	if job.Name == "" {
		return fmt.Errorf("name is required")
	}
	if job.Project == "" {
		return fmt.Errorf("project is required")
	}
	if job.Message == "" {
		return fmt.Errorf("message is required")
	}
	switch job.Schedule {
	case ScheduleCron:
		if job.CronExpr == "" {
			return fmt.Errorf("cronExpr is required for cron schedule")
		}
		if _, err := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor).Parse(job.CronExpr); err != nil {
			return fmt.Errorf("invalid cron expression: %w", err)
		}
	case ScheduleEvery:
		if job.EverySeconds <= 0 {
			return fmt.Errorf("everySeconds must be positive")
		}
	case ScheduleAt:
		if job.AtTime == "" {
			return fmt.Errorf("atTime is required for at schedule")
		}
		if _, err := time.Parse(time.RFC3339, job.AtTime); err != nil {
			return fmt.Errorf("invalid atTime format (use RFC3339): %w", err)
		}
	case ScheduleManual:
		// No schedule fields required — manual tasks run on trigger only
	default:
		return fmt.Errorf("invalid schedule type: %s", job.Schedule)
	}
	return nil
}

// save writes jobs to the store file.
func (s *Scheduler) save() {
	if s.storePath == "" {
		return
	}

	store := struct {
		Jobs       []*Job         `json:"jobs"`
		Executions []*JobExecution `json:"executions"`
	}{
		Jobs:       make([]*Job, 0, len(s.jobs)),
		Executions: s.executions,
	}
	for _, j := range s.jobs {
		store.Jobs = append(store.Jobs, j)
	}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		s.logger.Error("failed to marshal scheduler state", zap.Error(err))
		return
	}

	// Ensure directory exists
	dir := filepath.Dir(s.storePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		s.logger.Error("failed to create scheduler dir", zap.Error(err))
		return
	}

	// Atomic write: write to temp file then rename
	tmpPath := s.storePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		s.logger.Error("failed to write scheduler state", zap.Error(err))
		return
	}
	if err := os.Rename(tmpPath, s.storePath); err != nil {
		s.logger.Error("failed to rename scheduler state", zap.Error(err))
		return
	}
}

// truncateClip clips s to maxLen without any suffix.
func truncateClip(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// truncateEllipsis clips s to maxLen and appends "...".
func truncateEllipsis(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// errorBackoff returns the backoff duration for a given error count.
func (s *Scheduler) errorBackoff(consecutiveErrors int) time.Duration {
	idx := consecutiveErrors - 1
	if idx < 0 {
		return 0
	}
	if idx >= len(DefaultBackoffSchedule) {
		return DefaultBackoffSchedule[len(DefaultBackoffSchedule)-1]
	}
	return DefaultBackoffSchedule[idx]
}

// deliverOutput delivers job output based on the configured delivery mode.
func (s *Scheduler) deliverOutput(job *Job, output string) {
	if output == "" {
		return
	}
	switch job.DeliveryMode {
	case DeliveryStore:
		// Already saved to run log
	case DeliveryWebhook:
		if job.DeliveryWebhookURL != "" {
			s.deliverWebhook(job, output)
		}
	case DeliverySession:
		if s.sessionInjector != nil {
			s.sessionInjector(job.Project, job.AgentID,
				fmt.Sprintf("📅 %s: %s", job.Name, truncateClip(output, 1000)))
		}
	}
}

// deliverWebhook POSTs job output to a webhook URL.
func (s *Scheduler) deliverWebhook(job *Job, output string) {
	payload := map[string]interface{}{
		"jobId":      job.ID,
		"jobName":    job.Name,
		"status":     "ok",
		"output":     truncateClip(output, 5000),
		"runAt":      time.Now().Format(time.RFC3339),
	}
	data, _ := json.Marshal(payload)
	client := httpClient()
	resp, err := client.Post(job.DeliveryWebhookURL, "application/json", bytes.NewReader(data))
	if err != nil {
		s.logger.Warn("webhook delivery failed",
			zap.String("job", job.ID),
			zap.Error(err),
		)
		return
	}
	defer resp.Body.Close()
}

// sendFailureAlert sends a failure alert if configured.
func (s *Scheduler) sendFailureAlert(job *Job, errMsg string) {
	if job.FailureAlertAfter <= 0 {
		job.FailureAlertAfter = maxConsecutiveErrors
	}
	if job.ConsecutiveErrors < job.FailureAlertAfter {
		return
	}
	if job.FailureAlertWebhookURL == "" {
		return
	}
	payload := map[string]interface{}{
		"jobId":      job.ID,
		"jobName":    job.Name,
		"status":     "error",
		"error":      errMsg,
		"consecutiveErrors": job.ConsecutiveErrors,
		"alertAt":    time.Now().Format(time.RFC3339),
	}
	data, _ := json.Marshal(payload)
	client := httpClient()
	resp, err := client.Post(job.FailureAlertWebhookURL, "application/json", bytes.NewReader(data))
	if err != nil {
		s.logger.Error("failure alert webhook failed",
			zap.String("job", job.ID),
			zap.Error(err),
		)
		return
	}
	defer resp.Body.Close()
	s.logger.Info("failure alert sent",
		zap.String("job", job.ID),
		zap.Int("errors", job.ConsecutiveErrors),
	)
}

// load reads jobs from the store file, clears stuck runs, and catches up missed jobs.
func (s *Scheduler) load() error {
	if s.storePath == "" {
		return nil
	}

	data, err := os.ReadFile(s.storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var store struct {
		Jobs       []*Job          `json:"jobs"`
		Executions []*JobExecution `json:"executions"`
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return err
	}

	now := time.Now()
	var missedJobs []*Job

	for _, job := range store.Jobs {
		job.cronEntryID = 0 // Reset on load

		// Clear stuck runs (older than 2h)
		if job.Status == StatusRunning && !job.LastRunAt.IsZero() {
			if now.Sub(job.LastRunAt) > stuckRunThreshold {
				s.logger.Info("clearing stuck run",
					zap.String("id", job.ID),
					zap.Duration("age", now.Sub(job.LastRunAt)),
				)
				job.Status = StatusIdle
				job.ConsecutiveErrors++
			}
		}

		s.jobs[job.ID] = job

		// Re-schedule enabled cron jobs
		if job.Enabled && job.Schedule == ScheduleCron {
			entryID, err := s.cronScheduler.AddFunc(job.CronExpr, func() {
				s.executeJob(job.ID)
			})
			if err != nil {
				s.logger.Warn("failed to reschedule cron job",
					zap.String("id", job.ID),
					zap.Error(err),
				)
				continue
			}
			job.cronEntryID = entryID
		}

		// Compute next run and collect missed jobs (skip manual tasks)
		if job.Enabled && job.Status != StatusError && job.Schedule != ScheduleManual {
			s.computeNextRun(job)
			if job.NextRunAt.Before(now) {
				missedJobs = append(missedJobs, job)
			}
		}
	}
	s.executions = store.Executions
	if s.executions == nil {
		s.executions = make([]*JobExecution, 0)
	}

	// Startup catch-up: run max N missed jobs immediately, stagger the rest
	if len(missedJobs) > 0 {
		s.logger.Info("startup catch-up",
			zap.Int("missed", len(missedJobs)),
			zap.Int("maxImmediate", maxMissedOnStartup),
		)
		// Sort by nextRunAt (earliest first)
		for i := 0; i < len(missedJobs); i++ {
			job := missedJobs[i]
			if i < maxMissedOnStartup {
				s.logger.Info("running missed job immediately",
					zap.String("id", job.ID),
					zap.String("name", job.Name),
				)
				go s.executeJob(job.ID)
			} else {
				stagger := time.Duration(i-maxMissedOnStartup+1) * time.Duration(missedJobStaggerMs) * time.Millisecond
				job.NextRunAt = now.Add(stagger)
				s.logger.Info("staggering missed job",
					zap.String("id", job.ID),
					zap.Duration("delay", stagger),
				)
			}
		}
	}

	return nil
}

var defaultHTTPClient *http.Client

func httpClient() *http.Client {
	if defaultHTTPClient == nil {
		defaultHTTPClient = &http.Client{
			Timeout: 30 * time.Second,
		}
	}
	return defaultHTTPClient
}
