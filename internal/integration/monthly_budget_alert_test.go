package integration_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/marcoantonios1/costguard/internal/budget"
	"github.com/marcoantonios1/costguard/internal/cache"
	"github.com/marcoantonios1/costguard/internal/gateway"
	"github.com/marcoantonios1/costguard/internal/providers"
	openai_provider "github.com/marcoantonios1/costguard/internal/providers/openai"
	openai_http "github.com/marcoantonios1/costguard/internal/server/openai"
)

// ---------------------------------------------------------------------------
// fixedSpendReader — returns a preset total spend so tests can place the
// budget at any threshold without needing real requests to accumulate cost.
// ---------------------------------------------------------------------------

type fixedSpendReader struct {
	mu    sync.Mutex
	spend float64
}

func (r *fixedSpendReader) set(v float64) {
	r.mu.Lock()
	r.spend = v
	r.mu.Unlock()
}

func (r *fixedSpendReader) GetTotalSpend(_ context.Context, _, _ time.Time) (float64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.spend, nil
}
func (r *fixedSpendReader) GetSpendForTeam(_ context.Context, _ string, _, _ time.Time) (float64, error) {
	return 0, nil
}
func (r *fixedSpendReader) GetSpendForProject(_ context.Context, _ string, _, _ time.Time) (float64, error) {
	return 0, nil
}
func (r *fixedSpendReader) GetSpendForAgent(_ context.Context, _ string, _, _ time.Time) (float64, error) {
	return 0, nil
}

// ---------------------------------------------------------------------------
// perThresholdAlertStore — tracks MarkSent calls per threshold so tests can
// assert exactly which thresholds fired and how many times.
// ---------------------------------------------------------------------------

type perThresholdAlertStore struct {
	mu    sync.Mutex
	calls map[int]int // thresholdPercent → MarkSent call count
}

func (s *perThresholdAlertStore) WasSent(_ context.Context, _ time.Time, pct int, _ string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[pct] > 0, nil
}

func (s *perThresholdAlertStore) MarkSent(_ context.Context, _ time.Time, pct int, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls == nil {
		s.calls = make(map[int]int)
	}
	s.calls[pct]++
	return nil
}

func (s *perThresholdAlertStore) countFor(pct int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[pct]
}

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

// newMonthlyAlertHarness builds a gateway + test server backed by a
// fixedSpendReader (for deterministic threshold control) and a
// perThresholdAlertStore. monthlyUSD is the budget ceiling.
func newMonthlyAlertHarness(
	reg *providers.Registry,
	spendReader *fixedSpendReader,
	monthlyUSD float64,
	alerts *perThresholdAlertStore,
) *httptest.Server {
	budgetSvc := budget.NewService(spendReader, budget.Config{
		Enabled:    true,
		MonthlyUSD: monthlyUSD,
	})

	gw, err := gateway.New(gateway.Deps{
		Router:        &staticRouter{},
		Registry:      reg,
		Cache:         cache.NewMemory(0),
		BudgetChecker: budgetSvc,
		AlertStore:    alerts,
	})
	if err != nil {
		panic("gateway.New: " + err.Error())
	}

	mux := http.NewServeMux()
	openai_http.Register(mux, openai_http.Deps{Gateway: gw})
	return httptest.NewServer(mux)
}

