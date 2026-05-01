package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marcoantonios1/costguard/internal/cache"
	"github.com/marcoantonios1/costguard/internal/gateway"
	"github.com/marcoantonios1/costguard/internal/providers"
	anthropic_provider "github.com/marcoantonios1/costguard/internal/providers/anthropic"
	openai_provider "github.com/marcoantonios1/costguard/internal/providers/openai"
	"github.com/marcoantonios1/costguard/internal/router"
	openai_http "github.com/marcoantonios1/costguard/internal/server/openai"
)

// ---------------------------------------------------------------------------
// Routing harness
// ---------------------------------------------------------------------------

// newRoutingHarness wires a gateway using the real router.Router (not the
// staticRouter stub used everywhere else). Providers must be registered under
// the names returned by MatchProviderByModel: "openai_primary" and
// "anthropic_primary". No X-Costguard-Provider hint is set on requests so
// every dispatch goes through PickProvider → MatchProviderByModel.
func newRoutingHarness(openaiBaseURL, anthropicBaseURL string, store *fakeStore) (*httptest.Server, error) {
	openaiClient, err := openai_provider.NewClient(openai_provider.ClientConfig{
		Name:    "openai_primary",
		BaseURL: openaiBaseURL,
		APIKey:  "test-key",
	})
	if err != nil {
		return nil, err
	}

	anthropicClient, err := anthropic_provider.NewClient(anthropic_provider.ClientConfig{
		Name:             "anthropic_primary",
		BaseURL:          anthropicBaseURL,
		APIKey:           "test-key",
		AnthropicVersion: "2023-06-01",
	})
	if err != nil {
		return nil, err
	}

	reg := providers.NewRegistry()
	reg.Register("openai_primary", openaiClient)
	reg.Register("anthropic_primary", anthropicClient)

	// router.New with an empty config falls through to MatchProviderByModel for
	// every model that is not in the explicit modelToProvider table.
	r := router.New(router.Config{})

	gw, err := gateway.New(gateway.Deps{
		Router:     r,
		Registry:   reg,
		Cache:      cache.NewMemory(0),
		UsageStore: store,
	})
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	openai_http.Register(mux, openai_http.Deps{Gateway: gw})
	return httptest.NewServer(mux), nil
}

// postRoute sends a POST to /v1/chat/completions without any provider hint
// header so routing is determined purely by the model name in the body.
func postRoute(t *testing.T, srv *httptest.Server, body any) (int, []byte) {
	t.Helper()
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	// Intentionally no X-Costguard-Provider header — model name drives routing.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestRouting_GptPrefix_RoutesToOpenAI verifies that a request carrying a
// gpt-* model name is dispatched to the OpenAI provider by MatchProviderByModel
// without any explicit provider hint.
func TestRouting_GptPrefix_RoutesToOpenAI(t *testing.T) {
	openaiUpstream, openaiCalls := alwaysSucceedUpstream(t, "/v1/chat/completions", openAITextResponse())
	defer openaiUpstream.Close()

	// Anthropic upstream must exist so the registry is fully populated, but
	// it should never receive a call for a gpt-* model.
	anthropicUpstream, anthropicCalls := alwaysSucceedUpstream(t, "/v1/messages", anthropicTextResponse())
	defer anthropicUpstream.Close()

	store := &fakeStore{}
	srv, err := newRoutingHarness(openaiUpstream.URL, anthropicUpstream.URL, store)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	status, body := postRoute(t, srv, simpleRequest("gpt-4o-mini"))
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}

	if got := openaiCalls.Load(); got != 1 {
		t.Errorf("openai upstream calls: got %d, want 1", got)
	}
	if got := anthropicCalls.Load(); got != 0 {
		t.Errorf("anthropic upstream calls: got %d, want 0 (gpt-* must not reach Anthropic)", got)
	}

	records := store.all()
	if len(records) != 1 {
		t.Fatalf("expected 1 usage record, got %d", len(records))
	}
	if records[0].Provider != "openai_primary" {
		t.Errorf("record.Provider: got %q, want openai_primary", records[0].Provider)
	}
}

