package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type callPair struct {
	resp *http.Response
	err  error
}

// makeResponses returns a doCall function that cycles through the given responses/errors.
func makeResponses(pairs []callPair) func() (*http.Response, error) {
	i := 0
	return func() (*http.Response, error) {
		if i >= len(pairs) {
			return nil, errors.New("doCall called more times than expected")
		}
		p := pairs[i]
		i++
		return p.resp, p.err
	}
}

func resp200() *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody}
}

func resp429(retryAfter string) *http.Response {
	h := make(http.Header)
	if retryAfter != "" {
		h.Set("Retry-After", retryAfter)
	}
	return &http.Response{StatusCode: http.StatusTooManyRequests, Header: h, Body: http.NoBody}
}

func err5xx() error { return errors.New("upstream_5xx status=503 provider=test") }
func errTimeout() error {
	return context.DeadlineExceeded
}

// TestIsTimeoutError verifies detection of timeout error types.
func TestIsTimeoutError(t *testing.T) {
	if !isTimeoutError(context.DeadlineExceeded) {
		t.Error("context.DeadlineExceeded should be timeout")
	}
	if isTimeoutError(errors.New("some other error")) {
		t.Error("random error should not be timeout")
	}
	if isTimeoutError(nil) {
		t.Error("nil should not be timeout")
	}
}

