package integration_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	openai_provider "github.com/marcoantonios1/costguard/internal/providers/openai"
	"github.com/marcoantonios1/costguard/internal/providers"
)

// fakeErrorUpstream returns a test server that always responds with statusCode
// and a JSON error body, counting total calls made to path.
func fakeErrorUpstream(t *testing.T, path string, statusCode int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		fmt.Fprintf(w, `{"error":{"message":"upstream error","type":"error","code":"%d"}}`, statusCode)
	})
	return httptest.NewServer(mux), &calls
}

// newErrorReg registers an OpenAI client against the given upstream URL.
func newErrorReg(t *testing.T, name, upstreamURL string) (*providers.Registry, string) {
	t.Helper()
	client, err := openai_provider.NewClient(openai_provider.ClientConfig{
		Name: name, BaseURL: upstreamURL, APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	reg := providers.NewRegistry()
	reg.Register(name, client)
	return reg, name
}

// TestMeter_4xxUpstream verifies that a 4xx upstream response produces exactly
// one usage record with the real status code, zero cost, and PriceFound=false.
// Team/Project/Agent headers must be carried through so per-dimension breakdowns
// remain possible from /admin/usage/*.
func TestMeter_4xxUpstream(t *testing.T) {
	upstream, upstreamCalls := fakeErrorUpstream(t, "/v1/chat/completions", http.StatusTooManyRequests)
	defer upstream.Close()

	reg, provider := newErrorReg(t, "openai-4xx-test", upstream.URL)
	store := &fakeStore{}
	h := newHarness(reg, store)
	defer h.server.Close()

	status, _ := postWithHeaders(t, h.server, provider, simpleRequest("gpt-4o-mini"), map[string]string{
		"X-Costguard-Team":    "alpha",
		"X-Costguard-Project": "proj-1",
		"X-Costguard-Agent":   "bot-a",
	})

	// The gateway passes 4xx upstream responses through unchanged to the caller.
	if status != http.StatusTooManyRequests {
		t.Fatalf("expected client to see 429, got %d", status)
	}
	if got := upstreamCalls.Load(); got != 1 {
		t.Fatalf("upstream calls: got %d, want 1", got)
	}

	records := store.all()
	if len(records) != 1 {
		t.Fatalf("expected 1 usage record, got %d", len(records))
	}
	rec := records[0]

	if rec.StatusCode != http.StatusTooManyRequests {
		t.Errorf("StatusCode: got %d, want %d", rec.StatusCode, http.StatusTooManyRequests)
	}
	if rec.EstimatedCostUSD != 0 {
		t.Errorf("EstimatedCostUSD: got %v, want 0 (must not pollute spend totals)", rec.EstimatedCostUSD)
	}
	if rec.PriceFound {
		t.Error("PriceFound: got true, want false")
	}
	if rec.CacheHit {
		t.Error("CacheHit: got true, want false")
	}
	if rec.Team != "alpha" {
		t.Errorf("Team: got %q, want alpha", rec.Team)
	}
	if rec.Project != "proj-1" {
		t.Errorf("Project: got %q, want proj-1", rec.Project)
	}
	if rec.Agent != "bot-a" {
		t.Errorf("Agent: got %q, want bot-a", rec.Agent)
	}
	if rec.PromptTokens != 0 || rec.CompletionTokens != 0 || rec.TotalTokens != 0 {
		t.Errorf("tokens: got prompt=%d completion=%d total=%d, want all 0",
			rec.PromptTokens, rec.CompletionTokens, rec.TotalTokens)
	}
}

// TestMeter_5xxUpstream verifies that a 5xx upstream response (converted to a
// Go error in callSingleProvider so that retry/fallback/breaker can act on it)
// produces exactly one usage record with the real upstream status code and zero
// cost.
func TestMeter_5xxUpstream(t *testing.T) {
	upstream, upstreamCalls := fakeErrorUpstream(t, "/v1/chat/completions", http.StatusServiceUnavailable)
	defer upstream.Close()

	reg, provider := newErrorReg(t, "openai-5xx-test", upstream.URL)
	store := &fakeStore{}
	h := newHarness(reg, store)
	defer h.server.Close()

	status, _ := h.rawPost(t, provider, simpleRequest("gpt-4o-mini"))

	// The gateway wraps 5xx upstream errors as 502 Bad Gateway to the client.
	if status != http.StatusBadGateway {
		t.Fatalf("expected client to see 502, got %d", status)
	}
	if got := upstreamCalls.Load(); got != 1 {
		t.Fatalf("upstream calls: got %d, want 1", got)
	}

	records := store.all()
	if len(records) != 1 {
		t.Fatalf("expected 1 usage record, got %d", len(records))
	}
	rec := records[0]

	if rec.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("StatusCode: got %d, want %d (real upstream status, not gateway 502)",
			rec.StatusCode, http.StatusServiceUnavailable)
	}
	if rec.EstimatedCostUSD != 0 {
		t.Errorf("EstimatedCostUSD: got %v, want 0", rec.EstimatedCostUSD)
	}
	if rec.PriceFound {
		t.Error("PriceFound: got true, want false")
	}
	if rec.PromptTokens != 0 || rec.CompletionTokens != 0 || rec.TotalTokens != 0 {
		t.Errorf("tokens: got prompt=%d completion=%d total=%d, want all 0",
			rec.PromptTokens, rec.CompletionTokens, rec.TotalTokens)
	}
}

// TestMeter_4xx_NotCached verifies that 4xx upstream responses are never stored
// in the response cache. A second identical request must reach upstream again
// rather than being served from cache.
func TestMeter_4xx_NotCached(t *testing.T) {
	upstream, upstreamCalls := fakeErrorUpstream(t, "/v1/chat/completions", http.StatusBadRequest)
	defer upstream.Close()

	reg, provider := newErrorReg(t, "openai-4xx-nocache", upstream.URL)
	store := &fakeStore{}
	// newHarness configures cache capacity=1000, TTL=1m — caching is active for
	// successful responses, so this test is meaningful.
	h := newHarness(reg, store)
	defer h.server.Close()

	payload := simpleRequest("gpt-4o-mini")

	// Two identical requests. If the 4xx were incorrectly cached, the second
	// would not reach upstream and we'd see only 1 upstream call.
	h.rawPost(t, provider, payload)
	h.rawPost(t, provider, payload)

	if got := upstreamCalls.Load(); got != 2 {
		t.Errorf("upstream calls: got %d, want 2 (4xx responses must not be cached)", got)
	}
	records := store.all()
	if len(records) != 2 {
		t.Errorf("expected 2 usage records (one per request), got %d", len(records))
	}
}
