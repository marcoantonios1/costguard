package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/marcoantonios1/costguard/internal/budget"
	"github.com/marcoantonios1/costguard/internal/server"
	"github.com/marcoantonios1/costguard/internal/usage"
)

// ProxySpeech handles POST /v1/audio/speech (OpenAI TTS format).
// Input is a JSON body with model, input, and voice fields.
// The upstream response is raw audio bytes (audio/mpeg or audio/opus) which
// are streamed back verbatim. Metering uses the character count of the input
// field because TTS providers charge per character, not per token.
func (g *Gateway) ProxySpeech(r *http.Request) (*http.Response, error) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	model := extractModel(bodyBytes)
	charCount := extractInputCharCount(bodyBytes)

	if g.audioTTSProvider == "local" {
		return g.proxySpeechToLocal(r, bodyBytes, model, charCount)
	}

	hintedProvider := requestedProviderHint(r)
	hintedMode := requestedModeHint(r)

	providerName := ""

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
	} else if hintedMode != "" {
		if !isSupportedMode(hintedMode) {
			return newJSONErrorResponse(r, http.StatusBadRequest, "unsupported mode hint: "+hintedMode), nil
		}
		modeProvider := strings.TrimSpace(g.modeToProvider[hintedMode])
		if modeProvider == "" {
			return newJSONErrorResponse(r, http.StatusBadRequest, "mode not configured: "+hintedMode), nil
		}
		if _, err := g.reg.Get(modeProvider); err != nil {
			return newJSONErrorResponse(r, http.StatusBadRequest, "configured provider for mode is unavailable: "+hintedMode), nil
		}
		providerName = modeProvider
	} else {
		providerName = g.router.PickProvider(model)
	}

	if g.budgetChecker != nil {
		now := time.Now()
		team := r.Header.Get("X-Costguard-Team")
		project := r.Header.Get("X-Costguard-Project")
		agent := r.Header.Get("X-Costguard-Agent")

		if err := g.budgetChecker.CheckRequestBudget(r.Context(), now, team, project, agent); err != nil {
			switch {
			case errors.Is(err, budget.ErrMonthlyBudgetExceeded):
				return newJSONErrorResponse(r, http.StatusPaymentRequired, "monthly budget exceeded"), nil
			case errors.Is(err, budget.ErrTeamBudgetExceeded):
				return newJSONErrorResponse(r, http.StatusPaymentRequired, "team monthly budget exceeded"), nil
			case errors.Is(err, budget.ErrProjectBudgetExceeded):
				return newJSONErrorResponse(r, http.StatusPaymentRequired, "project monthly budget exceeded"), nil
			case errors.Is(err, budget.ErrAgentBudgetExceeded):
				return newJSONErrorResponse(r, http.StatusPaymentRequired, "agent monthly budget exceeded"), nil
			default:
				return nil, err
			}
		}
	}

	if g.log != nil {
		g.log.Info("tts_routing", map[string]any{
			"request_id": server.RequestIDFromContext(r.Context()),
			"provider":   providerName,
			"model":      model,
			"chars":      charCount,
			"path":       r.URL.Path,
		})
	}

	resp, actualProvider, _, err := g.callProviderWithFallback(r, providerName, bodyBytes, model)
	if err != nil {
		return nil, err
	}

	if resp != nil && resp.StatusCode == http.StatusOK {
		g.meterSpeechResponse(r, actualProvider, model, charCount, resp.StatusCode)
	}

	// Return resp with body intact — the handler streams audio bytes directly.
	return resp, nil
}

// meterSpeechResponse records a usage entry for a TTS request using the
// input character count as PromptTokens (MeteringEstimated=true) since TTS
// providers bill per character, not per token.
func (g *Gateway) meterSpeechResponse(r *http.Request, providerName, model string, charCount, statusCode int) {
	if g.usageStore == nil {
		return
	}

	requestID := server.RequestIDFromContext(r.Context())

	record := usage.Record{
		RequestID:         requestID,
		Timestamp:         time.Now().UTC(),
		Provider:          providerName,
		Model:             model,
		PromptTokens:      charCount,
		CompletionTokens:  0,
		TotalTokens:       charCount,
		EstimatedCostUSD:  0,
		PriceFound:        false,
		CacheHit:          false,
		MeteringEstimated: true,
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

// proxySpeechToLocal forwards a TTS request directly to the configured local
// URL, bypassing the provider registry. The JSON body and audio response are
// preserved verbatim. If audioTTSModel is set the model field is rewritten.
func (g *Gateway) proxySpeechToLocal(r *http.Request, bodyBytes []byte, model string, charCount int) (*http.Response, error) {
	if g.audioTTSModel != "" {
		if rewritten, err := rewriteJSONModel(bodyBytes, g.audioTTSModel); err == nil {
			bodyBytes = rewritten
			model = g.audioTTSModel
		}
	}

	target := g.audioTTSURL + r.URL.Path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header = r.Header.Clone()
	req.Header.Del("X-Costguard-Provider")
	req.Header.Del("X-Costguard-Mode")

	if g.log != nil {
		g.log.Info("tts_routing", map[string]any{
			"request_id": server.RequestIDFromContext(r.Context()),
			"provider":   "local",
			"target":     g.audioTTSURL,
			"model":      model,
			"chars":      charCount,
			"path":       r.URL.Path,
		})
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp != nil && resp.StatusCode == http.StatusOK {
		g.meterSpeechResponse(r, "local", model, charCount, resp.StatusCode)
	}

	return resp, nil
}

// rewriteJSONModel sets the "model" field in a JSON object to newModel.
func rewriteJSONModel(body []byte, newModel string) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	v, _ := json.Marshal(newModel)
	m["model"] = v
	return json.Marshal(m)
}

// extractInputCharCount returns the UTF-8 character count of the "input" field.
func extractInputCharCount(body []byte) int {
	var v struct {
		Input string `json:"input"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return 0
	}
	return len([]rune(v.Input))
}
