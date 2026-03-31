package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// passthroughStreaming returns the response immediately with its body replaced
// by a pipe. A goroutine forwards the upstream SSE stream through the pipe,
// scanning each chunk for usage data. After the stream ends the goroutine
// calls meterStreamingUsage asynchronously.
func (g *Gateway) passthroughStreaming(r *http.Request, resp *http.Response, providerName, model string) *http.Response {
	pr, pw := io.Pipe()

	go func() {
		defer resp.Body.Close()
		defer pw.Close()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)

		var (
			finalModel       = model
			promptTokens     int
			completionTokens int
			totalTokens      int
		)

		for scanner.Scan() {
			line := scanner.Text()
			// Write the line plus the newline the scanner stripped.
			if _, err := fmt.Fprintf(pw, "%s\n", line); err != nil {
				return // client disconnected
			}

			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				continue
			}

			var chunk struct {
				Model string `json:"model"`
				Usage *struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
					TotalTokens      int `json:"total_tokens"`
				} `json:"usage"`
			}
			if json.Unmarshal([]byte(data), &chunk) == nil {
				if chunk.Model != "" {
					finalModel = chunk.Model
				}
				if chunk.Usage != nil && chunk.Usage.TotalTokens > 0 {
					promptTokens = chunk.Usage.PromptTokens
					completionTokens = chunk.Usage.CompletionTokens
					totalTokens = chunk.Usage.TotalTokens
				}
			}
		}

		g.meterStreamingUsage(r, providerName, finalModel, promptTokens, completionTokens, totalTokens)
	}()

	return &http.Response{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Header:     resp.Header.Clone(),
		Body:       pr,
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
