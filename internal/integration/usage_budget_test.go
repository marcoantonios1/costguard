package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/marcoantonios1/costguard/internal/cache"
	"github.com/marcoantonios1/costguard/internal/gateway"
	"github.com/marcoantonios1/costguard/internal/providers"
	anthropic_provider "github.com/marcoantonios1/costguard/internal/providers/anthropic"
	openai_provider "github.com/marcoantonios1/costguard/internal/providers/openai"
	admin_server "github.com/marcoantonios1/costguard/internal/server/admin"
	openai_http "github.com/marcoantonios1/costguard/internal/server/openai"
	"github.com/marcoantonios1/costguard/internal/usage"
)

// ---------------------------------------------------------------------------
// trackingStore — in-memory usage.Store with real aggregation
// ---------------------------------------------------------------------------

// trackingStore is an in-memory Store that properly aggregates spend so both
// the budget checker and the admin summary/teams endpoints work correctly in
// integration tests.
type trackingStore struct {
	mu      sync.Mutex
	records []usage.Record
}

func (s *trackingStore) Save(_ context.Context, r usage.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, r)
	return nil
}

func (s *trackingStore) all() []usage.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]usage.Record, len(s.records))
	copy(cp, s.records)
	return cp
}

func (s *trackingStore) GetTotalSpend(_ context.Context, from, to time.Time) (float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var sum float64
	for _, r := range s.records {
		if !r.Timestamp.Before(from) && r.Timestamp.Before(to) {
			sum += r.EstimatedCostUSD
		}
	}
	return sum, nil
}

func (s *trackingStore) GetSpendByTeam(_ context.Context, from, to time.Time) ([]usage.TeamSpend, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	byTeam := make(map[string]float64)
	for _, r := range s.records {
		if r.Team != "" && !r.Timestamp.Before(from) && r.Timestamp.Before(to) {
			byTeam[r.Team] += r.EstimatedCostUSD
		}
	}
	out := make([]usage.TeamSpend, 0, len(byTeam))
	for team, spend := range byTeam {
		out = append(out, usage.TeamSpend{Team: team, Spend: spend})
	}
	return out, nil
}

func (s *trackingStore) GetSpendForTeam(_ context.Context, team string, from, to time.Time) (float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var sum float64
	for _, r := range s.records {
		if r.Team == team && !r.Timestamp.Before(from) && r.Timestamp.Before(to) {
			sum += r.EstimatedCostUSD
		}
	}
	return sum, nil
}

func (s *trackingStore) GetSpendByProvider(_ context.Context, _, _ time.Time) ([]usage.ProviderSpend, error) {
	return nil, nil
}
func (s *trackingStore) GetSpendByModel(_ context.Context, _, _ time.Time) ([]usage.ModelSpend, error) {
	return nil, nil
}
func (s *trackingStore) GetSpendByProject(_ context.Context, _, _ time.Time) ([]usage.ProjectSpend, error) {
	return nil, nil
}
func (s *trackingStore) GetSpendForProject(_ context.Context, _ string, _, _ time.Time) (float64, error) {
	return 0, nil
}
func (s *trackingStore) GetSpendForAgent(_ context.Context, _ string, _, _ time.Time) (float64, error) {
	return 0, nil
}
func (s *trackingStore) GetSpendByAgent(_ context.Context, _, _ time.Time) ([]usage.AgentSpend, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// Harness helpers
// ---------------------------------------------------------------------------

// newTrackingHarness wires a gateway with the given trackingStore and no
// caching so every request reaches the upstream and produces a fresh record.
func newTrackingHarness(reg *providers.Registry, store *trackingStore) *httptest.Server {
	gw, err := gateway.New(gateway.Deps{
		Router:     &staticRouter{},
		Registry:   reg,
		Cache:      cache.NewMemory(0),
		UsageStore: store,
	})
	if err != nil {
		panic("gateway.New: " + err.Error())
	}
	mux := http.NewServeMux()
	openai_http.Register(mux, openai_http.Deps{Gateway: gw})
	return httptest.NewServer(mux)
}

// newAdminSrv starts a test admin server backed by the given store.
func newAdminSrv(t *testing.T, store usage.Store) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	admin_server.Register(mux, admin_server.Deps{UsageStore: store})
	return httptest.NewServer(mux)
}

