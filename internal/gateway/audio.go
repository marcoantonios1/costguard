package gateway

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/marcoantonios1/costguard/internal/budget"
	"github.com/marcoantonios1/costguard/internal/server"
	"github.com/marcoantonios1/costguard/internal/usage"
)

// ProxyAudio handles multipart/form-data audio transcription requests
// (POST /v1/audio/transcriptions) and proxies them to the selected provider.
// Unlike Proxy, it does not attempt JSON body parsing, caching, or model
// rewriting — multipart bodies are forwarded verbatim. Metering saves a
// zero-token record with MeteringEstimated=true because Whisper-style
// responses do not include token counts.
func (g *Gateway) ProxyAudio(r *http.Request) (*http.Response, error) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	model := extractModelFromMultipart(r.Header.Get("Content-Type"), bodyBytes)

	if g.audioTranscriptionProvider == "local" {
		return g.proxyAudioToLocal(r, bodyBytes, model)
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
		g.log.Info("audio_transcription_routing", map[string]any{
			"request_id": server.RequestIDFromContext(r.Context()),
			"provider":   providerName,
			"model":      model,
			"path":       r.URL.Path,
		})
	}

	resp, actualProvider, _, err := g.callProviderWithFallback(r, providerName, bodyBytes, model)
	if err != nil {
		return nil, err
	}

	if resp != nil && resp.Body != nil && resp.StatusCode == http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}

		g.meterAudioResponse(r, actualProvider, model, resp.StatusCode)

		return &http.Response{
			StatusCode: resp.StatusCode,
			Header:     resp.Header.Clone(),
			Body:       io.NopCloser(bytes.NewReader(body)),
			Request:    r,
		}, nil
	}

	return resp, nil
}

// meterAudioResponse records a zero-token usage entry for an audio transcription.
// Whisper-style responses carry no token counts, so MeteringEstimated is set.
func (g *Gateway) meterAudioResponse(r *http.Request, providerName, model string, statusCode int) {
	if g.usageStore == nil {
		return
	}

	requestID := server.RequestIDFromContext(r.Context())

	record := usage.Record{
		RequestID:         requestID,
		Timestamp:         time.Now().UTC(),
		Provider:          providerName,
		Model:             model,
		PromptTokens:      0,
		CompletionTokens:  0,
		TotalTokens:       0,
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

// proxyAudioToLocal forwards a transcription request directly to the configured
// local URL, bypassing the provider registry. The multipart body and all
// relevant headers are preserved verbatim.
func (g *Gateway) proxyAudioToLocal(r *http.Request, bodyBytes []byte, model string) (*http.Response, error) {
	target := g.audioTranscriptionURL + r.URL.Path
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
		g.log.Info("audio_transcription_routing", map[string]any{
			"request_id": server.RequestIDFromContext(r.Context()),
			"provider":   "local",
			"target":     g.audioTranscriptionURL,
			"model":      model,
			"path":       r.URL.Path,
		})
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp != nil && resp.Body != nil && resp.StatusCode == http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		g.meterAudioResponse(r, "local", model, resp.StatusCode)
		return &http.Response{
			StatusCode: resp.StatusCode,
			Header:     resp.Header.Clone(),
			Body:       io.NopCloser(bytes.NewReader(body)),
			Request:    r,
		}, nil
	}

	return resp, nil
}

// extractModelFromMultipart reads the "model" field from a multipart/form-data body.
func extractModelFromMultipart(contentType string, body []byte) string {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	boundary, ok := params["boundary"]
	if !ok {
		return ""
	}
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		if p.FormName() == "model" {
			v, _ := io.ReadAll(p)
			return string(v)
		}
	}
	return ""
}
