package feedback_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcoantonios1/costguard/internal/feedback"
	"github.com/marcoantonios1/costguard/internal/server"
)

const testAPIKey = "test-admin-key"

func newTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "feedback-*.jsonl")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	path := f.Name()
	f.Close()

	store, err := feedback.NewJSONLStore(path)
	if err != nil {
		t.Fatalf("NewJSONLStore: %v", err)
	}

	h := feedback.NewHandler(store, nil)

	mux := http.NewServeMux()
	mux.Handle("/v1/feedback", server.AdminAuth(testAPIKey)(h))

	return httptest.NewServer(mux), path
}

func newStatsTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "feedback-*.jsonl")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	path := f.Name()
	f.Close()

	reader := feedback.NewReader(path)
	h := feedback.NewStatsHandler(reader)

	mux := http.NewServeMux()
	mux.Handle("/v1/feedback/stats", server.AdminAuth(testAPIKey)(h))

	return httptest.NewServer(mux), path
}

func seedRecords(t *testing.T, path string, records ...feedback.FeedbackRecord) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open temp file: %v", err)
	}
	defer f.Close()
	for _, r := range records {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshal record: %v", err)
		}
		if _, err := fmt.Fprintf(f, "%s\n", b); err != nil {
			t.Fatalf("write record: %v", err)
		}
	}
}

func getStats(t *testing.T, ts *httptest.Server, query string) (int, feedback.ModelStats) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/feedback/stats"+query, nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var stats feedback.ModelStats
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return resp.StatusCode, stats
}

func TestStats_AllRecords(t *testing.T) {
	ts, path := newStatsTestServer(t)
	defer ts.Close()

	seedRecords(t, path,
		feedback.FeedbackRecord{Consumer: "forge", Role: "coder", Model: "qwen3-coder:30b", Outcome: "success"},
		feedback.FeedbackRecord{Consumer: "forge", Role: "coder", Model: "qwen3-coder:30b", Outcome: "failure"},
		feedback.FeedbackRecord{Consumer: "other", Role: "reviewer", Model: "claude-sonnet-4-6", Outcome: "success"},
	)

	status, stats := getStats(t, ts, "")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	if stats.TaskCount != 3 {
		t.Fatalf("expected TaskCount 3, got %d", stats.TaskCount)
	}
}

func TestStats_FilterByModel(t *testing.T) {
	ts, path := newStatsTestServer(t)
	defer ts.Close()

	seedRecords(t, path,
		feedback.FeedbackRecord{Consumer: "forge", Role: "coder", Model: "qwen3-coder:30b", Outcome: "success"},
		feedback.FeedbackRecord{Consumer: "forge", Role: "coder", Model: "qwen3-coder:30b", Outcome: "failure"},
		feedback.FeedbackRecord{Consumer: "other", Role: "reviewer", Model: "claude-sonnet-4-6", Outcome: "success"},
	)

	status, stats := getStats(t, ts, "?model=qwen3-coder:30b")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	if stats.TaskCount != 2 {
		t.Fatalf("expected TaskCount 2, got %d", stats.TaskCount)
	}
	if stats.Model != "qwen3-coder:30b" {
		t.Errorf("expected model echoed back, got %q", stats.Model)
	}
}

func TestStats_FilterByRoleConsumerCombined(t *testing.T) {
	ts, path := newStatsTestServer(t)
	defer ts.Close()

	seedRecords(t, path,
		feedback.FeedbackRecord{Consumer: "forge", Role: "coder", Model: "qwen3-coder:30b", Outcome: "success"},
		feedback.FeedbackRecord{Consumer: "forge", Role: "reviewer", Model: "qwen3-coder:30b", Outcome: "success"},
		feedback.FeedbackRecord{Consumer: "other", Role: "coder", Model: "qwen3-coder:30b", Outcome: "success"},
	)

	status, stats := getStats(t, ts, "?consumer=forge&role=coder&model=qwen3-coder:30b")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	if stats.TaskCount != 1 {
		t.Fatalf("expected TaskCount 1 (AND of all filters), got %d", stats.TaskCount)
	}
}

func TestStats_LastN(t *testing.T) {
	ts, path := newStatsTestServer(t)
	defer ts.Close()

	seedRecords(t, path,
		feedback.FeedbackRecord{Consumer: "forge", Outcome: "failure"},
		feedback.FeedbackRecord{Consumer: "forge", Outcome: "failure"},
		feedback.FeedbackRecord{Consumer: "forge", Outcome: "failure"},
		feedback.FeedbackRecord{Consumer: "forge", Outcome: "success"},
		feedback.FeedbackRecord{Consumer: "forge", Outcome: "success"},
	)

	status, stats := getStats(t, ts, "?last_n=2")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	if stats.TaskCount != 2 {
		t.Fatalf("expected TaskCount 2, got %d", stats.TaskCount)
	}
	if stats.SuccessRate != 1.0 {
		t.Errorf("expected SuccessRate 1.0 over the last 2 (both success), got %v", stats.SuccessRate)
	}
}

