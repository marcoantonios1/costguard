package gateway

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/marcoantonios1/costguard/internal/metering"
	"github.com/marcoantonios1/costguard/internal/providers"
	"github.com/marcoantonios1/costguard/internal/providers/openaiformat"
	"github.com/marcoantonios1/costguard/internal/server"
	"github.com/marcoantonios1/costguard/internal/usage"
)

// estimateVisionTokens returns a client-side image token estimate for the
// request body, dispatched by provider family. Returns 0 for providers that
// report image tokens in their usage response (gemini) or have no estimation
// formula (openaicompat).
func estimateVisionTokens(p providers.Provider, reqBody []byte) int {
	images := openaiformat.ExtractRequestImages(reqBody)
	if len(images) == 0 {
		return 0
	}
	switch p.Family() {
	case "anthropic":
		return openaiformat.AnthropicImageTokens(images)
	case "openai":
		return openaiformat.OpenAIImageTokens(images)
	default:
		return 0
	}
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

func (g *Gateway) meterResponse(
	r *http.Request,
	reqBodyBytes []byte,
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
	agent := r.Header.Get("X-Costguard-Agent")

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
			"agent":              agent,
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
				Agent:            agent,
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

	p, err := g.reg.Get(providerName)
	if err != nil {
		if g.log != nil {
			g.log.Error("metering_provider_lookup_failed", map[string]any{
				"request_id": requestID,
				"provider":   providerName,
				"error":      err.Error(),
			})
		}
		return
	}

	meta, err := p.ParseResponseMeta(body)
	if err != nil {
		if g.log != nil {
			g.log.Error("metering_parse_failed", map[string]any{
				"request_id": requestID,
				"provider":   providerName,
				"error":      err.Error(),
			})
		}
		return
	}

	// Vision token correction: if the upstream did not report any prompt tokens
	// and the request contained image blocks, add a client-side estimate so
	// that budget accounting reflects the true vision cost.
	if estimate := estimateVisionTokens(p, reqBodyBytes); estimate > 0 && meta.PromptTokens == 0 {
		meta.PromptTokens = estimate
		meta.TotalTokens = estimate + meta.CompletionTokens
	}

	finalModel := meta.Model
	if finalModel == "" {
		finalModel = model
	}

	usageData := metering.Usage{
		Provider:         providerName,
		Model:            finalModel,
		PromptTokens:     meta.PromptTokens,
		CompletionTokens: meta.CompletionTokens,
		TotalTokens:      meta.TotalTokens,
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
		"agent":             agent,
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
			Agent:            agent,
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
