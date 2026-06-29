package openaicompat

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/marcoantonios1/costguard/internal/providers"
)

func parseErrorBody(t *testing.T, b []byte) providers.ErrorBody {
	t.Helper()
	var out providers.ErrorBody
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("failed to parse normalized error: %v\nbody: %s", err, b)
	}
	return out
}

func newCompatClient() *Client {
	return &Client{}
}

// ---------------------------------------------------------------------------
// Generic errors
// ---------------------------------------------------------------------------

func TestNormalizeError_Compat_PassthroughOpenAIShape(t *testing.T) {
	body := []byte(`{"error":{"message":"Invalid API key.","type":"authentication_error"}}`)
	out, err := newCompatClient().NormalizeError(http.StatusUnauthorized, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Type != "authentication_error" {
		t.Errorf("type: got %q, want authentication_error", parsed.Error.Type)
	}
	if parsed.Error.Message != "Invalid API key." {
		t.Errorf("message: got %q", parsed.Error.Message)
	}
}

func TestNormalizeError_Compat_UnparsableBody(t *testing.T) {
	body := []byte(`not json`)
	out, err := newCompatClient().NormalizeError(http.StatusBadGateway, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Type != "upstream_error" {
		t.Errorf("type: got %q, want upstream_error", parsed.Error.Type)
	}
	if parsed.Error.Message == "" {
		t.Error("expected a non-empty fallback message")
	}
}

func TestNormalizeError_Compat_EmptyErrorMessage(t *testing.T) {
	// Body has the right shape but empty message — fall through to status text.
	body := []byte(`{"error":{"message":"","type":"some_error"}}`)
	out, err := newCompatClient().NormalizeError(http.StatusInternalServerError, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Message == "" {
		t.Error("expected a non-empty fallback message")
	}
}

// ---------------------------------------------------------------------------
// Tool-related errors
// ---------------------------------------------------------------------------

func TestNormalizeError_Compat_InvalidToolName(t *testing.T) {
	body := []byte(`{"error":{"message":"Invalid tool name: must not contain hyphens.","type":"invalid_request_error"}}`)
	out, err := newCompatClient().NormalizeError(http.StatusBadRequest, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Type != "invalid_request_error" {
		t.Errorf("type: got %q, want invalid_request_error", parsed.Error.Type)
	}
	if parsed.Error.Message == "" {
		t.Error("expected a non-empty message")
	}
}

func TestNormalizeError_Compat_MalformedToolSchema(t *testing.T) {
	body := []byte(`{"error":{"message":"tools[0].function.parameters must be a valid JSON Schema object.","type":"invalid_request_error"}}`)
	out, err := newCompatClient().NormalizeError(http.StatusBadRequest, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Type != "invalid_request_error" {
		t.Errorf("type: got %q, want invalid_request_error", parsed.Error.Type)
	}
}

func TestNormalizeError_Compat_OutputIsValidErrorBodyShape(t *testing.T) {
	bodies := [][]byte{
		[]byte(`{"error":{"message":"Invalid tool name","type":"invalid_request_error"}}`),
		[]byte(`{"error":{"message":"Rate limit exceeded","type":"rate_limit_error"}}`),
		[]byte(`{}`),
		[]byte(`not json`),
	}
	statuses := []int{400, 429, 500, 502}

	c := newCompatClient()
	for i, body := range bodies {
		out, err := c.NormalizeError(statuses[i], body)
		if err != nil {
			t.Errorf("body[%d]: unexpected error: %v", i, err)
			continue
		}
		var shape struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(out, &shape); err != nil {
			t.Errorf("body[%d]: output is not valid JSON: %v", i, err)
			continue
		}
		if shape.Error.Message == "" {
			t.Errorf("body[%d]: error.message is empty", i)
		}
	}
}

// ---------------------------------------------------------------------------
// multimodal guard
// ---------------------------------------------------------------------------

const minimalOKResponse = `{"id":"x","object":"chat.completion","created":1,"model":"llama3","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`

func compatStubServer(t *testing.T, capture *[]byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*capture = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalOKResponse))
	}))
}

