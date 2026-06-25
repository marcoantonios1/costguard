package gateway

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/marcoantonios1/costguard/internal/breaker"
	"github.com/marcoantonios1/costguard/internal/health"
	"github.com/marcoantonios1/costguard/internal/providers"
	"github.com/marcoantonios1/costguard/internal/server"
)

// upstreamStatusError is returned for upstream 5xx responses. It preserves the
// original HTTP status code so proxy.go can record it in the usage store, while
// keeping the "upstream_5xx" substring that is5xxError and
// isRetryableProviderError rely on for retry/fallback/breaker decisions.
type upstreamStatusError struct {
	statusCode int
	provider   string
}

func (e *upstreamStatusError) Error() string {
	return fmt.Sprintf("upstream_5xx status=%d provider=%s", e.statusCode, e.provider)
}

func (g *Gateway) maybeStoreAndReturn(
	r *http.Request,
	reqBodyBytes []byte,
	resp *http.Response,
	providerName, model string,
	cacheable bool,
	cacheKey string,
) (*http.Response, error) {
	if resp == nil || resp.Body == nil {
		return resp, nil
	}

	if resp.StatusCode != http.StatusOK {
		// Meter a zero-cost record so 4xx responses appear in /admin/usage/*.
		// The body and headers are passed back unchanged to the caller.
		//
		// Streaming + non-200 is unreachable: callSingleProvider forces
		// Content-Type: application/json for all error responses, so
		// isStreamingResponse can never return true for a non-200 response.
		//
		// Cache writes are intentionally skipped — failed responses must never
		// be served from cache on subsequent requests.
		//
		// 5xx responses are converted to Go errors in callSingleProvider so that
		// retry/fallback/breaker machinery can act on them; they are metered in
		// proxy.go via the error path instead.
		g.meterFailedResponse(r, providerName, model, resp.StatusCode)
		return resp, nil
	}

	// Streaming responses must not be buffered. Pass them through directly and
	// meter asynchronously after the stream is consumed.
	if isStreamingResponse(resp) {
		return g.passthroughStreaming(r, resp, providerName, model, reqBodyBytes), nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	_ = resp.Body.Close()

	g.meterResponse(r, reqBodyBytes, providerName, model, body, false, resp.StatusCode)

	if !cacheable || g.cache == nil || g.cacheTTL <= 0 {
		return &http.Response{
			StatusCode: resp.StatusCode,
			Status:     fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode)),
			Header:     resp.Header.Clone(),
			Body:       io.NopCloser(bytes.NewReader(body)),
			Request:    r,
		}, nil
	}

	entry := cacheEntryFromResponse(resp, body, g.cacheTTL)
	g.cache.Set(cacheKey, entry)

	if g.log != nil {
		g.log.Info("cache_store", map[string]any{
			"request_id": server.RequestIDFromContext(r.Context()),
			"key":        shortKey(cacheKey),
			"path":       r.URL.Path,
			"status":     resp.StatusCode,
			"ttl_ms":     g.cacheTTL.Milliseconds(),
		})
	}

	return responseFromCacheEntry(r, entry), nil
}

// gatewayErrorCategoryAndType derives the taxonomy category and normalized type
// string from a gateway-level error (network, timeout, or upstream_5xx).
func gatewayErrorCategoryAndType(err error) (category, errType string) {
	if err == nil {
		return providers.ErrCategoryUpstreamFailure, "upstream_error"
	}
	if isTimeoutError(err) || strings.Contains(err.Error(), "provider_timeout") {
		return providers.ErrCategoryProviderUnavailable, "provider_unavailable_error"
	}
	if is5xxError(err) {
		return providers.ErrCategoryUpstreamFailure, "upstream_error"
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return providers.ErrCategoryProviderUnavailable, "provider_unavailable_error"
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "dial tcp") ||
		strings.Contains(msg, "no such host") {
		return providers.ErrCategoryProviderUnavailable, "provider_unavailable_error"
	}
	return providers.ErrCategoryUpstreamFailure, "upstream_error"
}

