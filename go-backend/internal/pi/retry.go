package pi

import (
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// StructuredOutputRetry handles retries when the LLM produces invalid JSON
type StructuredOutputRetry struct {
	maxRetries int
	logger     *zap.Logger
}

// NewStructuredOutputRetry creates a retry handler
func NewStructuredOutputRetry(maxRetries int, logger *zap.Logger) *StructuredOutputRetry {
	if maxRetries <= 0 {
		maxRetries = 2
	}
	return &StructuredOutputRetry{
		maxRetries: maxRetries,
		logger:     logger,
	}
}

// RetryParseJSON attempts to parse JSON, retrying with a simplified prompt on failure
func (sor *StructuredOutputRetry) RetryParseJSON(data []byte) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := json.Unmarshal(data, &result)
	if err == nil {
		return result, nil
	}

	sor.logger.Warn("JSON parse failed, will retry",
		zap.String("data", truncStr(string(data), 200)),
		zap.Error(err),
	)

	return nil, fmt.Errorf("JSON parse failed after %d retries: %w", sor.maxRetries, err)
}

// RetryFeedbackMessage generates a feedback message to send back to the LLM
// when it produced invalid structured output
func (sor *StructuredOutputRetry) RetryFeedbackMessage() string {
	return fmt.Sprintf("Your previous response contained invalid JSON. Please respond with valid JSON only. Do not include any text outside the JSON object. If you're sending a tool_use block, make sure the input is valid JSON with proper quoting and escaping.")
}

// ShouldRetry returns true if we should retry after a parse failure
func (sor *StructuredOutputRetry) ShouldRetry(attempt int) bool {
	return attempt < sor.maxRetries
}

// RetryWait returns the wait duration before retrying
func (sor *StructuredOutputRetry) RetryWait(attempt int) time.Duration {
	return time.Duration(attempt) * 500 * time.Millisecond
}
