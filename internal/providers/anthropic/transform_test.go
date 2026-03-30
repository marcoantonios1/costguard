package anthropic

import (
	"encoding/json"
	"testing"
)

func ptr[T any](v T) *T { return &v }

// unmarshalRequest is a helper that decodes a JSON string into openAIChatCompletionRequest.
func unmarshalRequest(t *testing.T, raw string) openAIChatCompletionRequest {
	t.Helper()
	var req openAIChatCompletionRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return req
}

func baseRequest(toolsJSON, toolChoiceJSON string) string {
	tools := "null"
	if toolsJSON != "" {
		tools = toolsJSON
	}
	choice := "null"
	if toolChoiceJSON != "" {
		choice = toolChoiceJSON
	}
	return `{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":"hi"}],"tools":` + tools + `,"tool_choice":` + choice + `}`
}

// ---------------------------------------------------------------------------
// Tool mapping
// ---------------------------------------------------------------------------

func TestToolsAreMappedToAnthropicFormat(t *testing.T) {
	raw := baseRequest(`[{"type":"function","function":{"name":"get_weather","description":"Gets weather","parameters":{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}}}]`, `"auto"`)
	out, err := toAnthropicRequest(unmarshalRequest(t, raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(out.Tools))
	}
	tool := out.Tools[0]
	if tool.Name != "get_weather" {
		t.Errorf("name: got %q, want %q", tool.Name, "get_weather")
	}
	if tool.Description != "Gets weather" {
		t.Errorf("description: got %q, want %q", tool.Description, "Gets weather")
	}
	if tool.InputSchema.Type != "object" {
		t.Errorf("input_schema.type: got %q, want %q", tool.InputSchema.Type, "object")
	}
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "location" {
		t.Errorf("input_schema.required: got %v", tool.InputSchema.Required)
	}
}

