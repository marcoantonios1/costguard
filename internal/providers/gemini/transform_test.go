package gemini

import (
	"encoding/json"
	"strings"
	"testing"
)

func unmarshalRequest(t *testing.T, raw string) openAIChatCompletionRequest {
	t.Helper()
	var req openAIChatCompletionRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return req
}

// ---------------------------------------------------------------------------
// Tool definitions → Gemini functionDeclarations
// ---------------------------------------------------------------------------

func TestToolsMappedToFunctionDeclarations(t *testing.T) {
	raw := `{
		"model": "gemini-2.5-flash",
		"messages": [{"role": "user", "content": "What is the weather in London?"}],
		"tools": [{"type": "function", "function": {
			"name": "get_weather",
			"description": "Gets weather",
			"parameters": {"type": "object", "properties": {"location": {"type": "string"}}, "required": ["location"]}
		}}],
		"tool_choice": "auto"
	}`
	out, err := toGeminiRequest(unmarshalRequest(t, raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(out.Tools))
	}
	decl := out.Tools[0].FunctionDeclarations
	if len(decl) != 1 {
		t.Fatalf("expected 1 function declaration, got %d", len(decl))
	}
	if decl[0].Name != "get_weather" {
		t.Errorf("name: got %q, want %q", decl[0].Name, "get_weather")
	}
	if decl[0].Description != "Gets weather" {
		t.Errorf("description: got %q", decl[0].Description)
	}
}