func (g *Gateway) providerRetryPolicy(providerName string) RetryPolicy {
	if p, ok := g.retryPolicies[providerName]; ok {
		return p
	}
	return DefaultRetryPolicy()
}

func (g *Gateway) callProviderWithFallback(r *http.Request, providerName string, bodyBytes []byte, originalModel string) (*http.Response, string, string, error) {
	resp, err := g.callThroughBreaker(r, providerName, bodyBytes)
	if err == nil {
		return resp, providerName, originalModel, nil
	}

	if g.fallback == "" || providerName == g.fallback || !isRetryableProviderError(err) {
		return nil, providerName, originalModel, err
	}

	fallbackModel := originalModel
	rewrittenBody := bodyBytes

	if compatible := g.compatibleModelForProvider(originalModel, g.fallback); compatible != "" && compatible != originalModel {
		var rewriteErr error
		rewrittenBody, rewriteErr = rewriteModelInBody(bodyBytes, compatible)
		if rewriteErr != nil {
			return nil, g.fallback, originalModel, rewriteErr
		}
		fallbackModel = compatible
	}

	if g.log != nil {
		category, _ := gatewayErrorCategoryAndType(err)
		g.log.Error("provider_failed_try_fallback", map[string]any{
			"request_id":     server.RequestIDFromContext(r.Context()),
			"primary":        providerName,
			"fallback":       g.fallback,
			"original_model": originalModel,
			"fallback_model": fallbackModel,
			"err":            err.Error(),
			"error_category": category,
		})
	}

	fallbackResp, fallbackErr := g.callThroughBreaker(r, g.fallback, rewrittenBody)
	if fallbackErr != nil {
		if g.log != nil {
			category, _ := gatewayErrorCategoryAndType(fallbackErr)
			g.log.Error("fallback_failed", map[string]any{
				"request_id":     server.RequestIDFromContext(r.Context()),
				"primary":        providerName,
				"fallback":       g.fallback,
				"original_model": originalModel,
				"fallback_model": fallbackModel,
				"err":            fallbackErr.Error(),
				"error_category": category,
			})
		}
		return nil, g.fallback, fallbackModel, fallbackErr
	}

	if g.log != nil {
		g.log.Info("fallback_used", map[string]any{
			"request_id":     server.RequestIDFromContext(r.Context()),
			"primary":        providerName,
			"fallback":       g.fallback,
			"original_model": originalModel,
			"fallback_model": fallbackModel,
		})
	}

	return fallbackResp, g.fallback, fallbackModel, nil
}

// callThroughBreaker gates a single provider call through the circuit breaker:
// pre-check → callSingleProvider → record outcome.
func (g *Gateway) callThroughBreaker(r *http.Request, providerName string, bodyBytes []byte) (*http.Response, error) {
	if g.breakers != nil {
		b := g.breakers.For(providerName)
		if allowed, state := b.Allow(); !allowed {
			if g.log != nil {
				g.log.Warn("circuit_breaker_open", map[string]any{
					"request_id": server.RequestIDFromContext(r.Context()),
					"provider":   providerName,
					"state":      string(state),
				})
			}
			return nil, fmt.Errorf("circuit_breaker_open provider=%s", providerName)
		}
	}

	resp, err := g.callSingleProvider(r, providerName, bodyBytes, g.providerRetryPolicy(providerName))

	if g.breakers != nil {
		b := g.breakers.For(providerName)
		if err != nil {
			prevState := b.State()
			category, _ := gatewayErrorCategoryAndType(err)
			b.RecordFailure(category)
			if b.State() == breaker.StateOpen && prevState == breaker.StateClosed {
				if g.log != nil {
					g.log.Warn("circuit_breaker_tripped", map[string]any{
						"request_id": server.RequestIDFromContext(r.Context()),
						"provider":   providerName,
						"threshold":  b.Stats().ConsecFailures,
					})
				}
			}
		} else {
			prevState := b.State()
			b.RecordSuccess()
			if prevState == breaker.StateHalfOpen && g.log != nil {
				g.log.Info("circuit_breaker_closed", map[string]any{
					"request_id": server.RequestIDFromContext(r.Context()),
					"provider":   providerName,
				})
			}
		}
	}

	return resp, err
}

