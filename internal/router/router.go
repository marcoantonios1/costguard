package router

import (
	"sort"

	"github.com/marcoantonios1/costguard/internal/logging"
)

// ModelCatalog is the subset of *providers.Catalog the router needs.
// Nil is a valid value — all catalog checks become no-ops.
type ModelCatalog interface {
	ModelSupported(providerName, modelID string) bool
}

// ProviderPriority resolves the numeric priority for a provider name.
// Nil is a valid value — priority-based selection is disabled.
type ProviderPriority interface {
	Priority(providerName string) int
}

type Router struct {
	defaultProvider    string
	modelToProvider    map[string]string
	availableProviders map[string]bool
	catalog            ModelCatalog
	priority           ProviderPriority
	log                *logging.Log
}

type Config struct {
	DefaultProvider    string
	ModelToProvider    map[string]string
	AvailableProviders map[string]bool
	Catalog            ModelCatalog    // may be nil (no-op)
	Priority           ProviderPriority // may be nil (legacy order)
	Log                *logging.Log
}

func New(cfg Config) *Router {
	m := cfg.ModelToProvider
	if m == nil {
		m = map[string]string{}
	}

	available := cfg.AvailableProviders
	if available == nil {
		available = map[string]bool{}
	}

	return &Router{
		defaultProvider:    cfg.DefaultProvider,
		modelToProvider:    m,
		availableProviders: available,
		catalog:            cfg.Catalog,
		priority:           cfg.Priority,
		log:                cfg.Log,
	}
}

func (r *Router) isAvailable(provider string) bool {
	if provider == "" {
		return false
	}
	if len(r.availableProviders) == 0 {
		return true
	}
	return r.availableProviders[provider]
}

func (r *Router) PickProvider(model string) string {
	if r.priority != nil {
		return r.pickWithPriority(model)
	}
	return r.pickWithLegacyOrder(model)
}

// pickWithLegacyOrder is the original staged resolution: exact_mapping →
// model_matcher → default. First valid candidate wins; existing log keys are
// preserved unchanged.
func (r *Router) pickWithLegacyOrder(model string) string {
	if provider := r.pickFromExactMapping(model); provider != "" {
		if !r.isAvailable(provider) {
			r.logSkippedUnavailable(model, provider, "exact_mapping")
		} else if !r.catalogAllows(provider, model) {
			r.logSkippedCatalog(model, provider, "exact_mapping")
		} else {
			return provider
		}
	}

	if provider := r.pickFromMatchers(model); provider != "" {
		if !r.isAvailable(provider) {
			r.logSkippedUnavailable(model, provider, "matcher")
		} else if !r.catalogAllows(provider, model) {
			r.logSkippedCatalog(model, provider, "matcher")
		} else {
			return provider
		}
	}

	if !r.isAvailable(r.defaultProvider) {
		r.logSkippedUnavailable(model, r.defaultProvider, "default")
		return ""
	}
	if !r.catalogAllows(r.defaultProvider, model) {
		r.logSkippedCatalog(model, r.defaultProvider, "default")
		return ""
	}
	return r.defaultProvider
}

type candidate struct {
	provider string
	priority int
	reason   string
}

// pickWithPriority honours explicit model_to_provider mappings as authoritative
// (user intent), then ranks matcher and default candidates by priority (desc)
// with lexicographic name as tiebreaker.
// Emits provider_selected / provider_candidate_skipped / provider_not_found.
func (r *Router) pickWithPriority(model string) string {
	// Explicit mapping is the user's direct choice — honour it immediately.
	if provider := r.pickFromExactMapping(model); provider != "" {
		if !r.isAvailable(provider) {
			r.logSkippedUnavailable(model, provider, "exact_mapping")
		} else if !r.catalogAllows(provider, model) {
			r.logSkippedCatalog(model, provider, "exact_mapping")
		} else {
			if r.log != nil {
				r.log.Info("provider_selected", map[string]any{
					"model":      model,
					"provider":   provider,
					"reason":     "exact_mapping",
					"priority":   r.priority.Priority(provider),
					"candidates": 1,
				})
			}
			return provider
		}
	}

	// No explicit mapping (or it was blocked): rank matcher + default by priority.
	var candidates []candidate
	seen := map[string]bool{}

	tryAdd := func(provider, stage string) {
		if provider == "" || seen[provider] {
			return
		}
		seen[provider] = true
		if !r.isAvailable(provider) {
			r.logSkippedUnavailable(model, provider, stage)
			return
		}
		if !r.catalogAllows(provider, model) {
			r.logSkippedCatalog(model, provider, stage)
			return
		}
		candidates = append(candidates, candidate{
			provider: provider,
			priority: r.priority.Priority(provider),
			reason:   stage,
		})
	}

	tryAdd(r.pickFromMatchers(model), "model_matcher")
	tryAdd(r.defaultProvider, "default")

	if len(candidates) == 0 {
		if r.log != nil {
			r.log.Info("provider_not_found", map[string]any{"model": model})
		}
		return ""
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority > candidates[j].priority
		}
		return candidates[i].provider < candidates[j].provider
	})

	winner := candidates[0]
	for _, c := range candidates[1:] {
		if r.log != nil {
			r.log.Info("provider_candidate_skipped", map[string]any{
				"provider": c.provider,
				"priority": c.priority,
				"reason":   "lower_priority",
			})
		}
	}

	if r.log != nil {
		r.log.Info("provider_selected", map[string]any{
			"model":      model,
			"provider":   winner.provider,
			"reason":     winner.reason,
			"priority":   winner.priority,
			"candidates": len(candidates),
		})
	}

	return winner.provider
}

func (r *Router) catalogAllows(provider, model string) bool {
	if r.catalog == nil {
		return true
	}
	return r.catalog.ModelSupported(provider, model)
}

func (r *Router) logSkippedUnavailable(model, provider, stage string) {
	if r.log != nil {
		r.log.Info("provider_skipped_unavailable", map[string]any{
			"model":    model,
			"provider": provider,
			"stage":    stage,
		})
	}
}

func (r *Router) logSkippedCatalog(model, provider, stage string) {
	if r.log != nil {
		r.log.Info("provider_skipped_model_not_in_catalog", map[string]any{
			"model":    model,
			"provider": provider,
			"stage":    stage,
		})
	}
}

func (r *Router) pickFromExactMapping(model string) string {
	if model == "" {
		return ""
	}

	if p, ok := r.modelToProvider[model]; ok && p != "" {
		return p
	}

	return ""
}

func (r *Router) pickFromMatchers(model string) string {
	if model == "" {
		return ""
	}

	return MatchProviderByModel(model)
}