// postWithHeaders sends a POST to gatewaySrv/v1/chat/completions and returns
// (statusCode, body). Extra headers (e.g. X-Costguard-Team) are applied from
// the headers map.
func postWithHeaders(t *testing.T, gatewaySrv *httptest.Server, providerName string, body any, headers map[string]string) (int, []byte) {
	t.Helper()
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, gatewaySrv.URL+"/v1/chat/completions", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(gateway.HeaderProviderHint, providerName)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// wideRange returns RFC3339 from/to strings spanning ±1 hour around now.
func wideRange() (string, string) {
	now := time.Now().UTC()
	return now.Add(-time.Hour).Format(time.RFC3339),
		now.Add(time.Hour).Format(time.RFC3339)
}

// simpleRequest builds a minimal non-streaming chat request body.
func simpleRequest(model string) map[string]any {
	return map[string]any{
		"model":    model,
		"messages": []any{map[string]any{"role": "user", "content": "Hello"}},
	}
}

// adminGet performs a GET against the admin server and returns (statusCode, body).
func adminGet(t *testing.T, adminSrv *httptest.Server, path string, params url.Values) (int, []byte) {
	t.Helper()
	u := fmt.Sprintf("%s%s?%s", adminSrv.URL, path, params.Encode())
	resp, err := http.Get(u)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestUsage_RecordSavedAfterRequest verifies that a successful non-streaming
// request through the gateway produces exactly one usage.Record with the
// correct provider, model, token counts, and metadata fields.
func TestUsage_RecordSavedAfterRequest(t *testing.T) {
	upstream, _ := fakeUpstream(t, "/v1/chat/completions", openAITextResponse(), openAITextResponse())
	defer upstream.Close()

	client, _ := openai_provider.NewClient(openai_provider.ClientConfig{
		Name: "openai-record-test", BaseURL: upstream.URL, APIKey: "test-key",
	})
	reg := providers.NewRegistry()
	reg.Register("openai-record-test", client)

	store := &fakeStore{}
	h := newHarness(reg, store)
	defer h.server.Close()

	status, body := h.rawPost(t, "openai-record-test", simpleRequest("gpt-4o-mini"))
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}

	records := store.all()
	if len(records) != 1 {
		t.Fatalf("expected 1 usage record, got %d", len(records))
	}
	rec := records[0]

	if rec.Provider != "openai-record-test" {
		t.Errorf("Provider: got %q, want openai-record-test", rec.Provider)
	}
	if rec.Model != "gpt-4o-mini" {
		t.Errorf("Model: got %q, want gpt-4o-mini", rec.Model)
	}
	// openAITextResponse reports prompt=150, completion=15, total=165.
	if rec.PromptTokens != 150 {
		t.Errorf("PromptTokens: got %d, want 150", rec.PromptTokens)
	}
	if rec.CompletionTokens != 15 {
		t.Errorf("CompletionTokens: got %d, want 15", rec.CompletionTokens)
	}
	if rec.TotalTokens != 165 {
		t.Errorf("TotalTokens: got %d, want 165", rec.TotalTokens)
	}
	if rec.CacheHit {
		t.Error("CacheHit: got true, want false")
	}
	if rec.StatusCode != http.StatusOK {
		t.Errorf("StatusCode: got %d, want 200", rec.StatusCode)
	}
	if rec.Path != "/v1/chat/completions" {
		t.Errorf("Path: got %q, want /v1/chat/completions", rec.Path)
	}
	if rec.Timestamp.IsZero() {
		t.Error("Timestamp must not be zero")
	}
}

// TestUsage_CacheHitRecordSaved verifies that a repeated identical request
// produces a second metering record with CacheHit=true and zero token counts,
// while the upstream is called exactly once.
func TestUsage_CacheHitRecordSaved(t *testing.T) {
	upstream, calls := fakeUpstream(t, "/v1/chat/completions", openAITextResponse(), openAITextResponse())
	defer upstream.Close()

	client, _ := openai_provider.NewClient(openai_provider.ClientConfig{
		Name: "openai-cache-hit", BaseURL: upstream.URL, APIKey: "test-key",
	})
	reg := providers.NewRegistry()
	reg.Register("openai-cache-hit", client)

	store := &fakeStore{}
	h := newHarness(reg, store) // cache capacity 1000 — caching is active
	defer h.server.Close()

	payload := simpleRequest("gpt-4o-mini")

	// First request hits the upstream.
	status1, body1 := h.rawPost(t, "openai-cache-hit", payload)
	if status1 != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d: %s", status1, body1)
	}

	// Second identical request must be served from the cache.
	status2, body2 := h.rawPost(t, "openai-cache-hit", payload)
	if status2 != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d: %s", status2, body2)
	}

	if got := calls.Load(); got != 1 {
		t.Errorf("upstream calls: got %d, want 1 (second request should be a cache hit)", got)
	}

	records := store.all()
	if len(records) != 2 {
		t.Fatalf("expected 2 usage records, got %d", len(records))
	}

	first, second := records[0], records[1]

	if first.CacheHit {
		t.Error("records[0].CacheHit: got true, want false (first request is a real call)")
	}
	if first.PromptTokens != 150 {
		t.Errorf("records[0].PromptTokens: got %d, want 150", first.PromptTokens)
	}

	if !second.CacheHit {
		t.Error("records[1].CacheHit: got false, want true (second request hits cache)")
	}
	if second.PromptTokens != 0 {
		t.Errorf("records[1].PromptTokens: got %d, want 0 (cache hit carries no token counts)", second.PromptTokens)
	}
	if second.CompletionTokens != 0 {
		t.Errorf("records[1].CompletionTokens: got %d, want 0", second.CompletionTokens)
	}
}

