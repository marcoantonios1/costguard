package router

import "github.com/marcoantonios1/costguard/internal/config"

func ResolveProvider(cfg config.RoutingConfig, model string) string {
	if provider, ok := cfg.ModelToProvider[model]; ok && provider != "" {
		return provider
	}

	if matched := MatchProviderByModel(model); matched != "" {
		return matched
	}

	return cfg.DefaultProvider
}
