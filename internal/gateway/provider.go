package gateway

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/marcoantonios1/costguard/internal/server"
)

func (g *Gateway) maybeStoreAndReturn(
	r *http.Request,
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	_ = resp.Body.Close()

	g.meterResponse(r, providerName, model, body, false, resp.StatusCode)

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

func (g *Gateway) callProviderWithFallback(r *http.Request, providerName string) (*http.Response, string, error) {
	resp, err := g.callSingleProvider(r, providerName)
	if err == nil {
		return resp, providerName, nil
	}

	if g.fallback == "" || providerName == g.fallback || !isRetryableProviderError(err) {
		return nil, providerName, err
	}

	if g.log != nil {
		g.log.Error("provider_failed_try_fallback", map[string]any{
			"request_id": server.RequestIDFromContext(r.Context()),
			"primary":    providerName,
			"fallback":   g.fallback,
			"err":        err.Error(),
		})
	}

	fallbackResp, fallbackErr := g.callSingleProvider(r, g.fallback)
	if fallbackErr != nil {
		return nil, g.fallback, fallbackErr
	}

	if g.log != nil {
		g.log.Info("fallback_used", map[string]any{
			"request_id": server.RequestIDFromContext(r.Context()),
			"primary":    providerName,
			"fallback":   g.fallback,
		})
	}

	return fallbackResp, g.fallback, nil
}

func (g *Gateway) callSingleProvider(r *http.Request, providerName string) (*http.Response, error) {
	p, err := g.reg.Get(providerName)
	if err != nil {
		return nil, err
	}

	reqCopy, err := cloneRequest(r)
	if err != nil {
		return nil, err
	}

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

	msg := err.Error()

	switch {
	case strings.Contains(msg, "provider not found"):
		return true
	case strings.Contains(msg, "upstream_5xx"):
		return true
	default:
		return false
	}
}

func cloneRequest(r *http.Request) (*http.Request, error) {
	var bodyBytes []byte
	var err error

	if r.Body != nil {
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	reqCopy := r.Clone(r.Context())
	reqCopy.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	reqCopy.ContentLength = int64(len(bodyBytes))

	return reqCopy, nil
}