// newOpenAIReg registers a fresh OpenAI stub client against the given upstream
// URL and returns the registry + provider name.
func newOpenAIReg(t *testing.T, upstreamURL string) (*providers.Registry, string) {
	t.Helper()
	client, err := openai_provider.NewClient(openai_provider.ClientConfig{
		Name: "openai-alert-test", BaseURL: upstreamURL, APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	reg := providers.NewRegistry()
	reg.Register("openai-alert-test", client)
	return reg, "openai-alert-test"
}

// makeRequest fires a single chat-completions request and returns the HTTP
// status code.
func makeRequest(t *testing.T, srv *httptest.Server, providerName string) int {
	t.Helper()
	status, _ := postToServer(t, srv, providerName, simpleRequest("gpt-4o-mini"))
	return status
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestMonthlyBudget_EightyPercentAlert verifies that when total spend is
// between 80% and 89% of the budget, exactly the 80% alert fires and the 90%
// alert does not.
func TestMonthlyBudget_EightyPercentAlert(t *testing.T) {
	upstream, _ := fakeUpstream(t, "/v1/chat/completions", openAITextResponse(), openAITextResponse())
	defer upstream.Close()

	reg, provider := newOpenAIReg(t, upstream.URL)
	spendReader := &fixedSpendReader{}
	alerts := &perThresholdAlertStore{}

	const monthlyUSD = 10.0
	srv := newMonthlyAlertHarness(reg, spendReader, monthlyUSD, alerts)
	defer srv.Close()

	// Set spend at 85% — above 80%, below 90%.
	spendReader.set(monthlyUSD * 0.85)

	status := makeRequest(t, srv, provider)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if alerts.countFor(80) != 1 {
		t.Errorf("80%% alert MarkSent count: got %d, want 1", alerts.countFor(80))
	}
	if alerts.countFor(90) != 0 {
		t.Errorf("90%% alert MarkSent count: got %d, want 0 (spend is only 85%%)", alerts.countFor(90))
	}
}

// TestMonthlyBudget_NinetyPercentAlert_EmitsBothAlerts verifies that when
// total spend is at or above 90%, both the 80% and 90% alerts fire — even if
// spend jumped straight past 80% in a single step. CheckMonthlyBudget returns
// only the highest-crossed sentinel, so the gateway must cascade downward.
func TestMonthlyBudget_NinetyPercentAlert_EmitsBothAlerts(t *testing.T) {
	upstream, _ := fakeUpstream(t, "/v1/chat/completions", openAITextResponse(), openAITextResponse())
	defer upstream.Close()

	reg, provider := newOpenAIReg(t, upstream.URL)
	spendReader := &fixedSpendReader{}
	alerts := &perThresholdAlertStore{}

	const monthlyUSD = 10.0
	srv := newMonthlyAlertHarness(reg, spendReader, monthlyUSD, alerts)
	defer srv.Close()

	// Set spend at 95% — above 90%, below 100%.
	spendReader.set(monthlyUSD * 0.95)

	status := makeRequest(t, srv, provider)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if alerts.countFor(80) != 1 {
		t.Errorf("80%% alert MarkSent count: got %d, want 1 (90%% crossing implies 80%% was also crossed)", alerts.countFor(80))
	}
	if alerts.countFor(90) != 1 {
		t.Errorf("90%% alert MarkSent count: got %d, want 1", alerts.countFor(90))
	}
}

// TestMonthlyBudget_NoAlertBelow80Percent verifies that when spend is below
// 80%, no monthly threshold alerts fire at all.
func TestMonthlyBudget_NoAlertBelow80Percent(t *testing.T) {
	upstream, _ := fakeUpstream(t, "/v1/chat/completions", openAITextResponse(), openAITextResponse())
	defer upstream.Close()

	reg, provider := newOpenAIReg(t, upstream.URL)
	spendReader := &fixedSpendReader{}
	alerts := &perThresholdAlertStore{}

	const monthlyUSD = 10.0
	srv := newMonthlyAlertHarness(reg, spendReader, monthlyUSD, alerts)
	defer srv.Close()

	spendReader.set(monthlyUSD * 0.70) // 70% — below both thresholds

	status := makeRequest(t, srv, provider)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if alerts.countFor(80) != 0 {
		t.Errorf("80%% alert should not fire at 70%% spend, but MarkSent was called %d time(s)", alerts.countFor(80))
	}
	if alerts.countFor(90) != 0 {
		t.Errorf("90%% alert should not fire at 70%% spend, but MarkSent was called %d time(s)", alerts.countFor(90))
	}
}

// TestMonthlyBudget_AlertDedup_SamePeriod verifies that making multiple
// requests while spend stays at the same threshold fires each alert exactly
// once — the alertStore dedup prevents re-emission.
func TestMonthlyBudget_AlertDedup_SamePeriod(t *testing.T) {
	// Four responses queued so three requests succeed (fakeUpstream cycles through them).
	upstream, _ := fakeUpstream(t, "/v1/chat/completions",
		openAITextResponse(), openAITextResponse())
	defer upstream.Close()

	reg, provider := newOpenAIReg(t, upstream.URL)
	spendReader := &fixedSpendReader{}
	alerts := &perThresholdAlertStore{}

	const monthlyUSD = 10.0
	srv := newMonthlyAlertHarness(reg, spendReader, monthlyUSD, alerts)
	defer srv.Close()

	spendReader.set(monthlyUSD * 0.85) // fixed at 85% across all requests

	for i := 0; i < 3; i++ {
		status := makeRequest(t, srv, provider)
		if status != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, status)
		}
	}

	// alertStore.WasSent returns true after the first MarkSent, so subsequent
	// requests skip emission. Total MarkSent calls must be exactly 1.
	if got := alerts.countFor(80); got != 1 {
		t.Errorf("80%% alert MarkSent count across 3 requests: got %d, want exactly 1", got)
	}
	if got := alerts.countFor(90); got != 0 {
		t.Errorf("90%% alert should not fire at 85%% spend; got %d MarkSent call(s)", got)
	}
}

// TestMonthlyBudget_JumpStraightToNinety_BothAlertsFire explicitly models
// the case where spend starts under 80% and crosses 90% in a single step
// (e.g. one large expensive request). Both alerts must fire on that crossing
// request because CheckMonthlyBudget returns only ErrMonthlyBudgetReachedNinetyPercent,
// so the gateway must also emit the implied 80% alert.
func TestMonthlyBudget_JumpStraightToNinety_BothAlertsFire(t *testing.T) {
	upstream, _ := fakeUpstream(t, "/v1/chat/completions", openAITextResponse(), openAITextResponse())
	defer upstream.Close()

	reg, provider := newOpenAIReg(t, upstream.URL)
	spendReader := &fixedSpendReader{}
	alerts := &perThresholdAlertStore{}

	const monthlyUSD = 10.0
	srv := newMonthlyAlertHarness(reg, spendReader, monthlyUSD, alerts)
	defer srv.Close()

	// First request: spend well below 80% — no alerts.
	spendReader.set(monthlyUSD * 0.50)
	if status := makeRequest(t, srv, provider); status != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", status)
	}
	if alerts.countFor(80) != 0 || alerts.countFor(90) != 0 {
		t.Fatalf("no alerts expected at 50%% spend")
	}

	// Simulate a single large request that jumps spend from 50% to 91% — the
	// budget now reports ≥90% before the second request is processed.
	spendReader.set(monthlyUSD * 0.91)

	if status := makeRequest(t, srv, provider); status != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d", status)
	}

	if alerts.countFor(80) != 1 {
		t.Errorf("80%% alert count after jump to 91%%: got %d, want 1", alerts.countFor(80))
	}
	if alerts.countFor(90) != 1 {
		t.Errorf("90%% alert count after jump to 91%%: got %d, want 1", alerts.countFor(90))
	}
}