func compatClient(t *testing.T, srvURL string, allowMultimodal bool) *Client {
	t.Helper()
	c, err := NewClient(ClientConfig{
		Name:            "test",
		BaseURL:         srvURL,
		AllowMultimodal: allowMultimodal,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func compatClientWithTools(t *testing.T, srvURL string, allowMultimodal, supportTools bool) *Client {
	t.Helper()
	c, err := NewClient(ClientConfig{
		Name:            "test",
		BaseURL:         srvURL,
		AllowMultimodal: allowMultimodal,
		SupportTools:    supportTools,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func compatRequest(t *testing.T, srvURL, payload string) *http.Request {
	t.Helper()
	u, _ := url.Parse(srvURL + "/v1/chat/completions")
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, u.String(), strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestGuard_ImageBlockRejectedByDefault(t *testing.T) {
	var received []byte
	srv := compatStubServer(t, &received)
	defer srv.Close()

	client := compatClient(t, srv.URL, false)
	payload := `{"model":"llama3","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/img.png"}}]}]}`

	resp, err := client.Do(context.Background(), compatRequest(t, srv.URL, payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
	if len(received) != 0 {
		t.Errorf("upstream should not have been called, got body: %s", received)
	}

	var errBody struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &errBody); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if errBody.Error.Type != "invalid_request_error" {
		t.Errorf("error type: got %q, want invalid_request_error", errBody.Error.Type)
	}
	if errBody.Error.Message == "" {
		t.Error("error message should not be empty")
	}
}

func TestGuard_ImageBlockAllowedWhenMultimodalEnabled(t *testing.T) {
	var received []byte
	srv := compatStubServer(t, &received)
	defer srv.Close()

	client := compatClient(t, srv.URL, true)
	payload := `{"model":"llava","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/img.png"}}]}]}`

	resp, err := client.Do(context.Background(), compatRequest(t, srv.URL, payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
	if string(received) != payload {
		t.Errorf("upstream received wrong body\ngot:  %s\nwant: %s", received, payload)
	}
}

func TestGuard_TextOnlyRequestAlwaysForwarded(t *testing.T) {
	var received []byte
	srv := compatStubServer(t, &received)
	defer srv.Close()

	// Guard off — text only should still pass through.
	client := compatClient(t, srv.URL, false)
	payload := `{"model":"llama3","messages":[{"role":"user","content":"hello"}]}`

	resp, err := client.Do(context.Background(), compatRequest(t, srv.URL, payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
	if string(received) != payload {
		t.Errorf("body changed in transit: %s", received)
	}
}

func TestGuard_TextBlockArrayForwarded(t *testing.T) {
	var received []byte
	srv := compatStubServer(t, &received)
	defer srv.Close()

	client := compatClient(t, srv.URL, false)
	payload := `{"model":"llama3","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`

	resp, err := client.Do(context.Background(), compatRequest(t, srv.URL, payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
}

func TestGuard_ImageOnlyMessageRejectedByDefault(t *testing.T) {
	var received []byte
	srv := compatStubServer(t, &received)
	defer srv.Close()

	client := compatClient(t, srv.URL, false)
	payload := `{"model":"llama3","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,abc="}}]}]}`

	resp, err := client.Do(context.Background(), compatRequest(t, srv.URL, payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
	if len(received) != 0 {
		t.Errorf("upstream should not have been called")
	}
}

func TestGuard_DetailFieldPreservedWhenAllowed(t *testing.T) {
	var received []byte
	srv := compatStubServer(t, &received)
	defer srv.Close()

	client := compatClient(t, srv.URL, true)
	payload := `{"model":"llava","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/img.png","detail":"high"}}]}]}`

	resp, err := client.Do(context.Background(), compatRequest(t, srv.URL, payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]any
	if err := json.Unmarshal(received, &body); err != nil {
		t.Fatalf("upstream body not valid JSON: %v", err)
	}
	messages := body["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	imageURL := content[0].(map[string]any)["image_url"].(map[string]any)
	if imageURL["detail"] != "high" {
		t.Errorf("detail field lost: got %v", imageURL["detail"])
	}
}

func TestGuard_NonChatPathNotGuarded(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received = body
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer srv.Close()

	// Even with allowMultimodal=false, non-chat paths are not inspected.
	client := compatClient(t, srv.URL, false)
	u, _ := url.Parse(srv.URL + "/v1/models")
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, u.String(), nil)

	resp, err := client.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
	_ = received
}

// ---------------------------------------------------------------------------
// tools guard
// ---------------------------------------------------------------------------

const toolsPayload = `{"model":"llama3","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"get_weather","description":"Get weather","parameters":{"type":"object","properties":{}}}}]}`
const noToolsPayload = `{"model":"llama3","messages":[{"role":"user","content":"hello"}]}`

func TestGuard_ToolsRejectedByDefault(t *testing.T) {
	var received []byte
	srv := compatStubServer(t, &received)
	defer srv.Close()

	client := compatClientWithTools(t, srv.URL, false, false)

	resp, err := client.Do(context.Background(), compatRequest(t, srv.URL, toolsPayload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
	if len(received) != 0 {
		t.Errorf("upstream should not have been called, got body: %s", received)
	}

	var errBody struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &errBody); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if errBody.Error.Type != "invalid_request_error" {
		t.Errorf("error type: got %q, want invalid_request_error", errBody.Error.Type)
	}
	if errBody.Error.Message == "" {
		t.Error("error message should not be empty")
	}
}

func TestGuard_ToolsAllowedWhenSupportToolsEnabled(t *testing.T) {
	var received []byte
	srv := compatStubServer(t, &received)
	defer srv.Close()

	client := compatClientWithTools(t, srv.URL, false, true)

	resp, err := client.Do(context.Background(), compatRequest(t, srv.URL, toolsPayload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
	if string(received) != toolsPayload {
		t.Errorf("upstream received wrong body\ngot:  %s\nwant: %s", received, toolsPayload)
	}
}

func TestGuard_NoToolsAlwaysForwarded(t *testing.T) {
	var received []byte
	srv := compatStubServer(t, &received)
	defer srv.Close()

	// supportTools=false, but no tools in the request — must pass through.
	client := compatClientWithTools(t, srv.URL, false, false)

	resp, err := client.Do(context.Background(), compatRequest(t, srv.URL, noToolsPayload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
	if string(received) != noToolsPayload {
		t.Errorf("body changed in transit: %s", received)
	}
}

func TestGuard_ImageRejectedBeforeToolsCheck(t *testing.T) {
	// When both guards are off and the request has both an image and tools,
	// the multimodal guard fires first (checked first in Do).
	var received []byte
	srv := compatStubServer(t, &received)
	defer srv.Close()

	client := compatClientWithTools(t, srv.URL, false, false)
	mixedPayload := `{"model":"llava","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/img.png"}}]}],"tools":[{"type":"function","function":{"name":"f","description":"d","parameters":{}}}]}`

	resp, err := client.Do(context.Background(), compatRequest(t, srv.URL, mixedPayload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
	if len(received) != 0 {
		t.Errorf("upstream should not have been called")
	}
}

// continuationToolResultPayload is a follow-up turn that sends a tool result
// back — it has no `tools` field, only a role:"tool" message.
const continuationToolResultPayload = `{"model":"llama3","messages":[{"role":"user","content":"What is the weather?"},{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"15°C, cloudy"}]}`

// continuationAssistantToolCallsPayload is a follow-up turn where the prior
// assistant message still carries tool_calls — no top-level `tools` field.
const continuationAssistantToolCallsPayload = `{"model":"llama3","messages":[{"role":"user","content":"What is the weather?"},{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{}"}}]}]}`

func TestGuard_ContinuationToolResultRejectedByDefault(t *testing.T) {
	var received []byte
	srv := compatStubServer(t, &received)
	defer srv.Close()

	// supportTools=false — the continuation turn (role:"tool") must be blocked.
	client := compatClientWithTools(t, srv.URL, false, false)

	resp, err := client.Do(context.Background(), compatRequest(t, srv.URL, continuationToolResultPayload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
	if len(received) != 0 {
		t.Errorf("upstream should not have been called, got body: %s", received)
	}

	var errBody struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &errBody); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if errBody.Error.Type != "invalid_request_error" {
		t.Errorf("error type: got %q, want invalid_request_error", errBody.Error.Type)
	}
}

func TestGuard_ContinuationToolResultAllowedWhenSupportToolsEnabled(t *testing.T) {
	var received []byte
	srv := compatStubServer(t, &received)
	defer srv.Close()

	client := compatClientWithTools(t, srv.URL, false, true)

	resp, err := client.Do(context.Background(), compatRequest(t, srv.URL, continuationToolResultPayload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
	if string(received) != continuationToolResultPayload {
		t.Errorf("upstream received wrong body\ngot:  %s\nwant: %s", received, continuationToolResultPayload)
	}
}

func TestGuard_ContinuationAssistantToolCallsRejectedByDefault(t *testing.T) {
	var received []byte
	srv := compatStubServer(t, &received)
	defer srv.Close()

	// supportTools=false — assistant message with tool_calls must be blocked.
	client := compatClientWithTools(t, srv.URL, false, false)

	resp, err := client.Do(context.Background(), compatRequest(t, srv.URL, continuationAssistantToolCallsPayload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
	if len(received) != 0 {
		t.Errorf("upstream should not have been called, got body: %s", received)
	}

	var errBody struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &errBody); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if errBody.Error.Type != "invalid_request_error" {
		t.Errorf("error type: got %q, want invalid_request_error", errBody.Error.Type)
	}
}

func TestGuard_ContinuationAssistantToolCallsAllowedWhenSupportToolsEnabled(t *testing.T) {
	var received []byte
	srv := compatStubServer(t, &received)
	defer srv.Close()

	client := compatClientWithTools(t, srv.URL, false, true)

	resp, err := client.Do(context.Background(), compatRequest(t, srv.URL, continuationAssistantToolCallsPayload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
	if string(received) != continuationAssistantToolCallsPayload {
		t.Errorf("upstream received wrong body\ngot:  %s\nwant: %s", received, continuationAssistantToolCallsPayload)
	}
}

func TestGuard_ToolsGuardSkippedOnNonChatPath(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received = body
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer srv.Close()

	// supportTools=false, but path is /v1/models — guard must not fire.
	client := compatClientWithTools(t, srv.URL, false, false)
	u, _ := url.Parse(srv.URL + "/v1/models")
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, u.String(), nil)

	resp, err := client.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
	_ = received
}

// ---------------------------------------------------------------------------
// Header whitelisting
// ---------------------------------------------------------------------------

// headerCapturingServer returns a test server that records the headers of every
// inbound request and responds with minimalOKResponse.
func headerCapturingServer(t *testing.T, captured *http.Header) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*captured = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalOKResponse))
	}))
}

func compatClientWithKey(t *testing.T, srvURL, apiKey string) *Client {
	t.Helper()
	c, err := NewClient(ClientConfig{
		Name:            "test",
		BaseURL:         srvURL,
		APIKey:          apiKey,
		AllowMultimodal: true,
		SupportTools:    true,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestDo_InboundAuthorizationNotForwarded(t *testing.T) {
	var captured http.Header
	srv := headerCapturingServer(t, &captured)
	defer srv.Close()

	client := compatClientWithKey(t, srv.URL, "configured-provider-key")

	req := compatRequest(t, srv.URL, noToolsPayload)
	req.Header.Set("Authorization", "Bearer client-side-token")

	resp, err := client.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	got := captured.Get("Authorization")
	if got != "Bearer configured-provider-key" {
		t.Errorf("Authorization: got %q, want %q", got, "Bearer configured-provider-key")
	}
}

func TestDo_CostguardHeadersNotForwarded(t *testing.T) {
	var captured http.Header
	srv := headerCapturingServer(t, &captured)
	defer srv.Close()

	client := compatClientWithKey(t, srv.URL, "")

	req := compatRequest(t, srv.URL, noToolsPayload)
	req.Header.Set("X-Costguard-Team", "team-a")
	req.Header.Set("X-Costguard-Project", "proj-b")
	req.Header.Set("X-Costguard-User", "user-c")
	req.Header.Set("X-Costguard-Agent", "agent-d")
	req.Header.Set("X-Request-Id", "req-123")

	resp, err := client.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	for _, h := range []string{"X-Costguard-Team", "X-Costguard-Project", "X-Costguard-User", "X-Costguard-Agent", "X-Request-Id"} {
		if v := captured.Get(h); v != "" {
			t.Errorf("header %q must not reach upstream, got %q", h, v)
		}
	}
}

func TestDo_AcceptEncodingNotForwarded(t *testing.T) {
	var captured http.Header
	srv := headerCapturingServer(t, &captured)
	defer srv.Close()

	client := compatClientWithKey(t, srv.URL, "")

	req := compatRequest(t, srv.URL, noToolsPayload)
	req.Header.Set("Accept-Encoding", "deflate")

	resp, err := client.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if v := captured.Get("Accept-Encoding"); strings.Contains(v, "deflate") {
		t.Errorf("client Accept-Encoding must not reach upstream, got %q", v)
	}
}

func TestDo_GzippedUpstreamBody_MeteredCorrectly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		_, _ = gz.Write([]byte(minimalOKResponse))
		_ = gz.Close()

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()

	client := compatClientWithKey(t, srv.URL, "")

	resp, err := client.Do(context.Background(), compatRequest(t, srv.URL, noToolsPayload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}

	// Body must be plain JSON, not raw gzip bytes.
	meta, err := new(Client).ParseResponseMeta(body)
	if err != nil {
		t.Fatalf("ParseResponseMeta failed (body likely still gzip): %v\nbody: %q", err, body)
	}
	if meta.TotalTokens != 7 {
		t.Errorf("TotalTokens: got %d, want 7", meta.TotalTokens)
	}
	if resp.Header.Get("Content-Encoding") != "" {
		t.Errorf("Content-Encoding should be empty after transparent decompression, got %q", resp.Header.Get("Content-Encoding"))
	}
}
