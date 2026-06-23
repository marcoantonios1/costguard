package gateway

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcoantonios1/costguard/internal/providers"
	"github.com/marcoantonios1/costguard/internal/usage"
)

// ---------------------------------------------------------------------------
// stub usage.Store
// ---------------------------------------------------------------------------

type stubStore struct {
	mu      sync.Mutex
	records []usage.Record
}

func (s *stubStore) Save(_ context.Context, r usage.Record) error {
	s.mu.Lock()
	s.records = append(s.records, r)
	s.mu.Unlock()
	return nil
}

func (s *stubStore) last() (usage.Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.records) == 0 {
		return usage.Record{}, false
	}
	return s.records[len(s.records)-1], true
}

func (s *stubStore) GetTotalSpend(_ context.Context, _, _ time.Time) (float64, error) {
	return 0, nil
}
func (s *stubStore) GetSpendByTeam(_ context.Context, _, _ time.Time) ([]usage.TeamSpend, error) {
	return nil, nil
}
func (s *stubStore) GetSpendByProvider(_ context.Context, _, _ time.Time) ([]usage.ProviderSpend, error) {
	return nil, nil
}
func (s *stubStore) GetSpendByModel(_ context.Context, _, _ time.Time) ([]usage.ModelSpend, error) {
	return nil, nil
}
func (s *stubStore) GetSpendByProject(_ context.Context, _, _ time.Time) ([]usage.ProjectSpend, error) {
	return nil, nil
}
func (s *stubStore) GetSpendForTeam(_ context.Context, _ string, _, _ time.Time) (float64, error) {
	return 0, nil
}
func (s *stubStore) GetSpendForProject(_ context.Context, _ string, _, _ time.Time) (float64, error) {
	return 0, nil
}
func (s *stubStore) GetSpendForAgent(_ context.Context, _ string, _, _ time.Time) (float64, error) {
	return 0, nil
}
func (s *stubStore) GetSpendByAgent(_ context.Context, _, _ time.Time) ([]usage.AgentSpend, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func streamingGateway(store *stubStore) *Gateway {
	return &Gateway{
		reg:        providers.NewRegistry(),
		usageStore: store,
	}
}

func streamingRequest() *http.Request {
	u := &url.URL{Scheme: "http", Host: "costguard.internal", Path: "/v1/chat/completions"}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, u.String(), nil)
	req.Header.Set("X-Costguard-Team", "team-alpha")
	req.Header.Set("X-Costguard-Project", "proj-beta")
	req.Header.Set("X-Costguard-User", "user-gamma")
	req.Header.Set("X-Costguard-Agent", "agent-delta")
	return req
}

func sseStreamResp(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       sseBody(body),
	}
}

// drainAndWait drains the body (triggering onDone) then waits briefly for the
// goroutine spawned inside onDone to call Save.
func drainAndWait(t *testing.T, resp *http.Response) {
	t.Helper()
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatalf("drain: %v", err)
	}
	resp.Body.Close()
	// Give the metering goroutine time to complete.
	time.Sleep(50 * time.Millisecond)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestPassthroughStreaming_MetersAfterRequestInvalidated asserts that header
// and URL values captured at passthroughStreaming time survive even if the
// caller mutates the original *http.Request afterwards (simulating net/http
// recycling the request after ServeHTTP returns).
func TestPassthroughStreaming_MetersAfterRequestInvalidated(t *testing.T) {
	store := &stubStore{}
	g := streamingGateway(store)
	r := streamingRequest()

	input := sseChunk(roleLine) + sseChunk(finishLine) + "data: [DONE]\n\n"
	wrapped := g.passthroughStreaming(r, sseStreamResp(input), "openai", "gpt-4o", nil)

	// Simulate net/http invalidating the request after ServeHTTP exits.
	r.Header = nil
	r.URL = &url.URL{}

	drainAndWait(t, wrapped)

	rec, ok := store.last()
	if !ok {
		t.Fatal("no usage record saved")
	}

	if rec.Team != "team-alpha" {
		t.Errorf("Team: got %q, want %q", rec.Team, "team-alpha")
	}
	if rec.Project != "proj-beta" {
		t.Errorf("Project: got %q, want %q", rec.Project, "proj-beta")
	}
	if rec.User != "user-gamma" {
		t.Errorf("User: got %q, want %q", rec.User, "user-gamma")
	}
	if rec.Agent != "agent-delta" {
		t.Errorf("Agent: got %q, want %q", rec.Agent, "agent-delta")
	}
	if rec.Path != "/v1/chat/completions" {
		t.Errorf("Path: got %q, want %q", rec.Path, "/v1/chat/completions")
	}
}

// TestStreaming_RaceClean drives passthroughStreaming with a concurrent
// mutation of the original *http.Request on a separate goroutine, asserting
// no data race and that the saved record reflects pre-mutation values.
func TestStreaming_RaceClean(t *testing.T) {
	store := &stubStore{}
	g := streamingGateway(store)
	r := streamingRequest()

	input := sseChunk(roleLine) + sseChunk(finishLine) + "data: [DONE]\n\n"
	wrapped := g.passthroughStreaming(r, sseStreamResp(input), "openai", "gpt-4o", nil)

	// Concurrently mutate the original request while the body is being drained.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.Header = http.Header{"X-Costguard-Team": []string{"mutated"}}
		r.URL = &url.URL{Path: "/mutated"}
	}()

	drainAndWait(t, wrapped)
	wg.Wait()

	rec, ok := store.last()
	if !ok {
		t.Fatal("no usage record saved")
	}
	// Regardless of when the mutation raced, the snapshotted values must win.
	if strings.Contains(rec.Team, "mutated") {
		t.Errorf("Team was mutated to %q — snapshot did not protect the value", rec.Team)
	}
	if strings.Contains(rec.Path, "mutated") {
		t.Errorf("Path was mutated to %q — snapshot did not protect the value", rec.Path)
	}
}
