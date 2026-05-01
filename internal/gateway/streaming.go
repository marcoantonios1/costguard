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
//
// reqBodyBytes is the original request body (pre-provider-transform). Its
// length is used as a prompt token estimate when the stream ends without a
// usage chunk.
func (g *Gateway) passthroughStreaming(r *http.Request, resp *http.Response, providerName, model string, reqBodyBytes []byte) *http.Response {
	promptEstimate := len(reqBodyBytes) / 4

	visionEstimate := 0
	if p, err := g.reg.Get(providerName); err == nil {
		visionEstimate = estimateVisionTokens(p, reqBodyBytes)
	}

	meter := newStreamMeter(resp.Body, model, promptEstimate, visionEstimate, func(finalModel string, prompt, completion, total int, estimated bool) {
		go g.meterStreamingUsage(r, providerName, finalModel, prompt, completion, total, estimated)
	})

	return &http.Response{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Header:     resp.Header.Clone(),
		Body:       meter,
		Request:    r,
	}
}

func (g *Gateway) meterStreamingUsage(r *http.Request, providerName, model string, promptTokens, completionTokens, totalTokens int, estimated bool) {
	// Use a detached context so metering completes even after the HTTP
	// response is fully sent and the request context is cancelled.
	ctx := context.WithoutCancel(r.Context())

	requestID := server.RequestIDFromContext(r.Context())
	team := r.Header.Get("X-Costguard-Team")
	project := r.Header.Get("X-Costguard-Project")
	user := r.Header.Get("X-Costguard-User")
	agent := r.Header.Get("X-Costguard-Agent")

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
		"agent":             agent,
	}
	if estimated {
		fields["metering_estimated"] = true
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
			RequestID:         requestID,
			Timestamp:         time.Now().UTC(),
			Provider:          providerName,
			Model:             model,
			PromptTokens:      promptTokens,
			CompletionTokens:  completionTokens,
			TotalTokens:       totalTokens,
			EstimatedCostUSD:  cost,
			PriceFound:        priceFound,
			CacheHit:          false,
			MeteringEstimated: estimated,
			Team:              team,
			Project:           project,
			User:              user,
			Agent:             agent,
			Path:              r.URL.Path,
			StatusCode:        http.StatusOK,
		}
		if err := g.usageStore.Save(ctx, record); err != nil && g.log != nil {
			g.log.Error("usage_save_failed", map[string]any{
				"request_id": requestID,
				"error":      err.Error(),
			})
		}
	}
}