// TestUsageSummary_CorrectTotal verifies that GET /usage/summary returns a
// total_spend_usd that exactly equals the sum of EstimatedCostUSD across all
// records saved to the store within the queried time window.
func TestUsageSummary_CorrectTotal(t *testing.T) {
	// Two distinct requests so both reach the upstream (no caching in this harness).
	req1 := map[string]any{
		"model":    "gpt-4o-mini",
		"messages": []any{map[string]any{"role": "user", "content": "First question"}},
	}
	req2 := map[string]any{
		"model":    "gpt-4o-mini",
		"messages": []any{map[string]any{"role": "user", "content": "Second question"}},
	}

	upstream, _ := fakeUpstream(t, "/v1/chat/completions", openAITextResponse(), openAITextResponse())
	defer upstream.Close()

	// "openai-summary" starts with "openai" so the pricing engine can look up a
	// real rate, giving EstimatedCostUSD > 0 if the table has gpt-4o-mini.
	client, _ := openai_provider.NewClient(openai_provider.ClientConfig{
		Name: "openai-summary", BaseURL: upstream.URL, APIKey: "test-key",
	})
	reg := providers.NewRegistry()
	reg.Register("openai-summary", client)

	store := &trackingStore{}
	gatewaySrv := newTrackingHarness(reg, store)
	defer gatewaySrv.Close()
	adminSrv := newAdminSrv(t, store)
	defer adminSrv.Close()

	for _, payload := range []map[string]any{req1, req2} {
		status, body := postWithHeaders(t, gatewaySrv, "openai-summary", payload, nil)
		if status != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", status, body)
		}
	}

	records := store.all()
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	wantTotal := records[0].EstimatedCostUSD + records[1].EstimatedCostUSD

	from, to := wideRange()
	params := url.Values{"from": {from}, "to": {to}}
	statusCode, body := adminGet(t, adminSrv, "/usage/summary", params)
	if statusCode != http.StatusOK {
		t.Fatalf("/usage/summary: expected 200, got %d: %s", statusCode, body)
	}

	var summary usage.Summary
	if err := json.Unmarshal(body, &summary); err != nil {
		t.Fatalf("decode summary: %v\nbody: %s", err, body)
	}
	if summary.TotalSpendUSD != wantTotal {
		t.Errorf("TotalSpendUSD: got %g, want %g (sum of 2 records)", summary.TotalSpendUSD, wantTotal)
	}
}

