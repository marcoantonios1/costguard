package gateway

import (
	"bytes"
	"errors"
	"io"
	"net/http"
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