// TestCallWithRetry_MaxAttempts1_NoOp verifies that MaxAttempts=1 makes exactly
// one call with no log entries and returns the result immediately.
func TestCallWithRetry_MaxAttempts1_NoOp(t *testing.T) {
	calls := 0
	doCall := func() (*http.Response, error) {
		calls++
		return resp200(), nil
	}
	policy := RetryPolicy{MaxAttempts: 1, RetryOn5xx: true, RetryOnTimeout: true,
		InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond}
	resp, err := callWithRetry(context.Background(), policy, nil, "test", "", doCall)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

// TestCallWithRetry_5xxRetry verifies that 5xx errors are retried when RetryOn5xx=true.
func TestCallWithRetry_5xxRetry(t *testing.T) {
	doCall := makeResponses([]callPair{
		{nil, err5xx()},
		{resp200(), nil},
	})
	policy := RetryPolicy{
		MaxAttempts:    2,
		RetryOn5xx:     true,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
	}
	resp, err := callWithRetry(context.Background(), policy, nil, "test", "", doCall)
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestCallWithRetry_5xxNoRetry verifies that 5xx errors are NOT retried when RetryOn5xx=false.
func TestCallWithRetry_5xxNoRetry(t *testing.T) {
	calls := 0
	doCall := func() (*http.Response, error) {
		calls++
		return nil, err5xx()
	}
	policy := RetryPolicy{
		MaxAttempts:    3,
		RetryOn5xx:     false,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
	}
	_, err := callWithRetry(context.Background(), policy, nil, "test", "", doCall)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 1 {
		t.Errorf("expected 1 call (no retry), got %d", calls)
	}
}

// TestCallWithRetry_TimeoutRetry verifies that timeout errors are retried when RetryOnTimeout=true.
func TestCallWithRetry_TimeoutRetry(t *testing.T) {
	doCall := makeResponses([]callPair{
		{nil, errTimeout()},
		{resp200(), nil},
	})
	policy := RetryPolicy{
		MaxAttempts:    2,
		RetryOnTimeout: true,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
	}
	resp, err := callWithRetry(context.Background(), policy, nil, "test", "", doCall)
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestCallWithRetry_TimeoutExhausted verifies that exhausted timeout errors are wrapped.
func TestCallWithRetry_TimeoutExhausted(t *testing.T) {
	doCall := func() (*http.Response, error) { return nil, errTimeout() }
	policy := RetryPolicy{
		MaxAttempts:    2,
		RetryOnTimeout: true,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
	}
	_, err := callWithRetry(context.Background(), policy, nil, "myprovider", "", doCall)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if !isTimeoutError(errors.Unwrap(err)) {
		t.Errorf("expected wrapped timeout error, got: %v", err)
	}
}

// TestCallWithRetry_429WithRetryAfter verifies that 429 responses are retried
// and the Retry-After header is respected.
func TestCallWithRetry_429WithRetryAfter(t *testing.T) {
	// First call returns 429 with Retry-After: 0 (instant), second succeeds.
	doCall := makeResponses([]callPair{
		{resp429("0"), nil},
		{resp200(), nil},
	})
	policy := RetryPolicy{
		MaxAttempts:    2,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
	}
	resp, err := callWithRetry(context.Background(), policy, nil, "test", "", doCall)
	if err != nil {
		t.Fatalf("expected success after 429 retry, got: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestCallWithRetry_429Exhausted verifies that a 429 on the last attempt is returned as-is.
func TestCallWithRetry_429Exhausted(t *testing.T) {
	doCall := func() (*http.Response, error) { return resp429(""), nil }
	policy := RetryPolicy{
		MaxAttempts:    1,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
	}
	resp, err := callWithRetry(context.Background(), policy, nil, "test", "", doCall)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("expected 429 pass-through, got %d", resp.StatusCode)
	}
}

// TestCallWithRetry_ContextCancellation verifies that context cancellation is
// detected before issuing the next attempt.
func TestCallWithRetry_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	doCall := func() (*http.Response, error) {
		calls++
		cancel() // cancel after first call
		return nil, err5xx()
	}
	policy := RetryPolicy{
		MaxAttempts:    3,
		RetryOn5xx:     true,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
	}
	_, err := callWithRetry(ctx, policy, nil, "test", "", doCall)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call before cancellation, got %d", calls)
	}
}

// TestCallWithRetry_NonRetryable4xx verifies that non-429 4xx responses are
// returned immediately without retry.
func TestCallWithRetry_NonRetryable4xx(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.WriteHeader(http.StatusBadRequest)
	badResp := rec.Result()

	calls := 0
	doCall := func() (*http.Response, error) {
		calls++
		return badResp, nil
	}
	policy := RetryPolicy{
		MaxAttempts:    3,
		RetryOn5xx:     true,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
	}
	resp, err := callWithRetry(context.Background(), policy, nil, "test", "", doCall)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 pass-through, got %d", resp.StatusCode)
	}
	if calls != 1 {
		t.Errorf("expected 1 call (no retry for 4xx), got %d", calls)
	}
}

// TestExponentialBackoff verifies that backoff grows and is capped at MaxBackoff.
func TestExponentialBackoff(t *testing.T) {
	initial := 100 * time.Millisecond
	maxB := 500 * time.Millisecond

	for attempt := 0; attempt < 10; attempt++ {
		d := exponentialBackoff(initial, maxB, attempt)
		// Allow ±10% jitter on top of the cap.
		ceiling := time.Duration(float64(maxB) * 1.1)
		if d > ceiling {
			t.Errorf("attempt %d: backoff %v exceeds ceiling %v", attempt, d, ceiling)
		}
		if d < 0 {
			t.Errorf("attempt %d: negative backoff %v", attempt, d)
		}
	}
}

// ---------------------------------------------------------------------------
// Body-drain tests
// ---------------------------------------------------------------------------

type trackingBody struct {
	io.Reader
	closed bool
}

func (tb *trackingBody) Close() error {
	tb.closed = true
	return nil
}

func resp429WithBody(retryAfter string, body io.ReadCloser) *http.Response {
	h := make(http.Header)
	if retryAfter != "" {
		h.Set("Retry-After", retryAfter)
	}
	return &http.Response{StatusCode: http.StatusTooManyRequests, Header: h, Body: body}
}

// TestCallWithRetry_429BodyClosedOnRetry asserts that the body of a retried
// 429 response is drained and closed before the next attempt.
func TestCallWithRetry_429BodyClosedOnRetry(t *testing.T) {
	tb := &trackingBody{Reader: strings.NewReader("rate limited")}
	doCall := makeResponses([]callPair{
		{resp429WithBody("0", tb), nil},
		{resp200(), nil},
	})
	policy := RetryPolicy{
		MaxAttempts:    2,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
	}
	resp, err := callWithRetry(context.Background(), policy, nil, "test", "", doCall)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if !tb.closed {
		t.Error("429 response body was not closed after retry")
	}
}

// TestCallWithRetry_SustainedTrackingNoLeak drives multiple consecutive 429s
// and asserts every retried body was closed.
func TestCallWithRetry_SustainedTrackingNoLeak(t *testing.T) {
	const n = 5
	bodies := make([]*trackingBody, n)
	pairs := make([]callPair, n+1)
	for i := 0; i < n; i++ {
		bodies[i] = &trackingBody{Reader: strings.NewReader("rate limited")}
		pairs[i] = callPair{resp429WithBody("0", bodies[i]), nil}
	}
	pairs[n] = callPair{resp200(), nil}

	policy := RetryPolicy{
		MaxAttempts:    n + 1,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
	}
	resp, err := callWithRetry(context.Background(), policy, nil, "test", "", makeResponses(pairs))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	for i, tb := range bodies {
		if !tb.closed {
			t.Errorf("body[%d] was not closed", i)
		}
	}
}

// TestCallWithRetry_429Exhausted_BodyNotClosed is a regression guard: when a
// 429 occurs on the final attempt, callWithRetry returns it with body open so
// ownership passes to the caller — we must not close it prematurely.
func TestCallWithRetry_429Exhausted_BodyNotClosed(t *testing.T) {
	tb := &trackingBody{Reader: strings.NewReader("rate limited")}
	doCall := makeResponses([]callPair{
		{resp429WithBody("", tb), nil},
	})
	policy := RetryPolicy{
		MaxAttempts:    1,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     10 * time.Millisecond,
	}
	resp, err := callWithRetry(context.Background(), policy, nil, "test", "", doCall)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("expected 429 pass-through, got %d", resp.StatusCode)
	}
	if tb.closed {
		t.Error("final 429 body must not be closed by callWithRetry — caller owns it")
	}
}

// ---------------------------------------------------------------------------
// parseRetryAfterSeconds unit tests
// ---------------------------------------------------------------------------

func makeRetryAfterResp(header string) *http.Response {
	h := make(http.Header)
	if header != "" {
		h.Set("Retry-After", header)
	}
	return &http.Response{StatusCode: http.StatusTooManyRequests, Header: h, Body: http.NoBody}
}

func TestParseRetryAfterSeconds_Numeric(t *testing.T) {
	secs, ok := parseRetryAfterSeconds(makeRetryAfterResp("120"))
	if !ok {
		t.Fatal("ok: got false, want true")
	}
	if secs != 120 {
		t.Errorf("secs: got %v, want 120", secs)
	}
}

func TestParseRetryAfterSeconds_HTTPDateFuture(t *testing.T) {
	const offset = 90 * time.Second
	future := time.Now().Add(offset).UTC().Format(http.TimeFormat)

	secs, ok := parseRetryAfterSeconds(makeRetryAfterResp(future))
	if !ok {
		t.Fatalf("ok: got false, want true (header: %q)", future)
	}
	const tol = 2.0
	if secs < float64(offset/time.Second)-tol || secs > float64(offset/time.Second)+tol {
		t.Errorf("secs: got %v, want ~%v (±%v)", secs, offset.Seconds(), tol)
	}
}

func TestParseRetryAfterSeconds_HTTPDatePast(t *testing.T) {
	past := time.Now().Add(-30 * time.Second).UTC().Format(http.TimeFormat)

	secs, ok := parseRetryAfterSeconds(makeRetryAfterResp(past))
	if !ok {
		t.Fatal("ok: got false, want true — past date is valid, signals immediate retry")
	}
	if secs != 0 {
		t.Errorf("secs: got %v, want 0 (past date → retry immediately)", secs)
	}
}

func TestParseRetryAfterSeconds_Malformed(t *testing.T) {
	secs, ok := parseRetryAfterSeconds(makeRetryAfterResp("banana"))
	if ok {
		t.Error("ok: got true, want false for garbage value")
	}
	if secs != 0 {
		t.Errorf("secs: got %v, want 0", secs)
	}
}

func TestParseRetryAfterSeconds_Empty(t *testing.T) {
	secs, ok := parseRetryAfterSeconds(makeRetryAfterResp(""))
	if ok {
		t.Error("ok: got true, want false for absent header")
	}
	if secs != 0 {
		t.Errorf("secs: got %v, want 0", secs)
	}
}
