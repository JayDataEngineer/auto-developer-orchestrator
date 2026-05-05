package llama

import (
	"encoding/json"
	"fmt"
	"sync"
)

// mockLLMClient is a fake ChatProvider for testing agent loops without a real model.
type mockLLMClient struct {
	mu         sync.Mutex
	responses  []mockResponse
	callCount  int
	defaultMsg string
	blockCh    chan struct{} // if set, blocks until closed before each response
}

type mockResponse struct {
	content string
	toolMsg *mockToolMsg
	finish  FinishReason
}

type mockToolMsg struct {
	toolCallID string
	toolName   string
	args       map[string]interface{}
}

// newMockLLMClient creates a mock that returns the given response texts in sequence.
func newMockLLMClient(responses []string, defaultMsg string) *mockLLMClient {
	m := &mockLLMClient{defaultMsg: defaultMsg}
	for _, r := range responses {
		m.responses = append(m.responses, mockResponse{
			content: r,
			finish:  FinishStop,
		})
	}
	return m
}

// addToolCall queues a response that includes a tool call.
func (m *mockLLMClient) addToolCall(content, toolCallID, toolName string, args map[string]interface{}) {
	m.responses = append(m.responses, mockResponse{
		content: content,
		toolMsg: &mockToolMsg{toolCallID: toolCallID, toolName: toolName, args: args},
		finish:  FinishToolCalls,
	})
}

func (m *mockLLMClient) nextResponse() (*ChatCompletionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.callCount < len(m.responses) {
		resp := m.responses[m.callCount]
		m.callCount++
		return m.buildResponse(resp), nil
	}
	m.callCount++
	return m.buildResponse(mockResponse{
		content: m.defaultMsg,
		finish:  FinishStop,
	}), nil
}

func (m *mockLLMClient) buildResponse(r mockResponse) *ChatCompletionResponse {
	choice := ChatChoice{
		Message: ChatMessage{
			Role:    "assistant",
			Content: r.content,
		},
		FinishReason: r.finish,
	}

	if r.toolMsg != nil {
		argsJSON, _ := json.Marshal(r.toolMsg.args)
		choice.Message.ToolCalls = []ToolCallResponse{
			{
				ID:   r.toolMsg.toolCallID,
				Type: "function",
				Function: FunctionCallData{
					Name:      r.toolMsg.toolName,
					Arguments: string(argsJSON),
				},
			},
		}
	}

	return &ChatCompletionResponse{
		Choices: []ChatChoice{choice},
	}
}

// ChatProvider interface implementation

func (m *mockLLMClient) chatComplete(req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("mock: no messages in request")
	}
	// If blocked, wait for signal
	if m.blockCh != nil {
		<-m.blockCh
	}
	return m.nextResponse()
}

func (m *mockLLMClient) chatCompleteStream(req ChatCompletionRequest, onChunk func(delta StreamDelta, finish FinishReason, usage *StreamUsage) bool) error {
	resp, err := m.chatComplete(req)
	if err != nil {
		return err
	}

	for _, choice := range resp.Choices {
		// Stream reasoning content first (if present) in a separate chunk
		if choice.Message.ReasoningContent != "" {
			if !onChunk(StreamDelta{
				Role:             choice.Message.Role,
				ReasoningContent: choice.Message.ReasoningContent,
			}, "", nil) {
				return nil
			}
		}

		// Stream tool calls as separate deltas
		for _, tc := range choice.Message.ToolCalls {
			if !onChunk(StreamDelta{
				Role: choice.Message.Role,
				ToolCalls: []ToolCallDelta{{
					Index:    0,
					ID:       tc.ID,
					Type:     tc.Type,
					Function: FunctionCallDelta{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				}},
			}, "", nil) {
				return nil
			}
		}

		// Stream content
		usage := StreamUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
		}
		finish := choice.FinishReason
		if finish == "" {
			finish = FinishStop
		}
		if !onChunk(StreamDelta{
			Role:    choice.Message.Role,
			Content: choice.Message.Content,
		}, finish, &usage) {
			return nil
		}
	}
	return nil
}

func (m *mockLLMClient) CheckHealth() error { return nil }
func (m *mockLLMClient) IsLoaded() bool     { return true }
func (m *mockLLMClient) ModelName() string  { return "mock" }
func (m *mockLLMClient) IsCloud() bool      { return false }
func (m *mockLLMClient) HasVision() bool    { return false }
func (m *mockLLMClient) WarmUp() error      { return nil }
func (m *mockLLMClient) Close() error       { return nil }
func (m *mockLLMClient) NewSession(ctxSize int) (*Session, error) {
	return &Session{
		engine:    m,
		sessionID: "mock-session",
		ctxSize:   ctxSize,
		messages:  []Message{},
	}, nil
}