func (g *Gateway) callSingleProvider(r *http.Request, providerName string, bodyBytes []byte, policy RetryPolicy) (*http.Response, error) {
	requestID := server.RequestIDFromContext(r.Context())
	return callWithRetry(r.Context(), policy, g.log, providerName, requestID, func() (*http.Response, error) {
		p, err := g.reg.Get(providerName)
		if err != nil {
			return nil, err
		}

		reqCopy := cloneRequestWithBody(r, bodyBytes)

		start := time.Now()
		resp, err := p.Do(reqCopy.Context(), reqCopy)
		latency := time.Since(start)

		if err != nil {
			g.recordOutcome(providerName, health.Outcome{
				Success:       false,
				Latency:       latency,
				Timestamp:     time.Now(),
				ErrorCategory: providers.ErrCategoryProviderUnavailable,
			})
			return nil, err
		}

		if resp != nil && resp.StatusCode >= 400 && resp.Body != nil {
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr != nil {
				g.recordOutcome(providerName, health.Outcome{
					Success:       false,
					Latency:       latency,
					Timestamp:     time.Now(),
					ErrorCategory: providers.ErrCategoryUpstreamFailure,
				})
				return nil, readErr
			}

			normalizedBody, normErr := p.NormalizeError(resp.StatusCode, body)
			if normErr != nil {
				g.recordOutcome(providerName, health.Outcome{
					Success:       false,
					Latency:       latency,
					Timestamp:     time.Now(),
					ErrorCategory: providers.ErrCategoryUpstreamFailure,
				})
				return nil, normErr
			}

			resp.Body = io.NopCloser(bytes.NewReader(normalizedBody))
			resp.ContentLength = int64(len(normalizedBody))
			resp.Header.Set("Content-Type", "application/json")

			if resp.StatusCode >= 500 {
				g.recordOutcome(providerName, health.Outcome{
					Success:       false,
					Latency:       latency,
					Timestamp:     time.Now(),
					ErrorCategory: providers.ErrCategoryUpstreamFailure,
				})
				return nil, &upstreamStatusError{statusCode: resp.StatusCode, provider: providerName}
			}

			g.recordOutcome(providerName, health.Outcome{
				Success:       false,
				Latency:       latency,
				Timestamp:     time.Now(),
				ErrorCategory: providers.ErrorCategory("", resp.StatusCode),
			})
			return resp, nil
		}

		g.recordOutcome(providerName, health.Outcome{
			Success:   true,
			Latency:   latency,
			Timestamp: time.Now(),
		})
		return resp, nil
	})
}

func (g *Gateway) recordOutcome(provider string, o health.Outcome) {
	if g.health != nil {
		g.health.Record(provider, o)
	}
}

func isRetryableProviderError(err error) bool {
	if err == nil {
		return false
	}

	// Structured network errors first.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	msg := strings.ToLower(err.Error())

	switch {
	case strings.Contains(msg, "circuit_breaker_open"):
		return true
	case strings.Contains(msg, "provider not found"):
		return true
	case strings.Contains(msg, "upstream_5xx"):
		return true
	case strings.Contains(msg, "dial tcp"):
		return true
	case strings.Contains(msg, "lookup "):
		return true
	case strings.Contains(msg, "no such host"):
		return true
	case strings.Contains(msg, "connection refused"):
		return true
	case strings.Contains(msg, "i/o timeout"):
		return true
	case strings.Contains(msg, "context deadline exceeded"):
		return true
	case strings.Contains(msg, "timeout"):
		return true
	case strings.Contains(msg, "tls handshake timeout"):
		return true
	case strings.Contains(msg, "server misbehaving"):
		return true
	default:
		return false
	}
}

func cloneRequestWithBody(r *http.Request, bodyBytes []byte) *http.Request {
	reqCopy := r.Clone(r.Context())
	reqCopy.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	reqCopy.ContentLength = int64(len(bodyBytes))
	return reqCopy
}
