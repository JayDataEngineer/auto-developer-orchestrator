package scheduler

import (
	"context"
	"encoding/json"
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
	DeliveryStore   DeliveryMode = "store"   // Save to run log only (default)
	DeliveryWebhook DeliveryMode = "webhook" // POST JSON to URL
	DeliverySession DeliveryMode = "session" // Inject into main agent session
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
	maxMissedOnStartup   = 5
	missedJobStaggerMs   = 5000
	stuckRunThreshold    = 2 * time.Hour
)

// JobStatus tracks the runtime state of a job.
type JobStatus string

const (
	StatusIdle     JobStatus = "idle"
	StatusRunning  JobStatus = "running"
	StatusError    JobStatus = "error"
	StatusDisabled JobStatus = "disabled"
)

// Job represents a scheduled job that sends prompts to Pux.
type Job struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Project     string       `json:"project"`
	AgentID     string       `json:"agentId,omitempty"`
	Message     string       `json:"message"`
	Model       string       `json:"model,omitempty"`
	Org         string       `json:"org,omitempty"` // --org flag equivalent
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
	DeliveryMode       DeliveryMode `json:"deliveryMode,omitempty"`
	DeliveryWebhookURL string       `json:"deliveryWebhookUrl,omitempty"`
	DeliveryBestEffort bool         `json:"deliveryBestEffort,omitempty"`
	// Failure alerts
	FailureAlertAfter      int    `json:"failureAlertAfter,omitempty"`
	FailureAlertWebhookURL string `json:"failureAlertWebhookUrl,omitempty"`
	// Runtime state
	Status            JobStatus `json:"status"`
	LastRunAt         time.Time `json:"lastRunAt,omitempty"`
	LastRunStatus     string    `json:"lastRunStatus,omitempty"`
	LastError         string    `json:"lastError,omitempty"`
	NextRunAt         time.Time `json:"nextRunAt,omitempty"`
	ConsecutiveErrors int       `json:"consecutiveErrors"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
	// Metrics (from execution results)
	InputTokens  int   `json:"inputTokens,omitempty"`
	OutputTokens int   `json:"outputTokens,omitempty"`
	DurationMs   int64 `json:"durationMs,omitempty"`
	// Dependencies (task-like)
	Blocks    []string `json:"blocks,omitempty"`    // job IDs this blocks
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

// PromptSender is a function that sends a prompt to the /api/pux/prompt endpoint and returns the response text.
type PromptSender func(ctx context.Context, project, agentID, message, model, org string, autoBranch, autoMerge bool) (string, error)

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

	// Run log manager
	runLogMgr   *RunLogManager
	projectRoot string

	// Session delivery
	sessionInjector SessionInjector

	// (removed: non-contract subscriber)
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

// SetRunLogManager configures the run log manager and project root.
func (s *Scheduler) SetRunLogManager(runLogMgr *RunLogManager, projectRoot string) {
	s.runLogMgr = runLogMgr
	s.projectRoot = projectRoot
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
	// Try under projects/ subdirectory
	candidate = filepath.Join(s.projectRoot, "projects", project)
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate
	}
	// Try as absolute path
	if info, err := os.Stat(project); err == nil && info.IsDir() {
		return project
	}
	return ""
}

// save writes jobs to the store file.
func (s *Scheduler) save() {
	if s.storePath == "" {
		return
	}

	store := struct {
		Jobs       []*Job          `json:"jobs"`
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
