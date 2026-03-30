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
