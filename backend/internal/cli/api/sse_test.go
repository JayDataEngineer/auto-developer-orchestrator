package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestStreamSSETextDelta(t *testing.T) {
	input := "event: text_delta\ndata: {\"text\":\"hello world\"}\n\n"
	ch := make(chan SSEEvent, 10)

	go StreamSSE(strings.NewReader(input), ch)

	select {
	case event := <-ch:
		if event.Type != EventTextDelta {
			t.Errorf("expected %s, got %s", EventTextDelta, event.Type)
		}
		var d TextDeltaData
		if err := json.Unmarshal(event.Data, &d); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		if d.Text != "hello world" {
			t.Errorf("expected 'hello world', got '%s'", d.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestStreamSSEThinkingDelta(t *testing.T) {
	input := "event: thinking_delta\ndata: {\"text\":\"thinking...\"}\n\n"
	ch := make(chan SSEEvent, 10)

	go StreamSSE(strings.NewReader(input), ch)

	event := <-ch
	if event.Type != EventThinkingDelta {
		t.Errorf("expected %s, got %s", EventThinkingDelta, event.Type)
	}
}

func TestStreamSSEToolStart(t *testing.T) {
	input := "event: tool_execution_start\ndata: {\"toolName\":\"bash\",\"toolId\":\"t1\",\"args\":\"ls -la\"}\n\n"
	ch := make(chan SSEEvent, 10)

	go StreamSSE(strings.NewReader(input), ch)

	event := <-ch
	if event.Type != EventToolStart {
		t.Errorf("expected %s, got %s", EventToolStart, event.Type)
	}
	var d ToolStartData
	if err := json.Unmarshal(event.Data, &d); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if d.ToolName != "bash" {
		t.Errorf("expected bash, got %s", d.ToolName)
	}
}

func TestStreamSSEToolEnd(t *testing.T) {
	input := "event: tool_execution_end\ndata: {\"toolName\":\"bash\",\"toolId\":\"t1\",\"result\":\"file1.txt\"}\n\n"
	ch := make(chan SSEEvent, 10)

	go StreamSSE(strings.NewReader(input), ch)

	event := <-ch
	if event.Type != EventToolEnd {
		t.Errorf("expected %s, got %s", EventToolEnd, event.Type)
	}
	var d ToolEndData
	if err := json.Unmarshal(event.Data, &d); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if d.ToolName != "bash" {
		t.Errorf("expected bash, got %s", d.ToolName)
	}
}

func TestStreamSSEAgentEnd(t *testing.T) {
	input := "event: agent_end\ndata: {\"input\":100,\"output\":200,\"cache\":50,\"model\":\"test\"}\n\n"
	ch := make(chan SSEEvent, 10)

	go StreamSSE(strings.NewReader(input), ch)

	event := <-ch
	if event.Type != EventAgentEnd {
		t.Errorf("expected %s, got %s", EventAgentEnd, event.Type)
	}
	var d AgentEndData
	if err := json.Unmarshal(event.Data, &d); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if d.Input != 100 || d.Output != 200 {
		t.Errorf("expected 100/200, got %d/%d", d.Input, d.Output)
	}
}

func TestStreamSSEError(t *testing.T) {
	input := "event: error\ndata: {\"error\":\"something broke\"}\n\n"
	ch := make(chan SSEEvent, 10)

	go StreamSSE(strings.NewReader(input), ch)

	event := <-ch
	if event.Type != EventError {
		t.Errorf("expected %s, got %s", EventError, event.Type)
	}
}

func TestStreamSSEMultipleEvents(t *testing.T) {
	input := strings.Join([]string{
		"event: text_delta\ndata: {\"text\":\"hello\"}\n\n",
		"event: text_delta\ndata: {\"text\":\" world\"}\n\n",
		"event: agent_end\ndata: {\"input_tokens\":10,\"output_tokens\":5}\n\n",
	}, "")

	ch := make(chan SSEEvent, 10)
	go StreamSSE(strings.NewReader(input), ch)

	var events []SSEEvent
	for i := 0; i < 3; i++ {
		select {
		case e := <-ch:
			events = append(events, e)
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for event %d", i)
		}
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].Type != EventTextDelta {
		t.Errorf("event 0: expected text_delta, got %s", events[0].Type)
	}
	if events[1].Type != EventTextDelta {
		t.Errorf("event 1: expected text_delta, got %s", events[1].Type)
	}
	if events[2].Type != EventAgentEnd {
		t.Errorf("event 2: expected agent_end, got %s", events[2].Type)
	}
}

func TestStreamSSEApprovalRequest(t *testing.T) {
	input := fmt.Sprintf("event: %s\ndata: {\"requestId\":\"r1\",\"toolName\":\"bash\",\"risk\":\"high\",\"message\":\"delete files?\"}\n\n", EventApprovalRequest)
	ch := make(chan SSEEvent, 10)

	go StreamSSE(strings.NewReader(input), ch)

	event := <-ch
	if event.Type != EventApprovalRequest {
		t.Errorf("expected %s, got %s", EventApprovalRequest, event.Type)
	}
	var d ApprovalData
	if err := json.Unmarshal(event.Data, &d); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if d.RequestID != "r1" {
		t.Errorf("expected r1, got %s", d.RequestID)
	}
}

func TestStreamSSEEmpty(t *testing.T) {
	ch := make(chan SSEEvent, 10)

	go StreamSSE(strings.NewReader(""), ch)

	// Channel should be closed immediately — read should get zero value
	_, ok := <-ch
	if ok {
		t.Error("expected channel to be closed")
	}
}

func TestStreamSSEIgnoreComments(t *testing.T) {
	input := ": this is a comment\nevent: text_delta\ndata: {\"text\":\"hi\"}\n\n"
	ch := make(chan SSEEvent, 10)

	go StreamSSE(strings.NewReader(input), ch)

	event := <-ch
	if event.Type != EventTextDelta {
		t.Errorf("expected text_delta, got %s", event.Type)
	}
}
