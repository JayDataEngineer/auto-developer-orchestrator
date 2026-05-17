// Package truncate provides line-aware truncation for tool outputs.
//
// Two strategies:
//   - TruncateHead: keeps the first N lines/bytes (file reads)
//   - TruncateTail: keeps the last N lines/bytes (bash output)
//
// Both respect line boundaries — never splits mid-line.
// Ported from Pi-Mono's truncate.ts with the same semantics.
package truncate

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Limits used across tools.
const (
	FileMaxLines = 2000           // max lines for file reads
	FileMaxBytes = 50 * 1024      // 50KB for file reads
	BashMaxChars = 30000          // ~30KB for bash output
	LineMaxChars = 2000           // per-line truncation threshold
	GrepLineMax  = 500            // per-line truncation for grep matches
)

// Result holds metadata about a truncation operation.
type Result struct {
	Content    string // truncated (or original) content
	Truncated  bool   // whether truncation occurred
	TruncatedBy string // "lines", "bytes", or "" if not truncated
	TotalLines int    // total lines in original content
	OutputLines int   // lines in the output content
	TotalBytes  int    // bytes in original content
	OutputBytes int    // bytes in the output content
	FirstLineExceedsLimit bool // first line alone exceeds byte limit
}

// FormatSize returns a human-readable byte size.
func FormatSize(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
}

// Head truncates content from the top, keeping the first N lines/bytes.
// Suitable for file reads where you want to see the beginning.
// Never returns partial lines. If the first line alone exceeds maxBytes,
// returns empty content with FirstLineExceedsLimit=true.
func Head(content string, maxLines, maxBytes int) Result {
	if maxLines <= 0 {
		maxLines = FileMaxLines
	}
	if maxBytes <= 0 {
		maxBytes = FileMaxBytes
	}

	totalBytes := len(content)
	lines := splitLines(content)
	totalLines := len(lines)

	// No truncation needed
	if totalLines <= maxLines && totalBytes <= maxBytes {
		return Result{
			Content:     content,
			Truncated:   false,
			TotalLines:  totalLines,
			OutputLines: totalLines,
			TotalBytes:  totalBytes,
			OutputBytes: totalBytes,
		}
	}

	// Check if first line alone exceeds byte limit
	firstLineBytes := len(lines[0])
	if firstLineBytes > maxBytes {
		return Result{
			Content:     "",
			Truncated:   true,
			TruncatedBy: "bytes",
			TotalLines:  totalLines,
			TotalBytes:  totalBytes,
			FirstLineExceedsLimit: true,
		}
	}

	// Collect complete lines that fit
	var output []string
	outputBytes := 0
	truncatedBy := "lines"

	for i, line := range lines {
		if i >= maxLines {
			truncatedBy = "lines"
			break
		}
		lineBytes := len(line)
		if i > 0 {
			lineBytes++ // +1 for newline
		}
		if outputBytes+lineBytes > maxBytes {
			truncatedBy = "bytes"
			break
		}
		output = append(output, line)
		outputBytes += lineBytes
	}

	joined := strings.Join(output, "\n")
	return Result{
		Content:     joined,
		Truncated:   true,
		TruncatedBy: truncatedBy,
		TotalLines:  totalLines,
		OutputLines: len(output),
		TotalBytes:  totalBytes,
		OutputBytes: len(joined),
	}
}

// Tail truncates content keeping the last N lines/bytes.
// Suitable for bash output where you want to see errors and final results.
// May partially truncate the first output line if it exceeds maxBytes.
func Tail(content string, maxLines, maxBytes int) Result {
	if maxLines <= 0 {
		maxLines = FileMaxLines
	}
	if maxBytes <= 0 {
		maxBytes = FileMaxBytes
	}

	totalBytes := len(content)
	lines := splitLines(content)
	totalLines := len(lines)

	// No truncation needed
	if totalLines <= maxLines && totalBytes <= maxBytes {
		return Result{
			Content:     content,
			Truncated:   false,
			TotalLines:  totalLines,
			OutputLines: totalLines,
			TotalBytes:  totalBytes,
			OutputBytes: totalBytes,
		}
	}

	// Work backwards from the end
	var output []string
	outputBytes := 0

	for i := len(lines) - 1; i >= 0 && len(output) < maxLines; i-- {
		line := lines[i]
		lineBytes := len(line)
		if len(output) > 0 {
			lineBytes++ // +1 for newline
		}
		if outputBytes+lineBytes > maxBytes {
			// If we haven't added any lines yet and this line is huge,
			// take the end of it (partial)
			if len(output) == 0 {
				truncated := truncateStringFromEnd(line, maxBytes)
				output = append([]string{truncated}, output...)
				outputBytes = len(truncated)
			}
			break
		}
		output = append([]string{line}, output...)
		outputBytes += lineBytes
	}

	joined := strings.Join(output, "\n")
	return Result{
		Content:     joined,
		Truncated:   true,
		TruncatedBy: "bytes",
		TotalLines:  totalLines,
		OutputLines: len(output),
		TotalBytes:  totalBytes,
		OutputBytes: len(joined),
	}
}

