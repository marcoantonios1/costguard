package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/marcoantonios1/costguard/internal/cache"
	"github.com/marcoantonios1/costguard/internal/gateway"
	"github.com/marcoantonios1/costguard/internal/providers"
	openai_provider "github.com/marcoantonios1/costguard/internal/providers/openai"
	openai_http "github.com/marcoantonios1/costguard/internal/server/openai"
)

// ---------------------------------------------------------------------------
// Fallback-specific upstream helpers
// ---------------------------------------------------------------------------

// alwaysFailUpstream starts a fake upstream at path that always responds with
// the given HTTP status code and a JSON error body, counting each call.
func alwaysFailUpstream(t *testing.T, path string, statusCode int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(`{"error":{"message":"simulated failure","type":"server_error"}}`))
	})
	return httptest.NewServer(mux), &calls
}

// alwaysSucceedUpstream starts a fake upstream at path that always responds
// 200 OK with the given JSON body, counting each call.
func alwaysSucceedUpstream(t *testing.T, path string, resp map[string]any) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	return httptest.NewServer(mux), &calls
}

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

// newFallbackHarness wires a gateway with FallbackProvider set to
// fallbackName, caching disabled, and the given store. The caller is
// responsible for registering primary and fallback clients in reg before
// calling this function.
func newFallbackHarness(reg *providers.Registry, fallbackName string, store *fakeStore) *httptest.Server {
	gw, err := gateway.New(gateway.Deps{
		Router:           &staticRouter{},
		Registry:         reg,
		Cache:            cache.NewMemory(0),
		UsageStore:       store,
		FallbackProvider: fallbackName,
	})
	if err != nil {
		panic("gateway.New: " + err.Error())
	}
	mux := http.NewServeMux()
	openai_http.Register(mux, openai_http.Deps{Gateway: gw})
	return httptest.NewServer(mux)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestFallback_PrimaryFails_FallbackSucceeds verifies the core fallback path:
// when the primary upstream returns 5xx (retryable), the gateway transparently
// retries via the fallback provider and returns a 200 to the client.
//
// The usage record Provider field being set to the fallback provider name is
// the observable proof that the fallback_used code path was executed.
func TestFallback_PrimaryFails_FallbackSucceeds(t *testing.T) {
	const path = "/v1/chat/completions"

	primaryUpstream, primaryCalls := alwaysFailUpstream(t, path, http.StatusInternalServerError)
	defer primaryUpstream.Close()

	fallbackUpstream, fallbackCalls := alwaysSucceedUpstream(t, path, openAITextResponse())
	defer fallbackUpstream.Close()

	primaryClient, _ := openai_provider.NewClient(openai_provider.ClientConfig{
		Name: "primary-openai", BaseURL: primaryUpstream.URL, APIKey: "test-key",
	})
	fallbackClient, _ := openai_provider.NewClient(openai_provider.ClientConfig{
		Name: "fallback-openai", BaseURL: fallbackUpstream.URL, APIKey: "test-key",
	})

	reg := providers.NewRegistry()
	reg.Register("primary-openai", primaryClient)
	reg.Register("fallback-openai", fallbackClient)

	store := &fakeStore{}
	srv := newFallbackHarness(reg, "fallback-openai", store)
	defer srv.Close()

	// Pin to primary via hint; fallback is triggered by the 500.
	status, body := postWithHeaders(t, srv, "primary-openai", simpleRequest("gpt-4o-mini"), nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200 after fallback, got %d: %s", status, body)
	}

	// Primary must have been tried exactly once.
	if got := primaryCalls.Load(); got != 1 {
		t.Errorf("primary upstream calls: got %d, want 1", got)
	}
	// Fallback must have been called exactly once.
	if got := fallbackCalls.Load(); got != 1 {
		t.Errorf("fallback upstream calls: got %d, want 1", got)
	}

	// The usage record must name the fallback as the actual provider, proving
	// the fallback_used code path was reached.
	records := store.all()
	if len(records) != 1 {
		t.Fatalf("expected 1 usage record, got %d", len(records))
	}
	if records[0].Provider != "fallback-openai" {
		t.Errorf("record.Provider: got %q, want fallback-openai", records[0].Provider)
	}
}

// TestFallback_PrimarySucceeds_FallbackNotCalled verifies that when the
// primary provider returns 200 the fallback is never contacted.
func TestFallback_PrimarySucceeds_FallbackNotCalled(t *testing.T) {
	const path = "/v1/chat/completions"

	primaryUpstream, primaryCalls := alwaysSucceedUpstream(t, path, openAITextResponse())
	defer primaryUpstream.Close()

	fallbackUpstream, fallbackCalls := alwaysSucceedUpstream(t, path, openAITextResponse())
	defer fallbackUpstream.Close()

	primaryClient, _ := openai_provider.NewClient(openai_provider.ClientConfig{
		Name: "primary-ok", BaseURL: primaryUpstream.URL, APIKey: "test-key",
	})
	fallbackClient, _ := openai_provider.NewClient(openai_provider.ClientConfig{
		Name: "fallback-ok", BaseURL: fallbackUpstream.URL, APIKey: "test-key",
	})

	reg := providers.NewRegistry()
	reg.Register("primary-ok", primaryClient)
	reg.Register("fallback-ok", fallbackClient)

	store := &fakeStore{}
	srv := newFallbackHarness(reg, "fallback-ok", store)
	defer srv.Close()

	status, body := postWithHeaders(t, srv, "primary-ok", simpleRequest("gpt-4o-mini"), nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}

	if got := primaryCalls.Load(); got != 1 {
		t.Errorf("primary upstream calls: got %d, want 1", got)
	}
	if got := fallbackCalls.Load(); got != 0 {
		t.Errorf("fallback upstream calls: got %d, want 0 (should not be contacted)", got)
	}

	records := store.all()
	if len(records) != 1 {
		t.Fatalf("expected 1 usage record, got %d", len(records))
	}
	if records[0].Provider != "primary-ok" {
		t.Errorf("record.Provider: got %q, want primary-ok", records[0].Provider)
	}
}

// TestFallback_4xxNotRetried verifies that a 4xx response from the primary is
// passed straight through to the client without attempting the fallback.
// A 4xx indicates a client-side error (bad request) rather than a transient
// provider failure, so retrying would be incorrect.
func TestFallback_4xxNotRetried(t *testing.T) {
	const path = "/v1/chat/completions"

	primaryUpstream, primaryCalls := alwaysFailUpstream(t, path, http.StatusBadRequest)
	defer primaryUpstream.Close()

	fallbackUpstream, fallbackCalls := alwaysSucceedUpstream(t, path, openAITextResponse())
	defer fallbackUpstream.Close()

	primaryClient, _ := openai_provider.NewClient(openai_provider.ClientConfig{
		Name: "primary-4xx", BaseURL: primaryUpstream.URL, APIKey: "test-key",
	})
	fallbackClient, _ := openai_provider.NewClient(openai_provider.ClientConfig{
		Name: "fallback-4xx", BaseURL: fallbackUpstream.URL, APIKey: "test-key",
	})

	reg := providers.NewRegistry()
	reg.Register("primary-4xx", primaryClient)
	reg.Register("fallback-4xx", fallbackClient)

	store := &fakeStore{}
	srv := newFallbackHarness(reg, "fallback-4xx", store)
	defer srv.Close()

	status, _ := postWithHeaders(t, srv, "primary-4xx", simpleRequest("gpt-4o-mini"), nil)
	if status != http.StatusBadRequest {
		t.Errorf("expected 400 (4xx passed through), got %d", status)
	}

	if got := primaryCalls.Load(); got != 1 {
		t.Errorf("primary upstream calls: got %d, want 1", got)
	}
	if got := fallbackCalls.Load(); got != 0 {
		t.Errorf("fallback upstream calls: got %d, want 0 (4xx must not trigger fallback)", got)
	}
}

// TestFallback_BothFail verifies that when both the primary and fallback
// providers return 5xx the gateway returns 502 Bad Gateway to the client.
func TestFallback_BothFail(t *testing.T) {
	const path = "/v1/chat/completions"

	primaryUpstream, primaryCalls := alwaysFailUpstream(t, path, http.StatusInternalServerError)
	defer primaryUpstream.Close()

	fallbackUpstream, fallbackCalls := alwaysFailUpstream(t, path, http.StatusInternalServerError)
	defer fallbackUpstream.Close()

	primaryClient, _ := openai_provider.NewClient(openai_provider.ClientConfig{
		Name: "primary-both-fail", BaseURL: primaryUpstream.URL, APIKey: "test-key",
	})
	fallbackClient, _ := openai_provider.NewClient(openai_provider.ClientConfig{
		Name: "fallback-both-fail", BaseURL: fallbackUpstream.URL, APIKey: "test-key",
	})

	reg := providers.NewRegistry()
	reg.Register("primary-both-fail", primaryClient)
	reg.Register("fallback-both-fail", fallbackClient)

	store := &fakeStore{}
	srv := newFallbackHarness(reg, "fallback-both-fail", store)
	defer srv.Close()

	status, _ := postWithHeaders(t, srv, "primary-both-fail", simpleRequest("gpt-4o-mini"), nil)
	if status != http.StatusBadGateway {
		t.Errorf("expected 502 when both providers fail, got %d", status)
	}

	// Both upstreams must have been attempted.
	if got := primaryCalls.Load(); got != 1 {
		t.Errorf("primary upstream calls: got %d, want 1", got)
	}
	if got := fallbackCalls.Load(); got != 1 {
		t.Errorf("fallback upstream calls: got %d, want 1", got)
	}
}
