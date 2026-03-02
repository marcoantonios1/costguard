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

// PickProvider returns the provider name to use for a given model.
// Phase A: pure config-based mapping with fallback.
func (r *Router) PickProvider(model string) string {
	if model == "" {
		return r.defaultProvider
	}
	if p, ok := r.modelToProvider[model]; ok && p != "" {
		return p
	}
	return r.defaultProvider
}
