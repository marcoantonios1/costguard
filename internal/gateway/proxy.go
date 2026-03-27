package gateway

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/marcoantonios1/costguard/internal/budget"
	"github.com/marcoantonios1/costguard/internal/server"
)

func (g *Gateway) Proxy(r *http.Request) (*http.Response, error) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	model := extractModel(bodyBytes)

	hintedProvider := requestedProviderHint(r)
	hintedMode := requestedModeHint(r)

	requestedProvider := ""
	providerName := ""

	if hintedProvider != "" {
		if _, err := g.reg.Get(hintedProvider); err != nil {
			if g.log != nil {
				g.log.Info("provider_hint_rejected", map[string]any{
					"request_id": server.RequestIDFromContext(r.Context()),
					"provider":   hintedProvider,
					"path":       r.URL.Path,
					"model":      model,
					"reason":     "unknown_provider",
				})
			}

			return newJSONErrorResponse(
				r,
				http.StatusBadRequest,
				"unknown provider hint: "+hintedProvider,
			), nil
		}

		requestedProvider = hintedProvider
		providerName = hintedProvider

		if g.log != nil {
			g.log.Info("provider_hint_accepted", map[string]any{
				"request_id": server.RequestIDFromContext(r.Context()),
				"provider":   hintedProvider,
				"path":       r.URL.Path,
				"model":      model,
			})
		}
	} else if hintedMode != "" {
		if !isSupportedMode(hintedMode) {
			if g.log != nil {
				g.log.Info("mode_hint_rejected", map[string]any{
					"request_id": server.RequestIDFromContext(r.Context()),
					"mode":       hintedMode,
					"path":       r.URL.Path,
					"model":      model,
					"reason":     "unsupported_mode",
				})
			}

			return newJSONErrorResponse(
				r,
				http.StatusBadRequest,
				"unsupported mode hint: "+hintedMode,
			), nil
		}

		modeProvider := strings.TrimSpace(g.modeToProvider[hintedMode])
		if modeProvider == "" {
			if g.log != nil {
				g.log.Info("mode_hint_rejected", map[string]any{
					"request_id": server.RequestIDFromContext(r.Context()),
					"mode":       hintedMode,
					"path":       r.URL.Path,
					"model":      model,
					"reason":     "mode_not_configured",
				})
			}

			return newJSONErrorResponse(
				r,
				http.StatusBadRequest,
				"mode not configured: "+hintedMode,
			), nil
		}

		if _, err := g.reg.Get(modeProvider); err != nil {
			if g.log != nil {
				g.log.Info("mode_hint_rejected", map[string]any{
					"request_id": server.RequestIDFromContext(r.Context()),
					"mode":       hintedMode,
					"provider":   modeProvider,
					"path":       r.URL.Path,
					"model":      model,
					"reason":     "unknown_provider",
				})
			}

			return newJSONErrorResponse(
				r,
				http.StatusBadRequest,
				"configured provider for mode is unavailable: "+hintedMode,
			), nil
		}

		requestedProvider = modeProvider
		providerName = modeProvider

		if g.log != nil {
			g.log.Info("mode_hint_accepted", map[string]any{
				"request_id": server.RequestIDFromContext(r.Context()),
				"mode":       hintedMode,
				"provider":   modeProvider,
				"path":       r.URL.Path,
				"model":      model,
			})
		}
	} else {
		providerName = g.router.PickProvider(model)
		requestedProvider = providerName

		if g.log != nil {
			g.log.Info("route_selected", map[string]any{
				"request_id": server.RequestIDFromContext(r.Context()),
				"model":      model,
				"provider":   providerName,
				"path":       r.URL.Path,
			})
		}
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
		now := time.Now()
		team := r.Header.Get("X-Costguard-Team")
		project := r.Header.Get("X-Costguard-Project")

		if err := g.budgetChecker.CheckRequestBudget(r.Context(), now, team, project); err != nil {
			if errors.Is(err, budget.ErrMonthlyBudgetExceeded) {
				g.emitMonthlyBudgetAlertOnce(r.Context(), now, 100)

				if g.log != nil {
					g.log.Error("monthly_budget_exceeded", map[string]any{
						"request_id": server.RequestIDFromContext(r.Context()),
						"path":       r.URL.Path,
						"model":      model,
						"provider":   providerName,
					})
				}

				return newJSONErrorResponse(
					r,
					http.StatusPaymentRequired,
					"monthly budget exceeded",
				), nil
			}

			if errors.Is(err, budget.ErrTeamBudgetExceeded) {
				if g.log != nil {
					g.log.Error("team_budget_exceeded", map[string]any{
						"request_id": server.RequestIDFromContext(r.Context()),
						"path":       r.URL.Path,
						"team":       team,
						"model":      model,
						"provider":   providerName,
					})
				}

				return newJSONErrorResponse(
					r,
					http.StatusPaymentRequired,
					"team monthly budget exceeded",
				), nil
			}

			if errors.Is(err, budget.ErrProjectBudgetExceeded) {
				if g.log != nil {
					g.log.Error("project_budget_exceeded", map[string]any{
						"request_id": server.RequestIDFromContext(r.Context()),
						"path":       r.URL.Path,
						"project":    project,
						"model":      model,
						"provider":   providerName,
					})
				}

				return newJSONErrorResponse(
					r,
					http.StatusPaymentRequired,
					"project monthly budget exceeded",
				), nil
			}

			return nil, err
		}
	}

	if g.log != nil {
		g.log.Info("provider_call", map[string]any{
			"request_id":         server.RequestIDFromContext(r.Context()),
			"provider":           providerName,
			"requested_provider": requestedProvider,
			"requested_mode":     hintedMode,
			"path":               r.URL.Path,
		})
	}

	resp, actualProvider, finalModel, err := g.callProviderWithFallback(r, providerName, bodyBytes, model)
	if err != nil {
		return nil, err
	}

	if g.log != nil && actualProvider != requestedProvider {
		g.log.Info("provider_resolution_changed", map[string]any{
			"request_id":         server.RequestIDFromContext(r.Context()),
			"requested_provider": requestedProvider,
			"actual_provider":    actualProvider,
			"requested_mode":     hintedMode,
			"original_model":     model,
			"final_model":        finalModel,
			"path":               r.URL.Path,
		})
	}

	return g.maybeStoreAndReturn(r, resp, actualProvider, finalModel, cacheable, cacheKey)
}