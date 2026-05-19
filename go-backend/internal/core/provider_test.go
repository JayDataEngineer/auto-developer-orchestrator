package core

import (
	"encoding/json"
	"testing"
)

func TestRoleConstants(t *testing.T) {
	if RoleSystem != "system" {
		t.Errorf("RoleSystem = %q, want %q", RoleSystem, "system")
	}
	if RoleUser != "user" {
		t.Errorf("RoleUser = %q, want %q", RoleUser, "user")
	}
	if RoleAssistant != "assistant" {
		t.Errorf("RoleAssistant = %q, want %q", RoleAssistant, "assistant")
	}
	if RoleTool != "tool" {
		t.Errorf("RoleTool = %q, want %q", RoleTool, "tool")
	}
}

func TestFinishReasonConstants(t *testing.T) {
	if FinishStop != "stop" {
		t.Errorf("FinishStop = %q, want %q", FinishStop, "stop")
	}
	if FinishToolCalls != "tool_calls" {
		t.Errorf("FinishToolCalls = %q, want %q", FinishToolCalls, "tool_calls")
	}
}

func TestMessage_System(t *testing.T) {
	m := Message{Role: "system", Content: "You are a helpful assistant."}
	if m.Role != "system" {
		t.Errorf("Role = %q, want %q", m.Role, "system")
	}
	if m.Content != "You are a helpful assistant." {
		t.Errorf("Content = %q, want %q", m.Content, "You are a helpful assistant.")
	}
}

func TestMessage_User(t *testing.T) {
	m := Message{Role: "user", Content: "Hello!"}
	if m.Role != "user" {
		t.Errorf("Role = %q, want %q", m.Role, "user")
	}
}

func TestMessage_Assistant(t *testing.T) {
	m := Message{
		Role:    "assistant",
		Content: "I can help with that.",
		ToolCalls: []ToolCallResponse{
			{ID: "call_1", Type: "function", Function: FunctionCallData{Name: "bash", Arguments: `{"cmd":"ls"}`}},
		},
		ReasoningContent: "Let me think...",
	}
	if m.Role != "assistant" {
		t.Errorf("Role = %q, want %q", m.Role, "assistant")
	}
	if len(m.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(m.ToolCalls))
	}
	if m.ToolCalls[0].Function.Name != "bash" {
		t.Errorf("ToolCall name = %q, want %q", m.ToolCalls[0].Function.Name, "bash")
	}
	if m.ReasoningContent != "Let me think..." {
		t.Errorf("ReasoningContent = %q, want %q", m.ReasoningContent, "Let me think...")
	}
}

func TestMessage_Tool(t *testing.T) {
	m := Message{
		Role:       "tool",
		Content:    `{"output": "file contents"}`,
		ToolCallID: "call_1",
		Name:       "read_file",
	}
	if m.Role != "tool" {
		t.Errorf("Role = %q, want %q", m.Role, "tool")
	}
	if m.ToolCallID != "call_1" {
		t.Errorf("ToolCallID = %q, want %q", m.ToolCallID, "call_1")
	}
	if m.Name != "read_file" {
		t.Errorf("Name = %q, want %q", m.Name, "read_file")
	}
}

func TestToolCallResponse_ToToolCall(t *testing.T) {
	tcr := ToolCallResponse{
		ID:   "call_1",
		Type: "function",
		Function: FunctionCallData{
			Name:      "bash",
			Arguments: `{"command":"echo hello"}`,
		},
	}
	tc := tcr.ToToolCall()
	if tc.ID != "call_1" {
		t.Errorf("ToolCall.ID = %q, want %q", tc.ID, "call_1")
	}
	if tc.Name != "bash" {
		t.Errorf("ToolCall.Name = %q, want %q", tc.Name, "bash")
	}
	if tc.Args["command"] != "echo hello" {
		t.Errorf("ToolCall.Args = %v, want command=echo hello", tc.Args)
	}
}

func TestToolCallResponse_ToToolCall_InvalidJSON(t *testing.T) {
	tcr := ToolCallResponse{
		ID:   "call_1",
		Type: "function",
		Function: FunctionCallData{
			Name:      "bash",
			Arguments: `not-json`,
		},
	}
	tc := tcr.ToToolCall()
	if tc.Name != "bash" {
		t.Errorf("ToolCall.Name = %q, want %q", tc.Name, "bash")
	}
	if tc.Args["raw"] != "not-json" {
		t.Errorf("expected raw fallback, got %v", tc.Args)
	}
}

