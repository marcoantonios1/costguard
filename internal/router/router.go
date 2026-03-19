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
	if provider := r.pickFromExactMapping(model); provider != "" {
		return provider
	}

	if provider := r.pickFromMatchers(model); provider != "" {
		return provider
	}

	return r.defaultProvider
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
