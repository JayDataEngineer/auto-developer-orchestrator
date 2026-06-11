package scheduler

import (
	"fmt"
	"time"
)

// FormatSchedule returns a human-readable schedule string.
func FormatSchedule(schedule ScheduleType, cronExpr string, everySeconds int64, atTime string) string {
	switch schedule {
	case ScheduleCron:
		return "cron: " + cronExpr
	case ScheduleEvery:
		s := everySeconds
		if s < 60 {
			return fmt.Sprintf("every: %ds", s)
		}
		if s < 3600 {
			return fmt.Sprintf("every: %dm", s/60)
		}
		if s < 86400 {
			return fmt.Sprintf("every: %dh", s/3600)
		}
		return fmt.Sprintf("every: %dd", s/86400)
	case ScheduleAt:
		return "at: " + atTime
	case ScheduleManual:
		return "manual"
	default:
		return string(schedule)
	}
}

// FormatDuration returns a human-readable duration string.
func FormatDuration(ms int64) string {
	s := ms / 1000
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	if s < 3600 {
		return fmt.Sprintf("%dm", s/60)
	}
	if s < 86400 {
		return fmt.Sprintf("%dh", s/3600)
	}
	return fmt.Sprintf("%dd", s/86400)
}

// FormatTime formats a time for display.
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("Jan 02 15:04")
}
