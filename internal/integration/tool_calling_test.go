// Package integration contains end-to-end tests for tool calling across all
// provider adapters (OpenAI, Anthropic, Gemini). Tests run entirely in-process
// using httptest servers for the fake upstream APIs and an in-memory usage
// store — no real API keys or database required.
package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcoantonios1/costguard/internal/cache"
	"github.com/marcoantonios1/costguard/internal/gateway"
	"github.com/marcoantonios1/costguard/internal/providers"
	anthropic_provider "github.com/marcoantonios1/costguard/internal/providers/anthropic"
	gemini_provider "github.com/marcoantonios1/costguard/internal/providers/gemini"
	openai_provider "github.com/marcoantonios1/costguard/internal/providers/openai"
	openai_http "github.com/marcoantonios1/costguard/internal/server/openai"
	"github.com/marcoantonios1/costguard/internal/usage"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// staticRouter always returns the same provider. The integration tests pin to
// a provider via the X-Costguard-Provider header, so PickProvider is never
// actually called; we only need a non-nil Router.
type staticRouter struct{}

func (r *staticRouter) PickProvider(_ string) string { return "" }

// fakeStore is a thread-safe in-memory usage.Store used to verify metering.
type fakeStore struct {
	mu      sync.Mutex
	records []usage.Record
}

func (s *fakeStore) Save(_ context.Context, r usage.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, r)
	return nil
}

func (s *fakeStore) all() []usage.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]usage.Record, len(s.records))
	copy(cp, s.records)
	return cp
}

