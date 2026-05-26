package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/marcoantonios1/costguard/internal/server"
	"github.com/marcoantonios1/costguard/internal/usage"
)

// ProxyEmbeddings handles POST /v1/embeddings requests. It selects the
// configured embedding provider, optionally rewrites the model field, proxies
// the request, parses token usage from the response, and meters usage.
func (g *Gateway) ProxyEmbeddings(r *http.Request) (*http.Response, error) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	_ = r.Body.Close()

	effectiveModel := extractModel(bodyBytes)
	effectiveBody := bodyBytes

	hintedProvider := requestedProviderHint(r)

	var providerName string
	if hintedProvider != "" {
		if _, err := g.reg.Get(hintedProvider); err != nil {
			if g.log != nil {
				g.log.Info("provider_hint_rejected", map[string]any{
					"request_id": server.RequestIDFromContext(r.Context()),
					"provider":   hintedProvider,
					"path":       r.URL.Path,
					"reason":     "unknown_provider",
				})
			}
			return newJSONErrorResponse(r, http.StatusBadRequest, "unknown provider hint: "+hintedProvider), nil
		}
		providerName = hintedProvider
	} else {
		providerName = g.embeddingProviderName
		if providerName == "" {
			return newJSONErrorResponse(r, http.StatusServiceUnavailable, "no embedding provider configured"), nil
		}
		if _, err := g.reg.Get(providerName); err != nil {
			return newJSONErrorResponse(r, http.StatusServiceUnavailable, "embedding provider unavailable: "+providerName), nil
		}
	}

	if g.embeddingModel != "" {
		rewritten, rewriteErr := rewriteModelInBody(bodyBytes, g.embeddingModel)
		if rewriteErr == nil {
			effectiveBody = rewritten
			effectiveModel = g.embeddingModel
		}
	}

	if g.log != nil {
		g.log.Info("embedding_routing", map[string]any{
			"request_id": server.RequestIDFromContext(r.Context()),
			"provider":   providerName,
			"model":      effectiveModel,
			"path":       r.URL.Path,
		})
	}

	resp, err := g.callSingleProvider(r, providerName, effectiveBody, g.providerRetryPolicy(providerName))
	if err != nil {
		category, errType := gatewayErrorCategoryAndType(err)
		if g.log != nil {
			g.log.Error("embedding_provider_error", map[string]any{
				"request_id":     server.RequestIDFromContext(r.Context()),
				"provider":       providerName,
				"model":          effectiveModel,
				"error_category": category,
				"error":          err.Error(),
			})
		}
		return newJSONErrorResponseCategorized(r, http.StatusBadGateway, err.Error(), errType, category), nil
	}

	if resp == nil || resp.Body == nil {
		return resp, nil
	}

	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, readErr
	}

	promptTokens, totalTokens := parseEmbeddingUsage(body)

	if g.embeddingDimensions > 0 {
		var parsed struct {
			Data []struct {
				Embedding []float64 `json:"embedding"`
			} `json:"data"`
		}
		if jsonErr := json.Unmarshal(body, &parsed); jsonErr == nil && len(parsed.Data) > 0 {
			actual := len(parsed.Data[0].Embedding)
			if actual != g.embeddingDimensions && g.log != nil {
				g.log.Warn("embedding_dimension_mismatch", map[string]any{
					"expected": g.embeddingDimensions,
					"actual":   actual,
					"provider": providerName,
					"model":    effectiveModel,
				})
			}
		}
	}

	g.meterEmbeddingResponse(r, providerName, effectiveModel, resp.StatusCode, promptTokens, totalTokens)

	return &http.Response{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    r,
	}, nil
}

// parseEmbeddingUsage extracts prompt_tokens and total_tokens from a standard
// OpenAI embeddings response body. Returns zeros on parse failure.
func parseEmbeddingUsage(body []byte) (promptTokens, totalTokens int) {
	var parsed struct {
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return 0, 0
	}
	return parsed.Usage.PromptTokens, parsed.Usage.TotalTokens
}

// meterEmbeddingResponse saves a usage record for an embedding request.
func (g *Gateway) meterEmbeddingResponse(
	r *http.Request,
	providerName, model string,
	statusCode, promptTokens, totalTokens int,
) {
	if g.usageStore == nil {
		return
	}

	requestID := server.RequestIDFromContext(r.Context())

	record := usage.Record{
		RequestID:         requestID,
		Timestamp:         time.Now().UTC(),
		Provider:          providerName,
		Model:             model,
		PromptTokens:      promptTokens,
		CompletionTokens:  0,
		TotalTokens:       totalTokens,
		EstimatedCostUSD:  0,
		PriceFound:        false,
		CacheHit:          false,
		MeteringEstimated: false,
		Team:              r.Header.Get("X-Costguard-Team"),
		Project:           r.Header.Get("X-Costguard-Project"),
		User:              r.Header.Get("X-Costguard-User"),
		Agent:             r.Header.Get("X-Costguard-Agent"),
		Path:              r.URL.Path,
		StatusCode:        statusCode,
	}

	if err := g.usageStore.Save(r.Context(), record); err != nil && g.log != nil {
		g.log.Error("usage_save_failed", map[string]any{
			"request_id": requestID,
			"error":      err.Error(),
		})
	}
}