func TestToolCallResponse_ToToolCall_EmptyArgs(t *testing.T) {
	tcr := ToolCallResponse{
		ID:   "call_1",
		Type: "function",
		Function: FunctionCallData{
			Name:      "bash",
			Arguments: "",
		},
	}
	tc := tcr.ToToolCall()
	if tc.Name != "bash" {
		t.Errorf("ToolCall.Name = %q, want %q", tc.Name, "bash")
	}
}

func TestGenerateOptions_Default(t *testing.T) {
	opts := GenerateOptions{}
	// Default zero values
	if opts.MaxTokens != 0 {
		t.Errorf("expected 0, got %d", opts.MaxTokens)
	}
	if opts.Temperature != 0 {
		t.Errorf("expected 0, got %f", opts.Temperature)
	}
}

func TestGenerateOptions_Full(t *testing.T) {
	opts := GenerateOptions{
		MaxTokens:   4096,
		Temperature: 0.7,
		TopP:        0.9,
		TopK:        40,
	}
	if opts.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, want %d", opts.MaxTokens, 4096)
	}
	if opts.Temperature != 0.7 {
		t.Errorf("Temperature = %f, want %f", opts.Temperature, 0.7)
	}
	if opts.TopP != 0.9 {
		t.Errorf("TopP = %f, want %f", opts.TopP, 0.9)
	}
	if opts.TopK != 40 {
		t.Errorf("TopK = %d, want %d", opts.TopK, 40)
	}
}

func TestStreamUsage(t *testing.T) {
	u := StreamUsage{PromptTokens: 100, CompletionTokens: 50}
	if u.PromptTokens != 100 {
		t.Errorf("PromptTokens = %d, want %d", u.PromptTokens, 100)
	}
	if u.CompletionTokens != 50 {
		t.Errorf("CompletionTokens = %d, want %d", u.CompletionTokens, 50)
	}
}