func (s *fakeStore) GetTotalSpend(_ context.Context, _, _ time.Time) (float64, error) {
	return 0, nil
}
func (s *fakeStore) GetSpendByTeam(_ context.Context, _, _ time.Time) ([]usage.TeamSpend, error) {
	return nil, nil
}
func (s *fakeStore) GetSpendByProvider(_ context.Context, _, _ time.Time) ([]usage.ProviderSpend, error) {
	return nil, nil
}
func (s *fakeStore) GetSpendByModel(_ context.Context, _, _ time.Time) ([]usage.ModelSpend, error) {
	return nil, nil
}
func (s *fakeStore) GetSpendByProject(_ context.Context, _, _ time.Time) ([]usage.ProjectSpend, error) {
	return nil, nil
}
func (s *fakeStore) GetSpendForTeam(_ context.Context, _ string, _, _ time.Time) (float64, error) {
	return 0, nil
}
func (s *fakeStore) GetSpendForProject(_ context.Context, _ string, _, _ time.Time) (float64, error) {
	return 0, nil
}
func (s *fakeStore) GetSpendForAgent(_ context.Context, _ string, _, _ time.Time) (float64, error) {
	return 0, nil
}
func (s *fakeStore) GetSpendByAgent(_ context.Context, _, _ time.Time) ([]usage.AgentSpend, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// Test harness
// ---------------------------------------------------------------------------

type harness struct {
	server *httptest.Server
	store  *fakeStore
}

// newHarness wires up the full gateway stack backed by the given provider
// registry. The returned httptest.Server is the Costguard proxy endpoint.
func newHarness(reg *providers.Registry, store *fakeStore) *harness {
	c := cache.NewMemory(1000)

	gw, err := gateway.New(gateway.Deps{
		Router:    &staticRouter{},
		Registry:  reg,
		Cache:     c,
		CacheTTL:  time.Minute,
		UsageStore: store,
		// BudgetChecker, AlertStore, Notifier, Log are all nil-safe in the gateway.
	})
	if err != nil {
		panic("gateway.New: " + err.Error())
	}

	mux := http.NewServeMux()
	openai_http.Register(mux, openai_http.Deps{Gateway: gw})

	srv := httptest.NewServer(mux)
	return &harness{server: srv, store: store}
}

// post sends a JSON POST to the Costguard proxy and returns the decoded response body.
func (h *harness) post(t *testing.T, providerName string, body any) map[string]any {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPost, h.server.URL+"/v1/chat/completions", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(gateway.HeaderProviderHint, providerName)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return result
}

func finishReason(t *testing.T, resp map[string]any) string {
	t.Helper()
	choices, _ := resp["choices"].([]any)
	if len(choices) == 0 {
		t.Fatal("no choices in response")
	}
	choice, _ := choices[0].(map[string]any)
	return choice["finish_reason"].(string)
}

func toolCalls(t *testing.T, resp map[string]any) []any {
	t.Helper()
	choices, _ := resp["choices"].([]any)
	choice, _ := choices[0].(map[string]any)
	msg, _ := choice["message"].(map[string]any)
	tc, _ := msg["tool_calls"].([]any)
	return tc
}

func textContent(t *testing.T, resp map[string]any) string {
	t.Helper()
	choices, _ := resp["choices"].([]any)
	choice, _ := choices[0].(map[string]any)
	msg, _ := choice["message"].(map[string]any)
	s, _ := msg["content"].(string)
	return s
}

// ---------------------------------------------------------------------------
// Fake upstream responses
// ---------------------------------------------------------------------------

// openAIToolCallResponse is the upstream response for turn 1 (OpenAI format,
// since the OpenAI adapter passes through unchanged).
func openAIToolCallResponse() map[string]any {
	return map[string]any{
		"id": "chatcmpl-test-1", "object": "chat.completion",
		"created": 1000000, "model": "gpt-4o-mini",
		"choices": []any{map[string]any{
			"index": 0,
			"message": map[string]any{
				"role": "assistant", "content": nil,
				"tool_calls": []any{map[string]any{
					"id": "call_abc", "type": "function",
					"function": map[string]any{"name": "get_weather", "arguments": `{"location":"London"}`},
				}},
			},
			"finish_reason": "tool_calls",
		}},
		"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120},
	}
}

func openAITextResponse() map[string]any {
	return map[string]any{
		"id": "chatcmpl-test-2", "object": "chat.completion",
		"created": 1000001, "model": "gpt-4o-mini",
		"choices": []any{map[string]any{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": "The weather in London is 15°C and cloudy."},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 150, "completion_tokens": 15, "total_tokens": 165},
	}
}

// anthropicToolUseResponse is what the real Anthropic API returns for turn 1.
// The Anthropic adapter transforms this to OpenAI tool_calls format.
func anthropicToolUseResponse() map[string]any {
	return map[string]any{
		"id": "msg_test1", "type": "message", "role": "assistant",
		"model": "claude-sonnet-4-5-20250929",
		"content": []any{map[string]any{
			"type": "tool_use", "id": "toolu_abc",
			"name":  "get_weather",
			"input": map[string]any{"location": "London"},
		}},
		"stop_reason": "tool_use",
		"usage":       map[string]any{"input_tokens": 100, "output_tokens": 20},
	}
}

func anthropicTextResponse() map[string]any {
	return map[string]any{
		"id": "msg_test2", "type": "message", "role": "assistant",
		"model":       "claude-sonnet-4-5-20250929",
		"content":     []any{map[string]any{"type": "text", "text": "The weather in London is 15°C and cloudy."}},
		"stop_reason": "end_turn",
		"usage":       map[string]any{"input_tokens": 150, "output_tokens": 15},
	}
}

// geminiToolCallResponse is what the Gemini API returns for turn 1.
// The Gemini adapter transforms this to OpenAI tool_calls format.
func geminiToolCallResponse() map[string]any {
	return map[string]any{
		"candidates": []any{map[string]any{
			"content": map[string]any{
				"role": "model",
				"parts": []any{map[string]any{
					"functionCall": map[string]any{
						"name": "get_weather",
						"args": map[string]any{"location": "London"},
					},
				}},
			},
			"finishReason": "STOP",
		}},
		"usageMetadata": map[string]any{
			"promptTokenCount": 100, "candidatesTokenCount": 20, "totalTokenCount": 120,
		},
		"modelVersion": "gemini-2.5-flash",
	}
}

func geminiTextResponse() map[string]any {
	return map[string]any{
		"candidates": []any{map[string]any{
			"content": map[string]any{
				"role":  "model",
				"parts": []any{map[string]any{"text": "The weather in London is 15°C and cloudy."}},
			},
			"finishReason": "STOP",
		}},
		"usageMetadata": map[string]any{
			"promptTokenCount": 150, "candidatesTokenCount": 15, "totalTokenCount": 165,
		},
		"modelVersion": "gemini-2.5-flash",
	}
}

// fakeUpstream returns an httptest.Server that responds with turn1Resp on the
// first call and turn2Resp on every subsequent call. It also counts calls.
func fakeUpstream(t *testing.T, path string, turn1Resp, turn2Resp map[string]any) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		resp := turn2Resp
		if n == 1 {
			resp = turn1Resp
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	return httptest.NewServer(mux), &calls
}

// ---------------------------------------------------------------------------
// Common request bodies
// ---------------------------------------------------------------------------

func toolCallRequest(model string) map[string]any {
	return map[string]any{
		"model": model,
		"messages": []any{
			map[string]any{"role": "user", "content": "What is the weather in London?"},
		},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "get_weather",
				"description": "Get the current weather",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"location": map[string]any{"type": "string"}},
					"required":   []any{"location"},
				},
			},
		}},
		"tool_choice": "auto",
	}
}

