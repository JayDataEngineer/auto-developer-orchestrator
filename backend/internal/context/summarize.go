package context

import (
	"context"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// SummarizeText summarizes arbitrary text using an LLM provider.
// Used for sub-agent result compression and explicit agent-triggered summarization.
// Returns the summarized text or an error if the provider fails.
func SummarizeText(ctx context.Context, provider core.LLMProvider, text string, targetChars int) (string, error) {
	if provider == nil || len(text) <= targetChars {
		return text, nil
	}

	// Cap input to avoid blowing up the summarization prompt
	input := text
	const maxInputChars = 12000
	if len(input) > maxInputChars {
		input = input[:maxInputChars] + "\n...[input truncated for summarization]"
	}

	prompt := `Summarize the following content concisely. Preserve:
1. Key findings, decisions, and results
2. Any errors encountered and how they were resolved
3. Important data, file paths, or values produced
4. The overall outcome and current state

Be concise but do not lose critical information.`

	msgs := []core.Message{
		{Role: "user", Content: prompt + "\n\n---\n\n" + input},
	}

	opts := core.GenerateOptions{
		MaxTokens:   min(targetChars/3, 1024),
		Temperature: 0.3,
		TopP:        0.9,
	}

	ch, err := provider.StreamChat(ctx, msgs, nil, opts)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	for evt := range ch {
		if evt.Type == core.ChatEventContent {
			b.WriteString(evt.Content)
		}
		if evt.Type == core.ChatEventError {
			return "", evt.Err
		}
	}

	result := strings.TrimSpace(b.String())
	if len(result) < 30 {
		return "", nil // too short to be useful
	}
	return result, nil
}