// TestRouting_ClaudePrefix_RoutesToAnthropic verifies that a request carrying
// a claude-* model name is dispatched to the Anthropic provider.
func TestRouting_ClaudePrefix_RoutesToAnthropic(t *testing.T) {
	openaiUpstream, openaiCalls := alwaysSucceedUpstream(t, "/v1/chat/completions", openAITextResponse())
	defer openaiUpstream.Close()

	anthropicUpstream, anthropicCalls := alwaysSucceedUpstream(t, "/v1/messages", anthropicTextResponse())
	defer anthropicUpstream.Close()

	store := &fakeStore{}
	srv, err := newRoutingHarness(openaiUpstream.URL, anthropicUpstream.URL, store)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	status, body := postRoute(t, srv, simpleRequest("claude-3-5-sonnet-20241022"))
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}

	if got := anthropicCalls.Load(); got != 1 {
		t.Errorf("anthropic upstream calls: got %d, want 1", got)
	}
	if got := openaiCalls.Load(); got != 0 {
		t.Errorf("openai upstream calls: got %d, want 0 (claude-* must not reach OpenAI)", got)
	}

	records := store.all()
	if len(records) != 1 {
		t.Fatalf("expected 1 usage record, got %d", len(records))
	}
	if records[0].Provider != "anthropic_primary" {
		t.Errorf("record.Provider: got %q, want anthropic_primary", records[0].Provider)
	}
}

// TestRouting_NoCrossContamination sends a gpt-* request followed by a
// claude-* request through the same gateway and verifies each reaches exactly
// its own provider with zero bleed to the other.
func TestRouting_NoCrossContamination(t *testing.T) {
	openaiUpstream, openaiCalls := alwaysSucceedUpstream(t, "/v1/chat/completions", openAITextResponse())
	defer openaiUpstream.Close()

	anthropicUpstream, anthropicCalls := alwaysSucceedUpstream(t, "/v1/messages", anthropicTextResponse())
	defer anthropicUpstream.Close()

	store := &fakeStore{}
	srv, err := newRoutingHarness(openaiUpstream.URL, anthropicUpstream.URL, store)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	// First: gpt-* → OpenAI only.
	if status, body := postRoute(t, srv, simpleRequest("gpt-4o")); status != http.StatusOK {
		t.Fatalf("gpt request: expected 200, got %d: %s", status, body)
	}
	if got := openaiCalls.Load(); got != 1 {
		t.Errorf("after gpt request — openai calls: got %d, want 1", got)
	}
	if got := anthropicCalls.Load(); got != 0 {
		t.Errorf("after gpt request — anthropic calls: got %d, want 0", got)
	}

	// Second: claude-* → Anthropic only; OpenAI call count must not change.
	if status, body := postRoute(t, srv, simpleRequest("claude-sonnet-4-5")); status != http.StatusOK {
		t.Fatalf("claude request: expected 200, got %d: %s", status, body)
	}
	if got := anthropicCalls.Load(); got != 1 {
		t.Errorf("after claude request — anthropic calls: got %d, want 1", got)
	}
	if got := openaiCalls.Load(); got != 1 {
		t.Errorf("after claude request — openai calls: got %d, want 1 (must not increase)", got)
	}

	// Usage records: first names openai_primary, second names anthropic_primary.
	records := store.all()
	if len(records) != 2 {
		t.Fatalf("expected 2 usage records, got %d", len(records))
	}
	if records[0].Provider != "openai_primary" {
		t.Errorf("records[0].Provider: got %q, want openai_primary", records[0].Provider)
	}
	if records[1].Provider != "anthropic_primary" {
		t.Errorf("records[1].Provider: got %q, want anthropic_primary", records[1].Provider)
	}
}

// TestRouting_UnknownModel_Returns502 verifies that a model name with no
// matching prefix produces a 502 (the router returns "" and the registry
// lookup fails with a retryable "provider not found" error which propagates
// as a Bad Gateway when no fallback is configured).
func TestRouting_UnknownModel_Returns502(t *testing.T) {
	openaiUpstream, openaiCalls := alwaysSucceedUpstream(t, "/v1/chat/completions", openAITextResponse())
	defer openaiUpstream.Close()

	anthropicUpstream, anthropicCalls := alwaysSucceedUpstream(t, "/v1/messages", anthropicTextResponse())
	defer anthropicUpstream.Close()

	store := &fakeStore{}
	srv, err := newRoutingHarness(openaiUpstream.URL, anthropicUpstream.URL, store)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	status, _ := postRoute(t, srv, simpleRequest("unknown-model-xyz"))
	if status != http.StatusBadGateway {
		t.Errorf("expected 502 for unroutable model, got %d", status)
	}

	// Neither provider should have been called.
	if got := openaiCalls.Load(); got != 0 {
		t.Errorf("openai calls: got %d, want 0", got)
	}
	if got := anthropicCalls.Load(); got != 0 {
		t.Errorf("anthropic calls: got %d, want 0", got)
	}
}