// MiddleOut keeps the first half and last half of the content,
// truncating the middle. OpenCode-style for bash output.
func MiddleOut(content string, maxChars int) Result {
	if maxChars <= 0 {
		maxChars = BashMaxChars
	}

	totalBytes := len(content)
	if totalBytes <= maxChars {
		lines := splitLines(content)
		return Result{
			Content:     content,
			Truncated:   false,
			TotalLines:  len(lines),
			OutputLines: len(lines),
			TotalBytes:  totalBytes,
			OutputBytes: totalBytes,
		}
	}

	half := maxChars / 2
	start := content[:half]
	end := content[totalBytes-half:]
	middle := content[half : totalBytes-half]
	truncatedLines := strings.Count(middle, "\n") + 1

	result := fmt.Sprintf("%s\n\n... [%d lines truncated] ...\n\n%s", start, truncatedLines, end)
	lines := splitLines(result)
	return Result{
		Content:     result,
		Truncated:   true,
		TruncatedBy: "bytes",
		TotalLines:  strings.Count(content, "\n") + 1,
		OutputLines: len(lines),
		TotalBytes:  totalBytes,
		OutputBytes: len(result),
	}
}

// Line truncates a single line to maxChars, adding a suffix.
func Line(line string, maxChars int) string {
	if maxChars <= 0 {
		maxChars = LineMaxChars
	}
	if utf8.RuneCountInString(line) <= maxChars {
		return line
	}
	// Truncate by runes to avoid breaking multi-byte characters
	runes := []rune(line)
	return string(runes[:maxChars]) + "... [truncated]"
}

// FormatFileContinuation builds the actionable continuation message for file reads.
// startLine and limit are 1-indexed (what the model passes).
// fileTotalLines is the true total line count of the original file (not the truncated slice).
func FormatFileContinuation(tr Result, startLine, userLimit, fileTotalLines int) string {
	if !tr.Truncated && userLimit <= 0 {
		return ""
	}

	if tr.FirstLineExceedsLimit {
		return fmt.Sprintf(
			"[Line %d is %s, exceeds %s limit. Use bash: sed -n '%dp' <file> | head -c %d]",
			startLine, FormatSize(tr.TotalBytes), FormatSize(FileMaxBytes),
			startLine, FileMaxBytes,
		)
	}

	if tr.Truncated {
		endLine := startLine + tr.OutputLines - 1
		nextOffset := endLine + 1
		if tr.TruncatedBy == "lines" {
			return fmt.Sprintf(
				"\n\n[Showing lines %d-%d of %d. Use offset=%d to continue.]",
				startLine, endLine, fileTotalLines, nextOffset,
			)
		}
		return fmt.Sprintf(
			"\n\n[Showing lines %d-%d of %d (%s limit). Use offset=%d to continue.]",
			startLine, endLine, fileTotalLines, FormatSize(FileMaxBytes), nextOffset,
		)
	}

	// User-specified limit stopped before end of file
	if userLimit > 0 {
		remaining := fileTotalLines - (startLine - 1 + userLimit)
		if remaining > 0 {
			return fmt.Sprintf(
				"\n\n[%d more lines in file. Use offset=%d to continue.]",
				remaining, startLine+userLimit,
			)
		}
	}

	return ""
}

// FormatBashTruncation builds the truncation notice for bash output.
func FormatBashTruncation(tr Result) string {
	if !tr.Truncated {
		return ""
	}
	removed := tr.TotalLines - tr.OutputLines
	return fmt.Sprintf("\n\n... [%d lines truncated, showing last %d] ...", removed, tr.OutputLines)
}

// splitLines splits content into lines, preserving empty lines.
// Unlike strings.Split, does not produce a trailing empty element
// for content ending in \n.
func splitLines(content string) []string {
	if content == "" {
		return []string{}
	}
	lines := strings.Split(content, "\n")
	// Remove trailing empty element from trailing newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// truncateStringFromEnd takes the last maxBytes bytes of a string,
// respecting UTF-8 boundaries.
func truncateStringFromEnd(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	start := len(s) - maxBytes
	// Find valid UTF-8 boundary
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}