func TestToolsWithNoParametersDefaultToObjectType(t *testing.T) {
	raw := baseRequest(`[{"type":"function","function":{"name":"ping"}}]`, `"auto"`)
	out, err := toAnthropicRequest(unmarshalRequest(t, raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Tools[0].InputSchema.Type != "object" {
		t.Errorf("expected default type 'object', got %q", out.Tools[0].InputSchema.Type)
	}
}

func TestNoToolsProducesEmptyAnthropicTools(t *testing.T) {
	raw := `{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":"hi"}]}`
	out, err := toAnthropicRequest(unmarshalRequest(t, raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Tools) != 0 {
		t.Errorf("expected no tools, got %d", len(out.Tools))
	}
}

// ---------------------------------------------------------------------------
// tool_choice variants
// ---------------------------------------------------------------------------

func TestToolChoiceAuto(t *testing.T) {
	raw := baseRequest(`[{"type":"function","function":{"name":"f"}}]`, `"auto"`)
	out, err := toAnthropicRequest(unmarshalRequest(t, raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ToolChoice == nil || out.ToolChoice.Type != "auto" {
		t.Errorf("expected tool_choice {type:auto}, got %+v", out.ToolChoice)
	}
}

func TestToolChoiceRequired(t *testing.T) {
	raw := baseRequest(`[{"type":"function","function":{"name":"f"}}]`, `"required"`)
	out, err := toAnthropicRequest(unmarshalRequest(t, raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ToolChoice == nil || out.ToolChoice.Type != "any" {
		t.Errorf("expected tool_choice {type:any}, got %+v", out.ToolChoice)
	}
}

func TestToolChoiceNoneOmitsTools(t *testing.T) {
	raw := baseRequest(`[{"type":"function","function":{"name":"f"}}]`, `"none"`)
	out, err := toAnthropicRequest(unmarshalRequest(t, raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Tools) != 0 {
		t.Errorf("expected tools omitted for tool_choice=none, got %d tools", len(out.Tools))
	}
	if out.ToolChoice != nil {
		t.Errorf("expected tool_choice omitted for none, got %+v", out.ToolChoice)
	}
}

func TestToolChoiceSpecificFunction(t *testing.T) {
	raw := baseRequest(`[{"type":"function","function":{"name":"get_weather"}}]`, `{"type":"function","function":{"name":"get_weather"}}`)
	out, err := toAnthropicRequest(unmarshalRequest(t, raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ToolChoice == nil || out.ToolChoice.Type != "tool" || out.ToolChoice.Name != "get_weather" {
		t.Errorf("expected tool_choice {type:tool, name:get_weather}, got %+v", out.ToolChoice)
	}
}

func TestToolChoiceObjectMissingNameErrors(t *testing.T) {
	raw := baseRequest(`[{"type":"function","function":{"name":"f"}}]`, `{"type":"function","function":{}}`)
	_, err := toAnthropicRequest(unmarshalRequest(t, raw))
	if err == nil {
		t.Error("expected error for tool_choice object with missing function.name")
	}
}

func TestToolChoiceUnsupportedStringErrors(t *testing.T) {
	raw := baseRequest(`[{"type":"function","function":{"name":"f"}}]`, `"bogus"`)
	_, err := toAnthropicRequest(unmarshalRequest(t, raw))
	if err == nil {
		t.Error("expected error for unknown tool_choice string")
	}
}

// ---------------------------------------------------------------------------
// toOpenAIResponse — tool_use block mapping
// ---------------------------------------------------------------------------

func TestToolUseBlockMappedToToolCalls(t *testing.T) {
	in := anthropicMessagesResponse{
		ID:         "msg_01",
		Model:      "claude-3-5-sonnet-20241022",
		StopReason: "tool_use",
		Content: []anthropicContentBlock{
			{
				Type:  "tool_use",
				ID:    "toolu_abc",
				Name:  "get_weather",
				Input: map[string]any{"location": "London"},
			},
		},
		Usage: anthropicUsage{InputTokens: 10, OutputTokens: 5},
	}

	out := toOpenAIResponse("claude-3-5-sonnet-20241022", in)

	if out.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("finish_reason: got %q, want %q", out.Choices[0].FinishReason, "tool_calls")
	}
	msg := out.Choices[0].Message
	if msg.Content != nil {
		t.Errorf("content should be nil when tool_calls present, got %q", *msg.Content)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "toolu_abc" {
		t.Errorf("tool_call id: got %q, want %q", tc.ID, "toolu_abc")
	}
	if tc.Type != "function" {
		t.Errorf("tool_call type: got %q, want %q", tc.Type, "function")
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("function name: got %q, want %q", tc.Function.Name, "get_weather")
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Fatalf("arguments not valid JSON: %v", err)
	}
	if args["location"] != "London" {
		t.Errorf("arguments location: got %v", args["location"])
	}
}

func TestMultipleToolUseBlocksMapped(t *testing.T) {
	in := anthropicMessagesResponse{
		ID:         "msg_02",
		StopReason: "tool_use",
		Content: []anthropicContentBlock{
			{Type: "tool_use", ID: "toolu_1", Name: "fn1", Input: map[string]any{"a": 1}},
			{Type: "tool_use", ID: "toolu_2", Name: "fn2", Input: map[string]any{"b": 2}},
		},
		Usage: anthropicUsage{InputTokens: 10, OutputTokens: 5},
	}

	out := toOpenAIResponse("", in)

	if len(out.Choices[0].Message.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(out.Choices[0].Message.ToolCalls))
	}
	if out.Choices[0].Message.ToolCalls[1].Function.Name != "fn2" {
		t.Errorf("second tool call name wrong")
	}
}

func TestMixedTextAndToolUseResponse(t *testing.T) {
	in := anthropicMessagesResponse{
		ID:         "msg_03",
		StopReason: "tool_use",
		Content: []anthropicContentBlock{
			{Type: "text", Text: "Let me check that."},
			{Type: "tool_use", ID: "toolu_3", Name: "get_weather", Input: map[string]any{"location": "Paris"}},
		},
		Usage: anthropicUsage{InputTokens: 10, OutputTokens: 5},
	}

	out := toOpenAIResponse("", in)

	msg := out.Choices[0].Message
	// tool_calls present → content nil
	if msg.Content != nil {
		t.Errorf("expected content nil in mixed response, got %q", *msg.Content)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msg.ToolCalls))
	}
}

func TestTextOnlyResponseHasNoToolCalls(t *testing.T) {
	in := anthropicMessagesResponse{
		ID:         "msg_04",
		StopReason: "end_turn",
		Content:    []anthropicContentBlock{{Type: "text", Text: "Hello!"}},
		Usage:      anthropicUsage{InputTokens: 5, OutputTokens: 3},
	}

	out := toOpenAIResponse("", in)

	msg := out.Choices[0].Message
	if len(msg.ToolCalls) != 0 {
		t.Errorf("expected no tool calls for text response")
	}
	if msg.Content == nil || *msg.Content != "Hello!" {
		t.Errorf("content: got %v", msg.Content)
	}
	if out.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason: got %q, want %q", out.Choices[0].FinishReason, "stop")
	}
}