// TestTeamSpend_CorrectGrouping verifies that GET /usage/teams returns a
// separate entry for each team, with each entry's spend_usd equal to the sum
// of EstimatedCostUSD for that team's records in the queried window.
func TestTeamSpend_CorrectGrouping(t *testing.T) {
	upstream, _ := fakeUpstream(t, "/v1/chat/completions", openAITextResponse(), openAITextResponse())
	defer upstream.Close()

	client, _ := openai_provider.NewClient(openai_provider.ClientConfig{
		Name: "openai-teams", BaseURL: upstream.URL, APIKey: "test-key",
	})
	reg := providers.NewRegistry()
	reg.Register("openai-teams", client)

	store := &trackingStore{}
	gatewaySrv := newTrackingHarness(reg, store)
	defer gatewaySrv.Close()
	adminSrv := newAdminSrv(t, store)
	defer adminSrv.Close()

	payload := simpleRequest("gpt-4o-mini")

	// Two requests from "alpha", one from "beta" — all with the same body.
	// Caching is disabled in newTrackingHarness so all three reach the upstream.
	for i := 0; i < 2; i++ {
		status, body := postWithHeaders(t, gatewaySrv, "openai-teams", payload,
			map[string]string{"X-Costguard-Team": "alpha"})
		if status != http.StatusOK {
			t.Fatalf("alpha request %d: expected 200, got %d: %s", i+1, status, body)
		}
	}
	if status, body := postWithHeaders(t, gatewaySrv, "openai-teams", payload,
		map[string]string{"X-Costguard-Team": "beta"}); status != http.StatusOK {
		t.Fatalf("beta request: expected 200, got %d: %s", status, body)
	}

	records := store.all()
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}
	wantAlpha := records[0].EstimatedCostUSD + records[1].EstimatedCostUSD
	wantBeta := records[2].EstimatedCostUSD

	from, to := wideRange()
	params := url.Values{"from": {from}, "to": {to}}
	statusCode, body := adminGet(t, adminSrv, "/usage/teams", params)
	if statusCode != http.StatusOK {
		t.Fatalf("/usage/teams: expected 200, got %d: %s", statusCode, body)
	}

	var result struct {
		Teams []usage.TeamSpend `json:"teams"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode teams response: %v\nbody: %s", err, body)
	}
	if len(result.Teams) != 2 {
		t.Fatalf("expected 2 team entries, got %d: %+v", len(result.Teams), result.Teams)
	}

	// Sort alphabetically for deterministic comparison (map iteration is random).
	sort.Slice(result.Teams, func(i, j int) bool {
		return result.Teams[i].Team < result.Teams[j].Team
	})

	alpha, beta := result.Teams[0], result.Teams[1]

	if alpha.Team != "alpha" {
		t.Errorf("teams[0].team: got %q, want alpha", alpha.Team)
	}
	if alpha.Spend != wantAlpha {
		t.Errorf("teams[0].spend_usd: got %g, want %g", alpha.Spend, wantAlpha)
	}
	if beta.Team != "beta" {
		t.Errorf("teams[1].team: got %q, want beta", beta.Team)
	}
	if beta.Spend != wantBeta {
		t.Errorf("teams[1].spend_usd: got %g, want %g", beta.Spend, wantBeta)
	}
}

// TestBudget_Rejection_Returns402 verifies that once the global monthly budget
// is exhausted the gateway returns 402 Payment Required with a JSON error body.
// A vision request is used because the Anthropic tile formula guarantees a
// non-zero EstimatedCostUSD even when the upstream reports zero tokens, which
// ensures the budget is provably exceeded after the first request.
func TestBudget_Rejection_Returns402(t *testing.T) {
	upstream, _ := captureUpstream(t, "/v1/messages", anthropicZeroUsageResponse())
	defer upstream.Close()

	// Name starts with "anthropic" so NormalizeProvider → "anthropic",
	// enabling a real price lookup and non-zero EstimatedCostUSD.
	client, _ := anthropic_provider.NewClient(anthropic_provider.ClientConfig{
		Name: "anthropic-bgt402", BaseURL: upstream.URL,
		APIKey: "test-key", AnthropicVersion: "2023-06-01",
	})
	reg := providers.NewRegistry()
	reg.Register("anthropic-bgt402", client)

	// Budget is smaller than the cost of a single 1024×1024 vision request:
	// 3125 prompt tokens × ($0.75/1M) ≈ $0.0000023 > $0.000001.
	srv, store := newHarnessWithBudget(reg, 0.000001)
	defer srv.Close()

	visionPayload := imageOnlyPayload("claude-sonnet-4-5-20250929", "https://example.com/photo.png")

	// First request: budget not yet exceeded — must succeed.
	status1, body1 := postToServer(t, srv, "anthropic-bgt402", visionPayload)
	if status1 != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d: %s", status1, body1)
	}
	if store.totalCost() <= 0 {
		t.Fatal("expected non-zero cost after first vision request")
	}

	// Second request: cumulative spend now exceeds the budget → 402.
	status2, body2 := postToServer(t, srv, "anthropic-bgt402", visionPayload)
	if status2 != http.StatusPaymentRequired {
		t.Errorf("second request: expected 402, got %d\nbody: %s", status2, body2)
	}

	// The 402 response must carry a structured JSON error body.
	var errResp struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body2, &errResp); err != nil {
		t.Fatalf("402 body is not valid JSON: %v\nbody: %s", err, body2)
	}
	if errResp.Error.Message == "" {
		t.Error("402 error.message must not be empty")
	}
	if errResp.Error.Type == "" {
		t.Error("402 error.type must not be empty")
	}
}
