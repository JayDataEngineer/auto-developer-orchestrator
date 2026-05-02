package scheduler

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// CreateJobFromManifest creates a cron job from a pux.yaml schedule entry.
// This implements the ScheduleRegisterer interface used by ProjectHandler.
func (s *Scheduler) CreateJobFromManifest(project, name, cronExpr, promptText, description string) (string, error) {
	job := &Job{
		Name:        name,
		Description: description,
		Project:     project,
		Message:     promptText,
		Schedule:    ScheduleCron,
		CronExpr:    cronExpr,
		Enabled:     true,
	}
	if err := s.CreateJob(job); err != nil {
		return "", err
	}
	return job.ID, nil
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

// validateJob checks that a job has all required fields.
func (s *Scheduler) validateJob(job *Job) error {
	if job.Name == "" {
		return fmt.Errorf("name is required")
	}
	if job.Project == "" && job.Schedule != ScheduleManual {
		return fmt.Errorf("project is required for scheduled jobs")
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
