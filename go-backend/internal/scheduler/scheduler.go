package scheduler

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/storage"
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
	runLogKeepN          = 2000
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
	LastOutput        string    `json:"lastOutput,omitempty"` // last successful output (for context chaining)
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

	// ContextFrom: job IDs whose last output is injected as context before execution.
	// Enables chaining: job A collects data → job B processes it.
	ContextFrom []string `json:"contextFrom,omitempty"`

	// Sandbox-only mode: job can only run bash/file ops inside its sandbox.
	// Blocks delegation, MCP, browser, desktop, memory, skills, etc.
	SandboxOnly bool `json:"sandboxOnly,omitempty"`

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
type PromptSender func(ctx context.Context, project, agentID, message, model, org string, autoBranch, autoMerge, sandboxOnly bool) (string, error)

// SessionInjector injects text into the main agent session.
type SessionInjector func(project, agentID, text string) error

// Scheduler manages scheduled jobs.
type Scheduler struct {
	jobs          map[string]*Job          // Write-through cache from DB
	executions    []*JobExecution          // In-memory only (ephemeral active runs)
	cronScheduler *cron.Cron
	promptSender  PromptSender
	logger        *zap.Logger
	db            *storage.Database
	mu            sync.RWMutex
	stopCh        chan struct{}

	projectRoot string

	// Session delivery
	sessionInjector SessionInjector
}

// NewScheduler creates a new scheduler backed by the database.
func NewScheduler(db *storage.Database, sender PromptSender, logger *zap.Logger) *Scheduler {
	return &Scheduler{
		jobs:          make(map[string]*Job),
		executions:    make([]*JobExecution, 0),
		cronScheduler: cron.New(cron.WithSeconds()),
		promptSender:  sender,
		logger:        logger,
		db:            db,
		stopCh:        make(chan struct{}),
	}
}

// Start loads persisted jobs from the database and starts the scheduler.
func (s *Scheduler) Start(ctx context.Context) error {
	if err := s.load(ctx); err != nil {
		s.logger.Warn("failed to load scheduler state, starting fresh", zap.Error(err))
	}

	s.cronScheduler.Start()

	// Start interval/at timers
	go s.runIntervalLoop(ctx)

	s.logger.Info("scheduler started", zap.Int("jobs", len(s.jobs)))
	return nil
}

// Stop gracefully stops the scheduler.
func (s *Scheduler) Stop() {
	close(s.stopCh)
	s.cronScheduler.Stop()
	s.logger.Info("scheduler stopped")
}

// SetSessionInjector sets the session delivery callback.
func (s *Scheduler) SetSessionInjector(fn SessionInjector) {
	s.sessionInjector = fn
}

