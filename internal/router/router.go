package router

import "github.com/marcoantonios1/costguard/internal/logging"

// ModelCatalog is the subset of *providers.Catalog the router needs.
// Nil is a valid value — all catalog checks become no-ops.
type ModelCatalog interface {
	ModelSupported(providerName, modelID string) bool
}

type Router struct {
	defaultProvider    string
	modelToProvider    map[string]string
	availableProviders map[string]bool
	catalog            ModelCatalog
	log                *logging.Log
}

type Config struct {
	DefaultProvider    string
	ModelToProvider    map[string]string
	AvailableProviders map[string]bool
	Catalog            ModelCatalog // may be nil (no-op)
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
