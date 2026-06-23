package llama

import (
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// TestBuildRequestThinkingKwargs proves that GenerateOptions.Thinking=true
// flows through buildRequest to set chat_template_kwargs.enable_thinking=true
// on the wire-format request for local llama-server. This is the end-to-end
// proof of the Fable/Mythos diligence prompt work (PR1): the YAML flag →
// RoleConfig → AgentRole → GenerateOptions → ChatCompletionRequest chain
// terminates here.
//
// Cloud-provider behavior is the negative case: the kwargs must NOT appear
// because cloud providers don't accept llama-server-specific fields.
func TestBuildRequestThinkingKwargs(t *testing.T) {
	mock := &mockLLMClient{}
	session, err := mock.NewSession(4096)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	t.Run("local llama-server with Thinking=true", func(t *testing.T) {
		opts := core.GenerateOptions{
			MaxTokens: 256,
			Thinking:  true,
		}
		req := session.buildRequest(opts)
		if req.ChatTemplateKwargs == nil {
			t.Fatal("ChatTemplateKwargs: got nil, want map with enable_thinking=true")
		}
		v, ok := req.ChatTemplateKwargs["enable_thinking"]
		if !ok {
			t.Fatalf("ChatTemplateKwargs missing 'enable_thinking' key; got: %v", req.ChatTemplateKwargs)
		}
		b, _ := v.(bool)
		if !b {
			t.Errorf("enable_thinking: got %v, want true", v)
		}
	})

	t.Run("local llama-server with Thinking=false omits kwargs", func(t *testing.T) {
		opts := core.GenerateOptions{
			MaxTokens: 256,
			Thinking:  false,
		}
		req := session.buildRequest(opts)
		if req.ChatTemplateKwargs != nil {
			t.Errorf("ChatTemplateKwargs: got %v, want nil when Thinking=false", req.ChatTemplateKwargs)
		}
	})
}

// cloudMockLLMClient is a mock that reports IsCloud=true, to exercise the
// sanitizer path. chat_template_kwargs must not survive onto the wire when
// the engine is a cloud provider.
type cloudMockLLMClient struct {
	mockLLMClient
}

func (c *cloudMockLLMClient) IsCloud() bool      { return true }
func (c *cloudMockLLMClient) ModelName() string  { return "cloud-model" }
func (c *cloudMockLLMClient) HasVision() bool    { return true }

func TestBuildRequestThinkingOmittedForCloud(t *testing.T) {
	cloud := &cloudMockLLMClient{}
	session := &Session{
		engine:    cloud,
		sessionID: "cloud-session",
		ctxSize:   4096,
		messages:  []Message{},
	}

	opts := core.GenerateOptions{
		MaxTokens: 256,
		Thinking:  true,
	}
	req := session.buildRequest(opts)
	if req.ChatTemplateKwargs != nil {
		t.Errorf("ChatTemplateKwargs: got %v, want nil for cloud provider", req.ChatTemplateKwargs)
	}
}
