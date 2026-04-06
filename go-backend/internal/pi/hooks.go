package pi

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// HookType identifies which hook to run
type HookType string

const (
	HookPreToolUse      HookType = "pre-tool-use"
	HookPostToolUse     HookType = "post-tool-use"
	HookOnToolFailure   HookType = "on-tool-failure"
)

// HookAction is the decision a hook makes
type HookAction string

const (
	HookActionAllow  HookAction = "allow"
	HookActionDeny   HookAction = "deny"
	HookActionAbort  HookAction = "abort"
	HookActionRetry  HookAction = "retry"
)

// HookPreToolUseInput is sent to the pre-tool-use hook
type HookPreToolUseInput struct {
	ToolName string                 `json:"tool_name"`
	Input    map[string]interface{} `json:"input"`
	TurnID   string                 `json:"turn_id"`
}

// HookPreToolUseOutput is what the hook returns
type HookPreToolUseOutput struct {
	Action       HookAction           `json:"action"`
	UpdatedInput map[string]interface{} `json:"updated_input,omitempty"`
	Reason       string               `json:"reason,omitempty"`
}

// HookPostToolUseInput is sent to the post-tool-use hook
type HookPostToolUseInput struct {
	ToolName string                 `json:"tool_name"`
	Input    map[string]interface{} `json:"input"`
	Output   string                 `json:"output"`
	TurnID   string                 `json:"turn_id"`
}

// HookPostToolUseOutput is what the hook returns
type HookPostToolUseOutput struct {
	OK       bool   `json:"ok"`
	Feedback string `json:"feedback,omitempty"`
}

// HookOnToolFailureInput is sent when a tool fails
type HookOnToolFailureInput struct {
	ToolName string                 `json:"tool_name"`
	Input    map[string]interface{} `json:"input"`
	Error    string                 `json:"error"`
	TurnID   string                 `json:"turn_id"`
}

// HookOnToolFailureOutput is what the hook returns
type HookOnToolFailureOutput struct {
	Action     HookAction           `json:"action"`
	RetryInput map[string]interface{} `json:"retry_input,omitempty"`
	Reason     string               `json:"reason,omitempty"`
}

// HookManager runs hooks for tool lifecycle events
type HookManager struct {
	hooksDir string
	logger   *zap.Logger
}

// NewHookManager creates a hook manager for the given .pi/hooks directory
func NewHookManager(hooksDir string, logger *zap.Logger) *HookManager {
	return &HookManager{
		hooksDir: hooksDir,
		logger:   logger,
	}
}

// RunPreToolUse runs the pre-tool-use hook
func (hm *HookManager) RunPreToolUse(turnID, toolName string, input map[string]interface{}) (*HookPreToolUseOutput, error) {
	script := hm.scriptPath("pre-tool-use")
	if script == "" {
		return nil, nil // no hook registered
	}

	payload := HookPreToolUseInput{
		ToolName: toolName,
		Input:    input,
		TurnID:   turnID,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("hook marshal error: %w", err)
	}

	out, err := hm.runHook(script, map[string]string{
		"HOOK_EVENT":     string(HookPreToolUse),
		"HOOK_TOOL_NAME": toolName,
		"HOOK_TOOL_INPUT": string(data),
		"HOOK_TURN_ID":   turnID,
	})
	if err != nil {
		hm.logger.Warn("pre-tool-use hook failed", zap.String("tool", toolName), zap.Error(err))
		return nil, nil // hook failure doesn't block execution
	}

	var result HookPreToolUseOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &result); err != nil {
		hm.logger.Warn("pre-tool-use hook returned invalid JSON", zap.String("tool", toolName), zap.String("output", out))
		return nil, nil
	}

	if result.Action == "" {
		result.Action = HookActionAllow
	}

	hm.logger.Debug("pre-tool-use hook result",
		zap.String("tool", toolName),
		zap.String("action", string(result.Action)),
		zap.String("reason", result.Reason),
	)

	return &result, nil
}

// RunPostToolUse runs the post-tool-use hook
func (hm *HookManager) RunPostToolUse(turnID, toolName string, input map[string]interface{}, output string) *HookPostToolUseOutput {
	script := hm.scriptPath("post-tool-use")
	if script == "" {
		return nil
	}

	payload := HookPostToolUseInput{
		ToolName: toolName,
		Input:    input,
		Output:   output,
		TurnID:   turnID,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil
	}

	out, err := hm.runHook(script, map[string]string{
		"HOOK_EVENT":       string(HookPostToolUse),
		"HOOK_TOOL_NAME":   toolName,
		"HOOK_TOOL_INPUT":  string(data),
		"HOOK_TOOL_OUTPUT": output,
		"HOOK_TURN_ID":     turnID,
	})
	if err != nil {
		hm.logger.Warn("post-tool-use hook failed", zap.String("tool", toolName), zap.Error(err))
		return nil
	}

	var result HookPostToolUseOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &result); err != nil {
		hm.logger.Warn("post-tool-use hook returned invalidJSON", zap.String("tool", toolName), zap.String("output", out))
		return nil
	}

	if result.Feedback != "" {
		hm.logger.Debug("post-tool-use hook feedback",
			zap.String("tool", toolName),
			zap.String("feedback", result.Feedback),
		)
	}

	return &result
}

// RunOnToolFailure runs the on-tool-failure hook
func (hm *HookManager) RunOnToolFailure(turnID, toolName string, input map[string]interface{}, errStr string) *HookOnToolFailureOutput {
	script := hm.scriptPath("on-tool-failure")
	if script == "" {
		return nil
	}

	payload := HookOnToolFailureInput{
		ToolName: toolName,
		Input:    input,
		Error:    errStr,
		TurnID:   turnID,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil
	}

	out, execErr := hm.runHook(script, map[string]string{
		"HOOK_EVENT":      string(HookOnToolFailure),
		"HOOK_TOOL_NAME":  toolName,
		"HOOK_TOOL_INPUT": string(data),
		"HOOK_ERROR":      errStr,
		"HOOK_TURN_ID":    turnID,
	})
	if execErr != nil {
		hm.logger.Warn("on-tool-failure hook failed", zap.String("tool", toolName), zap.Error(execErr))
		return nil
	}

	var result HookOnToolFailureOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &result); err != nil {
		hm.logger.Warn("on-tool-failure hook returned invalidJSON", zap.String("tool", toolName), zap.String("output", out))
		return nil
	}

	if result.Action == "" {
		result.Action = HookActionAbort
	}

	return &result
}

// scriptPath finds the executable script for a hook type
func (hm *HookManager) scriptPath(hookType string) string {
	// Check multiple possible extensions/shell types
	candidates := []string{
		hookType,
		hookType + ".sh",
		hookType + ".bash",
		hookType + ".py",
	}

	for _, name := range candidates {
		path := filepath.Join(hm.hooksDir, name)
		if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
			return path
		}
	}

	return ""
}

// runHook executes a hook script with environment variables and returns stdout
func (hm *HookManager) runHook(script string, env map[string]string) (string, error) {
	cmd := exec.Command(script)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Dir = hm.hooksDir

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("hook %s failed (exit %v): %s", script, err, string(out))
	}

	return string(out), nil
}
