package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/util"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

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

	// Execute via PromptSender — calls /api/pux/prompt (same as orch agent prompt)
	var output string
	var err error
	if s.promptSender != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		output, err = s.promptSender(ctx, job.Project, job.AgentID, job.Message, job.Model, job.Org, job.AutoBranch, job.AutoMerge)
	} else {
		err = fmt.Errorf("no PromptSender configured for scheduled jobs")
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
		execution.Error = util.Truncate(err.Error(), 2000)
		job.LastRunStatus = "error"
		job.LastError = util.Truncate(err.Error(), 500)
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
		execution.Output = util.Truncate(output, 5000)
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
			Summary:     util.Truncate(output, 500),
			RunAtMs:     execution.StartedAt.UnixMilli(),
			DurationMs:  execution.EndedAt.Sub(execution.StartedAt).Milliseconds(),
			NextRunAtMs: nextRunMs,
			Model:       job.Model,
		})
	}

	s.save()

	// Log failure — no SSE event emitted (non-contract events are prohibited)
	if execution.Status == "error" {
		s.logger.Warn("scheduled job failed",
			zap.String("job", job.Name),
			zap.String("error", execution.Error),
		)
	}
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
				fmt.Sprintf("job-scheduled: %s: %s", job.Name, util.Truncate(output, 1000)))
		}
	}
}

// deliverWebhook POSTs job output to a webhook URL.
func (s *Scheduler) deliverWebhook(job *Job, output string) {
	payload := map[string]interface{}{
		"jobId":   job.ID,
		"jobName": job.Name,
		"status":  "ok",
		"output":  util.Truncate(output, 5000),
		"runAt":   time.Now().Format(time.RFC3339),
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
		"jobId":             job.ID,
		"jobName":           job.Name,
		"status":            "error",
		"error":             errMsg,
		"consecutiveErrors": job.ConsecutiveErrors,
		"alertAt":           time.Now().Format(time.RFC3339),
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
