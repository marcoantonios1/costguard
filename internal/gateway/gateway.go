package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/marcoantonios1/costguard/internal/budget"
	"github.com/marcoantonios1/costguard/internal/cache"
	"github.com/marcoantonios1/costguard/internal/logging"
	"github.com/marcoantonios1/costguard/internal/metering"
	"github.com/marcoantonios1/costguard/internal/providers"
	"github.com/marcoantonios1/costguard/internal/server"
	"github.com/marcoantonios1/costguard/internal/usage"
)

func newJSONErrorResponse(r *http.Request, status int, message string) *http.Response {
	body := fmt.Sprintf(`{"error":{"message":"%s"}}`, message)

	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader([]byte(body))),
		Request:    r,
	}
}

type Router interface {
	PickProvider(model string) string
}

type BudgetChecker interface {
	CheckMonthlyBudget(ctx context.Context, now time.Time) error
}

type Gateway struct {
	router Router
	reg    *providers.Registry
	log    *logging.Log

	fallback      string
	cache         cache.Cache
	cacheTTL      time.Duration
	usageStore    usage.Store
	budgetChecker BudgetChecker
}

type Deps struct {
	Router   Router
	Registry *providers.Registry
	Log      *logging.Log

	FallbackProvider string
	Cache            cache.Cache
	CacheTTL         time.Duration
	UsageStore       usage.Store
	BudgetChecker    BudgetChecker
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
		return nil, errors.New("router is required")
	}
	if d.Registry == nil {
		return nil, errors.New("registry is required")
	}

	return &Gateway{
		router:        d.Router,
		reg:           d.Registry,
		log:           d.Log,
		fallback:      d.FallbackProvider,
		cache:         d.Cache,
		cacheTTL:      d.CacheTTL,
		usageStore:    d.UsageStore,
		budgetChecker: d.BudgetChecker,
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

			g.meterResponse(r, providerName, model, entry.Body, true, http.StatusOK)
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

	if g.budgetChecker != nil {
		if err := g.budgetChecker.CheckMonthlyBudget(r.Context(), time.Now()); err != nil {
			if errors.Is(err, budget.ErrMonthlyBudgetExceeded) {
				if g.log != nil {
					g.log.Error("monthly_budget_exceeded", map[string]any{
						"request_id": server.RequestIDFromContext(r.Context()),
					})
				}

				return newJSONErrorResponse(
					r,
					http.StatusPaymentRequired,
					"monthly budget exceeded",
				), nil
			}

			return nil, err
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
	g.meterResponse(r, providerName, model, body, false, resp.StatusCode)

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

func (g *Gateway) meterResponse(
	r *http.Request,
	providerName string,
	model string,
	body []byte,
	cacheHit bool,
	statusCode int,
) {
	requestID := server.RequestIDFromContext(r.Context())
	team := r.Header.Get("X-Costguard-Team")
	project := r.Header.Get("X-Costguard-Project")
	user := r.Header.Get("X-Costguard-User")

	if cacheHit {
		usageData := metering.Usage{
			Provider:         providerName,
			Model:            model,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
			CacheHit:         true,
		}

		cost, priceFound := metering.EstimateCost(usageData)

		fields := map[string]any{
			"request_id":         requestID,
			"provider":           providerName,
			"model":              model,
			"prompt_tokens":      0,
			"completion_tokens":  0,
			"total_tokens":       0,
			"estimated_cost_usd": cost,
			"cache_hit":          true,
		}
		if !priceFound {
			fields["price_found"] = false
		}

		if g.log != nil {
			g.log.Info("request_metered", fields)
		}

		if g.usageStore != nil {
			record := usage.Record{
				RequestID:        requestID,
				Timestamp:        time.Now().UTC(),
				Provider:         providerName,
				Model:            model,
				PromptTokens:     0,
				CompletionTokens: 0,
				TotalTokens:      0,
				EstimatedCostUSD: cost,
				PriceFound:       priceFound,
				CacheHit:         true,
				Team:             team,
				Project:          project,
				User:             user,
				Path:             r.URL.Path,
				StatusCode:       statusCode,
			}

			if err := g.usageStore.Save(r.Context(), record); err != nil && g.log != nil {
				g.log.Error("usage_save_failed", map[string]any{
					"request_id": requestID,
					"error":      err.Error(),
				})
			}
		}

		return
	}

	var resp openAIUsageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		if g.log != nil {
			g.log.Error("metering_parse_failed", map[string]any{
				"request_id": requestID,
				"provider":   providerName,
				"error":      err.Error(),
			})
		}
		return
	}

	finalModel := resp.Model
	if finalModel == "" {
		finalModel = model
	}

	usageData := metering.Usage{
		Provider:         providerName,
		Model:            finalModel,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
		CacheHit:         false,
	}

	cost, priceFound := metering.EstimateCost(usageData)

	fields := map[string]any{
		"request_id":        requestID,
		"provider":          providerName,
		"model":             finalModel,
		"prompt_tokens":     usageData.PromptTokens,
		"completion_tokens": usageData.CompletionTokens,
		"total_tokens":      usageData.TotalTokens,
		"cache_hit":         false,
	}

	if priceFound {
		fields["estimated_cost_usd"] = cost
	} else {
		fields["estimated_cost_usd"] = 0
		fields["price_found"] = false
	}

	if g.log != nil {
		g.log.Info("request_metered", fields)
	}

	if g.usageStore != nil {
		record := usage.Record{
			RequestID:        requestID,
			Timestamp:        time.Now().UTC(),
			Provider:         providerName,
			Model:            finalModel,
			PromptTokens:     usageData.PromptTokens,
			CompletionTokens: usageData.CompletionTokens,
			TotalTokens:      usageData.TotalTokens,
			EstimatedCostUSD: cost,
			PriceFound:       priceFound,
			CacheHit:         false,
			Team:             team,
			Project:          project,
			User:             user,
			Path:             r.URL.Path,
			StatusCode:       statusCode,
		}

		if err := g.usageStore.Save(r.Context(), record); err != nil && g.log != nil {
			g.log.Error("usage_save_failed", map[string]any{
				"request_id": requestID,
				"error":      err.Error(),
			})
		}
	}
}
