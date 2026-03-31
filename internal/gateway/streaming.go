package gateway

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/marcoantonios1/costguard/internal/metering"
	"github.com/marcoantonios1/costguard/internal/server"
	"github.com/marcoantonios1/costguard/internal/usage"
)

func isStreamingResponse(resp *http.Response) bool {
	return strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
}

// passthroughStreaming wraps the response body in a StreamMeter and returns
// immediately. The StreamMeter inspects SSE chunks as the handler reads
// through them, then fires meterStreamingUsage in a goroutine once the
// stream ends — no extra goroutine or pipe needed for data forwarding.
func (g *Gateway) passthroughStreaming(r *http.Request, resp *http.Response, providerName, model string) *http.Response {
	meter := newStreamMeter(resp.Body, model, func(finalModel string, prompt, completion, total int) {
		go g.meterStreamingUsage(r, providerName, finalModel, prompt, completion, total)
	})

	return &http.Response{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Header:     resp.Header.Clone(),
		Body:       meter,
		Request:    r,
	}
}

func (g *Gateway) meterStreamingUsage(r *http.Request, providerName, model string, promptTokens, completionTokens, totalTokens int) {
	// Use a detached context so metering completes even after the HTTP
	// response is fully sent and the request context is cancelled.
	ctx := context.WithoutCancel(r.Context())

	requestID := server.RequestIDFromContext(r.Context())
	team := r.Header.Get("X-Costguard-Team")
	project := r.Header.Get("X-Costguard-Project")
	user := r.Header.Get("X-Costguard-User")

	usageData := metering.Usage{
		Provider:         providerName,
		Model:            model,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
	}

	cost, priceFound := metering.EstimateCost(usageData)

	fields := map[string]any{
		"request_id":        requestID,
		"provider":          providerName,
		"model":             model,
		"prompt_tokens":     promptTokens,
		"completion_tokens": completionTokens,
		"total_tokens":      totalTokens,
		"streaming":         true,
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
			Model:            model,
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
			EstimatedCostUSD: cost,
			PriceFound:       priceFound,
			CacheHit:         false,
			Team:             team,
			Project:          project,
			User:             user,
			Path:             r.URL.Path,
			StatusCode:       http.StatusOK,
		}
		if err := g.usageStore.Save(ctx, record); err != nil && g.log != nil {
			g.log.Error("usage_save_failed", map[string]any{
				"request_id": requestID,
				"error":      err.Error(),
			})
		}
	}
}
