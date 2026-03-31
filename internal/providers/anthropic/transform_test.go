package anthropic

import (
	"encoding/json"
	"strings"
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

// ---------------------------------------------------------------------------
// tool role messages (tool results)
// ---------------------------------------------------------------------------

func TestToolRoleMessageMappedToToolResult(t *testing.T) {
	raw := `{
		"model": "claude-3-5-sonnet-20241022",
		"messages": [
			{"role": "user", "content": "What is the weather in London?"},
			{"role": "assistant", "content": null, "tool_calls": [{"id": "toolu_1", "type": "function", "function": {"name": "get_weather", "arguments": "{\"location\":\"London\"}"}}]},
			{"role": "tool", "tool_call_id": "toolu_1", "content": "15°C and cloudy"}
		]
	}`
	out, err := toAnthropicRequest(unmarshalRequest(t, raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect: user, assistant (tool_use), user (tool_result)
	if len(out.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out.Messages))
	}

	assistantMsg := out.Messages[1]
	if assistantMsg.Role != "assistant" {
		t.Errorf("message[1] role: got %q, want assistant", assistantMsg.Role)
	}
	if len(assistantMsg.Content) != 1 || assistantMsg.Content[0].Type != "tool_use" {
		t.Errorf("message[1] should have a tool_use block, got %+v", assistantMsg.Content)
	}
	toolUse := assistantMsg.Content[0]
	if toolUse.ID != "toolu_1" || toolUse.Name != "get_weather" {
		t.Errorf("tool_use block: id=%q name=%q", toolUse.ID, toolUse.Name)
	}

	resultMsg := out.Messages[2]
	if resultMsg.Role != "user" {
		t.Errorf("message[2] role: got %q, want user", resultMsg.Role)
	}
	if len(resultMsg.Content) != 1 || resultMsg.Content[0].Type != "tool_result" {
		t.Errorf("message[2] should have a tool_result block, got %+v", resultMsg.Content)
	}
	tr := resultMsg.Content[0]
	if tr.ToolUseID != "toolu_1" {
		t.Errorf("tool_result tool_use_id: got %q, want toolu_1", tr.ToolUseID)
	}
	if tr.Content != "15°C and cloudy" {
		t.Errorf("tool_result content: got %q", tr.Content)
	}
}

func TestAssistantMessageWithToolCallsConvertsArguments(t *testing.T) {
	raw := `{
		"model": "claude-3-5-sonnet-20241022",
		"messages": [
			{"role": "user", "content": "hi"},
			{"role": "assistant", "content": null, "tool_calls": [{"id": "tc_1", "type": "function", "function": {"name": "fn", "arguments": "{\"key\":\"value\"}"}}]}
		]
	}`
	out, err := toAnthropicRequest(unmarshalRequest(t, raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	block := out.Messages[1].Content[0]
	if block.Type != "tool_use" {
		t.Fatalf("expected tool_use block, got %q", block.Type)
	}
	input, ok := block.Input.(map[string]any)
	if !ok {
		t.Fatalf("input not a map, got %T", block.Input)
	}
	if input["key"] != "value" {
		t.Errorf("input key: got %v", input["key"])
	}
}

func TestParallelToolResultsCoalescedIntoSingleUserMessage(t *testing.T) {
	raw := `{
		"model": "claude-3-5-sonnet-20241022",
		"messages": [
			{"role": "user", "content": "hi"},
			{"role": "assistant", "content": null, "tool_calls": [
				{"id": "tc_1", "type": "function", "function": {"name": "fn1", "arguments": "{}"}},
				{"id": "tc_2", "type": "function", "function": {"name": "fn2", "arguments": "{}"}}
			]},
			{"role": "tool", "tool_call_id": "tc_1", "content": "result1"},
			{"role": "tool", "tool_call_id": "tc_2", "content": "result2"}
		]
	}`
	out, err := toAnthropicRequest(unmarshalRequest(t, raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect: user, assistant, user (with 2 tool_result blocks — not 2 separate user messages)
	if len(out.Messages) != 3 {
		t.Fatalf("expected 3 messages (parallel results coalesced), got %d", len(out.Messages))
	}
	resultMsg := out.Messages[2]
	if resultMsg.Role != "user" {
		t.Errorf("coalesced message role: got %q, want user", resultMsg.Role)
	}
	if len(resultMsg.Content) != 2 {
		t.Fatalf("expected 2 tool_result blocks, got %d", len(resultMsg.Content))
	}
	if resultMsg.Content[0].ToolUseID != "tc_1" || resultMsg.Content[1].ToolUseID != "tc_2" {
		t.Errorf("tool_use_ids: %q %q", resultMsg.Content[0].ToolUseID, resultMsg.Content[1].ToolUseID)
	}
}

// ---------------------------------------------------------------------------
// streaming: toAnthropicRequest
// ---------------------------------------------------------------------------

func TestStreamRequestSetsStreamTrue(t *testing.T) {
	raw := `{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":"hi"}],"stream":true}`
	out, err := toAnthropicRequest(unmarshalRequest(t, raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Stream {
		t.Error("expected Stream=true in anthropic request")
	}
}

func TestNonStreamRequestSetsStreamFalse(t *testing.T) {
	raw := `{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":"hi"}]}`
	out, err := toAnthropicRequest(unmarshalRequest(t, raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Stream {
		t.Error("expected Stream=false in anthropic request")
	}
}

// ---------------------------------------------------------------------------
// streaming: translateAnthropicStream
// ---------------------------------------------------------------------------

func sseInput(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

func parseSSEChunks(t *testing.T, output string) []openAIStreamChunk {
	t.Helper()
	var chunks []openAIStreamChunk
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}
		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			t.Fatalf("invalid SSE chunk JSON %q: %v", data, err)
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

func hasDone(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "data: [DONE]" {
			return true
		}
	}
	return false
}

func TestTranslateStream_TextResponse(t *testing.T) {
	input := sseInput(
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_01","model":"claude-3-5-sonnet-20241022"}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
	)

	var buf strings.Builder
	translateAnthropicStream("claude-3-5-sonnet-20241022", strings.NewReader(input), &buf)
	output := buf.String()

	if !hasDone(output) {
		t.Error("expected data: [DONE] at end")
	}

	chunks := parseSSEChunks(t, output)
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	// First chunk: role
	if chunks[0].Choices[0].Delta.Role != "assistant" {
		t.Errorf("first chunk role: got %q, want assistant", chunks[0].Choices[0].Delta.Role)
	}
	if chunks[0].ID != "msg_01" {
		t.Errorf("chunk id: got %q, want msg_01", chunks[0].ID)
	}

	// Find text chunks
	var texts []string
	for _, ch := range chunks {
		if ch.Choices[0].Delta.Content != nil {
			texts = append(texts, *ch.Choices[0].Delta.Content)
		}
	}
	if strings.Join(texts, "") != "Hello world" {
		t.Errorf("text content: got %q, want %q", strings.Join(texts, ""), "Hello world")
	}

	// Last chunk before DONE: finish_reason=stop
	last := chunks[len(chunks)-1]
	if last.Choices[0].FinishReason == nil || *last.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason: got %v, want stop", last.Choices[0].FinishReason)
	}
}

func TestTranslateStream_ToolUse(t *testing.T) {
	input := sseInput(
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_02","model":"claude-3-5-sonnet-20241022"}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_01","name":"get_weather","input":{}}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"location\":"}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"London\"}"}}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
	)

	var buf strings.Builder
	translateAnthropicStream("claude-3-5-sonnet-20241022", strings.NewReader(input), &buf)
	output := buf.String()

	if !hasDone(output) {
		t.Error("expected data: [DONE] at end")
	}

	chunks := parseSSEChunks(t, output)

	// Find the tool call start chunk (has id+name)
	var startChunk *openAIStreamChunk
	for i, ch := range chunks {
		if len(ch.Choices[0].Delta.ToolCalls) > 0 && ch.Choices[0].Delta.ToolCalls[0].ID != "" {
			startChunk = &chunks[i]
			break
		}
	}
	if startChunk == nil {
		t.Fatal("no tool call start chunk found")
	}
	tc := startChunk.Choices[0].Delta.ToolCalls[0]
	if tc.ID != "toolu_01" {
		t.Errorf("tool call id: got %q, want toolu_01", tc.ID)
	}
	if tc.Type != "function" {
		t.Errorf("tool call type: got %q, want function", tc.Type)
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("tool call name: got %q, want get_weather", tc.Function.Name)
	}

	// Collect argument deltas
	var args string
	for _, ch := range chunks {
		if len(ch.Choices[0].Delta.ToolCalls) > 0 {
			args += ch.Choices[0].Delta.ToolCalls[0].Function.Arguments
		}
	}
	if !strings.Contains(args, "London") {
		t.Errorf("accumulated arguments %q do not contain expected content", args)
	}

	// Finish reason
	last := chunks[len(chunks)-1]
	if last.Choices[0].FinishReason == nil || *last.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("finish_reason: got %v, want tool_calls", last.Choices[0].FinishReason)
	}
}

func TestTranslateStream_ParallelToolCalls(t *testing.T) {
	input := sseInput(
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_03","model":"claude-3-5-sonnet-20241022"}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_a","name":"fn_a","input":{}}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_b","name":"fn_b","input":{}}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop","index":1}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
	)

	var buf strings.Builder
	translateAnthropicStream("claude-3-5-sonnet-20241022", strings.NewReader(input), &buf)
	chunks := parseSSEChunks(t, buf.String())

	toolIndices := map[int]string{} // tool_calls array index → id
	for _, ch := range chunks {
		for _, tc := range ch.Choices[0].Delta.ToolCalls {
			if tc.ID != "" {
				toolIndices[tc.Index] = tc.ID
			}
		}
	}
	if toolIndices[0] != "toolu_a" {
		t.Errorf("tool index 0: got %q, want toolu_a", toolIndices[0])
	}
	if toolIndices[1] != "toolu_b" {
		t.Errorf("tool index 1: got %q, want toolu_b", toolIndices[1])
	}
}

func TestTranslateStream_ModelFromMessageStart(t *testing.T) {
	input := sseInput(
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_04","model":"claude-3-opus-20240229"}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
	)

	var buf strings.Builder
	translateAnthropicStream("fallback-model", strings.NewReader(input), &buf)
	chunks := parseSSEChunks(t, buf.String())

	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}
	if chunks[0].Model != "claude-3-opus-20240229" {
		t.Errorf("model: got %q, want claude-3-opus-20240229", chunks[0].Model)
	}
}

func TestFullToolCallingCycleProducesCorrectMessageSequence(t *testing.T) {
	// Full loop: user → assistant (tool_use) → tool result → user follow-up
	raw := `{
		"model": "claude-3-5-sonnet-20241022",
		"messages": [
			{"role": "user", "content": "What is the weather in London?"},
			{"role": "assistant", "content": null, "tool_calls": [{"id": "tc_1", "type": "function", "function": {"name": "get_weather", "arguments": "{\"location\":\"London\"}"}}]},
			{"role": "tool", "tool_call_id": "tc_1", "content": "15°C and cloudy"},
			{"role": "user", "content": "Thanks!"}
		]
	}`
	out, err := toAnthropicRequest(unmarshalRequest(t, raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(out.Messages))
	}
	roles := []string{"user", "assistant", "user", "user"}
	types := []string{"text", "tool_use", "tool_result", "text"}
	for i, msg := range out.Messages {
		if msg.Role != roles[i] {
			t.Errorf("message[%d] role: got %q, want %q", i, msg.Role, roles[i])
		}
		if len(msg.Content) == 0 || msg.Content[0].Type != types[i] {
			t.Errorf("message[%d] content[0].type: got %q, want %q", i, msg.Content[0].Type, types[i])
		}
	}
}