func TestOpenAITool(t *testing.T) {
	tool := OpenAITool{
		Type: "function",
		Function: FunctionDef{
			Name:        "bash",
			Description: "Execute a bash command",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"cmd":{"type":"string"}}}`),
		},
	}
	if tool.Type != "function" {
		t.Errorf("Type = %q, want %q", tool.Type, "function")
	}
	if tool.Function.Name != "bash" {
		t.Errorf("Function.Name = %q, want %q", tool.Function.Name, "bash")
	}
}

func TestFunctionDef(t *testing.T) {
	fd := FunctionDef{
		Name:        "test",
		Description: "A test function",
		Parameters:  json.RawMessage(`{"type":"object"}`),
	}
	if fd.Name != "test" {
		t.Errorf("Name = %q, want %q", fd.Name, "test")
	}
	if fd.Description != "A test function" {
		t.Errorf("Description = %q, want %q", fd.Description, "A test function")
	}
}

func TestStreamDelta(t *testing.T) {
	delta := StreamDelta{
		Role:             "assistant",
		Content:          "Hello",
		ReasoningContent: "thinking...",
		Reasoning:        "more thinking",
		ToolCalls: []ToolCallDelta{
			{Index: 0, ID: "call_1", Type: "function"},
		},
	}
	if delta.Role != "assistant" {
		t.Errorf("Role = %q, want %q", delta.Role, "assistant")
	}
	if delta.Content != "Hello" {
		t.Errorf("Content = %q, want %q", delta.Content, "Hello")
	}
	if len(delta.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(delta.ToolCalls))
	}
	if delta.ToolCalls[0].ID != "call_1" {
		t.Errorf("ToolCall ID = %q, want %q", delta.ToolCalls[0].ID, "call_1")
	}
}

func TestToolCallDelta(t *testing.T) {
	delta := ToolCallDelta{
		Index: 0,
		ID:    "call_1",
		Type:  "function",
		Function: FunctionCallDelta{
			Name:      "bash",
			Arguments: `{"cmd":"ls"}`,
		},
	}
	_ = delta
}

func TestFunctionCallDelta(t *testing.T) {
	fcd := FunctionCallDelta{
		Name:      "bash",
		Arguments: `{"cmd":"ls"}`,
	}
	if fcd.Name != "bash" {
		t.Errorf("Name = %q, want %q", fcd.Name, "bash")
	}
	if fcd.Arguments != `{"cmd":"ls"}` {
		t.Errorf("Arguments = %q, want %q", fcd.Arguments, `{"cmd":"ls"}`)
	}
}

func TestFunctionCallData(t *testing.T) {
	fcd := FunctionCallData{
		Name:      "bash",
		Arguments: `{"cmd":"ls"}`,
	}
	if fcd.Name != "bash" {
		t.Errorf("Name = %q, want %q", fcd.Name, "bash")
	}
	if fcd.Arguments != `{"cmd":"ls"}` {
		t.Errorf("Arguments = %q, want %q", fcd.Arguments, `{"cmd":"ls"}`)
	}
}

func TestToolResult(t *testing.T) {
	tr := ToolResult{
		ToolCallID: "call_1",
		ToolName:   "bash",
		Content:    `{"output":"ok"}`,
	}
	if tr.ToolCallID != "call_1" {
		t.Errorf("ToolCallID = %q, want %q", tr.ToolCallID, "call_1")
	}
	if tr.ToolName != "bash" {
		t.Errorf("ToolName = %q, want %q", tr.ToolName, "bash")
	}
	if tr.Content != `{"output":"ok"}` {
		t.Errorf("Content = %q, want %q", tr.Content, `{"output":"ok"}`)
	}
}

func TestToolCall(t *testing.T) {
	tc := ToolCall{
		ID:   "call_1",
		Name: "bash",
		Args: map[string]any{"cmd": "ls"},
	}
	if tc.ID != "call_1" {
		t.Errorf("ID = %q, want %q", tc.ID, "call_1")
	}
	if tc.Name != "bash" {
		t.Errorf("Name = %q, want %q", tc.Name, "bash")
	}
	if tc.Args["cmd"] != "ls" {
		t.Errorf("Args = %v, want cmd=ls", tc.Args)
	}
}

func TestGenerateOptions_ResponseFormat(t *testing.T) {
	opts := GenerateOptions{
		MaxTokens: 2048,
		ResponseFormat: &ResponseFormat{
			Type: "json_object",
		},
	}
	if opts.ResponseFormat == nil {
		t.Fatal("expected ResponseFormat to be set")
	}
	if opts.ResponseFormat.Type != "json_object" {
		t.Errorf("Type = %q, want %q", opts.ResponseFormat.Type, "json_object")
	}

	// JSON serialization
	data, err := json.Marshal(opts.ResponseFormat)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	if string(data) != `{"type":"json_object"}` {
		t.Errorf("marshaled = %q, want %q", string(data), `{"type":"json_object"}`)
	}
}

func TestGenerateOptions_ResponseFormatWithSchema(t *testing.T) {
	opts := GenerateOptions{
		ResponseFormat: &ResponseFormat{
			Type: "json_schema",
			JSONSchema: &JSONSchemaFormat{
				Name:   "result",
				Schema: json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}}}`),
				Strict: true,
			},
		},
	}
	data, err := json.Marshal(opts.ResponseFormat)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if parsed["type"] != "json_schema" {
		t.Errorf("type = %v, want json_schema", parsed["type"])
	}
	schema, ok := parsed["json_schema"].(map[string]any)
	if !ok {
		t.Fatal("expected json_schema object")
	}
	if schema["name"] != "result" {
		t.Errorf("name = %v, want result", schema["name"])
	}
	if schema["strict"] != true {
		t.Errorf("strict = %v, want true", schema["strict"])
	}
}

func TestGenerateOptions_NilResponseFormat(t *testing.T) {
	opts := GenerateOptions{
		MaxTokens:   1024,
		Temperature: 0.5,
	}
	if opts.ResponseFormat != nil {
		t.Error("expected nil ResponseFormat by default")
	}
}

func TestMessage_JSONMarshaling(t *testing.T) {
	m := Message{
		Role:    "assistant",
		Content: "Hello",
		ToolCalls: []ToolCallResponse{
			{ID: "call_1", Type: "function", Function: FunctionCallData{Name: "bash", Arguments: `{}`}},
		},
		ReasoningContent: "thinking",
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	var unmarshaled Message
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if unmarshaled.Role != "assistant" {
		t.Errorf("role = %q, want %q", unmarshaled.Role, "assistant")
	}
	if unmarshaled.Content != "Hello" {
		t.Errorf("content = %q, want %q", unmarshaled.Content, "Hello")
	}
	if len(unmarshaled.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(unmarshaled.ToolCalls))
	}
}
