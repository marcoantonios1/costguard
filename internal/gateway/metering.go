package gateway

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/marcoantonios1/costguard/internal/metering"
	"github.com/marcoantonios1/costguard/internal/server"
	"github.com/marcoantonios1/costguard/internal/usage"
)

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
