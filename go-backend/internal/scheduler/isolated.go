package scheduler

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// IsolatedExecutor runs Pi subprocesses for isolated job execution.
type IsolatedExecutor struct {
	logger      *zap.Logger
	piPath      string
	projectRoot string
}

// NewIsolatedExecutor creates an isolated executor.
func NewIsolatedExecutor(projectRoot string, logger *zap.Logger) (*IsolatedExecutor, error) {
	piPath, err := exec.LookPath("pi")
	if err != nil {
		return nil, fmt.Errorf("pi binary not found: %w", err)
	}
	return &IsolatedExecutor{
		logger:      logger,
		piPath:      piPath,
		projectRoot: projectRoot,
	}, nil
}

// JobResult is the output of an isolated job execution.
type JobResult struct {
	Output       string
	Error        string
	DurationMs   int64
	Model        string
	InputTokens  int
	OutputTokens int
	CacheTokens  int
}

// Execute runs a job in an isolated Pi subprocess.
func (e *IsolatedExecutor) Execute(ctx context.Context, jobID, jobName, projectPath, message, model string, timeoutSec int) *JobResult {
	start := time.Now()
	if timeoutSec <= 0 {
		timeoutSec = 600
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	systemPrompt := fmt.Sprintf(`You are a scheduled job executor. Complete the following task and report results concisely.

Job: %s
Task: %s

Execute the task using your available tools. Report results as plain text.`, jobName, message)

	promptInput := fmt.Sprintf(`{"type":"prompt","message":%s}
`, jsonStr(message))

	shellCmd := fmt.Sprintf(`(echo %s; sleep 10) | %s --mode rpc --append-system-prompt %s %s`,
		shellEscape(promptInput),
		e.piPath,
		shellEscape(systemPrompt),
		modelFlag(model),
	)

	cmd := exec.CommandContext(ctx, "bash", "-c", shellCmd)
	cmd.Dir = projectPath
	cmd.Env = append(os.Environ(), fmt.Sprintf("PROJECT_DIR=%s", projectPath))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return &JobResult{Error: fmt.Sprintf("failed to create stdout: %v", err)}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return &JobResult{Error: fmt.Sprintf("failed to create stderr: %v", err)}
	}

	if err := cmd.Start(); err != nil {
		return &JobResult{Error: fmt.Sprintf("failed to start: %v", err)}
	}

	e.logger.Info("isolated Pi started", zap.String("job", jobID), zap.Int("pid", cmd.Process.Pid))

	// Collect stderr
	var stderrBuf strings.Builder
	var stderrMu sync.Mutex
	stderrDone := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
		for scanner.Scan() {
			stderrMu.Lock()
			stderrBuf.WriteString(scanner.Text() + "\n")
			stderrMu.Unlock()
		}
		close(stderrDone)
	}()

	// Collect output from stdout
	var output strings.Builder
	var agentEnded bool
	var mu sync.Mutex
	done := make(chan struct{})

	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var event map[string]json.RawMessage
			if err := json.Unmarshal(line, &event); err != nil {
				continue
			}

			eventType := ""
			if t, ok := event["type"]; ok {
				json.Unmarshal(t, &eventType)
			}

			mu.Lock()

			// Handle message_update events (text_delta and agent_end inside)
			if eventType == "message_update" {
				if ame, ok := event["assistantMessageEvent"]; ok {
					var ameObj struct {
						Type  string `json:"type"`
						Delta string `json:"delta"`
					}
					json.Unmarshal(ame, &ameObj)
					if ameObj.Type == "text_delta" {
						output.WriteString(ameObj.Delta)
					} else if ameObj.Type == "agent_end" {
						// Extract text from the agent_end message
						if msg, ok := event["message"]; ok {
							var msgObj struct {
								Content []struct {
									Type string `json:"type"`
									Text string `json:"text"`
								} `json:"content"`
							}
							json.Unmarshal(msg, &msgObj)
							for _, c := range msgObj.Content {
								if c.Type == "text" {
									output.WriteString(c.Text)
								}
							}
						}
						agentEnded = true
						close(done)
						mu.Unlock()
						return
					}
				}
				mu.Unlock()
				continue
			}

			// Handle direct agent_end event
			if eventType == "agent_end" {
				if msg, ok := event["message"]; ok {
					var msgObj struct {
						Content []struct {
							Type string `json:"type"`
							Text string `json:"text"`
						} `json:"content"`
					}
					json.Unmarshal(msg, &msgObj)
					for _, c := range msgObj.Content {
						if c.Type == "text" {
							output.WriteString(c.Text)
						}
					}
				}
				agentEnded = true
				close(done)
				mu.Unlock()
				return
			}

			mu.Unlock()
		}
		// Scanner finished without agent_end
		close(done)
	}()

	// Wait for completion or timeout
	select {
	case <-ctx.Done():
		cmd.Process.Kill()
		stdout.Close()
		<-done
		<-stderrDone
		mu.Lock()
		out := output.String()
		mu.Unlock()
		stderrMu.Lock()
		errStr := stderrBuf.String()
		stderrMu.Unlock()

		if ctx.Err() == context.DeadlineExceeded {
			return &JobResult{
				Output:     out,
				Error:      fmt.Sprintf("job execution timed out after %ds. stderr: %s", timeoutSec, truncateEllipsis(errStr, 200)),
				DurationMs: time.Since(start).Milliseconds(),
				Model:      model,
			}
		}
		return &JobResult{
			Output:     out,
			Error:      ctx.Err().Error(),
			DurationMs: time.Since(start).Milliseconds(),
			Model:      model,
		}
	case <-done:
		if err := cmd.Wait(); err != nil {
			stderrMu.Lock()
			errStr := stderrBuf.String()
			stderrMu.Unlock()
			mu.Lock()
			out := output.String()
			ended := agentEnded
			mu.Unlock()

			if ended {
				return &JobResult{
					Output:     out,
					DurationMs: time.Since(start).Milliseconds(),
					Model:      model,
				}
			}
			return &JobResult{
				Output:     out,
				Error:      fmt.Sprintf("pi process exited: %v. stderr: %s", err, truncateEllipsis(errStr, 200)),
				DurationMs: time.Since(start).Milliseconds(),
				Model:      model,
			}
		}
		<-stderrDone

		mu.Lock()
		out := output.String()
		ended := agentEnded
		mu.Unlock()

		if !ended && out == "" {
			stderrMu.Lock()
			errStr := stderrBuf.String()
			stderrMu.Unlock()
			if errStr != "" {
				return &JobResult{
					Output:     "",
					Error:      fmt.Sprintf("pi produced no output. stderr: %s", truncateEllipsis(errStr, 500)),
					DurationMs: time.Since(start).Milliseconds(),
					Model:      model,
				}
			}
		}

		return &JobResult{
			Output:     out,
			DurationMs: time.Since(start).Milliseconds(),
			Model:      model,
		}
	}
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func modelFlag(model string) string {
	if model == "" {
		return ""
	}
	return "--model litellm/" + model
}

func (e *IsolatedExecutor) resolveProjectPath(project string) string {
	if project == "" {
		return ""
	}
	candidate := filepath.Join(e.projectRoot, project)
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate
	}
	if info, err := os.Stat(project); err == nil && info.IsDir() {
		return project
	}
	return ""
}