func TestToolChoiceNoneOmitsTools(t *testing.T) {
	raw := `{
		"model": "gemini-2.5-flash",
		"messages": [{"role": "user", "content": "hi"}],
		"tools": [{"type": "function", "function": {"name": "fn"}}],
		"tool_choice": "none"
	}`
	out, err := toGeminiRequest(unmarshalRequest(t, raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Tools) != 0 {
		t.Errorf("expected tools omitted for tool_choice=none")
	}
	if out.ToolConfig != nil {
		t.Errorf("expected toolConfig nil for tool_choice=none")
	}
}

func TestToolChoiceAutoMapsToAUTO(t *testing.T) {
	raw := `{
		"model": "gemini-2.5-flash",
		"messages": [{"role": "user", "content": "hi"}],
		"tools": [{"type": "function", "function": {"name": "fn"}}],
		"tool_choice": "auto"
	}`
	out, err := toGeminiRequest(unmarshalRequest(t, raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ToolConfig == nil || out.ToolConfig.FunctionCallingConfig.Mode != "AUTO" {
		t.Errorf("expected mode AUTO, got %+v", out.ToolConfig)
	}
}

func TestToolChoiceRequiredMapsToANY(t *testing.T) {
	raw := `{
		"model": "gemini-2.5-flash",
		"messages": [{"role": "user", "content": "hi"}],
		"tools": [{"type": "function", "function": {"name": "fn"}}],
		"tool_choice": "required"
	}`
	out, err := toGeminiRequest(unmarshalRequest(t, raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ToolConfig == nil || out.ToolConfig.FunctionCallingConfig.Mode != "ANY" {
		t.Errorf("expected mode ANY, got %+v", out.ToolConfig)
	}
}

func TestToolChoiceSpecificFunctionMapsToAllowedFunctionNames(t *testing.T) {
	raw := `{
		"model": "gemini-2.5-flash",
		"messages": [{"role": "user", "content": "hi"}],
		"tools": [{"type": "function", "function": {"name": "get_weather"}}],
		"tool_choice": {"type": "function", "function": {"name": "get_weather"}}
	}`
	out, err := toGeminiRequest(unmarshalRequest(t, raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg := out.ToolConfig
	if cfg == nil || cfg.FunctionCallingConfig.Mode != "ANY" {
		t.Errorf("expected mode ANY, got %+v", cfg)
	}
	if len(cfg.FunctionCallingConfig.AllowedFunctionNames) != 1 || cfg.FunctionCallingConfig.AllowedFunctionNames[0] != "get_weather" {
		t.Errorf("allowedFunctionNames: %v", cfg.FunctionCallingConfig.AllowedFunctionNames)
	}
}

// ---------------------------------------------------------------------------
// Assistant messages with tool_calls → model functionCall parts
// ---------------------------------------------------------------------------

func TestAssistantToolCallsMappedToFunctionCallParts(t *testing.T) {
	raw := `{
		"model": "gemini-2.5-flash",
		"messages": [
			{"role": "user", "content": "What is the weather in London?"},
			{"role": "assistant", "content": null, "tool_calls": [
				{"id": "call_get_weather", "type": "function", "function": {"name": "get_weather", "arguments": "{\"location\":\"London\"}"}}
			]}
		]
	}`
	out, err := toGeminiRequest(unmarshalRequest(t, raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Contents) != 2 {
		t.Fatalf("expected 2 contents, got %d", len(out.Contents))
	}
	modelMsg := out.Contents[1]
	if modelMsg.Role != "model" {
		t.Errorf("role: got %q, want model", modelMsg.Role)
	}
	if len(modelMsg.Parts) != 1 || modelMsg.Parts[0].FunctionCall == nil {
		t.Fatalf("expected functionCall part, got %+v", modelMsg.Parts)
	}
	fc := modelMsg.Parts[0].FunctionCall
	if fc.Name != "get_weather" {
		t.Errorf("functionCall name: got %q", fc.Name)
	}
	args, ok := fc.Args.(map[string]any)
	if !ok || args["location"] != "London" {
		t.Errorf("functionCall args: %+v", fc.Args)
	}
}

// ---------------------------------------------------------------------------
// Tool role messages → user functionResponse parts
// ---------------------------------------------------------------------------

func TestToolRoleMappedToFunctionResponse(t *testing.T) {
	raw := `{
		"model": "gemini-2.5-flash",
		"messages": [
			{"role": "user", "content": "What is the weather in London?"},
			{"role": "assistant", "content": null, "tool_calls": [
				{"id": "call_gw", "type": "function", "function": {"name": "get_weather", "arguments": "{\"location\":\"London\"}"}}
			]},
			{"role": "tool", "tool_call_id": "call_gw", "content": "15°C and cloudy"}
		]
	}`
	out, err := toGeminiRequest(unmarshalRequest(t, raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// user, model (tool_call), user (tool_result)
	if len(out.Contents) != 3 {
		t.Fatalf("expected 3 contents, got %d", len(out.Contents))
	}
	resultMsg := out.Contents[2]
	if resultMsg.Role != "user" {
		t.Errorf("tool result role: got %q, want user", resultMsg.Role)
	}
	if len(resultMsg.Parts) != 1 || resultMsg.Parts[0].FunctionResponse == nil {
		t.Fatalf("expected functionResponse part, got %+v", resultMsg.Parts)
	}
	fr := resultMsg.Parts[0].FunctionResponse
	if fr.Name != "get_weather" {
		t.Errorf("functionResponse name: got %q", fr.Name)
	}
	resp, ok := fr.Response.(map[string]any)
	if !ok || resp["content"] != "15°C and cloudy" {
		t.Errorf("functionResponse response: %+v", fr.Response)
	}
}

func TestParallelToolResultsCoalesced(t *testing.T) {
	raw := `{
		"model": "gemini-2.5-flash",
		"messages": [
			{"role": "user", "content": "hi"},
			{"role": "assistant", "content": null, "tool_calls": [
				{"id": "c1", "type": "function", "function": {"name": "fn1", "arguments": "{}"}},
				{"id": "c2", "type": "function", "function": {"name": "fn2", "arguments": "{}"}}
			]},
			{"role": "tool", "tool_call_id": "c1", "content": "r1"},
			{"role": "tool", "tool_call_id": "c2", "content": "r2"}
		]
	}`
	out, err := toGeminiRequest(unmarshalRequest(t, raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// user, model, user (with 2 functionResponse parts coalesced)
	if len(out.Contents) != 3 {
		t.Fatalf("expected 3 contents (coalesced), got %d", len(out.Contents))
	}
	if len(out.Contents[2].Parts) != 2 {
		t.Fatalf("expected 2 functionResponse parts, got %d", len(out.Contents[2].Parts))
	}
}

// ---------------------------------------------------------------------------
// toOpenAIResponse — functionCall parts → tool_calls
// ---------------------------------------------------------------------------

func TestFunctionCallPartMappedToToolCalls(t *testing.T) {
	in := geminiGenerateContentResponse{
		Candidates: []geminiCandidate{
			{
				FinishReason: "STOP",
				Content: geminiContent{
					Role: "model",
					Parts: []geminiPart{
						{FunctionCall: &geminiFunctionCall{Name: "get_weather", Args: map[string]any{"location": "London"}}},
					},
				},
			},
		},
	}
	out := toOpenAIResponse("gemini-2.5-flash", in)

	if out.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("finish_reason: got %q, want tool_calls", out.Choices[0].FinishReason)
	}
	msg := out.Choices[0].Message
	if msg.Content != nil {
		t.Errorf("content should be nil when tool_calls present")
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_get_weather" {
		t.Errorf("id: got %q", tc.ID)
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("name: got %q", tc.Function.Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Fatalf("arguments not valid JSON: %v", err)
	}
	if args["location"] != "London" {
		t.Errorf("args location: %v", args["location"])
	}
}

func TestMultipleFunctionCallParts(t *testing.T) {
	in := geminiGenerateContentResponse{
		Candidates: []geminiCandidate{
			{
				FinishReason: "STOP",
				Content: geminiContent{
					Role: "model",
					Parts: []geminiPart{
						{FunctionCall: &geminiFunctionCall{Name: "fn1", Args: map[string]any{}}},
						{FunctionCall: &geminiFunctionCall{Name: "fn2", Args: map[string]any{}}},
					},
				},
			},
		},
	}
	out := toOpenAIResponse("", in)
	if len(out.Choices[0].Message.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(out.Choices[0].Message.ToolCalls))
	}
}

// ---------------------------------------------------------------------------
// translateGeminiStream
// ---------------------------------------------------------------------------

func geminiSSE(partials ...string) string {
	var b strings.Builder
	for _, p := range partials {
		b.WriteString("data: ")
		b.WriteString(p)
		b.WriteString("\n\n")
	}
	return b.String()
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

func TestTranslateGeminiStream_TextResponse(t *testing.T) {
	input := geminiSSE(
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"Hello"}]},"index":0}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":" world"}]},"index":0}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2,"totalTokenCount":7},"modelVersion":"gemini-2.5-flash"}`,
	)

	var buf strings.Builder
	translateGeminiStream("gemini-2.5-flash", strings.NewReader(input), &buf)
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

	// Collect text
	var text string
	for _, ch := range chunks {
		if ch.Choices[0].Delta.Content != nil {
			text += *ch.Choices[0].Delta.Content
		}
	}
	if text != "Hello world" {
		t.Errorf("accumulated text: got %q, want %q", text, "Hello world")
	}

	// Last chunk: finish_reason=stop
	last := chunks[len(chunks)-1]
	if last.Choices[0].FinishReason == nil || *last.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason: got %v, want stop", last.Choices[0].FinishReason)
	}
}

func TestTranslateGeminiStream_ModelFromChunk(t *testing.T) {
	input := geminiSSE(
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP","index":0}],"modelVersion":"gemini-2.5-flash-001"}`,
	)

	var buf strings.Builder
	translateGeminiStream("fallback-model", strings.NewReader(input), &buf)
	chunks := parseSSEChunks(t, buf.String())

	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}
	for _, ch := range chunks {
		if ch.Model != "gemini-2.5-flash-001" {
			t.Errorf("model: got %q, want gemini-2.5-flash-001", ch.Model)
		}
	}
}

func TestTranslateGeminiStream_ToolCall(t *testing.T) {
	input := geminiSSE(
		`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"location":"London"}}}]},"finishReason":"STOP","index":0}]}`,
	)

	var buf strings.Builder
	translateGeminiStream("gemini-2.5-flash", strings.NewReader(input), &buf)
	output := buf.String()

	if !hasDone(output) {
		t.Error("expected data: [DONE] at end")
	}

	chunks := parseSSEChunks(t, output)

	// Find tool call chunk
	var tcChunk *openAIStreamChunk
	for i, ch := range chunks {
		if len(ch.Choices[0].Delta.ToolCalls) > 0 {
			tcChunk = &chunks[i]
			break
		}
	}
	if tcChunk == nil {
		t.Fatal("no tool call chunk found")
	}
	tc := tcChunk.Choices[0].Delta.ToolCalls[0]
	if tc.Index != 0 {
		t.Errorf("tool call index: got %d, want 0", tc.Index)
	}
	if tc.ID != "call_get_weather" {
		t.Errorf("tool call id: got %q, want call_get_weather", tc.ID)
	}
	if tc.Type != "function" {
		t.Errorf("tool call type: got %q, want function", tc.Type)
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("function name: got %q, want get_weather", tc.Function.Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Fatalf("arguments not valid JSON: %v", err)
	}
	if args["location"] != "London" {
		t.Errorf("args location: %v", args["location"])
	}

	// Finish reason should be tool_calls
	last := chunks[len(chunks)-1]
	if last.Choices[0].FinishReason == nil || *last.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("finish_reason: got %v, want tool_calls", last.Choices[0].FinishReason)
	}
}

func TestTranslateGeminiStream_ParallelToolCalls(t *testing.T) {
	input := geminiSSE(
		`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"fn_a","args":{}}},{"functionCall":{"name":"fn_b","args":{}}}]},"finishReason":"STOP","index":0}]}`,
	)

	var buf strings.Builder
	translateGeminiStream("gemini-2.5-flash", strings.NewReader(input), &buf)
	chunks := parseSSEChunks(t, buf.String())

	toolIndices := map[int]string{}
	for _, ch := range chunks {
		for _, tc := range ch.Choices[0].Delta.ToolCalls {
			if tc.ID != "" {
				toolIndices[tc.Index] = tc.ID
			}
		}
	}
	if toolIndices[0] != "call_fn_a" {
		t.Errorf("tool index 0: got %q, want call_fn_a", toolIndices[0])
	}
	if toolIndices[1] != "call_fn_b" {
		t.Errorf("tool index 1: got %q, want call_fn_b", toolIndices[1])
	}
}

func TestTranslateGeminiStream_AlwaysEndsDone(t *testing.T) {
	// Even if upstream closes with no finishReason, [DONE] must be sent.
	input := geminiSSE(`{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"index":0}]}`)

	var buf strings.Builder
	translateGeminiStream("gemini-2.5-flash", strings.NewReader(input), &buf)

	if !hasDone(buf.String()) {
		t.Error("expected data: [DONE] even without finishReason chunk")
	}
}

func TestTextResponseHasNoToolCalls(t *testing.T) {
	in := geminiGenerateContentResponse{
		Candidates: []geminiCandidate{
			{
				FinishReason: "STOP",
				Content: geminiContent{
					Role:  "model",
					Parts: []geminiPart{{Text: "Hello!"}},
				},
			},
		},
	}
	out := toOpenAIResponse("", in)
	msg := out.Choices[0].Message
	if len(msg.ToolCalls) != 0 {
		t.Errorf("expected no tool calls")
	}
	if msg.Content == nil || *msg.Content != "Hello!" {
		t.Errorf("content: %v", msg.Content)
	}
	if out.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason: got %q", out.Choices[0].FinishReason)
	}
}
