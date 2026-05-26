package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
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

// fakeEmbeddingUpstream starts a fake embedding server that returns a standard
// OpenAI-shaped response with 768-dimensional embeddings and token counts.
func fakeEmbeddingUpstream(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/embeddings", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []any{map[string]any{
				"object":    "embedding",
				"index":     0,
				"embedding": make([]float64, 768),
			}},
			"model": "nomic-embed-text",
			"usage": map[string]any{
				"prompt_tokens": 12,
				"total_tokens":  12,
			},
		})
	})
	return httptest.NewServer(mux), &calls
}

// newEmbeddingHarness wires a gateway with the given embedding provider name
// pointing at a pre-registered client, returning the test HTTP server.
func newEmbeddingHarness(reg *providers.Registry, providerName string, store *fakeStore) (*httptest.Server, error) {
	gw, err := gateway.New(gateway.Deps{
		Router:                &staticRouter{},
		Registry:              reg,
		Cache:                 cache.NewMemory(0),
		UsageStore:            store,
		EmbeddingProvider:     "ollama",
		EmbeddingProviderName: providerName,
	})
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	openai_http.Register(mux, openai_http.Deps{Gateway: gw})
	return httptest.NewServer(mux), nil
}

func postEmbedding(t *testing.T, srv *httptest.Server, providerHint string) (int, []byte) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"model": "nomic-embed-text",
		"input": "hello world",
	})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/embeddings", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if providerHint != "" {
		req.Header.Set("X-Costguard-Provider", providerHint)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /v1/embeddings: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// ---------------------------------------------------------------------------
// Test 1 — Ollama provider proxies and meters
// ---------------------------------------------------------------------------

// TestEmbeddings_OllamaProvider_ProxiesAndMeters verifies that:
//   - POST /v1/embeddings is forwarded to the configured provider
//   - The upstream response is returned unchanged
//   - A usage record is saved with the correct token counts
func TestEmbeddings_OllamaProvider_ProxiesAndMeters(t *testing.T) {
	upstream, calls := fakeEmbeddingUpstream(t)
	defer upstream.Close()

	client, err := openai_provider.NewClient(openai_provider.ClientConfig{
		Name:    "local-ollama",
		BaseURL: upstream.URL,
		APIKey:  "ignored",
	})
	if err != nil {
		t.Fatal(err)
	}
	reg := providers.NewRegistry()
	reg.Register("local-ollama", client)

	store := &fakeStore{}
	srv, err := newEmbeddingHarness(reg, "local-ollama", store)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	status, body := postEmbedding(t, srv, "")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}

	// Upstream was called exactly once.
	if n := calls.Load(); n != 1 {
		t.Errorf("upstream calls: got %d, want 1", n)
	}

	// Response contains embeddings.
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("response is not JSON: %v — body: %s", err, body)
	}
	data, ok := got["data"].([]any)
	if !ok || len(data) == 0 {
		t.Fatalf("expected data array in response, got: %s", body)
	}

	// Usage record was saved with correct token counts.
	records := store.all()
	if len(records) != 1 {
		t.Fatalf("expected 1 usage record, got %d", len(records))
	}
	rec := records[0]
	if rec.PromptTokens != 12 {
		t.Errorf("PromptTokens: got %d, want 12", rec.PromptTokens)
	}
	if rec.TotalTokens != 12 {
		t.Errorf("TotalTokens: got %d, want 12", rec.TotalTokens)
	}
	if rec.MeteringEstimated {
		t.Errorf("MeteringEstimated: got true, want false")
	}
	if rec.CompletionTokens != 0 {
		t.Errorf("CompletionTokens: got %d, want 0", rec.CompletionTokens)
	}
}

// ---------------------------------------------------------------------------
// Test 2 — X-Costguard-Provider hint overrides configured provider
// ---------------------------------------------------------------------------

// TestEmbeddings_ProviderHint_Accepted verifies that X-Costguard-Provider
// header overrides the configured embedding provider.
func TestEmbeddings_ProviderHint_Accepted(t *testing.T) {
	upstream, calls := fakeEmbeddingUpstream(t)
	defer upstream.Close()

	client, err := openai_provider.NewClient(openai_provider.ClientConfig{
		Name:    "local-ollama",
		BaseURL: upstream.URL,
		APIKey:  "ignored",
	})
	if err != nil {
		t.Fatal(err)
	}
	reg := providers.NewRegistry()
	reg.Register("local-ollama", client)

	// Wire with a different (empty) embeddingProviderName; the hint must win.
	gw, err := gateway.New(gateway.Deps{
		Router:                &staticRouter{},
		Registry:              reg,
		Cache:                 cache.NewMemory(0),
		EmbeddingProvider:     "ollama",
		EmbeddingProviderName: "", // deliberately unset — hint must override
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	openai_http.Register(mux, openai_http.Deps{Gateway: gw})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	status, body := postEmbedding(t, srv, "local-ollama")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("upstream calls: got %d, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// Test 3 — Unknown provider hint returns 400
// ---------------------------------------------------------------------------

// TestEmbeddings_UnknownProviderHint_Returns400 verifies that an unknown
// X-Costguard-Provider hint returns 400.
func TestEmbeddings_UnknownProviderHint_Returns400(t *testing.T) {
	reg := providers.NewRegistry()

	gw, err := gateway.New(gateway.Deps{
		Router:   &staticRouter{},
		Registry: reg,
		Cache:    cache.NewMemory(0),
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	openai_http.Register(mux, openai_http.Deps{Gateway: gw})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	status, body := postEmbedding(t, srv, "nonexistent-provider")
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", status, body)
	}

	var errResp map[string]any
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("response is not JSON: %v — body: %s", err, body)
	}
}
