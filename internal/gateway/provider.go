package gateway

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/marcoantonios1/costguard/internal/server"
)

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

func (g *Gateway) callProviderWithFallback(r *http.Request, providerName string, bodyBytes []byte, originalModel string) (*http.Response, string, string, error) {
	resp, err := g.callSingleProvider(r, providerName, bodyBytes)
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
		g.log.Error("provider_failed_try_fallback", map[string]any{
			"request_id":     server.RequestIDFromContext(r.Context()),
			"primary":        providerName,
			"fallback":       g.fallback,
			"original_model": originalModel,
			"fallback_model": fallbackModel,
			"err":            err.Error(),
		})
	}

	fallbackResp, fallbackErr := g.callSingleProvider(r, g.fallback, rewrittenBody)
	if fallbackErr != nil {
		if g.log != nil {
			g.log.Error("fallback_failed", map[string]any{
				"request_id":     server.RequestIDFromContext(r.Context()),
				"primary":        providerName,
				"fallback":       g.fallback,
				"original_model": originalModel,
				"fallback_model": fallbackModel,
				"err":            fallbackErr.Error(),
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

func (g *Gateway) callSingleProvider(r *http.Request, providerName string, bodyBytes []byte) (*http.Response, error) {
	p, err := g.reg.Get(providerName)
	if err != nil {
		return nil, err
	}

	reqCopy := cloneRequestWithBody(r, bodyBytes)

	resp, err := p.Do(reqCopy.Context(), reqCopy)
	if err != nil {
		return nil, err
	}

	if resp != nil && resp.StatusCode >= 400 && resp.Body != nil {
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}

		normalizedBody, normErr := p.NormalizeError(resp.StatusCode, body)
		if normErr != nil {
			return nil, normErr
		}

		resp.Body = io.NopCloser(bytes.NewReader(normalizedBody))
		resp.ContentLength = int64(len(normalizedBody))
		resp.Header.Set("Content-Type", "application/json")

		if resp.StatusCode >= 500 {
			return nil, fmt.Errorf("upstream_5xx status=%d provider=%s", resp.StatusCode, providerName)
		}

		return resp, nil
	}

	return resp, nil
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
