package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/marcoantonios1/costguard/internal/logging"
)

// RetryPolicy configures per-attempt retry behaviour for a provider.
// MaxAttempts=1 means a single attempt with no retry loop and no log entries.
type RetryPolicy struct {
	MaxAttempts    int
	RetryOn5xx     bool
	RetryOnTimeout bool
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:    1,
		InitialBackoff: 500 * time.Millisecond,
		MaxBackoff:     10 * time.Second,
	}
}

func isTimeoutError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func is5xxError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "upstream_5xx")
}

func exponentialBackoff(initial, maxB time.Duration, attempt int) time.Duration {
	d := time.Duration(float64(initial) * math.Pow(2, float64(attempt)))
	if d > maxB {
		d = maxB
	}
	jitter := float64(d) * 0.1
	d += time.Duration((rand.Float64()*2 - 1) * jitter)
	if d < 0 {
		d = 0
	}
	return d
}

// parseRetryAfterSeconds reads the Retry-After header and returns the delay in
// seconds. It accepts both forms permitted by RFC 9110:
//   - A decimal number of seconds ("120", "0.5").
//   - An HTTP-date ("Wed, 21 Oct 2099 07:28:00 GMT"). The delay is computed as
//     time.Until(parsedTime). If the date is in the past the delay is 0 and ok
//     is still true — the header was valid, just already expired, so the caller
//     should retry immediately rather than treating the header as absent.
//
// Returns (0, false) when the header is absent or cannot be parsed by either
// method; the caller falls back to exponential backoff in that case.
func parseRetryAfterSeconds(resp *http.Response) (float64, bool) {
	if resp == nil {
		return 0, false
	}
	ra := resp.Header.Get("Retry-After")
	if ra == "" {
		return 0, false
	}
	if secs, err := strconv.ParseFloat(ra, 64); err == nil {
		return secs, true
	}
	if t, err := http.ParseTime(ra); err == nil {
		d := time.Until(t)
		if d <= 0 {
			return 0, true
		}
		return d.Seconds(), true
	}
	return 0, false
}

// callWithRetry runs doCall up to policy.MaxAttempts times, retrying on 429,
// retryable 5xx errors, and timeout errors according to policy flags.
// A MaxAttempts of 1 (or 0) executes a single call with no retry overhead.
func callWithRetry(
	ctx context.Context,
	policy RetryPolicy,
	log *logging.Log,
	providerName string,
	requestID string,
	doCall func() (*http.Response, error),
) (*http.Response, error) {
	maxAttempts := policy.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	var lastErr error
	exhausted := false

	for attempt := 0; attempt < maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		resp, err := doCall()

		// 429: always retry when attempts remain.
		if err == nil && resp != nil && resp.StatusCode == http.StatusTooManyRequests {
			if attempt == maxAttempts-1 {
				return resp, nil
			}
			backoff := exponentialBackoff(policy.InitialBackoff, policy.MaxBackoff, attempt)
			if secs, ok := parseRetryAfterSeconds(resp); ok {
				d := time.Duration(secs * float64(time.Second))
				cap := policy.MaxBackoff * 3
				if d > cap {
					if log != nil {
						log.Info("retry_after_capped", map[string]any{
							"provider":      providerName,
							"retry_after_s": secs,
							"cap_ms":        cap.Milliseconds(),
							"request_id":    requestID,
						})
					}
					d = cap
				}
				backoff = d
			}
			if log != nil {
				log.Info("provider_retry_attempt", map[string]any{
					"provider":     providerName,
					"attempt":      attempt + 1,
					"max_attempts": maxAttempts,
					"reason":       "429",
					"backoff_ms":   backoff.Milliseconds(),
					"request_id":   requestID,
				})
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			continue
		}

		if err == nil {
			return resp, nil
		}

		lastErr = err

		reason := ""
		switch {
		case isTimeoutError(err) && policy.RetryOnTimeout:
			reason = "timeout"
		case is5xxError(err) && policy.RetryOn5xx:
			reason = "5xx"
		}

		if reason == "" || attempt == maxAttempts-1 {
			if reason != "" {
				exhausted = true
			}
			break
		}

		backoff := exponentialBackoff(policy.InitialBackoff, policy.MaxBackoff, attempt)
		if log != nil {
			log.Info("provider_retry_attempt", map[string]any{
				"provider":     providerName,
				"attempt":      attempt + 1,
				"max_attempts": maxAttempts,
				"reason":       reason,
				"backoff_ms":   backoff.Milliseconds(),
				"request_id":   requestID,
			})
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
	}

	if exhausted && log != nil {
		log.Info("provider_retry_exhausted", map[string]any{
			"provider":     providerName,
			"max_attempts": maxAttempts,
			"err":          lastErr.Error(),
			"request_id":   requestID,
		})
	}

	if isTimeoutError(lastErr) {
		return nil, fmt.Errorf("provider_timeout provider=%s: %w", providerName, lastErr)
	}

	return nil, lastErr
}
