package gateway

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/marcoantonios1/costguard/internal/server"
)

func (g *Gateway) callProvider(r *http.Request, bodyBytes []byte, providerName string) (*http.Response, error) {
	p, err := g.reg.Get(providerName)
	if err != nil {
		return nil, err
	}

	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	if g.log != nil {
		g.log.Info("provider_call", map[string]any{
			"request_id": server.RequestIDFromContext(r.Context()),
			"provider":   providerName,
			"path":       r.URL.Path,
		})
	}

	resp, err := p.Do(r.Context(), r)
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

		if resp.StatusCode >= 500 && g.fallback != "" && providerName != g.fallback {
			return nil, fmt.Errorf("upstream_5xx status=%d provider=%s", resp.StatusCode, providerName)
		}

		return resp, nil
	}

	if resp.StatusCode >= 500 && g.fallback != "" && providerName != g.fallback {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("upstream_5xx status=%d provider=%s", resp.StatusCode, providerName)
	}

	return resp, nil
}

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
