package util

// Truncate clips s to maxLen characters without adding a suffix.
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// TruncateEllipsis clips s to maxLen characters and appends "...".
func TruncateEllipsis(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
