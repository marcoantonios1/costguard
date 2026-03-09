package gateway

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/marcoantonios1/costguard/internal/cache"
	"github.com/marcoantonios1/costguard/internal/logging"
	"github.com/marcoantonios1/costguard/internal/metering"
	"github.com/marcoantonios1/costguard/internal/providers"
	"github.com/marcoantonios1/costguard/internal/server"
)

type Router interface {
	PickProvider(model string) string
}

type Gateway struct {
	router Router
	reg    *providers.Registry
	log    *logging.Log

	fallback string
	cache    cache.Cache
	cacheTTL time.Duration
}

type Deps struct {
	Router   Router
	Registry *providers.Registry
	Log      *logging.Log

	FallbackProvider string
	Cache            cache.Cache
	CacheTTL         time.Duration
}

type openAIUsageResponse struct {
	Model string `json:"model"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func New(d Deps) (*Gateway, error) {
	if d.Router == nil {
		return nil, fmt.Errorf("router is nil")
	}
	if d.Registry == nil {
		return nil, fmt.Errorf("registry is nil")
	}

	return &Gateway{
		router:   d.Router,
		reg:      d.Registry,
		log:      d.Log,
		fallback: d.FallbackProvider,
		cache:    d.Cache,
		cacheTTL: d.CacheTTL,
	}, nil
}

func (g *Gateway) Proxy(r *http.Request) (*http.Response, error) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	model := extractModel(bodyBytes)
	providerName := g.router.PickProvider(model)

	if g.log != nil {
		g.log.Info("route_selected", map[string]any{
			"request_id": server.RequestIDFromContext(r.Context()),
			"model":      model,
			"provider":   providerName,
			"path":       r.URL.Path,
		})
	}

	cacheable := g.isCacheableRequest(r, bodyBytes)
	cacheKey := ""

	if cacheable && g.cache != nil && g.cacheTTL > 0 {
		cacheKey = buildCacheKey(r, bodyBytes)

		if entry, ok := g.cache.Get(cacheKey); ok {
			if g.log != nil {
				g.log.Info("cache_hit", map[string]any{
					"request_id": server.RequestIDFromContext(r.Context()),
					"key":        shortKey(cacheKey),
					"path":       r.URL.Path,
					"model":      model,
				})
			}

			g.meterResponse(r, providerName, model, entry.Body, true)
			return responseFromCacheEntry(r, entry), nil
		}

		if g.log != nil {
			g.log.Info("cache_miss", map[string]any{
				"request_id": server.RequestIDFromContext(r.Context()),
				"key":        shortKey(cacheKey),
				"path":       r.URL.Path,
				"model":      model,
			})
		}
	}

	resp, err := g.callProvider(r, bodyBytes, providerName)
	if err == nil {
		return g.maybeStoreAndReturn(r, resp, providerName, model, cacheable, cacheKey)
	}

	shouldTryFallback := g.fallback != "" && g.fallback != providerName
	if shouldTryFallback {
		if g.log != nil {
			g.log.Error("provider_failed_try_fallback", map[string]any{
				"request_id": server.RequestIDFromContext(r.Context()),
				"primary":    providerName,
				"fallback":   g.fallback,
				"err":        err.Error(),
			})
		}

		resp, err = g.callProvider(r, bodyBytes, g.fallback)
		if err != nil {
			return nil, err
		}

		return g.maybeStoreAndReturn(r, resp, g.fallback, model, cacheable, cacheKey)
	}

	return nil, err
}

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

	if resp.StatusCode >= 500 && g.fallback != "" && providerName != g.fallback {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("upstream_5xx status=%d provider=%s", resp.StatusCode, providerName)
	}

	return resp, nil
}

func extractModel(body []byte) string {
	var v struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return ""
	}
	return v.Model
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

	// For Phase A, only meter/cache successful responses.
	if resp.StatusCode != http.StatusOK {
		return resp, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	_ = resp.Body.Close()

	// Meter all successful provider responses, even when caching is disabled.
	g.meterResponse(r, providerName, model, body, false)

	// If not cacheable, or cache disabled, just rebuild the response and return it.
	if !cacheable || g.cache == nil || g.cacheTTL <= 0 {
		return &http.Response{
			StatusCode: resp.StatusCode,
			Status:     fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode)),
			Header:     resp.Header.Clone(),
			Body:       io.NopCloser(bytes.NewReader(body)),
			Request:    r,
		}, nil
	}

	entry := cache.Entry{
		StatusCode: resp.StatusCode,
		Header:     cloneHeader(resp.Header),
		Body:       append([]byte(nil), body...),
		ExpiresAt:  time.Now().Add(g.cacheTTL),
	}
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

func (g *Gateway) isCacheableRequest(r *http.Request, body []byte) bool {
	if r.Method != http.MethodPost {
		return false
	}
	if r.URL.Path != "/v1/chat/completions" {
		return false
	}

	var v struct {
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(body, &v); err == nil && v.Stream {
		return false
	}

	return true
}

func buildCacheKey(r *http.Request, body []byte) string {
	sum := sha256.Sum256([]byte(r.Method + "\n" + r.URL.Path + "\n" + string(body)))
	return hex.EncodeToString(sum[:])
}

func shortKey(key string) string {
	if len(key) <= 12 {
		return key
	}
	return key[:12]
}

func cloneHeader(h http.Header) map[string][]string {
	out := make(map[string][]string)

	for k, vv := range h {
		switch http.CanonicalHeaderKey(k) {
		case "Content-Type",
			"Content-Length",
			"Openai-Organization",
			"Openai-Processing-Ms",
			"Openai-Project",
			"Openai-Version":
			out[k] = append([]string(nil), vv...)
		}
	}

	return out
}

func responseFromCacheEntry(r *http.Request, entry cache.Entry) *http.Response {
	header := make(http.Header, len(entry.Header))
	for k, vv := range entry.Header {
		header[k] = append([]string(nil), vv...)
	}

	return &http.Response{
		StatusCode: entry.StatusCode,
		Status:     fmt.Sprintf("%d %s", entry.StatusCode, http.StatusText(entry.StatusCode)),
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(entry.Body)),
		Request:    r,
	}
}

func (g *Gateway) meterResponse(r *http.Request, providerName string, model string, body []byte, cacheHit bool) {
	if g.log == nil {
		return
	}

	if cacheHit {
		usage := metering.Usage{
			Provider:         providerName,
			Model:            model,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
			CacheHit:         true,
		}

		cost, _ := metering.EstimateCost(usage)

		g.log.Info("request_metered", map[string]any{
			"request_id":         server.RequestIDFromContext(r.Context()),
			"provider":           providerName,
			"model":              model,
			"prompt_tokens":      0,
			"completion_tokens":  0,
			"total_tokens":       0,
			"estimated_cost_usd": cost,
			"cache_hit":          true,
		})
		return
	}

	var resp openAIUsageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		g.log.Error("metering_parse_failed", map[string]any{
			"request_id": server.RequestIDFromContext(r.Context()),
			"provider":   providerName,
			"error":      err.Error(),
		})
		return
	}

	finalModel := resp.Model
	if finalModel == "" {
		finalModel = model
	}

	usage := metering.Usage{
		Provider:         providerName,
		Model:            finalModel,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
		CacheHit:         false,
	}

	cost, ok := metering.EstimateCost(usage)

	fields := map[string]any{
		"request_id":        server.RequestIDFromContext(r.Context()),
		"provider":          providerName,
		"model":             finalModel,
		"prompt_tokens":     usage.PromptTokens,
		"completion_tokens": usage.CompletionTokens,
		"total_tokens":      usage.TotalTokens,
		"cache_hit":         false,
	}

	if ok {
		fields["estimated_cost_usd"] = cost
	} else {
		fields["estimated_cost_usd"] = 0
		fields["price_found"] = false
	}

	g.log.Info("request_metered", fields)
}