func toolResultRequest(model, toolCallID string) map[string]any {
	return map[string]any{
		"model": model,
		"messages": []any{
			map[string]any{"role": "user", "content": "What is the weather in London?"},
			map[string]any{
				"role": "assistant", "content": nil,
				"tool_calls": []any{map[string]any{
					"id": toolCallID, "type": "function",
					"function": map[string]any{"name": "get_weather", "arguments": `{"location":"London"}`},
				}},
			},
			map[string]any{"role": "tool", "tool_call_id": toolCallID, "content": "15°C and cloudy"},
		},
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestOpenAIToolCallingCycle(t *testing.T) {
	upstream, calls := fakeUpstream(t, "/v1/chat/completions", openAIToolCallResponse(), openAITextResponse())
	defer upstream.Close()

	client, err := openai_provider.NewClient(openai_provider.ClientConfig{
		Name:    "fake-openai",
		BaseURL: upstream.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatal(err)
	}

	reg := providers.NewRegistry()
	reg.Register("fake-openai", client)

	store := &fakeStore{}
	h := newHarness(reg, store)
	defer h.server.Close()

	// Turn 1: request with tools → should get tool_calls back.
	resp1 := h.post(t, "fake-openai", toolCallRequest("gpt-4o-mini"))
	if got := finishReason(t, resp1); got != "tool_calls" {
		t.Errorf("turn1 finish_reason: got %q, want tool_calls", got)
	}
	tc := toolCalls(t, resp1)
	if len(tc) != 1 {
		t.Fatalf("turn1: expected 1 tool call, got %d", len(tc))
	}
	callID, _ := tc[0].(map[string]any)["id"].(string)
	if callID == "" {
		t.Fatal("turn1: tool call id is empty")
	}

	// Verify metering recorded turn 1.
	records := store.all()
	if len(records) != 1 {
		t.Fatalf("expected 1 metering record after turn1, got %d", len(records))
	}
	if records[0].PromptTokens != 100 || records[0].CompletionTokens != 20 {
		t.Errorf("turn1 tokens: prompt=%d completion=%d", records[0].PromptTokens, records[0].CompletionTokens)
	}
	if records[0].CacheHit {
		t.Error("turn1 should not be a cache hit")
	}

	// Cache: repeating the same request should be served from cache (no new upstream call).
	h.post(t, "fake-openai", toolCallRequest("gpt-4o-mini"))
	if got := calls.Load(); got != 1 {
		t.Errorf("upstream should have been called once (cache served repeat); got %d calls", got)
	}
	// Cache hit produces a metering record with CacheHit=true.
	records = store.all()
	if len(records) != 2 {
		t.Fatalf("expected 2 metering records after cached repeat, got %d", len(records))
	}
	if !records[1].CacheHit {
		t.Error("second record should be a cache hit")
	}

	// Turn 2: send tool result → should get final text back.
	resp2 := h.post(t, "fake-openai", toolResultRequest("gpt-4o-mini", callID))
	if got := finishReason(t, resp2); got != "stop" {
		t.Errorf("turn2 finish_reason: got %q, want stop", got)
	}
	if text := textContent(t, resp2); text == "" {
		t.Error("turn2: expected non-empty text content")
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("expected 2 upstream calls total, got %d", got)
	}

	// Verify metering recorded turn 2.
	records = store.all()
	if len(records) != 3 {
		t.Fatalf("expected 3 metering records total, got %d", len(records))
	}
	if records[2].PromptTokens != 150 || records[2].CompletionTokens != 15 {
		t.Errorf("turn2 tokens: prompt=%d completion=%d", records[2].PromptTokens, records[2].CompletionTokens)
	}
}

func TestAnthropicToolCallingCycle(t *testing.T) {
	upstream, calls := fakeUpstream(t, "/v1/messages", anthropicToolUseResponse(), anthropicTextResponse())
	defer upstream.Close()

	client, err := anthropic_provider.NewClient(anthropic_provider.ClientConfig{
		Name:             "fake-anthropic",
		BaseURL:          upstream.URL,
		APIKey:           "test-key",
		AnthropicVersion: "2023-06-01",
	})
	if err != nil {
		t.Fatal(err)
	}

	reg := providers.NewRegistry()
	reg.Register("fake-anthropic", client)

	store := &fakeStore{}
	h := newHarness(reg, store)
	defer h.server.Close()

	// Turn 1: OpenAI-format request → adapter transforms to Anthropic → fake upstream
	// returns Anthropic tool_use → adapter transforms back to OpenAI tool_calls.
	resp1 := h.post(t, "fake-anthropic", toolCallRequest("claude-sonnet-4-5"))
	if got := finishReason(t, resp1); got != "tool_calls" {
		t.Errorf("turn1 finish_reason: got %q, want tool_calls", got)
	}
	tc := toolCalls(t, resp1)
	if len(tc) != 1 {
		t.Fatalf("turn1: expected 1 tool call, got %d", len(tc))
	}
	callID, _ := tc[0].(map[string]any)["id"].(string)
	if callID == "" {
		t.Fatal("turn1: tool call id is empty")
	}

	// Verify metering: Anthropic tokens come from input_tokens/output_tokens.
	records := store.all()
	if len(records) != 1 {
		t.Fatalf("expected 1 metering record after turn1, got %d", len(records))
	}
	if records[0].PromptTokens != 100 || records[0].CompletionTokens != 20 {
		t.Errorf("turn1 tokens: prompt=%d completion=%d", records[0].PromptTokens, records[0].CompletionTokens)
	}

	// Cache repeat.
	h.post(t, "fake-anthropic", toolCallRequest("claude-sonnet-4-5"))
	if got := calls.Load(); got != 1 {
		t.Errorf("upstream should have been called once (cache); got %d", got)
	}

	// Turn 2: send tool result → adapter maps role:tool → Anthropic tool_result
	// content block → fake upstream returns Anthropic text → adapter maps back.
	resp2 := h.post(t, "fake-anthropic", toolResultRequest("claude-sonnet-4-5", callID))
	if got := finishReason(t, resp2); got != "stop" {
		t.Errorf("turn2 finish_reason: got %q, want stop", got)
	}
	if text := textContent(t, resp2); text == "" {
		t.Error("turn2: expected non-empty text content")
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("expected 2 upstream calls total, got %d", got)
	}

	records = store.all()
	if records[len(records)-1].PromptTokens != 150 {
		t.Errorf("turn2 prompt tokens: got %d, want 150", records[len(records)-1].PromptTokens)
	}
}

func TestGeminiToolCallingCycle(t *testing.T) {
	// Gemini URLs look like /v1beta/models/{model}:generateContent
	upstream, calls := fakeUpstream(t, "/v1beta/models/", geminiToolCallResponse(), geminiTextResponse())
	defer upstream.Close()

	client, err := gemini_provider.NewClient(gemini_provider.ClientConfig{
		Name:    "fake-gemini",
		BaseURL: upstream.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatal(err)
	}

	reg := providers.NewRegistry()
	reg.Register("fake-gemini", client)

	store := &fakeStore{}
	h := newHarness(reg, store)
	defer h.server.Close()

	// Turn 1: OpenAI-format request → adapter transforms to Gemini (functionDeclarations)
	// → fake upstream returns Gemini functionCall → adapter maps back to tool_calls.
	resp1 := h.post(t, "fake-gemini", toolCallRequest("gemini-2.5-flash"))
	if got := finishReason(t, resp1); got != "tool_calls" {
		t.Errorf("turn1 finish_reason: got %q, want tool_calls", got)
	}
	tc := toolCalls(t, resp1)
	if len(tc) != 1 {
		t.Fatalf("turn1: expected 1 tool call, got %d", len(tc))
	}
	// Gemini synthesises the id as call_{name}.
	callID, _ := tc[0].(map[string]any)["id"].(string)
	if callID != "call_get_weather" {
		t.Errorf("turn1: expected synthesised id call_get_weather, got %q", callID)
	}

	// Verify metering: Gemini tokens come from promptTokenCount/candidatesTokenCount.
	records := store.all()
	if len(records) != 1 {
		t.Fatalf("expected 1 metering record after turn1, got %d", len(records))
	}
	if records[0].PromptTokens != 100 || records[0].CompletionTokens != 20 {
		t.Errorf("turn1 tokens: prompt=%d completion=%d", records[0].PromptTokens, records[0].CompletionTokens)
	}

	// Cache repeat.
	h.post(t, "fake-gemini", toolCallRequest("gemini-2.5-flash"))
	if got := calls.Load(); got != 1 {
		t.Errorf("upstream should have been called once (cache); got %d", got)
	}

	// Turn 2: send tool result → adapter maps role:tool → Gemini functionResponse
	// → fake upstream returns text → adapter maps back to OpenAI text.
	resp2 := h.post(t, "fake-gemini", toolResultRequest("gemini-2.5-flash", callID))
	if got := finishReason(t, resp2); got != "stop" {
		t.Errorf("turn2 finish_reason: got %q, want stop", got)
	}
	if text := textContent(t, resp2); text == "" {
		t.Error("turn2: expected non-empty text content")
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("expected 2 upstream calls total, got %d", got)
	}

	records = store.all()
	if records[len(records)-1].PromptTokens != 150 {
		t.Errorf("turn2 prompt tokens: got %d, want 150", records[len(records)-1].PromptTokens)
	}
}

func TestCacheSkipsToolCallingRequestsWithDifferentBodies(t *testing.T) {
	// Two different tool-calling requests (different messages) must have different
	// cache keys and both reach the upstream.
	upstream, calls := fakeUpstream(t, "/v1/chat/completions", openAIToolCallResponse(), openAIToolCallResponse())
	defer upstream.Close()

	client, _ := openai_provider.NewClient(openai_provider.ClientConfig{
		Name: "fake-openai-cache", BaseURL: upstream.URL, APIKey: "test-key",
	})
	reg := providers.NewRegistry()
	reg.Register("fake-openai-cache", client)

	h := newHarness(reg, &fakeStore{})
	defer h.server.Close()

	req1 := toolCallRequest("gpt-4o-mini")
	req2 := toolCallRequest("gpt-4o-mini")
	// Mutate req2 to have a different user message → different cache key.
	req2["messages"] = []any{
		map[string]any{"role": "user", "content": "What is the weather in Paris?"},
	}

	h.post(t, "fake-openai-cache", req1)
	h.post(t, "fake-openai-cache", req2)

	if got := calls.Load(); got != 2 {
		t.Errorf("expected 2 upstream calls for distinct requests, got %d", got)
	}
}