func TestStats_EmptyFile(t *testing.T) {
	ts, _ := newStatsTestServer(t)
	defer ts.Close()

	status, stats := getStats(t, ts, "")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	if stats.TaskCount != 0 {
		t.Fatalf("expected TaskCount 0, got %d", stats.TaskCount)
	}
	if stats.SuccessRate != 0 || stats.AvgDurationMS != 0 {
		t.Errorf("expected zero-valued stats, got %+v", stats)
	}
}

func TestStats_WrongMethod(t *testing.T) {
	ts, _ := newStatsTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/feedback/stats", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}

func TestStats_NegativeLastN(t *testing.T) {
	ts, _ := newStatsTestServer(t)
	defer ts.Close()

	status, _ := getStats(t, ts, "?last_n=-1")
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}

	status, _ = getStats(t, ts, "?last_n=abc")
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestStats_MissingAuth(t *testing.T) {
	ts, _ := newStatsTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/feedback/stats")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestStats_WrongToken(t *testing.T) {
	ts, _ := newStatsTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/feedback/stats", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func validBody(consumer string) []byte {
	b, _ := json.Marshal(map[string]any{
		"consumer": consumer,
		"role":     "agent",
		"model":    "claude-sonnet-4-6",
		"outcome":  "success",
	})
	return b
}

func TestFeedback_Accepted(t *testing.T) {
	ts, path := newTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/feedback", bytes.NewReader(validBody("svc-a")))
	req.Header.Set("Authorization", "Bearer "+testAPIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}

	// Give the fire-and-forget goroutine a moment to flush.
	waitForLines(t, path, 1)

	lines := readLines(t, path)
	var rec feedback.FeedbackRecord
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("invalid JSON line: %v", err)
	}
	if rec.Consumer != "svc-a" {
		t.Errorf("expected consumer svc-a, got %s", rec.Consumer)
	}
	if rec.ReceivedAt.IsZero() {
		t.Error("expected received_at to be set")
	}
}

func TestFeedback_ConcurrentWritesNoInterleaving(t *testing.T) {
	ts, path := newTestServer(t)
	defer ts.Close()

	const n = 10
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			consumer := fmt.Sprintf("consumer-%d", i)
			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/feedback", bytes.NewReader(validBody(consumer)))
			req.Header.Set("Authorization", "Bearer "+testAPIKey)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Errorf("request failed: %v", err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusAccepted {
				t.Errorf("expected 202, got %d", resp.StatusCode)
			}
		}(i)
	}
	wg.Wait()

	waitForLines(t, path, n)

	lines := readLines(t, path)
	if len(lines) != n {
		t.Fatalf("expected %d lines, got %d", n, len(lines))
	}

	seen := map[string]bool{}
	for _, line := range lines {
		var rec feedback.FeedbackRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line is not valid JSON (interleaved write?): %q err=%v", line, err)
		}
		if seen[rec.Consumer] {
			t.Errorf("duplicate consumer %s", rec.Consumer)
		}
		seen[rec.Consumer] = true
	}
	if len(seen) != n {
		t.Fatalf("expected %d distinct consumers, got %d", n, len(seen))
	}
}

func TestFeedback_MissingAuth(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/feedback", "application/json", bytes.NewReader(validBody("svc-a")))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestFeedback_WrongToken(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/feedback", bytes.NewReader(validBody("svc-a")))
	req.Header.Set("Authorization", "Bearer wrong-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestFeedback_MethodNotAllowed(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/feedback", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}

func TestFeedback_MissingConsumer(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	body, _ := json.Marshal(map[string]any{"outcome": "success"})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/feedback", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testAPIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", resp.StatusCode)
	}
}

func TestFeedback_InvalidOutcome(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	body, _ := json.Marshal(map[string]any{"consumer": "svc-a", "outcome": "maybe"})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/feedback", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testAPIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", resp.StatusCode)
	}
}

// TestFeedback_RouteNotRegisteredWhenDisabled mirrors internal/app/app.go's
// conditional registration: when Feedback.LogPath is empty, /v1/feedback is
// never wired into the mux, so unmatched requests hit the default 404.
func TestFeedback_RouteNotRegisteredWhenDisabled(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/feedback", "application/json", bytes.NewReader(validBody("svc-a")))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestFeedback_MalformedJSON(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/feedback", strings.NewReader("{not json"))
	req.Header.Set("Authorization", "Bearer "+testAPIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// waitForLines polls path until it contains at least n newline-terminated
// lines or the timeout elapses, to tolerate the handler's fire-and-forget write.
func waitForLines(t *testing.T, path string, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(readLines(t, path)) >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d lines in %s", n, path)
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	trimmed := strings.TrimRight(string(b), "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}
