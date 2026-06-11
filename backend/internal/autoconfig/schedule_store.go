package autoconfig

import (
	"context"
	"fmt"
	"sort"
	"time"

	schedulertool "github.com/auto-developer-orchestrator/backend/internal/tools/scheduler"
)

// ScheduleStore adapts schedulertool.Backend to the ArtifactStore contract.
// Follows the same pattern as WorkerStore and ProfileStore.
type ScheduleStore struct {
	backend schedulertool.Backend
}

// NewScheduleStore creates a ScheduleStore wrapping a schedulertool.Backend.
func NewScheduleStore(backend schedulertool.Backend) *ScheduleStore {
	return &ScheduleStore{backend: backend}
}

// List returns all schedule names.
func (s *ScheduleStore) List(ctx context.Context) (any, error) {
	jobs := s.backend.ListJobsInfo()
	names := make([]string, 0, len(jobs))
	seen := map[string]bool{}
	for _, j := range jobs {
		if !seen[j.Name] {
			names = append(names, j.Name)
			seen[j.Name] = true
		}
	}
	sort.Strings(names)
	return ListResult(names, len(names)), nil
}

// Get returns a single schedule's full detail.
func (s *ScheduleStore) Get(ctx context.Context, name string) (any, error) {
	job := s.backend.FindJobByNameOrID(name)
	if job == nil {
		return nil, fmt.Errorf("schedule %q not found", name)
	}
	return map[string]any{
		"id":                 job.ID,
		"name":               job.Name,
		"description":        job.Description,
		"project":            job.Project,
		"message":            job.Message,
		"model":              job.Model,
		"scheduleType":       job.Schedule,
		"cronExpr":           job.CronExpr,
		"everySeconds":       job.EverySeconds,
		"atTime":             job.AtTime,
		"enabled":            job.Enabled,
		"status":             job.Status,
		"lastRunAt":          formatTime(job.LastRunAt),
		"lastRunStatus":      job.LastRunStatus,
		"lastError":          job.LastError,
		"nextRunAt":          formatTime(job.NextRunAt),
		"consecutiveErrors":  job.ConsecutiveErrors,
		"inputTokens":        job.InputTokens,
		"outputTokens":       job.OutputTokens,
		"durationMs":         job.DurationMs,
	}, nil
}

// Put creates or updates a schedule. If a schedule with the given name exists
// it updates; otherwise it creates a new one.
func (s *ScheduleStore) Put(ctx context.Context, name string, spec map[string]any) (any, error) {
	existing := s.backend.FindJobByNameOrID(name)

	message, _ := spec["message"].(string)
	project, _ := spec["project"].(string)

	if existing != nil {
		var enabled *bool
		if v, ok := spec["enabled"].(bool); ok {
			enabled = &v
		}

		scheduleType, _ := spec["scheduleType"].(string)
		cronExpr, _ := spec["cronExpr"].(string)
		atTime, _ := spec["atTime"].(string)
		newName, _ := spec["name"].(string)
		description, _ := spec["description"].(string)
		model, _ := spec["model"].(string)
		var everySeconds int64
		if v, ok := spec["everySeconds"].(float64); ok {
			everySeconds = int64(v)
		}

		err := s.backend.UpdateJobParams(
			existing.ID, newName, message, project, model, description,
			scheduleType, cronExpr, atTime, everySeconds, enabled,
		)
		if err != nil {
			return nil, fmt.Errorf("update schedule: %w", err)
		}
		return TextResult(fmt.Sprintf("Schedule %q updated.", name)), nil
	}

	scheduleType, _ := spec["scheduleType"].(string)
	if scheduleType == "" {
		scheduleType = "manual"
	}
	cronExpr, _ := spec["cronExpr"].(string)
	atTime, _ := spec["atTime"].(string)
	description, _ := spec["description"].(string)
	model, _ := spec["model"].(string)
	var everySeconds int64
	if v, ok := spec["everySeconds"].(float64); ok {
		everySeconds = int64(v)
	}
	enabled := true
	if v, ok := spec["enabled"].(bool); ok {
		enabled = v
	}

	id, err := s.backend.CreateJobParams(
		name, project, message, scheduleType, cronExpr, atTime,
		description, model, everySeconds, enabled,
	)
	if err != nil {
		return nil, fmt.Errorf("create schedule: %w", err)
	}

	return TextResult(fmt.Sprintf("Schedule %q created (ID: %s).", name, id)), nil
}

// Delete removes a schedule by name or ID.
func (s *ScheduleStore) Delete(ctx context.Context, name string) error {
	job := s.backend.FindJobByNameOrID(name)
	if job == nil {
		return fmt.Errorf("schedule %q not found", name)
	}
	return s.backend.DeleteJob(job.ID)
}

// Backend returns the underlying schedulertool.Backend for extras (trigger, runs).
func (s *ScheduleStore) Backend() schedulertool.Backend {
	return s.backend
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
