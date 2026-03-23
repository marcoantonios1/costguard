package router

import "github.com/marcoantonios1/costguard/internal/logging"

type Router struct {
	defaultProvider    string
	modelToProvider    map[string]string
	availableProviders map[string]bool
	log                *logging.Log
}

type Config struct {
	DefaultProvider    string
	ModelToProvider    map[string]string
	AvailableProviders map[string]bool
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
		if r.isAvailable(provider) {
			return provider
		}
		if r.log != nil {
			r.log.Info("provider_skipped_unavailable", map[string]any{
				"model":    model,
				"provider": provider,
				"stage":    "exact_mapping",
			})
		}
	}

	if provider := r.pickFromMatchers(model); provider != "" {
		if r.isAvailable(provider) {
			return provider
		}
		if r.log != nil {
			r.log.Info("provider_skipped_unavailable", map[string]any{
				"model":    model,
				"provider": provider,
				"stage":    "matcher",
			})
		}
	}

	if r.isAvailable(r.defaultProvider) {
		return r.defaultProvider
	}

	if r.log != nil {
		r.log.Info("provider_skipped_unavailable", map[string]any{
			"model":    model,
			"provider": r.defaultProvider,
			"stage":    "default",
		})
	}

	return ""
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
