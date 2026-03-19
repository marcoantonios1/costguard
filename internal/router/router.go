package router

type Router struct {
	defaultProvider string
	modelToProvider map[string]string
}

type Config struct {
	DefaultProvider string
	ModelToProvider map[string]string
}

func New(cfg Config) *Router {
	m := cfg.ModelToProvider
	if m == nil {
		m = map[string]string{}
	}
	return &Router{
		defaultProvider: cfg.DefaultProvider,
		modelToProvider: m,
	}
}

func (r *Router) PickProvider(model string) string {
	if model == "" {
		return r.defaultProvider
	}

	// 1. Exact config mapping (highest priority)
	if p, ok := r.modelToProvider[model]; ok && p != "" {
		return p
	}

	// 2. Pattern-based matching
	if matched := MatchProviderByModel(model); matched != "" {
		return matched
	}

	// 3. Default fallback
	return r.defaultProvider
}
