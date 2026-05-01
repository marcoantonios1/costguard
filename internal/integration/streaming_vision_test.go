package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marcoantonios1/costguard/internal/gateway"
	"github.com/marcoantonios1/costguard/internal/providers"
	anthropic_provider "github.com/marcoantonios1/costguard/internal/providers/anthropic"
	openai_provider "github.com/marcoantonios1/costguard/internal/providers/openai"
	"github.com/marcoantonios1/costguard/internal/usage"
)

// ---------------------------------------------------------------------------
// SSE fake upstream helpers
// ---------------------------------------------------------------------------

// sseUpstream starts a fake upstream server at path that writes rawSSE as a
// text/event-stream response. The raw string must be pre-formatted SSE lines.
func sseUpstream(t *testing.T, path, rawSSE string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, rawSSE)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	})
	return httptest.NewServer(mux)
}

// streamPayloadWithImage builds a streaming chat request body containing one
// image_url content block.
func streamPayloadWithImage(model, imageURL string) map[string]any {
	return map[string]any{
		"model":  model,
		"stream": true,
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "Describe this."},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": imageURL}},
				},
			},
		},
	}
}

// streamPayload builds a plain streaming chat request (no image).
func streamPayload(model string) map[string]any {
	return map[string]any{
		"model":  model,
		"stream": true,
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
		},
	}
}

// postStreaming sends a streaming POST to the gateway server and drains the
// full SSE response body. Returns (statusCode, body).
func postStreaming(t *testing.T, srv *httptest.Server, providerName string, body any) (int, []byte) {
	t.Helper()
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(gateway.HeaderProviderHint, providerName)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	// Drain the full SSE body — this triggers StreamMeter.finish().
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// waitForUsageRecord polls the accumulating store until at least one record
// appears or the timeout expires. Returns the last-saved record.
func waitForUsageRecord(t *testing.T, store *accumulatingStore, timeout time.Duration) usage.Record {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if records := store.all(); len(records) > 0 {
			return records[len(records)-1]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for usage record")
	return usage.Record{}
}

// ---------------------------------------------------------------------------
// Streaming vision metering tests
// ---------------------------------------------------------------------------

// TestStreaming_VisionTokens_Anthropic: a streaming Anthropic request that
// contains an image must produce a metering record with prompt_tokens >= 3125
// (the tile-based estimate for a 1024×1024 image using the Anthropic formula).
func TestStreaming_VisionTokens_Anthropic(t *testing.T) {
	// Minimal valid Anthropic SSE with zero token counts.
	// translateAnthropicStream will emit a usage chunk with total_tokens=0,
	// which StreamMeter ignores — triggering the fallback+vision estimation.
	anthSSE := "" +
		"event: message_start\n" +
		`data: {"message":{"id":"msg_sv1","model":"claude-sonnet-4-5-20250929","usage":{"input_tokens":0}}}` + "\n\n" +
		"event: content_block_start\n" +
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n" +
		"event: content_block_delta\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}` + "\n\n" +
		"event: content_block_stop\n" +
		`data: {"type":"content_block_stop","index":0}` + "\n\n" +
		"event: message_delta\n" +
		`data: {"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0}}` + "\n\n" +
		"event: message_stop\n" +
		`data: {"type":"message_stop"}` + "\n\n"

	upstream := sseUpstream(t, "/v1/messages", anthSSE)
	defer upstream.Close()

	client, _ := anthropic_provider.NewClient(anthropic_provider.ClientConfig{
		Name: "fake-anth-stream", BaseURL: upstream.URL,
		APIKey: "test-key", AnthropicVersion: "2023-06-01",
	})
	reg := providers.NewRegistry()
	reg.Register("fake-anth-stream", client)
	srv, store := newHarnessWithBudget(reg, 1000)
	defer srv.Close()

	status, body := postStreaming(t, srv, "fake-anth-stream",
		streamPayloadWithImage("claude-sonnet-4-5-20250929", "https://example.com/photo.png"))
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}

	rec := waitForUsageRecord(t, store, 200*time.Millisecond)

	const wantMin = 3125 // 1024×1024 default → Anthropic tile formula
	if rec.PromptTokens < wantMin {
		t.Errorf("PromptTokens: got %d, want >= %d (Anthropic tile formula for 1024×1024)", rec.PromptTokens, wantMin)
	}
}

// TestStreaming_VisionTokens_OpenAI: a streaming OpenAI request that contains
// an image must produce a metering record with prompt_tokens >= 765
// (the tile-based estimate for a 1024×1024 high-detail image via OpenAI formula).
func TestStreaming_VisionTokens_OpenAI(t *testing.T) {
	// OpenAI SSE with no usage chunk — triggers fallback+vision estimation.
	oaiSSE := "" +
		`data: {"id":"c1","model":"gpt-4o-mini","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}` + "\n\n" +
		`data: {"id":"c1","model":"gpt-4o-mini","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}` + "\n\n" +
		"data: [DONE]\n\n"

	upstream := sseUpstream(t, "/v1/chat/completions", oaiSSE)
	defer upstream.Close()

	client, _ := openai_provider.NewClient(openai_provider.ClientConfig{
		Name: "fake-oai-stream", BaseURL: upstream.URL, APIKey: "test-key",
	})
	reg := providers.NewRegistry()
	reg.Register("fake-oai-stream", client)
	srv, store := newHarnessWithBudget(reg, 1000)
	defer srv.Close()

	status, body := postStreaming(t, srv, "fake-oai-stream",
		streamPayloadWithImage("gpt-4o-mini", "https://example.com/photo.png"))
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}

	rec := waitForUsageRecord(t, store, 200*time.Millisecond)

	const wantMin = 765 // 1024×1024 → OpenAI tile formula (high/auto detail)
	if rec.PromptTokens < wantMin {
		t.Errorf("PromptTokens: got %d, want >= %d (OpenAI tile formula for 1024×1024)", rec.PromptTokens, wantMin)
	}
}

// TestStreaming_NonVision_NoFalseTokens: a streaming request with no image
// content must not have phantom vision tokens added to the metering record.
// The upstream provides a real usage chunk so the exact prompt count is known.
func TestStreaming_NonVision_NoFalseTokens(t *testing.T) {
	// OpenAI SSE with explicit usage chunk: prompt=10, completion=5, total=15.
	usageChunk := `{"id":"c1","model":"gpt-4o-mini","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`
	oaiSSE := "" +
		`data: {"id":"c1","model":"gpt-4o-mini","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}` + "\n\n" +
		"data: " + usageChunk + "\n\n" +
		"data: [DONE]\n\n"

	upstream := sseUpstream(t, "/v1/chat/completions", oaiSSE)
	defer upstream.Close()

	client, _ := openai_provider.NewClient(openai_provider.ClientConfig{
		Name: "fake-oai-novision", BaseURL: upstream.URL, APIKey: "test-key",
	})
	reg := providers.NewRegistry()
	reg.Register("fake-oai-novision", client)
	srv, store := newHarnessWithBudget(reg, 1000)
	defer srv.Close()

	status, body := postStreaming(t, srv, "fake-oai-novision",
		streamPayload("gpt-4o-mini"))
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}

	rec := waitForUsageRecord(t, store, 200*time.Millisecond)

	// Upstream reported prompt=10 and there were no images: result must be
	// exactly 10 — no phantom vision tokens added.
	if rec.PromptTokens != 10 {
		t.Errorf("PromptTokens: got %d, want 10 (no vision tokens should be added)", rec.PromptTokens)
	}
	if rec.CompletionTokens != 5 {
		t.Errorf("CompletionTokens: got %d, want 5", rec.CompletionTokens)
	}
}