// SetProjectRoot sets the project root path.
func (s *Scheduler) SetProjectRoot(root string) {
	s.projectRoot = root
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

// saveJob persists a single job to the database.
func (s *Scheduler) saveJob(job *Job) {
	ctx := context.Background()
	dbJob := jobToDB(job)
	if err := s.db.SaveScheduledJob(ctx, dbJob); err != nil {
		s.logger.Error("failed to persist job to database", zap.String("id", job.ID), zap.Error(err))
	}
}

// load reads jobs from the database, clears stuck runs, and catches up missed jobs.
func (s *Scheduler) load(ctx context.Context) error {
	dbJobs, err := s.db.ListScheduledJobs(ctx)
	if err != nil {
		return err
	}

	now := time.Now()
	var missedJobs []*Job

	for _, dbJob := range dbJobs {
		job := dbToJob(dbJob)
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

// ── Job <-> DB conversion ──────────────────────────────────────────────

func jobToDB(j *Job) *storage.ScheduledJob {
	var lastRunAt, nextRunAt, createdAt, updatedAt *time.Time
	if !j.LastRunAt.IsZero() {
		lastRunAt = &j.LastRunAt
	}
	if !j.NextRunAt.IsZero() {
		nextRunAt = &j.NextRunAt
	}
	if !j.CreatedAt.IsZero() {
		createdAt = &j.CreatedAt
	}
	if !j.UpdatedAt.IsZero() {
		updatedAt = &j.UpdatedAt
	}

	blocks, _ := json.Marshal(j.Blocks)
	blockedBy, _ := json.Marshal(j.BlockedBy)
	contextFrom, _ := json.Marshal(j.ContextFrom)

	return &storage.ScheduledJob{
		ID:                     j.ID,
		Name:                   j.Name,
		Description:            j.Description,
		Project:                j.Project,
		AgentID:                j.AgentID,
		Message:                j.Message,
		Model:                  j.Model,
		Org:                    j.Org,
		ScheduleType:           string(j.Schedule),
		CronExpr:               j.CronExpr,
		Timezone:               j.Timezone,
		EverySeconds:           j.EverySeconds,
		AtTime:                 j.AtTime,
		AutoBranch:             j.AutoBranch,
		AutoMerge:              j.AutoMerge,
		Enabled:                j.Enabled,
		DeliveryMode:           string(j.DeliveryMode),
		DeliveryWebhookURL:     j.DeliveryWebhookURL,
		DeliveryBestEffort:     j.DeliveryBestEffort,
		FailureAlertAfter:      j.FailureAlertAfter,
		FailureAlertWebhookURL: j.FailureAlertWebhookURL,
		Status:                 string(j.Status),
		LastRunAt:              lastRunAt,
		LastRunStatus:          j.LastRunStatus,
		LastError:              j.LastError,
		NextRunAt:              nextRunAt,
		ConsecutiveErrors:      j.ConsecutiveErrors,
		InputTokens:            j.InputTokens,
		OutputTokens:           j.OutputTokens,
		DurationMs:             j.DurationMs,
		Blocks:                 string(blocks),
		BlockedBy:              string(blockedBy),
		LastOutput:             j.LastOutput,
		ContextFrom:            string(contextFrom),
		SandboxOnly:            j.SandboxOnly,
		WebhookToken:           j.WebhookToken,
		CreatedAt:              createdAt,
		UpdatedAt:              updatedAt,
	}
}

func dbToJob(d *storage.ScheduledJob) *Job {
	j := &Job{
		ID:                     d.ID,
		Name:                   d.Name,
		Description:            d.Description,
		Project:                d.Project,
		AgentID:                d.AgentID,
		Message:                d.Message,
		Model:                  d.Model,
		Org:                    d.Org,
		Schedule:               ScheduleType(d.ScheduleType),
		CronExpr:               d.CronExpr,
		Timezone:               d.Timezone,
		EverySeconds:           d.EverySeconds,
		AtTime:                 d.AtTime,
		AutoBranch:             d.AutoBranch,
		AutoMerge:              d.AutoMerge,
		Enabled:                d.Enabled,
		DeliveryMode:           DeliveryMode(d.DeliveryMode),
		DeliveryWebhookURL:     d.DeliveryWebhookURL,
		DeliveryBestEffort:     d.DeliveryBestEffort,
		FailureAlertAfter:      d.FailureAlertAfter,
		FailureAlertWebhookURL: d.FailureAlertWebhookURL,
		Status:                 JobStatus(d.Status),
		LastRunStatus:          d.LastRunStatus,
		LastError:              d.LastError,
		ConsecutiveErrors:      d.ConsecutiveErrors,
		InputTokens:            d.InputTokens,
		OutputTokens:           d.OutputTokens,
		DurationMs:             d.DurationMs,
		SandboxOnly:            d.SandboxOnly,
		WebhookToken:           d.WebhookToken,
	}
	if d.LastRunAt != nil {
		j.LastRunAt = *d.LastRunAt
	}
	if d.NextRunAt != nil {
		j.NextRunAt = *d.NextRunAt
	}
	if d.CreatedAt != nil {
		j.CreatedAt = *d.CreatedAt
	}
	if d.UpdatedAt != nil {
		j.UpdatedAt = *d.UpdatedAt
	}
	_ = json.Unmarshal([]byte(d.Blocks), &j.Blocks)
	_ = json.Unmarshal([]byte(d.BlockedBy), &j.BlockedBy)
	_ = json.Unmarshal([]byte(d.ContextFrom), &j.ContextFrom)
	j.LastOutput = d.LastOutput
	return j
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
